//go:build router

package geodata

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Сценарии запускателя `xkeen -ug` на устройстве: источник истины — настоящий
// XKeen, а не заготовленный вывод. Файл не использует помощников из обычных
// _test.go пакета (их нет: тесты пакета — только роутерные).

const (
	rtRunXKeenBin = "/opt/sbin/xkeen"
	rtRunDatDir   = "/opt/etc/xray/dat"
	rtRunConfig   = "/opt/etc/xkeen/xkeen.json"
)

// rtRunPid возвращает PID процесса с этим именем (пусто — процесса нет).
func rtRunPid(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, _, _ := routertest.Exec(ctx, "pidof", name)
	return strings.TrimSpace(stdout)
}

// rtRunWaitKernel ждёт, пока процесс ядра снова появится.
func rtRunWaitKernel(name string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if rtRunPid(name) != "" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

// rtRunExists — файл или симлинк (XKeen проверяет оба).
func rtRunExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// rtRunUserEntries читает .xkeen.xray.geodata[] из xkeen.json без панели.
func rtRunUserEntries(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(rtRunConfig)
	if err != nil {
		return nil
	}
	var root struct {
		XKeen struct {
			Xray struct {
				Geodata []struct {
					File string `json:"file"`
				} `json:"geodata"`
			} `json:"xray"`
		} `json:"xkeen"`
	}
	if text := strings.TrimSpace(services.StripJSONComments(string(raw))); text != "" {
		if err := json.Unmarshal([]byte(text), &root); err != nil {
			t.Logf("xkeen.json не разобран как JSON: %v", err)
			return nil
		}
	}
	var files []string
	for _, e := range root.XKeen.Xray.Geodata {
		if e.File != "" {
			files = append(files, e.File)
		}
	}
	return files
}

// TestRouterXKeenUpdateRun запускает настоящий `xkeen -ug` панельным запускателем
// (как cron, мимо панели), проверяет чистоту строк, разбор итога по файлам и то,
// что перезапущенное ядро поднялось. Ядро не переключается и не читается для выбора
// поведения, поэтому метки ядра нет; на обоих ядрах сценарий идёт в сессиях стенда.
func TestRouterXKeenUpdateRun(t *testing.T) {
	routertest.Current(t)
	if !rtRunExists(rtRunXKeenBin) {
		t.Skipf("нет %s: XKeen не установлен", rtRunXKeenBin)
	}

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "last-ug.log")

	t.Run("update", func(t *testing.T) {
		// Окно cron XKeen (05:30): запуск рядом с ним столкнулся бы с настоящим -ug.
		now := time.Now()
		if m := now.Hour()*60 + now.Minute(); m >= 5*60+20 && m < 5*60+45 {
			t.Skip("часы стенда 05:20-05:45: окно cron XKeen")
		}

		kernels := map[string]bool{}
		for _, name := range []string{"xray", "mihomo"} {
			if rtRunPid(name) != "" {
				kernels[name] = true
			}
		}

		displays := map[string]string{}
		var expected []string
		for _, s := range StandardSets {
			displays[s.Display] = s.File
			if rtRunExists(filepath.Join(rtRunDatDir, s.File)) || (s.Alias != "" && rtRunExists(filepath.Join(rtRunDatDir, s.Alias))) {
				expected = append(expected, s.File)
			}
		}
		for _, f := range rtRunUserEntries(t) {
			displays[f] = f
		}

		out := NewOutput(0)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		r := &Runner{Bin: rtRunXKeenBin}
		code, err := r.Run(ctx, []string{"-ug"}, out)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		t.Logf("xkeen -ug: код %d, строк в журнале %d", code, len(out.Lines()))

		lines := out.Lines()
		for i, l := range lines {
			if strings.ContainsAny(l, "\r\x1b") {
				t.Fatalf("строка %d содержит CR или ESC", i)
			}
		}

		parsed := ParseUGOutput(lines, displays)
		if parsed.RunError == RunErrNetwork {
			t.Skip("сеть стенда: XKeen не дошёл до GitHub, это не красный прогон")
		}
		if !parsed.Parsed {
			t.Fatalf("итог не разобран: формат вывода не узнан (код %d, строк %d)", code, len(lines))
		}
		for _, f := range expected {
			o, ok := parsed.Files[f]
			if !ok {
				t.Errorf("для файла %s, лежавшего до запуска, нет итога в разборе", f)
				continue
			}
			switch o.Status {
			case StatusUpdated, StatusInstalled, StatusKept, StatusFailed:
			default:
				t.Errorf("файл %s: неизвестный статус %q", f, o.Status)
			}
			t.Logf("файл %s: %s %s unverified=%v", f, o.Status, o.Reason, o.Unverified)
		}

		// Чтение по смещениям согласовано с построчным содержимым.
		text, next, truncated := out.Read(0)
		if truncated {
			t.Errorf("Read(0) на непереполненном буфере вернул truncated")
		}
		if int64(len(text)) != next {
			t.Errorf("Read(0): next=%d, длина текста %d", next, len(text))
		}
		if len(lines) > 0 && text != strings.Join(lines, "\n")+"\n" {
			t.Errorf("Read(0) и Lines расходятся")
		}
		if tail, n2, _ := out.Read(next); tail != "" || n2 != next {
			t.Errorf("Read(next) вернул лишнее: %d байт, next %d", len(tail), n2)
		}

		// Ядро, работавшее до запуска, снова работает.
		for name := range kernels {
			if !rtRunWaitKernel(name, 60*time.Second) {
				t.Errorf("ядро %s не поднялось в течение 60 с после -ug", name)
			}
		}

		// Журнал сохраняется и читается обратно.
		if err := out.Save(logPath); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if saved, err := os.ReadFile(logPath); err != nil || string(saved) != text {
			t.Errorf("сохранённый журнал не совпал с буфером (err=%v)", err)
		}
	})
	// Формат вывода не узнан: итог не придумывается, вывод не теряется.
	t.Run("unknown_format", func(t *testing.T) {
		out := NewOutput(0)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		code, err := (&Runner{Bin: rtRunXKeenBin}).Run(ctx, []string{"-v"}, out)
		if err != nil {
			t.Fatalf("Run -v: %v", err)
		}
		lines := out.Lines()
		if len(lines) == 0 {
			t.Fatalf("вывод xkeen -v не сохранён (код %d)", code)
		}
		parsed := ParseUGOutput(lines, map[string]string{})
		if parsed.Parsed || parsed.RunError != "" {
			t.Errorf("вывод версии принят за вывод -ug: Parsed=%v RunError=%q", parsed.Parsed, parsed.RunError)
		}
	})

	// Тот же настоящий журнал в буфере на 1 КиБ: начало вытесняется, смещения растут.
	t.Run("truncation", func(t *testing.T) {
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Skip("настоящий -ug в этом прогоне не выполнен: нет сохранённого журнала")
		}
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		if len(lines) == 0 || lines[0] == "" {
			t.Skip("журнал -ug пуст")
		}
		const limit = 1024
		o := NewOutput(limit)
		var prev int64
		var appended int
		for appended < 4*limit { // реальных строк может быть мало: повторяем журнал
			for _, l := range lines {
				o.Append(l)
				appended += len(l) + 1
				_, next, _ := o.Read(prev)
				if next < prev {
					t.Fatalf("смещение уменьшилось: %d -> %d", prev, next)
				}
				prev = next
			}
		}
		text, next, truncated := o.Read(0)
		if !truncated {
			t.Errorf("Read(0) после переполнения не вернул truncated")
		}
		if len(text) > limit {
			t.Errorf("буфер вышел за предел: %d > %d", len(text), limit)
		}
		if next != int64(appended) {
			t.Errorf("next=%d, записано %d байт", next, appended)
		}
		if tail, n2, tr := o.Read(next); tail != "" || n2 != next || tr {
			t.Errorf("Read(next): текст %d байт, next %d, truncated %v", len(tail), n2, tr)
		}
		if got := o.Lines(); len(got) == 0 || got[0] == "" {
			t.Errorf("после вытеснения буфер начинается не со строки")
		}
	})

	// Таймаут: процесс получает SIGTERM, Run возвращается быстро и с ошибкой.
	t.Run("timeout", func(t *testing.T) {
		sleepBin := "/bin/sleep"
		if !rtRunExists(sleepBin) {
			p, err := exec.LookPath("sleep")
			if err != nil {
				t.Skip("на стенде нет sleep")
			}
			sleepBin = p
		}
		before := rtRunSleepPids()
		start := time.Now()
		_, err := (&Runner{Bin: sleepBin, Timeout: time.Second}).Run(context.Background(), []string{"30"}, NewOutput(0))
		if elapsed := time.Since(start); elapsed > 35*time.Second {
			t.Errorf("Run вернулся через %s: дольше 35 с", elapsed)
		}
		if !errors.Is(err, ErrRunTimeout) {
			t.Errorf("ожидалась ErrRunTimeout, получено %v", err)
		}
		for pid := range rtRunSleepPids() {
			if !before[pid] {
				t.Errorf("процесс sleep (pid %s) пережил таймаут", pid)
			}
		}
	})
}

// rtRunSleepPids — PID процессов `sleep 30` по /proc.
func rtRunSleepPids() map[string]bool {
	pids := map[string]bool{}
	dirs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, f := range dirs {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fields := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(fields) == 2 && filepath.Base(fields[0]) == "sleep" && fields[1] == "30" {
			pids[filepath.Base(filepath.Dir(f))] = true
		}
	}
	return pids
}
