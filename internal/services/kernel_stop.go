package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Переменные, а не константы: тесты подменяют их (с восстановлением через Cleanup).
var (
	// kernelStopWait — сколько ждать завершения ядра после SIGTERM.
	kernelStopWait = 10 * time.Second
	// kernelStopInterval — шаг проверки процессов во время ожидания.
	kernelStopInterval = 250 * time.Millisecond
	// signalProcess — отправка сигнала процессу; шов тестов (на роутере живой
	// сигнал уходит только в проверенный PID ядра).
	signalProcess = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
)

// KernelStopOutcome — исход остановки выбранного ядра.
type KernelStopOutcome string

const (
	// KernelStopStopped — процессов ядра больше нет.
	KernelStopStopped KernelStopOutcome = "stopped"
	// KernelStopNotRunning — ядро и не было запущено, сигналов не отправлено.
	KernelStopNotRunning KernelStopOutcome = "not_running"
	// KernelStopStillRunning — ядро не завершилось к дедлайну после SIGTERM.
	KernelStopStillRunning KernelStopOutcome = "still_running"
)

// Способ остановки в KernelStopResult.Method.
const (
	// KernelStopMethodSignal — SIGTERM по PID из /proc.
	KernelStopMethodSignal = "signal"
	// KernelStopMethodXKeen — штатный `xkeen -stop` (единственное запущенное ядро).
	KernelStopMethodXKeen = "xkeen"
)

// KernelStopResult — ответ «остановить конкретное ядро». Method выставляет
// вызывающий (обработчик): сервис знает только про сигналы.
type KernelStopResult struct {
	Kernel  string            `json:"kernel"`
	Outcome KernelStopOutcome `json:"outcome"`
	Method  string            `json:"method"`
}

// kernelPIDs — PID запущенных процессов ядра name (xray или mihomo) и базовое
// имя его бинарника. Имя проходит через белый список canonicalKernelName; PID от
// клиента сюда не попадает. Вспомогательные процессы (`mihomo convert-ruleset`,
// `xray -test`, …) не учитываются.
func (s *KernelService) kernelPIDs(name string) (pids []int, base string, err error) {
	name, err = canonicalKernelName(name)
	if err != nil {
		return nil, "", err
	}

	s.mu.Lock()
	k, ok := s.kernels[name]
	if !ok {
		s.mu.Unlock()
		return nil, "", fmt.Errorf("unknown kernel: %s", name)
	}
	s.resolveBinaryPath(k)
	base = filepath.Base(k.BinaryPath)
	fn := s.processStatesFn
	s.mu.Unlock()

	// Подмена источника состояний (тесты): PID берутся оттуда, перепроверка exe
	// по /proc выполняется всё равно перед сигналом.
	if fn != nil {
		for _, st := range fn() {
			if st.Name == name && st.Status == "running" && st.PID > 1 {
				pids = append(pids, st.PID)
			}
		}
		return pids, base, nil
	}

	matches, _ := filepath.Glob(filepath.Join(procDir, "*/exe"))
	for _, link := range matches {
		pidStr := filepath.Base(filepath.Dir(link))
		pid, convErr := strconv.Atoi(pidStr)
		if convErr != nil || pid <= 1 {
			continue
		}
		if isKernelProcess(pid, base) {
			pids = append(pids, pid)
		}
	}
	return pids, base, nil
}

// isKernelProcess — PID всё ещё процесс ядра: exe указывает на бинарник с базовым
// именем base (суффикс « (deleted)» отбрасывается, как при обнаружении ядра) и
// процесс не вспомогательный. PID ≤ 1 никогда не считается ядром: сигнал в 0 и
// отрицательные значения адресуется группам процессов.
func isKernelProcess(pid int, base string) bool {
	if pid <= 1 || base == "" {
		return false
	}
	pidStr := strconv.Itoa(pid)
	target, err := os.Readlink(filepath.Join(procDir, pidStr, "exe"))
	if err != nil {
		return false
	}
	target = strings.TrimSuffix(target, " (deleted)")
	if filepath.Base(target) != base {
		return false
	}
	return !isShortLivedOrHelperProcess(pidStr)
}

// StopKernelProcess останавливает процессы ядра name сигналом SIGTERM: PID
// определяются по /proc, перед сигналом каждый перепроверяется (exe всё ещё
// бинарник этого ядра — PID мог быть переиспользован), затем до kernelStopWait
// ожидается исчезновение процессов. Принудительное завершение не применяется
// (D-04): ядро, не завершившееся к дедлайну, возвращается исходом still_running.
// Метод остановки (Method) выставляет вызывающий.
func (s *KernelService) StopKernelProcess(name string) (KernelStopResult, error) {
	pids, base, err := s.kernelPIDs(name)
	if err != nil {
		return KernelStopResult{}, err
	}
	// По выходу кэш активного ядра не должен хранить прежний ответ.
	defer s.InvalidateActiveState()
	res := KernelStopResult{Kernel: name}
	if len(pids) == 0 {
		res.Outcome = KernelStopNotRunning
		return res, nil
	}

	for _, pid := range pids {
		if !isKernelProcess(pid, base) {
			continue
		}
		// ESRCH — процесс уже завершился между перепроверкой и сигналом, не ошибка.
		if sigErr := signalProcess(pid, syscall.SIGTERM); sigErr != nil && !errors.Is(sigErr, syscall.ESRCH) {
			return res, fmt.Errorf("signal %s (pid %d): %w", name, pid, sigErr)
		}
	}

	deadline := time.Now().Add(kernelStopWait)
	for {
		alive := false
		for _, pid := range pids {
			if isKernelProcess(pid, base) {
				alive = true
				break
			}
		}
		if !alive {
			res.Outcome = KernelStopStopped
			return res, nil
		}
		if !time.Now().Before(deadline) {
			res.Outcome = KernelStopStillRunning
			return res, nil
		}
		time.Sleep(kernelStopInterval)
	}
}
