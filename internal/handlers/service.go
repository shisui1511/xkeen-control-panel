package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

type ServiceStatusResponse struct {
	IsRunning    bool   `json:"is_running"`
	ActiveKernel string `json:"active_kernel"`
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`
	BinaryPath   string `json:"binary_path"`
	Raw          string `json:"raw"`
}

func (a *API) ServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	out, err := a.xkeenSvc.Status()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, out)
		return
	}

	resp := ServiceStatusResponse{
		BinaryPath: a.cfg.XKeenBinary,
		Raw:        out,
	}

	// Detect which kernel is running and get its PID/Uptime
	if a.kernelSvc != nil {
		for _, info := range a.kernelSvc.List() {
			if info.ProcessStatus == "running" {
				resp.IsRunning = true
				resp.ActiveKernel = info.Name
				resp.PID = info.PID
				resp.Uptime = info.Uptime
				break
			}
		}
	}

	// Fallback to checking raw output if kernelSvc list is empty or doesn't find running
	if !resp.IsRunning {
		lower := strings.ToLower(out)
		if strings.Contains(lower, "running") || strings.Contains(lower, "запущен") {
			resp.IsRunning = true
		}
	}

	JSONSuccess(w, resp)
}

func (a *API) ServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	action := r.URL.Query().Get("action")

	var out string
	var err error

	// Determine kernel to monitor if restart or switch_kernel
	var targetKernel string
	if action == "restart" {
		// Detect which kernel was running before restart
		if k := a.kernelSvc.Get("xray"); k != nil && k.ProcessStatus == "running" {
			targetKernel = "xray"
		} else if k := a.kernelSvc.Get("mihomo"); k != nil && k.ProcessStatus == "running" {
			targetKernel = "mihomo"
		}
	} else if action == "switch_kernel" {
		targetKernel = r.URL.Query().Get("kernel")
		if targetKernel != "xray" && targetKernel != "mihomo" {
			a.errorResponse(w, a.t(r, "service.invalid_kernel"), http.StatusBadRequest)
			return
		}
	}

	switch action {
	case "start":
		out, err = a.xkeenSvc.Start()
	case "stop":
		out, err = a.xkeenSvc.Stop()
	case "restart":
		out, err = a.xkeenSvc.Restart()
	case "switch_kernel":
		out, err = a.xkeenSvc.SwitchKernel(targetKernel)
		if err == nil {
			// После успешной смены ядра сразу запускаем XKeen
			startOut, startErr := a.xkeenSvc.Start()
			if startErr != nil {
				out = out + "\n" + startOut
				err = startErr
			} else {
				out = out + "\n" + startOut
			}
		}
	default:
		a.errorResponse(w, a.t(r, "service.invalid_action"), http.StatusBadRequest)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	a.ClearCapabilitiesCache()

	w.Write([]byte(out))
}

func (a *API) ServiceRestartLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	entries := a.xkeenSvc.GetRestartLog()
	// Return newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	a.jsonResponse(w, entries)
}

func (a *API) ServiceDNSRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, a.t(r, "error.invalid_json"), http.StatusBadRequest)
		return
	}
	if req.Enabled == nil {
		a.errorResponse(w, a.t(r, "error.bad_request"), http.StatusBadRequest)
		return
	}

	out, err := a.xkeenSvc.SetDNSProxying(*req.Enabled)
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	a.ClearCapabilitiesCache()

	w.Write([]byte(out))
}
