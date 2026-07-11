package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func newProxyProvidersTestAPI(t *testing.T) (*API, *services.SubscriptionService) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		DataDir:       tmpDir,
		XRayConfigDir: tmpDir,
		AllowedRoots:  []string{tmpDir},
		MihomoAPIURL:  "http://127.0.0.1:1", // по умолчанию недоступный
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

func TestProxyProviders_List_MihomoOffline(t *testing.T) {
	api, subSvc := newProxyProvidersTestAPI(t)

	// Add a test subscription
	sub := &services.Subscription{
		ID:           "sub1",
		Name:         "Sub 1",
		URL:          "http://example.com/sub",
		EnableMihomo: true,
	}
	_ = subSvc.Add(sub)

	// Mihomo is stopped (non-existent binary path)
	api.mihomoSvc = services.NewMihomoService("/path/to/nonexistent/binary", "", api.cfg.DataDir)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var list []ProxyProviderResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}

	if list[0].ID != "sub1" {
		t.Errorf("expected ID 'sub1', got '%s'", list[0].ID)
	}

	if list[0].MihomoProvider != nil {
		t.Errorf("expected mihomo_provider to be null when Mihomo is stopped")
	}
}

func TestProxyProviders_List_MihomoOnline(t *testing.T) {
	api, subSvc := newProxyProvidersTestAPI(t)

	// Add a test subscription
	sub := &services.Subscription{
		ID:           "sub1",
		Name:         "Sub 1",
		URL:          "http://example.com/sub",
		EnableMihomo: true,
	}
	_ = subSvc.Add(sub)

	// Mock Clash API server
	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/providers/proxies" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// sub1 provider name is derived via sub.GetProviderName()
			providerName := sub.GetProviderName()
			response := map[string]interface{}{
				"providers": map[string]interface{}{
					providerName: map[string]interface{}{
						"name":        providerName,
						"type":        "proxy",
						"vehicleType": "HTTP",
						"updatedAt":   "2026-07-11T12:00:00Z",
						"proxies": []interface{}{
							map[string]interface{}{"name": "node1"},
							map[string]interface{}{"name": "node2"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockClashAPI.Close()

	api.cfg.MihomoAPIURL = mockClashAPI.URL

	// Mihomo is running (using /bin/sh binary to trigger 'running' status)
	api.mihomoSvc = services.NewMihomoService("/bin/sh", "", api.cfg.DataDir)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var list []ProxyProviderResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}

	mp := list[0].MihomoProvider
	if mp == nil {
		t.Fatal("expected mihomo_provider to be non-null")
	}

	expectedName := sub.GetProviderName()
	if mp.Name != expectedName {
		t.Errorf("expected provider name '%s', got '%s'", expectedName, mp.Name)
	}
	if mp.VehicleType != "HTTP" {
		t.Errorf("expected vehicle type 'HTTP', got '%s'", mp.VehicleType)
	}
	if mp.UpdatedAt != "2026-07-11T12:00:00Z" {
		t.Errorf("expected updatedAt '2026-07-11T12:00:00Z', got '%s'", mp.UpdatedAt)
	}
	if mp.NodeCount != 2 {
		t.Errorf("expected node count 2, got %d", mp.NodeCount)
	}
}

func TestProxyProviders_Refresh(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)

	// Mock Clash API server for PUT reload
	calledRefresh := false
	gotAuth := ""
	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/providers/proxies/test_provider" {
			calledRefresh = true
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockClashAPI.Close()

	// Initialize subscription service with Mihomo API details
	api.subscriptionSvc.SetMihomoAPI(mockClashAPI.URL, "test_secret")

	req := httptest.NewRequest(http.MethodPut, "/api/proxy-providers/test_provider/refresh", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	if !calledRefresh {
		t.Error("expected Clash API refresh to be called, but it was not")
	}
	if gotAuth != "Bearer test_secret" {
		t.Errorf("expected Authorization 'Bearer test_secret', got '%s'", gotAuth)
	}
}

// TestProxyProviders_Refresh_SecretFallback проверяет, что при пустом секрете
// в конфиге панели используется fallback-резолвер (секрет из config.yaml Mihomo).
func TestProxyProviders_Refresh_SecretFallback(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)

	gotAuth := ""
	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/providers/proxies/test_provider" {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockClashAPI.Close()

	// Секрет панели пуст — резолвер эмулирует чтение из config.yaml Mihomo.
	api.subscriptionSvc.SetMihomoAPI(mockClashAPI.URL, "")
	api.subscriptionSvc.SetMihomoSecretResolver(func() string { return "yaml_secret" })

	req := httptest.NewRequest(http.MethodPut, "/api/proxy-providers/test_provider/refresh", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if gotAuth != "Bearer yaml_secret" {
		t.Errorf("expected Authorization 'Bearer yaml_secret', got '%s'", gotAuth)
	}
}
