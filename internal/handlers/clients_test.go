package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestSystemClientsHandler(t *testing.T) {
	cfg := &config.Config{}
	api := NewAPI(cfg, nil)

	// Mock RCI backend via httptest server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"host": [
				{
					"mac": "f8:4d:89:bf:ce:5b",
					"ip": "172.16.0.148",
					"name": "Iphone 12",
					"hostname": "avolkov",
					"active": true
				}
			]
		}`))
	}))
	defer ts.Close()

	resolver := services.NewClientResolver()
	resolver.SetRCIURL(ts.URL)
	api.SetClientResolver(resolver)

	// Test GET /api/system/clients
	req := httptest.NewRequest(http.MethodGet, "/api/system/clients", nil)
	w := httptest.NewRecorder()

	api.SystemClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Clients map[string]services.ClientInfo `json:"clients"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if len(resp.Clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(resp.Clients))
	}

	client, ok := resp.Clients["172.16.0.148"]
	if !ok {
		t.Fatalf("expected client 172.16.0.148 in response")
	}
	if client.DisplayName != "Iphone 12" {
		t.Errorf("expected DisplayName 'Iphone 12', got '%s'", client.DisplayName)
	}

	// Test MethodNotAllowed (POST)
	postReq := httptest.NewRequest(http.MethodPost, "/api/system/clients", nil)
	postW := httptest.NewRecorder()
	api.SystemClients(postW, postReq)
	if postW.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 for POST, got %d", postW.Code)
	}
}
