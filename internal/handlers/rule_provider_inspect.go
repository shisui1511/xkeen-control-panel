package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// RuleProvidersInfo handles GET /api/rule-providers/info: rule-providers from
// the Mihomo config with the state of their local files.
func (a *API) RuleProvidersInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	list, err := a.routeTracerSvc.ListRuleProviders()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, list)
}

// RuleProviderContent handles GET /api/rule-providers/content?name=&q=&offset=&limit=.
func (a *API) RuleProviderContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	q := r.URL.Query()
	name := q.Get("name")
	if name == "" || len(name) > 256 {
		JSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	search := q.Get("q")
	if len(search) > 256 {
		search = search[:256]
	}

	// First read of a large MRS set converts it (seconds on a router CPU).
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	page, err := a.routeTracerSvc.RuleProviderContent(ctx, name, search, offset, limit)
	switch {
	case errors.Is(err, services.ErrRuleProviderNotFound):
		JSONError(w, http.StatusNotFound, err.Error())
	case err != nil:
		// Typically the provider file is not downloaded yet.
		JSONError(w, http.StatusConflict, err.Error())
	default:
		JSONSuccess(w, page)
	}
}

// RuleProviderCheckURL handles POST /api/rule-providers/check-url?name=.
func (a *API) RuleProviderCheckURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || len(name) > 256 {
		JSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	res, err := a.routeTracerSvc.CheckRuleProviderURL(r.Context(), name)
	if errors.Is(err, services.ErrRuleProviderNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, res)
}
