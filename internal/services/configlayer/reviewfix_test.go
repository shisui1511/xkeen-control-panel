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
