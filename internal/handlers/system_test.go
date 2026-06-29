package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/cert"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestSystemStats_NoProc(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		XRayConfigDir:   tmpDir,
		MihomoConfigDir: tmpDir,
		AllowedRoots:    []string{tmpDir},
	}

	api := &API{
		cfg:     cfg,
		pathVal: utils.NewPathValidator(cfg.AllowedRoots),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/stats", nil)
	rr := httptest.NewRecorder()

	api.SystemStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}

	var stats SystemStats
	if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Hostname should not be empty (unless OS fails completely)
	if stats.Hostname == "" {
		t.Log("Hostname is empty (could be environment limitation)")
	}

	// Go runtime stats should be populated
	if stats.GoRuntime.GoVersion == "" {
		t.Error("expected GoVersion to be populated")
	}

	// Disk space stats should be populated (either real or fallback mock)
	if stats.Disk.Total == 0 {
		t.Error("expected Disk.Total to be greater than 0")
	}
	if stats.Disk.Free == 0 {
		t.Error("expected Disk.Free to be greater than 0")
	}
	if stats.Disk.Used != stats.Disk.Total-stats.Disk.Free {
		t.Errorf("expected Disk.Used to be %d, got %d", stats.Disk.Total-stats.Disk.Free, stats.Disk.Used)
	}
}

func TestCountDirLines(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Empty dir
	if cnt := countDirLines(tmpDir); cnt != 0 {
		t.Errorf("expected 0 lines, got %d", cnt)
	}

	// 2. Dir with files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	file3 := filepath.Join(tmpDir, "file3.txt") // Empty file (0 bytes)

	if err := os.WriteFile(file1, []byte("line1\nline2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file3, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// File 1 has 1 newline, no trailing newline -> count is 2 lines.
	// File 2 has 3 newlines, with trailing newline -> count is 3 lines.
	// File 3 is empty -> count is 0 lines.
	// Total expected = 5 lines.

	expected := 5
	if cnt := countDirLines(tmpDir); cnt != expected {
		t.Errorf("expected %d lines, got %d", expected, cnt)
	}
}

func TestSystemStats_SSLCertDays(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	// 1. HTTPS disabled
	cfg := &config.Config{
		XRayConfigDir:   tmpDir,
		MihomoConfigDir: tmpDir,
		AllowedRoots:    []string{tmpDir},
		HTTPS: config.HTTPSConfig{
			Enabled:  false,
			CertPath: certPath,
			KeyPath:  keyPath,
		},
	}
	api := &API{
		cfg:     cfg,
		pathVal: utils.NewPathValidator(cfg.AllowedRoots),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/stats", nil)
	rr := httptest.NewRecorder()
	api.SystemStats(rr, req)

	var stats SystemStats
	if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if stats.SSLCertDays != -1 {
		t.Errorf("expected SSLCertDays = -1 when HTTPS disabled, got %d", stats.SSLCertDays)
	}

	// 2. HTTPS enabled but cert file missing
	cfg.HTTPS.Enabled = true
	rr2 := httptest.NewRecorder()
	api.SystemStats(rr2, req)
	var stats2 SystemStats
	if err := json.Unmarshal(rr2.Body.Bytes(), &stats2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if stats2.SSLCertDays != -1 {
		t.Errorf("expected SSLCertDays = -1 when cert file missing, got %d", stats2.SSLCertDays)
	}

	// 3. HTTPS enabled with valid cert
	if err := cert.GenerateSelfSigned(certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	// Clear cache by manually setting cache time to 2 minutes ago or creating a new API instance
	api = &API{
		cfg:     cfg,
		pathVal: utils.NewPathValidator(cfg.AllowedRoots),
	}

	rr3 := httptest.NewRecorder()
	api.SystemStats(rr3, req)
	var stats3 SystemStats
	if err := json.Unmarshal(rr3.Body.Bytes(), &stats3); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// The generated cert by GenerateSelfSigned has 365 days of validity.
	// Since we calculate it, it should be around 364-365 days.
	if stats3.SSLCertDays < 360 || stats3.SSLCertDays > 366 {
		t.Errorf("expected SSLCertDays around 365, got %d", stats3.SSLCertDays)
	}
}
