package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Жизнь перехвата при действиях с ядром из панели (D-14, D-15): остановка ядра-
// перехватчика возвращает DNS роутера прошивке и помнит перехват желаемым, запуск
// возвращает его пробой без окна подтверждения, смена ядра переносит перехват на
// новое ядро с пробой и окном kernel_switch.
//
// Обработчики служб держат замок жизненного цикла ядра, пока выполняют команду, а
// конвейер слоя берёт его сам после замка применения (порядок applyMu →
// lifecycleMu). Поэтому под замком обработчика здесь делаются только команды
// прошивки, а запись файлов ядра и возврат перехвата идут в горутине службы после
// того, как обработчик замок отпустит. Флаг операции службы (beginOp) держится всё
// это время: сторож перехвата и другие действия службы видят занятость.
//
// Возврат перехвата собирает конфигурацию ядра тем же общим путём, что включение
// (runTakeover из txn.go): живые данные перехвата строятся там заново при каждом
// вызове, адрес резолвера прошивки читается свежим по решению владельца D-01.
// Здесь адрес не читается и не берётся из прежних живых данных.

const (
	// opKernelStop — операция и вид записи истории: остановка ядра-перехватчика.
	opKernelStop = "kernel_stop"
	// opRestore — операция и вид записи истории: возврат желаемого перехвата.
	opRestore = "restore"
	// waitingKernelStopped — причина ожидания: ядро-перехватчик остановлено панелью.
	waitingKernelStopped = "kernel_stopped"
	// reasonKernelStop — причина отката окна: ядро остановлено пользователем.
	reasonKernelStop = "kernel_stop"
)

const (
	// lifecyclePoll — шаг ожидания замка жизненного цикла и процессов ядра.
	lifecyclePoll = 250 * time.Millisecond
	// lifecycleAttempts — сколько раз запуск конвейера повторяется, если замок
	// успел занять кто-то ещё.
	lifecycleAttempts = 3
	// kernelStopSettle — сколько ждать исчезновения процессов ядра после остановки.
	kernelStopSettle = 10 * time.Second
	// kernelStartSettle — сколько ждать запущенного ядра после команды запуска.
	kernelStartSettle = 30 * time.Second
)

var (
	// kernelStopSteps — шаги остановки ядра-перехватчика: release — DNS прошивке,
	// apply — файлы ядра без слушателя.
	kernelStopSteps = []string{stepRelease, stepApply}
	// restoreSteps — шаги возврата перехвата: как у включения, без окна.
	restoreSteps = []string{stepValidate, stepSnapshot, stepWrite, stepRestart, stepProbe}
)

// takeoverLive — перехват включён по живым данным слоя и его ядро.
func (s *Service) takeoverLive() (string, bool) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return "", false
	}
	live, on := configlayer.ParseDNSTakeoverLive(st.Live)
	return live.Kernel, on
}

// awaitLifecycle ждёт, пока замок жизненного цикла ядра освободится (обработчик,
// вызвавший службу, его отпустит), и сразу отпускает его: конвейер слоя возьмёт
// замок сам. Остановка службы прерывает ожидание.
func (s *Service) awaitLifecycle() error {
	for !s.d.Lifecycle.TryLock() {
		if err := sleepCtx(s.ctx, lifecyclePoll); err != nil {
			return err
		}
	}
	s.d.Lifecycle.Unlock()
	return nil
}

// restartConfirmed перезапускает запущенное ядро через KernelApplier под замком
// жизненного цикла и ждёт, пока новый процесс не продержится устойчивое окно:
// xkeen -restart без терминала уходит в фон и возвращается раньше, чем ядро
// поднялось, а вызывающему после него нужен действующий слушатель или правила
// XKeen. Остановленное ядро не запускается (ApplyLocked). Ошибка — перезапуск не
// удался или ядро не поднялось устойчиво. Остановка службы прерывает ожидание
// замка и подтверждение запуска, поэтому Stop не зависает на перезапуске.
func (s *Service) restartConfirmed(ctx context.Context, kernel string) error {
	if err := s.lockLifecycle(s.ctx); err != nil {
		return err
	}
	defer s.d.Lifecycle.Unlock()
	cctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer context.AfterFunc(s.ctx, cancel)()
	oldPID := 0
	for _, st := range s.d.Kernel.ProcessStates() {
		if st.Name == kernel && st.Status == "running" {
			oldPID = st.PID
		}
	}
	res := s.d.Applier.ApplyLocked(kernel)
	switch res.Outcome {
	case services.ApplyRestartFailed:
		return errors.New(res.Error)
	case services.ApplyRestarted:
		return configlayer.ConfirmRestarted(cctx, s.d.Kernel.ProcessStates, kernel, oldPID)
	}
	return nil
}

// kernelProcessRunning — процесс ядра kernel запущен по /proc.
func (s *Service) kernelProcessRunning(kernel string) bool {
	for _, st := range s.d.Kernel.ProcessStates() {
		if st.Name == kernel && st.Status == "running" {
			return true
		}
	}
	return false
}

// waitKernel ждёт не дольше limit, пока процесс ядра kernel окажется в состоянии
// running; true — дождался.
func (s *Service) waitKernel(kernel string, running bool, limit time.Duration) bool {
	deadline := s.now().Add(limit)
	for s.kernelProcessRunning(kernel) != running {
		if !s.now().Before(deadline) {
			return false
		}
		if sleepCtx(s.ctx, lifecyclePoll) != nil {
			return false
		}
	}
	return true
}

// lockBusy — запуск конвейера не начался: замок применения или жизненного цикла занят.
func lockBusy(err error) bool {
	return errors.Is(err, configlayer.ErrKernelBusy) || errors.Is(err, configlayer.ErrApplyBusy)
}

// disabledLive — живые данные выключенного перехвата ядра kernel (файлы без слушателя).
func disabledLive(kernel string) (json.RawMessage, error) {
	return json.Marshal(configlayer.DNSTakeoverLive{Enabled: false, Kernel: kernel})
}

// --- остановка ядра (D-14) -------------------------------------------------------

// AroundKernelStop — явная остановка ядра из панели (action stop, stop с kernel).
// Перехват не держит это ядро — просто stop(). Иначе операцией службы: открытое
// окно подтверждения забирается под откат (неподтверждённое не помнится), stop()
// останавливает ядро (оно отпускает :53), затем `no opkg dns-override` (если до
// включения override не был включён) — всё это под замком обработчика; запись
// файлов без слушателя (ядро остановлено — без перезапуска) и итог — в горутине
// службы, когда обработчик отпустит замок. Итог: desired=true, state waiting с
// причиной kernel_stopped (окно включения или переноса — откат, desired=false).
// Остановка не удалась (ошибка stop или процесс жив) — перехват, окно и прошивка
// не меняются; ошибка stop возвращается как есть. Идёт другая операция службы —
// busy до остановки.
func (s *Service) AroundKernelStop(ctx context.Context, kernel string, stop func() error) error {
	if s.opActive() {
		return errCode(CodeBusy)
	}
	liveKernel, on := s.takeoverLive()
	if !on || (kernel != "" && liveKernel != kernel) {
		return stop()
	}
	if !s.beginOp(opKernelStop, kernelStopSteps) {
		return errCode(CodeBusy)
	}
	handed := false
	defer func() {
		if !handed {
			s.endOp()
		}
	}()
	w, hasWindow := s.claimWindow(nil)
	keepWindow := func() {
		if hasWindow {
			s.releaseClaim()
			s.armWindowTimer(time.UnixMilli(w.DeadlineUnixMs))
		}
	}
	if err := stop(); err != nil {
		keepWindow()
		return err
	}
	if !s.waitKernel(liveKernel, false, kernelStopSettle) {
		log.Printf("dnstakeover: ядро %s после остановки ещё работает — перехват не меняется", liveKernel)
		keepWindow()
		return nil
	}
	s.clearOutcome()

	s.mu.Lock()
	overrideBefore := s.st.OverrideBefore
	s.mu.Unlock()
	if hasWindow && w.Kind == kindEnable {
		overrideBefore = w.Snapshot.OverrideBefore
	}
	s.setStep(stepRelease, statusRunning)
	var relErr error
	if !overrideBefore {
		hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookTimeout)
		relErr = s.releaseOverride(hctx)
		cancel()
	}
	s.stepResult(stepRelease, relErr == nil)

	handed = true
	if !s.spawn(func() {
		defer s.endOp()
		s.finishKernelStop(context.WithoutCancel(ctx), liveKernel, w, hasWindow, relErr)
	}) {
		// Служба останавливается: окно остаётся в файле, старт панели откатит его.
		handed = false
		if hasWindow {
			s.releaseClaim()
		}
	}
	return nil
}

// finishKernelStop — запись файлов после остановки ядра-перехватчика (горутина
// службы, вне замка обработчика). Окно включения и переноса откатывается по виду
// (rollbackWindow: DNS прошивке, desired=false); окно «Применить» возвращает
// применённое состояние до применения уже без слушателя; без окна — файлы ядра без
// слушателя. Затем перехват желаем и ждёт запуска ядра.
func (s *Service) finishKernelStop(ctx context.Context, kernel string, w windowState, hasWindow bool, relErr error) {
	if err := s.awaitLifecycle(); err != nil {
		// Остановка службы: окно остаётся в файле, старт панели откатит его.
		if hasWindow {
			s.releaseClaim()
		}
		return
	}
	if hasWindow && (w.Kind == kindEnable || w.Kind == kindKernelSwitch) {
		s.rollbackWindow(ctx, w, reasonKernelStop)
		return
	}

	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	var errs []string
	code := CodeGeneric
	if relErr != nil {
		errs = append(errs, relErr.Error())
		code = CodeOverrideFailed
	}
	s.setStep(stepApply, statusRunning)
	var werr error
	if hasWindow {
		// Окно «Применить»: неподтверждённое применение не остаётся, перехват —
		// подтверждённый до него — остаётся желаемым.
		snap := w.Snapshot
		snap.Live, werr = disabledLive(kernel)
		if werr == nil {
			_, werr = s.restoreWindowSnapshot(hctx, kernel, snap, false)
		}
	} else {
		werr = s.writeWithoutListener(hctx, kernel)
	}
	s.stepResult(stepApply, werr == nil)
	if werr != nil {
		errs = append(errs, werr.Error())
	}

	if err := s.update(func(p *persisted) {
		p.Desired = true
		p.Kernel = kernel
		p.KernelWasRunning = false
		p.WaitingReason = waitingKernelStopped
		if hasWindow {
			p.Window = nil
		}
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	if hasWindow {
		s.releaseClaim()
	}
	var fail *Failure
	if len(errs) > 0 {
		fail = &Failure{Code: code, Detail: truncateDetail(strings.Join(errs, "\n"))}
		s.setFailure(fail)
		log.Printf("dnstakeover: возврат DNS прошивке при остановке %s: %s", kernel, strings.Join(errs, "; "))
	}
	s.recordHistory(opKernelStop, kernel, fail)
}

// --- запуск ядра и возврат перехвата (D-14, D-16) ----------------------------------

// wantsRestore — перехват желаем, а DNS у прошивки (state waiting): живые данные
// перехвата выключены, окна нет, выключение не обрывалось сбоем (disable_failed).
func (s *Service) wantsRestore() bool {
	s.mu.Lock()
	desired, window := s.st.Desired && !s.st.DisableFailed, s.st.Window != nil
	s.mu.Unlock()
	if !desired || window {
		return false
	}
	_, on := s.takeoverLive()
	return !on
}

// AfterKernelStart — ядро запущено из панели (start, restart). Перехват желаем и
// ждёт — возврат в горутине службы: после того как обработчик отпустит замок
// жизненного цикла и процесс ядра поднимется, restoreDesired (проба и включение
// без окна подтверждения). Провал — DNS остаётся у прошивки, state waiting.
func (s *Service) AfterKernelStart(ctx context.Context) {
	if !s.wantsRestore() || s.ctx.Err() != nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	s.spawn(func() {
		if err := s.awaitLifecycle(); err != nil {
			return
		}
		s.d.Kernel.InvalidateActiveState()
		if kernel := s.d.Kernel.ActiveState().Kernel; kernel != "" {
			s.waitKernel(kernel, true, kernelStartSettle)
		}
		if err := s.restoreDesired(ctx); err != nil {
			log.Printf("dnstakeover: возврат перехвата после запуска ядра: %v", err)
		}
	})
}

// RestoreDesired — возврат желаемого перехвата (сторож и сверка на старте, 148-23,
// D-16): синхронно, тем же путём, что после запуска ядра из панели. nil без
// действия — возвращать нечего.
func (s *Service) RestoreDesired(ctx context.Context) error {
	return s.restoreDesired(context.WithoutCancel(ctx))
}

// errNothingToRestore — перехват не желаем, уже включён или идёт окно.
var errNothingToRestore = errors.New("dnstakeover: возвращать нечего")

// restoreDesired — предпроверки (перехват желаем и ждёт, прошивка доступна, нет
// конфликта ядер, нет применения и операции службы, активное ядро запущено,
// DNS-секция ядра у панели, группа-цель есть), затем операцией службы — путь
// включения без окна: снимок, runTakeover (общий путь включения 148-12: живые
// данные и адрес резолвера прошивки собираются заново), проба D-29. Успех — state
// on; провал — откат как у включения, DNS у прошивки, state waiting (причина probe
// при провале пробы). Отказ предпроверки — *Error, сбой — *Error с кодом сбоя.
func (s *Service) restoreDesired(ctx context.Context) error {
	pl, err := s.restorePreflight(ctx)
	switch {
	case errors.Is(err, errNothingToRestore):
		return nil
	case err != nil:
		var de *Error
		if errors.As(err, &de) && de.Code != CodeBusy && de.Code != CodeKernelStopped && de.Code != CodeDraftOpen {
			s.setFailure(&Failure{Code: de.Code, Params: de.Params, Detail: de.Detail})
		}
		return err
	}
	if !s.beginOp(opRestore, restoreSteps) {
		return errCode(CodeBusy)
	}
	defer s.endOp()
	if fail := s.runRestore(ctx, pl); fail != nil {
		return &Error{Code: fail.Code, Params: fail.Params, Detail: fail.Detail}
	}
	return nil
}

// restorePreflight — предпроверки возврата перехвата; errNothingToRestore —
// возвращать нечего. override до включения — из состояния службы (значение до
// самого включения, к нему возвращает откат), proxy_dns — текущая строка.
func (s *Service) restorePreflight(ctx context.Context) (enablePlan, error) {
	if !s.wantsRestore() {
		return enablePlan{}, errNothingToRestore
	}
	if ok, reason := s.fw.Available(ctx); !ok {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: reason}
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return enablePlan{}, err
	}
	s.d.Kernel.InvalidateActiveState()
	active := s.d.Kernel.ActiveState()
	snap := s.d.Layer.Snapshot()
	switch {
	case active.Conflict:
		return enablePlan{}, errCode(CodeKernelConflict)
	case snap.Apply.Running || snap.BusyHolder != "" || s.checking.Load() || s.opActive() || s.rollingClaim():
		return enablePlan{}, errCode(CodeBusy)
	case snap.DraftChanges > 0:
		// Черновик открыт (D-01): Guard прохода отказал бы тем же кодом уже после
		// снимка и отката, а сторож повторял бы это раз в интервал возврата.
		return enablePlan{}, errCode(CodeDraftOpen)
	}
	kernel := active.Kernel
	if (kernel != configlayer.KernelXray && kernel != configlayer.KernelMihomo) || !s.kernelRunning(active) {
		return enablePlan{}, errCode(CodeKernelStopped)
	}
	id, name, err := takeoverTarget(st, kernel)
	if err != nil {
		return enablePlan{}, err
	}
	proxy, err := ProxyDNS()
	if err != nil {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: "proxy_dns: " + err.Error()}
	}
	s.mu.Lock()
	overrideBefore := s.st.OverrideBefore
	s.mu.Unlock()
	return enablePlan{kernel: kernel, groupID: id, groupName: name, overrideBefore: overrideBefore, proxyDNSBefore: proxy}, nil
}

// takeoverTarget — DNS-секция ядра kernel и его маршрутизация у панели, группа-цель
// перехвата есть (у Mihomo её имя годится для суффикса DoH): id и имя группы или
// отказ section_foreign, routing_not_managed, self_server, group_missing,
// group_name_invalid — единственное место правил предпроверок включения, возврата
// и переноса.
// Слушатель :53 выпускает генератор маршрутизации ядра (cores.XrayDNSTakeover,
// профиль Mihomo): маршрутизация отпущена — override включился бы без слушателя.
func takeoverTarget(st configlayer.StateView, kernel string) (id, name string, err error) {
	if !configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")].Managed {
		return "", "", &Error{Code: CodeSectionForeign, Params: map[string]string{"kernel": kernel}}
	}
	if !configlayer.DesiredRouting(st.Applied).ManagedFor(kernel) {
		return "", "", &Error{Code: CodeRoutingNotManaged, Params: map[string]string{"kernel": kernel}}
	}
	if kernel == configlayer.KernelMihomo {
		// Сервер DNS-секции на LAN-адрес роутера замкнул бы запрос на слушатель: чистый
		// генератор адресов интерфейсов не знает, поэтому сверка здесь (IN-08).
		cs := configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")]
		if list := routing.MihomoDNSRouterServers(cs.Value, routerHostCheck()); len(list) > 0 {
			return "", "", &Error{Code: CodeSelfServer, Params: map[string]string{"server": list[0]}}
		}
	}
	p := appliedParams(st)
	id, name, ok := targetGroup(st.Applied, p)
	if !ok {
		ref := name
		if ref == "" {
			ref = p.Group
		}
		return "", "", &Error{Code: CodeGroupMissing, Params: map[string]string{"group": ref}}
	}
	if kernel == configlayer.KernelMihomo && !routing.DNSGroupNameURLSafe(name) {
		return "", "", &Error{Code: CodeGroupNameInvalid, Params: map[string]string{"name": name}}
	}
	return id, name, nil
}

// runRestore — проход возврата перехвата внутри операции службы: снимок (как у
// включения — Applied, Live, override, proxy_dns), runTakeover и проба. Возвращает
// сбой (nil — перехват вернулся).
func (s *Service) runRestore(ctx context.Context, pl enablePlan) *Failure {
	finish := func(fail *Failure, rb rollbackPlan) *Failure {
		s.failRunning()
		s.setFailure(fail)
		s.rollbackEnable(ctx, pl, fail, rb)
		if err := s.update(func(p *persisted) {
			if fail.Code == CodeProbeExternal || fail.Code == CodeProbeLocal || p.WaitingReason == "" {
				p.WaitingReason = waitingProbe
			}
		}); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
		s.setFailure(fail)
		s.recordHistory(opRestore, pl.kernel, fail)
		return fail
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return finish(&Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}, rollbackPlan{})
	}
	sections, merge, err := s.enableSections(st, pl)
	if err != nil {
		return finish(&Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}, rollbackPlan{})
	}
	var (
		view     configlayer.ApplyView
		snapErr  error
		undoMu   sync.Mutex
		undoErrs []string
	)
	for attempt := 1; ; attempt++ {
		if err = s.awaitLifecycle(); err != nil {
			return finish(&Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}, rollbackPlan{})
		}
		view, err = s.runTakeover(ctx, takeoverRun{
			Kernel:         pl.kernel,
			Sections:       sections,
			MergeDraft:     merge,
			OverrideBefore: pl.overrideBefore,
			ProxyDNSBefore: pl.proxyDNSBefore,
			BeforeOverride: func(context.Context) error {
				snapErr = s.takeSnapshot(pl)
				return snapErr
			},
			OnUndoError: func(err error) {
				undoMu.Lock()
				undoErrs = append(undoErrs, err.Error())
				undoMu.Unlock()
			},
		})
		if !lockBusy(err) || attempt >= lifecycleAttempts {
			break
		}
	}
	if fail := takeoverFailure(view, err, snapErr); fail != nil {
		if len(undoErrs) > 0 {
			fail.Detail = truncateDetail(strings.TrimSpace(fail.Detail + "\n" + strings.Join(undoErrs, "\n")))
		}
		return finish(fail, rollbackPlan{restarted: pipelineRestarted(view)})
	}

	s.setStep(stepProbe, statusRunning)
	res, fail, stopped := s.probeUntilStop(ctx)
	if stopped {
		return &Failure{Code: CodeGeneric, Detail: "остановка службы посреди пробы"}
	}
	s.storeCheck(res)
	if fail != nil {
		return finish(fail, rollbackPlan{restarted: true, restore: true})
	}
	s.setStep(stepProbe, statusDone)
	if err := s.update(func(p *persisted) {
		p.Desired = true
		p.Kernel = pl.kernel
		p.KernelWasRunning = true
		p.WaitingReason = ""
		p.Snapshot = nil
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.recordHistory(opRestore, pl.kernel, nil)
	return nil
}

// --- смена ядра из панели (D-15) ---------------------------------------------------

const (
	// opTransfer — операция и вид записи истории: перенос перехвата на новое ядро.
	opTransfer = "transfer"
	// reasonKernelSwitch — причина отката окна: ядро сменено пользователем.
	reasonKernelSwitch = "kernel_switch"
)

// transferSteps — шаги переноса перехвата: как у включения, с окном переноса.
var transferSteps = []string{stepValidate, stepSnapshot, stepWrite, stepRestart, stepProbe, stepWindow}

// TransferToKernel — ядро сменено из панели (switch_kernel, переключение
// подтверждено по процессам): желаемый перехват переносится на новое ядро to в
// горутине службы, после того как обработчик отпустит замок жизненного цикла.
// Операцией службы (сторож на паузе): файлы прежнего ядра — без слушателя;
// DNS-секцию нового ядра ведёт не панель или группы-цели нет — DNS прошивке сразу,
// desired=false, сбой с кодом; иначе снимок (Applied, Live, override, proxy_dns) →
// runTakeover для нового ядра (общий путь 148-12: живые данные нового ядра
// собираются заново, адрес резолвера прошивки — свежим чтением по D-01) → проба
// D-29 → окно kernel_switch 60 с. Сбой прохода или пробы — DNS прошивке, ядро
// остаётся новым (как откат окна kernel_switch). Перехват не желаем — ничего.
func (s *Service) TransferToKernel(ctx context.Context, from, to string) {
	if (to != configlayer.KernelXray && to != configlayer.KernelMihomo) || s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	desired := s.st.Desired && !s.st.DisableFailed
	s.mu.Unlock()
	if !desired {
		return
	}
	ctx = context.WithoutCancel(ctx)
	s.spawn(func() {
		if err := s.awaitLifecycle(); err != nil {
			return
		}
		prev, on := s.takeoverLive()
		if !on {
			s.mu.Lock()
			prev = s.st.Kernel
			s.mu.Unlock()
		}
		if prev == "" {
			prev = from
		}
		if prev == to {
			// Ядро «сменено» на то же: файлы прежние; ждущий перехват возвращается.
			if err := s.restoreDesired(ctx); err != nil {
				log.Printf("dnstakeover: возврат перехвата после смены ядра на то же: %v", err)
			}
			return
		}
		for !s.beginOp(opTransfer, transferSteps) {
			if sleepCtx(s.ctx, time.Second) != nil {
				return
			}
		}
		defer s.endOp()
		s.runTransfer(ctx, prev, on, to)
	})
}

// runTransfer — перенос перехвата с ядра prev на to внутри операции службы; on —
// живые данные перехвата включены (на prev).
func (s *Service) runTransfer(ctx context.Context, prev string, on bool, to string) {
	s.mu.Lock()
	desired, overrideBefore := s.st.Desired && !s.st.DisableFailed, s.st.OverrideBefore
	s.mu.Unlock()
	if !desired {
		return
	}
	s.clearOutcome()
	if w, ok := s.claimWindow(nil); ok {
		switch w.Kind {
		case kindEnable, kindKernelSwitch:
			// Неподтверждённый перехват не переносится: откат по виду окна.
			s.replaceSteps(rollbackSteps(w.Kind))
			s.rollbackWindow(ctx, w, reasonKernelSwitch)
			return
		default:
			// Окно «Применить» заменяет окно переноса: подтверждение нового ядра
			// подтверждает и применение.
			if err := s.update(func(p *persisted) { p.Window = nil }); err != nil {
				log.Printf("dnstakeover: %v", err)
			}
			s.releaseClaim()
		}
	}

	st, err := s.d.Layer.ReadState()
	if err != nil {
		s.dropTransfer(ctx, prev, on, to, overrideBefore, &Failure{Code: CodeGeneric, Detail: truncateDetail(err.Error())})
		return
	}
	id, name, err := takeoverTarget(st, to)
	if err != nil {
		fail := &Failure{Code: CodeGeneric, Detail: truncateDetail(err.Error())}
		var de *Error
		if errors.As(err, &de) {
			fail = &Failure{Code: de.Code, Params: de.Params, Detail: de.Detail}
		}
		s.dropTransfer(ctx, prev, on, to, overrideBefore, fail)
		return
	}
	proxy, err := ProxyDNS()
	if err != nil {
		s.dropTransfer(ctx, prev, on, to, overrideBefore, &Failure{Code: CodeUnavailable, Detail: "proxy_dns: " + err.Error()})
		return
	}
	pl := enablePlan{kernel: to, groupID: id, groupName: name, overrideBefore: overrideBefore, proxyDNSBefore: proxy}
	var prevLive json.RawMessage
	if raw, ok := st.Live[configlayer.LiveKeyDNSTakeover]; ok {
		prevLive = append(json.RawMessage(nil), raw...)
	}
	snap := txnSnapshot{
		AtMs:           s.now().UnixMilli(),
		Kernel:         to,
		Applied:        st.Applied,
		Live:           prevLive,
		OverrideBefore: overrideBefore,
		ProxyDNSBefore: proxy,
	}

	// Прежнее ядро остановлено: его файлы — без слушателя, иначе следующий его
	// запуск без перехвата занял бы :53 рядом с прошивкой.
	if on {
		if err := s.writeWithoutListener(ctx, prev); err != nil {
			log.Printf("dnstakeover: файлы %s без слушателя при переносе: %v", prev, err)
		}
		if st, err = s.d.Layer.ReadState(); err != nil {
			s.dropTransfer(ctx, prev, false, to, overrideBefore, &Failure{Code: CodeGeneric, Detail: truncateDetail(err.Error())})
			return
		}
	}
	sections, merge, err := s.enableSections(st, pl)
	if err != nil {
		s.dropTransfer(ctx, prev, false, to, overrideBefore, &Failure{Code: CodeGeneric, Detail: truncateDetail(err.Error())})
		return
	}
	// Снимок переноса — в файл до прохода: остановка панели посреди пробы, до
	// окна kernel_switch, оставляет его, и старт откатывает перенос (rollbackPendingTxn).
	snap.Kind = kindKernelSwitch
	if err := s.update(func(p *persisted) {
		pending := snap
		pending.InFlight = true
		p.Snapshot = &pending
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	var (
		view     configlayer.ApplyView
		undoMu   sync.Mutex
		undoErrs []string
	)
	for attempt := 1; ; attempt++ {
		if err = s.awaitLifecycle(); err != nil {
			return
		}
		view, err = s.runTakeover(ctx, takeoverRun{
			Kernel:         to,
			Sections:       sections,
			MergeDraft:     merge,
			OverrideBefore: overrideBefore,
			ProxyDNSBefore: proxy,
			BeforeOverride: func(context.Context) error {
				s.setStep(stepSnapshot, statusDone)
				return nil
			},
			OnUndoError: func(err error) {
				undoMu.Lock()
				undoErrs = append(undoErrs, err.Error())
				undoMu.Unlock()
			},
		})
		if !lockBusy(err) || attempt >= lifecycleAttempts {
			break
		}
	}
	if fail := takeoverFailure(view, err, nil); fail != nil {
		if len(undoErrs) > 0 {
			fail.Detail = truncateDetail(strings.TrimSpace(fail.Detail + "\n" + strings.Join(undoErrs, "\n")))
		}
		s.failRunning()
		s.rollbackTransfer(ctx, to, overrideBefore, fail)
		return
	}

	s.setStep(stepProbe, statusRunning)
	res, fail, stopped := s.probeUntilStop(ctx)
	if stopped {
		return
	}
	s.storeCheck(res)
	if fail != nil {
		s.failRunning()
		s.rollbackTransfer(ctx, to, overrideBefore, fail)
		return
	}
	s.setStep(stepProbe, statusDone)
	if err := s.update(func(p *persisted) {
		p.Desired = true
		p.Kernel = to
		p.KernelWasRunning = true
		p.WaitingReason = ""
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.recordHistory(opTransfer, to, nil)
	if err := s.openWindow(kindKernelSwitch, snap); err != nil {
		log.Printf("dnstakeover: окно подтверждения переноса не открыто: %v", err)
		s.setStep(stepWindow, statusSkipped)
		s.settleSnapshot()
	}
}

// dropTransfer — перенос невозможен (DNS-секцию нового ядра ведёт не панель,
// группы-цели нет): DNS роутера прошивке (override к значению до включения, файлы
// прежнего ядра без слушателя, если живые данные перехвата ещё включены, строка
// proxy_dns к значению до включения), перехват не желаем, сбой с кодом.
func (s *Service) dropTransfer(ctx context.Context, prev string, on bool, to string, overrideBefore bool, fail *Failure) {
	s.replaceSteps([]string{stepRelease, stepApply})
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	var errs []string
	s.setStep(stepRelease, statusRunning)
	if !overrideBefore {
		if err := s.releaseOverride(hctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.stepResult(stepRelease, len(errs) == 0)
	if on {
		s.setStep(stepApply, statusRunning)
		err := s.writeWithoutListener(hctx, prev)
		s.stepResult(stepApply, err == nil)
		if err != nil {
			errs = append(errs, err.Error())
		}
	} else {
		s.setStep(stepApply, statusSkipped)
	}
	if len(errs) == 0 {
		// Перехват не желаем: строка proxy_dns возвращается к значению до включения.
		if err := s.releaseProxyLine(hctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.endTransfer(to, fail, errs)
}

// rollbackTransfer — сбой прохода или пробы переноса: DNS роутера прошивке и
// файлы нового ядра без слушателя, ядро остаётся новым (D-15), затем строка
// proxy_dns к значению до включения — те же шаги, что у отката окна kernel_switch.
func (s *Service) rollbackTransfer(ctx context.Context, to string, overrideBefore bool, fail *Failure) {
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	var errs []string
	if err := s.releaseToFirmwareDNS(hctx, to, overrideBefore); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		// Перехват не желаем: строка proxy_dns возвращается к значению до включения
		// (WR-04), иначе прежнее перенаправление XKeen пропадает без следа.
		if err := s.releaseProxyLine(hctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.endTransfer(to, fail, errs)
}

// endTransfer фиксирует неудачный перенос: перехват не желаем, окна нет, сбой
// (rolled_back — DNS вернулся прошивке; ошибки возврата — rollback_failed с
// причиной), запись истории.
func (s *Service) endTransfer(to string, fail *Failure, errs []string) {
	if len(errs) > 0 {
		if fail.Params == nil {
			fail.Params = map[string]string{}
		}
		fail.Params["cause"] = fail.Code
		fail.Code = CodeRollbackFailed
		fail.RolledBack = false
		fail.Detail = truncateDetail(strings.TrimSpace(fail.Detail + "\n" + strings.Join(errs, "\n")))
		log.Printf("dnstakeover: возврат DNS прошивке после переноса на %s не удался: %s", to, strings.Join(errs, "; "))
	} else {
		fail.RolledBack = true
	}
	if err := s.update(func(p *persisted) {
		p.Desired = false
		p.Kernel = ""
		p.KernelWasRunning = false
		p.WaitingReason = ""
		p.OverrideBefore = false
		p.Snapshot = nil
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.setFailure(fail)
	s.recordHistory(opTransfer, to, fail)
}
