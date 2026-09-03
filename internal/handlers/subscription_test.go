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

func TestSubscriptionAdd_DefaultKernels(t *testing.T) {
	// Case A: omitted (defaulting to true)
	{
		api, _ := newSubTestAPI(t)
		payload := `{"name": "testA", "url": "http://example.com/subA"}`
		req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", strings.NewReader(payload))
		rr := httptest.NewRecorder()
		api.SubscriptionAdd(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var created services.Subscription
		if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if !created.EnableXray || !created.EnableMihomo {
			t.Errorf("expected enable_xray and enable_mihomo to default to true, got: xray=%t, mihomo=%t", created.EnableXray, created.EnableMihomo)
		}
	}

	// Case B: explicit false (preservation of false)
	{
		api, _ := newSubTestAPI(t)
		payload := `{"name": "testB", "url": "http://example.com/subB", "enable_xray": false, "enable_mihomo": false}`
		req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", strings.NewReader(payload))
		rr := httptest.NewRecorder()
		api.SubscriptionAdd(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var created services.Subscription
		if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if created.EnableXray || created.EnableMihomo {
			t.Errorf("expected explicit false to be preserved, got: xray=%t, mihomo=%t", created.EnableXray, created.EnableMihomo)
		}
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
	if !updated.EnableXray {
		t.Errorf("expected EnableXray to remain true after update without explicit flags")
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
	providerName := services.GetMihomoProviderName("", "", adhocID)
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

	// 8. Отключенная подписка -> 403.
	disabledSub := &services.Subscription{
		Name:         "Disabled Sub",
		URL:          ts.URL + "/disabled",
		Enabled:      false,
		EnableMihomo: true,
	}
	subSvc.Add(disabledSub)
	req = httptest.NewRequest(http.MethodGet, "/api/provider.yaml?url="+disabledSub.URL, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	api.MihomoProviderAdapter(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for disabled subscription, got %d", rr.Code)
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

func TestSubscriptionUpdate_SockoptPreserve(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	sub := &services.Subscription{
		ID:              "sub-test",
		Name:            "Original Name",
		URL:             "https://example.com/sub",
		Enabled:         true,
		EnableXray:      true,
		SockoptMark:     123,
		SockoptFastOpen: true,
		SockoptMptcp:    true,
	}
	if err := subSvc.Add(sub); err != nil {
		t.Fatalf("failed to add sub: %v", err)
	}

	// 1. Partial update without any sockopt fields in JSON
	updateBody := `{"name": "Renamed Sub"}`
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/update?id=sub-test", strings.NewReader(updateBody))
	rr := httptest.NewRecorder()
	api.SubscriptionUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	updated := subSvc.Get("sub-test")
	if updated == nil {
		t.Fatalf("subscription not found after update")
	}
	if updated.Name != "Renamed Sub" {
		t.Errorf("expected Name='Renamed Sub', got %q", updated.Name)
	}
	if updated.SockoptMark != 123 {
		t.Errorf("expected SockoptMark=123 preserved, got %d", updated.SockoptMark)
	}
	if !updated.SockoptFastOpen {
		t.Errorf("expected SockoptFastOpen=true preserved")
	}
	if !updated.SockoptMptcp {
		t.Errorf("expected SockoptMptcp=true preserved")
	}

	// 2. Explicit update of sockopt fields
	explicitBody := `{"sockopt_mark": 456, "sockopt_fast_open": false}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/subscriptions/update?id=sub-test", strings.NewReader(explicitBody))
	rr2 := httptest.NewRecorder()
	api.SubscriptionUpdate(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second update, got %d", rr2.Code)
	}

	updated2 := subSvc.Get("sub-test")
	if updated2.SockoptMark != 456 {
		t.Errorf("expected updated SockoptMark=456, got %d", updated2.SockoptMark)
	}
	if updated2.SockoptFastOpen {
		t.Errorf("expected updated SockoptFastOpen=false, got true")
	}
	if !updated2.SockoptMptcp {
		t.Errorf("expected SockoptMptcp=true preserved when omitted, got false")
	}
}

func TestSubscriptionNodeDialerProxy(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	sub := &services.Subscription{
		ID:         "sub-1",
		Name:       "Sub 1",
		URL:        "http://example.com/sub",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node-src", Name: "Source Node", Protocol: "vless"},
			{Tag: "node-target", Name: "Target Node", Protocol: "vless"},
			{Tag: "node-chained", Name: "Chained Node", Protocol: "vless", DialerProxy: "node-target"},
		},
	}
	if err := subSvc.Add(sub); err != nil {
		t.Fatal(err)
	}

	// 1. Method not allowed (GET)
	req405 := httptest.NewRequest(http.MethodGet, "/api/subscriptions/node-dialer-proxy?id=sub-1", nil)
	rr405 := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rr405, req405)
	if rr405.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr405.Code)
	}

	// 2. Missing id (400)
	reqNoID := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy", strings.NewReader(`{"node_tag":"node-src","target_tag":"node-target"}`))
	rrNoID := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrNoID, reqNoID)
	if rrNoID.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing id, got %d", rrNoID.Code)
	}

	// 3. Missing node_tag in body (400)
	reqNoNode := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"target_tag":"node-target"}`))
	rrNoNode := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrNoNode, reqNoNode)
	if rrNoNode.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing node_tag, got %d", rrNoNode.Code)
	}

	// 4. Non-existent sub (404)
	req404Sub := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=nonexistent", strings.NewReader(`{"node_tag":"node-src","target_tag":"node-target"}`))
	rr404Sub := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rr404Sub, req404Sub)
	if rr404Sub.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing sub, got %d", rr404Sub.Code)
	}

	// 5. Non-existent node (404)
	req404Node := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"nonexistent","target_tag":"node-target"}`))
	rr404Node := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rr404Node, req404Node)
	if rr404Node.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing node, got %d", rr404Node.Code)
	}

	// 6. Self-cascade (409)
	reqSelf := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"node-src","target_tag":"node-src"}`))
	rrSelf := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrSelf, reqSelf)
	if rrSelf.Code != http.StatusConflict {
		t.Errorf("expected 409 for self-cascade, got %d", rrSelf.Code)
	}

	// 7. Target not available (409)
	reqMissingTarget := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"node-src","target_tag":"ghost"}`))
	rrMissingTarget := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrMissingTarget, reqMissingTarget)
	if rrMissingTarget.Code != http.StatusConflict {
		t.Errorf("expected 409 for missing target, got %d", rrMissingTarget.Code)
	}

	// 8. Chain limited to one level (409)
	reqChain := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"node-src","target_tag":"node-chained"}`))
	rrChain := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrChain, reqChain)
	if rrChain.Code != http.StatusConflict {
		t.Errorf("expected 409 for chain limit, got %d", rrChain.Code)
	}

	// 9. Success set target (200)
	reqSuccess := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"node-src","target_tag":"node-target"}`))
	rrSuccess := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrSuccess, reqSuccess)
	if rrSuccess.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrSuccess.Code, rrSuccess.Body.String())
	}

	// 10. Success clear target (200)
	reqClear := httptest.NewRequest(http.MethodPost, "/api/subscriptions/node-dialer-proxy?id=sub-1", strings.NewReader(`{"node_tag":"node-src","target_tag":""}`))
	rrClear := httptest.NewRecorder()
	api.SubscriptionSetNodeDialerProxy(rrClear, reqClear)
	if rrClear.Code != http.StatusOK {
		t.Fatalf("expected 200 for clear, got %d: %s", rrClear.Code, rrClear.Body.String())
	}
}

func TestSubscriptionDialerProxyTargets(t *testing.T) {
	api, subSvc := newSubTestAPI(t)

	sub := &services.Subscription{
		ID:         "sub-a",
		Name:       "Sub A",
		URL:        "http://example.com/sub-a",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node-1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node-2", Name: "Node 2", Protocol: "vless"},
		},
	}
	if err := subSvc.Add(sub); err != nil {
		t.Fatal(err)
	}

	// 1. Method not allowed (POST)
	req405 := httptest.NewRequest(http.MethodPost, "/api/subscriptions/dialer-proxy-targets?id=sub-a&node_tag=node-1", nil)
	rr405 := httptest.NewRecorder()
	api.SubscriptionDialerProxyTargets(rr405, req405)
	if rr405.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr405.Code)
	}

	// 2. Missing params (400)
	req400 := httptest.NewRequest(http.MethodGet, "/api/subscriptions/dialer-proxy-targets?id=sub-a", nil)
	rr400 := httptest.NewRecorder()
	api.SubscriptionDialerProxyTargets(rr400, req400)
	if rr400.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr400.Code)
	}

	// 3. Success list targets (200)
	reqSuccess := httptest.NewRequest(http.MethodGet, "/api/subscriptions/dialer-proxy-targets?id=sub-a&node_tag=node-1", nil)
	rrSuccess := httptest.NewRecorder()
	api.SubscriptionDialerProxyTargets(rrSuccess, reqSuccess)
	if rrSuccess.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rrSuccess.Code)
	}
	var resp struct {
		Success bool                       `json:"success"`
		Data    []services.DialerProxyTarget `json:"data"`
	}
	if err := json.Unmarshal(rrSuccess.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Tag != "node-2" {
		t.Errorf("expected 1 target node-2, got %+v", resp.Data)
	}

	// 4. Empty list returns 200 and []
	// Disable sub-a to leave 0 active targets
	sub.Enabled = false
	subSvc.Update(sub.ID, sub)

	reqEmpty := httptest.NewRequest(http.MethodGet, "/api/subscriptions/dialer-proxy-targets?id=sub-a&node_tag=node-1", nil)
	rrEmpty := httptest.NewRecorder()
	api.SubscriptionDialerProxyTargets(rrEmpty, reqEmpty)
	if rrEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty targets, got %d", rrEmpty.Code)
	}
	var emptyResp struct {
		Success bool                       `json:"success"`
		Data    []services.DialerProxyTarget `json:"data"`
	}
	if err := json.Unmarshal(rrEmpty.Body.Bytes(), &emptyResp); err != nil {
		t.Fatal(err)
	}
	if len(emptyResp.Data) != 0 {
		t.Errorf("expected 0 targets, got %d", len(emptyResp.Data))
	}
}


