//go:build router

package handlers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Клиент, ушедший посреди потокового ответа Clash API через панель (/traffic — строка
// раз в секунду), обрывает копирование ответа в httputil.ReverseProxy, и прокси
// паникует значением http.ErrAbortHandler, чтобы сервер закрыл соединение. Это штатный
// обрыв (страница закрыта, вкладка ушла), а не сбой: строки «Panic recovered» в журнале
// панели быть не должно — смоук считает её паникой.

// rtAbortStreams — сколько потоков открыть и бросить.
const rtAbortStreams = 5

func TestRouterMihomoProxyClientAbort(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	if tg.Core != "mihomo" {
		t.Skip("ядро ветви не Mihomo: поток Clash API через панель не открывается")
	}
	logPath := config.Default().XCPLogPath
	if logPath == "" {
		logPath = "/opt/var/log/xcp.log"
	}
	before, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("журнал панели %s: %v", logPath, err)
	}
	p := routertest.NewPanel(t)

	for i := 1; i <= rtAbortStreams; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		resp, err := p.Stream(ctx, "/api/mihomo/proxy/traffic")
		if err != nil {
			cancel()
			t.Fatalf("поток %d: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			cancel()
			resp.Body.Close()
			t.Fatalf("поток %d: http %d", i, resp.StatusCode)
		}
		// Первая строка пришла: прокси уже копирует ответ, обрыв попадёт в копирование.
		if _, err := resp.Body.Read(make([]byte, 1)); err != nil {
			cancel()
			resp.Body.Close()
			t.Fatalf("поток %d: первая строка: %v", i, err)
		}
		cancel()
		resp.Body.Close()
	}
	// Копирование замечает обрыв на следующей строке ядра (раз в секунду).
	time.Sleep(3 * time.Second)

	window, err := rtReadLogFrom(logPath, before.Size())
	if err != nil {
		t.Fatalf("журнал панели с позиции %d: %v", before.Size(), err)
	}
	if n := strings.Count(window, "Panic recovered"); n > 0 {
		t.Errorf("обрыв потока клиентом записан как panic: строк %d", n)
	}
}
