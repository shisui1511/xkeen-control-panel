package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func newSnapshotTestAPI(t *testing.T) (*API, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	configDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(configDir, 0755)

	_ = os.WriteFile(filepath.Join(configDir, "sample.txt"), []byte("sample-data"), 0644)

	snapSvc := services.NewSnapshotService(dataDir, []string{configDir})
	xkeenSvc := services.NewXKeenService("/bin/true", dataDir)

	api := &API{
		cfg: &config.Config{
			DataDir:      dataDir,
			AllowedRoots: []string{tmpDir},
		},
		pathVal:     utils.NewPathValidator([]string{tmpDir}),
		snapshotSvc: snapSvc,
		xkeenSvc:    xkeenSvc,
	}

	return api, dataDir, configDir
}

func TestSnapshotHandlers(t *testing.T) {
	api, _, _ := newSnapshotTestAPI(t)

	// 1. SnapshotList: 405 on POST, 200 on GET
	reqPostList := httptest.NewRequest(http.MethodPost, "/api/snapshots", nil)
	recPostList := httptest.NewRecorder()
	api.SnapshotList(recPostList, reqPostList)
	if recPostList.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST SnapshotList, got %d", recPostList.Code)
	}

	reqGetList := httptest.NewRequest(http.MethodGet, "/api/snapshots", nil)
	recGetList := httptest.NewRecorder()
	api.SnapshotList(recGetList, reqGetList)
	if recGetList.Code != http.StatusOK {
		t.Errorf("expected 200 for GET SnapshotList, got %d", recGetList.Code)
	}

	// 2. SnapshotCreate: 405 on GET, 200 on POST
	reqGetCreate := httptest.NewRequest(http.MethodGet, "/api/snapshots/create", nil)
	recGetCreate := httptest.NewRecorder()
	api.SnapshotCreate(recGetCreate, reqGetCreate)
	if recGetCreate.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET SnapshotCreate, got %d", recGetCreate.Code)
	}

	bodyCreate, _ := json.Marshal(map[string]string{"label": "test-backup"})
	reqPostCreate := httptest.NewRequest(http.MethodPost, "/api/snapshots/create", bytes.NewReader(bodyCreate))
	recPostCreate := httptest.NewRecorder()
	api.SnapshotCreate(recPostCreate, reqPostCreate)
	if recPostCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 for POST SnapshotCreate, got %d: %s", recPostCreate.Code, recPostCreate.Body.String())
	}

	var meta services.SnapshotMeta
	if err := json.Unmarshal(recPostCreate.Body.Bytes(), &meta); err != nil {
		t.Fatalf("failed to decode snapshot meta: %v", err)
	}
	if meta.ID == "" {
		t.Fatal("expected non-empty snapshot ID")
	}

	// 3. SnapshotDownload: 405 on POST, 400 on bad ID, 200 on valid ID
	reqPostDownload := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/download", nil)
	recPostDownload := httptest.NewRecorder()
	api.SnapshotDownload(recPostDownload, reqPostDownload)
	if recPostDownload.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST SnapshotDownload, got %d", recPostDownload.Code)
	}

	reqBadDownload := httptest.NewRequest(http.MethodGet, "/api/snapshots/bad..id/download", nil)
	recBadDownload := httptest.NewRecorder()
	api.SnapshotDownload(recBadDownload, reqBadDownload)
	if recBadDownload.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad ID Download, got %d", recBadDownload.Code)
	}

	reqGetDownload := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+meta.ID+"/download", nil)
	recGetDownload := httptest.NewRecorder()
	api.SnapshotDownload(recGetDownload, reqGetDownload)
	if recGetDownload.Code != http.StatusOK {
		t.Errorf("expected 200 for valid Download, got %d", recGetDownload.Code)
	}

	// 4. SnapshotRouter: dispatch restore, download, delete, and unknown
	reqRouteUnknown := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+meta.ID+"/unknown", nil)
	recRouteUnknown := httptest.NewRecorder()
	api.SnapshotRouter(recRouteUnknown, reqRouteUnknown)
	if recRouteUnknown.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", recRouteUnknown.Code)
	}

	reqRouteDownload := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+meta.ID+"/download", nil)
	recRouteDownload := httptest.NewRecorder()
	api.SnapshotRouter(recRouteDownload, reqRouteDownload)
	if recRouteDownload.Code != http.StatusOK {
		t.Errorf("expected 200 for routed download, got %d", recRouteDownload.Code)
	}

	// 5. SnapshotRestore: 405 on GET, 400 on bad ID, 200 on valid ID
	reqGetRestore := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+meta.ID+"/restore", nil)
	recGetRestore := httptest.NewRecorder()
	api.SnapshotRestore(recGetRestore, reqGetRestore)
	if recGetRestore.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET SnapshotRestore, got %d", recGetRestore.Code)
	}

	reqBadRestore := httptest.NewRequest(http.MethodPost, "/api/snapshots/bad;id/restore", nil)
	recBadRestore := httptest.NewRecorder()
	api.SnapshotRestore(recBadRestore, reqBadRestore)
	if recBadRestore.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad ID Restore, got %d", recBadRestore.Code)
	}

	reqPostRestore := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/restore", nil)
	recPostRestore := httptest.NewRecorder()
	api.SnapshotRestore(recPostRestore, reqPostRestore)
	if recPostRestore.Code != http.StatusOK {
		t.Errorf("expected 200 for valid Restore, got %d: %s", recPostRestore.Code, recPostRestore.Body.String())
	}

	// 6. SnapshotDelete: 405 on GET, 400 on bad ID, 200 on valid ID
	reqGetDelete := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+meta.ID+"/delete", nil)
	recGetDelete := httptest.NewRecorder()
	api.SnapshotDelete(recGetDelete, reqGetDelete)
	if recGetDelete.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET SnapshotDelete, got %d", recGetDelete.Code)
	}

	reqBadDelete := httptest.NewRequest(http.MethodPost, "/api/snapshots/bad;id/delete", nil)
	recBadDelete := httptest.NewRecorder()
	api.SnapshotDelete(recBadDelete, reqBadDelete)
	if recBadDelete.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad ID Delete, got %d", recBadDelete.Code)
	}

	reqPostDelete := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/delete", nil)
	recPostDelete := httptest.NewRecorder()
	api.SnapshotDelete(recPostDelete, reqPostDelete)
	if recPostDelete.Code != http.StatusOK {
		t.Errorf("expected 200 for valid Delete, got %d", recPostDelete.Code)
	}
}

func TestSnapshotUpload(t *testing.T) {
	api, _, _ := newSnapshotTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/snapshots/upload", nil)
	recGet := httptest.NewRecorder()
	api.SnapshotUpload(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET SnapshotUpload, got %d", recGet.Code)
	}

	// 2. Missing multipart file -> 400
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/snapshots/upload", bytes.NewReader([]byte("not multipart")))
	recEmpty := httptest.NewRecorder()
	api.SnapshotUpload(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad multipart, got %d", recEmpty.Code)
	}

	// 3. Invalid extension (.zip) -> 400
	bodyZip := &bytes.Buffer{}
	writerZip := multipart.NewWriter(bodyZip)
	partZip, _ := writerZip.CreateFormFile("backup", "test.zip")
	_, _ = partZip.Write([]byte("fake zip content"))
	_ = writerZip.Close()

	reqZip := httptest.NewRequest(http.MethodPost, "/api/snapshots/upload", bodyZip)
	reqZip.Header.Set("Content-Type", writerZip.FormDataContentType())
	recZip := httptest.NewRecorder()
	api.SnapshotUpload(recZip, reqZip)
	if recZip.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for .zip extension, got %d", recZip.Code)
	}

	// 4. Valid .tar.gz -> 200
	bodyTar := &bytes.Buffer{}
	writerTar := multipart.NewWriter(bodyTar)
	partTar, _ := writerTar.CreateFormFile("backup", "test.tar.gz")
	gw := gzip.NewWriter(partTar)
	tw := tar.NewWriter(gw)
	tw.Close()
	gw.Close()
	_ = writerTar.Close()

	reqTar := httptest.NewRequest(http.MethodPost, "/api/snapshots/upload", bodyTar)
	reqTar.Header.Set("Content-Type", writerTar.FormDataContentType())
	recTar := httptest.NewRecorder()
	api.SnapshotUpload(recTar, reqTar)
	if recTar.Code != http.StatusOK {
		t.Errorf("expected 200 for valid .tar.gz, got %d: %s", recTar.Code, recTar.Body.String())
	}
}

// TestSnapshotRestore_ApplyOutcome: восстановление снимка применяется к активному
// ядру через KernelApplier. Остановленное ядро не запускается, сбой рестарта не
// превращается в 500.
func TestSnapshotRestore_ApplyOutcome(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		restartErr  error
		wantOutcome services.ApplyOutcome
		wantCalls   int32
		wantErrText bool
	}{
		{name: "active kernel stopped", status: "stopped", wantOutcome: services.ApplySavedKernelStopped, wantCalls: 0},
		{name: "active kernel running", status: "running", wantOutcome: services.ApplyRestarted, wantCalls: 1},
		{name: "restart fails", status: "running", restartErr: errors.New("xkeen: boom"), wantOutcome: services.ApplyRestartFailed, wantCalls: 1, wantErrText: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _ := newSnapshotTestAPI(t)
			var restarts int32
			api.kernelApplier = services.NewKernelApplierFunc(
				func(name string) string {
					if name == "xray" {
						return tc.status
					}
					return "not_installed"
				},
				func() string { return "xray" },
				func() (string, error) {
					atomic.AddInt32(&restarts, 1)
					return "", tc.restartErr
				},
			)

			recCreate := httptest.NewRecorder()
			api.SnapshotCreate(recCreate, httptest.NewRequest(http.MethodPost, "/api/snapshots/create", nil))
			var meta services.SnapshotMeta
			if err := json.Unmarshal(recCreate.Body.Bytes(), &meta); err != nil || meta.ID == "" {
				t.Fatalf("create snapshot: %v: %s", err, recCreate.Body.String())
			}

			rec := httptest.NewRecorder()
			api.SnapshotRestore(rec, httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/restore", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			res := decodeApplyResult(t, rec.Body.Bytes())
			if res.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", res.Outcome, tc.wantOutcome)
			}
			if got := atomic.LoadInt32(&restarts); got != tc.wantCalls {
				t.Errorf("restarts = %d, want %d", got, tc.wantCalls)
			}
			if tc.wantErrText && res.Error == "" {
				t.Error("restart_failed must carry an error reason")
			}
		})
	}
}

// TestSnapshotUploadRestore_SkipsStoplistNames (WR-04): загруженный архив с
// файлами-стоп-словами XKeen в корне каталога Xray восстанавливается без них, а
// ответ перечисляет пропущенные имена; поля ApplyResult остаются на верхнем
// уровне.
func TestSnapshotUploadRestore_SkipsStoplistNames(t *testing.T) {
	api, _, configDir := newSnapshotTestAPI(t)
	api.snapshotSvc.SetXrayRoot(configDir)
	api.kernelApplier = services.NewKernelApplierFunc(
		func(string) string { return "stopped" },
		func() string { return "xray" },
		func() (string, error) { return "", nil },
	)

	base := filepath.Base(configDir)
	var archive bytes.Buffer
	gw := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gw)
	for name, content := range map[string]string{
		base + "/05_routing.json":       `{"routing":{}}`,
		base + "/04_outbounds.bak.json": `{"bak":true}`,
		base + "/x.old.json":            `{"old":true}`,
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gw.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("backup", "evil.tar.gz")
	_, _ = part.Write(archive.Bytes())
	_ = mw.Close()
	reqUp := httptest.NewRequest(http.MethodPost, "/api/snapshots/upload", body)
	reqUp.Header.Set("Content-Type", mw.FormDataContentType())
	recUp := httptest.NewRecorder()
	api.SnapshotUpload(recUp, reqUp)
	if recUp.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", recUp.Code, recUp.Body.String())
	}
	var meta services.SnapshotMeta
	if err := json.Unmarshal(recUp.Body.Bytes(), &meta); err != nil || meta.ID == "" {
		t.Fatalf("decode upload meta: %v: %s", err, recUp.Body.String())
	}

	rec := httptest.NewRecorder()
	api.SnapshotRestore(rec, httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/restore", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Outcome         services.ApplyOutcome `json:"outcome"`
			SkippedStoplist []string              `json:"skipped_stoplist"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode restore: %v: %s", err, rec.Body.String())
	}
	if env.Data.Outcome != services.ApplySavedKernelStopped {
		t.Errorf("outcome = %q, want %q", env.Data.Outcome, services.ApplySavedKernelStopped)
	}
	if got := strings.Join(env.Data.SkippedStoplist, ","); got != "04_outbounds.bak.json,x.old.json" {
		t.Errorf("skipped_stoplist = %q, want 04_outbounds.bak.json,x.old.json", got)
	}
	if _, err := os.Stat(filepath.Join(configDir, "05_routing.json")); err != nil {
		t.Errorf("обычный файл должен быть восстановлен: %v", err)
	}
	for _, n := range []string{"04_outbounds.bak.json", "x.old.json"} {
		if _, err := os.Stat(filepath.Join(configDir, n)); !os.IsNotExist(err) {
			t.Errorf("%s не должен появиться в корне Xray (err=%v)", n, err)
		}
	}
}
