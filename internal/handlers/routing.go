package handlers

import (
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Маршруты маршрутизации (/api/configlayer/routing*) работают только при
// включённом слое «Конфигурация» и созданном сервисе: иначе они отвечают 404
// config_layer_disabled, как остальные маршруты слоя (FND-01). Все они
// регистрируются через HandleProtected: без сессии 401, изменяющие — ещё и с
// X-CSRF-Token (403). Ответы и журнал не содержат секретов узлов: представления
// сервиса несут только имена и ссылки.

// reasonFinalRequiresRedirect — причина отказа удаления группы, на которую
// ссылается «Остальное»: у него цель обязательна (код совпадает с routing).
const reasonFinalRequiresRedirect = "final_requires_redirect"

// requireRouting — requireLayer плюс наличие сервиса маршрутизации: без сервиса
// (он не создался в main.go) маршруты отвечают так же, как при выключенном слое.
func (a *API) requireRouting(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.routing == nil {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// writeRoutingError переводит ошибку сервиса маршрутизации и операций владения в
// HTTP-ответ со стабильным кодом. Параметры (field, reason, group, file) — имена
// полей, коды причин и имена файлов настроек; ни узлов, ни адресов в них нет.
// Остальные ошибки идут по таблице слоя.
func (a *API) writeRoutingError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		invalid *routing.InvalidInputError
		cycle   *routing.GroupCycleError
		refd    *routing.ReferencedError
		mixed   *configlayer.ForeignMixedError
	)
	switch {
	case errors.As(err, &invalid):
		code, key := "invalid_input", "configlayer.routing.invalid_input"
		if invalid.Reason == reasonFinalRequiresRedirect {
			code, key = reasonFinalRequiresRedirect, "configlayer.routing.final_requires_redirect"
		}
		JSONErrorCodeParams(w, http.StatusBadRequest, code, a.t(r, key),
			map[string]string{"field": invalid.Field, "reason": invalid.Reason})
	case errors.Is(err, routing.ErrGroupNotFound):
		JSONErrorCode(w, http.StatusNotFound, "group_not_found", a.t(r, "configlayer.routing.group_not_found"))
	case errors.Is(err, routing.ErrRuleNotFound):
		JSONErrorCode(w, http.StatusNotFound, "rule_not_found", a.t(r, "configlayer.routing.rule_not_found"))
	case errors.Is(err, routing.ErrNameTaken):
		JSONErrorCode(w, http.StatusConflict, "group_name_taken", a.t(r, "configlayer.routing.group_name_taken"))
	case errors.As(err, &cycle):
		JSONErrorCodeParams(w, http.StatusBadRequest, "group_cycle", a.t(r, "configlayer.routing.group_cycle"),
			map[string]string{"group": cycle.Group})
	case errors.As(err, &refd):
		JSONErrorCodeParamsData(w, http.StatusConflict, "group_referenced", a.t(r, "configlayer.routing.group_referenced"),
			nil, map[string]any{"impact": refd.Impact})
	case errors.Is(err, configlayer.ErrDraftConflict):
		JSONErrorCode(w, http.StatusConflict, "draft_conflict", a.t(r, "configlayer.routing.draft_conflict"))
	case errors.As(err, &mixed):
		JSONErrorCodeParams(w, http.StatusConflict, "foreign_mixed", a.t(r, "configlayer.routing.foreign_mixed"),
			map[string]string{"file": mixed.File})
	case errors.Is(err, configlayer.ErrForeignMixed):
		JSONErrorCode(w, http.StatusConflict, "foreign_mixed", a.t(r, "configlayer.routing.foreign_mixed"))
	case errors.Is(err, configlayer.ErrNothingToReclaim):
		JSONErrorCode(w, http.StatusConflict, "nothing_to_reclaim", a.t(r, "configlayer.routing.nothing_to_reclaim"))
	case errors.Is(err, configlayer.ErrApplyBusy):
		JSONErrorCode(w, http.StatusConflict, "apply_busy", a.t(r, "configlayer.routing.apply_busy"))
	default:
		a.writeLayerError(w, r, err)
	}
}

// ConfigRouting — GET /api/configlayer/routing: представление маршрутизации
// (группы, правила, «Остальное», проверка узлов, проблемы).
func (a *API) ConfigRouting(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireRouting(w, r) {
		return
	}
	view, err := a.routing.View()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigRoutingGroupsSave — POST /api/configlayer/routing/groups/save
// {revision, group}: сохраняет группу в черновик (пустой id — новая) и отдаёт
// {event, group} с присвоенным id.
func (a *API) ConfigRoutingGroupsSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64         `json:"revision"`
		Group    routing.Group `json:"group"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, g, err := a.routing.SaveGroup(req.Revision, req.Group)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]any{"event": ev, "group": g})
}

// ConfigRoutingTake — POST /api/configlayer/routing/take {revision}: желание
// «маршрутизацией управляет панель» в черновик; взятие происходит при «Применить».
func (a *API) ConfigRoutingTake(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.Take(req.Revision)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingRelease — POST /api/configlayer/routing/release {revision}: отказ
// от управления маршрутизацией в черновик; возврат файлов происходит при «Применить».
func (a *API) ConfigRoutingRelease(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.Release(req.Revision)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}
