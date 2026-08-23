package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestMihomoDNSQuery_And_FlushFakeIP(t *testing.T) {
	// Setup mock Mihomo Clash API server
	mockMihomo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testsecret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/dns/query" {
			name := r.URL.Query().Get("name")
			qtype := r.URL.Query().Get("type")
			resp := map[string]interface{}{
				"Status": 0,
				"TC":     false,
				"RD":     true,
				"RA":     true,
				"AD":     false,
				"CD":     false,
				"Question": []map[string]interface{}{
					{"name": name, "type": 1},
				},
				"Answer": []map[string]interface{}{
					{"name": name, "type": qtype, "data": "1.2.3.4", "TTL": 60},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/cache/fakeip/flush" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockMihomo.Close()

	tmpDir := t.TempDir()
	configYAML := "external-controller: " + mockMihomo.Listener.Addr().String() + "\nsecret: testsecret\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: tmpDir,
			AllowedRoots:    []string{tmpDir},
		},
		pathVal:   utils.NewPathValidator([]string{tmpDir}),
		mihomoSvc: services.NewMihomoService("", "", tmpDir),
	}

	// 1. Test DNS Query
	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/dns/query?name=google.com&type=A", nil)
	rr := httptest.NewRecorder()
	api.MihomoDNSQuery(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var dnsResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &dnsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status, ok := dnsResp["Status"].(float64); !ok || status != 0 {
		t.Errorf("expected Status 0, got %v", dnsResp["Status"])
	}

	// 2. Test Flush Fake-IP
	reqFlush := httptest.NewRequest(http.MethodPost, "/api/mihomo/cache/fakeip/flush", nil)
	rrFlush := httptest.NewRecorder()
	api.MihomoFlushFakeIP(rrFlush, reqFlush)

	if rrFlush.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrFlush.Code, rrFlush.Body.String())
	}
}
