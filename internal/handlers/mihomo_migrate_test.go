package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestAPI_MihomoMigrateSocket(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configYAML := `port: 7890
external-controller: 0.0.0.0:9090
secret: "test-secret"
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mihomoSvc := services.NewMihomoService("", "", tmpDir)
	api := &API{
		cfg:       &config.Config{},
		mihomoSvc: mihomoSvc,
	}

	// 1. GET Preview
	t.Run("GET preview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/config/mihomo-migrate-socket", nil)
		rr := httptest.NewRecorder()
		api.MihomoMigrateSocket(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp struct {
			Data services.MigrationPreview `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode preview response: %v", err)
		}
		if resp.Data.CurrentController != "0.0.0.0:9090" {
			t.Errorf("CurrentController = %q, want '0.0.0.0:9090'", resp.Data.CurrentController)
		}
		if !resp.Data.IsInsecure {
			t.Errorf("IsInsecure = false, want true")
		}
	})

	// 2. POST Apply
	t.Run("POST apply", func(t *testing.T) {
		body := bytes.NewBufferString(`{"action": "apply"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/config/mihomo-migrate-socket", body)
		rr := httptest.NewRecorder()
		api.MihomoMigrateSocket(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
		}
		var resp struct {
			Data services.MigrationResult `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode result response: %v", err)
		}
		if !resp.Data.Success {
			t.Errorf("Success = false, want true")
		}
		if resp.Data.SocketPath != services.DefaultMihomoSocketPath {
			t.Errorf("SocketPath = %q, want %q", resp.Data.SocketPath, services.DefaultMihomoSocketPath)
		}
	})
}
