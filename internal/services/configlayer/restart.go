package configlayer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// KernelApplier — единый источник решения «перезапускать ли ядро» и сам
// перезапуск (*services.KernelApplier).
type KernelApplier interface {
	Preview(targets ...string) services.ApplyResult
	ApplyLocked(targets ...string) services.ApplyResult
}

// MihomoControl — управление Mihomo через Clash API (*services.MihomoService).
type MihomoControl interface {
	ReloadConfig(configPath string) error
	ProviderCount(ctx context.Context, providerType, name string) (int, error)
}

// LifecycleLocker — замок жизненного цикла ядра (*sync.Mutex подходит).
type LifecycleLocker interface {
	TryLock() bool
	Lock()
	Unlock()
}

// Тайминги подтверждения перезапуска по процессу ядра (A2): xkeen -start может
// идти больше минуты, а окно устойчивости ловит ядро, упавшее сразу после старта.
var (
	RestartConfirmTimeout = 90 * time.Second
	RestartPollInterval   = 500 * time.Millisecond
	RestartStableWindow   = 5 * time.Second
	// RestartExpectTimeout — сколько после рестарта Mihomo ждать готовности API и
	// совпадения числа узлов и правил провайдеров с ожидаемым. Отсчёт идёт от
	// подтверждения процесса ядра, а не от начала применения.
	RestartExpectTimeout = defaultRestartExpectTimeout(runtime.GOARCH)
)

// defaultRestartExpectTimeout — время ожидания API и провайдеров Mihomo после
// рестарта для платформы. На MIPS (mips, mipsle) Mihomo с geodata и rule-провайдерами
// поднимает Clash API заметно дольше, поэтому там 60 с, иначе 15 с (допущение не
// проверено на железе, как и defaultValidateTimeout: значение подкручивается через
// RestartExpectTimeout).
func defaultRestartExpectTimeout(goarch string) time.Duration {
	switch goarch {
	case "mips", "mipsle":
		return 60 * time.Second
	}
	return 15 * time.Second
}

// Ошибки TryBegin.
var (
	ErrApplyBusy  = errors.New("применение уже идёт")
	ErrKernelBusy = errors.New("идёт другая операция с ядром")
)

// Исходы перезапуска (RestartView.Outcome).
const (
	RestartOutcomeRestarted        = "restarted"
	RestartOutcomeHotReloaded      = "hot_reloaded"
	RestartOutcomeRestartedReload  = "restarted_after_reload_failed"
	RestartOutcomeDeferred         = "deferred"
	RestartOutcomeUntouchedIdle    = "untouched_inactive"
	RestartOutcomeUntouchedClash   = "untouched_conflict"
	RestartOutcomeFailedRolledBack = "failed_rolled_back"
	// Откат файлов не удался: журнал остался для повторной попытки при старте.
	RestartOutcomeFailedRollbackFailed = "failed_rollback_failed"
	// Файлы возвращены, но ядро на них не поднялось.
	RestartOutcomeFailedKernelDown = "failed_kernel_down"
)

// Коды пояснений перезапуска (RestartView.NoteCode и StepView.NoteCode).
const (
	NoteKernelStopped        = "kernel_stopped"
	NoteKernelInactive       = "kernel_inactive"
	NoteKernelConflict       = "kernel_conflict"
	NoteHotReloadFailed      = "hot_reload_failed_restarting"
	NoteRestartFailedRolling = "restart_failed_rolling_back"
	NoteRestartNotConfigured = "restart_not_configured"
)

// errInterrupted — запуск прерван отменой контекста (остановка панели). Откат и
// повторные рестарты при этом не выполняются: журнал остаётся, и RecoverJournal
// вернёт файлы при следующем старте панели.
var errInterrupted = errors.New("применение прервано остановкой панели")

// interrupted — ошибка прерывания, если контекст отменён, иначе nil.
func interrupted(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", errInterrupted, err)
	}
	return nil
}

// restartError — перезапуск не удался. Kernel — ядро, на котором остановились.
// RolledBack — прежние файлы возвращены; Recovered — ядро снова работает на них.
type restartError struct {
	Kernel     string
	Err        error
	RolledBack bool
	Recovered  bool
}

func (e *restartError) Error() string {
	return fmt.Sprintf("ядро %s не поднялось после применения: %v", e.Kernel, e.Err)
}

func (e *restartError) Unwrap() error { return e.Err }

// setRestartViews сохраняет накопленные итоги перезапуска в состоянии запуска;
// вне запуска итог прошлого применения не переписывается.
func (p *Pipeline) setRestartViews(views []RestartView) {
	p.mu.Lock()
	if p.view.Running {
		p.view.Restart = append([]RestartView(nil), views...)
	}
	p.mu.Unlock()
}

// runRestart выполняет шаг «Перезапуск» и отражает его в состоянии шага.
func (p *Pipeline) runRestart(ctx context.Context, plan Plan, set *BackupSet) ([]RestartView, error) {
	if p.d.Applier == nil {
		p.setStep(StepRestart, StepSkipped, NoteRestartNotConfigured, "")
		return nil, nil
	}
	p.setStep(StepRestart, StepRunning, "", "")
	views, err := p.restartKernels(ctx, plan, set)
	if err != nil {
		note := ""
		var re *restartError
		if errors.As(err, &re) {
			note = NoteRestartFailedRolling
		}
		p.setStep(StepRestart, StepFailed, note, err.Error())
		return views, err
	}
	state, note := restartStepState(views)
	p.setStep(StepRestart, state, note, "")
	return views, nil
}

// restartStepState сводит итоги ядер к состоянию шага: done — хоть одно ядро
// перезапущено или перечитало конфиг; deferred — ядра с изменениями остановлены;
// иначе пропущен.
func restartStepState(views []RestartView) (StepState, string) {
	var deferred, untouched string
	for _, v := range views {
		switch v.Outcome {
		case RestartOutcomeRestarted, RestartOutcomeHotReloaded, RestartOutcomeRestartedReload:
			return StepDone, ""
		case RestartOutcomeDeferred:
			if deferred == "" {
				deferred = v.NoteCode
			}
		default:
			if untouched == "" {
				untouched = v.NoteCode
			}
		}
	}
	switch {
	case deferred != "":
		return StepDeferred, deferred
	case untouched != "":
		return StepSkipped, untouched
	default:
		return StepSkipped, NoteNoChanges
	}
}

// restartKernels перезапускает ядра с изменениями (порядок xray, mihomo). Решение
// по каждому ядру принимает KernelApplier.Preview; рестарт идёт только через
// ApplyLocked под замком жизненного цикла, который держит вызывающий (замок не
// реентерабелен: Apply взял бы его второй раз).
func (p *Pipeline) restartKernels(ctx context.Context, plan Plan, set *BackupSet) ([]RestartView, error) {
	var views []RestartView
	for _, kernel := range []string{KernelXray, KernelMihomo} {
		if !plan.Changes(kernel) {
			continue
		}
		if err := interrupted(ctx); err != nil {
			return views, err
		}
		rv, err := p.restartKernel(ctx, kernel, plan, set)
		views = append(views, rv)
		p.setRestartViews(views)
		if err != nil {
			return views, err
		}
	}
	return views, nil
}

// untouchedView переводит исход Applier в итог, где ядро не перезапускалось.
func untouchedView(kernel string, outcome services.ApplyOutcome) (RestartView, bool) {
	switch outcome {
	case services.ApplySavedKernelStopped:
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeDeferred, NoteCode: NoteKernelStopped}, true
	case services.ApplySavedKernelInactive:
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeUntouchedIdle, NoteCode: NoteKernelInactive}, true
	case services.ApplySavedKernelConflict:
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeUntouchedClash, NoteCode: NoteKernelConflict}, true
	}
	return RestartView{}, false
}

// kernelRestarter — необязательная возможность applier'а: принудительный
// рестарт ядра, которое панель сама уронила (*services.KernelApplier).
type kernelRestarter interface {
	RestartLocked(kernel string) services.ApplyResult
}

// restartKernel перезапускает одно ядро с изменениями. Неудача любого пути —
// откат файлов панели и повторный рестарт на прежних файлах (D-17, A3).
func (p *Pipeline) restartKernel(ctx context.Context, kernel string, plan Plan, set *BackupSet) (RestartView, error) {
	pr := p.d.Applier.Preview(kernel)
	if pr.Outcome != services.ApplyRestarted {
		if v, ok := untouchedView(kernel, pr.Outcome); ok {
			return v, nil
		}
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeUntouchedIdle, NoteCode: NoteKernelInactive}, nil
	}

	outcome := RestartOutcomeRestarted
	var hotErr error
	if kernel == KernelMihomo && p.d.Mihomo != nil {
		if hotErr = p.hotReloadMihomo(ctx, plan); hotErr == nil {
			return RestartView{Kernel: kernel, Outcome: RestartOutcomeHotReloaded}, nil
		}
		// Остановка панели посреди горячей перезагрузки: полный рестарт не нужен.
		if err := interrupted(ctx); err != nil {
			return RestartView{Kernel: kernel}, err
		}
		// Мягкий путь не удался: обычный рестарт Mihomo.
		p.setStep(StepRestart, StepRunning, NoteHotReloadFailed, "")
		outcome = RestartOutcomeRestartedReload
	}
	err := p.restartAndConfirm(ctx, kernel)
	if err == nil && kernel == KernelMihomo {
		err = p.confirmMihomoAfterRestart(ctx, plan)
	}
	if err != nil {
		// Отмена контекста — не неудача ядра: не откатываем и не перезапускаем ещё раз.
		if ierr := interrupted(ctx); ierr != nil {
			return RestartView{Kernel: kernel}, ierr
		}
		if hotErr != nil {
			err = fmt.Errorf("перезагрузка конфигурации: %v; перезапуск: %w", hotErr, err)
		}
		return p.rollbackAndRestart(ctx, kernel, set, err)
	}
	return RestartView{Kernel: kernel, Outcome: outcome}, nil
}

// hotReloadMihomo перечитывает конфиг Mihomo без смены PID (PUT /configs?force=true),
// сверяет провайдеры с Expect и подтверждает, что процесс остался прежним.
func (p *Pipeline) hotReloadMihomo(ctx context.Context, plan Plan) error {
	prev := 0
	if st, ok := p.processState(KernelMihomo); ok {
		prev = st.PID
	}
	if err := p.d.Mihomo.ReloadConfig(filepath.Join(p.d.Roots.Mihomo, "config.yaml")); err != nil {
		return err
	}
	if err := p.checkExpect(ctx, plan, false); err != nil {
		return err
	}
	if err := p.confirmRunning(ctx, KernelMihomo, prev); err != nil {
		return err
	}
	if p.d.MihomoAPIReady != nil && !p.d.MihomoAPIReady() {
		return errors.New("API Mihomo не отвечает после перезагрузки")
	}
	return nil
}

// confirmMihomoAfterRestart ждёт готовности API Mihomo после рестарта и сверяет
// провайдеры с Expect (API поднимается не сразу после старта процесса).
func (p *Pipeline) confirmMihomoAfterRestart(ctx context.Context, plan Plan) error {
	if p.d.MihomoAPIReady != nil {
		if err := pollUntil(ctx, RestartExpectTimeout, p.d.MihomoAPIReady); err != nil {
			return errors.New("API Mihomo не ответил после перезапуска")
		}
	}
	return p.checkExpect(ctx, plan, true)
}

// pollUntil вызывает ok каждые RestartPollInterval до true или таймаута.
func pollUntil(ctx context.Context, timeout time.Duration, ok func() bool) error {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(RestartPollInterval)
	defer tick.Stop()
	for !ok() {
		if time.Now().After(deadline) {
			return errors.New("таймаут")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}

// checkExpect сверяет число узлов и правил провайдеров записанных файлов Mihomo
// с ожидаемым: file-провайдер с битым содержимым молча даёт 0 (143-FINDINGS Q3).
// wait=true повторяет сверку до RestartExpectTimeout.
func (p *Pipeline) checkExpect(ctx context.Context, plan Plan, wait bool) error {
	if p.d.Mihomo == nil {
		return nil
	}
	for _, fp := range plan.Files {
		if fp.Kernel != KernelMihomo || fp.Action != ActionWrite || fp.Expect == nil {
			continue
		}
		exp := fp.Expect
		var last error
		matched := func() bool {
			n, err := p.d.Mihomo.ProviderCount(ctx, exp.ProviderType, exp.ProviderName)
			switch {
			case err == nil && n == exp.Count:
				return true
			case errors.Is(err, services.ErrProviderEmpty) && exp.Count == 0:
				return true
			case err != nil:
				last = fmt.Errorf("провайдер %s: %w", exp.ProviderName, err)
			default:
				last = fmt.Errorf("провайдер %s: ожидалось %d, получено %d", exp.ProviderName, exp.Count, n)
			}
			return false
		}
		if !wait {
			if !matched() {
				return last
			}
			continue
		}
		if err := pollUntil(ctx, RestartExpectTimeout, matched); err != nil {
			if last != nil {
				return last
			}
			return err
		}
	}
	return nil
}

// rollbackAndRestart возвращает файлы панели из набора копий и перезапускает ядро
// на прежних файлах; исход в RestartView и коды restartError отражают, что из
// этого удалось (откат файлов, подъём ядра). Манифест, Applied и черновик не меняются (D-07). Пока идёт
// откат, остальные ядра не перезапускаются: вызывающий прекращает обход.
func (p *Pipeline) rollbackAndRestart(ctx context.Context, kernel string, set *BackupSet, cause error) (RestartView, error) {
	p.setStep(StepRestart, StepRunning, NoteRestartFailedRolling, "")
	view := RestartView{Kernel: kernel, Outcome: RestartOutcomeFailedRolledBack, NoteCode: NoteRestartFailedRolling}
	re := &restartError{Kernel: kernel, Err: cause}
	rbErr := p.rollback(set)
	if !filesRestored(rbErr) {
		// Журнал остаётся: RecoverJournal повторит откат при старте панели.
		re.Err = fmt.Errorf("%w; откат файлов не удался: %v", cause, rbErr)
		view.Outcome = RestartOutcomeFailedRollbackFailed
		return view, re
	}
	re.RolledBack = true
	if rbErr != nil {
		// Файлы на месте, не снялся только журнал: ядро поднимаем на прежних файлах.
		re.Err = fmt.Errorf("%w; журнал отката не снят: %v", cause, rbErr)
		cause = re.Err
	}
	if rerr := p.recoverKernel(ctx, kernel); rerr != nil {
		re.Err = fmt.Errorf("%w; повторный рестарт на прежних файлах: %v", cause, rerr)
		view.Outcome = RestartOutcomeFailedKernelDown
		return view, re
	}
	re.Recovered = true
	return view, re
}

// recoverKernel поднимает ядро на прежних файлах после отката. Ядро, которое мы
// только что уронили, остановлено не пользователем, поэтому, если applier умеет,
// запускается принудительный рестарт (ApplyLocked не запускает остановленное).
func (p *Pipeline) recoverKernel(ctx context.Context, kernel string) error {
	if err := interrupted(ctx); err != nil {
		return err
	}
	var res services.ApplyResult
	if r, ok := p.d.Applier.(kernelRestarter); ok {
		res = r.RestartLocked(kernel)
	} else {
		res = p.d.Applier.ApplyLocked(kernel)
	}
	switch res.Outcome {
	case services.ApplyRestarted:
	case services.ApplyRestartFailed:
		return errors.New(res.Error)
	default:
		return fmt.Errorf("ядро не запущено: %s", res.Outcome)
	}
	return p.confirmRunning(ctx, kernel, 0)
}

// restartAndConfirm перезапускает ядро через ApplyLocked и подтверждает успех по
// процессу ядра: код выхода xkeen -restart всегда 0 и о запуске ничего не говорит.
func (p *Pipeline) restartAndConfirm(ctx context.Context, kernel string) error {
	if err := interrupted(ctx); err != nil {
		return err
	}
	res := p.d.Applier.ApplyLocked(kernel)
	if res.Outcome == services.ApplyRestartFailed {
		return errors.New(res.Error)
	}
	if res.Outcome != services.ApplyRestarted {
		return fmt.Errorf("перезапуск не выполнен: %s", res.Outcome)
	}
	return p.confirmRunning(ctx, kernel, 0)
}

// processState — текущее состояние процесса ядра.
func (p *Pipeline) processState(kernel string) (services.KernelProcessState, bool) {
	if p.d.ProcessStates == nil {
		return services.KernelProcessState{}, false
	}
	for _, st := range p.d.ProcessStates() {
		if st.Name == kernel {
			return st, true
		}
	}
	return services.KernelProcessState{}, false
}

// confirmRunning опрашивает процесс ядра каждые RestartPollInterval до статуса
// running (не дольше RestartConfirmTimeout), затем ядро обязано оставаться
// running с тем же PID всё окно RestartStableWindow. wantPID > 0 требует именно
// этот PID (горячая перезагрузка: процесс обязан остаться прежним).
func (p *Pipeline) confirmRunning(ctx context.Context, kernel string, wantPID int) error {
	if p.d.ProcessStates == nil {
		return errors.New("нет источника состояния процессов ядер")
	}
	deadline := time.Now().Add(RestartConfirmTimeout)
	var stableSince time.Time
	var pid int
	tick := time.NewTicker(RestartPollInterval)
	defer tick.Stop()
	for {
		st, ok := p.processState(kernel)
		now := time.Now()
		running := ok && st.Status == "running"
		switch {
		case running && wantPID > 0 && st.PID != wantPID:
			return fmt.Errorf("PID процесса сменился: был %d, стал %d", wantPID, st.PID)
		case running && stableSince.IsZero():
			stableSince, pid = now, st.PID
		case running && st.PID != pid:
			return fmt.Errorf("процесс перезапустился в окне устойчивости: PID %d → %d", pid, st.PID)
		case !running && !stableSince.IsZero():
			return errors.New("процесс пропал в окне устойчивости")
		case !running && now.After(deadline):
			return fmt.Errorf("процесс не поднялся за %v", RestartConfirmTimeout)
		}
		if running && now.Sub(stableSince) >= RestartStableWindow {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// waitFor вызывает try каждые LifecyclePollInterval, пока он не вернёт true;
// отмена контекста прекращает ожидание и возвращает ctx.Err().
func waitFor(ctx context.Context, try func() bool) error {
	tick := time.NewTicker(LifecyclePollInterval)
	defer tick.Stop()
	for !try() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}

// LifecyclePollInterval — как часто фоновый запуск пробует взять замки применения и жизненного цикла.
var LifecyclePollInterval = 250 * time.Millisecond

// TryBegin берёт замки перед Run: сначала applyMu («одно применение за раз»), затем
// замок жизненного цикла ядра. Порядок всегда applyMu → lifecycleMu; обратный
// запрещён: обработчики HTTP, держащие lifecycleMu, applyMu не берут
// (восстановление снимка — 144-10). Второе применение по кнопке во время идущего получает
// ErrApplyBusy. user=true (запуск по кнопке) при занятом замке ядра сразу получает
// ErrKernelBusy; user=false (фоновый запуск) ждёт оба замка (и applyMu) с паузой
// LifecyclePollInterval и отменяется контекстом (тогда возвращается ctx.Err()).
// release снимает замок ядра, затем applyMu; повторный вызов безопасен.
//
// Run вызывается только под этими замками и внутри использует ApplyLocked, а не
// Apply: sync.Mutex не реентерабелен.
func (p *Pipeline) TryBegin(ctx context.Context, user bool) (release func(), err error) {
	if !p.applyMu.TryLock() {
		if user {
			return nil, ErrApplyBusy
		}
		// Фоновый запуск (установка ядра, включение слоя) не должен теряться из-за
		// идущего применения: ждёт его конца с тем же опросом, что и замок ядра.
		if err := waitFor(ctx, p.applyMu.TryLock); err != nil {
			return nil, err
		}
	}
	lifecycle := p.d.Lifecycle
	if lifecycle != nil {
		switch {
		case user:
			if !lifecycle.TryLock() {
				p.applyMu.Unlock()
				return nil, ErrKernelBusy
			}
		default:
			if err := waitFor(ctx, lifecycle.TryLock); err != nil {
				p.applyMu.Unlock()
				return nil, err
			}
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if lifecycle != nil {
				lifecycle.Unlock()
			}
			p.applyMu.Unlock()
		})
	}, nil
}
