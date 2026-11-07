//go:build router

// router-area: dns

package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Старый переключатель перенаправления DNS XKeen (POST /api/service/dns-redirect)
// при включённом слое «Конфигурация» (D-34) на настоящей панели устройства. Прежде
// он выполнял `xkeen -dns on` с перезапуском XKeen и запускал остановленное ядро.
// Вердикт — факты устройства: процессы ядра (/proc) и строка proxy_dns
// init-скрипта XKeen. Стенд возвращается в t.Cleanup: строка proxy_dns прежняя,
// ядро запущено, как до теста.

const (
	rtDNSRedirectPath = "/api/service/dns-redirect"
	// rtDNSRedirectHold — сколько ядро должно оставаться остановленным после отказа.
	rtDNSRedirectHold = 20 * time.Second
	// rtDNSKernelStopWait — сколько ждать исчезновения процессов ядра после остановки.
	rtDNSKernelStopWait = 30 * time.Second
)

// rtDNSWaitKernelStopped ждёт, пока процессов ядра kernel не останется; не
// дождался — провал.
func rtDNSWaitKernelStopped(t *testing.T, kernel string) {
	t.Helper()
	deadline := time.Now().Add(rtDNSKernelStopWait)
	for {
		pids, out := rtRstKernelPIDs(kernel)
		if len(pids) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ядро %s за %v не остановилось: %s", kernel, rtDNSKernelStopWait, out)
		}
		time.Sleep(time.Second)
	}
}

// rtDNSRestoreKernelAndLine — t.Cleanup: строка proxy_dns равна before, ядро
// запущено; если строку пришлось менять при запущенном ядре, ядро перезапускается,
// чтобы XKeen перестроил правила по строке.
func rtDNSRestoreKernelAndLine(t *testing.T, e *rtSrc, kernel string, before bool) {
	t.Helper()
	t.Cleanup(func() {
		c := e.with(t)
		changed := rtDNSProxyOn(t) != before
		if changed {
			arg := "off"
			if before {
				arg = "on"
			}
			rtDNSXKeen(t, "-dns", arg)
		}
		pids, _ := rtRstKernelPIDs(kernel)
		switch {
		case len(pids) == 0:
			c.rtDNSServiceControl("start")
		case changed:
			c.rtDNSServiceControl("restart")
		default:
			return
		}
		rtCoresLiveWaitStatus(t)
	})
}

// Трасса: слой включён → ядро остановлено через API служб → POST
// /api/service/dns-redirect {enabled:true} → 409 legacy_blocked, через 20 с ядро
// всё ещё остановлено, строка proxy_dns прежняя. Прежний обработчик отвечал
// перезапуском XKeen и поднимал ядро — провал с деталью, которую ищет проход
// воспроизведения на прежних файлах.
// Метка ядра: сценарий останавливает активное ядро.
func TestRouterDNSLegacyEndpointBlocked(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	if pids, out := rtRstKernelPIDs(kernel); len(pids) == 0 {
		t.Fatalf("до сценария ядро %s не запущено: %s", kernel, out)
	}
	proxyBefore := rtDNSProxyOn(t)
	rtDNSRestoreKernelAndLine(t, e, kernel, proxyBefore)

	e.rtDNSServiceControl("stop")
	rtDNSWaitKernelStopped(t, kernel)

	sent := time.Now()
	r := e.callSlow(http.MethodPost, rtDNSRedirectPath, map[string]bool{"enabled": true})
	if left := rtDNSRedirectHold - time.Since(sent); left > 0 {
		time.Sleep(left)
	}
	pids, _ := rtRstKernelPIDs(kernel)
	running := len(pids) > 0
	if r.Status != http.StatusConflict || r.Code != "legacy_blocked" || running {
		t.Fatalf("dns_redirect_not_blocked: status %d, kernel running %v", r.Status, running)
	}
	if got := rtDNSProxyOn(t); got != proxyBefore {
		t.Errorf("proxy_dns после отказа %v, до запроса был %v", got, proxyBefore)
	}
}
