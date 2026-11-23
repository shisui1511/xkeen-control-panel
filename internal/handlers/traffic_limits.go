package handlers

import (
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// maxLimitBodyBytes — предел тела запросов лимитов трафика.
const maxLimitBodyBytes = 64 << 10

// limitIDBody — тело запросов delete, enabled и reset.
type limitIDBody struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// limitsReady пишет 503 limits_unavailable, если служба лимитов не подключена.
func (a *API) limitsReady(w http.ResponseWriter, r *http.Request) bool {
	if a.limitsSvc == nil {
		JSONErrorCode(w, http.StatusServiceUnavailable, "limits_unavailable", a.t(r, "traffic.limits.limits_unavailable"))
		return false
	}
	return true
}

// limitsError переводит ошибку службы лимитов в ответ API.
func (a *API) limitsError(w http.ResponseWriter, r *http.Request, err error) {
	var inErr *services.LimitInputError
	switch {
	case errors.Is(err, services.ErrLimitNotFound):
		JSONErrorCode(w, http.StatusNotFound, "limit_not_found", a.t(r, "traffic.limits.limit_not_found"))
	case errors.As(err, &inErr) && inErr.Reason == "action_invalid":
		JSONErrorCodeParams(w, http.StatusBadRequest, "action_invalid", a.t(r, "traffic.limits.action_invalid"),
			map[string]string{"field": inErr.Field, "reason": inErr.Reason})
	case errors.As(err, &inErr):
		JSONErrorCodeParams(w, http.StatusBadRequest, "invalid_input", a.t(r, "traffic.limits.invalid_input"),
			map[string]string{"field": inErr.Field, "reason": inErr.Reason})
	default:
		JSONErrorCode(w, http.StatusInternalServerError, "internal", a.t(r, "error.internal"))
	}
}

// TrafficLimitsList — GET /api/traffic/limits: лимиты с состояниями, состояние учёта и ядро.
func (a *API) TrafficLimitsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.limitsReady(w, r) {
		return
	}
	JSONSuccess(w, a.limitsSvc.List())
}

// TrafficLimitsSave — POST /api/traffic/limits/save: создание (пустой id) или правка лимита.
func (a *API) TrafficLimitsSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.limitsReady(w, r) {
		return
	}
	var l services.TrafficLimit
	if !a.decodeLayerBodyLimit(w, r, &l, maxLimitBodyBytes) {
		return
	}
	saved, err := a.limitsSvc.Save(l)
	if err != nil {
		a.limitsError(w, r, err)
		return
	}
	JSONSuccess(w, saved)
}

// decodeLimitID читает тело {id} или {id, enabled}.
func (a *API) decodeLimitID(w http.ResponseWriter, r *http.Request) (limitIDBody, bool) {
	var body limitIDBody
	if !a.decodeLayerBodyLimit(w, r, &body, maxLimitBodyBytes) {
		return body, false
	}
	return body, true
}

// TrafficLimitsDelete — POST /api/traffic/limits/delete {id}.
func (a *API) TrafficLimitsDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.limitsReady(w, r) {
		return
	}
	body, ok := a.decodeLimitID(w, r)
	if !ok {
		return
	}
	if err := a.limitsSvc.Delete(body.ID); err != nil {
		a.limitsError(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

// TrafficLimitsEnabled — POST /api/traffic/limits/enabled {id, enabled}.
func (a *API) TrafficLimitsEnabled(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.limitsReady(w, r) {
		return
	}
	body, ok := a.decodeLimitID(w, r)
	if !ok {
		return
	}
	if err := a.limitsSvc.SetEnabled(body.ID, body.Enabled); err != nil {
		a.limitsError(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

// TrafficLimitsReset — POST /api/traffic/limits/reset {id}: «Сбросить счётчик».
func (a *API) TrafficLimitsReset(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.limitsReady(w, r) {
		return
	}
	body, ok := a.decodeLimitID(w, r)
	if !ok {
		return
	}
	if err := a.limitsSvc.Reset(body.ID); err != nil {
		a.limitsError(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

// TrafficLimitsAlerts — GET /api/traffic/limits/alerts.
func (a *API) TrafficLimitsAlerts(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.limitsReady(w, r) {
		return
	}
	JSONSuccess(w, a.limitsSvc.Alerts())
}

// TrafficLimitsAlertsClear — POST /api/traffic/limits/alerts/clear.
func (a *API) TrafficLimitsAlertsClear(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.limitsReady(w, r) {
		return
	}
	a.limitsSvc.ClearAlerts()
	JSONSuccess(w, nil)
}
