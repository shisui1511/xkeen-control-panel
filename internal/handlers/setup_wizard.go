package handlers

import (
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services/setupwizard"
)

// Маршруты мастера первого запуска (фаза 149.1). Авторизацию и X-CSRF-Token
// изменяющих запросов проверяет обёртка HandleProtected до обработчика; маршруты
// работают сразу после старта, пока в main.go созданы слой и служба мастера.

// SetSetupWizardService подключает службу мастера первого запуска; создаётся,
// запускается и останавливается в main.go.
func (a *API) SetSetupWizardService(s *setupwizard.Service) {
	a.wizard = s
}

// requireWizard — requireConfigLayer плюс подключённая служба мастера: без службы
// маршруты отвечают 404 not_found.
func (a *API) requireWizard(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireConfigLayer(w, r) {
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

// ConfigLayerWizardClose — POST /api/configlayer/wizard/close: «Закрыть» мастер
// (признак closed, D-10). Идемпотентно, признак не понижается.
func (a *API) ConfigLayerWizardClose(w http.ResponseWriter, r *http.Request) {
	a.wizardAction(w, r, a.wizardClose)
}

// ConfigLayerWizardHide — POST /api/configlayer/wizard/hide: «Скрыть» карточку
// (признак hidden, D-11). Идемпотентно.
func (a *API) ConfigLayerWizardHide(w http.ResponseWriter, r *http.Request) {
	a.wizardAction(w, r, a.wizardHide)
}

// ConfigLayerWizardReset — POST /api/configlayer/wizard/reset: снять признак
// мастера. Только в режиме разработчика (403 dev_mode_required), для проверок.
func (a *API) ConfigLayerWizardReset(w http.ResponseWriter, r *http.Request) {
	a.wizardAction(w, r, a.wizardReset)
}

func (a *API) wizardClose() (setupwizard.View, error) { return a.wizard.Close() }
func (a *API) wizardHide() (setupwizard.View, error)  { return a.wizard.Hide() }
func (a *API) wizardReset() (setupwizard.View, error) { return a.wizard.Reset() }

// wizardAction — общий путь POST-действий мастера: действие задаёт путь, тело
// запроса не читается (свободных строк нет); успех — свежий вид, ошибка — по
// таблице слоя (ErrDevModeRequired → 403).
func (a *API) wizardAction(w http.ResponseWriter, r *http.Request, act func() (setupwizard.View, error)) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireWizard(w, r) {
		return
	}
	view, err := act()
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}
