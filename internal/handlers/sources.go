package handlers

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Маршруты источников (/api/configlayer/sources*) работают только при включённом
// слое «Конфигурация»: при выключенном флаге они отвечают 404 config_layer_disabled,
// как остальные маршруты слоя (FND-01). Все они регистрируются через
// HandleProtected: без сессии 401, изменяющие — ещё и с X-CSRF-Token (403).
// Ни один обработчик не пишет в журнал тела запросов и ответов: полный URL
// подписки и учётные данные узлов отдают только reveal и nodes/raw (D-33, D-34).

// maxSourcesInputBody — предел тела маршрутов с вводом узлов (preview, nodes/preview,
// nodes/save). JSON-экранирование (\n, \", \\) и обёртка {text, files} делают тело
// больше содержимого: при пределе, равном nodes.MaxInputBytes, ввод у границы
// обрезался бы MaxBytesReader безликим invalid_request и не доходил до
// распознавателя с понятным too_large. Размер содержимого проверяет Recognize.
const maxSourcesInputBody = 2*nodes.MaxInputBytes + 64<<10

// urlInText находит адрес со схемой в произвольном тексте ошибки.
var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

// maskURLsInText заменяет адреса в тексте ошибки: сообщения сетевых ошибок
// содержат запрошенный URL, а у подписки в нём могут быть ключи доступа.
func maskURLsInText(s string) string {
	return urlInText.ReplaceAllString(s, "[url]")
}

// requireSources — requireLayer плюс наличие сервиса источников: без сервиса
// (он не создался в main.go) маршруты отвечают так же, как при выключенном слое.
func (a *API) requireSources(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.sources == nil {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// writeSourceError переводит ошибку сервиса источников в HTTP-ответ. Ошибка
// ввода или ответа провайдера уходит стабильным кодом и параметрами без текста
// (тексты — на фронтенде, D-31); в params нет ни фрагментов ввода, ни ответа
// провайдера. Остальные ошибки идут по таблице слоя.
func (a *API) writeSourceError(w http.ResponseWriter, r *http.Request, err error) {
	var serr *nodes.SourceError
	switch {
	case errors.As(err, &serr):
		JSONErrorCodeParams(w, http.StatusBadRequest, serr.Code, a.t(r, "configlayer.source_error"), serr.Params)
	case errors.Is(err, sources.ErrSourceNotFound):
		JSONErrorCode(w, http.StatusNotFound, "source_not_found", a.t(r, "configlayer.source_not_found"))
	case errors.Is(err, sources.ErrNodeNotFound):
		JSONErrorCode(w, http.StatusNotFound, "node_not_found", a.t(r, "configlayer.node_not_found"))
	case errors.Is(err, sources.ErrPreviewExpired):
		JSONErrorCode(w, http.StatusGone, "preview_expired", a.t(r, "configlayer.preview_expired"))
	case errors.Is(err, sources.ErrSourceDisabled):
		JSONErrorCode(w, http.StatusConflict, "source_disabled", a.t(r, "configlayer.source_disabled"))
	case errors.Is(err, sources.ErrSourceChanged):
		JSONErrorCode(w, http.StatusConflict, "source_changed", a.t(r, "configlayer.source_changed"))
	case errors.Is(err, sources.ErrInvalidInput):
		JSONErrorCode(w, http.StatusBadRequest, "invalid_request", a.t(r, "configlayer.invalid_request"))
	default:
		a.writeLayerError(w, r, err)
	}
}

// ConfigSources — GET /api/configlayer/sources: представление источников.
func (a *API) ConfigSources(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireSources(w, r) {
		return
	}
	view, err := a.sources.View()
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigSourcesPreview — POST /api/configlayer/sources/preview {text, files}:
// распознаёт ввод поля «Добавить» и возвращает предпросмотр с токеном. Запросы к
// провайдерам привязаны к контексту запроса: закрытая вкладка их прерывает.
func (a *API) ConfigSourcesPreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var in nodes.Input
	if !a.decodeLayerBodyLimit(w, r, &in, maxSourcesInputBody) {
		return
	}
	view, err := a.sources.Preview(r.Context(), in)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigSourcesAdd — POST /api/configlayer/sources/add {revision, token}:
// кладёт результат предпросмотра в черновик.
func (a *API) ConfigSourcesAdd(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		Token    string `json:"token"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	res, err := a.sources.Add(req.Revision, req.Token)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}

// ConfigSourcesUpdate — POST /api/configlayer/sources/update {revision, id, patch}:
// правка настроек источника через черновик.
func (a *API) ConfigSourcesUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Revision int64         `json:"revision"`
		ID       string        `json:"id"`
		Patch    sources.Patch `json:"patch"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.sources.Update(req.Revision, req.ID, req.Patch)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigSourcesDelete — POST /api/configlayer/sources/delete {revision, id}.
func (a *API) ConfigSourcesDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		ID       string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	// С сервисом маршрутизации удаление идёт каскадом: ссылки групп на источник
	// и его узлы убираются той же ревизией (SRC-07).
	var (
		ev  configlayer.DraftEvent
		err error
	)
	if a.routing != nil {
		ev, err = a.routing.DeleteSource(req.Revision, req.ID)
	} else {
		ev, err = a.sources.Delete(req.Revision, req.ID)
	}
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigSourcesRefresh — POST /api/configlayer/sources/refresh {id}: обновление
// подписки по запросу пользователя. Ответ приходит после запроса к провайдеру и
// конвейера (на MIPS — до десятков секунд) и несёт итог для тоста инициатора
// (D-08); остальные вкладки узнают из события SSE sources.
func (a *API) ConfigSourcesRefresh(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	out, err := a.sources.Refresh(r.Context(), req.ID)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, out)
}

// writeSensitive пишет успешный ответ с секретом (полный URL, исходный текст
// узла): без кэширования и без записи в журнал.
func writeSensitive(w http.ResponseWriter, data any) {
	w.Header().Set("Cache-Control", "no-store")
	JSONSuccess(w, data)
}

// ConfigSourcesReveal — POST /api/configlayer/sources/reveal {id}: полный URL
// подписки по явному действию «Показать» (D-33). Единственный маршрут, где он
// есть в ответе, кроме nodes/raw для учётных данных узла.
func (a *API) ConfigSourcesReveal(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	u, err := a.sources.Reveal(req.ID)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	writeSensitive(w, map[string]string{"url": u})
}

// ConfigSourcesDiagnostics — GET /api/configlayer/sources/diagnostics?id=&kernel=:
// замаскированная диагностика ответа подписки по ядру.
func (a *API) ConfigSourcesDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireSources(w, r) {
		return
	}
	q := r.URL.Query()
	view, err := a.sources.Diagnostics(q.Get("id"), q.Get("kernel"))
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigSourcesNodePreview — POST /api/configlayer/sources/nodes/preview
// {text, entry_id?}: предпросмотр правки или добавления одного узла «Моих узлов».
// Текст, который не даёт ровно один узел, — 400 edit_single_node, а в data —
// предпросмотр этого текста: по нему видно, какая строка и почему не распознана.
func (a *API) ConfigSourcesNodePreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Text    string `json:"text"`
		EntryID string `json:"entry_id"`
	}
	if !a.decodeLayerBodyLimit(w, r, &req, maxSourcesInputBody) {
		return
	}
	view, err := a.sources.PreviewNode(r.Context(), req.Text, req.EntryID)
	var serr *nodes.SourceError
	switch {
	case errors.As(err, &serr):
		JSONErrorCodeParamsData(w, http.StatusBadRequest, serr.Code, a.t(r, "configlayer.source_error"), serr.Params, view)
		return
	case err != nil:
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigSourcesNodeRaw — POST /api/configlayer/sources/nodes/raw {entry_id}:
// исходный текст узла без маски для режима «Изменить» (D-34).
func (a *API) ConfigSourcesNodeRaw(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		EntryID string `json:"entry_id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	view, err := a.sources.NodeRaw(req.EntryID)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	writeSensitive(w, view)
}

// ConfigSourcesNodeSave — POST /api/configlayer/sources/nodes/save
// {revision, entry_id?, name, text}: сохраняет узел в черновик.
func (a *API) ConfigSourcesNodeSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		EntryID  string `json:"entry_id"`
		Name     string `json:"name"`
		Text     string `json:"text"`
	}
	if !a.decodeLayerBodyLimit(w, r, &req, maxSourcesInputBody) {
		return
	}
	ev, err := a.sources.SaveNode(req.Revision, req.EntryID, req.Name, req.Text)
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigSourcesNodeDelete — POST /api/configlayer/sources/nodes/delete
// {revision, entry_id}.
func (a *API) ConfigSourcesNodeDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireSources(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		EntryID  string `json:"entry_id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	// Как и удаление источника: с сервисом маршрутизации — каскадом (SRC-07).
	var (
		ev  configlayer.DraftEvent
		err error
	)
	if a.routing != nil {
		ev, err = a.routing.DeleteNode(req.Revision, req.EntryID)
	} else {
		ev, err = a.sources.DeleteNode(req.Revision, req.EntryID)
	}
	if err != nil {
		a.writeSourceError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}
