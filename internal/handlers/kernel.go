package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (a *API) KernelList(w http.ResponseWriter, r *http.Request) {
	JSONSuccess(w, a.kernelSvc.List())
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

	// Run check in background so response is immediate
	go a.kernelSvc.CheckLatest(context.Background(), name)

	JSONSuccess(w, map[string]string{"status": "checking"})
}

func (a *API) KernelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/install")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	// Reject concurrent install requests: if this kernel is already downloading or installing,
	// return HTTP 409 Conflict immediately.
	if k.Status == "downloading" || k.Status == "installing" {
		JSONError(w, http.StatusConflict, "install already in progress")
		return
	}

	// Run install in background
	go func() {
		_ = a.kernelSvc.Install(name)
	}()

	JSONSuccess(w, map[string]string{"status": "downloading"})
}

func (a *API) KernelStatus(w http.ResponseWriter, r *http.Request) {

	name := strings.TrimPrefix(r.URL.Path, "/api/kernels/")
	name = strings.TrimSuffix(name, "/status")

	k := a.kernelSvc.Get(name)
	if k == nil {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	JSONSuccess(w, k)
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

	if !a.kernelSvc.SetChannel(name, req.Channel) {
		JSONError(w, http.StatusNotFound, "Kernel not found")
		return
	}

	JSONSuccess(w, map[string]string{"channel": req.Channel})
}

func (a *API) KernelDebug(w http.ResponseWriter, r *http.Request) {
	JSONSuccess(w, a.kernelSvc.GetDebugInfo())
}
