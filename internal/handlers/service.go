package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"
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
	default:
		a.errorResponse(w, a.t(r, "service.invalid_action"), http.StatusBadRequest)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	a.ClearCapabilitiesCache()

	// If a kernel was targeted, launch async monitor
	if targetKernel != "" && (action == "restart" || action == "switch_kernel") {
		go a.monitorAndRollbackKernel(targetKernel)
	}

	w.Write([]byte(out))
}

func (a *API) monitorAndRollbackKernel(name string) {
	// Wait 10 seconds for the kernel to bootstrap and status to settle
	time.Sleep(10 * time.Second)

	k := a.kernelSvc.Get(name)
	if k == nil {
		return
	}

	if k.ProcessStatus != "running" {
		log.Printf("Service: kernel %s failed to reach running state, triggering auto-rollback...", name)

		if err := a.kernelSvc.Rollback(name); err != nil {
			log.Printf("Service: kernel auto-rollback failed: %v", err)
			a.xkeenSvc.RecordAction("auto_rollback:"+name, "Откат завершился ошибкой: "+err.Error(), err)
			return
		}

		// Restart service after rollback
		out, err := a.xkeenSvc.Restart()
		if err != nil {
			a.xkeenSvc.RecordAction("auto_rollback:"+name, "Откат выполнен. Перезапуск XKeen завершился ошибкой: "+err.Error()+"\nВывод:\n"+out, err)
		} else {
			a.xkeenSvc.RecordAction("auto_rollback:"+name, "Откат ядра и перезапуск XKeen выполнены успешно.\nВывод:\n"+out, nil)
		}
	}
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
