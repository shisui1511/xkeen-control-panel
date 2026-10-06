package configlayer

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// flagOffSets возвращает наборы копий apply-* с триггером flag_off.
func flagOffSets(t *testing.T, dataDir string) []*BackupSet {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(dataDir, "backup", "config-layer", "apply-*"))
	if err != nil {
		t.Fatal(err)
	}
	var out []*BackupSet
	for _, d := range dirs {
		set, err := LoadBackupSet(d)
		if err != nil {
			t.Fatal(err)
		}
		if set.Meta.Trigger == "flag_off" {
			out = append(out, set)
		}
	}
	return out
}

func applyLockedCount(env *layerEnv) int { return len(env.Applier.applyCalls()) }

func TestLayer_DisableMovesFilesToBackup(t *testing.T) {
	gen := newXrayGen("{\"a\":1}\n")
	env, _ := newApplyLayer(t, layerOpts{Generators: []Generator{gen}})
	otherPath := filepath.Join(env.Roots.Xray, gen.rel)
	otherKey := ManifestKey(KernelXray, gen.rel)
	if _, err := env.L.Release(otherKey); err != nil {
		t.Fatal(err)
	}
	appliedBefore := env.L.store.Snapshot().Applied
	callsBefore := applyLockedCount(env)

	if err := env.L.Disable(t.Context()); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(env.diagXrayPath()); !os.IsNotExist(err) {
		t.Errorf("managed-файл остался в рабочем каталоге: %v", err)
	}
	if got := mustReadFile(t, otherPath); got != "{\"a\":1}\n" {
		t.Errorf("отпущенный файл тронут: %q", got)
	}
	sets := flagOffSets(t, env.DataDir)
	if len(sets) != 1 {
		t.Fatalf("наборов с триггером flag_off = %d, want 1", len(sets))
	}
	var found bool
	for _, m := range sets[0].Meta.Files {
		if m.Key == diagXrayKey && m.Reason == ReasonFlagOff && m.Existed {
			found = true
			if data, err := os.ReadFile(sets[0].copyPath(m.Kernel, m.RelPath)); err != nil || string(data) != "{}\n" {
				t.Errorf("копия = %q, %v; want {}", data, err)
			}
		}
		if m.Key == otherKey {
			t.Errorf("отпущенный файл попал в набор копий: %+v", m)
		}
	}
	if !found {
		t.Errorf("в наборе нет %s с причиной flag_off: %+v", diagXrayKey, sets[0].Meta.Files)
	}

	calls := env.Applier.applyCalls()
	if len(calls)-callsBefore != 1 || calls[len(calls)-1] != "xray" {
		t.Errorf("ApplyLocked после Disable: %v (было %d), want один вызов xray", calls, callsBefore)
	}
	st := env.L.store.Snapshot()
	if len(st.Manifest) != 1 || st.Manifest[otherKey].Status != StatusReleased {
		t.Errorf("манифест = %+v, want только released", st.Manifest)
	}
	if st.Journal != nil {
		t.Error("журнал не очищен")
	}
	if len(st.Applied) != len(appliedBefore) || !SectionEqual(st.Applied[DiagSection], appliedBefore[DiagSection]) {
		t.Errorf("Applied изменился: %v → %v", appliedBefore, st.Applied)
	}
}

func TestLayer_DisableRestartFailureRollsBack(t *testing.T) {
	var failRestart atomic.Bool
	env, _ := newApplyLayer(t, layerOpts{Restart: func(e *layerEnv) (string, error) {
		if failRestart.Load() {
			e.Procs.set("xray", "stopped", 0)
			return "", nil
		}
		e.Procs.set("xray", "running", 101)
		return "", nil
	}})
	RestartConfirmTimeout = 300 * time.Millisecond
	manifestBefore := env.L.store.Snapshot().Manifest[diagXrayKey]
	callsBefore := applyLockedCount(env)
	failRestart.Store(true)

	err := env.L.Disable(t.Context())
	if err == nil {
		t.Fatal("Disable вернул nil при неудачном рестарте")
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл не возвращён: %q", got)
	}
	st := env.L.store.Snapshot()
	if st.Manifest[diagXrayKey] != manifestBefore || st.Journal != nil {
		t.Errorf("состояние изменилось: manifest %+v, journal %+v", st.Manifest[diagXrayKey], st.Journal)
	}
	if got := applyLockedCount(env) - callsBefore; got != 2 {
		t.Errorf("ApplyLocked вызван %d раз, want 2 (рестарт и повторный на прежних файлах)", got)
	}
}

func TestLayer_DisableBusy(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	rel, err := env.L.pipeline.TryBegin(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer rel()

	if err := env.L.Disable(t.Context()); err != ErrApplyBusy {
		t.Fatalf("Disable = %v, want ErrApplyBusy", err)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл тронут при занятом применении: %q", got)
	}
	if len(flagOffSets(t, env.DataDir)) != 0 {
		t.Error("набор копий создан при занятом применении")
	}
}

func TestLayer_EnableRebuildsFromApplied(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	if err := env.L.Disable(t.Context()); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	drain(events)
	env.enabled.Store(false)
	if _, err := env.L.Diag(0, "add"); err != ErrDisabled {
		t.Fatalf("Diag при выключенном слое = %v, want ErrDisabled", err)
	}
	env.enabled.Store(true)
	if _, err := env.L.Diag(env.L.store.DraftRevision(), "add_broken_xray"); err != nil {
		t.Fatal(err)
	}

	env.L.Enable()
	view := env.settleApply(t, events)

	if view.Trigger != TriggerFlagOn || view.Result == nil || !view.Result.OK {
		t.Fatalf("запуск = %+v, want flag_on, ok", view)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл = %q, want сборка из applied", got)
	}
	snap := env.L.Snapshot()
	if fv, _ := findFileView(snap.Files, diagXrayKey); fv.State != StatePending {
		// Черновик с broken_xray даёт другое содержимое: файл ok по диску, но pending по черновику.
		t.Errorf("state = %q, want pending (черновик не трогался)", fv.State)
	}
	if snap.DraftChanges != 1 {
		t.Errorf("draft_changes = %d, want 1", snap.DraftChanges)
	}
	if got := env.L.store.Snapshot().Manifest[diagXrayKey].Status; got != StatusManaged {
		t.Errorf("статус = %q, want managed", got)
	}
}
