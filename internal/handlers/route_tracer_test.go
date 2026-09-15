package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestRouteTestHandler_MethodNotAllowed(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, "/api/rules/test", nil)
	rec := httptest.NewRecorder()

	api.RouteTest(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET to RouteTest, got %d", rec.Code)
	}
}

func TestRouteTestHandler_InvalidBody(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest(http.MethodPost, "/api/rules/test", bytes.NewReader([]byte("{invalid-json")))
	rec := httptest.NewRecorder()

	api.RouteTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestRouteTestHandler_EmptyTarget(t *testing.T) {
	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{
		"target": "   ",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rules/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.RouteTest(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty target, got %d", rec.Code)
	}
}

func TestRouteTestHandler_Success(t *testing.T) {
	tmpDir := t.TempDir()
	userRulesSvc := services.NewUserRulesService(tmpDir)
	_ = userRulesSvc.Save([]services.UserRule{
		{
			ID:      "r1",
			Type:    "domain",
			Value:   "test-domain.com",
			Target:  "proxy",
			Group:   "ProxyNL",
			Enabled: true,
		},
	})
	tracerSvc := services.NewRouteTracerService(userRulesSvc, nil, tmpDir)

	api := &API{
		routeTracerSvc: tracerSvc,
	}

	body, _ := json.Marshal(map[string]interface{}{
		"target": "test-domain.com",
		"port":   443,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rules/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.RouteTest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid RouteTest, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Success bool                      `json:"success"`
		Data    *services.RouteTraceResult `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success || resp.Data == nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Data.Target != "test-domain.com" || resp.Data.TargetGroup != "ProxyNL" || resp.Data.Source != "user_rule" {
		t.Errorf("unexpected trace result data: %+v", resp.Data)
	}
	if resp.Data.TraceTimeMs < 0 {
		t.Errorf("expected non-negative TraceTimeMs")
	}
}
