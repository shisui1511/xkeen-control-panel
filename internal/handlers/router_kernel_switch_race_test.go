//go:build router

package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Переключение ядра сразу после перезапуска. `xkeen -restart` без терминала
// отвязывается в фон и возвращается сразу, а команда жизненного цикла держала
// замок только на время своего вызова: переключение на другое ядро запускало
// второй init-скрипт XKeen, пока фоновый перезапуск ещё работал. В гейте
// 20261020-232103-3aabae6e (обновление подписки перезапустило Xray, через 13 с
// переключение на Mihomo) работали оба ядра, панель упиралась в конфликт ядер, а
// восстановление стенда — в «ядро не остановилось после xkeen -stop»; в сессии на
// стенде то же давало ядро-сироту, которого XKeen не видит («Прокси-клиент не
// запущен»), и каждое следующее переключение — new_not_started. Вердикт — по /proc
// и `xkeen -status`: после переключения работает ровно одно ядро, целевое, XKeen
// видит его запущенным, и оно держится с одним PID.

// rtSwitchRaceWait — сколько ждать итога переключения: фоновый перезапуск, затем
// само переключение.
const rtSwitchRaceWait = 3 * time.Minute

var rtSwitchRaceANSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// rtSwitchRaceStatus — вывод `xkeen -status` без цветовых кодов.
func rtSwitchRaceStatus() string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, _ := routertest.Exec(ctx, config.Default().XKeenBinary, "-status")
	return strings.TrimSpace(rtSwitchRaceANSI.ReplaceAllString(stdout+" "+stderr, ""))
}

// rtSwitchRaceSettled ждёт (не дольше wait), пока работает ровно одно ядро want, ни
// одного other, и XKeen видит запущенным именно want; возвращает PID want или 0 и
// последнее наблюдение.
func rtSwitchRaceSettled(want, other string, wait time.Duration) (int, string) {
	deadline := time.Now().Add(wait)
	var last string
	for {
		w, wOut := rtRstKernelPIDs(want)
		o, oOut := rtRstKernelPIDs(other)
		st := rtSwitchRaceStatus()
		if len(w) == 1 && len(o) == 0 && services.IsKernelStatusHealthy(st) && strings.Contains(st, want) {
			return w[0], ""
		}
		last = want + ": [" + wOut + "], " + other + ": [" + oOut + "], xkeen -status: " + st
		if !time.Now().Before(deadline) {
			return 0, last
		}
		time.Sleep(time.Second)
	}
}

// rtSwitchRaceKillOrphans добивает процессы ядер, оставшиеся после остановки через
// XKeen: ядро, которого XKeen не видит, `xkeen -stop` не трогает, а оно держит порты
// и не даёт подняться следующему.
func rtSwitchRaceKillOrphans(t *testing.T) {
	t.Helper()
	signaled := false
	for _, name := range []string{"xray", "mihomo"} {
		pids, _ := rtRstKernelPIDs(name)
		for _, pid := range pids {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _, _ = routertest.Exec(ctx, "kill", strconv.Itoa(pid))
			cancel()
			signaled = true
			t.Logf("уборка: процесс %s %d остался после остановки через XKeen, остановлен сигналом", name, pid)
		}
	}
	if !signaled {
		return
	}
	// Сигнал отправлен: ждём, пока процессы исчезнут (шаг 200 мс, предел 10 с).
	var left []string
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		left = left[:0]
		for _, name := range []string{"xray", "mihomo"} {
			pids, _ := rtRstKernelPIDs(name)
			for _, pid := range pids {
				left = append(left, name+" "+strconv.Itoa(pid))
			}
		}
		if len(left) == 0 || !time.Now().Before(end) {
			break
		}
	}
	if len(left) != 0 {
		t.Logf("уборка: за 10 с не исчезли процессы: %s", strings.Join(left, ", "))
	}
}

func TestRouterSwitchKernelDuringDetachedRestart(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	other := map[string]string{"xray": "mihomo", "mihomo": "xray"}[kernel]
	if other == "" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	if _, last := rtSwitchRaceSettled(kernel, other, 30*time.Second); last != "" {
		t.Fatalf("до сценария ядро ветви %s не в рабочем состоянии: %s", kernel, last)
	}
	p := routertest.NewPanel(t)

	// Стенд возвращается к ядру ветви при любом исходе: остановка через панель,
	// сироты — сигналом, затем переключение на прежнее ядро (или запуск, если клиент
	// XKeen уже он).
	t.Cleanup(func() {
		if _, last := rtSwitchRaceSettled(kernel, other, time.Second); last == "" {
			return
		}
		_, _, _ = p.CallSlow(http.MethodPost, "/api/service/control?action=stop", nil)
		rtSwitchRaceKillOrphans(t)
		_, _, _ = p.CallSlow(http.MethodPost, "/api/service/control?action=switch_kernel&kernel="+kernel, nil)
		if _, last := rtSwitchRaceSettled(kernel, other, 30*time.Second); last == "" {
			return
		}
		_, _, _ = p.CallSlow(http.MethodPost, "/api/service/control?action=start", nil)
		if _, last := rtSwitchRaceSettled(kernel, other, rtSwitchRaceWait); last != "" {
			t.Errorf("уборка: ядро %s не вернулось за %v: %s", kernel, rtSwitchRaceWait, last)
		}
	})

	status, raw, err := p.Call(http.MethodPost, "/api/service/control?action=restart", nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("перезапуск %s: %d %v %.300s", kernel, status, err, raw)
	}
	status, raw, err = p.CallSlow(http.MethodPost, "/api/service/control?action=switch_kernel&kernel="+other, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("переключение на %s сразу после перезапуска %s: %d %v %.300s", other, kernel, status, err, raw)
	}
	pid, last := rtSwitchRaceSettled(other, kernel, rtSwitchRaceWait)
	if pid == 0 {
		t.Fatalf("после переключения на %s во время фонового перезапуска %s за %v нет ровно одного ядра %s, видимого XKeen: %s",
			other, kernel, rtSwitchRaceWait, other, last)
	}
	rtRstHoldPID(t, other, pid)
	if o, out := rtRstKernelPIDs(kernel); len(o) != 0 {
		t.Fatalf("ядро %s поднялось после переключения на %s: %s", kernel, other, out)
	}
}
