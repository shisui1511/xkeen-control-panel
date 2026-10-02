package services

import (
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// ApplyOutcome — исход применения записанной конфигурации к ядру.
type ApplyOutcome string

const (
	// ApplyRestarted — целевое ядро было запущено и перезапущено.
	ApplyRestarted ApplyOutcome = "restarted"
	// ApplySavedKernelStopped — целевое ядро остановлено: файлы записаны,
	// ядро не запускалось (его остановил пользователь).
	ApplySavedKernelStopped ApplyOutcome = "saved_kernel_stopped"
	// ApplySavedKernelInactive — целевое ядро не является активным: файлы
	// записаны, активное ядро не тронуто.
	ApplySavedKernelInactive ApplyOutcome = "saved_kernel_inactive"
	// ApplyRestartFailed — рестарт запущенного ядра завершился ошибкой;
	// файлы остаются записанными.
	ApplyRestartFailed ApplyOutcome = "restart_failed"
	// ApplySavedKernelConflict — запущены оба ядра: файлы записаны, ни одно ядро
	// не перезапускалось (панель сама ничего не трогает, пока конфликт не снят).
	ApplySavedKernelConflict ApplyOutcome = "saved_kernel_conflict"
)

// ApplyTargetActive — цель «активное ядро»: работающее ядро, иначе name_client
// init-скрипта XKeen.
const ApplyTargetActive = "active"

// applyErrorMaxBytes — предел поля Error исхода restart_failed.
const applyErrorMaxBytes = 500

// applyErrorMaxLines — сколько последних непустых строк вывода xkeen попадает
// в Error.
const applyErrorMaxLines = 5

// ApplyResult — структурированный исход применения: по нему UI выбирает тост.
type ApplyResult struct {
	Outcome ApplyOutcome `json:"outcome"`
	// Kernel — ядро, к которому относилось применение.
	Kernel string `json:"kernel"`
	// ActiveKernel — ядро, которое сейчас считается активным.
	ActiveKernel string `json:"active_kernel"`
	// ActiveRunning — активное ядро может работать (статус не stopped и не
	// not_installed).
	ActiveRunning bool `json:"active_running"`
	// Error — причина для restart_failed: последние строки вывода xkeen.
	Error string `json:"error,omitempty"`
}

// KernelMayRun — ядро считается «может работать» для любого статуса, кроме
// stopped и not_installed. unknown и not_accessible относятся к «может
// работать»: лучше перезапустить и получить рабочий конфиг, чем оставить
// применённый конфиг без эффекта.
func KernelMayRun(processStatus string) bool {
	switch processStatus {
	case "stopped", "not_installed":
		return false
	default:
		return true
	}
}

// KernelApplier решает, нужно ли перезапускать целевое ядро после записи
// конфигурации, и выполняет рестарт. Единственный источник этого решения для
// всех мест «записал → перезапустил»: остановленное пользователем ядро не
// запускается, чужое ядро не перезапускается.
type KernelApplier struct {
	// lock сериализует применения: одновременно идёт не больше одного рестарта, а
	// решение каждого вызова принимается по свежему статусу после захвата. В
	// production это замок жизненного цикла API (WithLifecycleLock): Apply ждёт его
	// (фоновые вызывающие — подписки), а обработчики HTTP берут замок сами и
	// вызывают ApplyLocked.
	lock   *sync.Mutex
	status func(string) string
	// active — единое определение активного ядра (то же, что у capabilities):
	// работающий процесс, name_client учитывается, только когда ничего не запущено.
	active  func() ActiveKernelState
	restart func() (string, error)
}

// NewKernelApplier собирает KernelApplier из сервиса ядер и XKeen. Статус берётся
// из лёгкого ProcessStates (без запуска бинарников), активное ядро — из
// ActiveState с кэшем и запасными источниками.
func NewKernelApplier(kernels *KernelService, xkeen *XKeenService) *KernelApplier {
	status := func(string) string { return "not_installed" }
	active := func() ActiveKernelState { return ActiveKernelState{Kernel: "none"} }
	if kernels != nil {
		status = func(name string) string {
			for _, st := range kernels.ProcessStates() {
				if st.Name == name {
					return st.Status
				}
			}
			return "not_installed"
		}
		active = kernels.ActiveState
	}
	restart := func() (string, error) { return "", errors.New("xkeen service is not available") }
	if xkeen != nil {
		// `xkeen -restart` = остановка и запуск; горячей перезагрузки конфига
		// у Mihomo в панели нет.
		restart = xkeen.Restart
	}
	return &KernelApplier{lock: &sync.Mutex{}, status: status, active: active, restart: restart}
}

// NewKernelApplierFunc собирает KernelApplier из функций (тесты и места, где
// перезапуск идёт не через XKeenService). Активное ядро выводится из status тем
// же ResolveActiveState; возраст процессов неизвестен, поэтому два запущенных
// ядра всегда считаются конфликтом.
func NewKernelApplierFunc(status func(string) string, configured func() string, restart func() (string, error)) *KernelApplier {
	active := func() ActiveKernelState {
		states := []KernelProcessState{
			{Name: "xray", Status: status("xray")},
			{Name: "mihomo", Status: status("mihomo")},
		}
		return ResolveActiveState(states, nil, configured)
	}
	return &KernelApplier{lock: &sync.Mutex{}, status: status, active: active, restart: restart}
}

// applyDecision — результат decide: исход без побочных эффектов.
type applyDecision struct {
	result ApplyResult
	// restart — целевое ядро нужно перезапустить.
	restart bool
}

func isApplyKernelName(name string) bool {
	return name == "xray" || name == "mihomo"
}

// decide не имеет побочных эффектов: только читает статусы.
func (k *KernelApplier) decide(targets []string) applyDecision {
	st := k.active()

	// concrete — конкретные цели без дублей; wantsActive — среди целей есть
	// «активное».
	var concrete []string
	wantsActive := len(targets) == 0
	for _, t := range targets {
		switch {
		case t == ApplyTargetActive:
			wantsActive = true
		case isApplyKernelName(t):
			dup := false
			for _, c := range concrete {
				if c == t {
					dup = true
					break
				}
			}
			if !dup {
				concrete = append(concrete, t)
			}
		}
	}

	// Запущены оба ядра: ни одно не перезапускаем, файлы уже записаны.
	if st.Conflict {
		res := ApplyResult{Outcome: ApplySavedKernelConflict, ActiveKernel: st.Label(), ActiveRunning: true}
		if len(concrete) > 0 {
			res.Kernel = concrete[0]
		}
		return applyDecision{result: res}
	}
	active := st.Kernel
	if !isApplyKernelName(active) {
		active = ""
	}

	if len(concrete) == 0 && !wantsActive {
		// Только неизвестные имена: применять не к чему, ядро не трогаем.
		res := ApplyResult{Outcome: ApplySavedKernelInactive, ActiveKernel: active}
		if active != "" {
			res.ActiveRunning = KernelMayRun(k.status(active))
		}
		return applyDecision{result: res}
	}

	if active == "" {
		if len(concrete) == 0 {
			// «Активное» без активного ядра: применять не к чему.
			return applyDecision{result: ApplyResult{Outcome: ApplySavedKernelStopped}}
		}
		// Активное ядро не определить: считаем им первую цель.
		active = concrete[0]
	}
	if wantsActive {
		found := false
		for _, c := range concrete {
			if c == active {
				found = true
				break
			}
		}
		if !found {
			concrete = append(concrete, active)
		}
	}

	activeRunning := KernelMayRun(k.status(active))
	res := ApplyResult{Kernel: active, ActiveKernel: active, ActiveRunning: activeRunning}

	touched := false
	for _, c := range concrete {
		if c == active {
			touched = true
			break
		}
	}
	switch {
	case !touched:
		// Другое ядро не трогаем.
		res.Kernel = concrete[0]
		res.Outcome = ApplySavedKernelInactive
		return applyDecision{result: res}
	case !activeRunning:
		res.Outcome = ApplySavedKernelStopped
		return applyDecision{result: res}
	default:
		res.Outcome = ApplyRestarted
		return applyDecision{result: res, restart: true}
	}
}

// Preview — что произойдёт при Apply, без рестарта. Исход restarted значит
// «будет перезапущено». Мьютекс не берёт: предсказание не должно ждать
// идущего рестарта.
func (k *KernelApplier) Preview(targets ...string) ApplyResult {
	return k.decide(targets).result
}

// WillRestart — применение к target перезапустит ядро.
func (k *KernelApplier) WillRestart(target string) bool {
	return k.Preview(target).Outcome == ApplyRestarted
}

// WithLifecycleLock подменяет собственный замок applier общим замком жизненного
// цикла ядра, чтобы применение конфигурации и операции start/stop/switch
// сериализовались одним мьютексом. Вызывать только при сборке, до первого Apply.
// При l == nil замок не меняется.
func (k *KernelApplier) WithLifecycleLock(l *sync.Mutex) *KernelApplier {
	if l != nil {
		k.lock = l
	}
	return k
}

// Apply ждёт замок (блокирующе) и применяет конфигурацию по свежему статусу.
// Для вызывающих, которые замок не держат (фоновое применение после обновления
// подписки). Обработчики, уже взявшие замок жизненного цикла, вызывают ApplyLocked.
func (k *KernelApplier) Apply(targets ...string) ApplyResult {
	k.lock.Lock()
	defer k.lock.Unlock()
	return k.ApplyLocked(targets...)
}

// ApplyLocked решает по свежему статусу и при необходимости перезапускает ядро.
// Вызывающий уже держит замок жизненного цикла, переданный в WithLifecycleLock:
// sync.Mutex не реентерабелен, повторный захват заблокировал бы вызов навсегда.
// Откат записанных файлов не делается: при restart_failed конфиг остаётся.
func (k *KernelApplier) ApplyLocked(targets ...string) ApplyResult {
	d := k.decide(targets)
	if !d.restart {
		return d.result
	}
	out, err := k.restart()
	if err != nil {
		d.result.Outcome = ApplyRestartFailed
		d.result.Error = restartErrorReason(out, err)
	}
	return d.result
}

// restartErrorReason — последние непустые строки вывода xkeen без ANSI, не
// длиннее applyErrorMaxBytes; пустой вывод заменяется текстом ошибки.
func restartErrorReason(out string, err error) string {
	var lines []string
	for _, line := range strings.Split(utils.StripANSI(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > applyErrorMaxLines {
		lines = lines[len(lines)-applyErrorMaxLines:]
	}
	reason := strings.Join(lines, "\n")
	if reason == "" {
		if err != nil {
			reason = strings.TrimSpace(utils.StripANSI(err.Error()))
		}
		if reason == "" {
			reason = "restart failed"
		}
	}
	return truncateTailBytes(reason, applyErrorMaxBytes)
}

// truncateTailBytes оставляет не больше max последних байт, не разрезая
// UTF-8-символ.
func truncateTailBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}
