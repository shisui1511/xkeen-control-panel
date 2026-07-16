package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// startFakeMihomo запускает подконтрольный долгоживущий процесс с уникальным
// именем и возвращает путь к его бинарнику. MihomoService.Status() (pidof)
// стабильно видит его как "running" без зависимости от посторонних процессов
// на хосте (например, наличия живого sh).
func startFakeMihomo(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skipf("cannot read /bin/sleep to build fake mihomo binary: %v", err)
	}

	// Имя короче 15 символов, чтобы гарантированно попасть в comm для pidof.
	name := fmt.Sprintf("xcpmhm%d", os.Getpid()%1000000)
	binPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(binPath, src, 0o755); err != nil {
		t.Fatalf("write fake mihomo binary: %v", err)
	}

	cmd := exec.Command(binPath, "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake mihomo: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Дождаться, пока pidof увидит процесс (устраняет гонку старта).
	svc := services.NewMihomoService(binPath, "", t.TempDir())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := svc.Status(); err == nil && strings.HasPrefix(status, "running") {
			return binPath
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("fake mihomo process %s did not become visible to pidof", name)
	return ""
}

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

	// Mihomo is running — запускаем подконтрольный процесс вместо зависимости
	// от посторонних процессов хоста.
	api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

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
		if r.Method == http.MethodPut && r.URL.Path == "/providers/proxies/test-provider" {
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

	req := httptest.NewRequest(http.MethodPut, "/api/proxy-providers/test-provider/refresh", nil)
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
		if r.Method == http.MethodPut && r.URL.Path == "/providers/proxies/test-provider" {
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

	req := httptest.NewRequest(http.MethodPut, "/api/proxy-providers/test-provider/refresh", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if gotAuth != "Bearer yaml_secret" {
		t.Errorf("expected Authorization 'Bearer yaml_secret', got '%s'", gotAuth)
	}
}

// TestProxyProviders_Refresh_InvalidName проверяет, что некорректные имена
// провайдеров отклоняются до исходящего запроса к Clash API, а негативные
// маршруты не доходят до handler'а обновления.
func TestProxyProviders_Refresh_InvalidName(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)

	outboundCalled := false
	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outboundCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockClashAPI.Close()

	api.subscriptionSvc.SetMihomoAPI(mockClashAPI.URL, "test_secret")

	cases := []struct {
		name     string
		method   string
		path     string
		wantCode int
	}{
		{"underscore", http.MethodPut, "/api/proxy-providers/bad_name/refresh", http.StatusBadRequest},
		{"multi-segment", http.MethodPut, "/api/proxy-providers/a/b/refresh", http.StatusBadRequest},
		{"query-like chars", http.MethodPut, "/api/proxy-providers/x%3Fy/refresh", http.StatusBadRequest},
		{"uppercase", http.MethodPut, "/api/proxy-providers/BadName/refresh", http.StatusBadRequest},
		{"no name in path", http.MethodPut, "/api/proxy-providers/refresh", http.StatusNotFound},
		{"wrong method", http.MethodPost, "/api/proxy-providers/test-provider/refresh", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			api.ProxyProvidersRouter(rr, req)
			if rr.Code != tc.wantCode {
				t.Errorf("expected %d, got %d: %s", tc.wantCode, rr.Code, rr.Body.String())
			}
		})
	}

	if outboundCalled {
		t.Error("Clash API must not be called for invalid provider names")
	}
}

// TestProxyProviders_Refresh_ErrorMapping проверяет маппинг ошибок обновления
// в HTTP-статусы: API не настроен → 503, неизвестный провайдер → 404,
// прочие ошибки Clash API → 502, Mihomo недоступен → 502.
func TestProxyProviders_Refresh_ErrorMapping(t *testing.T) {
	doRefresh := func(t *testing.T, api *API) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/proxy-providers/test-provider/refresh", nil)
		rr := httptest.NewRecorder()
		api.ProxyProvidersRouter(rr, req)
		return rr
	}

	t.Run("api not configured", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		api.subscriptionSvc.SetMihomoAPI("", "")
		if rr := doRefresh(t, api); rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("unknown provider maps to 404", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mock.Close()
		api.subscriptionSvc.SetMihomoAPI(mock.URL, "")
		if rr := doRefresh(t, api); rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("clash api error maps to 502", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer mock.Close()
		api.subscriptionSvc.SetMihomoAPI(mock.URL, "")
		rr := doRefresh(t, api)
		if rr.Code != http.StatusBadGateway {
			t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("mihomo unreachable maps to 502", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		// Только что закрытый порт гарантирует мгновенный connection refused
		// (фиксированный порт вроде 127.0.0.1:1 может зависать на 30s в WSL2).
		closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		closedURL := closed.URL
		closed.Close()
		api.subscriptionSvc.SetMihomoAPI(closedURL, "")
		rr := doRefresh(t, api)
		if rr.Code != http.StatusBadGateway {
			t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), closedURL) {
			t.Errorf("internal controller URL leaked to client: %s", rr.Body.String())
		}
	})
}

// TestProxyProviderNodes_Sanitize проверяет санитизацию данных нод:
// все чувствительные поля (uuid, password, obfs, server) отрезаются,
// остаются только tag, name, alive, tested, delay_ms.
func TestProxyProviderNodes_Sanitize(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)
	api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/providers/proxies/test-provider" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"name": "test-provider",
				"type": "proxy",
				"proxies": []interface{}{
					map[string]interface{}{
						"name":          "node1",
						"type":          "ss",
						"server":        "127.0.0.1",
						"password":      "secretpassword",
						"uuid":          "someuuid-123",
						"obfs-password": "obfs-secret",
						"alive":         true,
						"history": []interface{}{
							map[string]interface{}{
								"time":  "2026-07-16T12:00:00Z",
								"delay": 120,
							},
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

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/test-provider/nodes", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	bodyStr := rr.Body.String()
	for _, forbidden := range []string{"password", "uuid", "server", "obfs"} {
		if strings.Contains(strings.ToLower(bodyStr), forbidden) {
			t.Errorf("sensitive field %q leaked in response: %s", forbidden, bodyStr)
		}
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 node, got %d", len(list))
	}

	node := list[0]
	expectedKeys := map[string]bool{
		"tag":      true,
		"name":     true,
		"alive":    true,
		"tested":   true,
		"delay_ms": true,
	}

	for k := range node {
		if !expectedKeys[k] {
			t.Errorf("unexpected key %q in response", k)
		}
	}

	if node["tag"] != "node1" || node["name"] != "node1" || node["alive"] != true || node["tested"] != true || node["delay_ms"] != float64(120) {
		t.Errorf("unexpected node values: %+v", node)
	}
}

// TestProxyProviderNodes_UntestedNode проверяет логику для непроверенных нод:
// пустая история -> tested=false, delay_ms=0, alive=false.
// непустая история -> tested=true, delay_ms=последний delay, alive=alive && delay>0.
func TestProxyProviderNodes_UntestedNode(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)
	api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"name": "test-provider",
			"type": "proxy",
			"proxies": []interface{}{
				map[string]interface{}{
					"name":    "untested-node",
					"alive":   true,
					"history": []interface{}{},
				},
				map[string]interface{}{
					"name":  "tested-node",
					"alive": true,
					"history": []interface{}{
						map[string]interface{}{"delay": 80},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer mockClashAPI.Close()
	api.cfg.MihomoAPIURL = mockClashAPI.URL

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/test-provider/nodes", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var list []map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)

	if len(list) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(list))
	}

	// untested-node
	if list[0]["name"] != "untested-node" || list[0]["tested"] != false || list[0]["delay_ms"] != float64(0) || list[0]["alive"] != false {
		t.Errorf("unexpected untested-node state: %+v", list[0])
	}

	// tested-node
	if list[1]["name"] != "tested-node" || list[1]["tested"] != true || list[1]["delay_ms"] != float64(80) || list[1]["alive"] != true {
		t.Errorf("unexpected tested-node state: %+v", list[1])
	}
}

// TestProxyProviderNodes_InvalidName проверяет валидацию имени провайдера:
// недопустимое имя -> 400 bad request, исходящий запрос к Clash API не совершается.
func TestProxyProviderNodes_InvalidName(t *testing.T) {
	api, _ := newProxyProvidersTestAPI(t)
	api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

	apiCalls := 0
	mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer mockClashAPI.Close()
	api.cfg.MihomoAPIURL = mockClashAPI.URL

	req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/Bad_Name!/nodes", nil)
	rr := httptest.NewRecorder()
	api.ProxyProvidersRouter(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if apiCalls != 0 {
		t.Errorf("Clash API was called %d times, expected 0", apiCalls)
	}
}

// TestProxyProviderNodes_ErrorMapping проверяет обработку ошибок Clash API.
func TestProxyProviderNodes_ErrorMapping(t *testing.T) {
	t.Run("clash api 404 maps to 404", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

		mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockClashAPI.Close()
		api.cfg.MihomoAPIURL = mockClashAPI.URL

		req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/test-provider/nodes", nil)
		rr := httptest.NewRecorder()
		api.ProxyProvidersRouter(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("mihomo unreachable maps to 502", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

		closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		closedURL := closed.URL
		closed.Close()
		api.cfg.MihomoAPIURL = closedURL

		req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/test-provider/nodes", nil)
		rr := httptest.NewRecorder()
		api.ProxyProvidersRouter(rr, req)

		if rr.Code != http.StatusBadGateway {
			t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("clash api 500 maps to 502", func(t *testing.T) {
		api, _ := newProxyProvidersTestAPI(t)
		api.mihomoSvc = services.NewMihomoService(startFakeMihomo(t), "", api.cfg.DataDir)

		mockClashAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer mockClashAPI.Close()
		api.cfg.MihomoAPIURL = mockClashAPI.URL

		req := httptest.NewRequest(http.MethodGet, "/api/proxy-providers/test-provider/nodes", nil)
		rr := httptest.NewRecorder()
		api.ProxyProvidersRouter(rr, req)

		if rr.Code != http.StatusBadGateway {
			t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
