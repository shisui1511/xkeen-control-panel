//go:build router

package handlers

import (
	"context"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Перезапуск настоящей панели устройства init-скриптом, пока открыты потоки SSE
// (вкладка «Конфигурация» и ход обновления). Остановка ждала потоки до дедлайна
// http.Server.Shutdown (10 с); init-скрипт Entware за это время объявлял остановку
// неудачной, запуск отвечал «already running», и после выхода старого процесса
// панель оставалась лежать (деплой на arm64, прогон 20261015-015433-24d29880).
// Вердикт — процессы xcp в /proc (pidof, независимо от панели), время выхода
// старого процесса и ответ новой панели по прежней сессии.

const (
	rtPanelInit = "/opt/etc/init.d/S99xcp"
	// rtPanelStopLimit — за сколько старый процесс обязан выйти после команды:
	// init-скрипт ждёт остановку около 10 с, затем запуск видит старый процесс.
	rtPanelStopLimit = 8 * time.Second
	// rtPanelUpTimeout — предел ожидания ответа новой панели (mipsle читает
	// бинарник с флешки и поднимается десятки секунд).
	rtPanelUpTimeout = 90 * time.Second
)

// rtPanelPIDs — PID процессов панели: только запуски с -config (init-скрипт и
// самообновление), без «xcp -v» и других коротких вызовов бинарника.
func rtPanelPIDs() (pids []int, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "pidof", "xcp")
	out = strings.TrimSpace(stdout + " " + stderr)
	if code != 0 {
		return nil, out
	}
	for _, f := range strings.Fields(stdout) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + f + "/cmdline")
		if err != nil {
			continue
		}
		for _, a := range strings.Split(string(raw), "\x00") {
			if a == "-config" {
				pids = append(pids, n)
				break
			}
		}
	}
	return pids, out
}

// rtPanelANSI — цветовые коды вывода init-скриптов Entware.
var rtPanelANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// rtPanelInitCmd запускает init-скрипт панели с чистым окружением: новая панель не
// должна унаследовать переменные теста (адрес подписки, сессия, каталоги).
// Возвращает вывод скрипта одной строкой без цветовых кодов.
func rtPanelInitCmd(ctx context.Context, action string) (string, int) {
	stdout, stderr, code := routertest.Exec(ctx, "/bin/sh", "-c",
		"exec env -i PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root "+rtPanelInit+" "+action)
	return strings.Join(strings.Fields(rtPanelANSI.ReplaceAllString(stdout+" "+stderr, "")), " "), code
}

// rtPanelWaitUp ждёт панель с PID, отличным от old, которая отвечает на
// /api/auth/me по прежней сессии. Возвращает PID или 0 и последнее наблюдение.
func rtPanelWaitUp(p *routertest.Panel, old int, limit time.Duration) (int, string) {
	deadline := time.Now().Add(limit)
	last := ""
	for time.Now().Before(deadline) {
		pids, out := rtPanelPIDs()
		switch {
		case len(pids) != 1:
			last = "процессов панели " + strconv.Itoa(len(pids)) + " (pidof: " + out + ")"
		case pids[0] == old:
			last = "работает старый процесс " + strconv.Itoa(old)
		default:
			code, err := p.GetJSON("/api/auth/me", nil)
			if err == nil && code == http.StatusOK {
				return pids[0], ""
			}
			last = "новый процесс " + strconv.Itoa(pids[0]) + ", /api/auth/me: http " + strconv.Itoa(code)
			if err != nil {
				last += " (" + err.Error() + ")"
			}
		}
		time.Sleep(time.Second)
	}
	return 0, last
}

// rtPanelOpenStream открывает поток и возвращает канал, который закрывается, когда
// панель его завершила (EOF или обрыв соединения).
func rtPanelOpenStream(t *testing.T, ctx context.Context, p *routertest.Panel, path string, required bool) <-chan struct{} {
	t.Helper()
	resp, err := p.Stream(ctx, path)
	if err != nil {
		t.Fatalf("поток %s не открылся: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if required {
			t.Fatalf("поток %s: http %d", path, resp.StatusCode)
		}
		t.Logf("поток %s: http %d — без него", path, resp.StatusCode)
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
	return done
}

func TestRouterPanelRestartWithOpenStreams(t *testing.T) {
	p := routertest.NewPanel(t)

	pids, out := rtPanelPIDs()
	if len(pids) != 1 {
		t.Fatalf("процессов панели: %d (pidof: %q), ожидался ровно один", len(pids), out)
	}
	old := pids[0]

	// Что бы ни случилось, следующие тесты получают работающую панель.
	t.Cleanup(func() {
		if pid, _ := rtPanelWaitUp(p, old, 5*time.Second); pid != 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if pids, _ := rtPanelPIDs(); len(pids) == 0 {
			_, _ = rtPanelInitCmd(ctx, "start")
		}
		if pid, last := rtPanelWaitUp(p, old, rtPanelUpTimeout); pid == 0 {
			t.Errorf("панель не поднялась после теста: %s", last)
		}
	})

	streamCtx, cancelStreams := context.WithCancel(context.Background())
	defer cancelStreams()
	updDone := rtPanelOpenStream(t, streamCtx, p, "/api/update/events", true)
	// Поток слоя есть, только когда слой «Конфигурация» включён.
	layerDone := rtPanelOpenStream(t, streamCtx, p, "/api/configlayer/events", false)
	// Поток должен быть установлен до команды: первое событие уже отдано.
	time.Sleep(2 * time.Second)

	start := time.Now()
	restartCtx, cancelRestart := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelRestart()
	type initResult struct {
		out  string
		code int
	}
	restarted := make(chan initResult, 1)
	go func() {
		o, c := rtPanelInitCmd(restartCtx, "restart")
		restarted <- initResult{o, c}
	}()

	// Выход старого процесса — по /proc, независимо от панели.
	var stopTook time.Duration
	for {
		if _, err := os.Stat("/proc/" + strconv.Itoa(old)); err != nil {
			stopTook = time.Since(start)
			break
		}
		if time.Since(start) > 30*time.Second {
			stopTook = -1
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	streamEnd := func(done <-chan struct{}) string {
		if done == nil {
			return "-"
		}
		select {
		case <-done:
			return "закрыт"
		case <-time.After(5 * time.Second):
			return "открыт"
		}
	}
	upd, layer := streamEnd(updDone), streamEnd(layerDone)
	res := <-restarted
	initOut := res.out

	if stopTook < 0 {
		t.Fatalf("старый процесс %d не вышел за 30 с после restart (потоки: обновление %s, слой %s; init: код %d, %q)",
			old, upd, layer, res.code, initOut)
	}
	pid, last := rtPanelWaitUp(p, old, rtPanelUpTimeout)
	t.Logf("старый процесс вышел за %.1f с; потоки: обновление %s, слой %s; init: код %d, %q; новый PID %d",
		stopTook.Seconds(), upd, layer, res.code, initOut, pid)
	if pid == 0 {
		t.Fatalf("после restart панели нет за %s: %s (init: код %d, %q)", rtPanelUpTimeout, last, res.code, initOut)
	}
	if stopTook > rtPanelStopLimit {
		t.Errorf("старый процесс выходил %.1f с при открытых потоках, предел %s", stopTook.Seconds(), rtPanelStopLimit)
	}
}
