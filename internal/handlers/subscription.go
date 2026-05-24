package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) SubscriptionList(w http.ResponseWriter, r *http.Request) {

	subs := a.subscriptionSvc.List()
	a.jsonResponse(w, subs)
}

func (a *API) SubscriptionAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var sub services.Subscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if sub.URL == "" {
		a.errorResponse(w, "URL is required", http.StatusBadRequest)
		return
	}

	if sub.Interval == 0 {
		sub.Interval = 24 // default 24 hours
	}

	if err := a.subscriptionSvc.Add(&sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, sub)
}

func (a *API) SubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	var sub services.Subscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Update(id, &sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, sub)
}

func (a *API) SubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Delete(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) SubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Refresh(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sub := a.subscriptionSvc.Get(id)
	a.jsonResponse(w, sub)
}

func (a *API) SubscriptionRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var results []map[string]interface{}
	for _, sub := range a.subscriptionSvc.List() {
		if !sub.Enabled {
			continue
		}
		// Check if update is due
		if !sub.LastUpdate.IsZero() && time.Since(sub.LastUpdate) < time.Duration(sub.Interval)*time.Hour {
			continue
		}

		err := a.subscriptionSvc.Refresh(sub.ID)
		results = append(results, map[string]interface{}{
			"id":     sub.ID,
			"name":   sub.Name,
			"status": err == nil,
			"error":  err,
		})
	}

	a.jsonResponse(w, results)
}
