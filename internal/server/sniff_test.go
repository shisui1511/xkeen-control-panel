package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/cert"
)

// newSniffTestServer поднимает sniffListener поверх TLS с самоподписанным
// сертификатом и http.Server, считающим вызовы обработчика — общий хелпер
// для всех тестов sniffListener (134-06, Task 2).
func newSniffTestServer(t *testing.T, peekTimeout time.Duration) (addr string, handlerCalls *int64, closeFn func()) {
	t.Helper()

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	if err := cert.GenerateSelfSigned(certPath, keyPath, []string{"127.0.0.1"}); err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}
	tlsCfg, err := cert.LoadOrGenerate(certPath, keyPath, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("failed to load cert: %v", err)
	}

	var calls int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	sl := newSniffListener(raw, peekTimeout)
	tlsListener := tls.NewListener(sl, tlsCfg)

	httpSrv := &http.Server{Handler: mux}
	go func() {
		_ = httpSrv.Serve(tlsListener)
	}()

	// Дать accept-циклу и http.Server время подняться.
	time.Sleep(20 * time.Millisecond)

	return raw.Addr().String(), &calls, func() {
		_ = httpSrv.Close()
	}
}

func TestSniffListener_TLSServesNormally(t *testing.T) {
	addr, calls, closeFn := newSniffTestServer(t, sniffPeekTimeout)
	defer closeFn()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // тестовый самоподписанный сертификат
		},
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatalf("TLS GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(calls) != 1 {
		t.Errorf("expected handler called once, got %d", atomic.LoadInt64(calls))
	}
}

func TestSniffListener_PlainHTTPGets308(t *testing.T) {
	addr, _, closeFn := newSniffTestServer(t, sniffPeekTimeout)
	defer closeFn()

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/settings?tab=1", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "192.168.1.1:" + port

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("plain HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("expected 308, got %d", resp.StatusCode)
	}
	want := "https://192.168.1.1:" + port + "/settings?tab=1"
	if got := resp.Header.Get("Location"); got != want {
		t.Errorf("expected Location %q, got %q", want, got)
	}
}

func TestSniffListener_PlainHTTPBodyNeverReachesHandler(t *testing.T) {
	addr, calls, closeFn := newSniffTestServer(t, sniffPeekTimeout)
	defer closeFn()

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 3 * time.Second,
	}

	body := strings.NewReader(`{"username":"admin","password":"super-secret"}`)
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/api/auth/login", body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "192.168.1.1:" + port
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("plain HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("expected 308, got %d", resp.StatusCode)
	}
	if atomic.LoadInt64(calls) != 0 {
		t.Errorf("expected handler never called, got %d calls", atomic.LoadInt64(calls))
	}
}

func TestSniffListener_SilentClientDoesNotBlockTLS(t *testing.T) {
	addr, _, closeFn := newSniffTestServer(t, 500*time.Millisecond)
	defer closeFn()

	// Голое TCP-соединение без единого байта.
	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to dial silent conn: %v", err)
	}
	defer silent.Close()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
			Timeout: 3 * time.Second,
		}
		resp, err := client.Get("https://" + addr + "/")
		if err != nil {
			done <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			done <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TLS request failed: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= 2*time.Second {
			t.Errorf("TLS request took too long: %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TLS request blocked by silent connection")
	}
}

func TestSniffListener_HostWithoutPortGetsPanelPort(t *testing.T) {
	addr, _, closeFn := newSniffTestServer(t, sniffPeekTimeout)
	defer closeFn()

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "router.lan"

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("plain HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	want := "https://router.lan:" + port + "/"
	if got := resp.Header.Get("Location"); got != want {
		t.Errorf("expected Location %q, got %q", want, got)
	}
}

func TestSniffListener_InvalidHostFallsBackToSocketAddr(t *testing.T) {
	addr, _, closeFn := newSniffTestServer(t, sniffPeekTimeout)
	defer closeFn()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Пишем request-line и невалидный Host (с пробелом) руками, минуя
	// net/http, у которого своя валидация заголовков.
	raw := "GET / HTTP/1.1\r\nHost: invalid host with space\r\n\r\n"
	if _, err := conn.Write([]byte(raw)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	respStr := string(buf[:n])

	if !strings.Contains(respStr, "308 Permanent Redirect") {
		t.Fatalf("expected 308 response, got: %s", respStr)
	}
	if !strings.Contains(respStr, "Location: https://127.0.0.1:") {
		t.Errorf("expected fallback to socket addr in Location, got: %s", respStr)
	}
}
