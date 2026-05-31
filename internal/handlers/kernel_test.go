package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func buildKernelStubBinary(t *testing.T, name, output string) string {
	t.Helper()
	tmpDir := t.TempDir()

	src := `package main

import (
	"fmt"
)

func main() {
	fmt.Println("` + output + `")
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("write stub src: %v", err)
	}

	binPath := filepath.Join(tmpDir, name)
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub binary: %v\n%s", err, out)
	}

	return binPath
}

func newKernelTestAPI(t *testing.T) (*API, string) {
	t.Helper()

	// Build xray and mihomo stub binaries
	xrayBin := buildKernelStubBinary(t, "xray", "Xray 1.8.24 (Xray, Penetrates Everything.)")
	mihomoBin := buildKernelStubBinary(t, "mihomo", "Mihomo Version: v1.18.0")

	// Set PATH to find our stubs first
	binDir := filepath.Dir(xrayBin)
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)
	t.Cleanup(func() {
		os.Setenv("PATH", origPath)
	})

	tmpDir := t.TempDir()
	cfg := &config.Config{
		XRayConfigDir:   tmpDir,
		MihomoConfigDir: tmpDir,
		AllowedRoots:    []string{tmpDir, binDir},
	}

	// Move stubs to tmpDir so that we can check backups/rollbacks under allowed roots
	xrayDest := filepath.Join(tmpDir, "xray")
	mihomoDest := filepath.Join(tmpDir, "mihomo")
	if err := os.Rename(xrayBin, xrayDest); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(mihomoBin, mihomoDest); err != nil {
		t.Fatal(err)
	}

	// Update PATH again for the new locations
	os.Setenv("PATH", tmpDir+string(os.PathListSeparator)+origPath)

	kernelSvc := services.NewKernelService()

	return &API{
		cfg:       cfg,
		kernelSvc: kernelSvc,
		pathVal:   utils.NewPathValidator(cfg.AllowedRoots),
	}, tmpDir
}

func TestKernelList(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/kernels", nil)
	rr := httptest.NewRecorder()

	api.KernelList(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got false")
	}
}

func TestKernelCheck(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/check", nil)
	rr := httptest.NewRecorder()

	api.KernelCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelInstall(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/install", nil)
	rr := httptest.NewRecorder()

	api.KernelInstall(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelStatus(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/status", nil)
	rr := httptest.NewRecorder()

	api.KernelStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelChannel(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	body := `{"channel": "preview"}`
	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/channel", strings.NewReader(body))
	rr := httptest.NewRecorder()

	api.KernelChannel(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelRollback(t *testing.T) {
	api, tmpDir := newKernelTestAPI(t)

	// Create dummy backup
	backupDir := filepath.Join(tmpDir, ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "kernel.bak.12345")
	if err := os.WriteFile(backupPath, []byte("backup-content"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/rollback", nil)
	rr := httptest.NewRecorder()

	api.KernelRollback(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelDownload_Error(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	// Will fail with 500 because LatestVersion is empty/unknown
	req := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/download", nil)
	rr := httptest.NewRecorder()

	api.KernelDownload(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestKernelDebug(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/kernels/debug", nil)
	rr := httptest.NewRecorder()

	api.KernelDebug(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
