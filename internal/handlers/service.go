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
	IsRunning bool `json:"is_running"`
	// ActiveKernel — "xray" | "mihomo" | "none"; "both" при конфликте.
	ActiveKernel string `json:"active_kernel"`
	// KernelConflict — запущены оба ядра; PID и Uptime при этом пусты.
	KernelConflict bool `json:"kernel_conflict"`
	// RunningKernels — запущенные ядра в порядке [xray, mihomo].
	RunningKernels []string `json:"running_kernels,omitempty"`
	PID            int      `json:"pid"`
	Uptime         string   `json:"uptime"`
	BinaryPath     string   `json:"binary_path"`
	Raw            string   `json:"raw"`
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

	// Активное ядро — единый резолвер по процессам (запасные источники — свежий
	// снимок статуса и name_client — учтены внутри него).
	st := a.activeKernelState()
	resp.ActiveKernel = st.Label()
	resp.KernelConflict = st.Conflict
	resp.RunningKernels = st.Running
	resp.IsRunning = len(st.Running) > 0
	// PID и uptime — только когда активное ядро однозначно; при конфликте пусты.
	if !st.Conflict && a.kernelSvc != nil && len(st.Running) > 0 {
		for _, ps := range a.kernelSvc.ProcessStates() {
			if ps.Name == st.Kernel && ps.Status == "running" {
				resp.PID = ps.PID
				resp.Uptime = ps.Uptime
				break
			}
		}
	}

	// Запасной путь: ни одного процесса, но свежий вывод xkeen -status здоров
	if !resp.IsRunning {
		if !snap.Stale && services.IsKernelStatusHealthy(snap.Raw) {
			resp.IsRunning = true
		}
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

	// Валидация до замка: неверный ввод не должен упираться в чужую операцию.
	switch action {
	case "start", "stop", "restart", "switch_kernel", "apply":
	default:
		a.errorResponse(w, a.t(r, "service.invalid_action"), http.StatusBadRequest)
		return
	}
	var targetKernel string
	if action == "switch_kernel" {
		targetKernel = r.URL.Query().Get("kernel")
		if targetKernel != "xray" && targetKernel != "mihomo" {
			a.errorResponse(w, a.t(r, "service.invalid_kernel"), http.StatusBadRequest)
			return
		}
	}

	// stop с kernel= — остановка конкретного ядра; имя только из белого списка.
	stopKernel := ""
	if action == "stop" {
		stopKernel = r.URL.Query().Get("kernel")
		if stopKernel != "" && stopKernel != "xray" && stopKernel != "mihomo" {
			a.errorResponse(w, a.t(r, "service.invalid_kernel"), http.StatusBadRequest)
			return
		}
	}

	// Одна операция жизненного цикла за раз (двойной клик, две вкладки).
	if !a.tryLifecycleLock(w, r) {
		return
	}
	defer a.lifecycleMu.Unlock()

	// Запуск, рестарт, переключение и применение при двух запущенных ядрах только
	// усугубили бы конфликт (D-02); остановка разрешена всегда.
	var st services.ActiveKernelState
	if action != "stop" {
		a.kernelSvc.InvalidateActiveState()
		st = a.activeKernelState()
		if st.Conflict {
			JSONErrorCode(w, http.StatusConflict, "kernel_conflict", a.t(r, "kernel.conflict"))
			return
		}
	}

	var out string
	var err error
	switch action {
	case "apply":
		a.serviceApply(w, r)
		return
	case "start":
		out, err = a.xkeenSvc.Start()
	case "stop":
		if stopKernel != "" {
			a.serviceStopKernel(w, r, stopKernel)
			return
		}
		out, err = a.xkeenSvc.Stop()
	case "restart":
		out, err = a.xkeenSvc.Restart()
	case "switch_kernel":
		// Прежнее ядро — только реально запущенное. При нуле процессов резолвер
		// отдаёт ядро из name_client/снимка, но «остановленным» его считать нельзя:
		// оно не работало (WR-07). Конфликт к этому месту уже отсечён guard'ом.
		old := "none"
		if len(st.Running) > 0 {
			old = st.Kernel
		}
		a.serviceSwitchKernel(w, targetKernel, old)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	a.ClearCapabilitiesCache()

	w.Write([]byte(out))
}

// switchOutputLines — сколько последних строк вывода скрипта XKeen попадает в
// ответ switch_kernel.
const switchOutputLines = 20

// serviceSwitchKernel — action=switch_kernel: скрипт XKeen, запуск и
// подтверждение по процессам (D-03). Успех скрипта не означает успех
// переключения, поэтому HTTP 200 несёт типизированный исход; ошибка самого
// скрипта — по-прежнему 500. Принудительно процессы не завершаются (D-04).
func (a *API) serviceSwitchKernel(w http.ResponseWriter, target, old string) {
	if a.kernelSwitcher == nil {
		a.errorResponse(w, "kernel service is not configured", http.StatusInternalServerError)
		return
	}

	// old — ядро до переключения по свежему чтению процессов (guard в ServiceControl).
	if old == "" {
		old = "none"
	}

	out, err := a.xkeenSvc.SwitchKernel(target)
	if err == nil {
		// После успешной смены ядра сразу запускаем XKeen
		startOut, startErr := a.xkeenSvc.Start()
		out = out + "\n" + startOut
		err = startErr
	}
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}

	res := a.kernelSwitcher.Await(old, target)
	res.Output = lastNonEmptyLines(utils.StripANSI(out), switchOutputLines)
	log.Printf("switch_kernel: old=%s new=%s outcome=%s",
		utils.SanitizeLogInput(res.Old), utils.SanitizeLogInput(res.New), utils.SanitizeLogInput(string(res.Outcome)))

	a.ClearCapabilitiesCache()
	JSONSuccess(w, res)
}

// serviceStopKernel — action=stop&kernel=: остановить конкретное ядро. Если это
// единственное запущенное ядро — штатный `xkeen -stop`; при конфликте (или когда
// ядро не единственное) — SIGTERM по проверенному PID, без зависимости от того,
// что делает `xkeen -stop` при двух ядрах. Принудительного завершения нет (D-04).
func (a *API) serviceStopKernel(w http.ResponseWriter, r *http.Request, kernel string) {
	if a.kernelSvc == nil {
		a.errorResponse(w, "kernel service is not configured", http.StatusInternalServerError)
		return
	}

	a.kernelSvc.InvalidateActiveState()
	st := a.activeKernelState()

	if !st.Conflict && len(st.Running) == 1 && st.Running[0] == kernel {
		out, err := a.xkeenSvc.Stop()
		if err != nil {
			a.errorResponse(w, out, http.StatusInternalServerError)
			return
		}
		outcome := services.KernelStopStopped
		for _, ps := range a.kernelSvc.ProcessStates() {
			if ps.Name == kernel && ps.Status == "running" {
				outcome = services.KernelStopStillRunning
			}
		}
		a.ClearCapabilitiesCache()
		JSONSuccess(w, services.KernelStopResult{Kernel: kernel, Outcome: outcome, Method: services.KernelStopMethodXKeen})
		return
	}

	res, err := a.kernelSvc.StopKernelProcess(kernel)
	res.Method = services.KernelStopMethodSignal
	summary := "kernel=" + kernel + " outcome=" + string(res.Outcome)
	if err != nil {
		summary = "kernel=" + kernel + " error=" + err.Error()
	}
	a.xkeenSvc.RecordAction("stop_kernel:"+kernel, summary, err)
	if err != nil {
		log.Printf("stop_kernel: kernel=%s error=%s", utils.SanitizeLogInput(kernel), utils.SanitizeLogInput(err.Error()))
		JSONErrorCodeDetail(w, http.StatusInternalServerError, "kernel_stop_failed", a.t(r, "kernel.stop_failed"), err.Error())
		return
	}
	log.Printf("stop_kernel: kernel=%s outcome=%s", utils.SanitizeLogInput(kernel), utils.SanitizeLogInput(string(res.Outcome)))

	// Остановка настроенного ядра для сторожевого таймера — плановая, как после Stop().
	if kernel == a.configuredKernel() {
		a.xkeenSvc.MarkIntentionalStop()
	}
	a.xkeenSvc.NotifyLifecycle()
	a.ClearCapabilitiesCache()
	JSONSuccess(w, res)
}

// lastNonEmptyLines — последние n непустых строк текста.
func lastNonEmptyLines(s string, n int) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, "\r"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
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

	result := a.applyKernelLocked(target)
	log.Printf("apply: target=%s active=%s outcome=%s",
		utils.SanitizeLogInput(target), utils.SanitizeLogInput(result.ActiveKernel), utils.SanitizeLogInput(string(result.Outcome)))
	a.ClearCapabilitiesCache()
	JSONSuccess(w, result)
}

// applyKernelLocked применяет конфигурацию к целевым ядрам через KernelApplier
// без повторного захвата замка: только под lifecycleMu (sync.Mutex не
// реентерабелен). Без applier ничего не перезапускается.
func (a *API) applyKernelLocked(targets ...string) services.ApplyResult {
	if a.kernelApplier == nil {
		log.Printf("apply: kernel applier is not configured, restart skipped")
		res := services.ApplyResult{Outcome: services.ApplySavedKernelStopped}
		if len(targets) > 0 {
			res.Kernel = targets[0]
		}
		return res
	}
	return a.kernelApplier.ApplyLocked(targets...)
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

	// SetDNSProxying перезапускает XKeen (и откатывает при потере DNS): под
	// общим замком жизненного цикла.
	if !a.tryLifecycleLock(w, r) {
		return
	}
	defer a.lifecycleMu.Unlock()

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
