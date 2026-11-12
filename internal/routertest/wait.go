//go:build router

package routertest

// Общие помощники ожидания роутерных тестов: одно условие, один верхний предел и
// описание последнего наблюдения в сообщении о провале. Фиксированные паузы «на
// глаз» в тестах не нужны: ожидание заканчивается, как только система готова, а
// предел берётся из констант продукта или теста.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Poll вызывает check сразу и затем каждые every, пока check не вернёт true или не
// истечёт limit. Последняя проверка идёт на самом пределе. Возвращает признак
// успеха и последнее наблюдение check (для сообщения о провале).
func Poll(limit, every time.Duration, check func() (bool, string)) (bool, string) {
	deadline := time.Now().Add(limit)
	for {
		ok, seen := check()
		if ok {
			return true, seen
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false, seen
		}
		if left > every {
			left = every
		}
		time.Sleep(left)
	}
}

// WaitCond ждёт, пока условие станет истинным, не дольше limit; не дождался — провал
// теста с именем условия what.
func WaitCond(t testing.TB, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	ok, _ := Poll(limit, 200*time.Millisecond, func() (bool, string) { return cond(), "" })
	if !ok {
		t.Fatalf("не дождались: %s (%s)", what, limit)
	}
}

// kernelReadyStreak — сколько опросов подряд ядро должно держаться одним процессом с
// одним PID: процесс, уже уходящий после перезапуска, или ядро соседнего теста,
// ещё не вышедшее, за один опрос не отличить от готового.
const kernelReadyStreak = 3

// WaitKernelReady ждёт, пока работает ровно один процесс ядра и это kernel (xray или
// mihomo): процессов kernel ровно один, процессов другого ядра нет, PID один и тот
// же kernelReadyStreak опросов подряд (опрос раз в секунду, pidof). Вердикт — процессы
// в /proc, независимо от панели. Короткие вспомогательные запуски бинарника ядра
// (проверка конфигурации, запрос версии) ядром не считаются. Не дождался за limit —
// провал теста с последним наблюдением. Возвращает PID ядра.
//
// Ядро передаётся строкой из теста, размеченного routertest.CoreSpecific: помощник
// сам ядро цели не берёт.
func WaitKernelReady(t testing.TB, kernel string, limit time.Duration) int {
	t.Helper()
	var other string
	switch kernel {
	case "xray":
		other = "mihomo"
	case "mihomo":
		other = "xray"
	default:
		t.Fatalf("WaitKernelReady: неизвестное ядро %q", kernel)
	}
	pid, streak := 0, 0
	ok, seen := Poll(limit, time.Second, func() (bool, string) {
		own, ownOut := kernelPIDs(kernel)
		alien, alienOut := kernelPIDs(other)
		procs := map[string]string{
			kernel: fmt.Sprintf("%d (pidof: %q)", len(own), ownOut),
			other:  fmt.Sprintf("%d (pidof: %q)", len(alien), alienOut),
		}
		seen := fmt.Sprintf("процессы xray %s, mihomo %s", procs["xray"], procs["mihomo"])
		if len(own) != 1 || len(alien) != 0 {
			pid, streak = 0, 0
			return false, seen
		}
		if own[0] != pid {
			pid, streak = own[0], 0
		}
		streak++
		return streak >= kernelReadyStreak, seen
	})
	if !ok {
		t.Fatalf("ядро %s не готово за %v: %s", kernel, limit, seen)
	}
	return pid
}

// kernelPIDs — PID процессов ядра name по pidof без вспомогательных запусков
// бинарника; out — вывод pidof для сообщений.
func kernelPIDs(name string) (pids []int, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, code := Exec(ctx, "pidof", name)
	out = strings.TrimSpace(stdout + " " + stderr)
	if code != 0 {
		return nil, out
	}
	for _, f := range strings.Fields(stdout) {
		if n, err := strconv.Atoi(f); err == nil && n > 0 && !kernelHelperProcess(n, name) {
			pids = append(pids, n)
		}
	}
	return pids, out
}

// kernelHelperProcess — короткоживущий вспомогательный запуск бинарника ядра, а не
// само ядро: проверка конфигурации («-test»/«-t») и запрос версии, которые панель и
// XKeen запускают рядом с перезапуском. Различается по аргументам из
// /proc/<pid>/cmdline; процесс, исчезнувший к моменту чтения, и процесс, ещё не
// сменивший образ после fork (argv[0] не бинарник ядра), тоже не считаются ядром.
func kernelHelperProcess(pid int, name string) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return true
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if filepath.Base(args[0]) != name {
		return true
	}
	for _, a := range args[1:] {
		switch a {
		case "-test", "--test", "-t", "version", "-v", "-V", "--version":
			return true
		}
	}
	return false
}
