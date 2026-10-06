package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// configLayerDisableTimeout — предел выключения слоя: перенос файлов и рестарт ядра.
const configLayerDisableTimeout = 3 * time.Minute

type SettingsResponse struct {
	Port    int                `json:"port"`
	HTTPS   config.HTTPSConfig `json:"https"`
	DevMode bool               `json:"dev_mode"`
	// ConfigLayer — флаг слоя «Конфигурация» (D-01).
	ConfigLayer bool `json:"config_layer"`
}

func (a *API) SettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	a.cfg.RLock()
	resp := SettingsResponse{
		Port:    a.cfg.Port,
		HTTPS:   a.cfg.HTTPS,
		DevMode: a.cfg.DevMode,

		ConfigLayer: a.cfg.ConfigLayer,
	}
	a.cfg.RUnlock()
	JSONSuccess(w, resp)
}

func (a *API) SettingsDevMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Lock/Unlock bracket only the field write (134-REVIEW CR-01); config.Save
	// below takes its own RLock for the marshal, so it must run unlocked here.
	a.cfg.Lock()
	a.cfg.DevMode = req.Enabled
	a.cfg.Unlock()

	if a.cfg.ConfigPath != "" {
		if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}

	a.cfg.RLock()
	devMode := a.cfg.DevMode
	a.cfg.RUnlock()
	JSONSuccess(w, map[string]bool{"dev_mode": devMode})
}

// setConfigLayerFlag меняет флаг в памяти под замком конфигурации.
func (a *API) setConfigLayerFlag(enabled bool) (previous bool) {
	a.cfg.Lock()
	previous = a.cfg.ConfigLayer
	a.cfg.ConfigLayer = enabled
	a.cfg.Unlock()
	return previous
}

// saveConfigLayerFlag пишет config.json (без замка: Save берёт RLock сам).
func (a *API) saveConfigLayerFlag() error {
	if a.cfg.ConfigPath == "" {
		return nil
	}
	return config.Save(a.cfg.ConfigPath, a.cfg)
}

// SettingsConfigLayer — POST /api/settings/config-layer {enabled}: переключатель
// слоя «Конфигурация» (D-01, D-04).
//
// Включение: сначала ключ в config.json, затем фоновая сборка файлов из
// применённого состояния. Выключение: сначала перенос файлов в набор копий и
// рестарт ядра, и только потом снятие ключа; сбой оставляет ключ включённым.
func (a *API) SettingsConfigLayer(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}

	// Переключения не пересекаются: два встречных запроса иначе оставили бы флаг
	// и диск в разных состояниях.
	a.configLayerMu.Lock()
	defer a.configLayerMu.Unlock()

	a.cfg.RLock()
	current := a.cfg.ConfigLayer
	a.cfg.RUnlock()

	if req.Enabled {
		a.setConfigLayerFlag(true)
		if err := a.saveConfigLayerFlag(); err != nil {
			a.setConfigLayerFlag(current)
			JSONErrorCodeDetail(w, http.StatusInternalServerError, "internal", a.t(r, "error.internal"), utils.StripANSI(err.Error()))
			return
		}
		// Уже включённый слой повторно не пересобирается.
		if !current && a.configLayer != nil {
			a.configLayer.Enable()
		}
		JSONSuccess(w, map[string]bool{"config_layer": true})
		return
	}

	// Снятие ключа: с слоем — внутри Disable под замками применения (между
	// переносом файлов и снятием флага не успевает записаться ни один файл, D-04),
	// без слоя — сразу.
	var saveErr error
	commitFlag := func() error {
		a.setConfigLayerFlag(false)
		if saveErr = a.saveConfigLayerFlag(); saveErr != nil {
			// Ключ не сохранился: флаг остаётся включённым.
			a.setConfigLayerFlag(current)
		}
		return saveErr
	}
	if current && a.configLayer != nil {
		// Перенос не обрывается закрытием вкладки: контекст отвязан от запроса.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), configLayerDisableTimeout)
		defer cancel()
		if err := a.configLayer.Disable(ctx, commitFlag); err != nil {
			switch {
			case saveErr != nil:
				// Файлы уже перенесены, а ключ остался: возвращаем файлы слоя из applied.
				a.configLayer.Enable()
				JSONErrorCodeDetail(w, http.StatusInternalServerError, "internal", a.t(r, "error.internal"), utils.StripANSI(err.Error()))
			case errors.Is(err, configlayer.ErrApplyBusy):
				JSONErrorCode(w, http.StatusConflict, "apply_busy", a.t(r, "configlayer.apply_busy"))
			case errors.Is(err, configlayer.ErrKernelBusy):
				JSONErrorCode(w, http.StatusConflict, "kernel_op_in_progress", a.t(r, "kernel.op_in_progress"))
			default:
				JSONErrorCodeDetail(w, http.StatusInternalServerError, "config_layer_disable_failed",
					a.t(r, "configlayer.disable_failed"), utils.StripANSI(err.Error()))
			}
			return
		}
	} else if err := commitFlag(); err != nil {
		JSONErrorCodeDetail(w, http.StatusInternalServerError, "internal", a.t(r, "error.internal"), utils.StripANSI(err.Error()))
		return
	}
	JSONSuccess(w, map[string]bool{"config_layer": false})
}
