package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
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

// invalidReasonStatus — причины InvalidInputError, у которых свой код ответа
// (код совпадает с причиной).
var invalidReasonStatus = map[string]int{
	"preset_unknown":      http.StatusBadRequest,
	"country_unavailable": http.StatusBadRequest,
	"catalog_unavailable": http.StatusServiceUnavailable,
}

// genericInvalidReasons — причины InvalidInputError, у которых свой код (совпадает с
// причиной), статус 400 и общий текст invalid_input: отдельного слова в словаре у
// них нет, поле и причина идут в params.
var genericInvalidReasons = map[string]bool{
	"scope_invalid":  true,
	"action_invalid": true,
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
		} else if genericInvalidReasons[invalid.Reason] {
			code = invalid.Reason
		}
		JSONErrorCodeParams(w, status, code, a.t(r, key),
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
	case errors.Is(err, routing.ErrGroupNotApplied):
		JSONErrorCode(w, http.StatusConflict, "group_not_applied", a.t(r, "configlayer.routing.group_not_applied"))
	case errors.Is(err, routing.ErrMemberInvalid):
		JSONErrorCode(w, http.StatusBadRequest, "member_invalid", a.t(r, "configlayer.routing.member_invalid"))
	case errors.Is(err, configlayer.ErrApplyBusy):
		JSONErrorCode(w, http.StatusConflict, "apply_busy", a.t(r, "configlayer.routing.apply_busy"))
	case errors.Is(err, configlayer.ErrDraftOpen):
		JSONErrorCode(w, http.StatusConflict, "draft_open", a.t(r, "configlayer.routing.draft_open"))
	case errors.Is(err, routing.ErrProfileUnknown):
		JSONErrorCode(w, http.StatusNotFound, "profile_unknown", a.t(r, "configlayer.routing.profile_unknown"))
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

// takeScopeOf разбирает область взятия из запроса: пусто или all — оба ядра, mihomo,
// xray; иное значение — InvalidInputError (400 scope_invalid).
func takeScopeOf(scope string) (routing.TakeScope, error) {
	switch routing.TakeScope(scope) {
	case "", routing.ScopeAll:
		return routing.ScopeAll, nil
	case routing.ScopeMihomo:
		return routing.ScopeMihomo, nil
	case routing.ScopeXray:
		return routing.ScopeXray, nil
	}
	return "", &routing.InvalidInputError{Field: "scope", Reason: "scope_invalid"}
}

// ConfigRoutingTake — POST /api/configlayer/routing/take {revision, scope}: желание
// «маршрутизацией управляет панель» в черновик; взятие происходит при «Применить».
// scope — all (по умолчанию), mihomo или xray: какие ядра берёт панель.
func (a *API) ConfigRoutingTake(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		Scope    string `json:"scope"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	scope, err := takeScopeOf(req.Scope)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	ev, err := a.routing.TakeScoped(req.Revision, scope)
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
// {revision, choice, take, scope}: пресет одной правкой черновика (группы и правила);
// take=true — вместе с взятием маршрутизации под панель, scope (all, mihomo, xray)
// задаёт, какие ядра берутся.
func (a *API) ConfigRoutingPresetsApply(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Revision int64                `json:"revision"`
		Choice   routing.PresetChoice `json:"choice"`
		Take     bool                 `json:"take"`
		Scope    string               `json:"scope"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	scope, err := takeScopeOf(req.Scope)
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	var ev configlayer.DraftEvent
	if req.Take {
		ev, err = a.routing.TakeWithPresetScoped(req.Revision, req.Choice, scope)
	} else {
		ev, err = a.routing.ApplyPreset(req.Revision, req.Choice)
	}
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// panelMihomoProfileName — имя профиля панели («xcp-main»): под ним старый API
// профилей называет активным профиль, когда Mihomo у панели.
var panelMihomoProfileName = strings.TrimSuffix(strings.TrimPrefix(configlayer.PanelMihomoProfile, "profiles/"), ".yaml")

// mihomoSwitchResult переводит итог конвейера в ответ активации профилей: active — name
// при успехе, prev при отказе; restarted — Mihomo перезапущен или перезагружен;
// rolled_back и error — из результата запуска; outcome — запись сохранена, а ядро не
// трогалось (остановлено, неактивно или запущены оба).
func mihomoSwitchResult(view configlayer.ApplyView, name, prev string) profileActivationResponse {
	res := &services.ActivationResult{Active: prev}
	out := profileActivationResponse{ActivationResult: res}
	r := view.Result
	if r == nil || !r.OK {
		if r != nil {
			res.RolledBack = r.RolledBack
			res.Error = r.Message
			if res.Error == "" {
				res.Error = r.Code
			}
		}
		return out
	}
	res.Active = name
	for _, rv := range view.Restart {
		if rv.Kernel != configlayer.KernelMihomo {
			continue
		}
		switch rv.Outcome {
		case configlayer.RestartOutcomeRestarted, configlayer.RestartOutcomeRestartedReload, configlayer.RestartOutcomeHotReloaded:
			res.Restarted = true
		case configlayer.RestartOutcomeDeferred:
			out.Outcome = string(services.ApplySavedKernelStopped)
		case configlayer.RestartOutcomeUntouchedIdle:
			out.Outcome = string(services.ApplySavedKernelInactive)
		case configlayer.RestartOutcomeUntouchedClash:
			out.Outcome = string(services.ApplySavedKernelConflict)
		}
	}
	return out
}

// ConfigRoutingMihomo — POST /api/configlayer/routing/mihomo {action, profile}:
// переход панель ↔ свой профиль Mihomo одним синхронным применением (D-06). action:
// activate (профиль profile становится активным вместо профиля панели), release
// (возврат на прежний профиль), return (Mihomo снова у панели). Затрагивается только
// Mihomo, Xray и его файлы не меняются. Открытый черновик — 409 draft_open без
// изменений; отказ запуска возвращает панель целиком (rolled_back, error в ответе).
// Ответ приходит после перезапуска Mihomo; отмена запроса вкладкой операцию не
// прерывает.
func (a *API) ConfigRoutingMihomo(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireRouting(w, r) {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Profile string `json:"profile"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	var (
		view       configlayer.ApplyView
		err        error
		name, prev string
	)
	switch req.Action {
	case "activate":
		name, prev = req.Profile, panelMihomoProfileName
		view, err = a.routing.ActivateProfile(ctx, req.Profile)
	case "release":
		name, prev = a.configLayer.MihomoReturnTarget(), panelMihomoProfileName
		view, err = a.routing.ReleaseMihomo(ctx)
	case "return":
		name, prev = panelMihomoProfileName, a.configLayer.MihomoActiveName()
		view, err = a.routing.ReturnMihomo(ctx)
	default:
		a.writeRoutingError(w, r, &routing.InvalidInputError{Field: "action", Reason: "action_invalid"})
		return
	}
	if err != nil {
		a.writeRoutingError(w, r, err)
		return
	}
	JSONSuccess(w, mihomoSwitchResult(view, name, prev))
}
