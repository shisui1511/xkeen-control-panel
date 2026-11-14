package handlers

import (
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services/setupwizard"
)

// Маршруты мастера первого запуска (фаза 149.1). Авторизацию и X-CSRF-Token
// изменяющих запросов проверяет обёртка HandleProtected до обработчика; при
// выключенном флаге слоя маршруты отвечают 404 config_layer_disabled.

// SetSetupWizardService подключает службу мастера первого запуска; создаётся,
// запускается и останавливается в main.go.
func (a *API) SetSetupWizardService(s *setupwizard.Service) {
	a.wizard = s
}

// requireWizard — requireLayer плюс подключённая служба мастера: без службы
// маршруты отвечают 404 not_found.
func (a *API) requireWizard(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.wizard == nil {
		JSONErrorCode(w, http.StatusNotFound, "not_found", a.t(r, "error.not_found"))
		return false
	}
	return true
}

// ConfigLayerWizard — GET /api/configlayer/wizard: статус мастера и шагов 1–6.
// Только чтение: признак мастера здесь не пишется.
func (a *API) ConfigLayerWizard(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireWizard(w, r) {
		return
	}
	JSONSuccess(w, a.wizard.View())
}
