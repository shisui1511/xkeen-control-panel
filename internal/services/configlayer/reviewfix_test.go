package configlayer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

// CR-03: config.yaml указывает на профиль панели — проверяется новый профиль из
// плана, а не старые байты с диска.
func TestCR03_MihomoValidatesPlannedProfile(t *testing.T) {
	root := t.TempDir()
	const rel = "profiles/xcp-p.yaml"
	writeTestFile(t, filepath.Join(root, rel), "mixed-port: 7890\n")
	if err := os.Symlink(filepath.Join("profiles", "xcp-p.yaml"), filepath.Join(root, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	bin := writeFakeKernelScript(t, binDir, "mihomo", strings.Join([]string{
		`if grep -q BAD "$5"; then echo "bad profile"; exit 1; fi`,
		`exit 0`,
	}, "\n"))
	profilePlan := func(content string) Plan {
		return Plan{Files: []FilePlan{{
			Key: ManifestKey(KernelMihomo, rel), Kernel: KernelMihomo, RelPath: rel,
			Action: ActionWrite, Kind: KindMihomoProfile, Content: []byte(content),
		}}}
	}
	dataDir := t.TempDir()
	tmpBase := filepath.Join(dataDir, "tmp")

	res := ValidateMihomo(t.Context(), tmpBase, Roots{Mihomo: root}, bin, profilePlan("BAD: yaml\n"))
	if res.OK || res.Code != CodeValidationFailed || !strings.Contains(res.Message, "bad profile") {
		t.Fatalf("битый новый профиль прошёл проверку: %+v", res)
	}
	if got := mustRead(t, filepath.Join(root, rel)); got != "mixed-port: 7890\n" {
		t.Errorf("рабочий профиль изменён проверкой: %q", got)
	}

	res = ValidateMihomo(t.Context(), tmpBase, Roots{Mihomo: root}, bin, profilePlan("mixed-port: 7891\n"))
	if !res.OK {
		t.Fatalf("исправный новый профиль отклонён: %+v", res)
	}
	requireNoApplyDirs(t, dataDir)
}

// WR-01: выключение слоя не публикует apply_step и не переписывает итог
// последнего запуска: «фантомное» применение без apply_done заблокировало бы UI.
func TestWR01_DisableDoesNotPublishApplySteps(t *testing.T) {
	var failRestart atomic.Bool
	env, events := newApplyLayer(t, layerOpts{Restart: func(e *layerEnv) (string, error) {
		if failRestart.Load() {
			e.Procs.set("xray", "stopped", 0)
			return "", nil
		}
		e.Procs.set("xray", "running", 101)
		return "", nil
	}})
	RestartConfirmTimeout = 300 * time.Millisecond
	before := env.L.pipeline.Current()
	drain(events)
	failRestart.Store(true)

	if err := env.L.Disable(t.Context(), nil); err == nil {
		t.Fatal("Disable вернул nil при неудачном рестарте")
	}

	for {
		select {
		case ev := <-events:
			if ev.Type == EventApplyStep || ev.Type == EventApplyDone {
				t.Fatalf("выключение опубликовало %s: %+v", ev.Type, ev.Data)
			}
			continue
		default:
		}
		break
	}
	after := env.L.pipeline.Current()
	if after.Running || after.Result == nil || after.Result.Code != ResultApplied {
		t.Errorf("итог последнего запуска изменён: %+v", after)
	}
	if len(after.Restart) != len(before.Restart) {
		t.Errorf("строки перезапуска переписаны выключением: было %+v, стало %+v", before.Restart, after.Restart)
	}
}

// WR-02: снятие флага идёт под замками применения: пока commitFlag выполняется,
// запуск применения получает ErrApplyBusy, а фоновая сборка не пишет файлы.
func TestWR02_DisableCommitsFlagUnderLocks(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	var busyErr error
	called := 0

	err := env.L.Disable(t.Context(), func() error {
		called++
		_, busyErr = env.L.pipeline.TryBegin(t.Context(), true)
		env.enabled.Store(false)
		return nil
	})

	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if called != 1 {
		t.Errorf("commitFlag вызван %d раз, want 1", called)
	}
	if busyErr != ErrApplyBusy {
		t.Errorf("TryBegin внутри commitFlag = %v, want ErrApplyBusy (замки удержаны)", busyErr)
	}
	if _, statErr := os.Stat(env.diagXrayPath()); !os.IsNotExist(statErr) {
		t.Errorf("managed-файл остался: %v", statErr)
	}
	waitIdle(t, env.L)
}

// WR-02: ошибка commitFlag возвращается вызывающему; при неудачном переносе
// commitFlag не вызывается.
func TestWR02_DisableCommitFlagErrors(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	boom := errors.New("диск полон")
	if err := env.L.Disable(t.Context(), func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Disable = %v, want ошибку commitFlag", err)
	}

	var failRestart atomic.Bool
	env2, _ := newApplyLayer(t, layerOpts{Restart: func(e *layerEnv) (string, error) {
		if failRestart.Load() {
			e.Procs.set("xray", "stopped", 0)
			return "", nil
		}
		e.Procs.set("xray", "running", 101)
		return "", nil
	}})
	RestartConfirmTimeout = 300 * time.Millisecond
	failRestart.Store(true)
	called := false
	if err := env2.L.Disable(t.Context(), func() error { called = true; return nil }); err == nil {
		t.Fatal("Disable вернул nil при неудачном рестарте")
	}
	if called {
		t.Error("commitFlag вызван после неудачного переноса: флаг снялся бы при оставшихся файлах")
	}
}

// pendingJournalEnv оставляет в состоянии журнал неудавшегося отката: файл
// 99_user.json на диске содержит NEW, а набор копий хранит OLD.
func pendingJournalEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	xray := writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true})
	const rel = "99_user.json"
	abs := filepath.Join(env.Roots.Xray, rel)
	writeTestFile(t, abs, "NEW")
	set, err := NewBackupSet(env.DataDir, time.Now(), string(TriggerUser))
	if err != nil {
		t.Fatal(err)
	}
	if err := set.writeCopy(KernelXray, rel, []byte("OLD")); err != nil {
		t.Fatal(err)
	}
	set.Meta.Files = append(set.Meta.Files, BackupFileMeta{
		Key: ManifestKey(KernelXray, rel), Kernel: KernelXray, RelPath: rel, Existed: true, Reason: ReasonOverwrite,
	})
	if err := set.WriteMeta(); err != nil {
		t.Fatal(err)
	}
	err = env.Store.Update(func(st *State) error {
		st.Journal = &Journal{BackupDir: set.Dir, Trigger: string(TriggerUser), StartedAt: time.Now().UTC(),
			Files: []JournalFile{{Key: ManifestKey(KernelXray, rel), Existed: true}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return env, abs
}

// WR-03: следующее применение сначала возвращает файлы по журналу, а не
// перезаписывает его новым набором.
func TestWR03_RunRecoversPendingJournalFirst(t *testing.T) {
	env, abs := pendingJournalEnv(t)
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK {
		t.Fatalf("Result = %+v, want ok", view.Result)
	}
	if got := mustRead(t, abs); got != "OLD" {
		t.Errorf("файл по журналу = %q, want OLD (откат до нового применения)", got)
	}
	st := env.Store.Snapshot()
	if st.Journal != nil {
		t.Errorf("журнал не очищен: %+v", st.Journal)
	}
	if !st.Notices.RecoveredFromJournal {
		t.Error("нет уведомления recovered_from_journal")
	}
}

// commitFailEnv — Xray; рестарт проходит, а следующая запись файла состояния
// (фиксация применения) падает.
func commitFailEnv(t *testing.T, kernelRecovers bool) (*testEnv, *recoverApplier, *fileGen) {
	t.Helper()
	setRollbackTimings(t)
	xrayBin := writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "running", 100)
	var failNext atomic.Bool
	var env *testEnv
	pid := 100
	base := newProcApplier(procs, func() (string, error) {
		pid++
		procs.set("xray", "running", pid)
		if env != nil {
			failNext.Store(true)
		}
		return "", nil
	})
	applier := &recoverApplier{procApplier: base}
	applier.onForce = func(string) {
		if kernelRecovers {
			pid++
			procs.set("xray", "running", pid)
		} else {
			procs.set("xray", "stopped", 0)
		}
	}
	gen := newXrayGen("{}\n")
	env = newTestPipeline(t, pipeOpts{
		Bins: Binaries{Xray: xrayBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states,
	})
	env.Store.writeFile = func(path string, data []byte, perm os.FileMode) error {
		if failNext.CompareAndSwap(true, false) {
			return errors.New("диск полон")
		}
		return utils.AtomicWriteFile(path, data, perm)
	}
	gen.set("{}\n")
	env.setDraft(t, "note", `{"a":1}`)
	return env, applier, gen
}

// WR-04: сбой фиксации состояния после успешного рестарта откатывает файлы и
// перезапускает ядро на прежних, а не оставляет его на отменённом конфиге.
func TestWR04_CommitFailureRestartsKernelOnOldFiles(t *testing.T) {
	env, applier, _ := commitFailEnv(t, true)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultWriteFailed || !r.RolledBack {
		t.Fatalf("Result = %+v, want write_failed с RolledBack", r)
	}
	if len(applier.forced) != 1 || applier.forced[0] != KernelXray {
		t.Errorf("RestartLocked вызван %v, want [xray]: ядро осталось бы на новых файлах", applier.forced)
	}
	if _, err := os.Stat(filepath.Join(env.Roots.Xray, "04_outbounds.xcp-a.tail.json")); err == nil {
		t.Error("файл новой записи остался после отката")
	}
	if env.Store.Snapshot().Journal != nil {
		t.Error("журнал не очищен")
	}
}

// WR-04: ядро не поднялось на прежних файлах после сбоя фиксации — свой код.
func TestWR04_CommitFailureKernelNotRecovered(t *testing.T) {
	env, _, _ := commitFailEnv(t, false)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultKernelNotRecovered || !r.RolledBack || r.Kernel != KernelXray {
		t.Fatalf("Result = %+v, want kernel_not_recovered с RolledBack", r)
	}
}

// cancelMihomo отменяет контекст запуска в момент горячей перезагрузки и
// возвращает ошибку: так выглядит остановка панели посреди применения.
type cancelMihomo struct {
	*fakeMihomo
	cancel func()
}

func (m *cancelMihomo) ReloadConfig(path string) error {
	m.cancel()
	return errors.New("контекст отменён")
}

// WR-05: отмена контекста посреди перезапуска не откатывает применение и не
// перезапускает ядро повторно: журнал остаётся для RecoverJournal.
func TestWR05_CancelDuringRestartKeepsJournalNoRollback(t *testing.T) {
	setRollbackTimings(t)
	mihomoBin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	procs := newFakeProcs()
	procs.set("mihomo", "running", 100)
	base := newProcApplier(procs, func() (string, error) { procs.set("mihomo", "running", 101); return "", nil })
	applier := &recoverApplier{procApplier: base, onForce: func(string) { procs.set("mihomo", "running", 102) }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gen := newMihomoGen(providerV1, nil)
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Mihomo: mihomoBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states,
		Mihomo:   &cancelMihomo{fakeMihomo: &fakeMihomo{}, cancel: cancel},
		APIReady: func() bool { return true },
	})
	env.setDraft(t, "note", `{"a":1}`)

	view := env.P.Run(ctx, ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if r := view.Result; r == nil || r.OK || r.Code != ResultInterrupted {
		t.Fatalf("Result = %+v, want interrupted", r)
	}
	if calls := base.applyCalls(); len(calls) != 0 {
		t.Errorf("ApplyLocked вызван %v после отмены контекста", calls)
	}
	if len(applier.forced) != 0 {
		t.Errorf("RestartLocked вызван %v после отмены контекста", applier.forced)
	}
	if got := mustRead(t, filepath.Join(env.Roots.Mihomo, "proxy_providers/xcp-a.yaml")); got != providerV1 {
		t.Errorf("файл откачен при остановке панели: %q", got)
	}
	st := env.Store.Snapshot()
	if st.Journal == nil {
		t.Error("журнал снят: RecoverJournal не сможет вернуть файлы при старте")
	}
	if len(st.Manifest) != 0 {
		t.Errorf("манифест зафиксирован при прерванном применении: %+v", st.Manifest)
	}
}

// WR-06: файлы ядра, которое удалили, не считаются дрейфом и не блокируют
// «Применить»; пересобрать их нечем.
func TestWR06_UninstalledKernelNoDrift(t *testing.T) {
	mihomoBin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	env, events := newApplyLayer(t, layerOpts{MihomoStatus: "stopped", Bins: Binaries{Mihomo: mihomoBin}})
	mihomoKey := ManifestKey(KernelMihomo, DiagMihomoRel)
	if _, ok := env.L.store.Snapshot().Manifest[mihomoKey]; !ok {
		t.Fatal("файл Mihomo не попал в манифест")
	}

	env.setBins(Binaries{Xray: env.getBins().Xray})
	if err := os.Remove(env.diagMihomoPath()); err != nil {
		t.Fatal(err)
	}

	snap := env.L.Snapshot()
	if snap.DriftCount != 0 {
		t.Errorf("DriftCount = %d, want 0 (ядро Mihomo не установлено)", snap.DriftCount)
	}
	if _, ok := findFileView(snap.Files, mihomoKey); ok {
		t.Error("файл неустановленного ядра показан в списке")
	}
	if err := env.L.Rebuild([]string{mihomoKey}, false); err != ErrUnknownKey {
		t.Errorf("Rebuild по ключу неустановленного ядра = %v, want ErrUnknownKey", err)
	}
	if err := env.L.Rebuild(nil, true); err != ErrUnknownKey {
		t.Errorf("Rebuild(all) без дрейфа = %v, want ErrUnknownKey", err)
	}
	env.setDraftNote(t)
	if err := env.L.StartApply(true); err != nil {
		t.Fatalf("StartApply = %v, want nil: дрейф неустановленного ядра не должен блокировать", err)
	}
	env.settleApply(t, events)
}

func (e *layerEnv) setDraftNote(t *testing.T) {
	t.Helper()
	if _, err := e.L.EditDraft(e.L.store.DraftRevision(), "note", []byte(`{"a":1}`)); err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
}
