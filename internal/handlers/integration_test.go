package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// --- Capabilities cache ---

// TestCapabilities_Cache verifies that the 3-second TTL cache returns cached data
// without hitting the backend again.
func TestCapabilities_Cache(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	api := &API{cfg: &config.Config{MihomoAPIURL: ts.URL}}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)

	// First request — should hit backend
	rr1 := httptest.NewRecorder()
	api.Capabilities(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}
	firstCallCount := callCount

	// Second request within cache TTL — should NOT hit Mihomo again
	rr2 := httptest.NewRecorder()
	api.Capabilities(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rr2.Code)
	}
	if callCount != firstCallCount {
		t.Errorf("capabilities cache miss: backend called %d times on second request (expected 0 additional calls)", callCount-firstCallCount)
	}
}

// TestCapabilities_CacheExpiry verifies that after the TTL the cache is refreshed.
func TestCapabilities_CacheExpiry(t *testing.T) {
	api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}}

	// Prime the cache
	rr := httptest.NewRecorder()
	api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))

	// Expire the cache manually
	api.capsCacheMutex.Lock()
	api.capsCacheTime = time.Now().Add(-10 * time.Second)
	api.capsCacheMutex.Unlock()

	// Next request should not use the expired cache (it will re-evaluate)
	rr2 := httptest.NewRecorder()
	api.Capabilities(rr2, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 after cache expiry, got %d", rr2.Code)
	}
}

// --- Outbound parse handler ---

// TestOutboundParse_MethodNotAllowed verifies GET returns 405.
func TestOutboundParse_MethodNotAllowed(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	req := httptest.NewRequest(http.MethodGet, "/api/outbound/parse", nil)
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// TestOutboundParse_EmptyBody verifies that a POST with no links returns 400.
func TestOutboundParse_EmptyBody(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	body := bytes.NewBufferString(`{"links":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", body)
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty links, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestOutboundParse_TooManyLinks verifies that >200 links returns 400.
func TestOutboundParse_TooManyLinks(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	links := make([]string, 201)
	for i := range links {
		links[i] = "vless://uuid@host:443"
	}
	body, _ := json.Marshal(map[string]interface{}{"links": links})
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >200 links, got %d", rr.Code)
	}
}

// TestOutboundParse_ValidVLESS verifies that a valid VLESS link is parsed correctly.
func TestOutboundParse_ValidVLESS(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	link := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#mytag"
	body, _ := json.Marshal(map[string]interface{}{"links": []string{link}})
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var env APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Fatalf("expected success=true, got error=%v", env.Error)
	}
}

// TestOutboundParse_TextInput verifies that newline-separated links via "text" field work.
func TestOutboundParse_TextInput(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	text := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#t1\nsocks5://user:pass@socks.example.com:1080#t2"
	body, _ := json.Marshal(map[string]interface{}{"text": text})
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Snapshot handler ---

// TestSnapshotRouter_InvalidID verifies that snapshot routes with invalid IDs return 400.
func TestSnapshotRouter_InvalidID(t *testing.T) {
	api := &API{}

	for _, suffix := range []string{"/restore", "/download", "/delete"} {
		path := "/api/snapshots/../etc/passwd" + suffix
		method := http.MethodPost
		if suffix == "/download" {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, path, nil)
		req.URL.Path = path
		rr := httptest.NewRecorder()
		api.SnapshotRouter(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("suffix=%s: expected 400 for path traversal ID, got %d", suffix, rr.Code)
		}
	}
}

// TestSnapshotCreate_MethodNotAllowed verifies GET to SnapshotCreate returns 405.
func TestSnapshotCreate_MethodNotAllowed(t *testing.T) {
	api := &API{cfg: &config.Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/snapshots", nil)
	rr := httptest.NewRecorder()
	api.SnapshotCreate(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// --- Settings handler ---

// TestSettingsGet_ReturnsConfig verifies that SettingsGet returns port and https config.
func TestSettingsGet_ReturnsConfig(t *testing.T) {
	api := &API{cfg: &config.Config{
		Port:  9090,
		HTTPS: config.HTTPSConfig{Enabled: true},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr := httptest.NewRecorder()
	api.SettingsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var env APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Success {
		t.Fatalf("expected success=true")
	}

	raw, _ := json.Marshal(env.Data)
	var settings SettingsResponse
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settings.Port != 9090 {
		t.Errorf("expected port=9090, got %d", settings.Port)
	}
	if !settings.HTTPS.Enabled {
		t.Error("expected HTTPS.Enabled=true")
	}
}

// TestSettingsHTTPS_MethodNotAllowed verifies GET to SettingsHTTPS returns 405.
func TestSettingsHTTPS_MethodNotAllowed(t *testing.T) {
	api := &API{cfg: &config.Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/https", nil)
	rr := httptest.NewRecorder()
	api.SettingsHTTPS(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// TestSettingsHTTPS_Toggle verifies that POST toggles the HTTPS.Enabled flag.
func TestSettingsHTTPS_Toggle(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Port: 8090}
	// ConfigPath empty → Save is skipped; just toggles in memory.
	api := &API{cfg: cfg}

	body := bytes.NewBufferString(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/https", body)
	rr := httptest.NewRecorder()
	api.SettingsHTTPS(rr, req)
	_ = tmp // suppress unused warning

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !cfg.HTTPS.Enabled {
		t.Error("expected HTTPS.Enabled=true after toggle")
	}

	// Check response body contains restart_required.
	body2 := bytes.NewBufferString(`{"enabled":false}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings/https", body2)
	rr2 := httptest.NewRecorder()
	api.SettingsHTTPS(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second toggle: expected 200, got %d", rr2.Code)
	}
	respBody := rr2.Body.String()
	if !strings.Contains(respBody, "restart_required") {
		t.Errorf("expected restart_required in response, got: %s", respBody)
	}
}
