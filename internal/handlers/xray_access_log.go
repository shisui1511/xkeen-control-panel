package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// SetXrayAccessLogService wires device names from the router client list.
func (a *API) SetXrayAccessLogService(svc *services.XrayAccessLogService) {
	svc.Resolve = func(ip string) string {
		if a.clientResolver == nil {
			return ""
		}
		if c, ok := a.clientResolver.Resolve(ip); ok {
			if c.DisplayName != "" {
				return c.DisplayName
			}
			return c.Name
		}
		return ""
	}
	svc.PathVal = a.pathVal
	a.xrayAccessLogSvc = svc
}

// xrayAccessLogStatus builds the access log status. With the configuration layer
// on, the value in force comes from the section service (the last file with
// "log" as Xray merges them, or the panel section) and the owner of the section
// is reported; the path is read only after ReadablePath inside StatusOf. With
// the layer off the old single-file lookup is used.
func (a *API) xrayAccessLogStatus() (*services.XrayAccessLogStatus, error) {
	if a.cores != nil && a.configLayer != nil && a.configLayer.Enabled() {
		view, err := a.cores.AccessLog()
		if err != nil {
			return nil, err
		}
		status := a.xrayAccessLogSvc.StatusOf("", view.Path)
		status.Owner = view.Owner
		status.WillWrite = view.WillWrite
		return status, nil
	}
	return a.xrayAccessLogSvc.Status()
}

// XrayAccessLog handles GET /api/xray/access-log?ip=&dest=&outbound=&limit=:
// access log status plus parsed entries and a per-device summary.
func (a *API) XrayAccessLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.xrayAccessLogSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "xray access log service unavailable")
		return
	}
	status, err := a.xrayAccessLogStatus()
	if errors.Is(err, services.ErrXrayLogConfigNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	report, err := a.xrayAccessLogSvc.ReportOf(status, services.XrayAccessFilter{
		SourceIP:    q.Get("ip"),
		Destination: q.Get("dest"),
		Outbound:    q.Get("outbound"),
		Limit:       limit,
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"status": status, "report": report})
}

// XrayAccessLogToggle handles POST /api/xray/access-log/toggle {"enabled": bool}.
// The handler only writes the config; whether Xray is restarted is decided
// by the apply action (POST /api/service/control?action=apply).
func (a *API) XrayAccessLogToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.xrayAccessLogSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "xray access log service unavailable")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := a.xrayAccessLogSvc.SetEnabled(req.Enabled)
	if errors.Is(err, services.ErrXrayLogConfigNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"status": status})
}
