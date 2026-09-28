package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// SetMihomoProfileService wires the profile service to the running panel:
// validation with "mihomo -t", restart through XKeen and a health check that
// waits for the Mihomo process to come back.
func (a *API) SetMihomoProfileService(svc *services.MihomoProfileService) {
	svc.Validate = a.validateMihomoProfile
	// Перезапуск после активации профиля решает KernelApplier: Mihomo должен
	// быть активным ядром, а его процесс — мочь работать (unknown считается
	// запущенным). Логика отката в Activate остаётся прежней.
	svc.CoreActive = func() bool {
		return a.kernelApplier != nil && a.kernelApplier.WillRestart("mihomo")
	}
	svc.Restart = func() error {
		if a.xkeenSvc == nil {
			return errors.New("xkeen service unavailable")
		}
		_, err := a.xkeenSvc.Restart()
		return err
	}
	svc.Healthy = a.waitMihomoRunning
	a.mihomoProfileSvc = svc
}

func (a *API) validateMihomoProfile(path string) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-t", "-d", a.cfg.MihomoConfigDir, "-f", path).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return fmt.Errorf("mihomo -t: %s", msg)
	}
	return nil
}

// waitMihomoRunning polls the Mihomo process for up to 20 seconds.
func (a *API) waitMihomoRunning() bool {
	if a.kernelSvc == nil {
		return true
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		for _, info := range a.kernelSvc.List() {
			if info.Name == "mihomo" && info.ProcessStatus == "running" {
				return true
			}
		}
	}
	return false
}

// profileActivationResponse — ответ активации профиля: результат сервиса и
// исход применения. Outcome заполняется, только когда профиль записан, но ядро
// не перезапускалось (остановлено или неактивно): по нему карточка профилей
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
	case services.ApplySavedKernelStopped, services.ApplySavedKernelInactive:
		out.Outcome = string(preview)
	}
	return out
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
		err = svc.Create(req.Name, req.From, req.Empty)
	case "rename":
		err = svc.Rename(req.Name, req.NewName)
	case "delete":
		err = svc.Delete(req.Name)
	case "adopt":
		err = svc.Adopt(req.Name)
	case "activate":
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
	case errors.Is(err, services.ErrProfileNotFound):
		JSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrProfileExists), errors.Is(err, services.ErrProfileActive), errors.Is(err, services.ErrProfilesUnmanaged):
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
