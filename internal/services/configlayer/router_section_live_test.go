//go:build router

// router-area: configlayer

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Запуск секции поверх применённого состояния (D-13) на устройстве: настоящий
// слой, хранилище и `xray run -test` роутера, временный каталог в рабочем каталоге
// теста. Работающие ядра стенда не трогаются (Applier не задан), тест ядро не
// читает и не переключает — метка ядра не нужна.

const rtSecFile = "xcp-rtlog.json"

// rtSecGen пишет файл Xray из секции rt_log: access и (если задан в секции) level.
// Остальные ключи секции в файл не попадают (панельные поля). bad=true добавляет
// outbound с неизвестным протоколом, который Xray отвергает.
type rtSecGen struct{}

func (rtSecGen) ID() string { return "rt-sec" }

func (rtSecGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	raw, ok := in.Sections["rt_log"]
	if !ok {
		return nil, nil
	}
	var sec struct {
		Access string `json:"access"`
		Level  string `json:"level"`
		Bad    bool   `json:"bad"`
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return nil, err
	}
	logCfg := map[string]any{"access": sec.Access}
	if sec.Level != "" {
		logCfg["loglevel"] = sec.Level
	}
	doc := map[string]any{"log": logCfg}
	if sec.Bad {
		doc["outbounds"] = []map[string]any{{"tag": "xcp-rt-bad", "protocol": "xcp-no-such-protocol"}}
	}
	content, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return []GeneratedFile{{Kernel: KernelXray, RelPath: rtSecFile, Kind: KindXrayJSON, Content: content}}, nil
}

type rtSecEnv struct {
	layer *Layer
	dir   string
	file  string
}

func rtNewSecEnv(t *testing.T) *rtSecEnv {
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
	if err := layer.RegisterGenerator(rtSecGen{}); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	return &rtSecEnv{layer: layer, dir: dir, file: filepath.Join(roots.Xray, rtSecFile)}
}

// state — копия состояния слоя.
func (e *rtSecEnv) state(t *testing.T) State {
	t.Helper()
	return e.layer.store.Snapshot()
}

// edit правит секцию черновика от текущей ревизии.
func (e *rtSecEnv) edit(t *testing.T, section, value string) {
	t.Helper()
	if _, err := e.layer.EditDraft(e.state(t).DraftRevision, section, json.RawMessage(value)); err != nil {
		t.Fatalf("EditDraft %s: %v", section, err)
	}
}

// applyDraft — «Применить» черновика, ждёт итог и требует успеха.
func (e *rtSecEnv) applyDraft(t *testing.T) {
	t.Helper()
	if err := e.layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	v := rtWaitUserApply(t, e.layer)
	if !v.Result.OK {
		t.Fatalf("исходное применение: %+v", v.Result)
	}
}

// rtMergeKeys — слияние в черновик: из значения подмены берутся только названные
// ключи, остальное в черновике остаётся как есть.
func rtMergeKeys(value string, keys ...string) func(string, json.RawMessage) (json.RawMessage, error) {
	return func(_ string, draft json.RawMessage) (json.RawMessage, error) {
		cur := map[string]json.RawMessage{}
		if len(draft) > 0 {
			if err := json.Unmarshal(draft, &cur); err != nil {
				return nil, err
			}
		}
		var from map[string]json.RawMessage
		if err := json.Unmarshal([]byte(value), &from); err != nil {
			return nil, err
		}
		for _, k := range keys {
			if v, ok := from[k]; ok {
				cur[k] = v
			}
		}
		return json.Marshal(cur)
	}
}

func rtSecEqual(t *testing.T, what string, got json.RawMessage, want string) {
	t.Helper()
	if !SectionEqual(got, json.RawMessage(want)) {
		t.Errorf("%s = %s, ожидалось %s", what, got, want)
	}
}

// rtSecUnchanged сверяет, что отказавший запуск не изменил ни Applied, ни Draft,
// ни ревизию, ни файл.
func rtSecUnchanged(t *testing.T, e *rtSecEnv, before State, fileBefore string) {
	t.Helper()
	after := e.state(t)
	if after.DraftRevision != before.DraftRevision {
		t.Errorf("ревизия %d -> %d", before.DraftRevision, after.DraftRevision)
	}
	for _, name := range []string{"rt_log", "rt_other"} {
		if !SectionEqual(after.Applied[name], before.Applied[name]) {
			t.Errorf("Applied[%s]: %s -> %s", name, before.Applied[name], after.Applied[name])
		}
		if !SectionEqual(after.Draft[name], before.Draft[name]) {
			t.Errorf("Draft[%s]: %s -> %s", name, before.Draft[name], after.Draft[name])
		}
	}
	if got := hashOf(e.file); got != fileBefore {
		t.Errorf("файл изменён: %s -> %s", fileBefore, got)
	}
}

func TestRouterSectionLive(t *testing.T) {
	t.Run("overlay_keeps_foreign_draft", func(t *testing.T) {
		e := rtNewSecEnv(t)
		logPath := filepath.Join(e.dir, "x.log")
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)

		// Открытый черновик: второй ключ секции и чужая секция не применены.
		e.edit(t, "rt_log", `{"access":"none","level":"debug"}`)
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)

		value := `{"access":` + string(mustJSON(t, logPath)) + `}`
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:    TriggerAccessLog,
			Sections:   Sections{"rt_log": json.RawMessage(value)},
			MergeDraft: rtMergeKeys(value, "access"),
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		data, err := os.ReadFile(e.file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Log map[string]string `json:"log"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("файл не JSON: %v", err)
		}
		if doc.Log["access"] != logPath {
			t.Errorf("access в файле = %q, ожидалось %q", doc.Log["access"], logPath)
		}
		if _, has := doc.Log["loglevel"]; has {
			t.Errorf("правка черновика level попала в файл: %s", data)
		}
		after := e.state(t)
		rtSecEqual(t, "Applied[rt_log]", after.Applied["rt_log"], value)
		if _, has := after.Applied["rt_other"]; has {
			t.Error("чужая секция черновика применена")
		}
		rtSecEqual(t, "Draft[rt_log]", after.Draft["rt_log"], `{"access":`+string(mustJSON(t, logPath))+`,"level":"debug"}`)
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":1}`)
		if after.DraftRevision != before.DraftRevision+1 {
			t.Errorf("ревизия %d -> %d, ожидался +1", before.DraftRevision, after.DraftRevision)
		}
		if !draftDirty(after) {
			t.Error("чужие правки черновика пропали из числа изменений")
		}
	})

	t.Run("rejected_by_kernel", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)
		fileBefore := hashOf(e.file)

		value := `{"access":"none","bad":true}`
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:    TriggerAccessLog,
			Sections:   Sections{"rt_log": json.RawMessage(value)},
			MergeDraft: rtMergeKeys(value, "access", "bad"),
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.OK || view.Result.Code != CodeValidationFailed {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		rtSecUnchanged(t, e, before, fileBefore)
	})

	t.Run("drift", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)
		// Ручная правка файла панели: дрейф блокирует запуск.
		if err := os.WriteFile(e.file, []byte(`{"log":{"access":"none","loglevel":"error"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		fileBefore := hashOf(e.file)

		value := `{"access":"none","level":"info"}`
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:    TriggerAccessLog,
			Sections:   Sections{"rt_log": json.RawMessage(value)},
			MergeDraft: rtMergeKeys(value, "level"),
		})
		if !errors.Is(err, ErrDriftBlocked) {
			t.Fatalf("ожидался ErrDriftBlocked: %v, %+v", err, view.Result)
		}
		if view.Result == nil || view.Result.Code != ResultDriftBlocked {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		rtSecUnchanged(t, e, before, fileBefore)
	})

	t.Run("empty_plan", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)
		fileBefore := hashOf(e.file)

		// Панельное поле note в файл не попадает: файл тот же, план пуст.
		value := `{"access":"none","note":"n1"}`
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:    TriggerAccessLog,
			Sections:   Sections{"rt_log": json.RawMessage(value)},
			MergeDraft: rtMergeKeys(value, "note"),
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || !view.Result.OK || view.Result.Code != ResultNothingToApply {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		if got := hashOf(e.file); got != fileBefore {
			t.Errorf("файл изменён при пустом плане: %s -> %s", fileBefore, got)
		}
		after := e.state(t)
		rtSecEqual(t, "Applied[rt_log]", after.Applied["rt_log"], value)
		rtSecEqual(t, "Draft[rt_log]", after.Draft["rt_log"], value)
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":1}`)
		if after.DraftRevision != before.DraftRevision+1 {
			t.Errorf("ревизия %d -> %d, ожидался +1", before.DraftRevision, after.DraftRevision)
		}
	})

	t.Run("no_ownership_change", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		// Желаемое «взять маршрутизацию» в черновике запуском секции не применяется.
		e.edit(t, RoutingSection, `{"v":1,"managed":true}`)
		before := e.state(t)

		value := `{"access":"none","note":"n2"}`
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:    TriggerAccessLog,
			Sections:   Sections{"rt_log": json.RawMessage(value)},
			MergeDraft: rtMergeKeys(value, "note"),
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || !view.Result.OK {
			t.Fatalf("итог запуска: %+v", view.Result)
		}
		after := e.state(t)
		if !after.Routing.isEmpty() {
			t.Errorf("запуск секции изменил владение маршрутизацией: %+v", after.Routing)
		}
		rtSecEqual(t, "Draft[routing]", after.Draft[RoutingSection], `{"v":1,"managed":true}`)
		if !SectionEqual(after.Applied[RoutingSection], before.Applied[RoutingSection]) {
			t.Errorf("Applied[routing]: %s -> %s", before.Applied[RoutingSection], after.Applied[RoutingSection])
		}
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
