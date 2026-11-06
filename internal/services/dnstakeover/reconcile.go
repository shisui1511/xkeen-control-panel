package dnstakeover

import (
	"context"
	"log"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сверка прошивки и состояния перехвата на старте панели (D-16, R3, R8). Панель
// не сохраняет конфигурацию прошивки, поэтому после перезагрузки роутера override
// обычно снят, а живые данные перехвата включены: файлы ядра со слушателем :53 при
// занятом прошивкой порте не дают Xray подняться (факт xray_bind53_conflict),
// Mihomo поднимается без DNS (mihomo_bind53_conflict). Обратный случай R3 —
// override, поставленный панелью, остался (панель остановили или она упала до
// возврата DNS прошивке, override сохранён в стартовой конфигурации), а перехвата
// нет: LAN остаётся без DNS. Сверка снимает оба: первый — файлы без слушателя,
// подъём упавшего ядра и ожидание возврата сторожем; второй — `no opkg
// dns-override`, только если его ставила панель (OverrideSetByPanel).

const (
	// opReconcile — операция службы и запись истории: сверка на старте панели.
	opReconcile = "reconcile"
	// opReconcileOverride — запись истории: лишний override снят на старте.
	opReconcileOverride = "reconcile_override"
	// waitingReboot — причина ожидания: перезагрузка роутера или override снят
	// вне панели.
	waitingReboot = "reboot"
)

var (
	// reconcileSteps — шаги сверки при включённом перехвате без override: файлы
	// без слушателя, подъём упавшего ядра.
	reconcileSteps = []string{stepApply, stepRestart}
	// reconcileOverrideSteps — шаги снятия лишнего override.
	reconcileOverrideSteps = []string{stepRelease}
)

// reconcileOnStart — сверка на старте панели; вызывается в Start после отката
// окна подтверждения (148-16) и до сторожа. Читает override прошивки и живые
// данные перехвата:
//
//  1. перехват включён, override нет (перезагрузка роутера или override снят вне
//     панели) — файлы ядра-перехватчика без слушателя (запущенное ядро
//     перезапускается, остановленное — только запись); ядро не запущено, а до
//     этого работало (kernel_was_running) — RestartLocked под замком жизненного
//     цикла (ядро уронил слушатель панели на занятом прошивкой :53, R8);
//     перехват, желаемый до старта, остаётся желаемым, state waiting (reboot),
//     возврат — веткой сторожа; нежелаемым перехват желаемым не становится
//     (откат или выключение не дописали файлы ядра); активным
//     стало другое ядро — DNS у прошивки, перехват не желаем (kernel_changed);
//  2. перехват выключен, override включён, его поставила панель (OverrideSetByPanel,
//     WR-06) и порт 53 не держит ни ядро, ни другой процесс (R3) — `no opkg
//     dns-override` и запись в истории; override, поставленный не панелью, не
//     трогается;
//  3. перехват включён и override включён — слушатель проверит сторож.
//
// Команда сохранения конфигурации прошивки не вызывается (D-16).
func (s *Service) reconcileOnStart(ctx context.Context) {
	if !s.d.Layer.Enabled() {
		return
	}
	if ok, _ := s.fw.Available(ctx); !ok {
		return
	}
	override, err := s.fw.OverrideEnabled(ctx)
	if err != nil {
		log.Printf("dnstakeover: сверка на старте: override не прочитан: %v", err)
		return
	}
	if !override {
		// Override снят (перезагрузка роутера, снят вручную): признак «поставила
		// панель» устарел и не должен распространиться на override, который
		// пользователь включит позже.
		s.setOverrideByPanel(false)
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		log.Printf("dnstakeover: сверка на старте: состояние слоя не прочитано: %v", err)
		return
	}
	live, on := configlayer.ParseDNSTakeoverLive(st.Live)
	switch {
	case on && !override && isKernelName(live.Kernel):
		s.reconcileReboot(ctx, live.Kernel)
	case !on && override:
		s.reconcileStaleOverride(ctx)
	}
}

// reconcileReboot — случай 1 сверки: перехват ядра kernel включён, override нет.
func (s *Service) reconcileReboot(ctx context.Context, kernel string) {
	if !s.beginOp(opReconcile, reconcileSteps) {
		return
	}
	defer s.endOp()
	log.Printf("dnstakeover: сверка на старте: перехват %s включён, override нет — файлы без слушателя, ожидание возврата", kernel)
	hctx, cancel := context.WithTimeout(ctx, 4*hookTimeout)
	defer cancel()
	var errs []string
	code := CodeGeneric

	s.setStep(stepApply, statusRunning)
	werr := s.writeWithoutListener(hctx, kernel)
	s.stepResult(stepApply, werr == nil)
	if werr != nil {
		errs = append(errs, werr.Error())
	}
	if ctx.Err() != nil {
		return
	}

	s.mu.Lock()
	wasRunning := s.st.KernelWasRunning
	s.mu.Unlock()
	s.d.Kernel.InvalidateActiveState()
	active := s.d.Kernel.ActiveState()
	switch {
	case !active.Conflict && isKernelName(active.Kernel) && active.Kernel != kernel:
		// Ядро сменили мимо панели до её старта: DNS у прошивки (override уже
		// снят), файлы прежнего ядра без слушателя; перехват не желаем.
		s.setStep(stepRestart, statusSkipped)
		s.reconcileKernelChanged(hctx, kernel, active.Kernel, errs)
		return
	case active.Conflict || s.kernelProcessRunning(kernel) || !wasRunning:
		s.setStep(stepRestart, statusSkipped)
	case s.d.Applier == nil:
		s.setStep(stepRestart, statusFailed)
		errs = append(errs, "перезапуск ядра: KernelApplier не задан")
	default:
		s.setStep(stepRestart, statusRunning)
		if err := s.lockLifecycle(ctx); err != nil {
			return
		}
		res := s.d.Applier.RestartLocked(kernel)
		s.d.Lifecycle.Unlock()
		ok := res.Outcome != services.ApplyRestartFailed && res.Outcome != services.ApplySavedKernelConflict
		s.stepResult(stepRestart, ok)
		if !ok {
			errs = append(errs, "перезапуск "+kernel+": "+string(res.Outcome)+" "+res.Error)
			code = CodeRestartFailed
		}
	}

	desired := false
	if err := s.update(func(p *persisted) {
		desired = p.Desired
		if !p.Desired {
			// Перехват не желаем (откат или выключение не дописали файлы ядра):
			// файлы без слушателя записаны — и всё. Желаемым он не становится,
			// иначе сторож вернёт перехват, который пользователь откатил или не
			// подтвердил (D-07).
			return
		}
		p.Kernel = kernel
		p.WaitingReason = waitingReboot
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	if !desired && len(errs) == 0 {
		// Файлы без слушателя дописаны, а откат до строки proxy_dns не дошёл: сторож
		// (guardStaleListener) после этого Live уже не сработает, и строка осталась бы
		// выключенной. Возвращается к значению до включения, как на остальных путях,
		// делающих перехват нежелаемым.
		if err := s.releaseProxyLine(hctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	var fail *Failure
	if len(errs) > 0 {
		fail = &Failure{Code: code, Params: kernelParam(kernel), Detail: truncateDetail(strings.Join(errs, "\n"))}
		s.setFailure(fail)
		log.Printf("dnstakeover: сверка на старте: %s", strings.Join(errs, "; "))
	}
	s.recordHistory(opReconcile, kernel, fail)
}

// reconcileKernelChanged — на старте панели активным оказалось другое ядро: DNS у
// прошивки, перехват не желаем, исход kernel_changed (как у сторожа); строка
// proxy_dns возвращается к значению до включения, если файлы прежнего ядра
// записаны без слушателя (releaseProxyLine).
func (s *Service) reconcileKernelChanged(ctx context.Context, from, to string, errs []string) {
	log.Printf("dnstakeover: сверка на старте: активное ядро %s вместо %s — перехват не возвращается", to, from)
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
	if len(errs) == 0 {
		if err := s.releaseProxyLine(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	var fail *Failure
	if len(errs) > 0 {
		fail = &Failure{Code: CodeGeneric, Detail: truncateDetail(strings.Join(errs, "\n"))}
		s.setFailure(fail)
	}
	s.recordOutcome(Outcome{Kind: OutcomeKernelChanged, Kernel: to, From: from, To: to, Reason: reasonKernelChanged}, fail)
}

// reconcileStaleOverride — случай 2 сверки (R3): перехват выключен, override
// включён. Override снимается, если его поставила панель и не вернула
// (OverrideSetByPanel) и порт 53 не держит ни ядро, ни другой процесс: включённый
// override при свободном :53 оставляет LAN без DNS, а занятый :53 значит, что DNS
// роутера сознательно отдан другому слушателю. Override без признака (поставлен
// вручную, остался от прежнего формата файла) не трогается.
func (s *Service) reconcileStaleOverride(ctx context.Context) {
	s.mu.Lock()
	byPanel := s.st.OverrideSetByPanel
	s.mu.Unlock()
	if !byPanel {
		// Override поставила не панель (вручную, при старте роутера, прошивкой): чужой
		// override не трогается, даже когда :53 свободен.
		log.Printf("dnstakeover: сверка на старте: override включён не панелью — не трогаем")
		return
	}
	switch owner := PortOwner(); owner.Kind {
	case OwnerKernel, OwnerOther:
		log.Printf("dnstakeover: сверка на старте: override включён, порт 53 держит %s %s%s — не трогаем", owner.Kind, owner.Kernel, owner.Process)
		return
	}
	if !s.beginOp(opReconcile, reconcileOverrideSteps) {
		return
	}
	defer s.endOp()
	log.Printf("dnstakeover: сверка на старте: override включён без перехвата — no opkg dns-override")
	hctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	s.setStep(stepRelease, statusRunning)
	err := s.releaseOverride(hctx)
	s.stepResult(stepRelease, err == nil)
	var fail *Failure
	if err != nil {
		fail = &Failure{Code: CodeOverrideFailed, Detail: truncateDetail(err.Error())}
		s.setFailure(fail)
		log.Printf("dnstakeover: сверка на старте: %v", err)
	}
	s.recordHistory(opReconcileOverride, "", fail)
}
