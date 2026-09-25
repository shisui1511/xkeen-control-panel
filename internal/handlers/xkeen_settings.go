package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) SetXKeenSettingsService(svc *services.XKeenSettingsService) {
	a.xkeenSettingsSvc = svc
}

type xkeenSettingsRequest struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// XKeenSettingsList returns XKeen's ports, excluded IPs and xkeen.json with
// validation results.
func (a *API) XKeenSettingsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.xkeenSettingsSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "xkeen settings service unavailable")
		return
	}
	files, err := a.xkeenSettingsSvc.List()
	if errors.Is(err, services.ErrXKeenConfigDirMissing) {
		JSONError(w, http.StatusNotFound, a.t(r, "error.xkeen_not_installed"))
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, files)
}

// XKeenSettingsValidate checks unsaved content without writing it.
func (a *API) XKeenSettingsValidate(w http.ResponseWriter, r *http.Request) {
	req, ok := a.decodeXKeenSettingsRequest(w, r)
	if !ok {
		return
	}
	entries, issues := a.xkeenSettingsSvc.Validate(req.Kind, req.Content)
	JSONSuccess(w, map[string]interface{}{"entries": entries, "issues": issues})
}

// XKeenSettingsSave validates and writes a settings file. Content with errors
// is rejected with 422 and the issues in data, so the UI can highlight them.
func (a *API) XKeenSettingsSave(w http.ResponseWriter, r *http.Request) {
	req, ok := a.decodeXKeenSettingsRequest(w, r)
	if !ok {
		return
	}
	file, err := a.xkeenSettingsSvc.Save(req.Kind, req.Content)
	if errors.Is(err, services.ErrXKeenSettingsInvalid) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(APIResponse{Success: false, Error: err.Error(), Data: file})
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, file)
}

func (a *API) decodeXKeenSettingsRequest(w http.ResponseWriter, r *http.Request) (xkeenSettingsRequest, bool) {
	var req xkeenSettingsRequest
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return req, false
	}
	if a.xkeenSettingsSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "xkeen settings service unavailable")
		return req, false
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	if !services.IsXKeenSettingsKind(req.Kind) {
		JSONError(w, http.StatusBadRequest, services.ErrUnknownXKeenSetting.Error())
		return req, false
	}
	return req, true
}
