package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// SetMihomoProfileService wires the profile service to the running panel:
// validation with "mihomo -t", restart through XKeen and a health check that
// waits for the Mihomo process to come back.
func (a *API) SetMihomoProfileService(svc *services.MihomoProfileService) {
	svc.Validate = a.validateMihomoProfile
	svc.ValidateContent = a.validateMihomoContent
	// Перезапуск после активации профиля решает KernelApplier: Mihomo должен
	// быть активным ядром, а его процесс — мочь работать (unknown считается
	// запущенным). Логика отката в Activate остаётся прежней.
	svc.CoreActive = func() bool {
		return a.kernelApplier != nil && a.kernelApplier.WillRestart("mihomo")
	}
	// Сам рестарт идёт через KernelApplier.ApplyLocked с решением по свежему
	// статусу. Activate — единственный вызывающий svc.Restart, и он идёт только из
	// ветки activate в MihomoProfileAction под lifecycleMu (повторный захват
	// недопустим: sync.Mutex не реентерабелен). CoreActive выше — лишь
	// предварительная проверка.
	svc.Restart = func() error {
		if a.kernelApplier == nil {
			return errors.New("kernel applier is not configured")
		}
		res := a.kernelApplier.ApplyLocked("mihomo")
		switch res.Outcome {
		case services.ApplyRestarted:
			return nil
		case services.ApplyRestartFailed:
			return errors.New(res.Error)
		default:
			return services.ErrRestartSkipped
		}
	}
	// Успех запуска — только новый процесс: PID берётся до перезапуска, процесс со
	// старым PID не засчитывается, один PID должен продержаться RestartStableWindow
	// (то же подтверждение, что у конвейера «Применить»).
	svc.RunningPID = a.mihomoRunningPID
	svc.ReturnTarget = a.mihomoReturnTarget
	svc.Healthy = func(oldPID int) bool {
		if a.kernelSvc == nil {
			return true
		}
		return configlayer.ConfirmRestarted(context.Background(), a.kernelSvc.ProcessStates, "mihomo", oldPID) == nil
	}
	a.mihomoProfileSvc = svc
}

// validateMihomoProfile проверяет файл профиля: читает его и отдаёт содержимое
// validateMihomoContent (то же зеркало корня Mihomo, что у сохранения).
func (a *API) validateMihomoProfile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return a.validateMihomoContent(data)
}

// validateMihomoContent проверяет содержимое профиля `mihomo -t` в зеркале корня
// Mihomo: геобазы, rules/, providers/ видны ядру, config.yaml зеркала — проверяемое
// содержимое. Вывод ядра показывает рабочий корень, а не временный каталог.
func (a *API) validateMihomoContent(data []byte) error {
	bin := a.cfg.MihomoBinary
	if a.mihomoSvc != nil && a.mihomoSvc.BinaryPath != "" {
		bin = a.mihomoSvc.BinaryPath
	}
	if bin == "" {
		return nil
	}
	if _, err := exec.LookPath(bin); err != nil {
		// No core installed (e.g. development machine): nothing to test with.
		return nil
	}
	tmp, err := os.MkdirTemp("", "xcp-prof-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := configlayer.MirrorMihomo(a.cfg.MihomoConfigDir, data, tmp); err != nil {
		return err
	}
	// Проверка не идёт во время операции XKeen над ядром (services.AcquireKernelHelper);
	// ожидание не съедает её таймаут.
	defer services.AcquireKernelHelper(context.Background())()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-t", "-d", tmp, "-f", filepath.Join(tmp, "config.yaml")).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(strings.ReplaceAll(string(out), tmp, a.cfg.MihomoConfigDir))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return fmt.Errorf("mihomo -t: %s", msg)
	}
	return nil
}

// mihomoReturnTarget — профиль, на который вернётся Mihomo при отпускании; пусто,
// когда слой выключен или панель Mihomo не владеет. Читается при каждом обращении к
// сервису профилей: владение меняется применением, а не перезапуском панели.
func (a *API) mihomoReturnTarget() string {
	if a.configLayer == nil {
		return ""
	}
	return a.configLayer.MihomoReturnTarget()
}

// mihomoRunningPID — PID работающего процесса Mihomo, 0 — не запущен. По
// процессам, без определения версии: опрос не ждёт конца операции XKeen.
func (a *API) mihomoRunningPID() int {
	if a.kernelSvc == nil {
		return 0
	}
	for _, st := range a.kernelSvc.ProcessStates() {
		if st.Name == "mihomo" && st.Status == "running" && st.PID > 0 {
			return st.PID
		}
	}
	return 0
}

// profileActivationResponse — ответ активации профиля: результат сервиса и
// исход применения. Outcome заполняется, только когда профиль записан, но ядро
// не перезапускалось (остановлено, неактивно или запущены оба ядра): по нему карточка профилей
// показывает, когда профиль вступит в силу.
type profileActivationResponse struct {
	*services.ActivationResult
	Outcome string `json:"outcome,omitempty"`
}

// profileActivationResult дополняет результат активации исходом применения.
func (a *API) profileActivationResult(res *services.ActivationResult) profileActivationResponse {
	out := profileActivationResponse{ActivationResult: res}
	if res == nil || res.Restarted || res.Error != "" || a.kernelApplier == nil {
		return out
	}
	// Исход restarted в предпросмотре значит «Activate не перезапускал»: профиль
	// уже был активным, исход в ответ не попадает.
	switch preview := a.kernelApplier.Preview("mihomo").Outcome; preview {
	case services.ApplySavedKernelStopped, services.ApplySavedKernelInactive, services.ApplySavedKernelConflict:
		out.Outcome = string(preview)
	}
	return out
}

// activateByOwner ведёт «Сделать активным» по владельцу Mihomo, когда слой
// «Конфигурация» включён. Панель владеет Mihomo — переход идёт через конвейер
// (ActivateProfile под замком применения): черновик не затрагивается, при открытом
// ответ 409 draft_open. Панель не владеет, но черновик хочет взять Mihomo (желание
// есть, файл ещё не переключён) — тоже 409 draft_open: иначе «Применить» взял бы
// профиль, который пользователь только что выбрал. Смена одного своего профиля на
// другой черновика не касается и идёт обычной активацией. Возвращает true, когда
// ответ уже записан.
func (a *API) activateByOwner(w http.ResponseWriter, r *http.Request, name string) bool {
	if a.configLayer == nil || !a.configLayer.Enabled() || a.routing == nil {
		return false
	}
	if a.configLayer.MihomoTaken() {
		if services.IsReservedProfileName(name) {
			JSONErrorCode(w, http.StatusBadRequest, "profile_reserved", a.t(r, "profiles.err_reserved"))
			return true
		}
		view, err := a.routing.ActivateProfile(context.WithoutCancel(r.Context()), name)
		switch {
		case errors.Is(err, configlayer.ErrDraftOpen):
			JSONErrorCode(w, http.StatusConflict, "draft_open", a.t(r, "profiles.err_draft_open"))
		case err != nil:
			a.writeRoutingError(w, r, err)
		default:
			a.ClearCapabilitiesCache()
			JSONSuccess(w, mihomoSwitchResult(view, name, panelMihomoProfileName))
		}
		return true
	}
	if st, err := a.configLayer.ReadState(); err == nil && configlayer.DesiredRouting(st.Draft).ManagedFor(configlayer.KernelMihomo) {
		JSONErrorCode(w, http.StatusConflict, "draft_open", a.t(r, "profiles.err_draft_open"))
		return true
	}
	return false
}

type mihomoProfileRequest struct {
	Name    string `json:"name"`
	NewName string `json:"new_name"`
	From    string `json:"from"`
	Empty   bool   `json:"empty"`
}

// MihomoProfiles handles GET /api/mihomo/profiles.
func (a *API) MihomoProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.mihomoProfileSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "profile service unavailable")
		return
	}
	state, err := a.mihomoProfileSvc.List()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, state)
}

// MihomoProfileAction handles POST /api/mihomo/profiles/{create|rename|delete|activate|adopt}.
func (a *API) MihomoProfileAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.mihomoProfileSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "profile service unavailable")
		return
	}
	var req mihomoProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	svc := a.mihomoProfileSvc
	action := strings.TrimPrefix(r.URL.Path, "/api/mihomo/profiles/")

	var err error
	var result interface{}
	switch action {
	case "create":
		// Создание читает активный конфиг или другой профиль: под замком записи
		// файлов Mihomo, чтобы не прочесть его на полпути записи (сам сервис берёт
		// свой замок в Create).
		unlock := a.lockMihomoFiles()
		err = svc.Create(req.Name, req.From, req.Empty)
		unlock()
	case "rename":
		err = svc.Rename(req.Name, req.NewName)
	case "delete":
		err = svc.Delete(req.Name)
	case "adopt":
		err = svc.Adopt(req.Name)
	case "activate":
		// Слой включён: путь выбирает владелец Mihomo (D-06, D-08). Ответ записан —
		// дальше нечего делать.
		if a.activateByOwner(w, r, req.Name) {
			return
		}
		// Активация перезапускает ядро: под общим замком жизненного цикла, до
		// переключения ссылки config.yaml. create/rename/delete/adopt ядро не трогают.
		if !a.tryLifecycleLock(w, r) {
			return
		}
		defer a.lifecycleMu.Unlock()
		var res *services.ActivationResult
		res, err = svc.Activate(req.Name)
		if err == nil {
			a.ClearCapabilitiesCache()
			result = a.profileActivationResult(res)
		}
	default:
		JSONError(w, http.StatusNotFound, "unknown action")
		return
	}

	switch {
	case errors.Is(err, services.ErrProfileInvalidName):
		JSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrProfileReserved):
		JSONErrorCode(w, http.StatusBadRequest, "profile_reserved", a.t(r, "profiles.err_reserved"))
	case errors.Is(err, services.ErrProfileNotFound):
		JSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrProfileExists):
		JSONErrorCode(w, http.StatusConflict, "profile_exists", err.Error())
	case errors.Is(err, services.ErrProfileReturnTarget):
		JSONErrorCode(w, http.StatusConflict, "profile_return_target", a.t(r, "profiles.err_return_target"))
	case errors.Is(err, services.ErrProfileActive), errors.Is(err, services.ErrProfilesUnmanaged):
		JSONError(w, http.StatusConflict, err.Error())
	case err != nil:
		JSONError(w, http.StatusInternalServerError, err.Error())
	default:
		if result == nil {
			result, _ = svc.List()
		}
		JSONSuccess(w, result)
	}
}
