package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
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
		// остаётся для выключения после «Оставить».
		s.st.Snapshot = nil
		s.st.OverrideBefore = snap.OverrideBefore
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
	s.setStep(stepWindow, statusDone)
	s.recordHistory(opKeep, w.Snapshot.Kernel, nil)
	s.d.Layer.PublishDNS()
	return s.View(), nil
}

// Rollback — «Откатить сейчас» (D-06): окна нет — no_window; иначе окно
// забирается сразу (повтор — no_window), откат идёт в горутине службы под ctx без
// отмены — тот же, что по таймеру.
func (s *Service) Rollback(ctx context.Context) error {
	w, ok := s.claimWindow(nil)
	if !ok {
		return errCode(CodeNoWindow)
	}
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
//     прошивке, состояние waiting с причиной probe (D-26);
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
		s.setStep(stepApply, statusRunning)
		_, err := s.restoreWindowSnapshot(hctx, kernel, snap, false)
		note(err)
		s.stepResult(stepApply, err == nil)
		s.setStep(stepProbe, statusRunning)
		res, fail := s.probeTakeover(hctx)
		s.storeCheck(res)
		s.stepResult(stepProbe, fail == nil)
		if fail != nil {
			probeFail = fail
			s.setStep(stepRelease, statusRunning)
			rerr := s.releaseToFirmwareDNS(hctx, kernel, overrideBefore)
			note(rerr)
			s.stepResult(stepRelease, rerr == nil)
		}
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

// writeWithoutListener записывает файлы ядра без слушателя :53 панели
// (RunSectionLive с Live.enabled=false).
func (s *Service) writeWithoutListener(ctx context.Context, kernel string) error {
	raw, err := json.Marshal(configlayer.DNSTakeoverLive{Enabled: false, Kernel: kernel})
	if err != nil {
		return err
	}
	view, err := s.d.Layer.RunSectionLive(ctx, configlayer.SectionLiveRun{
		Trigger: configlayer.TriggerDNSTakeover,
		Kernel:  kernel,
		Live:    configlayer.Sections{configlayer.LiveKeyDNSTakeover: raw},
	})
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
	s.d.Lifecycle.Lock()
	res := s.d.Applier.ApplyLocked(kernel)
	s.d.Lifecycle.Unlock()
	if res.Outcome == services.ApplyRestartFailed {
		return fmt.Errorf("перезапуск %s после возврата proxy_dns: %s", kernel, res.Error)
	}
	return nil
}

// windowRolledBack — след отката окна: запись истории (одна на откат).
func (s *Service) windowRolledBack(w windowState, kernel, reason string, fail *Failure, probeFailed bool) {
	op := opRollback
	if reason != reasonManual {
		op = reasonTimeout
	}
	s.recordHistory(op, kernel, fail)
}
