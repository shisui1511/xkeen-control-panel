package configlayer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	return commitFailEnvMode(t, kernelRecovers, false)
}

// commitFailEnvMode — как commitFailEnv; persistent: после рестарта отказывают все
// записи файла состояния (диск полон), а не одна.
func commitFailEnvMode(t *testing.T, kernelRecovers, persistent bool) (*testEnv, *recoverApplier, *fileGen) {
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
		if persistent && failNext.Load() {
			return errors.New("диск полон")
		}
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
	if len(view.Restart) != 1 || view.Restart[0].Outcome != RestartOutcomeInterrupted {
		t.Errorf("Restart = %+v, want один итог interrupted (не пустой исход)", view.Restart)
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

// WR-07: фоновая сборка при занятом applyMu ждёт его освобождения, а не
// теряется с ErrApplyBusy.
func TestWR07_OnKernelInstalledWaitsForRunningApply(t *testing.T) {
	old := LifecyclePollInterval
	LifecyclePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { LifecyclePollInterval = old })
	env, events := newApplyLayer(t, layerOpts{MihomoStatus: "stopped"})
	release, err := env.L.pipeline.TryBegin(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}

	env.installMihomo(t)
	env.L.OnKernelInstalled("mihomo")
	requireNoEvent(t, events, EventApplyDone, 150*time.Millisecond)
	release()
	view := env.settleApply(t, events)

	if view.Trigger != TriggerKernelInstalled || view.Result == nil || !view.Result.OK {
		t.Fatalf("запуск = %+v, want kernel_installed, ok", view)
	}
	if got := mustReadFile(t, env.diagMihomoPath()); got != diagMihomoProvider {
		t.Errorf("файл Mihomo не собран после ожидания: %q", got)
	}
}

// WR-07: ожидание applyMu прерывается отменой контекста.
func TestWR07_BackgroundTryBeginCancelWhileApplyBusy(t *testing.T) {
	old := LifecyclePollInterval
	LifecyclePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { LifecyclePollInterval = old })
	env := newTestPipeline(t, pipeOpts{Lifecycle: &sync.Mutex{}})
	release, err := env.P.TryBegin(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := env.P.TryBegin(ctx, false)
		errc <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("TryBegin = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TryBegin не вернулся после отмены")
	}
}

// WR-08: config.yaml — симлинк на панельный профиль: Редактор не должен писать
// сквозь него в файл панели.
func TestWR08_IsManagedPathThroughFileSymlink(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	link := filepath.Join(env.Roots.Xray, "99_link.json")
	if err := os.Symlink(DiagXrayRel, link); err != nil {
		t.Fatal(err)
	}

	if !env.L.IsManagedPath(link) {
		t.Error("симлинк на панельный файл не распознан как управляемый: запись прошла бы сквозь него")
	}
	if env.L.IsManagedPath(filepath.Join(env.Roots.Xray, "01_log.json")) {
		t.Error("посторонний файл считается управляемым")
	}
}

// WR-08: каталог панели — симлинк на каталог вне корней: запись отклоняется до
// первой записи, внешний каталог не тронут.
func TestWR08_SymlinkedPanelDirOutsideRootRejected(t *testing.T) {
	mihomo := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Mihomo: mihomo}, DevMode: true})
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(env.Roots.Mihomo, "proxy_providers")); err != nil {
		t.Fatal(err)
	}
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultWriteFailed || !strings.Contains(r.Message, ErrSymlinkOutsideRoot.Error()) {
		t.Fatalf("Result = %+v, want write_failed с ErrSymlinkOutsideRoot", r)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("запись ушла за пределы корней: %v", entries)
	}
}

// WR-09: выключенный слой ничего не читает и не пишет: файл состояния не создаётся
// ни при New, ни при Start, ни при Snapshot.
func TestWR09_DisabledLayerWritesNothing(t *testing.T) {
	env := newTestLayer(t, layerOpts{Enabled: false})

	env.L.Snapshot()
	env.L.RequestCheck()
	if err := env.L.RestoreExternally(func() error { return nil }); err != nil {
		t.Fatalf("RestoreExternally: %v", err)
	}

	entries, err := os.ReadDir(env.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("выключенный слой создал файлы в каталоге данных: %v", entries)
	}
}

// WR-09: нечитаемый файл состояния выключенный слой не сбрасывает и не копирует.
func TestWR09_DisabledReloadKeepsCorruptState(t *testing.T) {
	env := newTestLayer(t, layerOpts{Enabled: false})
	statePath := filepath.Join(env.DataDir, StateFileName)
	mustWriteFile(t, statePath, "{not json")

	if err := env.L.RestoreExternally(func() error { return nil }); err != nil {
		t.Fatalf("RestoreExternally: %v", err)
	}

	if got := mustReadFile(t, statePath); got != "{not json" {
		t.Errorf("файл состояния переписан выключенным слоем: %q", got)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir, "backup")); err == nil {
		t.Error("выключенный слой создал каталог копий")
	}
}

// WR-09: флаг включили без перезапуска — состояние загружается лениво и
// сохранённые данные не теряются.
func TestWR09_EnabledLaterLoadsExistingState(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir, NewBroker())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EditDraft(0, "note", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	env := newTestLayer(t, layerOpts{Enabled: false, DataDir: dataDir})

	env.enabled.Store(true)

	if got := env.L.Snapshot().DraftChanges; got != 1 {
		t.Errorf("DraftChanges = %d, want 1: состояние не подхвачено после включения флага", got)
	}
	if _, err := env.L.EditDraft(1, "note", []byte(`{"a":2}`)); err != nil {
		t.Errorf("правка черновика на загруженной ревизии: %v", err)
	}
}

// WR-10: правка черновика, пришедшая посреди восстановления снимка, не затирает
// восстановленный файл состояния и не применяется поверх него.
func TestWR10_DraftEditDuringRestoreDoesNotOverwriteRestoredState(t *testing.T) {
	// Состояние «из снимка»: ревизия 5 и секция b.
	snapDir := t.TempDir()
	snapStore, err := OpenStore(snapDir, NewBroker())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := snapStore.EditDraft(int64(i), "pad", []byte(`1`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := snapStore.EditDraft(4, "b", []byte(`{"v":9}`)); err != nil {
		t.Fatal(err)
	}
	restored := mustReadFile(t, filepath.Join(snapDir, StateFileName))

	env := newTestLayer(t, layerOpts{Enabled: true})
	if _, err := env.L.EditDraft(0, "note", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(env.DataDir, StateFileName)

	editErr := make(chan error, 1)
	err = env.L.RestoreExternally(func() error {
		// Правка на ревизии 1 стартует посреди восстановления.
		go func() {
			_, e := env.L.EditDraft(1, "note", []byte(`{"a":2}`))
			editErr <- e
		}()
		time.Sleep(100 * time.Millisecond)
		return os.WriteFile(statePath, []byte(restored), 0o600)
	})
	if err != nil {
		t.Fatalf("RestoreExternally: %v", err)
	}

	select {
	case e := <-editErr:
		if !errors.Is(e, ErrDraftConflict) {
			t.Errorf("правка посреди восстановления = %v, want ErrDraftConflict (ревизия уже восстановленная)", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("правка черновика не завершилась")
	}
	snap := env.L.store.Snapshot()
	if snap.DraftRevision != 5 || !SectionEqual(snap.Draft["b"], []byte(`{"v":9}`)) {
		t.Errorf("восстановленное состояние потеряно: rev=%d draft=%v", snap.DraftRevision, snap.Draft)
	}
	if got := mustReadFile(t, statePath); got != restored {
		t.Error("восстановленный файл состояния затёрт")
	}
}

// WR-11: ожидание API Mihomo после рестарта на MIPS длиннее, чем на остальных
// платформах: иначе медленный старт приводит к ложному откату.
func TestWR11_RestartExpectTimeoutPerPlatform(t *testing.T) {
	if got := defaultRestartExpectTimeout("amd64"); got != 15*time.Second {
		t.Errorf("amd64 = %v, want 15s", got)
	}
	if got := defaultRestartExpectTimeout("arm64"); got != 15*time.Second {
		t.Errorf("arm64 = %v, want 15s", got)
	}
	for _, arch := range []string{"mips", "mipsle"} {
		if got := defaultRestartExpectTimeout(arch); got != 60*time.Second {
			t.Errorf("%s = %v, want 60s", arch, got)
		}
	}
}

// Review-fix WR-01: истёкший контекст при выключении слоя считается прерыванием:
// файлы не возвращаются и ядро повторно не перезапускается, журнал остаётся
// для RecoverJournal (как у Run).
func TestFollowupWR01_DisableInterruptedKeepsJournal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var restarts atomic.Int32
	var armed atomic.Bool
	env, _ := newApplyLayer(t, layerOpts{Restart: func(e *layerEnv) (string, error) {
		if !armed.Load() {
			e.Procs.set("xray", "running", 101)
			return "", nil
		}
		restarts.Add(1)
		cancel()
		return "", errors.New("контекст отменён")
	}})
	armed.Store(true)

	err := env.L.Disable(ctx, nil)

	if !errors.Is(err, errInterrupted) {
		t.Fatalf("Disable = %v, want errInterrupted", err)
	}
	if got := restarts.Load(); got != 1 {
		t.Errorf("рестартов = %d, want 1 (повторного рестарта после отмены быть не должно)", got)
	}
	if env.L.store.Snapshot().Journal == nil {
		t.Error("журнал снят: RecoverJournal не сможет вернуть файлы")
	}
	if _, statErr := os.Stat(env.diagXrayPath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("файл возвращён при прерывании: %v", statErr)
	}
}

// Review-fix WR-02: Run перепроверяет флаг под замками: запуск, дождавшийся замка
// после выключения слоя, файлов не пишет.
func TestFollowupWR02_RunSkipsWhenLayerDisabled(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	if err := os.Remove(env.diagXrayPath()); err != nil {
		t.Fatal(err)
	}
	env.enabled.Store(false)

	view := env.L.pipeline.Run(t.Context(), ApplyRequest{Trigger: TriggerRebuild, Source: SourceApplied})

	if r := view.Result; r == nil || !r.OK || r.Code != ResultNothingToApply {
		t.Fatalf("Result = %+v, want nothing_to_apply", r)
	}
	if _, err := os.Stat(env.diagXrayPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("файл записан при выключенном слое: %v", err)
	}
}

// Review-fix WR-03: отказ записи состояния валит и фиксацию, и очистку журнала:
// файлы всё равно возвращены, поэтому ядро поднимается на прежних файлах, а журнал
// остаётся для RecoverJournal.
func TestFollowupWR03_JournalNotClearedStillRestartsKernel(t *testing.T) {
	env, applier, _ := commitFailEnvMode(t, true, true)

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
	if !strings.Contains(r.Message, "журнал отката не снят") {
		t.Errorf("Message = %q, want упоминание неснятого журнала", r.Message)
	}
	if env.Store.Snapshot().Journal == nil {
		t.Error("журнал снят, хотя запись состояния отказывает")
	}
}

// Review-fix WR-05: файл ядра без бинарника сервер не защищает (интерфейс его не
// показывает), а установленного ядра — защищает.
func TestFollowupWR05_IsManagedPathIgnoresUninstalledKernel(t *testing.T) {
	mihomoBin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	env, _ := newApplyLayer(t, layerOpts{MihomoStatus: "stopped", Bins: Binaries{Mihomo: mihomoBin}})
	if !env.L.IsManagedPath(env.diagMihomoPath()) {
		t.Fatal("файл установленного ядра не защищён")
	}

	env.setBins(Binaries{Xray: env.getBins().Xray})

	if env.L.IsManagedPath(env.diagMihomoPath()) {
		t.Error("файл ядра без бинарника защищён, хотя в интерфейсе его нет")
	}
	if !env.L.IsManagedPath(env.diagXrayPath()) {
		t.Error("файл установленного ядра перестал быть защищённым")
	}
}

// Review-fix WR-05: config.yaml — симлинк на панельный профиль Mihomo: снимок
// отдаёт его путь как alias файла, чтобы интерфейс защитил Редактор так же,
// как сервер (IsManagedPath).
func TestFollowupWR05_SnapshotAliasForConfigSymlink(t *testing.T) {
	mihomoBin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	env, _ := newApplyLayer(t, layerOpts{MihomoStatus: "stopped", Bins: Binaries{Mihomo: mihomoBin}})
	cfg := filepath.Join(env.Roots.Mihomo, "config.yaml")
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(DiagMihomoRel, cfg); err != nil {
		t.Fatal(err)
	}

	snap := env.L.Snapshot()

	fv, ok := findFileView(snap.Files, ManifestKey(KernelMihomo, DiagMihomoRel))
	if !ok {
		t.Fatal("файл Mihomo не найден в снимке")
	}
	if len(fv.AliasPaths) != 1 || fv.AliasPaths[0] != cfg {
		t.Errorf("AliasPaths = %v, want [%s]", fv.AliasPaths, cfg)
	}
	if !env.L.IsManagedPath(cfg) {
		t.Error("config.yaml не защищён сервером")
	}
	if xf, ok := findFileView(snap.Files, ManifestKey(KernelXray, DiagXrayRel)); ok && len(xf.AliasPaths) != 0 {
		t.Errorf("у файла Xray есть alias: %v", xf.AliasPaths)
	}
}

// Review-fix IN-01: журнал прошлой записи не восстановился (набор копий пропал):
// отдельный код без обещания повтора, файлы не пишутся.
func TestFollowupIN01_RunJournalRecoveryFailedCode(t *testing.T) {
	env, abs := pendingJournalEnv(t)
	err := env.Store.Update(func(st *State) error {
		st.Journal.BackupDir = filepath.Join(env.DataDir, "backup", "config-layer", "apply-404")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if r := view.Result; r == nil || r.OK || r.Code != ResultJournalRecoveryFailed {
		t.Fatalf("Result = %+v, want journal_recovery_failed", r)
	}
	if got := mustRead(t, abs); got != "NEW" {
		t.Errorf("файл = %q, want нетронутый NEW", got)
	}
}

// Review-fix IN-01: после запуска, восстановившего файлы по журналу, слой публикует
// уведомления: «перезапустите ядро» появляется без перечитывания снимка.
func TestFollowupIN01_AfterRunPublishesNotices(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	drain(events)

	env.L.afterRun()

	for {
		select {
		case ev := <-events:
			if ev.Type == EventNotices {
				return
			}
		default:
			t.Fatal("afterRun не опубликовал notices")
		}
	}
}

// Review-fix IN-02: пустой план и отказ записи файла состояния — отдельный код без
// RolledBack: файлы не менялись, возвращать нечего.
func TestFollowupIN02_EmptyPlanStateWriteFailure(t *testing.T) {
	xray := writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}})
	env.setDraft(t, "note", `{"a":1}`)
	env.Store.writeFile = func(string, []byte, os.FileMode) error { return errors.New("диск полон") }

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultStateWriteFailed || r.RolledBack {
		t.Fatalf("Result = %+v, want state_write_failed без RolledBack", r)
	}
}

// Review-fix IN-05: переустановленное ядро с пропавшими файлами собирается заново,
// а не блокируется как дрейф (файл просто отсутствует, ручной правки в нём нет).
func TestFollowupIN05_KernelInstalledRestoresMissingFiles(t *testing.T) {
	mihomoBin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	env, events := newApplyLayer(t, layerOpts{MihomoStatus: "stopped", Bins: Binaries{Mihomo: mihomoBin}})
	env.setBins(Binaries{Xray: env.getBins().Xray})
	if err := os.Remove(env.diagMihomoPath()); err != nil {
		t.Fatal(err)
	}

	env.installMihomo(t)
	env.L.OnKernelInstalled("mihomo")
	view := env.settleApply(t, events)

	if view.Trigger != TriggerKernelInstalled || view.Result == nil || !view.Result.OK {
		t.Fatalf("запуск = %+v, want kernel_installed, ok (а не drift_blocked)", view)
	}
	if got := mustReadFile(t, env.diagMihomoPath()); got != diagMihomoProvider {
		t.Errorf("пропавший файл Mihomo не восстановлен: %q", got)
	}
}
