package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	return newKernelTestAPIWith(t, "", nil)
}

// defaultKernelReleases — локальный источник релизов по умолчанию.
func defaultKernelReleases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/releases/latest") {
		_, _ = w.Write([]byte(`{"tag_name":"v1.8.24"}`))
		return
	}
	_, _ = w.Write([]byte(`[{"tag_name":"v1.9.0-rc1","prerelease":true}]`))
}

// newKernelTestAPIWith — newKernelTestAPI с управляемой заглушкой xray и
// источником релизов. xrayScript == "" — заглушка по умолчанию (Xray 1.8.24);
// releases == nil — defaultKernelReleases.
func newKernelTestAPIWith(t *testing.T, xrayScript string, releases http.HandlerFunc) (*API, string) {
	t.Helper()

	// Build xray and mihomo stub binaries
	xrayBin := buildKernelStubBinary(t, "xray", "Xray 1.8.24 (Xray, Penetrates Everything.)")
	if xrayScript != "" {
		if err := os.WriteFile(xrayBin, []byte(xrayScript), 0755); err != nil {
			t.Fatalf("write xray stub: %v", err)
		}
	}
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

	// Проверка релизов идёт на локальный сервер, а не в реальный GitHub.
	if releases == nil {
		releases = defaultKernelReleases
	}
	releaseSrv := httptest.NewServer(releases)
	t.Cleanup(releaseSrv.Close)
	kernelSvc.SetReleaseSource(releaseSrv.URL, releaseSrv.Client())
	// Установка тоже не ходит в сеть: на amd64 у ядер нет ассетов, а загрузка
	// в тестах должна быть управляемой. Тесты, которым нужна своя, вызывают SetInstallSource.
	kernelSvc.SetInstallSource("arm64", func(context.Context, string, string) error {
		return errors.New("network is disabled in tests")
	})

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

// kernelStatusOf запрашивает GET /api/kernels/{name}/status и возвращает ядро.
func kernelStatusOf(t *testing.T, api *API, name string) services.KernelInfo {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/kernels/"+name+"/status", nil)
	rr := httptest.NewRecorder()
	api.KernelStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data services.KernelInfo `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func postKernelInstall(api *API, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/kernels/"+name+"/install", nil)
	rr := httptest.NewRecorder()
	api.KernelInstall(rr, req)
	return rr
}

// blockingInstall подменяет загрузку: она ждёт release и завершается ошибкой.
func blockingInstall(api *API) (release func()) {
	gate := make(chan struct{})
	api.kernelSvc.SetInstallSource("arm64", func(context.Context, string, string) error {
		<-gate
		return errors.New("stop")
	})
	return func() { close(gate) }
}

func waitKernelFailed(t *testing.T, api *API, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if k := kernelStatusOf(t, api, name); k.Status == "failed" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("kernel did not reach status failed in 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestKernelInstall_StageVisibleImmediately: 200 отдаётся, когда статус уже
// переходный, и первый же GET status не видит idle (KERN-02, D-09).
func TestKernelInstall_StageVisibleImmediately(t *testing.T) {
	api, _ := newKernelTestAPI(t)
	release := blockingInstall(api)

	rr := postKernelInstall(api, "xray")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data["status"] != "downloading" || resp.Data["stage"] != "starting" {
		t.Errorf("response data = %v, want status downloading and stage starting", resp.Data)
	}

	k := kernelStatusOf(t, api, "xray")
	if k.Status != "downloading" {
		t.Errorf("status right after POST = %q, want downloading", k.Status)
	}
	if k.Stage != "starting" && k.Stage != "downloading" {
		t.Errorf("stage right after POST = %q, want starting or downloading", k.Stage)
	}

	release()
	waitKernelFailed(t, api, "xray")
}

// TestKernelInstall_ReleaseLookupFailed: ядро без распознанной версии, источник
// релизов отвечает 403 — статус failed несёт error_kind release_lookup_failed,
// а не «Unsupported architecture» (G5-WR02, KERN-02).
func TestKernelInstall_ReleaseLookupFailed(t *testing.T) {
	// Заглушка xray, чья версия не распознаётся: запасного пути «переустановить
	// текущую версию» нет, версию может дать только проверка релиза.
	api, _ := newKernelTestAPIWith(t, "#!/bin/sh\nexit 1\n", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	})

	if rr := postKernelInstall(api, "xray"); rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	waitKernelFailed(t, api, "xray")

	req := httptest.NewRequest(http.MethodGet, "/api/kernels/xray/status", nil)
	rr := httptest.NewRecorder()
	api.KernelStatus(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, `"error_kind":"release_lookup_failed"`) {
		t.Errorf("status JSON lacks error_kind release_lookup_failed: %s", body)
	}
	if strings.Contains(body, "Unsupported architecture") {
		t.Errorf("message must not claim unsupported architecture: %s", body)
	}
}

// TestKernelInstall_Conflict409: второй POST при идущей установке — 409.
func TestKernelInstall_Conflict409(t *testing.T) {
	api, _ := newKernelTestAPI(t)
	release := blockingInstall(api)

	if rr := postKernelInstall(api, "xray"); rr.Code != http.StatusOK {
		t.Fatalf("first install: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	rr := postKernelInstall(api, "xray")
	if rr.Code != http.StatusConflict {
		t.Errorf("second install: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "install already in progress") {
		t.Errorf("unexpected 409 body: %s", rr.Body.String())
	}

	release()
	waitKernelFailed(t, api, "xray")
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

// TestKernelChannel_Recompute: смена канала синхронно перепроверяет релиз и
// возвращает в ответе пересчитанное ядро (KERN-01, D-08).
func TestKernelChannel_Recompute(t *testing.T) {
	api, _ := newKernelTestAPI(t)

	post := func(channel string) (string, string, string) {
		t.Helper()
		body := fmt.Sprintf(`{"channel": %q}`, channel)
		req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/channel", strings.NewReader(body))
		rr := httptest.NewRecorder()
		api.KernelChannel(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Success bool `json:"success"`
			Data    struct {
				Channel string `json:"channel"`
				Kernel  struct {
					LatestVersion string `json:"latest_version"`
					Status        string `json:"status"`
				} `json:"kernel"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success {
			t.Fatalf("expected success, got %s", rr.Body.String())
		}
		return resp.Data.Channel, resp.Data.Kernel.LatestVersion, resp.Data.Kernel.Status
	}

	ch, latest, status := post("preview")
	if ch != "preview" || latest != "1.9.0-rc1" || status != "idle" {
		t.Errorf("preview: channel=%q latest=%q status=%q", ch, latest, status)
	}
	ch, latest, status = post("stable")
	if ch != "stable" || latest != "1.8.24" || status != "idle" {
		t.Errorf("stable: channel=%q latest=%q status=%q", ch, latest, status)
	}
}

func TestKernelRollback(t *testing.T) {
	api, tmpDir := newKernelTestAPI(t)

	// Бэкап в формате с версией: xray.bak.<метка>.<версия>
	backupDir := filepath.Join(tmpDir, ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "xray.bak.1759100000.1.8.24")
	if err := os.WriteFile(backupPath, []byte("backup-content"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/rollback", nil)
	rr := httptest.NewRecorder()

	api.KernelRollback(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(backupPath); err == nil {
		t.Error("applied backup must be consumed")
	}
	if k := kernelStatusOf(t, api, "xray"); k.ResultKind != services.KernelResultRolledBack || k.Status != "done" {
		t.Errorf("status=%q result_kind=%q, want done/rolled_back", k.Status, k.ResultKind)
	}
}

// TestKernelRollback_NoBackup: без бэкапа откат — ошибка; чужой файл в .backup
// бэкапом не считается.
func TestKernelRollback_NoBackup(t *testing.T) {
	api, tmpDir := newKernelTestAPI(t)
	backupDir := filepath.Join(tmpDir, ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "notes.txt"), []byte("not a backup"), 0644); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	api.KernelRollback(rr, httptest.NewRequest(http.MethodPost, "/api/kernels/xray/rollback", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestKernelRollback_Conflict409: откат при идущей установке — 409, а не гонка.
func TestKernelRollback_Conflict409(t *testing.T) {
	api, _ := newKernelTestAPI(t)
	release := blockingInstall(api)

	if rr := postKernelInstall(api, "xray"); rr.Code != http.StatusOK {
		t.Fatalf("install: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	rr := httptest.NewRecorder()
	api.KernelRollback(rr, httptest.NewRequest(http.MethodPost, "/api/kernels/xray/rollback", nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("rollback while installing: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, _ := w.CreateFormFile("file", "xray")
	_, _ = part.Write([]byte("\x7fELF"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/kernels/xray/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	api.KernelUpload(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("upload while installing: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	release()
	waitKernelFailed(t, api, "xray")
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

// kernelListOf запрашивает GET /api/kernels и возвращает ядра по имени.
func kernelListOf(t *testing.T, api *API) map[string]services.KernelInfo {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/kernels", nil)
	rr := httptest.NewRecorder()
	api.KernelList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []services.KernelInfo `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]services.KernelInfo, len(resp.Data))
	for _, k := range resp.Data {
		out[k.Name] = k
	}
	return out
}

// waitKernelLatest опрашивает список, пока у ядра не появится latest_version.
func waitKernelLatest(t *testing.T, api *API, name string) services.KernelInfo {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		k := kernelListOf(t, api)[name]
		if k.LatestVersion != "" {
			return k
		}
		if time.Now().After(deadline) {
			t.Fatalf("latest_version у %s не появился за 5 с (автопроверка релиза не запущена)", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestKernelList_StaleStartVersionNoDowngradeOffer: симптом с роутера (G2) —
// при старте процесса версия не определилась (error), а установлена pre-release
// новее stable. Список сам получает релиз и не предлагает понижение.
func TestKernelList_StaleStartVersionNoDowngradeOffer(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	script := fmt.Sprintf("#!/bin/sh\nif [ ! -e %q ]; then : > %q; exit 1; fi\necho 'Xray 26.9.9 (Xray, Penetrates Everything.)'\n", marker, marker)
	releases := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			_, _ = w.Write([]byte(`{"tag_name":"v26.3.27"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}
	api, _ := newKernelTestAPIWith(t, script, releases)

	first := kernelListOf(t, api)["xray"]
	if first.HasUpdate {
		t.Errorf("первый ответ: has_update = true при пустом latest")
	}
	x := waitKernelLatest(t, api, "xray")
	if x.LatestVersion != "26.3.27" {
		t.Errorf("latest_version = %q, want 26.3.27", x.LatestVersion)
	}
	if x.CurrentVersion != "26.9.9" {
		t.Errorf("current_version = %q, want 26.9.9", x.CurrentVersion)
	}
	if x.HasUpdate {
		t.Errorf("has_update = true: предложено понижение pre-release на stable")
	}
	if !x.AheadOfLatest {
		t.Errorf("ahead_of_latest = false, want true")
	}
}

// TestKernelList_AutoCheckOncePerTTL: повторные GET в пределах TTL не порождают
// новых запросов к источнику релизов (лимит анонимного API).
func TestKernelList_AutoCheckOncePerTTL(t *testing.T) {
	var xrayHits, mihomoHits atomic.Int32
	releases := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "Xray-core"):
			xrayHits.Add(1)
		case strings.Contains(r.URL.Path, "mihomo"):
			mihomoHits.Add(1)
		}
		defaultKernelReleases(w, r)
	}
	api, _ := newKernelTestAPIWith(t, "", releases)

	for i := 0; i < 3; i++ {
		kernelListOf(t, api)
	}
	x := waitKernelLatest(t, api, "xray")
	m := waitKernelLatest(t, api, "mihomo")
	if x.LatestVersion == "" || m.LatestVersion == "" {
		t.Fatalf("latest_version не заполнен: xray=%q mihomo=%q", x.LatestVersion, m.LatestVersion)
	}
	// Дать возможным лишним проверкам дойти до сервера.
	time.Sleep(300 * time.Millisecond)
	kernelListOf(t, api)
	time.Sleep(100 * time.Millisecond)
	if n := xrayHits.Load(); n != 1 {
		t.Errorf("запросов релиза xray = %d, want 1", n)
	}
	if n := mihomoHits.Load(); n != 1 {
		t.Errorf("запросов релиза mihomo = %d, want 1", n)
	}
}
