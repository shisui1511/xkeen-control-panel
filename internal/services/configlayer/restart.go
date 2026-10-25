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
	// UpdateProvider обновляет один провайдер точечно (PUT /providers/{тип}/{имя}),
	// не перечитывая профиль целиком. providerType — "proxies" или "rules".
	UpdateProvider(ctx context.Context, providerType, name string) error
	// HasController — настроен ли у Mihomo external-controller. Без него Clash API
	// нет: ни горячей перезагрузки, ни сверки провайдеров, ядро подтверждается
	// только по процессу.
	HasController() bool
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
	// Перезапуск прерван остановкой панели: исход ядра неизвестен, журнал остался.
	RestartOutcomeInterrupted = "interrupted"
	// Все изменения ядра — файлы без влияния на запущенное ядро (Inert): записаны
	// и проверены, перезапуска и перезагрузки нет.
	RestartOutcomeNoRuntimeEffect = "no_runtime_effect"
)

// Коды пояснений перезапуска (RestartView.NoteCode и StepView.NoteCode).
const (
	NoteKernelStopped        = "kernel_stopped"
	NoteKernelInactive       = "kernel_inactive"
	NoteKernelConflict       = "kernel_conflict"
	NoteHotReloadFailed      = "hot_reload_failed_restarting"
	NoteRestartFailedRolling = "restart_failed_rolling_back"
	NoteRestartNotConfigured = "restart_not_configured"
	// NoteNoRuntimeEffect — файлы записаны, но ядро их пока не использует.
	NoteNoRuntimeEffect = "no_runtime_effect"
	// NoteProvidersUpdated — Mihomo обновил только изменившиеся провайдеры точечно
	// (PUT /providers/...), профиль не перечитывался. Код несёт RestartView у
	// исхода hot_reloaded; перезагрузка профиля целиком (PUT /configs) кода не имеет.
	NoteProvidersUpdated = "providers_updated"
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

// interruptedView — итог перезапуска ядра, прерванного остановкой панели.
func interruptedView(kernel string) RestartView {
	return RestartView{Kernel: kernel, Outcome: RestartOutcomeInterrupted}
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
// иначе пропущен (в том числе с пометкой no_runtime_effect: файлы записаны, а
// ядро их пока не использует).
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
		if !plan.RuntimeChanges(kernel) {
			// Только файлы с признаком Inert: ядро их не читает, трогать его незачем.
			views = append(views, RestartView{Kernel: kernel, Outcome: RestartOutcomeNoRuntimeEffect, NoteCode: NoteNoRuntimeEffect})
			p.setRestartViews(views)
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
	if kernel == KernelMihomo && p.d.Mihomo != nil && p.d.Mihomo.HasController() {
		var targeted bool
		if targeted, hotErr = p.hotReloadMihomo(ctx, plan, set); hotErr == nil {
			view := RestartView{Kernel: kernel, Outcome: RestartOutcomeHotReloaded}
			if targeted {
				view.NoteCode = NoteProvidersUpdated
			}
			return view, nil
		}
		// Остановка панели посреди горячей перезагрузки: полный рестарт не нужен.
		if err := interrupted(ctx); err != nil {
			return interruptedView(kernel), err
		}
		// Мягкий путь не удался: обычный рестарт Mihomo.
		p.setStep(StepRestart, StepRunning, NoteHotReloadFailed, "")
		outcome = RestartOutcomeRestartedReload
	}
	applied, err := p.restartAndConfirm(ctx, kernel)
	if err == nil && applied != services.ApplyRestarted {
		// Applier по свежему статусу решил не перезапускать (ядро остановили между
		// Preview и ApplyLocked): это не отказ ядра, откатывать файлы незачем.
		if v, ok := untouchedView(kernel, applied); ok {
			return v, nil
		}
		err = fmt.Errorf("перезапуск не выполнен: %s", applied)
	}
	if err == nil && kernel == KernelMihomo {
		err = p.confirmMihomoAfterRestart(ctx, plan)
	}
	if err != nil {
		// Отмена контекста — не неудача ядра: не откатываем и не перезапускаем ещё раз.
		if ierr := interrupted(ctx); ierr != nil {
			return interruptedView(kernel), ierr
		}
		if hotErr != nil {
			err = fmt.Errorf("перезагрузка конфигурации: %v; перезапуск: %w", hotErr, err)
		}
		return p.rollbackAndRestart(ctx, kernel, set, err)
	}
	return RestartView{Kernel: kernel, Outcome: outcome}, nil
}

// hotReloadMihomo применяет изменения Mihomo без смены PID, сверяет провайдеры с
// Expect и подтверждает, что процесс остался прежним. Путь выбирается по плану
// (D-28): если изменились только существующие файлы провайдеров с Expect, каждый
// обновляется точечно (PUT /providers/{тип}/{имя}); иначе, или если точечное
// обновление не удалось, профиль перечитывается целиком (PUT /configs?force=true,
// одна попытка). Результат targeted — прошло ли применение точечным путём.
func (p *Pipeline) hotReloadMihomo(ctx context.Context, plan Plan, set *BackupSet) (targeted bool, err error) {
	prev := 0
	if st, ok := p.processState(KernelMihomo); ok {
		prev = st.PID
	}
	if exps := providerTargets(plan, set); len(exps) > 0 {
		targeted = p.updateProviders(ctx, exps) == nil
		if !targeted {
			if err := interrupted(ctx); err != nil {
				return false, err
			}
		}
	}
	if !targeted {
		if err := p.d.Mihomo.ReloadConfig(filepath.Join(p.d.Roots.Mihomo, "config.yaml")); err != nil {
			return false, err
		}
	}
	// Точечное обновление отвечает 204 и при пустом наборе: считаем применённым
	// только после сверки числа узлов и правил с Expect (143-FINDINGS Q3).
	if err := p.checkExpect(ctx, plan, false); err != nil {
		return false, err
	}
	if err := p.confirmRunning(ctx, KernelMihomo, prev); err != nil {
		return false, err
	}
	if p.d.MihomoAPIReady != nil && !p.d.MihomoAPIReady() {
		return false, errors.New("API Mihomo не отвечает после перезагрузки")
	}
	return targeted, nil
}

// providerUpdateTimeout — предел одного точечного обновления провайдера.
const providerUpdateTimeout = 30 * time.Second

// updateProviders обновляет провайдеры точечно, прекращая на первой ошибке.
func (p *Pipeline) updateProviders(ctx context.Context, exps []*Expectation) error {
	for _, exp := range exps {
		uctx, cancel := context.WithTimeout(ctx, providerUpdateTimeout)
		err := p.d.Mihomo.UpdateProvider(uctx, exp.ProviderType, exp.ProviderName)
		cancel()
		if err != nil {
			return fmt.Errorf("провайдер %s: %w", exp.ProviderName, err)
		}
	}
	return nil
}

// providerTargets возвращает Expect провайдеров для точечного обновления или nil,
// если путь не подходит. Подходит план, где среди действий над Mihomo (записи без
// Inert) есть только перезапись файлов провайдеров с Expect, которые уже лежали
// на диске до записи (по набору копий), и нет ни профиля, ни удалений, ни новых
// файлов, ни сирот ядра. Inert-файлы ядро не читает и в выборе пути не участвуют.
func providerTargets(plan Plan, set *BackupSet) []*Expectation {
	if set == nil || plan.Routing.hasMihomoDisk() {
		// Перекладка config.yaml меняет профиль ядра: нужна полная перезагрузка.
		return nil
	}
	for _, o := range plan.Orphans {
		if o.Kernel == KernelMihomo {
			return nil
		}
	}
	existed := make(map[string]bool, len(set.Meta.Files))
	for _, m := range set.Meta.Files {
		if m.Existed && m.Reason == ReasonOverwrite {
			existed[m.Key] = true
		}
	}
	var out []*Expectation
	for _, fp := range plan.Files {
		if fp.Kernel != KernelMihomo || fp.Inert {
			continue
		}
		switch fp.Action {
		case ActionWrite:
		case ActionDelete:
			return nil
		default:
			continue
		}
		switch fp.Kind {
		case KindMihomoProxyProvider, KindMihomoRuleProvider, KindMihomoRuleProviderText:
		default:
			return nil
		}
		if fp.Expect == nil || fp.RemoveObsolete || !existed[fp.Key] {
			return nil
		}
		out = append(out, fp.Expect)
	}
	return out
}

// confirmMihomoAfterRestart ждёт готовности API Mihomo после рестарта и сверяет
// провайдеры с Expect (API поднимается не сразу после старта процесса). У Mihomo
// без external-controller API нет и не будет: там достаточно подтверждения процесса
// (confirmRestarted), которое к этому моменту уже пройдено.
func (p *Pipeline) confirmMihomoAfterRestart(ctx context.Context, plan Plan) error {
	if p.d.Mihomo != nil && !p.d.Mihomo.HasController() {
		return nil
	}
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
// Подтверждение то же, что после обычного перезапуска: PID, который был до
// рестарта, не засчитывается (confirmRestarted).
func (p *Pipeline) recoverKernel(ctx context.Context, kernel string) error {
	if err := interrupted(ctx); err != nil {
		return err
	}
	oldPID := p.runningPID(kernel)
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
	return p.confirmRestarted(ctx, kernel, oldPID)
}

// restartAndConfirm перезапускает ядро через ApplyLocked и подтверждает успех по
// процессу ядра: код выхода xkeen -restart всегда 0 и о запуске ничего не говорит.
// Возвращает исход ApplyLocked: подтверждение процесса (со сменой PID) идёт только
// после ApplyRestarted, то есть когда xkeen -restart действительно выполнялся. Любой
// другой исход без ошибки (ядро остановлено, неактивно, конфликт) значит «ядро не
// трогали» — решает вызывающий. PID до рестарта берётся до ApplyLocked.
func (p *Pipeline) restartAndConfirm(ctx context.Context, kernel string) (services.ApplyOutcome, error) {
	if err := interrupted(ctx); err != nil {
		return "", err
	}
	oldPID := p.runningPID(kernel)
	res := p.d.Applier.ApplyLocked(kernel)
	if res.Outcome == services.ApplyRestartFailed {
		return res.Outcome, errors.New(res.Error)
	}
	if res.Outcome != services.ApplyRestarted {
		return res.Outcome, nil
	}
	return res.Outcome, p.confirmRestarted(ctx, kernel, oldPID)
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

// runningPID — PID работающего процесса ядра сейчас, 0 — ядро не запущено.
func (p *Pipeline) runningPID(kernel string) int {
	if st, ok := p.processState(kernel); ok && st.Status == "running" && st.PID > 0 {
		return st.PID
	}
	return 0
}

// confirmRunning (строгий режим) опрашивает процесс ядра каждые RestartPollInterval до статуса
// running (не дольше RestartConfirmTimeout), затем ядро обязано оставаться
// running с тем же PID всё окно RestartStableWindow. wantPID > 0 требует именно
// этот PID (горячая перезагрузка: процесс обязан остаться прежним). Только для
// горячей перезагрузки: после xkeen -restart процесс обязан смениться, и там
// работает confirmRestarted.
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

// confirmRestarted подтверждает перезапуск ядра по процессу после xkeen -restart.
// xkeen -restart возвращается раньше, чем завершается старый процесс, а при старте
// ядра init-скрипт запускает и гасит вспомогательные экземпляры (проверки конфигурации,
// первый экземпляр до устойчивого), поэтому:
//   - процесс с PID oldPID (до рестарта) не засчитывается как запущенный;
//   - пропажа или смена PID внутри окна устойчивости не ошибка, а начало окна
//     заново: допустимо, пока не истёк общий срок RestartConfirmTimeout;
//   - успех — процесс держится с одним PID не меньше RestartStableWindow.
//
// Окно, начатое до истечения срока, доводится до конца (не дольше RestartStableWindow
// сверх срока), но новое окно после срока не начинается: смена или пропажа PID уже
// после RestartConfirmTimeout даёт ошибку сразу, в том числе когда PID меняется
// непрерывно и ни одно окно не доходит до конца. Ядро, которое так и не поднялось
// устойчиво, даёт ошибку.
func (p *Pipeline) confirmRestarted(ctx context.Context, kernel string, oldPID int) error {
	if p.d.ProcessStates == nil {
		return errors.New("нет источника состояния процессов ядер")
	}
	deadline := time.Now().Add(RestartConfirmTimeout)
	var stableSince time.Time
	var pid, lastPID, resets int
	onlyOld := false
	tick := time.NewTicker(RestartPollInterval)
	defer tick.Stop()
	for {
		st, ok := p.processState(kernel)
		now := time.Now()
		running := ok && st.Status == "running" && st.PID > 0
		onlyOld = false
		if running && oldPID > 0 && st.PID == oldPID {
			// Прежний процесс ещё не завершился: перезапуск им не подтверждается.
			running, onlyOld = false, true
		}
		switch {
		case running && stableSince.IsZero():
			stableSince, pid = now, st.PID
		case running && st.PID != pid:
			resets, lastPID = resets+1, pid
			stableSince, pid = now, st.PID
		case !running && !stableSince.IsZero():
			resets, lastPID = resets+1, pid
			stableSince = time.Time{}
		}
		if !stableSince.IsZero() && now.Sub(stableSince) >= RestartStableWindow {
			return nil
		}
		// Срок вышел, а устойчивого окна нет: либо процесса нет, либо окно (пере)начато
		// уже после срока. Окно, начатое до срока, ещё может дойти до конца.
		if now.After(deadline) && (stableSince.IsZero() || stableSince.After(deadline)) {
			switch {
			case onlyOld && resets == 0:
				return fmt.Errorf("прежний процесс PID %d не завершился за %v", oldPID, RestartConfirmTimeout)
			case resets > 0:
				return fmt.Errorf("процесс не поднялся устойчиво за %v: сбросов окна %d, последний PID %d", RestartConfirmTimeout, resets, lastPID)
			default:
				return fmt.Errorf("процесс не поднялся за %v", RestartConfirmTimeout)
			}
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

// applyTailWait — сколько запуск по кнопке ждёт конца хвоста завершённого запуска,
// прежде чем ответить ErrApplyBusy.
var applyTailWait = 5 * time.Second

// waitTail ждёт замок применения, пока прошлый запуск дорабатывает хвост: apply_done
// уже опубликован и клиент видит итог, а замок снимается чуть позже (runPipeline
// после Run ещё обновляет уведомления и публикует событие routing). Истина — замок
// взят; ложь — замок держит идущий запуск или другая операция под замком, хвост не
// кончился за applyTailWait или контекст отменён.
func (p *Pipeline) waitTail(ctx context.Context) bool {
	deadline := time.Now().Add(applyTailWait)
	for p.tail.Load() && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Millisecond):
		}
		if p.applyMu.TryLock() {
			return true
		}
	}
	return p.applyMu.TryLock()
}

// TryBegin берёт замки перед Run: сначала applyMu («одно применение за раз»), затем
// замок жизненного цикла ядра. Порядок всегда applyMu → lifecycleMu; обратный
// запрещён: обработчики HTTP, держащие lifecycleMu, applyMu не берут
// (восстановление снимка — 144-10). Второе применение по кнопке во время идущего получает
// ErrApplyBusy; пришедшее в хвост уже завершённого запуска (итог опубликован, замок
// ещё не снят) ждёт замок (waitTail). user=true (запуск по кнопке) при занятом замке
// ядра сразу получает ErrKernelBusy; user=false (фоновый запуск) ждёт оба замка (и applyMu) с паузой
// LifecyclePollInterval и отменяется контекстом (тогда возвращается ctx.Err()).
// release снимает замок ядра, затем applyMu; повторный вызов безопасен.
//
// Run вызывается только под этими замками и внутри использует ApplyLocked, а не
// Apply: sync.Mutex не реентерабелен.
func (p *Pipeline) TryBegin(ctx context.Context, user bool) (release func(), err error) {
	if !p.applyMu.TryLock() {
		if user {
			if !p.waitTail(ctx) {
				return nil, ErrApplyBusy
			}
		} else {
			// Фоновый запуск (установка ядра, включение слоя) не должен теряться из-за
			// идущего применения: ждёт его конца с тем же опросом, что и замок ядра.
			if err := waitFor(ctx, p.applyMu.TryLock); err != nil {
				return nil, err
			}
		}
	}
	// Замок взят: хвост прошлого запуска, если он был, кончился.
	p.tail.Store(false)
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
