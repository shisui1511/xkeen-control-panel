package handlers

import (
	"context"
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
// Остальные ошибки идут по таблицам источников и слоя.
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
		// Ошибки источников (узел или источник не найден) и слоя — по их таблицам.
		a.writeSourceError(w, r, err)
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

// ConfigRoutingGroupsPreview — POST /api/configlayer/routing/groups/preview
// {group}: какие узлы попадут в группу по каждому установленному ядру. Группа не
// сохраняется.
func (a *API) ConfigRoutingGroupsPreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Group routing.Group `json:"group"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	pv, err := a.routing.PreviewGroup(req.Group)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, pv)
}

// ConfigRoutingGroupsDelete — POST /api/configlayer/routing/groups/delete
// {revision, id, cascade: {mode, to}}: удаляет группу из черновика. Есть ссылки и
// действие не выбрано — 409 group_referenced с impact в data.
func (a *API) ConfigRoutingGroupsDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64           `json:"revision"`
		ID       string          `json:"id"`
		Cascade  routing.Cascade `json:"cascade"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.DeleteGroup(req.Revision, req.ID, req.Cascade)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingImpact — POST /api/configlayer/routing/impact {kind, id}: что
// затронет удаление группы, правила, источника или узла «Моих узлов».
func (a *API) ConfigRoutingImpact(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	imp, err := a.routing.Impact(req.Kind, req.ID)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, imp)
}

// ConfigRoutingRulesSave — POST /api/configlayer/routing/rules/save
// {revision, rule}: сохраняет правило (пустой id — новое) и отдаёт {event, rule}.
func (a *API) ConfigRoutingRulesSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64        `json:"revision"`
		Rule     routing.Rule `json:"rule"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, rule, err := a.routing.SaveRule(req.Revision, req.Rule)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]any{"event": ev, "rule": rule})
}

// ConfigRoutingRulesDelete — POST /api/configlayer/routing/rules/delete
// {revision, id}.
func (a *API) ConfigRoutingRulesDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		ID       string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.DeleteRule(req.Revision, req.ID)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingRulesMove — POST /api/configlayer/routing/rules/move
// {revision, id, to}: переставляет правило на позицию to (с нуля).
func (a *API) ConfigRoutingRulesMove(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		ID       string `json:"id"`
		To       int    `json:"to"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.MoveRule(req.Revision, req.ID, req.To)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingRulesEnable — POST /api/configlayer/routing/rules/enable
// {revision, id, enabled}: включает или выключает правило, не удаляя его.
func (a *API) ConfigRoutingRulesEnable(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		ID       string `json:"id"`
		Enabled  bool   `json:"enabled"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.SetRuleEnabled(req.Revision, req.ID, req.Enabled)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingFinal — POST /api/configlayer/routing/final {revision, target}:
// цель «Остального» (в Mihomo MATCH).
func (a *API) ConfigRoutingFinal(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64          `json:"revision"`
		Target   routing.Target `json:"target"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.SetFinal(req.Revision, req.Target)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingProbe — POST /api/configlayer/routing/probe {revision, probe}:
// параметры проверки узлов (одни на панель).
func (a *API) ConfigRoutingProbe(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64               `json:"revision"`
		Probe    routing.ProbeConfig `json:"probe"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.routing.SaveProbe(req.Revision, req.Probe)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigRoutingTakeover — GET /api/configlayer/routing/takeover: предпросмотр
// взятия для диалога (какие файлы routing Xray уйдут в копии, прежний профиль
// Mihomo). Диск не меняется.
func (a *API) ConfigRoutingTakeover(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireRouting(w, r) {
		return
	}
	pv, err := a.configLayer.RoutingPreview()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, pv)
}

// ConfigRoutingReclaim — POST /api/configlayer/routing/reclaim {kernel}: решает
// дрейф маршрутизации ядра в пользу панели. kernel — только xray или mihomo. Ответ
// приходит после перезапуска ядра; отмена запроса вкладкой операцию не прерывает.
func (a *API) ConfigRoutingReclaim(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Kernel string `json:"kernel"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if req.Kernel != configlayer.KernelXray && req.Kernel != configlayer.KernelMihomo {
		a.writeRoutingError(w, r, &routing.InvalidInputError{Field: "kernel", Reason: "kernel_invalid"})
		return
	}
	if err := a.configLayer.ReclaimRouting(context.WithoutCancel(r.Context()), req.Kernel); err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]bool{"reclaimed": true})
}
