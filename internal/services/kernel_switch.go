package services

import "time"

// Переменные, а не константы: тесты подменяют их (с восстановлением через Cleanup).
var (
	// switchConfirmDeadline — сколько ждать подтверждения переключения ядра по
	// процессам (D-03). Скрипт XKeen возвращает управление раньше, чем старое ядро
	// успевает завершиться, а новое — подняться.
	switchConfirmDeadline = 15 * time.Second
	// switchConfirmInterval — шаг опроса /proc во время ожидания.
	switchConfirmInterval = 500 * time.Millisecond
)

// SwitchOutcome — исход переключения ядра, определённый по процессам, а не по
// коду выхода скрипта XKeen.
type SwitchOutcome string

const (
	// SwitchSwitched — старое ядро не работает, новое работает.
	SwitchSwitched SwitchOutcome = "switched"
	// SwitchOldStillRunning — новое ядро работает, но старое живо к дедлайну:
	// два ядра одновременно.
	SwitchOldStillRunning SwitchOutcome = "old_still_running"
	// SwitchNewNotStarted — новое ядро к дедлайну не поднялось.
	SwitchNewNotStarted SwitchOutcome = "new_not_started"
)

// SwitchResult — типизированный ответ switch_kernel. Old — ядро до переключения
// ("xray" | "mihomo" | "none"), New — целевое. Output заполняет вызывающий
// (вывод скрипта XKeen); подтверждение его не формирует.
type SwitchResult struct {
	Outcome    SwitchOutcome `json:"outcome"`
	Old        string        `json:"old"`
	New        string        `json:"new"`
	OldRunning bool          `json:"old_running"`
	NewRunning bool          `json:"new_running"`
	Output     string        `json:"output"`
}

// KernelSwitcher подтверждает переключение ядра по процессам. Фоновых горутин
// нет: Await синхронно опрашивает состояния, поэтому это не фоновый сервис.
type KernelSwitcher struct {
	states   func() []KernelProcessState
	now      func() time.Time
	sleep    func(time.Duration)
	deadline time.Duration
	interval time.Duration
}

// NewKernelSwitcher — боевой подтверждатель: свежее чтение /proc (без кэша
// ActiveState), настоящие часы, значения дедлайна и шага из переменных пакета.
func NewKernelSwitcher(kernels *KernelService) *KernelSwitcher {
	return &KernelSwitcher{
		states:   kernels.ProcessStates,
		now:      time.Now,
		sleep:    time.Sleep,
		deadline: switchConfirmDeadline,
		interval: switchConfirmInterval,
	}
}

// NewKernelSwitcherFunc — шов тестов: состояния и часы подменяются.
func NewKernelSwitcherFunc(states func() []KernelProcessState, now func() time.Time, sleep func(time.Duration), deadline, interval time.Duration) *KernelSwitcher {
	return &KernelSwitcher{states: states, now: now, sleep: sleep, deadline: deadline, interval: interval}
}

// Await опрашивает процессы до подтверждения переключения old -> target или до
// дедлайна. Старое ядро отслеживается, только если это xray/mihomo и оно не равно
// target (переключение «из ничего» и «на себя» подтверждается одним новым
// ядром). Принудительно процессы не завершаются (D-04): незавершённое
// переключение возвращается исходом, решение о дальнейшем принимает пользователь.
func (k *KernelSwitcher) Await(old, target string) SwitchResult {
	trackOld := (old == "xray" || old == "mihomo") && old != target
	res := SwitchResult{Old: old, New: target}

	start := k.now()
	for {
		newRunning, oldRunning := false, false
		for _, st := range k.states() {
			if st.Status != "running" {
				continue
			}
			switch st.Name {
			case target:
				newRunning = true
			case old:
				if trackOld {
					oldRunning = true
				}
			}
		}
		res.NewRunning, res.OldRunning = newRunning, oldRunning

		if newRunning && !oldRunning {
			res.Outcome = SwitchSwitched
			return res
		}
		if k.now().Sub(start) >= k.deadline {
			break
		}
		k.sleep(k.interval)
	}

	if res.NewRunning {
		res.Outcome = SwitchOldStillRunning
	} else {
		res.Outcome = SwitchNewNotStarted
	}
	return res
}
