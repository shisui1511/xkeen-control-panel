package dnstakeover

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сторож DNS роутера (D-13): отдельный цикл службы раз в 5 с меряет живость
// слушателя :53, а не резолв внешнего имени. Проба — имя во встроенной зоне lan
// через 127.0.0.1:53: ответ даёт прошивка через ядро и не зависит от группы-цели,
// а любой окончательный ответ (NOERROR, NXDOMAIN) значит «жив»; SERVFAIL на имя
// встроенной зоны — промах (путь к прошивке сломан, Alive). Мёртвые узлы группы поэтому DNS
// прошивке не возвращают: маршрутизация DNS остаётся fail-closed (D-10), защищается
// только сам DNS роутера (fail-open).
//
// Три промаха подряд при том же ядре — перезапуск ядра KernelApplier.RestartLocked
// под замком жизненного цикла, не больше двух попыток; не помогло — DNS прошивке,
// перехват остаётся желаемым и ждёт (waiting, причина watchdog). Активное ядро
// сменили мимо панели — DNS прошивке сразу, перехват не желаем (kernel_changed).
// Оба исхода сообщаются так же, как автооткат окна (D-28): строка в карточке,
// уведомление dns_, история, однократный тост.
//
// Сторож на паузе (Pitfall 7, T-148-50): окно штатного перезапуска XKeen,
// намеренная остановка ядра, операция над ядрами и guardBusyGrace после неё,
// операция службы, проверка резолва, откат окна и применение слоя. Пауза сбрасывает
// счётчик промахов: промахи до неё к ней не относятся.

const (
	// guardInterval — шаг цикла сторожа.
	guardInterval = 5 * time.Second
	// guardMisses — сколько промахов подряд считаются падением слушателя.
	guardMisses = 3
	// guardRestartBudget — сколько раз сторож перезапускает ядро, прежде чем
	// вернуть DNS прошивке (T-148-52).
	guardRestartBudget = 2
	// guardBusyGrace — пауза после операции над ядрами (как watchdogKernelBusyGrace
	// у WatchdogService) и после старта панели: ядро поднимается не сразу.
	guardBusyGrace = 60 * time.Second
	// guardRearmMinInterval — не чаще этого сторож возвращает перехват после
	// аварийного возврата и в состоянии waiting: то же значение, что
	// watchdogRearmMinInterval в services/watchdog.go (цикл перезапусков ядра
	// роняет трафик).
	guardRearmMinInterval = 90 * time.Second
	// guardRestartSettle — через сколько после перезапуска ядра проба повторяется.
	guardRestartSettle = 15 * time.Second
	// guardProbeName — имя пробы сторожа во встроенной зоне lan.
	guardProbeName = "xcp-guard.lan"
)

const (
	// opGuard — операция службы: действие сторожа (перезапуск ядра, возврат DNS).
	opGuard = "guard"
	// opGuardRestart — запись истории: перезапуск ядра сторожем.
	opGuardRestart = "guard_restart"
	// waitingWatchdog — причина ожидания: сторож вернул DNS прошивке.
	waitingWatchdog = "watchdog"
	// reasonWatchdog — причина возврата DNS прошивке: слушатель не поднялся.
	reasonWatchdog = "watchdog"
	// reasonKernelChanged — причина возврата DNS прошивке: ядро сменено мимо панели.
	reasonKernelChanged = "kernel_changed"
)

var (
	// guardRestartSteps — шаги перезапуска ядра сторожем.
	guardRestartSteps = []string{stepRestart, stepProbe}
	// guardReleaseSteps — шаги возврата DNS прошивке сторожем.
	guardReleaseSteps = []string{stepRelease, stepApply}
	// guardStaleSteps — шаги повторной записи файлов без слушателя сторожем. Шаг
	// release нужен карточке: по нему она показывает возврат DNS прошивке, а не
	// включение. Override к этому моменту уже снят, шаг сразу done.
	guardStaleSteps = []string{stepRelease, stepApply}
)

// guardState — память цикла сторожа; живёт только в его горутине.
type guardState struct {
	// misses — промахи пробы подряд.
	misses int
	// lastBusy — когда сторож последний раз видел операцию над ядрами (и старт).
	lastBusy time.Time
	// lastRestore — последняя попытка возврата перехвата сторожем.
	lastRestore time.Time
	// sawStopped — после последней попытки возврата ядро было остановлено.
	sawStopped bool
	// lastStale — последняя сверка файлов со слушателем :53 при DNS у прошивки.
	lastStale time.Time
}

// isKernelName — xray или mihomo.
func isKernelName(k string) bool {
	return k == configlayer.KernelXray || k == configlayer.KernelMihomo
}

// guardLoop — цикл сторожа под s.wg (Add делает вызывающий); останавливается
// отменой ctx (Stop). Первые guardBusyGrace после старта — пауза: ядро после
// загрузки роутера и сверки на старте поднимается не сразу.
func (s *Service) guardLoop(ctx context.Context) {
	defer s.wg.Done()
	g := &guardState{lastBusy: s.now()}
	tick := time.NewTicker(guardInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.guardTick(ctx, g)
	}
}

// guardPaused — сторож на паузе (Pitfall 7). Операция над ядрами отмечается
// временем: пауза длится ещё guardBusyGrace после неё.
func (s *Service) guardPaused(g *guardState) bool {
	if !s.d.Layer.Enabled() {
		return true
	}
	if s.d.XKeen.InRestart() {
		// Окно штатного перезапуска (Restart, SwitchKernel и перезапуск сторожем).
		return true
	}
	if s.d.XKeen.IntentionalStop() {
		// Ядро остановлено из панели: его отсутствие на :53 — не падение.
		return true
	}
	now := s.now()
	if s.d.Kernel.Busy() {
		g.lastBusy = now
		return true
	}
	if !g.lastBusy.IsZero() && now.Sub(g.lastBusy) < guardBusyGrace {
		return true
	}
	if s.opActive() || s.checking.Load() {
		return true
	}
	s.mu.Lock()
	rolling := s.winRolling
	s.mu.Unlock()
	if rolling {
		return true
	}
	snap := s.d.Layer.Snapshot()
	return snap.Apply.Running || snap.BusyHolder != ""
}

// guardTick — один шаг сторожа: перехват не действует — ветка возврата; ядро
// сменили мимо панели — DNS прошивке; иначе проба слушателя и счёт промахов.
func (s *Service) guardTick(ctx context.Context, g *guardState) {
	if s.guardPaused(g) {
		g.misses = 0
		return
	}
	kernel, on := s.takeoverLive()
	if !on {
		g.misses = 0
		s.guardRestore(ctx, g)
		return
	}
	active := s.d.Kernel.ActiveState()
	if active.Conflict {
		// Запущены оба ядра: панель ничего не трогает, пока конфликт не снят.
		g.misses = 0
		return
	}
	if isKernelName(active.Kernel) && active.Kernel != kernel {
		g.misses = 0
		s.guardKernelChanged(ctx, kernel, active.Kernel)
		return
	}
	if s.guardStaleListener(ctx, g, kernel) {
		g.misses = 0
		return
	}
	if Alive(ctx, LoopbackDNS, guardProbeName) {
		g.misses = 0
		return
	}
	g.misses++
	if g.misses < guardMisses {
		return
	}
	g.misses = 0
	log.Printf("dnstakeover: сторож: слушатель %s не отвечает %d раз подряд — перезапуск %s", LoopbackDNS, guardMisses, kernel)
	if s.guardRecover(ctx, kernel) {
		g.lastRestore = s.now()
		g.sawStopped = false
	}
}

// guardRestore — ветка возврата перехвата (Claude's Discretion, D-16): перехват
// желаем и ждёт (waiting), окна нет, активное ядро запущено, конфликта нет —
// RestoreDesired (проба и включение без окна, 148-22). Не чаще
// guardRearmMinInterval с прошлой попытки; при причине kernel_stopped — сразу, как
// только запущенное ядро обнаружено после остановки. Провал — перехват остаётся в
// waiting, следующая попытка не раньше guardRearmMinInterval.
func (s *Service) guardRestore(ctx context.Context, g *guardState) {
	s.mu.Lock()
	desired, window, reason := s.st.Desired, s.st.Window != nil, s.st.WaitingReason
	disableFailed := s.st.DisableFailed
	s.mu.Unlock()
	// Неудавшееся выключение не возвращается: пользователь выключал перехват.
	if !desired || window || reason == "" || disableFailed {
		return
	}
	active := s.d.Kernel.ActiveState()
	if active.Conflict || !isKernelName(active.Kernel) {
		return
	}
	if !s.kernelRunning(active) {
		g.sawStopped = true
		return
	}
	now := s.now()
	immediate := reason == waitingKernelStopped && g.sawStopped
	if !immediate && !g.lastRestore.IsZero() && now.Sub(g.lastRestore) < guardRearmMinInterval {
		return
	}
	g.lastRestore = now
	g.sawStopped = false
	log.Printf("dnstakeover: сторож: возврат перехвата (причина ожидания %s)", reason)
	if err := s.RestoreDesired(ctx); err != nil {
		var de *Error
		if errors.As(err, &de) && (de.Code == CodeBusy || de.Code == CodeKernelStopped || de.Code == CodeDraftOpen) {
			// Возврат не начинался: следующий шаг сторожа попробует снова.
			g.lastRestore = time.Time{}
			return
		}
		log.Printf("dnstakeover: сторож: перехват не вернулся: %v", err)
	}
}

// guardStaleListener — живые данные перехвата включены (слушатель :53 в файлах
// ядра), а DNS роутера по состоянию службы у прошивки: перехват не желаем, ждёт
// возврата или его выключение не удалось, окна нет. Так бывает, когда на пути
// возврата DNS прошивке override снят, а запись файлов без слушателя не прошла
// (сбой конвейера, отмена ожидания замков). Повтора у этих путей нет, а проба
// слушателя здесь не помогает: через 127.0.0.1:53 отвечает прошивка или прежний
// процесс ядра, а следующий запуск ядра на этих файлах наткнётся на занятый :53.
// Поэтому не чаще guardRearmMinInterval override читается заново и, если он снят,
// файлы ядра kernel пишутся без слушателя операцией службы (запущенное ядро
// перезапускается). Override включён — :53 нужен ядру, ничего не меняется. true —
// запись выполнялась, проба слушателя на этом шаге не нужна.
func (s *Service) guardStaleListener(ctx context.Context, g *guardState, kernel string) bool {
	s.mu.Lock()
	stale := s.st.Window == nil && (!s.st.Desired || s.st.WaitingReason != "" || s.st.DisableFailed)
	s.mu.Unlock()
	if !stale {
		return false
	}
	now := s.now()
	if !g.lastStale.IsZero() && now.Sub(g.lastStale) < guardRearmMinInterval {
		return false
	}
	g.lastStale = now
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	if on, err := s.fw.OverrideEnabled(hctx); err != nil || on {
		return false
	}
	if !s.beginOp(opGuard, guardStaleSteps) {
		return false
	}
	defer s.endOp()
	log.Printf("dnstakeover: сторож: override снят, а в файлах %s слушатель :53 — запись без слушателя", kernel)
	s.setStep(stepRelease, statusDone)
	s.setStep(stepApply, statusRunning)
	err := s.writeWithoutListener(hctx, kernel)
	s.stepResult(stepApply, err == nil)
	if err != nil {
		log.Printf("dnstakeover: сторож: файлы %s без слушателя не записаны: %v", kernel, err)
	}
	return true
}

// guardKernelChanged — активное ядро не то, что ведёт перехват (сменили мимо
// панели): DNS прошивке операцией службы, перехват не желаем, исход kernel_changed.
func (s *Service) guardKernelChanged(ctx context.Context, from, to string) {
	if !s.beginOp(opGuard, guardReleaseSteps) {
		return
	}
	defer s.endOp()
	log.Printf("dnstakeover: сторож: ядро сменено мимо панели (%s → %s) — DNS прошивке", from, to)
	s.releaseToFirmware(ctx, from, reasonKernelChanged, to)
}

// lockLifecycle берёт замок жизненного цикла ядра; остановка службы прерывает
// ожидание (ошибка — замок не взят).
func (s *Service) lockLifecycle(ctx context.Context) error {
	for !s.d.Lifecycle.TryLock() {
		if err := sleepCtx(ctx, lifecyclePoll); err != nil {
			return err
		}
	}
	return nil
}

// guardRecover — слушатель упал: операцией службы до guardRestartBudget попыток
// перезапуска ядра (RestartLocked под замком жизненного цикла) с повторной пробой
// через guardRestartSettle. Не помогло — DNS прошивке (reasonWatchdog). Перед
// каждой попыткой ядро сверяется заново: конфликт — ничего, сменили мимо панели —
// возврат с kernel_changed. true — DNS возвращён прошивке с ожиданием (waiting).
func (s *Service) guardRecover(ctx context.Context, kernel string) (released bool) {
	if !s.beginOp(opGuard, guardRestartSteps) {
		return false
	}
	defer s.endOp()
	if s.d.Applier == nil {
		log.Printf("dnstakeover: сторож: KernelApplier не задан — перезапуск невозможен")
	}
	for attempt := 1; s.d.Applier != nil && attempt <= guardRestartBudget; attempt++ {
		if attempt > 1 {
			s.replaceSteps(guardRestartSteps)
		}
		s.d.Kernel.InvalidateActiveState()
		active := s.d.Kernel.ActiveState()
		switch {
		case active.Conflict:
			return false
		case isKernelName(active.Kernel) && active.Kernel != kernel:
			s.replaceSteps(guardReleaseSteps)
			s.releaseToFirmware(ctx, kernel, reasonKernelChanged, active.Kernel)
			return false
		}
		s.setStep(stepRestart, statusRunning)
		if err := s.lockLifecycle(ctx); err != nil {
			return false
		}
		res := s.d.Applier.RestartLocked(kernel)
		s.d.Lifecycle.Unlock()
		if res.Outcome == services.ApplySavedKernelConflict {
			s.setStep(stepRestart, statusSkipped)
			return false
		}
		var fail *Failure
		if res.Outcome == services.ApplyRestartFailed {
			fail = &Failure{Code: CodeRestartFailed, Params: kernelParam(kernel), Detail: truncateDetail(res.Error)}
		}
		s.stepResult(stepRestart, fail == nil)
		s.setStep(stepProbe, statusRunning)
		if sleepCtx(ctx, guardRestartSettle) != nil {
			return false
		}
		alive := Alive(ctx, LoopbackDNS, guardProbeName)
		if !alive && fail == nil {
			fail = &Failure{Code: CodeProbeLocal, Detail: "слушатель " + LoopbackDNS + " не отвечает после перезапуска"}
		}
		s.stepResult(stepProbe, alive)
		s.recordHistory(opGuardRestart, kernel, fail)
		if alive {
			log.Printf("dnstakeover: сторож: слушатель поднялся после перезапуска %s (попытка %d)", kernel, attempt)
			return false
		}
		log.Printf("dnstakeover: сторож: попытка %d перезапуска %s не помогла: %s", attempt, kernel, fail.Code)
	}
	if ctx.Err() != nil {
		return false
	}
	s.replaceSteps(guardReleaseSteps)
	s.releaseToFirmware(ctx, kernel, reasonWatchdog, "")
	return true
}

// releaseToFirmware возвращает DNS роутера прошивке от имени сторожа (вызывающий
// держит операцию службы): `no opkg dns-override` (если до включения override не
// был включён), затем файлы ядра kernel без слушателя (запущенное ядро
// перезапускается, остановленное — только запись). Открытое окно подтверждения
// забирается: неподтверждённое включение или перенос не помнятся, неподтверждённое
// «Применить» возвращается к снимку уже без слушателя. reasonWatchdog — перехват
// остаётся желаемым, state waiting (watchdog); reasonKernelChanged — перехват не
// желаем. Исход — recordOutcome (D-28): строка, уведомление dns_, история, тост.
func (s *Service) releaseToFirmware(ctx context.Context, kernel, reason, to string) {
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	w, hasWindow := s.claimWindow(nil)
	s.mu.Lock()
	overrideBefore := s.st.OverrideBefore
	s.mu.Unlock()
	if hasWindow && w.Kind == kindEnable {
		overrideBefore = w.Snapshot.OverrideBefore
	}

	var errs []string
	code := CodeGeneric
	s.setStep(stepRelease, statusRunning)
	if !overrideBefore {
		if err := s.fw.SetOverride(hctx, false); err != nil {
			errs = append(errs, err.Error())
			code = CodeOverrideFailed
		}
	}
	s.stepResult(stepRelease, len(errs) == 0)
	s.setStep(stepApply, statusRunning)
	var werr error
	if hasWindow && w.Kind == kindApply {
		snap := w.Snapshot
		if snap.Live, werr = disabledLive(kernel); werr == nil {
			_, werr = s.restoreWindowSnapshot(hctx, kernel, snap, false)
		}
	} else {
		werr = s.writeWithoutListener(hctx, kernel)
	}
	s.stepResult(stepApply, werr == nil)
	if werr != nil {
		errs = append(errs, werr.Error())
	}

	keep := reason == reasonWatchdog && !(hasWindow && (w.Kind == kindEnable || w.Kind == kindKernelSwitch))
	if err := s.update(func(p *persisted) {
		p.Window = nil
		if keep {
			p.Desired = true
			p.Kernel = kernel
			p.WaitingReason = waitingWatchdog
			return
		}
		p.Desired = false
		p.Kernel = ""
		p.KernelWasRunning = false
		p.WaitingReason = ""
		p.OverrideBefore = false
		p.Snapshot = nil
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
		log.Printf("dnstakeover: сторож: возврат DNS прошивке (%s): %s", reason, strings.Join(errs, "; "))
	}
	o := Outcome{Kind: OutcomeWatchdog, Kernel: kernel, Reason: reason}
	if reason == reasonKernelChanged {
		o = Outcome{Kind: OutcomeKernelChanged, Kernel: to, From: kernel, To: to, Reason: reason}
	}
	s.recordOutcome(o, fail)
}
