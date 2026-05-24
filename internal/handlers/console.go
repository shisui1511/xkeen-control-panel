package handlers

import (
	"encoding/json"
	"net/http"
)

func (a *API) ConsoleListCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.consoleSvc == nil {
		a.errorResponse(w, "Console service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.consoleSvc.GetCommands())
}

func (a *API) ConsoleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.consoleSvc == nil {
		a.errorResponse(w, "Console service unavailable", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := a.consoleSvc.Execute(req.Command)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}
