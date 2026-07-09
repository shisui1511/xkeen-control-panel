package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func newSubTestAPI(t *testing.T) (*API, *services.SubscriptionService) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		DataDir:       tmpDir,
		XRayConfigDir: tmpDir,
		AllowedRoots:  []string{tmpDir},
	}
	subSvc := services.NewSubscriptionService(tmpDir, tmpDir, tmpDir)
	subSvc.SetHTTPClient(http.DefaultClient)

	api := &API{
		cfg:             cfg,
		subscriptionSvc: subSvc,
		pathVal:         utils.NewPathValidator(cfg.AllowedRoots),
	}
	return api, subSvc
}

func TestSubscriptionList(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	// 1. Empty List
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	rr := httptest.NewRecorder()
	api.SubscriptionList(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var list []services.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(list))
	}

	// 2. Non-empty List
	sub := &services.Subscription{Name: "Sub 1", URL: "http://example.com/sub", EnableXray: true}
	subSvc.Add(sub)

	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	rr = httptest.NewRecorder()
	api.SubscriptionList(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Sub 1" {
		t.Errorf("unexpected list: %+v", list)
	}

	// 3. Method not allowed
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions", nil)
	rr = httptest.NewRecorder()
	api.SubscriptionList(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestSubscriptionAdd(t *testing.T) {
	api, _ := newSubTestAPI(t)

	// 1. Invalid payload
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", strings.NewReader("invalid json"))
	rr := httptest.NewRecorder()
	api.SubscriptionAdd(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Empty URL
	payload := `{"name": "test"}`
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", strings.NewReader(payload))
	rr = httptest.NewRecorder()
	api.SubscriptionAdd(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 3. Success Add (Default Interval)
	payload = `{"name": "test", "url": "http://example.com/sub"}`
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", strings.NewReader(payload))
	rr = httptest.NewRecorder()
	api.SubscriptionAdd(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var created services.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Interval != 24 {
		t.Errorf("expected default interval 24, got %d", created.Interval)
	}
}

func TestSubscriptionUpdate(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	sub := &services.Subscription{Name: "Old Name", URL: "http://example.com/sub", EnableXray: true}
	subSvc.Add(sub)
	id := subSvc.List()[0].ID

	// 1. Missing ID
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/update", strings.NewReader(`{"name":"New"}`))
	rr := httptest.NewRecorder()
	api.SubscriptionUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Success Update
	payload := `{"name": "New Name", "url": "http://example.com/new"}`
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions/update?id="+id, strings.NewReader(payload))
	rr = httptest.NewRecorder()
	api.SubscriptionUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var updated services.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "New Name" || updated.URL != "http://example.com/new" {
		t.Errorf("unexpected updated sub: %+v", updated)
	}
}

func TestSubscriptionDelete(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	sub := &services.Subscription{Name: "To Delete", URL: "http://example.com/sub", EnableXray: true}
	subSvc.Add(sub)
	id := subSvc.List()[0].ID

	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/delete?id="+id, nil)
	rr := httptest.NewRecorder()
	api.SubscriptionDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	if len(subSvc.List()) != 0 {
		t.Error("subscription was not deleted")
	}
}

func TestSubscriptionRefresh(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	// Mock server that serves a valid vless link format (which looks like base64 or share-links)
	vless := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#myserver"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(vless))
	}))
	defer ts.Close()

	sub := &services.Subscription{Name: "Refresh Sub", URL: ts.URL, Enabled: true, EnableXray: true}
	subSvc.Add(sub)
	id := subSvc.List()[0].ID

	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/refresh?id="+id, nil)
	rr := httptest.NewRecorder()
	api.SubscriptionRefresh(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	updated := subSvc.Get(id)
	if updated.LastError != "" {
		t.Errorf("expected no error, got %s", updated.LastError)
	}
	if len(updated.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(updated.Nodes))
	}
}

func TestSubscriptionRefreshAll(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	vless := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#myserver"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(vless))
	}))
	defer ts.Close()

	sub1 := &services.Subscription{Name: "Sub 1", URL: ts.URL, Enabled: true, EnableXray: true}
	sub2 := &services.Subscription{Name: "Sub 2", URL: ts.URL, Enabled: false, EnableXray: true}
	subSvc.Add(sub1)
	subSvc.Add(sub2)

	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/refresh-all", nil)
	rr := httptest.NewRecorder()
	api.SubscriptionRefreshAll(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var results []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status bool   `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	// Only enabled subscription should refresh
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestSubscriptionRawAndReport(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	vless := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#myserver"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "Value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(vless))
	}))
	defer ts.Close()

	sub := &services.Subscription{Name: "Raw Sub", URL: ts.URL, Enabled: true, EnableXray: true}
	subSvc.Add(sub)
	id := subSvc.List()[0].ID

	// Refresh first to write raw and parse report files
	subSvc.Refresh(id)

	// 1. SubscriptionRaw
	reqRaw := httptest.NewRequest(http.MethodGet, "/api/subscriptions/raw?id="+id, nil)
	rrRaw := httptest.NewRecorder()
	api.SubscriptionRaw(rrRaw, reqRaw)
	if rrRaw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrRaw.Code, rrRaw.Body.String())
	}
	var rawResp map[string]interface{}
	if err := json.Unmarshal(rrRaw.Body.Bytes(), &rawResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawResp["body"].(string), "vless://") {
		t.Errorf("expected vless:// in body, got %s", rawResp["body"])
	}

	// 2. SubscriptionParseReport
	reqRep := httptest.NewRequest(http.MethodGet, "/api/subscriptions/report?id="+id, nil)
	rrRep := httptest.NewRecorder()
	api.SubscriptionParseReport(rrRep, reqRep)
	if rrRep.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrRep.Code, rrRep.Body.String())
	}
	var report services.ParseReport
	if err := json.Unmarshal(rrRep.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ParsedCount != 1 {
		t.Errorf("expected parsed_count 1, got %d", report.ParsedCount)
	}
}

func TestSubscriptionSetActive(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	// Set up subscription with nodes
	sub := &services.Subscription{
		Name:       "Routing Sub",
		URL:        "http://example.com/sub",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node-1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node-2", Name: "Node 2", Protocol: "vless"},
		},
	}
	subSvc.Add(sub)
	id := subSvc.List()[0].ID

	// Write mock outbounds file
	fragmentPath := filepath.Join(api.cfg.XRayConfigDir, "04_outbounds."+id+".json")
	outboundsContent := `{"outbounds": [{"tag": "node-1", "protocol": "vless"}, {"tag": "node-2", "protocol": "vless"}]}`
	if err := os.WriteFile(fragmentPath, []byte(outboundsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Success SetActive
	body := `{"node_tag": "node-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/active?id="+id, strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.SubscriptionSetActive(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// 2. Balancer Auto Conflict
	subSvc.Update(id, &services.Subscription{
		RoutingMode: "auto",
		EnableXray:  true,
	})
	reqConf := httptest.NewRequest(http.MethodPost, "/api/subscriptions/active?id="+id, strings.NewReader(body))
	rrConf := httptest.NewRecorder()
	api.SubscriptionSetActive(rrConf, reqConf)
	if rrConf.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", rrConf.Code)
	}
}

func TestMihomoProviderAdapter(t *testing.T) {
	api, subSvc := newSubTestAPI(t)
	api.cfg.MihomoConfigDir = api.cfg.DataDir
	subSvc.SetHTTPClient(http.DefaultClient) // разрешить httptest на 127.0.0.1

	// Mock upstream отдаёт полный Clash config (proxies + dns) — адаптер
	// должен вернуть только секцию proxies:.
	upstreamYAML := "proxies:\n  - name: test-node\n    type: ss\n    server: 1.2.3.4\n    port: 8388\n    cipher: aes-256-gcm\n    password: pass\ndns:\n  nameserver:\n    - 8.8.8.8\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamYAML))
	}))
	defer ts.Close()

	sub := &services.Subscription{
		Name:         "Adapter Sub",
		URL:          ts.URL,
		Enabled:      true,
		EnableMihomo: true,
	}
	subSvc.Add(sub)

	// 1. Метод не GET
	req := httptest.NewRequest(http.MethodPost, "/api/provider.yaml", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}

	// 2. Внешний IP (не loopback) -> 403
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+sub.URL, nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}

	// 3. Отсутствие параметра url -> 400
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 4. Успешный запрос с loopback IP: upstream проксируется, полный config
	// сокращается до только proxies: секции.
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+sub.URL, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Content-Type") != "text/yaml; charset=utf-8" {
		t.Errorf("expected text/yaml content type, got %q", rr.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(rr.Body.String(), "proxies:") {
		t.Errorf("expected body to start with 'proxies:', got %q", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "dns:") {
		t.Errorf("expected full config to be reduced to proxies: section only, got %q", rr.Body.String())
	}

	// 5. Неизвестный (незарегистрированный) URL всё равно проксируется upstream
	// (адаптер создаёт временную подписку вместо возврата 404).
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+ts.URL+"/unknown-but-same-host", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for ad-hoc url, got %d: %s", rr.Code, rr.Body.String())
	}

	// 6. Сетевая ошибка + наличие кэша на диске -> отдаётся кэш (200).
	badURL := "http://127.0.0.1:1/unreachable"
	adhocID := adhocSubscriptionID(badURL)
	providerName := services.GetMihomoProviderName("", "", badURL, adhocID)
	proxyProvidersDir := filepath.Join(api.cfg.MihomoConfigDir, "proxy_providers")
	if err := os.MkdirAll(proxyProvidersDir, 0755); err != nil {
		t.Fatal(err)
	}
	cachedYAML := "proxies:\n  - name: cached-node\n"
	cacheFile := filepath.Join(proxyProvidersDir, providerName+".yaml")
	if err := os.WriteFile(cacheFile, []byte(cachedYAML), 0600); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+badURL, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (cache fallback), got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != cachedYAML {
		t.Errorf("expected cached payload %q, got %q", cachedYAML, rr.Body.String())
	}

	// 7. Сетевая ошибка без кэша -> 502.
	badURL2 := "http://127.0.0.1:2/unreachable-no-cache"
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+badURL2, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMihomoProviderRedirect(t *testing.T) {
	api, _ := newSubTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/mihomo/provider.yaml?url=https://example.com/sub&insecure=true", nil)
	rr := httptest.NewRecorder()
	api.MihomoProviderRedirect(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/provider.yaml?") {
		t.Errorf("expected redirect location to start with /api/provider.yaml?, got %q", loc)
	}
	if !strings.Contains(loc, "url=https%3A%2F%2Fexample.com%2Fsub") && !strings.Contains(loc, "url=https://example.com/sub") {
		t.Errorf("expected original url query param preserved, got %q", loc)
	}
	if !strings.Contains(loc, "insecure=true") {
		t.Errorf("expected all original query params preserved, got %q", loc)
	}
}
