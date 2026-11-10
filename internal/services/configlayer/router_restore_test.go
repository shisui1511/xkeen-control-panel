//go:build router

// router-area: configlayer

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// Откат зафиксированного применения к снимку (RunRestore) на устройстве: настоящий
// слой, хранилище и `xray run -test` роутера, временный каталог в рабочем каталоге
// теста. Работающие ядра стенда не трогаются (Applier не задан), тест ядро не
// читает и не переключает — метка ядра не нужна.

// rtSectionNames — отсортированные имена секций.
func rtSectionNames(s Sections) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rtSectionsEqual сверяет две карты секций посекционно, без лишних секций.
func rtSectionsEqual(t *testing.T, what string, got, want Sections) {
	t.Helper()
	gn, wn := rtSectionNames(got), rtSectionNames(want)
	if len(gn) != len(wn) {
		t.Errorf("%s: секции %v, ожидались %v", what, gn, wn)
		return
	}
	for i := range gn {
		if gn[i] != wn[i] {
			t.Errorf("%s: секции %v, ожидались %v", what, gn, wn)
			return
		}
		if !SectionEqual(got[gn[i]], want[gn[i]]) {
			t.Errorf("%s[%s] = %s, ожидалось %s", what, gn[i], got[gn[i]], want[gn[i]])
		}
	}
}

func TestRouterLayerRestore(t *testing.T) {
	t.Run("restore_applied", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		// Живой ключ, которого в снимке нет, и ключ, который снимок удаляет.
		if err := e.layer.UpdateLive(func(live Sections) error {
			live["rt_keep"] = json.RawMessage(`{"k":1}`)
			live["rt_drop"] = json.RawMessage(`{"d":1}`)
			return nil
		}); err != nil {
			t.Fatalf("UpdateLive: %v", err)
		}
		stateA := e.state(t)
		fileA := hashOf(e.file)

		// Состояние B: подмена секции меняет файл и добавляет секцию, которой в A нет.
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger: TriggerDNSTakeover,
			Sections: Sections{
				"rt_log":   json.RawMessage(`{"access":"none","level":"debug"}`),
				"rt_extra": json.RawMessage(`{"y":1}`),
			},
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска B: %+v", view.Result)
		}
		if hashOf(e.file) == fileA {
			t.Fatal("запуск B не изменил файл генератора")
		}
		// Правка пользователя в другой секции черновика.
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)

		view, err = e.layer.RunRestore(context.Background(), RestoreRun{
			Trigger: TriggerDNSRestore,
			Applied: stateA.Applied,
			Live:    Sections{"rt_drop": json.RawMessage(`null`), "rt_new": json.RawMessage(`{"n":1}`)},
		})
		if err != nil {
			t.Fatalf("RunRestore: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог отката: %+v", view.Result)
		}
		after := e.state(t)
		rtSectionsEqual(t, "Applied", after.Applied, stateA.Applied)
		if got := hashOf(e.file); got != fileA {
			t.Errorf("файл после отката %s, ожидался файл состояния A %s", got, fileA)
		}
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":1}`)
		rtSectionsEqual(t, "Draft", after.Draft, before.Draft)
		if after.DraftRevision != before.DraftRevision {
			t.Errorf("ревизия черновика %d -> %d", before.DraftRevision, after.DraftRevision)
		}
		rtSecEqual(t, "Live[rt_keep]", after.Live["rt_keep"], `{"k":1}`)
		rtSecEqual(t, "Live[rt_new]", after.Live["rt_new"], `{"n":1}`)
		if _, has := after.Live["rt_drop"]; has {
			t.Errorf("ключ живых данных с null в снимке остался: %s", after.Live["rt_drop"])
		}
	})

	t.Run("reset_draft", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.edit(t, "rt_other", `{"x":1}`)
		e.applyDraft(t)
		stateA := e.state(t)
		fileA := hashOf(e.file)

		// B: обе секции подменены, черновик получает значения подмены целиком.
		overlay := Sections{
			"rt_log":   json.RawMessage(`{"access":"none","level":"debug"}`),
			"rt_other": json.RawMessage(`{"x":2}`),
		}
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:  TriggerDNSTakeover,
			Sections: overlay,
			MergeDraft: func(name string, _ json.RawMessage) (json.RawMessage, error) {
				return overlay[name], nil
			},
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска B: %+v", view.Result)
		}
		// Пользователь правит rt_other после B; rt_log не трогает.
		e.edit(t, "rt_other", `{"x":3}`)
		before := e.state(t)

		view, err = e.layer.RunRestore(context.Background(), RestoreRun{
			Trigger:            TriggerDNSRestore,
			Applied:            stateA.Applied,
			ResetDraftSections: []string{"rt_log", "rt_other"},
		})
		if err != nil {
			t.Fatalf("RunRestore: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог отката: %+v", view.Result)
		}
		after := e.state(t)
		rtSectionsEqual(t, "Applied", after.Applied, stateA.Applied)
		if got := hashOf(e.file); got != fileA {
			t.Errorf("файл после отката %s, ожидался файл состояния A %s", got, fileA)
		}
		rtSecEqual(t, "Draft[rt_log]", after.Draft["rt_log"], `{"access":"none"}`)
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":3}`)
		if after.DraftRevision != before.DraftRevision+1 {
			t.Errorf("ревизия %d -> %d, ожидался +1", before.DraftRevision, after.DraftRevision)
		}
	})
}

const rtHookFile = "xcp-rthook.json"

// rtHookGen пишет файл Xray из живого ключа rt_hook (level и bad, как rtSecGen) и
// запоминает значения rt_hook, которые видел во входе.
type rtHookGen struct {
	mu   sync.Mutex
	seen []json.RawMessage
}

func (*rtHookGen) ID() string { return "rt-hook" }

func (g *rtHookGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	raw, ok := in.Live["rt_hook"]
	if !ok {
		return nil, nil
	}
	g.mu.Lock()
	g.seen = append(g.seen, cloneRaw(raw))
	g.mu.Unlock()
	var sec struct {
		Level string `json:"level"`
		Bad   bool   `json:"bad"`
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return nil, err
	}
	doc := map[string]any{"log": map[string]any{"loglevel": sec.Level}}
	if sec.Bad {
		doc["outbounds"] = []map[string]any{{"tag": "xcp-rt-bad", "protocol": "xcp-no-such-protocol"}}
	}
	content, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return []GeneratedFile{{Kernel: KernelXray, RelPath: rtHookFile, Kind: KindXrayJSON, Content: content}}, nil
}

// sawLive — генератор видел во входе значение rt_hook, равное want.
func (g *rtHookGen) sawLive(want string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, v := range g.seen {
		if SectionEqual(v, json.RawMessage(want)) {
			return true
		}
	}
	return false
}

type rtHookEnv struct {
	*rtSecEnv
	gen      *rtHookGen
	hookFile string

	mu      sync.Mutex
	applies []StateView
}

// rtNewHookEnv — слой с генераторами rtSecGen и rtHookGen и хуком BeforeApply,
// который запоминает свой вход. before (не больше одного) регистрируется до Start
// и вызывается тем же хуком после записи входа, синхронно под замком применения.
func rtNewHookEnv(t *testing.T, before ...func(e *rtHookEnv, st StateView)) *rtHookEnv {
	t.Helper()
	if len(before) > 1 {
		t.Fatal("rtNewHookEnv: допустим один колбэк BeforeApply")
	}
	if _, err := os.Stat(rtRefreshXrayBin); err != nil {
		t.Skipf("Xray не установлен на устройстве: %v", err)
	}
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, roots.Mihomo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	e := &rtHookEnv{
		rtSecEnv: &rtSecEnv{layer: layer, dir: dir, file: filepath.Join(roots.Xray, rtSecFile)},
		gen:      &rtHookGen{},
		hookFile: filepath.Join(roots.Xray, rtHookFile),
	}
	if err := layer.Register(Hooks{
		Generators: []Generator{rtSecGen{}, e.gen},
		BeforeApply: func(st StateView) {
			e.mu.Lock()
			e.applies = append(e.applies, st)
			e.mu.Unlock()
			if len(before) == 1 {
				before[0](e, st)
			}
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	layer.Start()
	return e
}

// liveRun — RunSectionLive только с живым ключом rt_hook.
func (e *rtHookEnv) liveRun(t *testing.T, value string, beforeWrite func(context.Context) error) ApplyView {
	t.Helper()
	view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
		Trigger:     TriggerDNSTakeover,
		Live:        Sections{"rt_hook": json.RawMessage(value)},
		BeforeWrite: beforeWrite,
	})
	if err != nil {
		t.Fatalf("RunSectionLive: %v", err)
	}
	if view.Result == nil {
		t.Fatal("нет итога запуска")
	}
	return view
}

func TestRouterLayerSectionLiveHooks(t *testing.T) {
	t.Run("live_only", func(t *testing.T) {
		e := rtNewHookEnv(t)
		value := `{"level":"warning"}`
		view := e.liveRun(t, value, nil)
		if view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		st, err := e.layer.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		rtSecEqual(t, "Live[rt_hook]", st.Live["rt_hook"], value)
		if !e.gen.sawLive(value) {
			t.Error("генератор не видел живой ключ запуска во входе")
		}
		if _, err := os.Stat(e.hookFile); err != nil {
			t.Errorf("файл генератора не записан: %v", err)
		}
	})

	t.Run("before_write_error", func(t *testing.T) {
		e := rtNewHookEnv(t)
		if v := e.liveRun(t, `{"level":"warning"}`, nil); v.Result.Code != ResultApplied {
			t.Fatalf("исходный запуск: %+v", v.Result)
		}
		before := e.state(t)
		fileBefore := hashOf(e.hookFile)
		called := 0
		view := e.liveRun(t, `{"level":"error"}`, func(context.Context) error {
			called++
			return errors.New("xcp-rt: отказ до записи")
		})
		if view.Result.OK || view.Result.Code != ResultBeforeWriteFailed {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		if called != 1 {
			t.Errorf("BeforeWrite вызван %d раз, ожидался 1", called)
		}
		if got := hashOf(e.hookFile); got != fileBefore {
			t.Errorf("файл изменён после отказа BeforeWrite: %s -> %s", fileBefore, got)
		}
		after := e.state(t)
		rtSectionsEqual(t, "Applied", after.Applied, before.Applied)
		rtSecEqual(t, "Live[rt_hook]", after.Live["rt_hook"], `{"level":"warning"}`)
	})

	t.Run("before_write_after_validation", func(t *testing.T) {
		e := rtNewHookEnv(t)
		if v := e.liveRun(t, `{"level":"warning"}`, nil); v.Result.Code != ResultApplied {
			t.Fatalf("исходный запуск: %+v", v.Result)
		}
		fileBefore := hashOf(e.hookFile)
		called := false
		view := e.liveRun(t, `{"level":"warning","bad":true}`, func(context.Context) error {
			called = true
			return nil
		})
		if view.Result.OK || view.Result.Code != CodeValidationFailed {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		if called {
			t.Error("BeforeWrite вызван, хотя Xray отверг конфигурацию")
		}
		if got := hashOf(e.hookFile); got != fileBefore {
			t.Errorf("файл изменён после отказа проверки: %s -> %s", fileBefore, got)
		}
		rtSecEqual(t, "Live[rt_hook]", e.state(t).Live["rt_hook"], `{"level":"warning"}`)
	})

	t.Run("before_apply", func(t *testing.T) {
		e := rtNewHookEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		e.mu.Lock()
		got := append([]StateView(nil), e.applies...)
		e.mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("BeforeApply вызван %d раз, ожидался 1", len(got))
		}
		rtSecEqual(t, "BeforeApply Draft[rt_log]", got[0].Draft["rt_log"], `{"access":"none"}`)
		if _, has := got[0].Applied["rt_log"]; has {
			t.Errorf("BeforeApply получил Applied уже после применения: %s", got[0].Applied["rt_log"])
		}
	})
}

// rtSnapshotInjector — поздняя правка черновика внутри хука BeforeApply, то есть
// между подтверждением ревизии в StartApplyAt и сборкой плана. Срабатывает один
// раз после arm, итог правки запоминает.
type rtSnapshotInjector struct {
	mu       sync.Mutex
	sections map[string]json.RawMessage
	fired    int
	err      error
}

func (in *rtSnapshotInjector) arm(sections map[string]json.RawMessage) {
	in.mu.Lock()
	in.sections = sections
	in.mu.Unlock()
}

// hook — колбэк rtNewHookEnv: настоящий EditDraftBatch по ревизии, которую видит хук.
func (in *rtSnapshotInjector) hook(e *rtHookEnv, st StateView) {
	in.mu.Lock()
	sections := in.sections
	in.sections = nil
	in.mu.Unlock()
	if sections == nil {
		return
	}
	_, err := e.layer.EditDraftBatch(st.DraftRevision, sections, nil)
	in.mu.Lock()
	in.fired++
	in.err = err
	in.mu.Unlock()
}

func (in *rtSnapshotInjector) result() (int, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.fired, in.err
}

// rtApplyAt — «Применить» с ревизией окна изменений; ждёт apply_done из подписки
// (не прошлый итог конвейера).
func rtApplyAt(t *testing.T, e *rtHookEnv, rev int64) ApplyView {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApplyAt(true, rev); err != nil {
		t.Fatalf("G149-01 snapshot: StartApplyAt(%d): %v", rev, err)
	}
	return waitApplyDone(t, events)
}

func rtSnapEqual(t *testing.T, what string, got json.RawMessage, want string) {
	t.Helper()
	if !SectionEqual(got, json.RawMessage(want)) {
		t.Errorf("G149-01 snapshot: %s = %s, ожидалось %s", what, got, want)
	}
}

// rtSnapLog — ключи log файла генератора rtSecGen.
func rtSnapLog(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("G149-01 snapshot: файл генератора: %v", err)
	}
	var doc struct {
		Log map[string]string `json:"log"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("G149-01 snapshot: файл генератора не JSON: %v", err)
	}
	return doc.Log
}

// rtSnapLate сверяет состояние после подтверждённого запуска с поздней правкой:
// Applied — снимок подтверждения, Draft — поздняя правка, ревизия не меньше
// ревизии правки, черновик открыт.
func rtSnapLate(t *testing.T, e *rtHookEnv, rev int64, applied, draft, late string) State {
	t.Helper()
	st := e.state(t)
	rtSnapEqual(t, "Applied[rt_log]", st.Applied["rt_log"], applied)
	if v, has := st.Applied["rt_late"]; has {
		t.Errorf("G149-01 snapshot: поздняя секция попала в Applied: %s", v)
	}
	rtSnapEqual(t, "Draft[rt_log]", st.Draft["rt_log"], draft)
	rtSnapEqual(t, "Draft[rt_late]", st.Draft["rt_late"], late)
	if st.DraftRevision < rev+1 {
		t.Errorf("G149-01 snapshot: ревизия черновика %d, ожидалась не меньше %d", st.DraftRevision, rev+1)
	}
	if !draftDirty(st) {
		t.Error("G149-01 snapshot: поздняя правка пропала из неприменённых изменений")
	}
	return st
}

// rtSnapClean — повторное «Применить» без инъекции применяет черновик целиком.
func rtSnapClean(t *testing.T, e *rtHookEnv, rev int64, late string) {
	t.Helper()
	st := e.state(t)
	if st.DraftRevision != rev {
		t.Errorf("G149-01 snapshot: ревизия после чистого применения %d, ожидалась %d", st.DraftRevision, rev)
	}
	if draftDirty(st) {
		t.Errorf("G149-01 snapshot: черновик не чист после применения поздней правки: Draft %v, Applied %v", rtSectionNames(st.Draft), rtSectionNames(st.Applied))
	}
	rtSnapEqual(t, "Applied[rt_late]", st.Applied["rt_late"], late)
}

// Подтверждение ревизии закрепляет снимок черновика (G149-01, D-08, D-09): правка,
// внесённая настоящим EditDraftBatch между проверкой ревизии в StartApplyAt и
// сборкой (хук BeforeApply работает как раз в этом окне), не применяется и остаётся
// в черновике (D-12, D-13). Настоящий слой, хранилище и `xray run -test` роутера;
// работающие ядра стенда не трогаются, ядро тест не читает и не переключает.
func TestRouterApplyRevisionSnapshot(t *testing.T) {
	const late = `{"late":1}`

	t.Run("write", func(t *testing.T) {
		inj := &rtSnapshotInjector{}
		e := rtNewHookEnv(t, inj.hook)
		const valA = `{"access":"none"}`
		const valB = `{"access":"none","level":"debug"}`
		e.edit(t, "rt_log", valA)
		rev := e.state(t).DraftRevision
		inj.arm(map[string]json.RawMessage{"rt_log": json.RawMessage(valB), "rt_late": json.RawMessage(late)})

		view := rtApplyAt(t, e, rev)
		if fired, err := inj.result(); fired != 1 || err != nil {
			t.Fatalf("G149-01 snapshot: поздняя правка не внесена: срабатываний %d, ошибка %v", fired, err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("G149-01 snapshot: итог подтверждённого запуска %+v, ожидался %s", view.Result, ResultApplied)
		}
		if lvl, has := rtSnapLog(t, e.file)["loglevel"]; has {
			t.Errorf("G149-01 snapshot: поздняя правка level попала в файл: loglevel=%q", lvl)
		}
		e.mu.Lock()
		seen := append([]StateView(nil), e.applies...)
		e.mu.Unlock()
		if len(seen) != 1 {
			t.Fatalf("G149-01 snapshot: BeforeApply вызван %d раз, ожидался 1", len(seen))
		}
		rtSnapEqual(t, "BeforeApply Draft[rt_log]", seen[0].Draft["rt_log"], valA)
		if seen[0].DraftRevision != rev {
			t.Errorf("G149-01 snapshot: BeforeApply видел ревизию %d, ожидалась %d", seen[0].DraftRevision, rev)
		}
		st := rtSnapLate(t, e, rev, valA, valB, late)

		// Поздняя правка применяется своим подтверждением, без повторной инъекции.
		view = rtApplyAt(t, e, st.DraftRevision)
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("G149-01 snapshot: итог применения поздней правки %+v", view.Result)
		}
		if lvl := rtSnapLog(t, e.file)["loglevel"]; lvl != "debug" {
			t.Errorf("G149-01 snapshot: loglevel после применения поздней правки %q, ожидалось debug", lvl)
		}
		rtSnapClean(t, e, st.DraftRevision, late)
		if fired, _ := inj.result(); fired != 1 {
			t.Errorf("G149-01 snapshot: инъекция сработала %d раз, ожидался 1", fired)
		}
	})

	t.Run("empty_plan", func(t *testing.T) {
		inj := &rtSnapshotInjector{}
		e := rtNewHookEnv(t, inj.hook)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		fileBefore := hashOf(e.file)
		if fileBefore == "" {
			t.Fatal("G149-01 snapshot: исходное применение не записало файл")
		}
		// Панельное поле note в файл не попадает: план пуст, Applied фиксирует syncApplied.
		const valA = `{"access":"none","note":"n1"}`
		const valB = `{"access":"none","note":"n2"}`
		e.edit(t, "rt_log", valA)
		rev := e.state(t).DraftRevision
		inj.arm(map[string]json.RawMessage{"rt_log": json.RawMessage(valB), "rt_late": json.RawMessage(late)})

		view := rtApplyAt(t, e, rev)
		if fired, err := inj.result(); fired != 1 || err != nil {
			t.Fatalf("G149-01 snapshot: поздняя правка не внесена: срабатываний %d, ошибка %v", fired, err)
		}
		if view.Result == nil || !view.Result.OK || view.Result.Code != ResultNothingToApply {
			t.Fatalf("G149-01 snapshot: итог подтверждённого запуска %+v, ожидался %s", view.Result, ResultNothingToApply)
		}
		if got := hashOf(e.file); got != fileBefore {
			t.Errorf("G149-01 snapshot: файл изменён при пустом плане: %s -> %s", fileBefore, got)
		}
		st := rtSnapLate(t, e, rev, valA, valB, late)

		view = rtApplyAt(t, e, st.DraftRevision)
		if view.Result == nil || !view.Result.OK || view.Result.Code != ResultNothingToApply {
			t.Fatalf("G149-01 snapshot: итог применения поздней правки %+v", view.Result)
		}
		if got := hashOf(e.file); got != fileBefore {
			t.Errorf("G149-01 snapshot: файл изменён панельным полем: %s -> %s", fileBefore, got)
		}
		rtSnapEqual(t, "Applied[rt_log] после поздней правки", e.state(t).Applied["rt_log"], valB)
		rtSnapClean(t, e, st.DraftRevision, late)
	})

	t.Run("stale_revision", func(t *testing.T) {
		e := rtNewHookEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		rev := e.state(t).DraftRevision
		e.edit(t, "rt_log", `{"access":"none","level":"debug"}`)
		before := e.state(t)

		if err := e.layer.StartApplyAt(true, rev); !errors.Is(err, ErrDraftConflict) {
			t.Fatalf("G149-01 snapshot: устаревшая ревизия %d: %v, ожидался ErrDraftConflict", rev, err)
		}
		if _, err := os.Stat(e.file); !os.IsNotExist(err) {
			t.Errorf("G149-01 snapshot: файл записан при устаревшей ревизии: %v", err)
		}
		e.mu.Lock()
		calls := len(e.applies)
		e.mu.Unlock()
		if calls != 0 {
			t.Errorf("G149-01 snapshot: BeforeApply вызван %d раз при устаревшей ревизии", calls)
		}
		if cur := e.layer.pipeline.Current(); cur.Running || cur.Result != nil {
			t.Errorf("G149-01 snapshot: конвейер запущен при устаревшей ревизии: %+v", cur)
		}
		after := e.state(t)
		if after.DraftRevision != before.DraftRevision {
			t.Errorf("G149-01 snapshot: ревизия %d -> %d", before.DraftRevision, after.DraftRevision)
		}
		rtSnapEqual(t, "Draft[rt_log]", after.Draft["rt_log"], `{"access":"none","level":"debug"}`)
		if len(after.Applied) != 0 {
			t.Errorf("G149-01 snapshot: Applied изменён при устаревшей ревизии: %v", rtSectionNames(after.Applied))
		}
	})
}
