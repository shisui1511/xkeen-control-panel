package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestTerminalWebSocket_ServiceUnavailable(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/ws", nil)
	rec := httptest.NewRecorder()

	api.TerminalWebSocket(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable when ptySvc is nil, got %d", rec.Code)
	}
}

func TestTerminalWebSocket_Streaming(t *testing.T) {
	ptySvc := services.NewPTYService()
	defer ptySvc.CloseAll()

	api := &API{
		cfg: config.Default(),
	}
	api.SetPTYService(ptySvc)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.TerminalWebSocket(w, r)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "?cols=80&rows=24"
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Set("Origin", ts.URL)

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send stdin command
	sendMsg := TerminalClientMessage{
		Type: "stdin",
		Data: "echo PTY_TEST_OK\n",
	}
	msgBytes, _ := json.Marshal(sendMsg)
	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// Read stream until PTY_TEST_OK is seen or timeout
	conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	found := false
	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if strings.Contains(string(p), "PTY_TEST_OK") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to receive PTY_TEST_OK in terminal output stream")
	}
}

func TestTerminalWebSocket_ResizeMessage(t *testing.T) {
	ptySvc := services.NewPTYService()
	defer ptySvc.CloseAll()

	api := &API{
		cfg: config.Default(),
	}
	api.SetPTYService(ptySvc)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.TerminalWebSocket(w, r)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Set("Origin", ts.URL)

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send resize message
	resizeMsg := TerminalClientMessage{
		Type: "resize",
		Cols: 120,
		Rows: 40,
	}
	msgBytes, _ := json.Marshal(resizeMsg)
	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		t.Fatalf("failed to send resize message: %v", err)
	}

	// Send ping message
	pingMsg := TerminalClientMessage{
		Type: "ping",
	}
	pingBytes, _ := json.Marshal(pingMsg)
	if err := conn.WriteMessage(websocket.TextMessage, pingBytes); err != nil {
		t.Fatalf("failed to send ping message: %v", err)
	}

	// Brief pause to ensure no crashes
	time.Sleep(50 * time.Millisecond)
}

func TestTerminalWebSocket_MaxSessions(t *testing.T) {
	ptySvc := services.NewPTYService()
	defer ptySvc.CloseAll()

	api := &API{
		cfg: config.Default(),
	}
	api.SetPTYService(ptySvc)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.TerminalWebSocket(w, r)
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	dialer := websocket.Dialer{}
	header := http.Header{}
	header.Set("Origin", ts.URL)

	// Session 1
	conn1, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect session 1: %v", err)
	}
	defer conn1.Close()

	// Session 2
	conn2, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect session 2: %v", err)
	}
	defer conn2.Close()

	// Wait for 2 active sessions
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ptySvc.ActiveSessionsCount() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count := ptySvc.ActiveSessionsCount(); count != 2 {
		t.Errorf("expected 2 active sessions, got %d", count)
	}

	// Session 3 should be rejected with policy violation or error message
	conn3, _, err := dialer.Dial(wsURL, header)
	if err == nil {
		// Read message, expect error or close
		conn3.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, p, readErr := conn3.ReadMessage()
		if readErr == nil {
			if !strings.Contains(string(p), "Maximum active terminal sessions") {
				t.Errorf("expected max sessions error message, got: %s", string(p))
			}
		}
		_ = conn3.Close()
	}

	// Close session 1
	_ = conn1.Close()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ptySvc.ActiveSessionsCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count := ptySvc.ActiveSessionsCount(); count != 1 {
		t.Errorf("expected 1 active session after closing conn1, got %d", count)
	}

	// Now session 4 should succeed
	conn4, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect session 4 after slot freed: %v", err)
	}
	defer conn4.Close()
}

// runXKeenInstallWS подключается к терминалу в режиме установки XKeen и
// возвращает весь вывод и код из кадра exit.
func runXKeenInstallWS(t *testing.T, api *API, query string) (string, int) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(api.TerminalWebSocket))
	defer ts.Close()
	header := http.Header{}
	header.Set("Origin", ts.URL)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+ts.URL[4:]+"?mode=xkeen-install&"+query, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	var out strings.Builder
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		msgType, p, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no exit frame, output so far: %q (%v)", out.String(), err)
		}
		if msgType == websocket.TextMessage {
			var msg struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			}
			if json.Unmarshal(p, &msg) == nil && msg.Type == "exit" {
				return out.String(), msg.Code
			}
		}
		out.Write(p)
	}
}

// TestTerminalWebSocket_XKeenInstall: установщик скачивается, запускается
// в PTY с флагом канала, его код завершения доходит до клиента.
func TestTerminalWebSocket_XKeenInstall(t *testing.T) {
	installer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\n# jameszeroX/XKeen\necho \"INSTALLER $1\"\nexit 7\n"))
	}))
	defer installer.Close()

	ptySvc := services.NewPTYService()
	defer ptySvc.CloseAll()
	api := &API{cfg: config.Default()}
	api.SetPTYService(ptySvc)
	api.SetXKeenInstaller(&services.XKeenInstaller{
		URL:     installer.URL + "/install.sh",
		Mirrors: []string{""},
		Client:  installer.Client(),
		Dir:     t.TempDir(),
		InitDir: t.TempDir(),
	})

	out, code := runXKeenInstallWS(t, api, "channel=beta")
	if !strings.Contains(out, "INSTALLER --beta") {
		t.Errorf("installer did not run with channel flag, output %q", out)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want installer's 7", code)
	}

	_, code = runXKeenInstallWS(t, api, "channel=nightly")
	if code == 0 {
		t.Error("unknown channel must fail")
	}
}

// TestTerminalWebSocket_XKeenInstallNoEntware: без Entware (ПК разработчика)
// установщик не скачивается и не запускается.
func TestTerminalWebSocket_XKeenInstallNoEntware(t *testing.T) {
	downloaded := false
	installer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloaded = true
	}))
	defer installer.Close()

	ptySvc := services.NewPTYService()
	defer ptySvc.CloseAll()
	api := &API{cfg: config.Default()}
	api.SetPTYService(ptySvc)
	api.SetXKeenInstaller(&services.XKeenInstaller{
		URL:     installer.URL + "/install.sh",
		Mirrors: []string{""},
		Client:  installer.Client(),
		Dir:     t.TempDir(),
		InitDir: "/nonexistent/init.d",
	})

	out, code := runXKeenInstallWS(t, api, "channel=stable")
	if code == 0 || !strings.Contains(out, "Entware") {
		t.Errorf("install without Entware must fail with a reason, got %d %q", code, out)
	}
	if downloaded {
		t.Error("installer must not be downloaded without Entware")
	}
}
