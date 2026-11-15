package handlers

import "net/http"

// Маршруты страницы «Прокси» при слое «Конфигурация» (фаза 150) читают
// применённое состояние через сервис маршрутизации и защищены HandleProtected.
// Флаг слоя проверяется на каждый запрос, независимо от активного ядра.
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
