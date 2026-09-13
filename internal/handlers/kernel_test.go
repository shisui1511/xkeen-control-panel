package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
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

func buildKernelStubBinary(t *testing.T, name, output string) string {
	t.Helper()
	tmpDir := t.TempDir()

	binPath := filepath.Join(tmpDir, name)
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", output)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub script: %v", err)
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

	kernelSvc := services.NewKernelService(t.TempDir())

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

func TestKernelHandlers_Validation(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	// 1. KernelList: POST -> 405
	reqListPost := httptest.NewRequest(http.MethodPost, "/api/kernels", nil)
	recListPost := httptest.NewRecorder()
	api.KernelList(recListPost, reqListPost)
	if recListPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST KernelList, got %d", recListPost.Code)
	}

	// 2. KernelStatus: POST -> 405, nonexistent -> 404
	reqStatusPost := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/status", nil)
	recStatusPost := httptest.NewRecorder()
	api.KernelStatus(recStatusPost, reqStatusPost)
	if recStatusPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST KernelStatus, got %d", recStatusPost.Code)
	}

	reqStatusGhost := httptest.NewRequest(http.MethodGet, "/api/kernels/ghost/status", nil)
	recStatusGhost := httptest.NewRecorder()
	api.KernelStatus(recStatusGhost, reqStatusGhost)
	if recStatusGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelStatus, got %d", recStatusGhost.Code)
	}

	// 3. KernelCheck: GET -> 405, nonexistent -> 404
	reqCheckGet := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/check", nil)
	recCheckGet := httptest.NewRecorder()
	api.KernelCheck(recCheckGet, reqCheckGet)
	if recCheckGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET KernelCheck, got %d", recCheckGet.Code)
	}

	reqCheckGhost := httptest.NewRequest(http.MethodPost, "/api/kernels/ghost/check", nil)
	recCheckGhost := httptest.NewRecorder()
	api.KernelCheck(recCheckGhost, reqCheckGhost)
	if recCheckGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelCheck, got %d", recCheckGhost.Code)
	}

	// 4. KernelInstall: GET -> 405, nonexistent -> 404
	reqInstallGet := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/install", nil)
	recInstallGet := httptest.NewRecorder()
	api.KernelInstall(recInstallGet, reqInstallGet)
	if recInstallGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET KernelInstall, got %d", recInstallGet.Code)
	}

	reqInstallGhost := httptest.NewRequest(http.MethodPost, "/api/kernels/ghost/install", nil)
	recInstallGhost := httptest.NewRecorder()
	api.KernelInstall(recInstallGhost, reqInstallGhost)
	if recInstallGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelInstall, got %d", recInstallGhost.Code)
	}

	// 5. KernelChannel: GET -> 405, bad JSON -> 400, nonexistent -> 404
	reqChanGet := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/channel", nil)
	recChanGet := httptest.NewRecorder()
	api.KernelChannel(recChanGet, reqChanGet)
	if recChanGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET KernelChannel, got %d", recChanGet.Code)
	}

	reqChanBad := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/channel", strings.NewReader("{bad"))
	recChanBad := httptest.NewRecorder()
	api.KernelChannel(recChanBad, reqChanBad)
	if recChanBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON KernelChannel, got %d", recChanBad.Code)
	}

	reqChanGhost := httptest.NewRequest(http.MethodPost, "/api/kernels/ghost/channel", strings.NewReader(`{"channel":"stable"}`))
	recChanGhost := httptest.NewRecorder()
	api.KernelChannel(recChanGhost, reqChanGhost)
	if recChanGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelChannel, got %d", recChanGhost.Code)
	}

	// An invalid channel value on a real kernel must be a 400, not the same 404
	// used for an unknown kernel name — the two failures have different causes.
	reqChanInvalid := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/channel", strings.NewReader(`{"channel":"nightly"}`))
	recChanInvalid := httptest.NewRecorder()
	api.KernelChannel(recChanInvalid, reqChanInvalid)
	if recChanInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid channel value, got %d", recChanInvalid.Code)
	}

	// 6. KernelRollback: GET -> 405, nonexistent -> 404
	reqRollGet := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/rollback", nil)
	recRollGet := httptest.NewRecorder()
	api.KernelRollback(recRollGet, reqRollGet)
	if recRollGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET KernelRollback, got %d", recRollGet.Code)
	}

	reqRollGhost := httptest.NewRequest(http.MethodPost, "/api/kernels/ghost/rollback", nil)
	recRollGhost := httptest.NewRecorder()
	api.KernelRollback(recRollGhost, reqRollGhost)
	if recRollGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelRollback, got %d", recRollGhost.Code)
	}

	// 7. KernelDownload: POST -> 405, nonexistent -> 404
	reqDownPost := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/download", nil)
	recDownPost := httptest.NewRecorder()
	api.KernelDownload(recDownPost, reqDownPost)
	if recDownPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST KernelDownload, got %d", recDownPost.Code)
	}

	reqDownGhost := httptest.NewRequest(http.MethodGet, "/api/kernels/ghost/download", nil)
	recDownGhost := httptest.NewRecorder()
	api.KernelDownload(recDownGhost, reqDownGhost)
	if recDownGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelDownload, got %d", recDownGhost.Code)
	}

	// 8. KernelDebug: POST -> 405
	reqDbgPost := httptest.NewRequest(http.MethodPost, "/api/kernels/debug", nil)
	recDbgPost := httptest.NewRecorder()
	api.KernelDebug(recDbgPost, reqDbgPost)
	if recDbgPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST KernelDebug, got %d", recDbgPost.Code)
	}

	// 9. KernelUpload: GET -> 405, ghost -> 404
	reqUpGet := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/upload", nil)
	recUpGet := httptest.NewRecorder()
	api.KernelUpload(recUpGet, reqUpGet)
	if recUpGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET KernelUpload, got %d", recUpGet.Code)
	}

	reqUpGhost := httptest.NewRequest(http.MethodPost, "/api/kernels/ghost/upload", nil)
	recUpGhost := httptest.NewRecorder()
	api.KernelUpload(recUpGhost, reqUpGhost)
	if recUpGhost.Code != http.StatusNotFound {
		t.Errorf("expected 404 for ghost KernelUpload, got %d", recUpGhost.Code)
	}
}

func TestKernelUpload_SuccessAndValidation(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	// Test upload with invalid file (not ELF)
	bodyNonElf := &bytes.Buffer{}
	writerNonElf := multipart.NewWriter(bodyNonElf)
	partNonElf, _ := writerNonElf.CreateFormFile("file", "xray")
	_, _ = partNonElf.Write([]byte("not an elf binary"))
	_ = writerNonElf.Close()

	reqNonElf := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/upload", bodyNonElf)
	reqNonElf.Header.Set("Content-Type", writerNonElf.FormDataContentType())
	recNonElf := httptest.NewRecorder()
	api.KernelUpload(recNonElf, reqNonElf)
	if recNonElf.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-ELF upload, got %d: %s", recNonElf.Code, recNonElf.Body.String())
	}

	// Test upload with valid ELF magic header
	bodyElf := &bytes.Buffer{}
	writerElf := multipart.NewWriter(bodyElf)
	partElf, _ := writerElf.CreateFormFile("file", "xray")
	elfContent := append([]byte{0x7f, 'E', 'L', 'F'}, []byte("\n#!/bin/sh\necho 'Xray 1.8.25'\n")...)
	_, _ = partElf.Write(elfContent)
	_ = writerElf.Close()

	reqElf := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/upload", bodyElf)
	reqElf.Header.Set("Content-Type", writerElf.FormDataContentType())
	recElf := httptest.NewRecorder()
	api.KernelUpload(recElf, reqElf)
	if recElf.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid ELF upload, got %d: %s", recElf.Code, recElf.Body.String())
	}

	var resp struct {
		Data services.KernelInfo `json:"data"`
	}
	if err := json.NewDecoder(recElf.Body).Decode(&resp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !resp.Data.HasBackup {
		t.Errorf("expected HasBackup=true after upload")
	}
	if resp.Data.Status != "done" {
		t.Errorf("expected Status=done, got %s", resp.Data.Status)
	}
}

