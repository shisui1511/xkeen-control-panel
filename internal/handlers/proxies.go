package handlers

import (
	"context"
	"net/http"
)

// Маршруты страницы «Прокси» при слое «Конфигурация» (фаза 150) читают
// применённое состояние через сервис маршрутизации и защищены HandleProtected.
// Работают сразу после старта, независимо от активного ядра: проверяется только
// наличие созданных в main.go служб, без них — 404 not_found.
func (a *API) requireProxies(w http.ResponseWriter, r *http.Request) bool {
	return a.requireRouting(w, r)
}

func (a *API) ConfigProxies(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireProxies(w, r) {
		return
	}
	view, err := a.routing.RuntimeView()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

func (a *API) ConfigProxiesNow(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireProxies(w, r) {
		return
	}
	now, err := a.routing.RuntimeNow(r.Context())
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, now)
}

func (a *API) requirePing(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireProxies(w, r) {
		return false
	}
	if a.ping == nil {
		JSONErrorCode(w, http.StatusNotFound, "not_found", a.t(r, "error.not_found"))
		return false
	}
	return true
}

func (a *API) ConfigProxiesResults(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requirePing(w, r) {
		return
	}
	JSONSuccess(w, a.ping.Snapshot())
}

func (a *API) ConfigProxiesPing(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requirePing(w, r) {
		return
	}
	var body struct {
		Scope string `json:"scope"`
		Group string `json:"group"`
	}
	if !a.decodeLayerBody(w, r, &body) {
		return
	}
	req, err := a.routing.PingRequest(body.Scope, body.Group)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	start, err := a.ping.Launch(req)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, start)
}

func (a *API) ConfigProxiesPingCancel(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requirePing(w, r) {
		return
	}
	batch, err := a.ping.Cancel()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, batch)
}

func (a *API) ConfigProxiesConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireProxies(w, r) {
		return
	}
	var body struct {
		Group string `json:"group"`
	}
	if !a.decodeLayerBody(w, r, &body) {
		return
	}
	res, err := a.routing.Confirm(context.WithoutCancel(r.Context()), body.Group)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}
