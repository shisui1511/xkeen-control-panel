package handlers

import "net/http"

func (a *API) Version(w http.ResponseWriter, r *http.Request) {
	a.jsonResponse(w, map[string]string{"version": a.srv.GetVersion()})
}
