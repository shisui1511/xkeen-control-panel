package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// newTestAPI creates a minimal API instance suitable for config handler tests.
func newTestAPI(t *testing.T, allowedDir string) *API {
	t.Helper()
	cfg := &config.Config{
		XRayConfigDir: allowedDir,
		AllowedRoots:  []string{allowedDir},
	}
	return &API{
		cfg:       cfg,
		configSvc: services.NewConfigService(allowedDir),
		pathVal:   utils.NewPathValidator(cfg.AllowedRoots),
	}
}

// TestConfigSaveBodyLimit verifies that ConfigSave returns 413 when the body exceeds 1 MB.
func TestConfigSaveBodyLimit(t *testing.T) {
	tmpDir := t.TempDir()
	api := newTestAPI(t, tmpDir)

	// Create a target file within the allowed directory
	targetPath := filepath.Join(tmpDir, "test.json")
	if err := os.WriteFile(targetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Build a body that exceeds maxConfigBytes (1 MB + 1 byte)
	body := bytes.Repeat([]byte("x"), maxConfigBytes+1)

	req := httptest.NewRequest(http.MethodPost, "/api/config/save?path="+targetPath, bytes.NewReader(body))
	rr := httptest.NewRecorder()

	api.ConfigSave(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "too large") {
		t.Errorf("expected 'too large' in response body, got: %s", rr.Body.String())
	}
}

// TestConfigSaveBodyLimitOK verifies that ConfigSave succeeds when body is within 1 MB.
func TestConfigSaveBodyLimitOK(t *testing.T) {
	tmpDir := t.TempDir()
	api := newTestAPI(t, tmpDir)

	targetPath := filepath.Join(tmpDir, "ok.json")
	if err := os.WriteFile(targetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Body well within limit
	body := []byte(`{"key": "value"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/config/save?path="+targetPath, bytes.NewReader(body))
	rr := httptest.NewRecorder()

	api.ConfigSave(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
