package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestNetworkIP_MethodNotAllowed(t *testing.T) {
	cfg := &config.Config{}
	api := NewAPI(cfg, nil)
	networkSvc := services.NewNetworkToolsService("")
	api.SetNetworkToolsService(networkSvc)

	reqPost := httptest.NewRequest(http.MethodPost, "/api/network/ip", nil)
	rrPost := httptest.NewRecorder()
	api.NetworkIP(rrPost, reqPost)

	if rrPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rrPost.Code)
	}
}

func TestNetworkIP_Success(t *testing.T) {
	cfg := &config.Config{}
	api := NewAPI(cfg, nil)
	networkSvc := services.NewNetworkToolsService("")
	api.SetNetworkToolsService(networkSvc)

	reqGet := httptest.NewRequest(http.MethodGet, "/api/network/ip", nil)
	rrGet := httptest.NewRecorder()
	api.NetworkIP(rrGet, reqGet)

	if rrGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rrGet.Code)
	}

	var res services.IPInfo
	if err := json.NewDecoder(rrGet.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
}
