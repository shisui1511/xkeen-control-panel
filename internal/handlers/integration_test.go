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

// TestOutboundParse_WgQuickConf verifies that wg-quick INI content in text is parsed as wireguard.
func TestOutboundParse_WgQuickConf(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	conf := `[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
DNS = 1.1.1.1
MTU = 1280
Jc = 4
Jmin = 40
Jmax = 70
S1 = 15
S2 = 40
H1 = 1000000001
H2 = 1000000002
H3 = 1000000003
H4 = 1000000004

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0
`
	body, _ := json.Marshal(map[string]interface{}{"text": conf})
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    []struct {
			Link     string             `json:"link"`
			Outbound *services.Outbound `json:"outbound"`
			Error    string             `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Data))
	}
	if resp.Data[0].Outbound == nil || resp.Data[0].Outbound.Protocol != "wireguard" {
		t.Fatalf("expected wireguard outbound, got: %+v", resp.Data[0].Outbound)
	}
	awg, ok := resp.Data[0].Outbound.Settings["awg"]
	if !ok || awg == nil {
		t.Fatalf("expected awg settings to be populated")
	}
}

// TestOutboundParse_OversizedBody verifies that an oversized request body on OutboundParse returns 413.
func TestOutboundParse_OversizedBody(t *testing.T) {
	tmp := t.TempDir()
	svc := services.NewSubscriptionService(tmp, tmp, tmp)
	api := &API{subscriptionSvc: svc}

	// Body larger than 1MB (maxConfigBytes is 1MB, i.e., 1024*1024) and formatted as valid JSON
	body := `{"links":["` + strings.Repeat("a", 1024*1024) + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.OutboundParse(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rr.Code)
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
