package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Вспомогательные запуски бинарника ядра (версия, проверка конфигурации,
// конвертация правил) согласуются с операциями XKeen над ядром.
//
// XKeen считает ядро запущенным по `pidof <ядро>`: процесс `mihomo -v` или
// `xray run -test`, который панель держит в момент, когда init-скрипт XKeen внутри
// start/restart проверяет, запущено ли ядро, принимается за само ядро. Тогда
// `xkeen -restart` останавливает ядро и тихо выходит, не запустив его заново
// («уже запущен» в выводе, который при отвязанном запуске никуда не попадает), а
// `xkeen -stop` своим killall обрывает саму проверку. Поэтому вспомогательный
// запуск не начинается, пока идёт start/stop/restart/переключение XKeen, а
// операция XKeen не начинается, пока вспомогательный запуск не закончился.
//
// `xkeen -restart` без терминала отвязывается в фон (start-stop-daemon) и
// возвращается сразу, поэтому одного замка вокруг его вызова мало: «операция
// идёт», пока в /proc есть процесс XKeen с командой жизненного цикла.

// kernelExecGate: вспомогательные запуски держат RLock на время работы
// бинарника, вызов команды жизненного цикла XKeen — Lock.
var kernelExecGate sync.RWMutex

// Сколько вспомогательный запуск ждёт конца операции XKeen и как часто проверяет.
// На mipsle restart с подготовкой правил идёт десятки секунд; дольше ждать нельзя:
// за пределом запуск идёт как раньше, без согласования.
var (
	kernelHelperWait = 2 * time.Minute
	kernelHelperPoll = 250 * time.Millisecond
)

// xkeenLifecycleMaxAge — процесс XKeen старше этого не считается идущей операцией:
// так ведут себя фоновые подоболочки init-скрипта (контроль файловых дескрипторов
// при check_fd="on" живёт столько же, сколько ядро, с той же командной строкой),
// и зависший скрипт не должен навсегда останавливать проверки панели.
var xkeenLifecycleMaxAge = 3 * time.Minute

// xkeenFDMonitorPIDFile — PID подоболочки контроля файловых дескрипторов XKeen.
var xkeenFDMonitorPIDFile = "/var/run/xkeen_fd.pid"

// xkeenLifecycleArgs — аргументы, по которым процесс XKeen меняет состояние ядра:
// ключи xkeen и команды init-скрипта S05xkeen.
var xkeenLifecycleArgs = map[string]bool{
	"-start": true, "-stop": true, "-restart": true, "-xray": true, "-mihomo": true,
	"start": true, "stop": true, "restart": true, "cold_start": true,
}

// xkeenLifecycleRunning — есть ли сейчас процесс XKeen (xkeen, S05xkeen или
// start-stop-daemon, отвязывающий xkeen) с командой жизненного цикла ядра.
func xkeenLifecycleRunning() bool {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false
	}
	fdMonitor := ""
	if raw, err := os.ReadFile(xkeenFDMonitorPIDFile); err == nil {
		fdMonitor = strings.TrimSpace(string(raw))
	}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' || name == fdMonitor {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(procDir, name, "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		if !isXKeenLifecycleCmdline(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")) {
			continue
		}
		if age, ok := procAge(name); ok && age > xkeenLifecycleMaxAge {
			continue
		}
		return true
	}
	return false
}

// isXKeenLifecycleCmdline — командная строка процесса XKeen, который запускает,
// останавливает или перезапускает ядро.
func isXKeenLifecycleCmdline(args []string) bool {
	xkeen, lifecycle := false, false
	for _, a := range args {
		switch filepath.Base(a) {
		case "xkeen", "S05xkeen":
			xkeen = true
		}
		if xkeenLifecycleArgs[a] {
			lifecycle = true
		}
	}
	return xkeen && lifecycle
}

// AcquireKernelHelper ждёт конца операции XKeen над ядром (не дольше
// kernelHelperWait и отмены ctx) и возвращает функцию освобождения: её вызывают,
// когда вспомогательный процесс ядра завершился.
func AcquireKernelHelper(ctx context.Context) (release func()) {
	deadline := time.Now().Add(kernelHelperWait)
	for {
		kernelExecGate.RLock()
		if !xkeenLifecycleRunning() || ctx.Err() != nil || !time.Now().Before(deadline) {
			return kernelExecGate.RUnlock
		}
		kernelExecGate.RUnlock()
		select {
		case <-ctx.Done():
		case <-time.After(kernelHelperPoll):
		}
	}
}

// TryAcquireKernelHelper — то же без ожидания: ok=false, если операция XKeen идёт.
func TryAcquireKernelHelper() (release func(), ok bool) {
	if !kernelExecGate.TryRLock() {
		return nil, false
	}
	if xkeenLifecycleRunning() {
		kernelExecGate.RUnlock()
		return nil, false
	}
	return kernelExecGate.RUnlock, true
}

// lockKernelLifecycleExec берёт замок на вызов команды жизненного цикла XKeen:
// ждёт конца идущих вспомогательных запусков и не даёт начаться новым, пока
// команда не вернулась (дальше их держит процесс XKeen в /proc).
func lockKernelLifecycleExec() (unlock func()) {
	kernelExecGate.Lock()
	return kernelExecGate.Unlock
}
