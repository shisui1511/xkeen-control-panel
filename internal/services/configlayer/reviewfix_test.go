package configlayer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Тесты правок по ревью фазы 144 (144-REVIEW.md): каждый назван по идентификатору находки.

func (e *layerEnv) diagMihomoPath() string { return filepath.Join(e.Roots.Mihomo, DiagMihomoRel) }

// installMihomo добавляет фейковый бинарник Mihomo к уже установленному Xray.
func (e *layerEnv) installMihomo(t *testing.T) {
	t.Helper()
	e.setBins(Binaries{Xray: e.getBins().Xray, Mihomo: writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)})
}

// CR-01: фоновая сборка для нового ядра не трогает ручную правку файла другого
// ядра и не перезапускает его.
func TestCR01_OnKernelInstalledKeepsOtherKernelManualEdit(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{MihomoStatus: "stopped"})
	mustWriteFile(t, env.diagXrayPath(), manualEdit)
	restartsBefore := env.restarts.Load()

	env.installMihomo(t)
	env.L.OnKernelInstalled("mihomo")
	view := env.settleApply(t, events)

	if view.Result == nil || !view.Result.OK {
		t.Fatalf("запуск = %+v, want ok (дрейф чужого ядра не блокирует сборку)", view.Result)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != manualEdit {
		t.Errorf("ручная правка Xray перезаписана фоновой сборкой Mihomo: %q", got)
	}
	if got := mustReadFile(t, env.diagMihomoPath()); got != diagMihomoProvider {
		t.Errorf("файл Mihomo не собран: %q", got)
	}
	if env.restarts.Load() != restartsBefore {
		t.Errorf("ядро перезапущено фоновой сборкой: %d -> %d", restartsBefore, env.restarts.Load())
	}
}

// CR-01: дрейф файлов самого устанавливаемого ядра блокирует фоновую сборку.
func TestCR01_OnKernelInstalledBlockedByOwnDrift(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{MihomoStatus: "stopped"})
	env.installMihomo(t)
	env.L.OnKernelInstalled("mihomo")
	env.settleApply(t, events)
	mustWriteFile(t, env.diagMihomoPath(), "proxies:\n  - {name: manual, type: direct}\n")

	env.L.OnKernelInstalled("mihomo")
	done := waitEvent(t, events, EventApplyDone, 10*time.Second, nil)

	view, _ := done.Data.(ApplyView)
	if view.Result == nil || view.Result.OK || view.Result.Code != ResultDriftBlocked {
		t.Fatalf("Result = %+v, want drift_blocked", view.Result)
	}
	if got := mustReadFile(t, env.diagMihomoPath()); got != "proxies:\n  - {name: manual, type: direct}\n" {
		t.Errorf("ручная правка Mihomo перезаписана: %q", got)
	}
	waitIdle(t, env.L)
	if _, ok := hasNotice(env.L.Snapshot().Notices, "build_failed:mihomo"); !ok {
		t.Error("нет уведомления build_failed:mihomo при заблокированной сборке")
	}
}

// CR-01: включение слоя не перезаписывает ручную правку при дрейфе.
func TestCR01_FlagOnBlockedByDrift(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)}, DevMode: true})
	writeDiagBoth(t, env)
	if v := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft}); v.Result == nil || !v.Result.OK {
		t.Fatalf("первое применение = %+v", v.Result)
	}
	diagPath := filepath.Join(env.Roots.Xray, DiagXrayRel)
	if err := os.WriteFile(diagPath, []byte(manualEdit), 0o644); err != nil {
		t.Fatal(err)
	}

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerFlagOn, Source: SourceApplied})

	if view.Result == nil || view.Result.OK || view.Result.Code != ResultDriftBlocked {
		t.Fatalf("Result = %+v, want drift_blocked", view.Result)
	}
	if got := mustReadFile(t, diagPath); got != manualEdit {
		t.Errorf("ручная правка перезаписана: %q", got)
	}
}

// newRestartFailEnv — Xray с базовой версией файла; рестарт после неё падает
// (процесс останавливается). onRestartFail вызывается в момент неудачного рестарта.
func newRestartFailEnv(t *testing.T, recover bool, onRestartFail func(env *testEnv)) (*testEnv, *recoverApplier, *fileGen, State) {
	t.Helper()
	setRollbackTimings(t)
	xrayBin := writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "running", 100)
	failing := false
	var env *testEnv
	base := newProcApplier(procs, func() (string, error) {
		if failing {
			procs.set("xray", "stopped", 0)
			if onRestartFail != nil {
				onRestartFail(env)
			}
		} else {
			procs.set("xray", "running", 100)
		}
		return "", nil
	})
	applier := &recoverApplier{procApplier: base}
	if recover {
		applier.onForce = func(string) { procs.set("xray", "running", 101) }
	}
	gen := newXrayGen("{}\n")
	env = newTestPipeline(t, pipeOpts{
		Bins: Binaries{Xray: xrayBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states,
	})
	before := applyBaseline(t, env, gen, "{}\n")
	failing = true
	gen.set("{\"outbounds\":[]}\n")
	return env, applier, gen, before
}

// CR-02: неудавшийся откат файлов не маскируется кодом «возвращены»: свой код,
// RolledBack=false, исход строки перезапуска и журнал на месте.
func TestCR02_RollbackFailedHasOwnCode(t *testing.T) {
	env, applier, _, _ := newRestartFailEnv(t, true, func(env *testEnv) {
		// Копии прежних версий пропали: откат прочитать их не сможет.
		_ = os.RemoveAll(filepath.Join(env.DataDir, "backup", "config-layer"))
	})

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultRollbackFailed || r.RolledBack {
		t.Fatalf("Result = %+v, want rollback_failed без RolledBack", r)
	}
	if rv := restartOf(t, view, KernelXray); rv.Outcome != RestartOutcomeFailedRollbackFailed {
		t.Errorf("RestartView = %+v, want failed_rollback_failed", rv)
	}
	if env.Store.Snapshot().Journal == nil {
		t.Error("журнал очищен, хотя откат не удался: RecoverJournal не сможет его повторить")
	}
	if len(applier.forced) != 0 {
		t.Errorf("ядро перезапускалось после неудавшегося отката: %v", applier.forced)
	}
}

// CR-02: файлы возвращены, но ядро на них не поднялось — свой код, а не «ядро
// работает на прежнем конфиге».
func TestCR02_KernelNotRecoveredHasOwnCode(t *testing.T) {
	env, applier, _, before := newRestartFailEnv(t, false, nil)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultKernelNotRecovered || !r.RolledBack || r.Kernel != KernelXray {
		t.Fatalf("Result = %+v, want kernel_not_recovered с RolledBack", r)
	}
	if rv := restartOf(t, view, KernelXray); rv.Outcome != RestartOutcomeFailedKernelDown {
		t.Errorf("RestartView = %+v, want failed_kernel_down", rv)
	}
	if got := mustRead(t, filepath.Join(env.Roots.Xray, "04_outbounds.xcp-a.tail.json")); got != "{}\n" {
		t.Errorf("файл после отката = %q, want прежние байты", got)
	}
	after := env.Store.Snapshot()
	if after.Journal != nil {
		t.Error("журнал не очищен после успешного отката файлов")
	}
	for k, e := range before.Manifest {
		if after.Manifest[k].Hash != e.Hash {
			t.Errorf("манифест %s изменён", k)
		}
	}
	if len(applier.forced) != 1 {
		t.Errorf("RestartLocked вызван %v, want один раз", applier.forced)
	}
}

// CR-02: при удавшихся откате и подъёме ядра код остаётся restart_failed.
func TestCR02_RolledBackAndRecovered(t *testing.T) {
	env, _, _, _ := newRestartFailEnv(t, true, nil)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if r := view.Result; r == nil || r.Code != ResultRestartFailed || !r.RolledBack {
		t.Fatalf("Result = %+v, want restart_failed, RolledBack", r)
	}
	if rv := restartOf(t, view, KernelXray); rv.Outcome != RestartOutcomeFailedRolledBack {
		t.Errorf("RestartView = %+v, want failed_rolled_back", rv)
	}
}
