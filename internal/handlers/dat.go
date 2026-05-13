package handlers

import (
	"net/http"
)

func (a *API) DATList(w http.ResponseWriter, r *http.Request) {
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.datSvc.List())
}

func (a *API) DATUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	err := a.datSvc.Update()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"success": true,
	})
}
