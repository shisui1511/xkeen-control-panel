package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func newTrafficTestAPI(t *testing.T) (*API, *services.TrafficQuotaService) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		AllowedRoots: []string{tmpDir},
	}
	tqSvc := services.NewTrafficQuotaService(tmpDir, "http://localhost:9090", "")
	return &API{
		cfg:             cfg,
		trafficQuotaSvc: tqSvc,
	}, tqSvc
}

func TestTrafficQuotaList_Empty(t *testing.T) {
	api, _ := newTrafficTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/traffic/quotas", nil)
	rr := httptest.NewRecorder()

	api.TrafficQuotaList(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var list []services.TrafficQuota
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(list) != 0 {
		t.Errorf("expected 0 quotas, got %d", len(list))
	}
}

func TestTrafficQuotaList_MethodNotAllowed(t *testing.T) {
	api, _ := newTrafficTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas", nil)
	rr := httptest.NewRecorder()

	api.TrafficQuotaList(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestTrafficQuotaAdd_ValidAndInvalid(t *testing.T) {
	api, _ := newTrafficTestAPI(t)

	// 1. Valid Add
	payload := `{"name": "Test Limit", "limit_bytes": 1048576, "target_type": "global"}`
	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/add", bytes.NewBufferString(payload))
	rr := httptest.NewRecorder()

	api.TrafficQuotaAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var created services.TrafficQuota
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if created.ID == "" || created.Name != "Test Limit" || created.LimitBytes != 1048576 {
		t.Errorf("unexpected created quota: %+v", created)
	}

	// 2. Invalid Add (no name)
	badPayload1 := `{"limit_bytes": 1048576, "target_type": "global"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/add", bytes.NewBufferString(badPayload1))
	rr1 := httptest.NewRecorder()
	api.TrafficQuotaAdd(rr1, req1)
	if rr1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", rr1.Code)
	}

	// 3. Invalid Add (negative limit)
	badPayload2 := `{"name": "Limit", "limit_bytes": -10, "target_type": "global"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/add", bytes.NewBufferString(badPayload2))
	rr2 := httptest.NewRecorder()
	api.TrafficQuotaAdd(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative limit, got %d", rr2.Code)
	}
}

func TestTrafficQuotaGet_ValidAndInvalid(t *testing.T) {
	api, tqSvc := newTrafficTestAPI(t)

	q := &services.TrafficQuota{
		Name:       "Get Me",
		LimitBytes: 2048,
		Period:     "daily",
		Enabled:    true,
	}
	if err := tqSvc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	// 1. Valid Get
	req := httptest.NewRequest(http.MethodGet, "/api/traffic/quotas/get?id="+q.ID, nil)
	rr := httptest.NewRecorder()
	api.TrafficQuotaGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var got services.TrafficQuota
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if got.ID != q.ID || got.Name != "Get Me" {
		t.Errorf("unexpected quota: %+v", got)
	}

	// 2. Invalid Get (not found)
	req1 := httptest.NewRequest(http.MethodGet, "/api/traffic/quotas/get?id=nonexistent", nil)
	rr1 := httptest.NewRecorder()
	api.TrafficQuotaGet(rr1, req1)
	if rr1.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr1.Code)
	}

	// 3. Invalid Get (empty id)
	req2 := httptest.NewRequest(http.MethodGet, "/api/traffic/quotas/get", nil)
	rr2 := httptest.NewRecorder()
	api.TrafficQuotaGet(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr2.Code)
	}
}

func TestTrafficQuotaUpdate(t *testing.T) {
	api, tqSvc := newTrafficTestAPI(t)

	q := &services.TrafficQuota{Name: "Old Name", LimitBytes: 1000}
	if err := tqSvc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	payload := `{"name": "New Name", "limit_bytes": 5000}`
	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/update?id="+q.ID, bytes.NewBufferString(payload))
	rr := httptest.NewRecorder()

	api.TrafficQuotaUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify update in service
	updated, ok := tqSvc.GetQuota(q.ID)
	if !ok {
		t.Fatal("quota not found after update")
	}
	if updated.Name != "New Name" || updated.LimitBytes != 5000 {
		t.Errorf("expected updated values, got: %+v", updated)
	}
}

func TestTrafficQuotaDelete(t *testing.T) {
	api, tqSvc := newTrafficTestAPI(t)

	q := &services.TrafficQuota{Name: "To Delete", LimitBytes: 1000}
	if err := tqSvc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/delete?id="+q.ID, nil)
	rr := httptest.NewRecorder()

	api.TrafficQuotaDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	_, ok := tqSvc.GetQuota(q.ID)
	if ok {
		t.Error("expected quota to be deleted, but it still exists")
	}
}

func TestTrafficQuotaSetEnabled(t *testing.T) {
	api, tqSvc := newTrafficTestAPI(t)

	q := &services.TrafficQuota{Name: "Toggle Me", LimitBytes: 1000, Enabled: true}
	if err := tqSvc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/enabled?id="+q.ID+"&enabled=false", nil)
	rr := httptest.NewRecorder()

	api.TrafficQuotaSetEnabled(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	updated, ok := tqSvc.GetQuota(q.ID)
	if !ok || updated.Enabled {
		t.Errorf("expected Enabled=false, got ok=%t, enabled=%t", ok, updated.Enabled)
	}
}

func TestTrafficQuotaReset(t *testing.T) {
	api, tqSvc := newTrafficTestAPI(t)

	q := &services.TrafficQuota{Name: "Reset Me", LimitBytes: 1000, CurrentBytes: 500}
	if err := tqSvc.AddQuota(q); err != nil {
		t.Fatalf("AddQuota failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/traffic/quotas/reset?id="+q.ID, nil)
	rr := httptest.NewRecorder()

	api.TrafficQuotaReset(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	updated, ok := tqSvc.GetQuota(q.ID)
	if !ok || updated.CurrentBytes != 0 {
		t.Errorf("expected CurrentBytes=0, got ok=%t, CurrentBytes=%d", ok, updated.CurrentBytes)
	}
}

func TestTrafficStats_Alerts_Clear(t *testing.T) {
	api, _ := newTrafficTestAPI(t)

	// 1. Traffic Stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/traffic/stats", nil)
	rrStats := httptest.NewRecorder()
	api.TrafficStats(rrStats, reqStats)
	if rrStats.Code != http.StatusOK {
		t.Errorf("expected 200 for stats, got %d", rrStats.Code)
	}

	// 2. Traffic Alerts
	reqAlerts := httptest.NewRequest(http.MethodGet, "/api/traffic/alerts", nil)
	rrAlerts := httptest.NewRecorder()
	api.TrafficAlerts(rrAlerts, reqAlerts)
	if rrAlerts.Code != http.StatusOK {
		t.Errorf("expected 200 for alerts, got %d", rrAlerts.Code)
	}

	// 3. Clear Alerts
	reqClear := httptest.NewRequest(http.MethodPost, "/api/traffic/alerts/clear", nil)
	rrClear := httptest.NewRecorder()
	api.TrafficAlertsClear(rrClear, reqClear)
	if rrClear.Code != http.StatusOK {
		t.Errorf("expected 200 for clear alerts, got %d", rrClear.Code)
	}
}
