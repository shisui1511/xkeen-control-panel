//go:build router

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

func rtNewHookEnv(t *testing.T) *rtHookEnv {
	t.Helper()
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
