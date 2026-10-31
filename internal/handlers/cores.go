package handlers

import (
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
)

// Маршруты секций ядер (/api/configlayer/cores*) работают только при включённом
// слое «Конфигурация» и созданном сервисе: иначе они отвечают 404
// config_layer_disabled, как остальные маршруты слоя (FND-01). Все они
// регистрируются через HandleProtected: без сессии 401, изменяющие — ещё и с
// X-CSRF-Token (403). Запись идёт в черновик, файлы меняет «Применить»; в ответах и
// журнале нет значений секций и secret: параметры ошибок — только имена ключей,
// файлов и групп.

// requireCores — requireLayer плюс наличие сервиса секций: без сервиса (он не
// создался в main.go) маршруты отвечают так же, как при выключенном слое.
func (a *API) requireCores(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.cores == nil {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// writeCoresError переводит ошибку сервиса секций в HTTP-ответ со стабильным кодом
// и текстом бэкенд-словаря configlayer.cores.<код>. Остальные ошибки идут по
// таблицам маршрутизации, источников и слоя.
func (a *API) writeCoresError(w http.ResponseWriter, r *http.Request, err error) {
	var ce *cores.Error
	if errors.As(err, &ce) {
		JSONErrorCodeParams(w, http.StatusBadRequest, ce.Code, a.t(r, "configlayer.cores."+ce.Code), ce.Params)
		return
	}
	a.writeRoutingError(w, r, err)
}

// ConfigCores — GET /api/configlayer/cores?kernel=: секции ядра с владельцем,
// действующими значениями и признаками правок.
func (a *API) ConfigCores(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireCores(w, r) {
		return
	}
	view, err := a.cores.View(r.URL.Query().Get("kernel"))
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}
