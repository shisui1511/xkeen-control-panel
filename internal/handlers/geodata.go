package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

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

// geodataFileRE — допустимое имя dat-файла в параметрах запросов (T-146.1-29):
// только имя в каталоге dat, без путей. Сервис и индекс проверяют имя ещё раз.
var geodataFileRE = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dat$`)

const (
	// geodataProbeTimeout — предел проверки источника (HEAD и пробная загрузка).
	geodataProbeTimeout = 60 * time.Second
	// geodataWriteTimeout — предел записи в xkeen.json вместе с проверкой источника.
	// Запуск -ug по update_now идёт в фоне и в предел не входит.
	geodataWriteTimeout = 3 * time.Minute
	// geodataLookupTimeout — предел поиска «куда относится» по всем dat каталога.
	geodataLookupTimeout = 20 * time.Second
	// geodataEntriesMaxLimit — наибольшее окно записей тега.
	geodataEntriesMaxLimit = 100
	// geodataLookupLimit — сколько совпадений поиска отдаётся.
	geodataLookupLimit = 50
	// geodataFinishDelete — вид операции «Довести удаление»: не запись по WriteOp, а шаг
	// сервиса над файлом с ожидающим удалением.
	geodataFinishDelete = "finish_delete"
)

// TagsView — теги dat-файла для инспектора: имя файла как в запросе, вид файла
// (geosite или geoip) и теги с числом записей и примерами.
type TagsView struct {
	File string          `json:"file"`
	Kind string          `json:"kind"`
	Tags []lists.TagInfo `json:"tags"`
}

// writeGeodataInvalid отвечает 400 invalid_input с полем и причиной.
func (a *API) writeGeodataInvalid(w http.ResponseWriter, r *http.Request, field, reason string) {
	JSONErrorCodeParams(w, http.StatusBadRequest, "invalid_input", a.t(r, "configlayer.geodata.invalid_input"),
		map[string]string{"field": field, "reason": reason})
}

// geodataFileParam проверяет имя файла запроса; неверное — 400 file_invalid.
func (a *API) geodataFileParam(w http.ResponseWriter, r *http.Request, file string) bool {
	if !geodataFileRE.MatchString(file) {
		a.writeGeodataCode(w, r, "file_invalid")
		return false
	}
	return true
}

// requireGeodataIndex — requireGeodata плюс индекс dat-файлов (инспектор и поиск).
func (a *API) requireGeodataIndex(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireGeodata(w, r) {
		return false
	}
	if a.geodataIndex == nil {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// geodataSnapshot отдаёт свежий снимок менеджера ответом (после изменяющих запросов).
func (a *API) geodataSnapshot(w http.ResponseWriter, r *http.Request) {
	view, err := a.geodata.Snapshot()
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// geodataRunView отвечает текущим заданием менеджера из снимка.
func (a *API) geodataRunView(w http.ResponseWriter, r *http.Request) {
	view, err := a.geodata.Snapshot()
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, view.Run)
}

// geodataQueryInt читает неотрицательное целое из параметра запроса; пусто — def.
func geodataQueryInt(r *http.Request, name string, def int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// ConfigGeodataOutput — GET /api/configlayer/geodata/output?job=ug|jq&from=N: вывод
// задания начиная со смещения from (опрос живого вывода, D-16).
func (a *API) ConfigGeodataOutput(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodata(w, r) {
		return
	}
	job := r.URL.Query().Get("job")
	if job != "ug" && job != "jq" {
		a.writeGeodataInvalid(w, r, "job", "unknown")
		return
	}
	from, ok := geodataQueryInt(r, "from", 0)
	if !ok {
		a.writeGeodataInvalid(w, r, "from", "negative")
		return
	}
	JSONSuccess(w, a.geodata.Output(job, int64(from)))
}

// ConfigGeodataRun — POST /api/configlayer/geodata/run {}: «Обновить сейчас». Отвечает
// сразу: замок применения берётся синхронно (занят — 409 geodata_busy, apply_busy или
// kernel_op_in_progress), `xkeen -ug` идёт в фоне, вывод — GET /output?job=ug.
func (a *API) ConfigGeodataRun(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct{}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.geodata.RunNow(true); err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	a.geodataRunView(w, r)
}

// ConfigGeodataJQ — POST /api/configlayer/geodata/jq {}: установка jq через opkg.
// Отвечает сразу, вывод — GET /output?job=jq.
func (a *API) ConfigGeodataJQ(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct{}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.geodata.InstallJQ(); err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	a.geodataRunView(w, r)
}

// ConfigGeodataSchedule — POST /api/configlayer/geodata/schedule {enabled, day, hour,
// minute}: расписание обновления баз панели; ответ — снимок.
func (a *API) ConfigGeodataSchedule(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req geodata.Schedule
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.geodata.SetSchedule(req); err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	a.geodataSnapshot(w, r)
}

// ConfigGeodataCron — POST /api/configlayer/geodata/cron {action}: задача cron XKeen
// (adopt, keep, remove, give); ответ — снимок.
func (a *API) ConfigGeodataCron(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.geodata.CronAction(req.Action); err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	a.geodataSnapshot(w, r)
}

// ConfigGeodataProbe — POST /api/configlayer/geodata/probe {file, url, mode}: проверка
// источника без записи (add — новый, edit — смена адреса). Адрес в ответ не
// возвращается: только коды проверок, размер и вид файла.
func (a *API) ConfigGeodataProbe(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		File string `json:"file"`
		URL  string `json:"url"`
		Mode string `json:"mode"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	// Закрытая вкладка проверку не обрывает: результат остаётся в кэше сервиса.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), geodataProbeTimeout)
	defer cancel()
	res, err := a.geodata.Probe(ctx, req.File, req.URL, req.Mode)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}

// ConfigGeodataPreview — POST /api/configlayer/geodata/preview {op}: файл xkeen.json до
// и после правки и признак сохранения комментариев. Файл не меняется.
func (a *API) ConfigGeodataPreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		Op geodata.WriteOp `json:"op"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	var (
		pv  geodata.PreviewView
		err error
	)
	if req.Op.Kind == geodataFinishDelete {
		if !a.geodataFileParam(w, r, req.Op.File) {
			return
		}
		pv, err = a.geodata.PreviewFinishDelete(req.Op.File)
	} else {
		pv, err = a.geodata.Preview(req.Op)
	}
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, pv)
}

// ConfigGeodataWrite — POST /api/configlayer/geodata/write {op, base, accept_comment_loss,
// update_now}: запись в xkeen.json сразу (D-18). Блок проверки источника — 409
// probe_blocked с data.probe; сбой запуска -ug после записи — 502 update_failed с
// data.written=true; успех — снимок.
func (a *API) ConfigGeodataWrite(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		Op                geodata.WriteOp `json:"op"`
		Base              string          `json:"base"`
		AcceptCommentLoss bool            `json:"accept_comment_loss"`
		UpdateNow         bool            `json:"update_now"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	// Отмена запроса вкладкой запись не прерывает: xkeen.json не остаётся наполовину.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), geodataWriteTimeout)
	defer cancel()
	var err error
	if req.Op.Kind == geodataFinishDelete {
		if !a.geodataFileParam(w, r, req.Op.File) {
			return
		}
		err = a.geodata.FinishDelete(ctx, req.Op.File, req.Base, req.AcceptCommentLoss)
	} else {
		err = a.geodata.Write(ctx, req.Op, req.Base, req.AcceptCommentLoss, req.UpdateNow)
	}
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	a.geodataSnapshot(w, r)
}

// ConfigGeodataImpact — GET /api/configlayer/geodata/impact?file=: что затронет
// удаление или выключение файла (списки с правилами, свои правила ядра, чужие конфиги).
func (a *API) ConfigGeodataImpact(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodata(w, r) {
		return
	}
	file := r.URL.Query().Get("file")
	if !a.geodataFileParam(w, r, file) {
		return
	}
	imp, err := a.geodata.Impact(file)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, imp)
}

// ConfigGeodataDrop — POST /api/configlayer/geodata/drop {rev, file, mode, actions}:
// пометка источника на удаление или выключение с каскадом правил в черновик.
// Чужие ссылки на файл — 409 foreign_ref с data.refs, свои сырые правила — 409 raw_ref.
func (a *API) ConfigGeodataDrop(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		Rev     int64                   `json:"rev"`
		File    string                  `json:"file"`
		Mode    string                  `json:"mode"`
		Actions []routing.GeodataAction `json:"actions"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if !a.geodataFileParam(w, r, req.File) {
		return
	}
	ev, err := a.geodata.Drop(req.Rev, req.File, req.Mode, req.Actions)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigGeodataRestore — POST /api/configlayer/geodata/restore {rev, file}: снимает
// пометку удаления или выключения («Вернуть источник»).
func (a *API) ConfigGeodataRestore(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireGeodata(w, r) {
		return
	}
	var req struct {
		Rev  int64  `json:"rev"`
		File string `json:"file"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if !a.geodataFileParam(w, r, req.File) {
		return
	}
	ev, err := a.geodata.Restore(req.Rev, req.File)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigGeodataTags — GET /api/configlayer/geodata/tags?file=: теги файла с числом
// записей и примерами (инспектор).
func (a *API) ConfigGeodataTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodataIndex(w, r) {
		return
	}
	file := r.URL.Query().Get("file")
	if !a.geodataFileParam(w, r, file) {
		return
	}
	files, err := a.geodataIndex.Files()
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	kind, found := "", false
	for _, f := range files {
		if f.Name == file || slices.Contains(f.Aliases, file) {
			kind, found = f.Kind, true
			break
		}
	}
	if !found {
		a.writeGeodataCode(w, r, "list_not_found")
		return
	}
	tags, err := a.geodataIndex.Tags(file)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	JSONSuccess(w, TagsView{File: file, Kind: kind, Tags: tags})
}

// ConfigGeodataEntries — GET /api/configlayer/geodata/entries?file=&tag=&q=&offset=&limit=:
// записи тега постранично (limit не больше 100) с поиском по подстроке.
func (a *API) ConfigGeodataEntries(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodataIndex(w, r) {
		return
	}
	q := r.URL.Query()
	file := q.Get("file")
	if !a.geodataFileParam(w, r, file) {
		return
	}
	offset, ok := geodataQueryInt(r, "offset", 0)
	if !ok {
		a.writeGeodataInvalid(w, r, "offset", "negative")
		return
	}
	limit, ok := geodataQueryInt(r, "limit", geodataEntriesMaxLimit)
	if !ok {
		a.writeGeodataInvalid(w, r, "limit", "negative")
		return
	}
	limit = min(limit, geodataEntriesMaxLimit)
	page, err := a.geodataIndex.Entries(file, q.Get("tag"), q.Get("q"), offset, limit)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	if page.Entries == nil {
		page.Entries = []string{}
	}
	JSONSuccess(w, page)
}

// ConfigGeodataLookup — GET /api/configlayer/geodata/lookup?q=: в какие теги каталога
// попадает домен или IP. Поиск ограничен 20 с и закрытием вкладки.
func (a *API) ConfigGeodataLookup(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireGeodataIndex(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), geodataLookupTimeout)
	defer cancel()
	view, err := a.geodataIndex.Lookup(ctx, r.URL.Query().Get("q"), geodataLookupLimit)
	if err != nil {
		a.writeGeodataError(w, r, err)
		return
	}
	if view.Matches == nil {
		view.Matches = []lists.LookupMatch{}
	}
	JSONSuccess(w, view)
}
