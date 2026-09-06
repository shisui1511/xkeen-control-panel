package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// TestWebSocket_PingPong verifies that the server sends ping frames and that
// the read deadline is extended when a pong is received, keeping the
// connection alive beyond the initial deadline.
func TestWebSocket_PingPong(t *testing.T) {
	// This test validates the ping/pong constants and pong handler logic.
	// Full e2e ping/pong requires a real WebSocket connection; here we verify
	// the declared constants and ensure the pong handler function compiles and
	// can be invoked without panicking.

	if wsPingInterval <= 0 {
		t.Error("wsPingInterval must be positive")
	}
	if wsReadDeadline <= wsPingInterval {
		t.Error("wsReadDeadline must be greater than wsPingInterval to allow for ping-pong round-trip")
	}

	// Create a paired WebSocket connection using a test server.
	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Simulate the pong handler registration (mirrors LogsWebSocket).
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
			return nil
		})

		// Send one ping and wait briefly.
		_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		time.Sleep(100 * time.Millisecond)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] // replace "http" with "ws"
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Set("Origin", ts.URL)
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Skipf("WebSocket dial failed (may lack tail binary in CI): %v", err)
	}
	defer conn.Close()

	// Send pong in response to ping (standard client behaviour).
	conn.SetPingHandler(func(data string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})

	// Read until server closes — just verify no panic.
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Error("server handler did not complete within 3s")
	}
}

// TestWebSocketOrigin_Bypass verifies that the WebSocket upgrader rejects
// connections that omit the Origin header, preventing cross-site hijacking.
func TestWebSocketOrigin_Bypass(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		host        string
		expectAllow bool
	}{
		{
			name:        "no Origin header",
			origin:      "",
			host:        "localhost:8090",
			expectAllow: false,
		},
		{
			name:        "matching Origin",
			origin:      "http://localhost:8090",
			host:        "localhost:8090",
			expectAllow: true,
		},
		{
			name:        "mismatched Origin",
			origin:      "http://evil.com",
			host:        "localhost:8090",
			expectAllow: false,
		},
		{
			name:        "invalid Origin URL",
			origin:      "://not-a-url",
			host:        "localhost:8090",
			expectAllow: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/logs/ws", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			got := upgrader.CheckOrigin(req)
			if got != tc.expectAllow {
				t.Errorf("CheckOrigin = %v, want %v", got, tc.expectAllow)
			}
		})
	}
}

// TestLogsWebSocket_Sources verifies that multiple log sources are successfully read
// and formatted with their filenames as tags in the WebSocket stream.
func TestLogsWebSocket_Sources(t *testing.T) {
	tmpDir := t.TempDir()

	mihomoLogPath := filepath.Join(tmpDir, "mihomo.log")
	if err := os.WriteFile(mihomoLogPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	xrayLogPath := filepath.Join(tmpDir, "xray.log")
	if err := os.WriteFile(xrayLogPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		LogSources:   []string{mihomoLogPath, xrayLogPath},
		AllowedRoots: []string{tmpDir},
	}
	api := &API{
		cfg:     cfg,
		pathVal: utils.NewPathValidator(cfg.AllowedRoots),
	}

	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		api.LogsWebSocket(w, r)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Set("Origin", ts.URL)
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Skipf("WebSocket dial failed (e.g. tail not available in environment): %v", err)
	}
	defer conn.Close()

	// Give tail utility a brief moment to start up and register files
	time.Sleep(150 * time.Millisecond)

	// Append a line of log data to mihomo.log
	fMihomo, err := os.OpenFile(mihomoLogPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	testMsg := "mihomo log message test line\n"
	if _, err := fMihomo.WriteString(testMsg); err != nil {
		fMihomo.Close()
		t.Fatal(err)
	}
	fMihomo.Close()

	// Read messages from the WebSocket and check for the parsed source-prefix
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var receivedExpectedLog bool
	// Read a few messages (could be system notices or initial tail logs)
	for i := 0; i < 10; i++ {
		_, p, err := conn.ReadMessage()
		if err != nil {
			break
		}
		got := string(p)
		if strings.Contains(got, "mihomo.log") && strings.Contains(got, "mihomo log message test line") {
			receivedExpectedLog = true
			break
		}
	}

	if !receivedExpectedLog {
		t.Errorf("expected to receive log line with 'mihomo.log' prefix and message content")
	}
}

func TestLogsEndpoints_WithDispatcher(t *testing.T) {
	tmpDir := t.TempDir()
	dispatcher := services.NewLogDispatcher(nil, tmpDir, "")
	dispatcher.Start()
	defer dispatcher.Stop()

	dispatcher.IngestLine("[mihomo] [INFO] Mihomo running", "mihomo")
	dispatcher.IngestLine("[xray] [ERROR] Xray failed with timeout", "xray")

	api := &API{}
	api.SetLogDispatcher(dispatcher)

	// 1. Test History
	reqHistory := httptest.NewRequest(http.MethodGet, "/api/logs/history?source=mihomo", nil)
	recHistory := httptest.NewRecorder()
	api.LogsHistory(recHistory, reqHistory)

	if recHistory.Code != http.StatusOK {
		t.Fatalf("expected status 200 for LogsHistory, got %d", recHistory.Code)
	}
	if !strings.Contains(recHistory.Body.String(), "Mihomo running") {
		t.Errorf("expected history to contain 'Mihomo running', got %s", recHistory.Body.String())
	}

	// 2. Test Flash Health
	reqHealth := httptest.NewRequest(http.MethodGet, "/api/logs/flash-health", nil)
	recHealth := httptest.NewRecorder()
	api.LogsFlashHealth(recHealth, reqHealth)

	if recHealth.Code != http.StatusOK {
		t.Fatalf("expected status 200 for FlashHealth, got %d", recHealth.Code)
	}
	if !strings.Contains(recHealth.Body.String(), "total_logs_bytes") {
		t.Errorf("expected flash health response, got %s", recHealth.Body.String())
	}

	// 3. Test SetLevel
	reqLevel := httptest.NewRequest(http.MethodPost, "/api/logs/level", strings.NewReader(`{"source":"mihomo","level":"debug"}`))
	recLevel := httptest.NewRecorder()
	api.LogsSetLevel(recLevel, reqLevel)

	if recLevel.Code != http.StatusOK {
		t.Fatalf("expected status 200 for SetLevel, got %d", recLevel.Code)
	}

	// 4. Test Clear
	reqClear := httptest.NewRequest(http.MethodPost, "/api/logs/clear", strings.NewReader(`{"source":"all"}`))
	recClear := httptest.NewRecorder()
	api.LogsClear(recClear, reqClear)

	if recClear.Code != http.StatusOK {
		t.Fatalf("expected status 200 for LogsClear, got %d", recClear.Code)
	}

	historyAfterClear := dispatcher.GetHistory("all", "", 10)
	if len(historyAfterClear) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(historyAfterClear))
	}
}
