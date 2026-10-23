package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
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

// geodataErrorStatus — коды ошибок записи geodata (пакет lists) и их HTTP-статусы.
// Код попадает в ответ как есть, текст берётся по ключу configlayer.routing.<код>.
var geodataErrorStatus = map[string]int{
	"set_unknown":        http.StatusBadRequest,
	"already_present":    http.StatusConflict,
	"no_jq":              http.StatusConflict,
	"geofile_update_off": http.StatusConflict,
	"comments_lost":      http.StatusConflict,
	"invalid_json":       http.StatusConflict,
	"invalid_structure":  http.StatusConflict,
	"update_unavailable": http.StatusServiceUnavailable,
	"update_failed":      http.StatusBadGateway,
	"geodata_changed":    http.StatusConflict,
}

// invalidReasonStatus — причины InvalidInputError, у которых свой код ответа
// (код совпадает с причиной).
var invalidReasonStatus = map[string]int{
	"preset_unknown":      http.StatusBadRequest,
	"country_unavailable": http.StatusBadRequest,
	"catalog_unavailable": http.StatusServiceUnavailable,
}

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
		coded   *lists.CodedError
		cycle   *routing.GroupCycleError
		refd    *routing.ReferencedError
		mixed   *configlayer.ForeignMixedError
	)
	switch {
	case errors.As(err, &invalid):
		code, key := "invalid_input", "configlayer.routing.invalid_input"
		status := http.StatusBadRequest
		if invalid.Reason == reasonFinalRequiresRedirect {
			code, key = reasonFinalRequiresRedirect, "configlayer.routing.final_requires_redirect"
		} else if st, ok := invalidReasonStatus[invalid.Reason]; ok {
			code, key, status = invalid.Reason, "configlayer.routing."+invalid.Reason, st
		}
		JSONErrorCodeParams(w, status, code, a.t(r, key),
			map[string]string{"field": invalid.Field, "reason": invalid.Reason})
	case errors.As(err, &coded) && geodataErrorStatus[coded.Code] != 0:
		JSONErrorCode(w, geodataErrorStatus[coded.Code], coded.Code, a.t(r, "configlayer.routing."+coded.Code))
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
	case errors.Is(err, routing.ErrGroupNotApplied):
		JSONErrorCode(w, http.StatusConflict, "group_not_applied", a.t(r, "configlayer.routing.group_not_applied"))
	case errors.Is(err, routing.ErrMemberInvalid):
		JSONErrorCode(w, http.StatusBadRequest, "member_invalid", a.t(r, "configlayer.routing.member_invalid"))
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

// ConfigRoutingSelect — POST /api/configlayer/routing/select {group, member}:
// выбор участника ручной группы без черновика. Mihomo получает выбор сразу, Xray —
// через перезапуск запущенного ядра. Ответ приходит после исхода по ядрам; отмена
// запроса вкладкой выбор не прерывает (запись в Live и ядро не расходятся).
func (a *API) ConfigRoutingSelect(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Group  string `json:"group"`
		Member string `json:"member"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	res, err := a.routing.Select(context.WithoutCancel(r.Context()), req.Group, req.Member)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}

// ConfigRoutingLists — GET /api/configlayer/routing/lists: каталог списков dat
// роутера (сервисы, страны, списки с примерами, расширенные наборы) и состояние
// geodata XKeen. Файлы dat не читаются — 503 catalog_unavailable.
func (a *API) ConfigRoutingLists(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireRouting(w, r) {
		return
	}
	cat, err := a.routing.Catalog()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, cat)
}

// ConfigRoutingGeodataPreview — POST /api/configlayer/routing/lists/geodata/preview
// {set}: файл настроек XKeen до и после записи набора и признак сохранения
// комментариев. set — только идентификатор набора встроенного каталога: адрес от
// клиента не принимается. Файл не меняется.
func (a *API) ConfigRoutingGeodataPreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Set string `json:"set"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	pv, err := a.routing.GeodataPreview(req.Set)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, pv)
}

// geodataUpdateTimeout — предел ожидания xkeen -ug после записи набора.
const geodataUpdateTimeout = 10 * time.Minute

// ConfigRoutingGeodataAdd — POST /api/configlayer/routing/lists/geodata/add
// {set, accept_comment_loss, update_now}: «Добавить в XKeen». Набор только из
// встроенного каталога, запись в xkeen.json с резервной копией; причины отказа —
// no_jq, geofile_update_off, already_present, comments_lost, geodata_changed (xkeen.json
// изменился между предпросмотром и записью, предпросмотр строится заново). update_now запускает
// xkeen -ug; сбой запуска — 502 update_failed, запись при этом уже сделана
// (data.written=true). Отмена запроса вкладкой запись и обновление не прерывает.
func (a *API) ConfigRoutingGeodataAdd(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Set               string `json:"set"`
		AcceptCommentLoss bool   `json:"accept_comment_loss"`
		UpdateNow         bool   `json:"update_now"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), geodataUpdateTimeout)
	defer cancel()
	st, err := a.routing.GeodataAdd(ctx, req.Set, req.AcceptCommentLoss, req.UpdateNow)
	if err != nil {
		if errors.Is(err, lists.ErrUpdateFailed) {
			JSONErrorCodeParamsData(w, http.StatusBadGateway, "update_failed", a.t(r, "configlayer.routing.update_failed"),
				nil, map[string]any{"written": true, "geodata": st})
			return
		}
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, st)
}

// ConfigRoutingPresets — GET /api/configlayer/routing/presets: данные диалога
// пресета (пресеты со странами, сервисы каталога, список рекламы, текущий выбор).
func (a *API) ConfigRoutingPresets(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireRouting(w, r) {
		return
	}
	view, err := a.routing.Presets()
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigRoutingPresetsPlan — POST /api/configlayer/routing/presets/plan {choice}:
// что уйдёт и что придёт при применении пресета. Черновик не меняется.
func (a *API) ConfigRoutingPresetsPlan(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Choice routing.PresetChoice `json:"choice"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	plan, err := a.routing.PlanPreset(req.Choice)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, plan)
}

// ConfigRoutingPresetsApply — POST /api/configlayer/routing/presets/apply
// {revision, choice, take}: пресет одной правкой черновика (группы и правила);
// take=true — вместе с взятием маршрутизации под панель.
func (a *API) ConfigRoutingPresetsApply(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64                `json:"revision"`
		Choice   routing.PresetChoice `json:"choice"`
		Take     bool                 `json:"take"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	var (
		ev  configlayer.DraftEvent
		err error
	)
	if req.Take {
		ev, err = a.routing.TakeWithPreset(req.Revision, req.Choice)
	} else {
		ev, err = a.routing.ApplyPreset(req.Revision, req.Choice)
	}
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}
