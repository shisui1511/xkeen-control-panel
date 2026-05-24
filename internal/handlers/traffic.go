package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) TrafficQuotaList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.ListQuotas())
}

func (a *API) TrafficQuotaGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	q, ok := a.trafficQuotaSvc.GetQuota(id)
	if !ok {
		a.errorResponse(w, "Quota not found", http.StatusNotFound)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	var q services.TrafficQuota
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if q.Name == "" || q.LimitBytes <= 0 {
		a.errorResponse(w, "Name and positive limit_bytes are required", http.StatusBadRequest)
		return
	}
	if q.TargetType == "" {
		q.TargetType = "global"
	}
	if q.Period == "" {
		q.Period = "monthly"
	}

	if err := a.trafficQuotaSvc.AddQuota(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	var q services.TrafficQuota
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.UpdateQuota(id, &q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.DeleteQuota(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) TrafficQuotaSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	enabled := r.URL.Query().Get("enabled") == "true"
	if err := a.trafficQuotaSvc.SetQuotaEnabled(id, enabled); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) TrafficQuotaReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.ResetQuota(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) TrafficStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.GetStats())
}

func (a *API) TrafficAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.GetAlerts())
}

func (a *API) TrafficAlertsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	a.trafficQuotaSvc.ClearAlerts()
	JSONSuccess(w, nil)
}
