package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

func newSettingsTestAPI(t *testing.T) *API {
	t.Helper()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		Port:         8090,
		ConfigPath:   cfgPath,
		AllowedRoots: []string{tmpDir},
		DevMode:      false,
		HTTPS: config.HTTPSConfig{
			Enabled: false,
		},
	}
	_ = config.Save(cfgPath, cfg)

	return &API{
		cfg: cfg,
	}
}

func TestSettingsGet(t *testing.T) {
	api := newSettingsTestAPI(t)

	// 1. Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	recPost := httptest.NewRecorder()
	api.SettingsGet(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. GET returns settings
	reqGet := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recGet := httptest.NewRecorder()
	api.SettingsGet(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}

	var resp struct {
		Success bool             `json:"success"`
		Data    SettingsResponse `json:"data"`
	}
	if err := json.NewDecoder(recGet.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Data.Port != 8090 {
		t.Errorf("expected port 8090, got %d", resp.Data.Port)
	}
}

func TestSettingsHTTPS(t *testing.T) {
	api := newSettingsTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/settings/https", nil)
	recGet := httptest.NewRecorder()
	api.SettingsHTTPS(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/settings/https", bytes.NewReader([]byte("{invalid")))
	recBadJSON := httptest.NewRecorder()
	api.SettingsHTTPS(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Valid toggle HTTPS enabled
	body, _ := json.Marshal(map[string]bool{"enabled": true})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/settings/https", bytes.NewReader(body))
	recGood := httptest.NewRecorder()
	api.SettingsHTTPS(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good HTTPS toggle, got %d", recGood.Code)
	}
	if !api.cfg.HTTPS.Enabled {
		t.Error("expected HTTPS.Enabled to be true")
	}
}

func TestSettingsDevMode(t *testing.T) {
	api := newSettingsTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/settings/dev-mode", nil)
	recGet := httptest.NewRecorder()
	api.SettingsDevMode(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/settings/dev-mode", bytes.NewReader([]byte("{invalid")))
	recBadJSON := httptest.NewRecorder()
	api.SettingsDevMode(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Valid toggle DevMode enabled
	body, _ := json.Marshal(map[string]bool{"enabled": true})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/settings/dev-mode", bytes.NewReader(body))
	recGood := httptest.NewRecorder()
	api.SettingsDevMode(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good DevMode toggle, got %d", recGood.Code)
	}
	if !api.cfg.DevMode {
		t.Error("expected DevMode to be true")
	}
}
