package handlers

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Маршруты источников (/api/configlayer/sources*) работают только при включённом
// слое «Конфигурация»: при выключенном флаге они отвечают 404 config_layer_disabled,
// как остальные маршруты слоя (FND-01). Все они регистрируются через
// HandleProtected: без сессии 401, изменяющие — ещё и с X-CSRF-Token (403).
// Ни один обработчик не пишет в журнал тела запросов и ответов: полный URL
// подписки и учётные данные узлов отдают только reveal и nodes/raw (D-33, D-34).

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
	if !a.decodeLayerBody(w, r, &in) {
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
