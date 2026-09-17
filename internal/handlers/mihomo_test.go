package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestMihomoStatus(t *testing.T) {
	tmpDir := t.TempDir()
	mihomoSvc := services.NewMihomoService("nonexistent-mihomo-bin", "nonexistent-xkeen", tmpDir)

	api := &API{
		cfg:       &config.Config{},
		mihomoSvc: mihomoSvc,
		pathVal:   utils.NewPathValidator([]string{tmpDir}),
	}

	// 1. Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/mihomo/status", nil)
	recPost := httptest.NewRecorder()
	api.MihomoStatus(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. GET returns status
	reqGet := httptest.NewRequest(http.MethodGet, "/api/mihomo/status", nil)
	recGet := httptest.NewRecorder()
	api.MihomoStatus(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}
	body := recGet.Body.String()
	if body != "stopped" && body[:7] != "running" {
		t.Errorf("unexpected status body: %q", body)
	}
}

func TestMihomoProxy(t *testing.T) {
	// Setup mock backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/version" {
				if auth != "Bearer test-secret" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"version":"1.18.0"}`))
				return
			}
		case http.MethodPut:
			if r.URL.Path == "/configs" {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Write(body)
				return
			}
		case http.MethodPost:
			if r.URL.Path == "/restart" {
				w.WriteHeader(http.StatusOK)
				return
			}
		case http.MethodDelete:
			if r.URL.Path == "/connections" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	tmpDir := t.TempDir()
	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: backend.URL,
			MihomoSecret: "test-secret",
		},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}

	// 1. Method Not Allowed (OPTIONS/TRACE)
	reqOpts := httptest.NewRequest(http.MethodOptions, "/api/mihomo/proxy/version", nil)
	recOpts := httptest.NewRecorder()
	api.MihomoProxy(recOpts, reqOpts)
	if recOpts.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for OPTIONS, got %d", recOpts.Code)
	}

	// 2. GET /api/mihomo/proxy/version
	reqGet := httptest.NewRequest(http.MethodGet, "/api/mihomo/proxy/version", nil)
	recGet := httptest.NewRecorder()
	api.MihomoProxy(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET, got %d: %s", recGet.Code, recGet.Body.String())
	}
	var verResp map[string]interface{}
	if err := json.Unmarshal(recGet.Body.Bytes(), &verResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if verResp["version"] != "1.18.0" {
		t.Errorf("expected version 1.18.0, got %v", verResp["version"])
	}

	// 3. PUT /api/mihomo/proxy/configs
	reqPut := httptest.NewRequest(http.MethodPut, "/api/mihomo/proxy/configs", bytes.NewBufferString(`{"mode":"rule"}`))
	recPut := httptest.NewRecorder()
	api.MihomoProxy(recPut, reqPut)
	if recPut.Code != http.StatusOK {
		t.Errorf("expected 200 for PUT, got %d: %s", recPut.Code, recPut.Body.String())
	}

	// 4. POST /api/mihomo/proxy/restart
	reqPost := httptest.NewRequest(http.MethodPost, "/api/mihomo/proxy/restart", nil)
	recPost := httptest.NewRecorder()
	api.MihomoProxy(recPost, reqPost)
	if recPost.Code != http.StatusOK {
		t.Errorf("expected 200 for POST, got %d: %s", recPost.Code, recPost.Body.String())
	}

	// 5. DELETE /api/mihomo/proxy/connections
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/mihomo/proxy/connections", nil)
	recDel := httptest.NewRecorder()
	api.MihomoProxy(recDel, reqDel)
	if recDel.Code != http.StatusNoContent {
		t.Errorf("expected 204 for DELETE, got %d", recDel.Code)
	}

	// 6. Test with MihomoService controller resolution
	configYAML := fmt.Sprintf("external-controller: %s\nsecret: ctrl-secret\n", backend.Listener.Addr().String())
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	apiWithSvc := &API{
		cfg: &config.Config{
			MihomoConfigDir: tmpDir,
		},
		mihomoSvc: services.NewMihomoService("", "", tmpDir),
		pathVal:   utils.NewPathValidator([]string{tmpDir}),
	}
	reqWithSvc := httptest.NewRequest(http.MethodPost, "/api/mihomo/proxy/restart", nil)
	recWithSvc := httptest.NewRecorder()
	apiWithSvc.MihomoProxy(recWithSvc, reqWithSvc)
	if recWithSvc.Code != http.StatusOK {
		t.Errorf("expected 200 for controller config, got %d: %s", recWithSvc.Code, recWithSvc.Body.String())
	}

	// 7. Backend failure (Bad Gateway)
	deadAPI := &API{
		cfg: &config.Config{
			MihomoAPIURL: "http://127.0.0.1:54321", // assuming closed port
		},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}
	reqDead := httptest.NewRequest(http.MethodGet, "/api/mihomo/proxy/version", nil)
	recDead := httptest.NewRecorder()
	deadAPI.MihomoProxy(recDead, reqDead)
	if recDead.Code != http.StatusBadGateway {
		t.Errorf("expected 502 Bad Gateway for dead backend, got %d", recDead.Code)
	}
}

func TestMihomoDNSQuery_And_FlushFakeIP_Errors(t *testing.T) {
	api := &API{}

	// DNSQuery Method Not Allowed (POST)
	reqDNSMethod := httptest.NewRequest(http.MethodPost, "/api/mihomo/dns/query?name=google.com", nil)
	recDNSMethod := httptest.NewRecorder()
	api.MihomoDNSQuery(recDNSMethod, reqDNSMethod)
	if recDNSMethod.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", recDNSMethod.Code)
	}

	// DNSQuery Empty Name
	reqDNSEmpty := httptest.NewRequest(http.MethodGet, "/api/mihomo/dns/query", nil)
	recDNSEmpty := httptest.NewRecorder()
	api.MihomoDNSQuery(recDNSEmpty, reqDNSEmpty)
	if recDNSEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", recDNSEmpty.Code)
	}

	// DNSQuery Service Unavailable
	reqDNSSvc := httptest.NewRequest(http.MethodGet, "/api/mihomo/dns/query?name=google.com", nil)
	recDNSSvc := httptest.NewRecorder()
	api.MihomoDNSQuery(recDNSSvc, reqDNSSvc)
	if recDNSSvc.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil service, got %d", recDNSSvc.Code)
	}

	// FlushFakeIP Method Not Allowed (GET)
	reqFlushMethod := httptest.NewRequest(http.MethodGet, "/api/mihomo/cache/fakeip/flush", nil)
	recFlushMethod := httptest.NewRecorder()
	api.MihomoFlushFakeIP(recFlushMethod, reqFlushMethod)
	if recFlushMethod.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", recFlushMethod.Code)
	}

	// FlushFakeIP Service Unavailable
	reqFlushSvc := httptest.NewRequest(http.MethodPost, "/api/mihomo/cache/fakeip/flush", nil)
	recFlushSvc := httptest.NewRecorder()
	api.MihomoFlushFakeIP(recFlushSvc, reqFlushSvc)
	if recFlushSvc.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil service, got %d", recFlushSvc.Code)
	}
}
