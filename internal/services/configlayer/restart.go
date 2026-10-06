package configlayer

import (
	"context"
	"errors"
	"fmt"
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
)

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

// restartError — перезапуск не удался. Kernel — ядро, на котором остановились.
type restartError struct {
	Kernel     string
	Err        error
	RolledBack bool
}

func (e *restartError) Error() string {
	return fmt.Sprintf("ядро %s не поднялось после применения: %v", e.Kernel, e.Err)
}

func (e *restartError) Unwrap() error { return e.Err }

// setRestartViews публикует накопленные итоги перезапуска в состоянии запуска.
func (p *Pipeline) setRestartViews(views []RestartView) {
	p.mu.Lock()
	p.view.Restart = append([]RestartView(nil), views...)
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
		p.setStep(StepRestart, StepFailed, "", err.Error())
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

func (p *Pipeline) restartKernel(ctx context.Context, kernel string, _ Plan, _ *BackupSet) (RestartView, error) {
	pr := p.d.Applier.Preview(kernel)
	if pr.Outcome != services.ApplyRestarted {
		if v, ok := untouchedView(kernel, pr.Outcome); ok {
			return v, nil
		}
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeUntouchedIdle, NoteCode: NoteKernelInactive}, nil
	}
	if err := p.restartAndConfirm(ctx, kernel); err != nil {
		return RestartView{Kernel: kernel, Outcome: RestartOutcomeFailedRolledBack, NoteCode: NoteRestartFailedRolling},
			&restartError{Kernel: kernel, Err: err}
	}
	return RestartView{Kernel: kernel, Outcome: RestartOutcomeRestarted}, nil
}

// restartAndConfirm перезапускает ядро через ApplyLocked и подтверждает успех по
// процессу ядра: код выхода xkeen -restart всегда 0 и о запуске ничего не говорит.
func (p *Pipeline) restartAndConfirm(ctx context.Context, kernel string) error {
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
