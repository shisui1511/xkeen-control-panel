package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// TestCapabilities_MihomoOffline verifies that when the Mihomo API is not
// reachable, the capabilities endpoint returns reachable=false without error.
func TestCapabilities_MihomoOffline(t *testing.T) {
	// Use a guaranteed-unreachable URL (port 1 is generally closed).
	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: "http://127.0.0.1:1",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()

	api.Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var envelope APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("expected success=true, got false: %v", envelope.Error)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var resp CapabilitiesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode capabilities data: %v", err)
	}

	if resp.Mihomo.Reachable {
		t.Error("expected Mihomo.Reachable=false when API is offline, got true")
	}
}

// TestCapabilities_MihomoOnline verifies that when the Mihomo API responds
// with 200 OK, the capabilities endpoint returns reachable=true.
func TestCapabilities_MihomoOnline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: ts.URL,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()

	api.Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var envelope APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("expected success=true, got false: %v", envelope.Error)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var resp CapabilitiesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode capabilities data: %v", err)
	}

	if !resp.Mihomo.Reachable {
		t.Error("expected Mihomo.Reachable=true when API is online, got false")
	}
}
