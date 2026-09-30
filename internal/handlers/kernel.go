package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// kernelCheckSemaphore limits the number of concurrent background release-check
// goroutines spawned by KernelCheck. Without this, rapid repeated POST requests
// could accumulate unbounded goroutines making outbound GitHub API calls (STAB-01).
var kernelCheckSemaphore = make(chan struct{}, 2)

// kernelLatestAutoCheckTTL — не чаще раза за этот срок KernelList сам запускает
// проверку релиза для ядра с пустым latest_version. Худший случай — 2 ядра × 6
// проверок в час, ниже лимита анонимного API GitHub (60/ч).
const kernelLatestAutoCheckTTL = 10 * time.Minute

// runKernelCheck запускает проверку релиза в фоне: не больше двух одновременно
// (kernelCheckSemaphore, без очереди), жёсткий таймаут 10 с (STAB-01).
func (a *API) runKernelCheck(name string) {
	go func() {
		select {
		case kernelCheckSemaphore <- struct{}{}:
			defer func() { <-kernelCheckSemaphore }()
		default:
			// Уже идут две проверки: эту отбрасываем, а не копим в очереди.
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = a.kernelSvc.CheckLatest(ctx, name)
	}()
}

func (a *API) KernelList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	list := a.kernelSvc.List()
	for i := range list {
		if list[i].Name == "mihomo" {
			addr := a.cfg.MihomoAPIURL
			if a.mihomoSvc != nil {
				if ctrl, err := a.mihomoSvc.ParseControllerConfig(); err == nil && ctrl.Target != "" {
					addr = ctrl.Target
				}
			}
			addr = strings.TrimPrefix(addr, "http://")
			addr = strings.TrimPrefix(addr, "https://")
			list[i].APIAddr = addr
		}
		// Сведения о релизе без ручной «Проверить»: после старта latest пуст, и
		// без него флаги обновления ничего не говорят (G2). Ответ не ждёт проверки.
		if a.kernelSvc.ClaimLatestCheck(list[i].Name, kernelLatestAutoCheckTTL) {
			a.runKernelCheck(list[i].Name)
		}
	}
	JSONSuccess(w, list)
}

func (a *API) KernelCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/check")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	// Проверка идёт в фоне, чтобы ответ был мгновенным.
	a.runKernelCheck(name)

	JSONSuccess(w, map[string]string{"status": "checking"})
}

func (a *API) KernelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/install")

	if a.kernelSvc.Get(name) == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	// Замок берёт и статус «Старт…» ставит сам сервис, до ответа: отдельной
	// проверки статуса здесь нет (проверка-затем-действие давала гонку). Занят — 409.
	err := a.kernelSvc.BeginInstall(name, func(error) {
		a.ClearCapabilitiesCache()
		a.invalidateXKeenStatus()
	})
	if errors.Is(err, services.ErrKernelBusy) {
		JSONError(w, http.StatusConflict, "install already in progress")
		return
	}
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.ClearCapabilitiesCache()

	JSONSuccess(w, map[string]string{"status": "downloading", "stage": services.KernelStageStarting})
}

func (a *API) KernelStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/status")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	if k.Name == "mihomo" {
		addr := a.cfg.MihomoAPIURL
		if a.mihomoSvc != nil {
			if ctrl, err := a.mihomoSvc.ParseControllerConfig(); err == nil && ctrl.Target != "" {
				addr = ctrl.Target
			}
		}
		addr = strings.TrimPrefix(addr, "http://")
		addr = strings.TrimPrefix(addr, "https://")
		k.APIAddr = addr
	}

	JSONSuccess(w, k)
}

// kernelChannelResponse — ответ смены канала: выбранный канал и пересчитанное ядро.
type kernelChannelResponse struct {
	Channel string               `json:"channel"`
	Kernel  *services.KernelInfo `json:"kernel"`
}

func (a *API) KernelChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/channel")

	var req struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Channel != "stable" && req.Channel != "preview" {
		JSONError(w, http.StatusBadRequest, "invalid channel: must be 'stable' or 'preview'")
		return
	}

	if err := a.kernelSvc.SetChannel(name, req.Channel); err != nil {
		switch {
		case errors.Is(err, services.ErrKernelNotFound):
			JSONError(w, http.StatusNotFound, "Kernel not found")
		case errors.Is(err, services.ErrKernelBusy):
			JSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, services.ErrInvalidChannel):
			JSONError(w, http.StatusBadRequest, err.Error())
		default:
			JSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Синхронно перепроверяем релиз нового канала: ответ сразу несёт пересчитанные
	// статусы. Ошибка проверки уже записана в статус ядра (status "failed"), поэтому
	// сам запрос остаётся успешным.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	_ = a.kernelSvc.CheckLatest(ctx, name)
	cancel()
	a.ClearCapabilitiesCache()

	JSONSuccess(w, kernelChannelResponse{Channel: req.Channel, Kernel: a.kernelSvc.Get(name)})
}

func (a *API) KernelRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/rollback")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	if err := a.kernelSvc.Rollback(name); err != nil {
		if errors.Is(err, services.ErrKernelBusy) {
			JSONError(w, http.StatusConflict, "install already in progress")
			return
		}
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.ClearCapabilitiesCache()
	a.invalidateXKeenStatus()

	JSONSuccess(w, map[string]string{"status": "rolled_back"})
}

func (a *API) KernelDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/download")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	data, filename, err := a.kernelSvc.FetchBinary(name)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(data)
}

func (a *API) KernelDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	JSONSuccess(w, a.kernelSvc.GetDebugInfo())
}

func (a *API) KernelUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/upload")

	if name != "xray" && name != "mihomo" {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	// 100 MB max in request
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		JSONError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		JSONError(w, http.StatusBadRequest, "missing file in form")
		return
	}
	defer file.Close()

	if err := a.kernelSvc.UploadBinary(name, file, header.Filename); err != nil {
		if errors.Is(err, services.ErrKernelBusy) {
			JSONError(w, http.StatusConflict, "install already in progress")
			return
		}
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.ClearCapabilitiesCache()
	a.invalidateXKeenStatus()

	kUpdated := a.kernelSvc.Get(name)
	JSONSuccess(w, kUpdated)
}
