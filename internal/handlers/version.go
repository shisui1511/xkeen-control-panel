package handlers

import "net/http"

func (a *API) Version(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	a.jsonResponse(w, map[string]string{
		"version":       a.xkeenSvc.GetVersion(),
		"panel_version": a.srv.GetVersion(),
	})
}
