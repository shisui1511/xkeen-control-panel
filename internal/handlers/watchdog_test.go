package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestWatchdogStatusResponse_FromSnapshot(t *testing.T) {
	now := time.Unix(1710000000, 0)
	nextAttempt := now.Add(30 * time.Second)
	degraded := now.Add(-60 * time.Second)

	snapshot := services.WatchdogSnapshot{
		State:               services.WatchdogStateDegraded,
		ConsecutiveFailures: 5,
		DisarmAttempts:      3,
		LastDisarmError:     "iptables: rule not found",
		InterceptionActive:  true,
		InterceptionFamily:  "ipv4+ipv6",
		NextAttemptAt:       nextAttempt,
		DegradedAt:          degraded,
	}

	resp := newWatchdogStatusResponse(snapshot)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	expectedKeys := []string{
		"state",
		"consecutive_failures",
		"disarm_attempts",
		"last_disarm_error",
		"interception_active",
		"interception_family",
		"next_attempt_at",
		"degraded_at",
	}

	for _, key := range expectedKeys {
		if _, exists := raw[key]; !exists {
			t.Errorf("missing expected json key %q in response", key)
		}
	}

	if raw["state"] != "degraded" {
		t.Errorf("expected state to be 'degraded', got %v", raw["state"])
	}
	if raw["consecutive_failures"] != float64(5) {
		t.Errorf("expected consecutive_failures=5, got %v", raw["consecutive_failures"])
	}
	if raw["disarm_attempts"] != float64(3) {
		t.Errorf("expected disarm_attempts=3, got %v", raw["disarm_attempts"])
	}
	if raw["last_disarm_error"] != "iptables: rule not found" {
		t.Errorf("expected last_disarm_error='iptables: rule not found', got %v", raw["last_disarm_error"])
	}
	if raw["interception_active"] != true {
		t.Errorf("expected interception_active=true, got %v", raw["interception_active"])
	}
	if raw["interception_family"] != "ipv4+ipv6" {
		t.Errorf("expected interception_family='ipv4+ipv6', got %v", raw["interception_family"])
	}
	if raw["next_attempt_at"] != float64(nextAttempt.Unix()) {
		t.Errorf("expected next_attempt_at=%d, got %v", nextAttempt.Unix(), raw["next_attempt_at"])
	}
	if raw["degraded_at"] != float64(degraded.Unix()) {
		t.Errorf("expected degraded_at=%d, got %v", degraded.Unix(), raw["degraded_at"])
	}

	// Test zero time produces 0 Unix timestamp
	zeroSnapshot := services.WatchdogSnapshot{
		State: services.WatchdogStateArmed,
	}
	zeroResp := newWatchdogStatusResponse(zeroSnapshot)
	zeroData, err := json.Marshal(zeroResp)
	if err != nil {
		t.Fatalf("json.Marshal zero failed: %v", err)
	}
	var zeroRaw map[string]interface{}
	if err := json.Unmarshal(zeroData, &zeroRaw); err != nil {
		t.Fatalf("json.Unmarshal zero failed: %v", err)
	}
	if zeroRaw["next_attempt_at"] != float64(0) {
		t.Errorf("expected zero next_attempt_at=0, got %v", zeroRaw["next_attempt_at"])
	}
	if zeroRaw["degraded_at"] != float64(0) {
		t.Errorf("expected zero degraded_at=0, got %v", zeroRaw["degraded_at"])
	}
}

func TestWatchdogStatus_ReturnsSnapshot(t *testing.T) {
	wd := services.NewWatchdogService(nil, "", "")
	api := &API{watchdogSvc: wd}

	req := httptest.NewRequest(http.MethodGet, "/api/service/watchdog/status", nil)
	rec := httptest.NewRecorder()

	api.WatchdogStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Success bool                   `json:"success"`
		Data    WatchdogStatusResponse `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !envelope.Success {
		t.Errorf("expected envelope.Success=true, got false")
	}
	if envelope.Data.State != "armed" {
		t.Errorf("expected state='armed', got %q", envelope.Data.State)
	}
}

func TestWatchdogReset_CooldownRejectsSecondCall(t *testing.T) {
	wd := services.NewWatchdogService(nil, "", "")
	api := &API{watchdogSvc: wd}

	// First POST should succeed with 200
	req1 := httptest.NewRequest(http.MethodPost, "/api/service/watchdog/reset", nil)
	rec1 := httptest.NewRecorder()
	api.WatchdogReset(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected first reset status 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var env1 struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &env1); err != nil {
		t.Fatalf("failed to parse first response: %v", err)
	}
	if !env1.Success {
		t.Errorf("expected first call Success=true")
	}

	// Immediate second POST should be rejected with 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/service/watchdog/reset", nil)
	rec2 := httptest.NewRecorder()
	api.WatchdogReset(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected second reset status 409 Conflict, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var env2 struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &env2); err != nil {
		t.Fatalf("failed to parse second response: %v", err)
	}
	if env2.Success {
		t.Errorf("expected second call Success=false")
	}
}

func TestWatchdogReset_MethodGuard(t *testing.T) {
	wd := services.NewWatchdogService(nil, "", "")
	api := &API{watchdogSvc: wd}

	req := httptest.NewRequest(http.MethodGet, "/api/service/watchdog/reset", nil)
	rec := httptest.NewRecorder()

	api.WatchdogReset(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 Method Not Allowed, got %d: %s", rec.Code, rec.Body.String())
	}
}
