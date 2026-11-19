package handlers

import (
	"encoding/json"
	"io"
	"net/http"
)

// ConfigLayerLegacy обрабатывает GET /api/configlayer/legacy.
func (a *API) ConfigLayerLegacy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireConfigLayer(w, r) {
		return
	}
	JSONSuccess(w, a.configLayer.Legacy())
}

// LegacyClose обрабатывает POST /api/configlayer/legacy/close.
func (a *API) LegacyClose(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireConfigLayer(w, r) || !a.requireEmptyLegacyBody(w, r) {
		return
	}
	summary, err := a.configLayer.MarkLegacyClosed()
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, summary)
}

// LegacyClean обрабатывает POST /api/configlayer/legacy/clean.
func (a *API) LegacyClean(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireConfigLayer(w, r) || !a.requireEmptyLegacyBody(w, r) {
		return
	}
	if err := a.configLayer.CleanLegacy(); err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, struct{}{})
}

func (a *API) requireEmptyLegacyBody(w http.ResponseWriter, r *http.Request) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	var body *struct{}
	err := dec.Decode(&body)
	if r.URL.RawQuery == "" && (err == io.EOF || err == nil && body != nil && dec.Decode(new(any)) == io.EOF) {
		return true
	}
	JSONErrorCode(w, http.StatusBadRequest, "invalid_request", a.t(r, "configlayer.invalid_request"))
	return false
}
