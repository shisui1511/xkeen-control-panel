package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// maxLayerBodyBytes — предел тела запросов слоя (T-144-34).
const maxLayerBodyBytes = 1 << 20

// layerSSEPing — период комментария ping в потоке событий слоя.
const layerSSEPing = 20 * time.Second

// requireLayer отвечает 404 config_layer_disabled, если слой не подключён или
// флаг config_layer выключен: панель ведёт себя как до появления слоя (FND-01).
func (a *API) requireLayer(w http.ResponseWriter, r *http.Request) bool {
	if a.configLayer == nil || !a.configLayer.Enabled() {
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
		return false
	}
	return true
}

// writeLayerError переводит ошибку слоя в HTTP-ответ по таблице из контракта API.
// Тексты — переведённые строки; внутренние пути и тексты ошибок ФС наружу не
// уходят.
func (a *API) writeLayerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, configlayer.ErrDisabled):
		JSONErrorCode(w, http.StatusNotFound, "config_layer_disabled", a.t(r, "configlayer.disabled"))
	case errors.Is(err, configlayer.ErrDraftConflict):
		JSONErrorCode(w, http.StatusConflict, "draft_conflict", a.t(r, "configlayer.draft_conflict"))
	case errors.Is(err, configlayer.ErrInvalidSection), errors.Is(err, configlayer.ErrUnknownDiagAction):
		JSONErrorCode(w, http.StatusBadRequest, "invalid_request", a.t(r, "configlayer.invalid_request"))
	case errors.Is(err, configlayer.ErrApplyBusy):
		JSONErrorCode(w, http.StatusConflict, "apply_busy", a.t(r, "configlayer.apply_busy"))
	case errors.Is(err, configlayer.ErrKernelBusy):
		JSONErrorCode(w, http.StatusConflict, "kernel_op_in_progress", a.t(r, "kernel.op_in_progress"))
	case errors.Is(err, configlayer.ErrDriftBlocked):
		JSONErrorCode(w, http.StatusConflict, "drift_blocked", a.t(r, "configlayer.drift_blocked"))
	case errors.Is(err, configlayer.ErrUnknownKey):
		JSONErrorCode(w, http.StatusNotFound, "unknown_key", a.t(r, "configlayer.unknown_key"))
	case errors.Is(err, configlayer.ErrFileReleased):
		JSONErrorCode(w, http.StatusConflict, "file_released", a.t(r, "configlayer.file_released"))
	case errors.Is(err, configlayer.ErrUnknownNotice):
		JSONErrorCode(w, http.StatusNotFound, "unknown_notice", a.t(r, "configlayer.unknown_notice"))
	case errors.Is(err, configlayer.ErrDevModeRequired):
		JSONErrorCode(w, http.StatusForbidden, "dev_mode_required", a.t(r, "configlayer.dev_mode_required"))
	default:
		// Деталь — текст ошибки без управляющих ANSI-последовательностей; наружу
		// идёт только он, внутренние структуры не раскрываются.
		JSONErrorCodeDetail(w, http.StatusInternalServerError, "internal", a.t(r, "error.internal"), utils.StripANSI(err.Error()))
	}
}

// requireMethod пишет 405 и возвращает false, если метод запроса не совпал.
func (a *API) requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// decodeLayerBody читает JSON-тело запроса слоя с пределом 1 МБ. Ошибка разбора
// или превышение предела — 400 invalid_request.
func (a *API) decodeLayerBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxLayerBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		JSONErrorCode(w, http.StatusBadRequest, "invalid_request", a.t(r, "configlayer.invalid_request"))
		return false
	}
	return true
}

// ConfigLayerState — GET /api/configlayer/state: снимок состояния слоя.
func (a *API) ConfigLayerState(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireLayer(w, r) {
		return
	}
	JSONSuccess(w, a.configLayer.Snapshot())
}

// writeLayerEvent пишет одно событие SSE: имя и JSON данных (json.Marshal, не Sprintf).
func writeLayerEvent(w http.ResponseWriter, flusher http.Flusher, name string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// ConfigLayerEvents — GET /api/configlayer/events: поток событий слоя (SSE).
// Первое событие — snapshot; дальше draft, files, apply_step, apply_done,
// notices; раз в 20 с комментарий ping. Выход по закрытию соединения, отписка в
// defer (D-06).
func (a *API) ConfigLayerEvents(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireLayer(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		a.errorResponse(w, a.t(r, "error.internal"), http.StatusInternalServerError)
		return
	}
	events, unsubscribe, err := a.configLayer.Subscribe()
	if err != nil {
		if errors.Is(err, configlayer.ErrTooManySubscribers) {
			JSONErrorCode(w, http.StatusServiceUnavailable, "too_many_subscribers", a.t(r, "configlayer.too_many_subscribers"))
			return
		}
		a.writeLayerError(w, r, err)
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Подписка заведена до снимка: событие, пришедшее между ними, не теряется.
	if err := writeLayerEvent(w, flusher, configlayer.EventSnapshot, a.configLayer.Snapshot()); err != nil {
		return
	}

	ticker := time.NewTicker(layerSSEPing)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-events:
			if !open {
				return
			}
			if err := writeLayerEvent(w, flusher, ev.Type, ev.Data); err != nil {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ConfigLayerDraft — POST /api/configlayer/draft {revision, section, value}.
func (a *API) ConfigLayerDraft(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		Revision int64           `json:"revision"`
		Section  string          `json:"section"`
		Value    json.RawMessage `json:"value"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.configLayer.EditDraft(req.Revision, req.Section, req.Value)
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigLayerDraftReset — POST /api/configlayer/draft/reset {revision}.
func (a *API) ConfigLayerDraftReset(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.configLayer.ResetDraft(req.Revision)
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigLayerApply — POST /api/configlayer/apply: запускает применение черновика
// и отвечает сразу {started:true}; итог приходит событием apply_done.
func (a *API) ConfigLayerApply(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	// Тело не используется, но размер ограничен как у остальных запросов.
	r.Body = http.MaxBytesReader(w, r.Body, maxLayerBodyBytes)
	if err := a.configLayer.StartApply(true); err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]bool{"started": true})
}

// ConfigLayerFilesRebuild — POST /api/configlayer/files/rebuild {keys} или {all}.
// Файлы адресуются только ключами манифеста.
func (a *API) ConfigLayerFilesRebuild(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		Keys []string `json:"keys"`
		All  bool     `json:"all"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := a.configLayer.Rebuild(req.Keys, req.All); err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]bool{"started": true})
}

// ConfigLayerFilesRelease — POST /api/configlayer/files/release {key}: «Принять правку».
func (a *API) ConfigLayerFilesRelease(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.configLayer.Release(req.Key)
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// ConfigLayerFilesDiff — GET /api/configlayer/files/diff?key=: ожидаемое и
// фактическое содержимое файла. Принимается ключ манифеста, а не путь (T-144-33).
func (a *API) ConfigLayerFilesDiff(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireLayer(w, r) {
		return
	}
	view, err := a.configLayer.Diff(r.URL.Query().Get("key"))
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, view)
}

// ConfigLayerNoticesDismiss — POST /api/configlayer/notices/dismiss {id}.
func (a *API) ConfigLayerNoticesDismiss(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	notices, err := a.configLayer.DismissNotice(req.ID)
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]any{"notices": notices})
}

// ConfigLayerDiag — POST /api/configlayer/diag {revision, action}: диагностика
// в черновик, только в режиме разработчика (D-19).
func (a *API) ConfigLayerDiag(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) || !a.requireLayer(w, r) {
		return
	}
	var req struct {
		Revision int64  `json:"revision"`
		Action   string `json:"action"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	ev, err := a.configLayer.Diag(req.Revision, req.Action)
	if err != nil {
		a.writeLayerError(w, r, err)
		return
	}
	JSONSuccess(w, ev)
}

// Переключатель флага — в задаче 3 плана.
func (a *API) SettingsConfigLayer(w http.ResponseWriter, r *http.Request) {
	JSONError(w, http.StatusNotImplemented, "not implemented")
}
