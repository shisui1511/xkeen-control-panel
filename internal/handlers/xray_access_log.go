package handlers

import (
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

// xrayAccessLogStatus builds the access log status from the section service:
// the value in force (the last file with "log" as Xray merges them, or the
// panel section) and the owner of the section; the path is read only after
// ReadablePath inside StatusOf.
func (a *API) xrayAccessLogStatus() (*services.XrayAccessLogStatus, error) {
	if a.cores == nil {
		return nil, errors.New("cores service unavailable")
	}
	view, err := a.cores.AccessLog()
	if err != nil {
		return nil, err
	}
	status := a.xrayAccessLogSvc.StatusOf("", view.Path)
	status.Owner = view.Owner
	status.WillWrite = view.WillWrite
	return status, nil
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
	// Без сервиса секций ядер (не создался в main.go) статус журнала взять неоткуда:
	// ответ как у остальных маршрутов слоя — 404 not_found с переведённым текстом.
	if a.cores == nil {
		JSONErrorCode(w, http.StatusNotFound, "not_found", a.t(r, "error.not_found"))
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
