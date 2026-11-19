package handlers

import (
	"encoding/json"
	"io"
	"net/http"
)

// LegacyClean обрабатывает POST /api/configlayer/legacy/clean.
func (a *API) LegacyClean(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) || !a.requireEmptyLegacyBody(w, r) {
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
