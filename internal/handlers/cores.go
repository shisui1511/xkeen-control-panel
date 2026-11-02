package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Маршруты секций ядер (/api/configlayer/cores*) работают только при включённом
// слое «Конфигурация» и созданном сервисе: иначе они отвечают 404
// config_layer_disabled, как остальные маршруты слоя (FND-01). Все они
// регистрируются через HandleProtected: без сессии 401, изменяющие — ещё и с
// X-CSRF-Token (403). Запись идёт в черновик, файлы меняет «Применить»; в ответах и
// журнале нет значений секций и secret: параметры ошибок — только имена ключей,
// файлов и групп.

// maxCoresBodyBytes — предел тела запросов с текстом «Дополнительно»: до 256 КиБ
// (сервис отвечает raw_too_large) в JSON-строке с экранированием.
const maxCoresBodyBytes = 2 << 20

// coresErrorStatus — код ответа по коду ошибки сервиса секций (коды совпадают с
// frontend/src/lib/coresErrorCodes.json). Код вне таблицы идёт по таблицам
// маршрутизации и слоя.
var coresErrorStatus = map[string]int{
	// Некорректный ввод и проблемы значения секции, которыми отвечает Save.
	"invalid_input":        http.StatusBadRequest,
	"core_section_invalid": http.StatusBadRequest,
	"inbound_duplicate":    http.StatusBadRequest,
	"inbound_port_invalid": http.StatusBadRequest,
	"raw_dup_key":          http.StatusBadRequest,
	"raw_pinned":           http.StatusBadRequest,
	"raw_reserved":         http.StatusBadRequest,
	"raw_syntax":           http.StatusBadRequest,
	"raw_too_large":        http.StatusBadRequest,
	"controller_no_secret": http.StatusBadRequest,
	"root_key_duplicate":   http.StatusBadRequest,
	"section_unknown":      http.StatusBadRequest,
	// Нет такого объекта.
	"group_not_found":      http.StatusNotFound,
	"kernel_not_installed": http.StatusNotFound,
	// Состояние не позволяет действие.
	"apply_busy":                http.StatusConflict,
	"comments_not_acknowledged": http.StatusConflict,
	"draft_conflict":            http.StatusConflict,
	"draft_open":                http.StatusConflict,
	"foreign_mixed":             http.StatusConflict,
	"inbounds_mixed":            http.StatusConflict,
	"mihomo_with_profile":       http.StatusConflict,
	"nothing_to_reclaim":        http.StatusConflict,
	"section_not_managed":       http.StatusConflict,
	"section_shadowed":          http.StatusConflict,
	"xkeen_too_old":             http.StatusConflict,
}

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
// и текстом бэкенд-словаря configlayer.cores.<код>. Параметры (key, file, group,
// line) — имена ключей и файлов, значений секций и secret в них нет. Остальные
// ошибки идут по таблицам маршрутизации, источников и слоя.
func (a *API) writeCoresError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		ce    *cores.Error
		mixed *configlayer.ForeignMixedError
	)
	switch {
	case errors.As(err, &ce):
		status, ok := coresErrorStatus[ce.Code]
		if !ok {
			a.writeRoutingError(w, r, err)
			return
		}
		JSONErrorCodeParams(w, status, ce.Code, a.t(r, "configlayer.cores."+ce.Code), ce.Params)
	case errors.Is(err, configlayer.ErrDraftConflict):
		JSONErrorCode(w, http.StatusConflict, "draft_conflict", a.t(r, "configlayer.cores.draft_conflict"))
	case errors.Is(err, configlayer.ErrApplyBusy):
		JSONErrorCode(w, http.StatusConflict, "apply_busy", a.t(r, "configlayer.cores.apply_busy"))
	case errors.Is(err, configlayer.ErrDraftOpen):
		JSONErrorCode(w, http.StatusConflict, "draft_open", a.t(r, "configlayer.cores.draft_open"))
	case errors.As(err, &mixed):
		JSONErrorCodeParams(w, http.StatusConflict, "foreign_mixed", a.t(r, "configlayer.cores.foreign_mixed"),
			map[string]string{"file": mixed.File})
	case errors.Is(err, configlayer.ErrForeignMixed):
		JSONErrorCode(w, http.StatusConflict, "foreign_mixed", a.t(r, "configlayer.cores.foreign_mixed"))
	case errors.Is(err, configlayer.ErrNothingToReclaim):
		JSONErrorCode(w, http.StatusConflict, "nothing_to_reclaim", a.t(r, "configlayer.cores.nothing_to_reclaim"))
	default:
		a.writeRoutingError(w, r, err)
	}
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

// ConfigCoresTakeover — GET /api/configlayer/cores/takeover?kernel=&sections=a,b:
// предпросмотр взятия для диалога (файлы, которые уйдут в копии, комментарии,
// заполнение из пресета, причина блокировки). Пустой sections — все секции Xray.
// Диск и черновик не меняются.
func (a *API) ConfigCoresTakeover(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireCores(w, r) {
		return
	}
	q := r.URL.Query()
	var ids []string
	if raw := q.Get("sections"); raw != "" {
		ids = strings.Split(raw, ",")
	}
	pv, err := a.cores.Preview(q.Get("kernel"), ids)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, pv)
}

// ConfigCoresTake — POST /api/configlayer/cores/take {revision, kernel, sections,
// ack_comments}: желание «секции ведёт панель» в черновик; файлы меняются при
// «Применить». Пустой список sections — все секции Xray.
func (a *API) ConfigCoresTake(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Revision    int64    `json:"revision"`
		Kernel      string   `json:"kernel"`
		Sections    []string `json:"sections"`
		AckComments bool     `json:"ack_comments"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.cores.Take(req.Revision, req.Kernel, req.Sections, req.AckComments)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigCoresSave — POST /api/configlayer/cores/save {revision, kernel, section,
// form, advanced}: значения формы и «Дополнительно» взятой секции в черновик.
func (a *API) ConfigCoresSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Revision int64           `json:"revision"`
		Kernel   string          `json:"kernel"`
		Section  string          `json:"section"`
		Form     json.RawMessage `json:"form"`
		Advanced string          `json:"advanced"`
	}
	if !a.decodeLayerBodyLimit(w, r, &req, maxCoresBodyBytes) {
		return
	}
	ev, err := a.cores.Save(req.Revision, req.Kernel, req.Section, req.Form, req.Advanced)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// coresSectionRequest — тело preset и release.
type coresSectionRequest struct {
	Revision int64  `json:"revision"`
	Kernel   string `json:"kernel"`
	Section  string `json:"section"`
}

// ConfigCoresPreset — POST /api/configlayer/cores/preset {revision, kernel,
// section}: заменяет значения секции в черновике значениями пресета.
func (a *API) ConfigCoresPreset(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req coresSectionRequest
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.cores.Preset(req.Revision, req.Kernel, req.Section)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigCoresRelease — POST /api/configlayer/cores/release {revision, kernel,
// section}: желание вернуть секцию прежнему владельцу; файлы возвращаются при
// «Применить».
func (a *API) ConfigCoresRelease(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req coresSectionRequest
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.cores.Release(req.Revision, req.Kernel, req.Section)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigCoresForceProxy — POST /api/configlayer/cores/force-proxy {revision,
// kernel, group}: группа, в которую панель направляет входящие force-proxy.
func (a *API) ConfigCoresForceProxy(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		Kernel   string `json:"kernel"`
		Group    string `json:"group"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.cores.SetForceProxyGroup(req.Revision, req.Kernel, req.Group)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigCoresRawParse — POST /api/configlayer/cores/raw/parse {kernel, section,
// text}: разбор сырого блока «Дополнительно» без записи. Ревизия не нужна.
func (a *API) ConfigCoresRawParse(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Kernel  string `json:"kernel"`
		Section string `json:"section"`
		Text    string `json:"text"`
	}
	if !a.decodeLayerBodyLimit(w, r, &req, maxCoresBodyBytes) {
		return
	}
	res, err := a.cores.ParseRaw(req.Kernel, req.Section, req.Text)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}

// ConfigCoresReclaim — POST /api/configlayer/cores/reclaim {kernel, section}:
// «Вернуть файл панели» — решает дрейф секции в пользу панели. Идёт под замком
// слоя; отмена запроса вкладкой операцию не прерывает.
func (a *API) ConfigCoresReclaim(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Kernel  string `json:"kernel"`
		Section string `json:"section"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.cores.Reclaim(context.WithoutCancel(r.Context()), req.Kernel, req.Section); err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]bool{"reclaimed": true})
}

// ConfigCoresAccessLog — POST /api/configlayer/cores/access-log {enabled,
// ack_comments}: включает или выключает access-лог Xray через секцию log. Секция у
// панели — применяется только log.access поверх применённого состояния (xray -test,
// запись, перезапуск запущенного ядра), остальные правки черновика ждут «Применить».
// Секция за XKeen или пользователем берётся и применяется одним шагом, если
// черновик чист; иначе 409 draft_open. Каталог журнала создаётся только для пути
// внутри каталога журналов (проверка пути валидатором). Ответ приходит после
// перезапуска ядра; отмена запроса вкладкой операцию не прерывает.
func (a *API) ConfigCoresAccessLog(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireCores(w, r) {
		return
	}
	var req struct {
		Enabled     bool `json:"enabled"`
		AckComments bool `json:"ack_comments"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	dirOK := func(path string) (string, bool) {
		clean, err := a.pathVal.ValidateUnder(path, utils.LogRoot)
		return clean, err == nil
	}
	view, err := a.cores.SetAccessLog(context.WithoutCancel(r.Context()), req.Enabled, req.AckComments, dirOK)
	if err != nil {
		a.writeCoresError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}
