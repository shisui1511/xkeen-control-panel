//go:build router

package utils

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Защита SafeHTTPClient от SSRF на устройстве: на всех адресах слушает настоящий
// HTTP-сервер, и запросы к неуказанному адресу, петле и широковещательному адресу
// получают ErrPrivateNetwork, а не ответ локальной службы роутера.
func TestRouterSafeHTTPClientBlocksLocal(t *testing.T) {
	routertest.Current(t)
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("local"))
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	client := SafeHTTPClient(5 * time.Second)
	for _, host := range []string{"0.0.0.0", "0.1.2.3", "127.0.0.1", "[::]", "255.255.255.255"} {
		resp, err := client.Get("http://" + host + ":" + port + "/")
		if err == nil {
			_ = resp.Body.Close()
			t.Errorf("%s: запрос прошёл к локальной службе (статус %d)", host, resp.StatusCode)
			continue
		}
		if !errors.Is(err, ErrPrivateNetwork) {
			t.Errorf("%s: ошибка не ErrPrivateNetwork: %v", host, err)
		}
	}
}
