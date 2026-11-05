package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Окно подтверждения перехвата (DNSF-05; D-05…D-08, D-26, D-27): после
// включения, «Применить» по D-08 или переноса на другое ядро панель ждёт
// «Оставить» 60 секунд. Таймер — одна горутина службы от дедлайна в файле
// состояния: закрытая вкладка окно не отменяет. Нет подтверждения — откат по виду
// окна; первым шагом отката DNS роутера возвращается прошивке (Pitfall 2), затем
// строка proxy_dns и только потом конфиг ядра с перезапуском.

const (
	// windowDuration — длительность окна подтверждения (D-05: не настраивается).
	windowDuration = 60 * time.Second
	// windowTotalS — длительность окна в секундах для DNSWindow.total_s.
	windowTotalS = int(windowDuration / time.Second)
)

// Виды окна (DNSWindow.kind).
const (
	kindEnable       = "enable"
	kindApply        = "apply"
	kindKernelSwitch = "kernel_switch"
)

// Причины отката окна: таймер, старт панели посреди окна (D-07), «Откатить сейчас».
const (
	reasonTimeout      = "timeout"
	reasonPanelRestart = "panel_restart"
	reasonManual       = "manual"
)

// Коды отказа действий окна (frontend/src/lib/dnsErrorCodes.json).
const (
	CodeNoWindow = "no_window"
)

// Операции окна и шаг окна в ходе включения.
const (
	opKeep     = "keep"
	opRollback = "rollback"
	stepWindow = "window"
)

// openWindow открывает окно подтверждения вида kind со снимком snap: окно
// пишется в файл состояния (дедлайн — сейчас плюс 60 с), шаг window — running,
// таймер взводится заново, событие dns. Уже открытое окно продлевается со
// снимком и видом прежними (повторное «Применить» в окне).
func (s *Service) openWindow(kind string, snap txnSnapshot) error {
	deadline := s.now().Add(windowDuration)
	s.mu.Lock()
	if s.winRolling {
		s.mu.Unlock()
		return &Error{Code: CodeBusy, Detail: "идёт откат окна подтверждения"}
	}
	if s.st.Window != nil {
		s.st.Window.DeadlineUnixMs = deadline.UnixMilli()
	} else {
		s.st.Window = &windowState{Kind: kind, DeadlineUnixMs: deadline.UnixMilli(), Snapshot: snap}
	}
	if kind == kindEnable {
		// Снимок включения переходит в окно целиком; override до включения
		// остаётся для выключения после «Оставить», proxy_dns до включения — для
		// выключения флага слоя (D-36).
		s.st.Snapshot = nil
		s.st.OverrideBefore = snap.OverrideBefore
		s.st.ProxyDNSBefore = snap.ProxyDNSBefore
	}
	if kind == kindKernelSwitch {
		// Снимок переноса тоже переходит в окно той же записью.
		s.st.Snapshot = nil
	}
	s.st.WaitingReason = ""
	saved := s.st
	s.mu.Unlock()
	if err := saveState(s.statePath(), saved); err != nil {
		// Окно есть в памяти и таймер взводится: откат по таймеру не зависит от
		// записи, теряется только откат при старте панели.
		log.Printf("dnstakeover: %v", err)
	}
	s.armWindowTimer(deadline)
	s.setStep(stepWindow, statusRunning)
	s.d.Layer.PublishDNS()
	return nil
}

// armWindowTimer взводит таймер окна на deadline: прежний таймер останавливается,
// у службы одна горутина-таймер. Остановка службы (Stop) её отменяет без отката.
func (s *Service) armWindowTimer(deadline time.Time) {
	stop := make(chan struct{})
	s.mu.Lock()
	if s.winStop != nil {
		close(s.winStop)
	}
	s.winStop = stop
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTimer(max(deadline.Sub(s.now()), 0))
		defer t.Stop()
		select {
		case <-s.ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
		}
		w, ok := s.claimWindow(stop)
		if !ok {
			return
		}
		s.rollbackWindowInOp(w, reasonTimeout)
	}()
}

// claimWindow забирает открытое окно под откат: флаг отката ставится, таймер
// останавливается. timer — канал таймера, который вызывает (nil — вызов не из
// таймера); таймер, уже заменённый новым, окно не забирает. false — окна нет или
// его уже откатывают.
func (s *Service) claimWindow(timer chan struct{}) (windowState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Window == nil || s.winRolling || (timer != nil && s.winStop != timer) {
		return windowState{}, false
	}
	s.winRolling = true
	s.stopWindowTimerLocked()
	return *s.st.Window, true
}

// stopWindowTimerLocked останавливает таймер окна. Вызывается под s.mu.
func (s *Service) stopWindowTimerLocked() {
	if s.winStop != nil {
		close(s.winStop)
		s.winStop = nil
	}
}

// windowOpen — окно подтверждения открыто и не откатывается.
func (s *Service) windowOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Window != nil && !s.winRolling
}

// Keep — «Оставить» (D-06): окно открыто — закрывается (таймер остановлен,
// снимок снят, шаг window done, state on, запись истории); окна нет, а перехват
// включён — тот же вид без изменений (повтор из второй вкладки); иначе no_window.
func (s *Service) Keep(ctx context.Context) (View, error) {
	s.mu.Lock()
	w := s.st.Window
	rolling := s.winRolling
	if w != nil && !rolling {
		s.stopWindowTimerLocked()
		s.st.Window = nil
	}
	saved := s.st
	s.mu.Unlock()
	switch {
	case w != nil && rolling:
		return View{}, &Error{Code: CodeNoWindow, Detail: "окно уже откатывается"}
	case w == nil:
		if v := s.View(); v.State == StateOn {
			return v, nil
		}
		return View{}, errCode(CodeNoWindow)
	}
	if err := saveState(s.statePath(), saved); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.clearOutcome()
	s.setStep(stepWindow, statusDone)
	s.recordHistory(opKeep, w.Snapshot.Kernel, nil)
	s.d.Layer.PublishDNS()
	return s.View(), nil
}

// Rollback — «Откатить сейчас» (D-06): окна нет — no_window; иначе окно
// забирается сразу (повтор — no_window), откат идёт в горутине службы — тот же,
// что по таймеру; закрытая вкладка его не обрывает.
func (s *Service) Rollback(ctx context.Context) error {
	w, ok := s.claimWindow(nil)
	if !ok {
		return errCode(CodeNoWindow)
	}
	s.clearOutcome()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.rollbackWindowInOp(w, reasonManual)
	}()
	return nil
}

// rollbackSteps — шаги отката окна по виду: release — DNS прошивке, apply —
// конфиг ядра к снимку, probe — проба после отката «Применить» (D-26).
func rollbackSteps(kind string) []string {
	if kind == kindApply {
		return []string{stepApply, stepProbe, stepRelease}
	}
	return []string{stepRelease, stepApply}
}

// rollbackWindowInOp — откат забранного окна операцией службы: ждёт конца идущей
// операции (откат не отказывает из-за занятости), выполняет откат по виду и
// закрывает операцию. Остановка службы прерывает ожидание и откат: окно остаётся
// в файле, и старт панели откатит его заново.
func (s *Service) rollbackWindowInOp(w windowState, reason string) {
	for !s.beginOp(opRollback, rollbackSteps(w.Kind)) {
		if err := sleepCtx(s.ctx, time.Second); err != nil {
			s.releaseClaim()
			return
		}
	}
	defer s.endOp()
	s.rollbackWindow(s.ctx, w, reason)
}

// rollbackPendingTxn — откат на старте панели прохода, прерванного остановкой до
// окна подтверждения: в файле состояния снимок (persisted.Snapshot), а окна нет.
// Обычно панель остановили посреди пробы, когда Live перехвата уже включён, а
// подтверждения не было (D-07). Включение и возврат перехвата откатываются как
// окно enable (DNS прошивке, строка proxy_dns, Applied и Live к снимку — файлы без
// слушателя), перенос — как окно kernel_switch. Перехват, желаемый до прохода
// (возврат или включение из ожидания), остаётся желаемым и ждёт (probe).
func (s *Service) rollbackPendingTxn() {
	s.mu.Lock()
	if s.st.Snapshot == nil || s.st.Window != nil || s.winRolling {
		s.mu.Unlock()
		return
	}
	snap := *s.st.Snapshot
	wasDesired := s.st.Desired && snap.Kind != kindKernelSwitch
	s.winRolling = true
	s.mu.Unlock()
	kind := kindEnable
	if snap.Kind == kindKernelSwitch {
		kind = kindKernelSwitch
	}
	log.Printf("dnstakeover: проход перехвата (%s) прерван остановкой панели до окна подтверждения — откат", kind)
	s.rollbackWindowInOp(windowState{Kind: kind, Snapshot: snap}, reasonPanelRestart)
	if s.ctx.Err() != nil || !wasDesired {
		return
	}
	if err := s.update(func(p *persisted) {
		if p.Desired {
			return
		}
		p.Desired = true
		p.Kernel = snap.Kernel
		p.OverrideBefore = snap.OverrideBefore
		p.WaitingReason = waitingProbe
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
}

// releaseClaim снимает флаг отката, не закрывая окно (откат прерван остановкой).
func (s *Service) releaseClaim() {
	s.mu.Lock()
	s.winRolling = false
	s.mu.Unlock()
}

// rollbackWindow откатывает забранное окно по виду и закрывает его. Вызывающий
// держит операцию службы (148-22 — внутри своей). Виды:
//
//   - enable — DNS роутера прошивке (`no opkg dns-override`, если override до
//     включения не был включён), строка proxy_dns к значению до включения (до
//     перезапуска: XKeen строит перехват при старте ядра), затем откат конвейером
//     слоя к снимку со сбросом секции DNS ядра и dns_takeover в черновике; если запуск не
//     перезапускал ядро, а строка менялась, — перезапуск запущенного ядра
//     KernelApplier.ApplyLocked; desired=false;
//   - apply — откат к прежнему Applied без сброса черновика (D-27: правки
//     остаются в нём), перехват остаётся; затем проба D-29, её провал — DNS
//     прошивке, состояние waiting с причиной probe (D-26). DNS уже у прошивки
//     (причина ожидания задана) — откат к снимку без слушателя и без пробы;
//   - kernel_switch — DNS прошивке, файлы нового ядра без слушателя, ядро
//     остаётся новым (D-15); desired=false.
//
// Ошибка любого шага — failure rollback_failed, но override всё равно
// возвращается. Остановка службы посреди отката оставляет окно в файле.
func (s *Service) rollbackWindow(ctx context.Context, w windowState, reason string) {
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	var errs []string
	note := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	snap := w.Snapshot
	kernel := s.windowKernel(snap)
	s.mu.Lock()
	overrideBefore := s.st.OverrideBefore
	s.mu.Unlock()
	if w.Kind == kindEnable {
		overrideBefore = snap.OverrideBefore
	}
	var probeFail *Failure

	switch w.Kind {
	case kindEnable:
		s.setStep(stepRelease, statusRunning)
		if !overrideBefore {
			note(s.fw.SetOverride(hctx, false))
		}
		proxyChanged := false
		if snap.ProxyDNSBefore {
			if cur, err := ProxyDNS(); err != nil || !cur {
				note(s.fw.SetProxyDNS(hctx, true))
				proxyChanged = true
			}
		}
		s.stepResult(stepRelease, len(errs) == 0)
		s.setStep(stepApply, statusRunning)
		view, err := s.restoreWindowSnapshot(hctx, kernel, snap, true)
		note(err)
		// Финальная сверка: override и строка равны значениям до включения.
		proxyLate, err := s.reconcileFirmware(hctx, enablePlan{kernel: kernel, overrideBefore: overrideBefore, proxyDNSBefore: snap.ProxyDNSBefore})
		note(err)
		if proxyLate || (proxyChanged && !pipelineRestarted(view)) {
			note(s.rebuildXKeenRules(kernel))
		}
		s.stepResult(stepApply, len(errs) == 0)
	case kindApply:
		s.mu.Lock()
		released := s.st.WaitingReason != ""
		s.mu.Unlock()
		if released {
			// DNS роутера уже у прошивки: повторная проба после «Применить» в окне
			// не прошла, и rollbackApply снял override, пока таймер окна ждал конца
			// его операции. Live снимка — перехват со слушателем :53, поэтому снимок
			// возвращается без слушателя и без пробы, перехват ждёт возврата.
			s.setStep(stepApply, statusRunning)
			var err error
			if snap.Live, err = disabledLive(kernel); err == nil {
				_, err = s.restoreWindowSnapshot(hctx, kernel, snap, false)
			}
			note(err)
			s.stepResult(stepApply, err == nil)
			s.setStep(stepProbe, statusSkipped)
			s.setStep(stepRelease, statusSkipped)
			break
		}
		var applyErrs []string
		probeFail, applyErrs = s.revertApply(hctx, kernel, snap, overrideBefore)
		errs = append(errs, applyErrs...)
	case kindKernelSwitch:
		s.setStep(stepRelease, statusRunning)
		if !overrideBefore {
			note(s.fw.SetOverride(hctx, false))
		}
		s.stepResult(stepRelease, len(errs) == 0)
		s.setStep(stepApply, statusRunning)
		aerr := s.writeWithoutListener(hctx, kernel)
		note(aerr)
		s.stepResult(stepApply, aerr == nil)
	default:
		note(fmt.Errorf("неизвестный вид окна %q", w.Kind))
	}

	if ctx.Err() != nil {
		// Остановка службы посреди отката: окно остаётся в файле, старт панели
		// откатит его заново (шаги идемпотентны).
		s.releaseClaim()
		return
	}
	var fail *Failure
	switch {
	case len(errs) > 0:
		fail = &Failure{Code: CodeRollbackFailed, RolledBack: false, Detail: truncateDetail(strings.Join(errs, "\n"))}
		if probeFail != nil {
			fail.Params = map[string]string{"cause": probeFail.Code}
		}
		log.Printf("dnstakeover: откат окна %s не удался: %s", w.Kind, strings.Join(errs, "; "))
	case probeFail != nil:
		fail = probeFail
		fail.RolledBack = true
	}
	if err := s.update(func(p *persisted) {
		p.Window = nil
		switch {
		case w.Kind == kindApply && probeFail != nil:
			// D-26: перехват желаем, DNS у прошивки до возврата пробой.
			p.WaitingReason = waitingProbe
		case w.Kind == kindEnable, w.Kind == kindKernelSwitch:
			p.Desired = false
			p.Kernel = ""
			p.KernelWasRunning = false
			p.OverrideBefore = false
			p.WaitingReason = ""
			// Снимок прохода, прерванного остановкой панели (rollbackPendingTxn), откачен.
			p.Snapshot = nil
		}
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.mu.Lock()
	s.winRolling = false
	s.mu.Unlock()
	if fail != nil {
		s.setFailure(fail)
	}
	s.windowRolledBack(w, kernel, reason, fail, probeFail != nil)
}

// waitingProbe — причина ожидания: проба после отката «Применить» не прошла.
const waitingProbe = "probe"

// stepResult — итог шага: done или failed.
func (s *Service) stepResult(id string, ok bool) {
	if ok {
		s.setStep(id, statusDone)
		return
	}
	s.setStep(id, statusFailed)
}

// windowKernel — ядро-перехватчик окна: из действующего Live перехвата (после
// переноса это новое ядро), иначе ядро снимка.
func (s *Service) windowKernel(snap txnSnapshot) string {
	if st, err := s.d.Layer.ReadState(); err == nil {
		if live, on := configlayer.ParseDNSTakeoverLive(st.Live); on && live.Kernel != "" {
			return live.Kernel
		}
	}
	return snap.Kernel
}

// restoreWindowSnapshot возвращает применённое состояние и Live перехвата к снимку
// через RunRestore (TriggerDNSRestore). reset — секции DNS ядра и dns_takeover в
// черновике возвращаются вместе с ним, если пользователь их не правил (откат
// включения); без reset черновик не меняется (D-27). Пустое Live снимка — ключа
// не было: он удаляется.
func (s *Service) restoreWindowSnapshot(ctx context.Context, kernel string, snap txnSnapshot, reset bool) (configlayer.ApplyView, error) {
	if snap.Applied == nil {
		return configlayer.ApplyView{}, errors.New("откат к снимку: снимка нет")
	}
	live := snap.Live
	if len(live) == 0 {
		live = json.RawMessage("null")
	}
	run := configlayer.RestoreRun{
		Trigger: configlayer.TriggerDNSRestore,
		Kernel:  kernel,
		Applied: snap.Applied,
		Live:    configlayer.Sections{configlayer.LiveKeyDNSTakeover: live},
		// Откат включения возвращает строку proxy_dns: правила XKeen с :53
		// пересобираются только полным перезапуском ядра, не горячей перезагрузкой.
		ForceFullRestart: reset && snap.ProxyDNSBefore,
	}
	if reset {
		run.ResetDraftSections = []string{configlayer.CoreSectionName(kernel, "dns"), configlayer.SectionDNSTakeover}
	}
	view, err := s.d.Layer.RunRestore(ctx, run)
	if err != nil {
		return view, fmt.Errorf("откат к снимку: %w", err)
	}
	if view.Result == nil || !view.Result.OK {
		code, msg := "", ""
		if view.Result != nil {
			code, msg = view.Result.Code, view.Result.Message
		}
		return view, fmt.Errorf("откат к снимку: %s: %s", code, msg)
	}
	return view, nil
}

// releaseToFirmwareDNS возвращает DNS роутера прошивке при желаемом перехвате:
// первым — `no opkg dns-override` (если до включения override не был включён),
// затем файлы ядра без слушателя (запущенное ядро перезапускается, остановленное
// не запускается).
func (s *Service) releaseToFirmwareDNS(ctx context.Context, kernel string, overrideBefore bool) error {
	var errs []string
	if !overrideBefore {
		if err := s.fw.SetOverride(ctx, false); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if err := s.writeWithoutListener(ctx, kernel); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// lockWaitLimit — сколько запись без слушателя ждёт замки применения и жизненного
// цикла (идущее применение, операция над ядром из обработчика служб).
const lockWaitLimit = 4 * hookTimeout

// runWithoutListener — проход конвейера с файлами ядра kernel без слушателя :53
// панели (RunSectionLive с Live.enabled=false). Это запись на путях возврата DNS
// роутера прошивке, поэтому замки применения и жизненного цикла ждутся (Wait), а
// не дают отказ занятости: обычно override к этому моменту уже снят, и потерянная
// запись оставила бы слушатель :53 в файлах ядра. Ожидание ограничено ctx,
// lockWaitLimit и остановкой службы.
func (s *Service) runWithoutListener(ctx context.Context, kernel string) (configlayer.ApplyView, error) {
	raw, err := disabledLive(kernel)
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, lockWaitLimit)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	return s.d.Layer.RunSectionLive(wctx, configlayer.SectionLiveRun{
		Trigger: configlayer.TriggerDNSTakeover,
		Kernel:  kernel,
		Live:    configlayer.Sections{configlayer.LiveKeyDNSTakeover: raw},
		Wait:    true,
	})
}

// writeWithoutListener записывает файлы ядра без слушателя :53 панели
// (runWithoutListener); сбой прохода — ошибка с кодом.
func (s *Service) writeWithoutListener(ctx context.Context, kernel string) error {
	view, err := s.runWithoutListener(ctx, kernel)
	if fail := takeoverFailure(view, err, nil); fail != nil {
		return fmt.Errorf("запись без слушателя: %s: %s", fail.Code, fail.Detail)
	}
	return nil
}

// rebuildXKeenRules перезапускает запущенное ядро через KernelApplier под
// замком жизненного цикла: XKeen строит перехват по строке proxy_dns при старте
// ядра. Остановленное ядро не запускается.
func (s *Service) rebuildXKeenRules(kernel string) error {
	if !s.kernelRunning(s.d.Kernel.ActiveState()) {
		return nil
	}
	if s.d.Applier == nil {
		return errors.New("перезапуск ядра: KernelApplier не задан")
	}
	if err := s.restartConfirmed(context.Background(), kernel); err != nil {
		return fmt.Errorf("перезапуск %s после возврата proxy_dns: %w", kernel, err)
	}
	return nil
}

// windowRolledBack — исход отката окна (D-28): timeout — таймер или старт панели
// посреди окна, rolled_manual — «Откатить сейчас», остановка или смена ядра
// пользователем (lifecycle.go), probe_failed — проба после отката «Применить» не
// прошла и DNS у прошивки.
func (s *Service) windowRolledBack(w windowState, kernel, reason string, fail *Failure, probeFailed bool) {
	kind := OutcomeRolledManual
	if reason != reasonManual && reason != reasonKernelStop && reason != reasonKernelSwitch {
		kind = OutcomeTimeout
	}
	if probeFailed {
		kind = OutcomeProbeFailed
	}
	s.recordOutcome(Outcome{Kind: kind, Kernel: kernel, Reason: reason}, fail)
}

// --- исходы без пользователя (D-28) -------------------------------------------

// Виды исхода (DNSOutcome.kind). watchdog и kernel_changed пишет сторож (148-23)
// тем же recordOutcome.
const (
	OutcomeTimeout       = "timeout"
	OutcomeRolledManual  = "rolled_manual"
	OutcomeProbeFailed   = "probe_failed"
	OutcomeWatchdog      = "watchdog"
	OutcomeKernelChanged = "kernel_changed"
)

const (
	// noticePrefix — префикс уведомлений службы в слое (Layer.SetNotices).
	noticePrefix = "dns_"
	// noticeOutcome — уведомление об исходе: dns_outcome:<вид>.
	noticeOutcome = noticePrefix + "outcome:"
)

// unattended — исход случился без пользователя: таймер, старт панели, сторож,
// смена ядра вне панели. О нём сообщают уведомление слоя и однократный тост.
func unattended(o Outcome) bool {
	switch o.Kind {
	case OutcomeTimeout, OutcomeWatchdog, OutcomeKernelChanged:
		return true
	case OutcomeProbeFailed:
		return o.Reason != reasonManual
	}
	return false
}

// recordOutcome фиксирует исход: строка в карточке (outcome в файле состояния и
// виде) до следующего действия пользователя, toast_pending — только у исхода без
// пользователя, уведомление слоя dns_outcome:<вид> (тоже только без
// пользователя), запись в истории действий XKeen и в History файла (не больше
// historyLimit), событие dns.
func (s *Service) recordOutcome(o Outcome, fail *Failure) {
	o.AtMs = s.now().UnixMilli()
	o.ToastPending = unattended(o)
	var err error
	code, detail := "", ""
	if fail != nil {
		code, detail = fail.Code, fail.Detail
		err = errors.New(fail.Code)
	}
	s.d.XKeen.RecordAction(actionPrefix+o.Kind, detail, err)
	entry := HistoryEntry{Kind: o.Kind, AtMs: o.AtMs, OK: fail == nil, Code: code, Kernel: o.Kernel}
	stored := o
	if uerr := s.update(func(p *persisted) {
		p.Outcome = &stored
		p.History = append(p.History, entry)
	}); uerr != nil {
		log.Printf("dnstakeover: %v", uerr)
	}
	s.d.Layer.SetNotices("dns_", outcomeNotices(&stored))
	s.d.Layer.PublishDNS()
}

// outcomeNotices — уведомление слоя об исходе без пользователя; иначе пусто.
// Params: time — локальное время роутера ЧЧ:ММ:СС, from и to — ядра (смена ядра
// вне панели).
func outcomeNotices(o *Outcome) []configlayer.NoticeView {
	if o == nil || !unattended(*o) {
		return nil
	}
	params := map[string]string{"time": time.UnixMilli(o.AtMs).Format("15:04:05")}
	if o.From != "" {
		params["from"] = o.From
	}
	if o.To != "" {
		params["to"] = o.To
	}
	return []configlayer.NoticeView{{ID: noticeOutcome + o.Kind, Kind: "warning", Kernel: o.Kernel, Params: params}}
}

// clearOutcome снимает исход и его уведомление: следующее действие пользователя
// (включение, выключение, «Оставить», «Откатить сейчас», выключение старого
// перенаправления) закрывает строку в карточке.
func (s *Service) clearOutcome() {
	s.mu.Lock()
	had := s.st.Outcome != nil
	s.mu.Unlock()
	if !had {
		return
	}
	if err := s.update(func(p *persisted) { p.Outcome = nil }); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.d.Layer.SetNotices(noticePrefix, nil)
	s.d.Layer.PublishDNS()
}

// OutcomeSeen — тост об исходе показан (POST outcome-seen): toast_pending=false,
// строка в карточке и уведомление остаются. Ответ — вид.
func (s *Service) OutcomeSeen(ctx context.Context) (View, error) {
	s.mu.Lock()
	pending := s.st.Outcome != nil && s.st.Outcome.ToastPending
	s.mu.Unlock()
	if pending {
		if err := s.update(func(p *persisted) {
			if p.Outcome != nil {
				o := *p.Outcome
				o.ToastPending = false
				p.Outcome = &o
			}
		}); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
		s.d.Layer.PublishDNS()
	}
	return s.View(), nil
}
