//go:build router

package routertest

// Общие помощники ожидания роутерных тестов: одно условие, один верхний предел и
// описание последнего наблюдения в сообщении о провале. Фиксированные паузы «на
// глаз» в тестах не нужны: ожидание заканчивается, как только система готова, а
// предел берётся из констант продукта или теста.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// storageQuietBytes — порог тишины носителя: грязных и записываемых данных в кэше
// страниц (Dirty + Writeback) меньше 1 МБ.
const storageQuietBytes = 1 << 20

// storageProbeDir — каталог данных панели на устройстве, где проверяется запись с fsync
// (значение config.Default().DataDir; пакет config здесь не импортируется: цикл импортов
// с тестами пакетов, которые подключают routertest).
const storageProbeDir = "/opt/etc/xcp"

// WaitStorageQuiet ждёт (не дольше limit), пока в кэше страниц почти не останется
// грязных и записываемых данных (Dirty + Writeback из /proc/meminfo меньше 1 МБ).
// Возвращает, достигнута ли тишина до предела, и описание состояния носителя для
// журнала теста: счётчики, время ожидания и задержку записи 4 КБ с fsync в каталоге
// данных панели. С limit 0 — одно наблюдение без ожидания.
func WaitStorageQuiet(t testing.TB, limit time.Duration) (bool, string) {
	t.Helper()
	start := time.Now()
	for {
		dirty, wb := meminfo(t, "Dirty"), meminfo(t, "Writeback")
		quiet := dirty+wb < storageQuietBytes
		if quiet || time.Since(start) > limit {
			return quiet, "носитель: грязных " + strconv.FormatInt(dirty>>10, 10) + " КБ, в записи " +
				strconv.FormatInt(wb>>10, 10) + " КБ (ожидание " + time.Since(start).Round(100*time.Millisecond).String() +
				"), запись 4 КБ с fsync " + fsyncProbe(storageProbeDir).Round(time.Millisecond).String()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// meminfo возвращает поле /proc/meminfo в байтах.
func meminfo(t testing.TB, field string) int64 {
	t.Helper()
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		t.Fatalf("/proc/meminfo: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || name != field {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			t.Fatalf("/proc/meminfo %s: %v", field, err)
		}
		return kb << 10
	}
	t.Fatalf("/proc/meminfo: нет поля %s", field)
	return 0
}

// fsyncProbe пишет 4 КБ в каталог dir с fsync и возвращает, сколько это заняло: на
// носителе, где идёт фиксация журнала ext4, мелкая запись с fsync ждёт её конца, поэтому
// задержка показывает застой, которого нет в счётчиках Dirty и Writeback. Ошибка — -1.
func fsyncProbe(dir string) time.Duration {
	f, err := os.CreateTemp(dir, "rt-fsync-*")
	if err != nil {
		return -1
	}
	defer os.Remove(f.Name())
	defer f.Close()
	start := time.Now()
	if _, err := f.Write(make([]byte, 4096)); err != nil {
		return -1
	}
	if err := f.Sync(); err != nil {
		return -1
	}
	return time.Since(start)
}

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
