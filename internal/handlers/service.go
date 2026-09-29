package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type ServiceStatusResponse struct {
	IsRunning    bool   `json:"is_running"`
	ActiveKernel string `json:"active_kernel"`
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`
	BinaryPath   string `json:"binary_path"`
	Raw          string `json:"raw"`
	// Stale — Raw нельзя считать свежим (опрос xkeen не удался или устарел);
	// AgeSeconds — возраст последнего успешного опроса, нет на холодном старте
	Stale      bool                    `json:"stale"`
	AgeSeconds *int                    `json:"age_seconds,omitempty"`
	Watchdog   *WatchdogStatusResponse `json:"watchdog,omitempty"`
	// XKeenInstalled — бинарник XKeen найден; XKeenInstallerAvailable —
	// панель может установить XKeen (есть Entware)
	XKeenInstalled          bool `json:"xkeen_installed"`
	XKeenInstallerAvailable bool `json:"xkeen_installer_available"`
	// XKeenSetupIncomplete — бинарник есть, но `xkeen -i` не дошёл до конца
	// (нет init-скрипта): установку нужно запустить снова
	XKeenSetupIncomplete bool `json:"xkeen_setup_incomplete"`
}

// xkeenSetupIncomplete — XKeen распакован, но настройка прервана.
func (a *API) xkeenSetupIncomplete() bool {
	return a.xkeenInstaller != nil && a.xkeenInstaller.Available() &&
		a.xkeenSvc.Installed() && !a.xkeenInstaller.SetupComplete()
}

func (a *API) ServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	// Без XKeen `xkeen -status` не запустить: это не ошибка сервера, а
	// состояние, по которому UI предлагает установку
	if !a.xkeenSvc.Installed() {
		JSONSuccess(w, ServiceStatusResponse{
			BinaryPath:              a.cfg.XKeenBinary,
			XKeenInstallerAvailable: a.xkeenInstaller != nil && a.xkeenInstaller.Available(),
		})
		return
	}
	// Статус берётся из кэша: медленный или упавший `xkeen -status` под
	// нагрузкой больше не превращается в 500, ответ помечается stale
	snap := a.xkeenStatusSnapshot()

	resp := ServiceStatusResponse{
		BinaryPath:              a.cfg.XKeenBinary,
		Raw:                     snap.Raw,
		Stale:                   snap.Stale,
		XKeenInstalled:          a.xkeenSvc.Installed(),
		XKeenInstallerAvailable: a.xkeenInstaller != nil && a.xkeenInstaller.Available(),
		XKeenSetupIncomplete:    a.xkeenSetupIncomplete(),
	}
	if age, ok := a.xkeenStatusAge(snap); ok {
		resp.AgeSeconds = &age
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
		if !snap.Stale && services.IsKernelStatusHealthy(snap.Raw) {
			resp.IsRunning = true
		}
	}
	if resp.ActiveKernel == "" {
		// Ядро остановлено: показываем то, которое запустит XKeen
		resp.ActiveKernel = a.xkeenSvc.ConfiguredKernel()
	}

	if a.watchdogSvc != nil {
		wd := newWatchdogStatusResponse(a.watchdogSvc.Snapshot())
		resp.Watchdog = &wd
	}

	JSONSuccess(w, resp)
}

func (a *API) ServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	action := r.URL.Query().Get("action")

	if action == "apply" {
		a.serviceApply(w, r)
		return
	}

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

// serviceApply — action=apply: применить записанную конфигурацию к ядру.
// Перезапускается только запущенное целевое ядро; остановленное не
// запускается, чужое не трогается. HTTP 200 для всех исходов: файлы к этому
// моменту уже записаны, по outcome UI выбирает тост.
func (a *API) serviceApply(w http.ResponseWriter, r *http.Request) {
	var target string
	if path := r.URL.Query().Get("path"); path != "" {
		cleanPath, err := a.pathVal.Validate(path)
		if err != nil {
			a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
			return
		}
		target = a.kernelForConfigPath(cleanPath)
	} else {
		target = r.URL.Query().Get("kernel")
		if target != "xray" && target != "mihomo" && target != services.ApplyTargetActive {
			a.errorResponse(w, a.t(r, "service.invalid_kernel"), http.StatusBadRequest)
			return
		}
	}

	result := a.applyKernel(target)
	log.Printf("apply: target=%s active=%s outcome=%s",
		utils.SanitizeLogInput(target), utils.SanitizeLogInput(result.ActiveKernel), utils.SanitizeLogInput(string(result.Outcome)))
	a.ClearCapabilitiesCache()
	JSONSuccess(w, result)
}

// applyKernel применяет конфигурацию к целевым ядрам через KernelApplier. Без
// applier ничего не перезапускается.
func (a *API) applyKernel(targets ...string) services.ApplyResult {
	if a.kernelApplier == nil {
		log.Printf("apply: kernel applier is not configured, restart skipped")
		res := services.ApplyResult{Outcome: services.ApplySavedKernelStopped}
		if len(targets) > 0 {
			res.Kernel = targets[0]
		}
		return res
	}
	return a.kernelApplier.Apply(targets...)
}

// kernelForConfigPath — ядро, которому принадлежит файл конфигурации: каталог
// Xray → xray, каталог Mihomo → mihomo, остальное → active.
func (a *API) kernelForConfigPath(cleanPath string) string {
	if pathInDir(cleanPath, a.cfg.XRayConfigDir) {
		return "xray"
	}
	if pathInDir(cleanPath, a.cfg.MihomoConfigDir) {
		return "mihomo"
	}
	return services.ApplyTargetActive
}

// pathInDir — path равен dir или лежит внутри него (граница по разделителю).
// Оба пути нормализуются как в PathValidator: Clean и разрешение симлинков (при
// ошибке остаётся Clean), иначе каталог-симлинк не узнал бы файл по реальному
// пути.
func pathInDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	dir = normalizeApplyPath(dir)
	path = normalizeApplyPath(path)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func normalizeApplyPath(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
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
	if errors.Is(err, services.ErrDNSRolledBack) {
		// Redirection was undone because the router stopped resolving names.
		a.ClearCapabilitiesCache()
		a.errorResponse(w, a.t(r, "dns.redirect_rolled_back"), http.StatusConflict)
		return
	}
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	a.ClearCapabilitiesCache()

	w.Write([]byte(out))
}
