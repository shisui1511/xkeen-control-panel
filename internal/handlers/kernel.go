package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/user/xkeen-control-panel/internal/services"
)

func (a *API) KernelList(w http.ResponseWriter, r *http.Request) {
	if a.kernelSvc == nil {
		a.kernelSvc = services.NewKernelService()
	}
	a.jsonResponse(w, a.kernelSvc.List())
}

func (a *API) KernelCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.kernelSvc == nil {
		a.kernelSvc = services.NewKernelService()
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/check")

	k := a.kernelSvc.Get(name)
	if k == nil {
		a.errorResponse(w, "Kernel not found", http.StatusNotFound)
		return
	}

	// Run check in background so response is immediate
	go a.kernelSvc.CheckLatest(name)

	a.jsonResponse(w, map[string]string{"status": "checking"})
}

func (a *API) KernelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.kernelSvc == nil {
		a.kernelSvc = services.NewKernelService()
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/install")

	k := a.kernelSvc.Get(name)
	if k == nil {
		a.errorResponse(w, "Kernel not found", http.StatusNotFound)
		return
	}

	// Run install in background
	go func() {
		_ = a.kernelSvc.Install(name)
	}()

	a.jsonResponse(w, map[string]string{"status": "downloading"})
}

func (a *API) KernelStatus(w http.ResponseWriter, r *http.Request) {
	if a.kernelSvc == nil {
		a.kernelSvc = services.NewKernelService()
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/status")

	k := a.kernelSvc.Get(name)
	if k == nil {
		a.errorResponse(w, "Kernel not found", http.StatusNotFound)
		return
	}

	a.jsonResponse(w, k)
}

func (a *API) KernelChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.kernelSvc == nil {
		a.kernelSvc = services.NewKernelService()
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/channel")

	var req struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !a.kernelSvc.SetChannel(name, req.Channel) {
		a.errorResponse(w, "Kernel not found", http.StatusNotFound)
		return
	}

	a.jsonResponse(w, map[string]string{"channel": req.Channel})
}
