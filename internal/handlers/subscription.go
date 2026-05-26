package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) SubscriptionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

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

	a.jsonResponse(w, a.subscriptionSvc.Get(id))
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

	subs := a.subscriptionSvc.List()
	type result struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status bool   `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(subs))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, sub := range subs {
		if !sub.Enabled {
			continue
		}
		wg.Add(1)
		go func(s services.Subscription) {
			defer wg.Done()
			err := a.subscriptionSvc.Refresh(s.ID)
			r := result{ID: s.ID, Name: s.Name, Status: err == nil}
			if err != nil {
				r.Error = err.Error()
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(sub)
	}
	wg.Wait()

	a.jsonResponse(w, results)
}

func (a *API) SubscriptionRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	body, headers, err := a.subscriptionSvc.GetRaw(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"body":    body,
		"headers": headers,
	})
}

func (a *API) SubscriptionParseReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	report, err := a.subscriptionSvc.GetParseReport(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, report)
}

func (a *API) SubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	sub := a.subscriptionSvc.Get(id)
	if sub == nil {
		a.errorResponse(w, "subscription not found", http.StatusNotFound)
		return
	}

	nodes := sub.Nodes
	if nodes == nil {
		nodes = []services.SubscriptionNode{}
	}

	a.jsonResponse(w, nodes)
}

func (a *API) SubscriptionHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if a.subscriptionSvc.Get(id) == nil {
		a.errorResponse(w, "subscription not found", http.StatusNotFound)
		return
	}

	if a.subscriptionHealthSvc == nil {
		a.jsonResponse(w, map[string]interface{}{})
		return
	}

	// ?force=true — немедленная перепроверка
	if r.URL.Query().Get("force") == "true" {
		nodeTag := r.URL.Query().Get("node_tag")
		if nodeTag != "" {
			a.subscriptionHealthSvc.ForceCheckNode(id, nodeTag)
		} else {
			a.subscriptionHealthSvc.ForceCheck(id)
		}
	}

	health := a.subscriptionHealthSvc.GetHealth(id)
	a.jsonResponse(w, health)
}

func (a *API) SubscriptionSetActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	var body struct {
		NodeTag string `json:"node_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeTag == "" {
		a.errorResponse(w, "node_tag is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.SetActiveNode(id, body.NodeTag); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "cannot set active node in auto routing mode (balancer is managing selection)" {
			status = http.StatusConflict
		}
		a.errorResponse(w, err.Error(), status)
		return
	}

	JSONSuccess(w, map[string]string{"active_node": body.NodeTag})
}
