package handlers

import (
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/geodata"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Маршруты менеджера geodata (/api/configlayer/geodata*) работают только при
// включённом слое «Конфигурация» и созданном сервисе: иначе они отвечают 404
// config_layer_disabled, как остальные маршруты слоя. Все они регистрируются через
// HandleProtected: без сессии 401, изменяющие (только POST) — ещё и с X-CSRF-Token
// (403). Они не входят в kernelGatedPrefixes: менеджер не обращается к ядру, а
// запуск `xkeen -ug` идёт под замком применения слоя (geodata_busy / apply_busy).

// geodataManagerStatus — коды ошибок менеджера geodata (пакеты lists, geodata и
// routing) и их HTTP-статусы. Код попадает в ответ как есть, текст берётся по ключу
// configlayer.geodata.<код>. Коды занятости (geodata_busy, apply_busy,
// kernel_op_in_progress) и draft_conflict разбирает writeLayerError.
var geodataManagerStatus = map[string]int{
	// Недопустимый ввод.
	"name_invalid":     http.StatusBadRequest,
	"url_invalid":      http.StatusBadRequest,
	"schedule_invalid": http.StatusBadRequest,
	"mode_invalid":     http.StatusBadRequest,
	"actions_invalid":  http.StatusBadRequest,
	"file_invalid":     http.StatusBadRequest,
	"lookup_query":     http.StatusBadRequest,
	"invalid_input":    http.StatusBadRequest,
	"set_unknown":      http.StatusBadRequest,
	// Не найдено.
	"entry_not_found": http.StatusNotFound,
	"list_not_found":  http.StatusNotFound,
	"cron_missing":    http.StatusNotFound,
	// Конфликты состояния.
	"already_present":    http.StatusConflict,
	"name_taken":         http.StatusConflict,
	"comments_lost":      http.StatusConflict,
	"geodata_changed":    http.StatusConflict,
	"no_jq":              http.StatusConflict,
	"geofile_update_off": http.StatusConflict,
	"xkeen_json_invalid": http.StatusConflict,
	"invalid_json":       http.StatusConflict,
	"invalid_structure":  http.StatusConflict,
	"probe_blocked":      http.StatusConflict,
	"foreign_ref":        http.StatusConflict,
	"raw_ref":            http.StatusConflict,
	"not_pending":        http.StatusConflict,
	"cron_nonstandard":   http.StatusConflict,
	"xkeen_too_old":      http.StatusConflict,
	// Прочее.
	"xkeen_missing":      http.StatusPreconditionFailed,
	"cron_failed":        http.StatusInternalServerError,
	"update_failed":      http.StatusBadGateway,
	"update_unavailable": http.StatusBadGateway,
}

// geodataInvalidReasonCode — причины InvalidInputError сервиса маршрутизации
// (каскад пометки источника), у которых свой код ответа. Остальные причины идут как
// invalid_input с параметрами field и reason.
var geodataInvalidReasonCode = map[string]string{
	"file_invalid":    "file_invalid",
	"mode_invalid":    "mode_invalid",
	"actions_invalid": "actions_invalid",
	"action_unknown":  "actions_invalid",
	"replace_invalid": "actions_invalid",
	"drop_not_found":  "not_pending",
}

// requireGeodata — requireLayer плюс наличие менеджера: без сервиса (он не создался в
// main.go) маршруты отвечают так же, как при выключенном слое.
func (a *API) requireGeodata(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.geodata == nil {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// writeGeodataError переводит ошибку менеджера в HTTP-ответ со стабильным кодом.
// Блок проверки источника и чужие ссылки несут данные для окна; сбой запуска после
// сделанной записи — data.written. Ошибки слоя (занятость, конфликт черновика) и
// ошибки без кода идут по таблице слоя. Секретов в ответах нет: тексты берутся из
// словаря по коду, адреса источников в ответ не попадают.
func (a *API) writeGeodataError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		blocked *geodata.ProbeBlockedError
		foreign *geodata.ForeignRefError
		invalid *routing.InvalidInputError
	)
	switch {
	case errors.As(err, &blocked):
		JSONErrorCodeParamsData(w, http.StatusConflict, "probe_blocked", a.t(r, "configlayer.geodata.probe_blocked"),
			nil, map[string]any{"probe": blocked.Result})
		return
	case errors.As(err, &foreign):
		refs := foreign.Refs
		if refs == nil {
			refs = []geodata.ForeignRef{}
		}
		JSONErrorCodeParamsData(w, http.StatusConflict, "foreign_ref", a.t(r, "configlayer.geodata.foreign_ref"),
			nil, map[string]any{"refs": refs})
		return
	case errors.Is(err, lists.ErrUpdateFailed):
		JSONErrorCodeParamsData(w, http.StatusBadGateway, "update_failed", a.t(r, "configlayer.geodata.update_failed"),
			nil, map[string]any{"written": true})
		return
	case errors.As(err, &invalid):
		code, ok := geodataInvalidReasonCode[invalid.Reason]
		if !ok {
			JSONErrorCodeParams(w, http.StatusBadRequest, "invalid_input", a.t(r, "configlayer.geodata.invalid_input"),
				map[string]string{"field": invalid.Field, "reason": invalid.Reason})
			return
		}
		JSONErrorCode(w, geodataManagerStatus[code], code, a.t(r, "configlayer.geodata."+code))
		return
	case errors.Is(err, lists.ErrLookupQuery):
		a.writeGeodataCode(w, r, "lookup_query")
		return
	case errors.Is(err, lists.ErrListNotFound):
		a.writeGeodataCode(w, r, "list_not_found")
		return
	}
	switch code := lists.ErrorCode(err); code {
	case "":
		// Без кода пакетов — ошибки слоя и неклассифицированные сбои.
	case "geodata_busy", "apply_busy":
		// Замок держит менеджер или идёт «Применить»: тексты слоя.
		a.writeLayerError(w, r, busyLayerError(code))
		return
	case "bad_op":
		JSONErrorCodeParams(w, http.StatusBadRequest, "invalid_input", a.t(r, "configlayer.geodata.invalid_input"),
			map[string]string{"field": "op", "reason": "bad_op"})
		return
	default:
		if _, known := geodataManagerStatus[code]; known {
			a.writeGeodataCode(w, r, code)
			return
		}
	}
	a.writeLayerError(w, r, err)
}

// busyLayerError сводит код занятости менеджера к ошибке слоя, у которой есть таблица
// ответов: geodata_busy — замок держит менеджер, иначе идёт «Применить».
func busyLayerError(code string) error {
	if code == "geodata_busy" {
		return configlayer.ErrGeodataBusy
	}
	return configlayer.ErrApplyBusy
}

// writeGeodataCode пишет ответ по коду из таблицы статусов.
func (a *API) writeGeodataCode(w http.ResponseWriter, r *http.Request, code string) {
	JSONErrorCode(w, geodataManagerStatus[code], code, a.t(r, "configlayer.geodata."+code))
}

// ConfigGeodata — GET /api/configlayer/geodata: снимок менеджера для вкладки «Списки»
// (источники, загрузка, расписание, cron, задание, итог последнего запуска).
func (a *API) ConfigGeodata(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodata(w, r) {
		return
	}
	view, err := a.geodata.Snapshot()
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}
