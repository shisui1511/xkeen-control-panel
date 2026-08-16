package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestMihomoProxy_UnixDomainSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "mihomo.sock")

	// Start a mock HTTP server on a Unix domain socket
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"1.19.0-meta"}`))
	})
	mux.HandleFunc("/proxies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"proxies":{"GLOBAL":{"type":"Selector"}}}`))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	// Write config.yaml
	configYAML := `
external-controller-unix: ` + sockPath + `
secret: test-secret
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	mihomoSvc := services.NewMihomoService("", "", tmpDir)
	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: "http://127.0.0.1:9090",
			MihomoSecret: "",
		},
		mihomoSvc: mihomoSvc,
	}

	// 1. Test ProbeAPI
	reachable, authenticated := mihomoSvc.ProbeAPI("test-secret")
	if !reachable || !authenticated {
		t.Fatalf("ProbeAPI() = (%v, %v), want (true, true)", reachable, authenticated)
	}

	// 2. Test MihomoProxy forwarding to /version
	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/proxy/version", nil)
	rr := httptest.NewRecorder()
	api.MihomoProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("MihomoProxy GET /version status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	var verResp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &verResp); err != nil {
		t.Fatalf("failed to parse version response: %v", err)
	}
	if verResp["version"] != "1.19.0-meta" {
		t.Errorf("version = %q, want '1.19.0-meta'", verResp["version"])
	}

	// 3. Test MihomoProxy forwarding to /proxies
	reqProxies := httptest.NewRequest(http.MethodGet, "/api/mihomo/proxy/proxies", nil)
	rrProxies := httptest.NewRecorder()
	api.MihomoProxy(rrProxies, reqProxies)

	if rrProxies.Code != http.StatusOK {
		t.Fatalf("MihomoProxy GET /proxies status = %d, want %d", rrProxies.Code, http.StatusOK)
	}

	// 4. Test Capabilities with Unix Domain Socket
	reqCaps := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rrCaps := httptest.NewRecorder()
	api.Capabilities(rrCaps, reqCaps)

	if rrCaps.Code != http.StatusOK {
		t.Fatalf("Capabilities status = %d, want %d", rrCaps.Code, http.StatusOK)
	}
	var capsResp struct {
		Data CapabilitiesResponse `json:"data"`
	}
	if err := json.Unmarshal(rrCaps.Body.Bytes(), &capsResp); err != nil {
		t.Fatalf("failed to unmarshal capabilities response: %v", err)
	}
	if capsResp.Data.Mihomo.ControllerType != "unix" {
		t.Errorf("ControllerType = %q, want 'unix'", capsResp.Data.Mihomo.ControllerType)
	}
	if capsResp.Data.Mihomo.ControllerTarget != sockPath {
		t.Errorf("ControllerTarget = %q, want %q", capsResp.Data.Mihomo.ControllerTarget, sockPath)
	}
	if capsResp.Data.Mihomo.IsInsecureLAN {
		t.Errorf("IsInsecureLAN = true, want false")
	}
	if !capsResp.Data.Mihomo.APIReachable {
		t.Errorf("APIReachable = false, want true")
	}
}
