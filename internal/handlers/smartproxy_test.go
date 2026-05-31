package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func newSmartProxyTestAPI(t *testing.T) (*API, *services.SmartProxyService) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		AllowedRoots: []string{tmpDir},
	}
	spSvc := services.NewSmartProxyService(tmpDir, "http://localhost:9090")
	return &API{
		cfg:           cfg,
		smartProxySvc: spSvc,
		pathVal:       utils.NewPathValidator(cfg.AllowedRoots),
	}, spSvc
}

func TestSmartProxyList(t *testing.T) {
	// 1. Success
	api, _ := newSmartProxyTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/smartproxy", nil)
	rr := httptest.NewRecorder()
	api.SmartProxyList(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// 2. Service Unavailable
	apiNoSvc := &API{}
	rr2 := httptest.NewRecorder()
	apiNoSvc.SmartProxyList(rr2, req)
	if rr2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr2.Code)
	}
}

func TestSmartProxyGet(t *testing.T) {
	api, spSvc := newSmartProxyTestAPI(t)

	p := &services.Profile{
		Name:      "Test Get",
		GroupName: "group",
		ProxyName: "proxy",
	}
	spSvc.Add(p)
	id := p.ID

	// 1. Success Get
	req := httptest.NewRequest(http.MethodGet, "/api/smartproxy/get?id="+id, nil)
	rr := httptest.NewRecorder()
	api.SmartProxyGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got services.Profile
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test Get" {
		t.Errorf("expected 'Test Get', got %q", got.Name)
	}

	// 2. Missing ID
	req2 := httptest.NewRequest(http.MethodGet, "/api/smartproxy/get", nil)
	rr2 := httptest.NewRecorder()
	api.SmartProxyGet(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr2.Code)
	}

	// 3. Not Found
	req3 := httptest.NewRequest(http.MethodGet, "/api/smartproxy/get?id=nonexistent", nil)
	rr3 := httptest.NewRecorder()
	api.SmartProxyGet(rr3, req3)
	if rr3.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr3.Code)
	}
}

func TestSmartProxyAdd(t *testing.T) {
	api, _ := newSmartProxyTestAPI(t)

	// 1. Invalid payload
	req := httptest.NewRequest(http.MethodPost, "/api/smartproxy/add", strings.NewReader("bad json"))
	rr := httptest.NewRecorder()
	api.SmartProxyAdd(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Missing fields
	req2 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/add", strings.NewReader(`{"name": "only name"}`))
	rr2 := httptest.NewRecorder()
	api.SmartProxyAdd(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr2.Code)
	}

	// 3. Success Add
	payload := `{"name": "Valid Add", "group_name": "group", "proxy_name": "proxy"}`
	req3 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/add", strings.NewReader(payload))
	rr3 := httptest.NewRecorder()
	api.SmartProxyAdd(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	var created services.Profile
	if err := json.Unmarshal(rr3.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Valid Add" {
		t.Errorf("expected 'Valid Add', got %q", created.Name)
	}
}

func TestSmartProxyUpdate(t *testing.T) {
	api, spSvc := newSmartProxyTestAPI(t)

	p := &services.Profile{
		Name:      "Before",
		GroupName: "group",
		ProxyName: "proxy",
	}
	spSvc.Add(p)
	id := p.ID

	// 1. Missing ID
	req := httptest.NewRequest(http.MethodPost, "/api/smartproxy/update", strings.NewReader(`{"name":"After"}`))
	rr := httptest.NewRecorder()
	api.SmartProxyUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Not Found
	req2 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/update?id=nonexistent", strings.NewReader(`{"name":"After"}`))
	rr2 := httptest.NewRecorder()
	api.SmartProxyUpdate(rr2, req2)
	if rr2.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr2.Code)
	}

	// 3. Success
	payload := `{"name": "After", "group_name": "group", "proxy_name": "proxy"}`
	req3 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/update?id="+id, strings.NewReader(payload))
	rr3 := httptest.NewRecorder()
	api.SmartProxyUpdate(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	updated := spSvc.Get(id)
	if updated.Name != "After" {
		t.Errorf("expected updated name to be 'After', got %q", updated.Name)
	}
}

func TestSmartProxyDelete(t *testing.T) {
	api, spSvc := newSmartProxyTestAPI(t)

	p := &services.Profile{
		Name:      "To Delete",
		GroupName: "group",
		ProxyName: "proxy",
	}
	spSvc.Add(p)
	id := p.ID

	// 1. Missing ID
	req := httptest.NewRequest(http.MethodPost, "/api/smartproxy/delete", nil)
	rr := httptest.NewRecorder()
	api.SmartProxyDelete(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Success
	req2 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/delete?id="+id, nil)
	rr2 := httptest.NewRecorder()
	api.SmartProxyDelete(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr2.Code)
	}
	if spSvc.Get(id) != nil {
		t.Error("expected profile to be deleted")
	}
}

func TestSmartProxySetEnabled(t *testing.T) {
	api, spSvc := newSmartProxyTestAPI(t)

	p := &services.Profile{
		Name:      "Toggle",
		GroupName: "group",
		ProxyName: "proxy",
		Enabled:   true,
	}
	spSvc.Add(p)
	id := p.ID

	// 1. Missing ID
	req := httptest.NewRequest(http.MethodPost, "/api/smartproxy/enabled?enabled=false", nil)
	rr := httptest.NewRecorder()
	api.SmartProxySetEnabled(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}

	// 2. Success Enabled = false
	req2 := httptest.NewRequest(http.MethodPost, "/api/smartproxy/enabled?id="+id+"&enabled=false", nil)
	rr2 := httptest.NewRecorder()
	api.SmartProxySetEnabled(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr2.Code)
	}
	if spSvc.Get(id).Enabled {
		t.Error("expected profile to be disabled")
	}
}

func TestSmartProxyStatus(t *testing.T) {
	api, _ := newSmartProxyTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/smartproxy/status", nil)
	rr := httptest.NewRecorder()
	api.SmartProxyStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
