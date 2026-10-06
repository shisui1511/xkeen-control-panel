package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

var snapshotIDRx = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// SnapshotRouter dispatches /api/snapshots/{id}/restore|download|delete
func (a *API) SnapshotRouter(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/restore"):
		a.SnapshotRestore(w, r)
	case strings.HasSuffix(path, "/download"):
		a.SnapshotDownload(w, r)
	case strings.HasSuffix(path, "/delete"):
		a.SnapshotDelete(w, r)
	default:
		a.errorResponse(w, "not found", http.StatusNotFound)
	}
}

func (a *API) SnapshotList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	list, err := a.snapshotSvc.List()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.jsonResponse(w, list)
}

func (a *API) SnapshotCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	// Ignore decode error — label is optional
	_ = json.NewDecoder(r.Body).Decode(&req)

	meta, err := a.snapshotSvc.Create(req.Label)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.jsonResponse(w, meta)
}

func (a *API) SnapshotRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	id = strings.TrimSuffix(id, "/restore")

	if !snapshotIDRx.MatchString(id) {
		a.errorResponse(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	// Восстановление файлов и рестарт — под общим замком жизненного цикла.
	if !a.tryLifecycleLock(w, r) {
		return
	}
	defer a.lifecycleMu.Unlock()

	skipped, err := a.snapshotSvc.Restore(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Снимок содержит каталог данных панели вместе с файлом состояния слоя, а слой
	// держит состояние в памяти: без перечитывания оно затёрло бы восстановленный
	// файл. ReloadFromDisk не берёт замок применения (порядок applyMu -> lifecycleMu,
	// lifecycleMu уже у нас) и сам просит сверку дрейфа.
	if a.configLayer != nil {
		if rerr := a.configLayer.ReloadFromDisk(); rerr != nil {
			log.Printf("snapshot restore: не удалось перечитать состояние слоя конфигурации: %v", rerr)
		}
	}
	a.layerRequestCheck()

	// Снимок возвращает каталоги обоих ядер: применяем к активному. Остановленное
	// ядро не запускается, сбой рестарта не откатывает восстановленные файлы и
	// приходит исходом restart_failed (HTTP 200).
	result := a.applyKernelLocked(services.ApplyTargetActive)
	a.ClearCapabilitiesCache()
	JSONSuccess(w, snapshotRestoreResponse{ApplyResult: result, SkippedStoplist: skipped})
}

// snapshotRestoreResponse — ответ восстановления: исход применения (поля
// ApplyResult на верхнем уровне, как раньше) и имена файлов корня каталога
// Xray, которые не восстановлены из-за стоп-списка XKeen.
type snapshotRestoreResponse struct {
	services.ApplyResult
	SkippedStoplist []string `json:"skipped_stoplist,omitempty"`
}

func (a *API) SnapshotUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (up to 10 MB)
	if err := r.ParseMultipartForm(10 * 1024 * 1024); err != nil {
		a.errorResponse(w, "Unable to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("backup")
	if err != nil {
		a.errorResponse(w, "Missing backup file in request: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := header.Filename
	if !strings.HasSuffix(strings.ToLower(filename), ".tar.gz") {
		a.errorResponse(w, "Invalid file format, only .tar.gz is allowed", http.StatusBadRequest)
		return
	}

	meta, err := a.snapshotSvc.SaveUploaded(file, filename)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, meta)
}

func (a *API) SnapshotDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	id = strings.TrimSuffix(id, "/download")

	if !snapshotIDRx.MatchString(id) {
		a.errorResponse(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	archPath, err := a.snapshotSvc.ArchivePath(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	f, err := os.Open(archPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fi, _ := f.Stat()
	filename := "snapshot-" + id + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(filename)+"\"")
	if fi != nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	}
	http.ServeContent(w, r, filename, fi.ModTime(), f)
}

func (a *API) SnapshotDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	id = strings.TrimSuffix(id, "/delete")

	if !snapshotIDRx.MatchString(id) {
		a.errorResponse(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	if err := a.snapshotSvc.Delete(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	JSONSuccess(w, nil)
}
