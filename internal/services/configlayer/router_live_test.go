//go:build router

package configlayer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка живых данных слоя и признака Inert на устройстве: настоящий файловый
// слой роутера, временный каталог в рабочем каталоге теста. Вердикт — поведение
// хранилища, слоя и расчёта плана; запуска ядер нет.

func rtLiveDir(t *testing.T) string {
	t.Helper()
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "cl-live-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func rtRaw(s string) json.RawMessage { return json.RawMessage(s) }

// rtProbeGen читает Live и Mode и выдаёт по файлу на каждый узел из Live["probe"].
type rtProbeGen struct{ sawMode SourceKind }

func (g *rtProbeGen) ID() string { return "rt-probe" }

func (g *rtProbeGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	g.sawMode = in.Mode
	var probe struct {
		N int `json:"n"`
	}
	raw, ok := in.Live["probe"]
	if !ok {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	outbounds := make([]map[string]any, 0, probe.N)
	for i := 0; i < probe.N; i++ {
		outbounds = append(outbounds, map[string]any{"tag": "xcp-probe-" + string(rune('a'+i)), "protocol": "freedom"})
	}
	content, err := json.Marshal(map[string]any{"outbounds": outbounds})
	if err != nil {
		return nil, err
	}
	return []GeneratedFile{{Kernel: KernelXray, RelPath: "04_outbounds.xcp-probe.tail.json", Kind: KindXrayJSON, Content: content}}, nil
}

func TestRouterLiveStore(t *testing.T) {
	dir := rtLiveDir(t)
	st := NewStore(dir, nil)

	if err := st.Update(func(s *State) error {
		s.Live["probe"] = rtRaw(`{"n":2}`)
		return nil
	}); err != nil {
		t.Fatalf("запись Live: %v", err)
	}
	if n := st.DraftChanges(); n != 0 {
		t.Errorf("Live не изменение черновика: draftChanges = %d", n)
	}
	info, err := os.Stat(filepath.Join(dir, StateFileName))
	if err != nil {
		t.Fatalf("файл состояния: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("права файла состояния %o, нужны 600", perm)
	}
	reopened := NewStore(dir, nil)
	if got := reopened.Snapshot().Live["probe"]; !SectionEqual(got, rtRaw(`{"n":2}`)) {
		t.Errorf("Live после повторного открытия: %q", got)
	}

	t.Run("EditDraftBatch", func(t *testing.T) {
		dir := rtLiveDir(t)
		s := NewStore(dir, nil)
		if _, err := s.EditDraft(0, "src_b", rtRaw(`{"x":1}`)); err != nil {
			t.Fatalf("EditDraft: %v", err)
		}
		if _, err := s.EditDraft(1, "src_c", rtRaw(`{}`)); err != nil {
			t.Fatalf("EditDraft: %v", err)
		}
		before := s.Snapshot()
		ev, err := s.EditDraftBatch(2, map[string]json.RawMessage{"src_a": rtRaw(`{ "a": 1 }`), "src_b": nil, "src_c": rtRaw(`null`)},
			func(live Sections) error {
				live["sources"] = rtRaw(`{"ok":true}`)
				return nil
			})
		if err != nil {
			t.Fatalf("EditDraftBatch: %v", err)
		}
		after := s.Snapshot()
		if ev.DraftRevision != 3 || after.DraftRevision != before.DraftRevision+1 {
			t.Errorf("ревизия %d -> %d, событие %d", before.DraftRevision, after.DraftRevision, ev.DraftRevision)
		}
		if _, ok := after.Draft["src_b"]; ok {
			t.Error("src_b не удалена")
		}
		if _, ok := after.Draft["src_c"]; ok {
			t.Error("src_c не удалена")
		}
		if string(after.Draft["src_a"]) != `{"a":1}` {
			t.Errorf("src_a = %q", after.Draft["src_a"])
		}
		if string(after.Live["sources"]) != `{"ok":true}` {
			t.Errorf("Live[sources] = %q", after.Live["sources"])
		}

		// Конфликт ревизии: ничего не записано.
		if _, err := s.EditDraftBatch(2, map[string]json.RawMessage{"src_z": rtRaw(`{}`)}, nil); !errors.Is(err, ErrDraftConflict) {
			t.Errorf("конфликт: %v", err)
		}
		// Ошибка live-функции: ничего не записано.
		boom := errors.New("boom")
		if _, err := s.EditDraftBatch(3, map[string]json.RawMessage{"src_z": rtRaw(`{}`)}, func(live Sections) error {
			live["leak"] = rtRaw(`1`)
			return boom
		}); !errors.Is(err, boom) {
			t.Errorf("ошибка live: %v", err)
		}
		// Недопустимое имя секции.
		if _, err := s.EditDraftBatch(3, map[string]json.RawMessage{"Bad Name": rtRaw(`{}`)}, nil); !errors.Is(err, ErrInvalidSection) {
			t.Errorf("имя секции: %v", err)
		}
		final := s.Snapshot()
		if final.DraftRevision != 3 {
			t.Errorf("ревизия изменилась без успеха: %d", final.DraftRevision)
		}
		if _, ok := final.Draft["src_z"]; ok {
			t.Error("src_z записана при отказе")
		}
		if _, ok := final.Live["leak"]; ok {
			t.Error("Live записан при ошибке live-функции")
		}
		// правки черновика пишутся в файл в фоне
		s.Flush()
		onDisk := NewStore(dir, nil).Snapshot()
		if onDisk.DraftRevision != 3 || len(onDisk.Draft) != 1 {
			t.Errorf("файл после отказов: ревизия %d, секций %d", onDisk.DraftRevision, len(onDisk.Draft))
		}

		// Сброс черновика Live не трогает.
		if _, err := s.ResetDraft(3); err != nil {
			t.Fatalf("ResetDraft: %v", err)
		}
		if string(s.Snapshot().Live["sources"]) != `{"ok":true}` {
			t.Error("ResetDraft изменил Live")
		}
	})

	t.Run("Schema1WithoutLive", func(t *testing.T) {
		dir := rtLiveDir(t)
		old := `{"schema_version":1,"draft_revision":4,"draft":{"diag":{"enabled":true}},"applied":{"diag":{"enabled":true}},` +
			`"manifest":{"xray:xcp-a.json":{"kernel":"xray","rel_path":"xcp-a.json","kind":"xray_json","hash":"h","status":"managed","written_at":"2026-01-01T00:00:00Z"}},"notices":{}}`
		if err := os.WriteFile(filepath.Join(dir, StateFileName), []byte(old), 0o600); err != nil {
			t.Fatal(err)
		}
		s := NewStore(dir, nil)
		got := s.Snapshot()
		if got.Notices.SchemaReset {
			t.Error("схема 1 без live сброшена")
		}
		if _, err := os.Stat(filepath.Join(dir, "backup")); err == nil {
			t.Error("появилась копия состояния")
		}
		if len(got.Live) != 0 || got.DraftRevision != 4 || len(got.Draft) != 1 || len(got.Applied) != 1 || len(got.Manifest) != 1 {
			t.Errorf("состояние прочитано не полностью: %+v", got)
		}
		if err := s.Update(func(st *State) error {
			st.Live["sources"] = rtRaw(`{}`)
			return nil
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, StateFileName))
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if string(raw["schema_version"]) != "1" {
			t.Errorf("schema_version = %s", raw["schema_version"])
		}
		if _, ok := raw["live"]; !ok {
			t.Error("в файле нет ключа live")
		}
	})
}

func TestRouterLiveLayer(t *testing.T) {
	dir := rtLiveDir(t)
	layer, err := New(Options{
		DataDir: dir,
		Roots:   Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")},
		Enabled: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer layer.Stop()

	gen := &rtProbeGen{}
	if err := layer.RegisterGenerator(gen); err != nil {
		t.Fatalf("RegisterGenerator до Start: %v", err)
	}
	if err := layer.RegisterGenerator(&rtProbeGen{}); err == nil {
		t.Error("дубликат ID принят")
	}

	// Генератор получает Live и Mode.
	if err := layer.UpdateLive(func(live Sections) error {
		live["probe"] = rtRaw(`{"n":2}`)
		return nil
	}); err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}
	view, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	files, err := layer.registry.Build(Input{Sections: view.Applied, Live: view.Live, Mode: SourceApplied}, InstalledKernels{Xray: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var probe []GeneratedFile
	for _, f := range files {
		if f.RelPath == "04_outbounds.xcp-probe.tail.json" {
			probe = append(probe, f)
		}
	}
	if len(probe) != 1 || gen.sawMode != SourceApplied {
		t.Fatalf("генератор не увидел Live: файлов %d, режим %q", len(probe), gen.sawMode)
	}
	var parsed struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(probe[0].Content, &parsed); err != nil || len(parsed.Outbounds) != 2 {
		t.Errorf("содержимое файла: %s (%v)", probe[0].Content, err)
	}

	// UpdateLive ревизию не меняет; ReadState отдаёт копии.
	if view.DraftRevision != 0 {
		t.Errorf("UpdateLive изменил ревизию: %d", view.DraftRevision)
	}
	view.Live["probe"] = rtRaw(`{"n":99}`)
	view.Draft["x"] = rtRaw(`1`)
	again, _ := layer.ReadState()
	if string(again.Live["probe"]) != `{"n":2}` || len(again.Draft) != 0 {
		t.Error("ReadState отдал не копии")
	}

	// Пакетная правка черновика не стирает Live.
	if _, err := layer.EditDraftBatch(again.DraftRevision, map[string]json.RawMessage{"src_a": rtRaw(`{}`)}, nil); err != nil {
		t.Fatalf("EditDraftBatch: %v", err)
	}
	if after, _ := layer.ReadState(); string(after.Live["probe"]) != `{"n":2}` || after.DraftRevision != 1 {
		t.Errorf("после правки: Live %q, ревизия %d", after.Live["probe"], after.DraftRevision)
	}

	layer.Start()
	if err := layer.RegisterGenerator(&rtProbeGen{}); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("регистрация после Start: %v", err)
	}
}

func TestRouterLiveDisabled(t *testing.T) {
	dir := rtLiveDir(t)
	layer, err := New(Options{DataDir: dir, Enabled: func() bool { return false }})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer layer.Stop()
	if err := layer.UpdateLive(func(Sections) error { return nil }); !errors.Is(err, ErrDisabled) {
		t.Errorf("UpdateLive: %v", err)
	}
	if _, err := layer.ReadState(); !errors.Is(err, ErrDisabled) {
		t.Errorf("ReadState: %v", err)
	}
	if _, err := layer.EditDraftBatch(0, nil, nil); !errors.Is(err, ErrDisabled) {
		t.Errorf("EditDraftBatch: %v", err)
	}
}

func TestRouterInertPlan(t *testing.T) {
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, filepath.Join(roots.Mihomo, "proxy_providers")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inert := GeneratedFile{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-x.yaml", Kind: KindMihomoProxyProvider, Content: []byte("proxies: []\n"), Inert: true}
	plan, err := ComputePlan(roots, []GeneratedFile{inert}, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plan.Files) != 1 || !plan.Files[0].Inert || plan.Files[0].Action != ActionWrite {
		t.Fatalf("Inert не перенесён в план: %+v", plan.Files)
	}
	if !plan.Changes(KernelMihomo) || plan.RuntimeChanges(KernelMihomo) {
		t.Error("Inert-запись: Changes должен быть true, RuntimeChanges false")
	}

	regular := GeneratedFile{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-y.yaml", Kind: KindMihomoProxyProvider, Content: []byte("proxies: []\n")}
	plan, err = ComputePlan(roots, []GeneratedFile{inert, regular}, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if !plan.RuntimeChanges(KernelMihomo) {
		t.Error("обычный файл того же ядра обязан давать RuntimeChanges")
	}

	// Удаление Inert-записи манифеста сохраняет признак.
	manifest := map[string]ManifestEntry{
		inert.Key(): {Kernel: inert.Kernel, RelPath: inert.RelPath, Kind: inert.Kind, Hash: "h", Status: StatusManaged, Inert: true},
	}
	plan, err = ComputePlan(roots, nil, manifest, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Action != ActionDelete || !plan.Files[0].Inert {
		t.Fatalf("удаление Inert: %+v", plan.Files)
	}
	if plan.RuntimeChanges(KernelMihomo) {
		t.Error("удаление Inert-файла не должно перезапускать ядро")
	}
}
