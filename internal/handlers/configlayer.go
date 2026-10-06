package handlers

import "net/http"

// Заготовка RED-коммита: обработчики появляются в следующем коммите.

func (a *API) requireLayer(w http.ResponseWriter, r *http.Request) bool { return false }

func (a *API) writeLayerError(w http.ResponseWriter, r *http.Request, err error) {}

func (a *API) notImplementedLayer(w http.ResponseWriter) {
	JSONError(w, http.StatusNotImplemented, "not implemented")
}

func (a *API) ConfigLayerState(w http.ResponseWriter, r *http.Request)      { a.notImplementedLayer(w) }
func (a *API) ConfigLayerEvents(w http.ResponseWriter, r *http.Request)     { a.notImplementedLayer(w) }
func (a *API) ConfigLayerDraft(w http.ResponseWriter, r *http.Request)      { a.notImplementedLayer(w) }
func (a *API) ConfigLayerDraftReset(w http.ResponseWriter, r *http.Request) { a.notImplementedLayer(w) }
func (a *API) ConfigLayerApply(w http.ResponseWriter, r *http.Request)      { a.notImplementedLayer(w) }
func (a *API) ConfigLayerFilesRebuild(w http.ResponseWriter, r *http.Request) {
	a.notImplementedLayer(w)
}
func (a *API) ConfigLayerFilesRelease(w http.ResponseWriter, r *http.Request) {
	a.notImplementedLayer(w)
}
func (a *API) ConfigLayerFilesDiff(w http.ResponseWriter, r *http.Request) { a.notImplementedLayer(w) }
func (a *API) ConfigLayerNoticesDismiss(w http.ResponseWriter, r *http.Request) {
	a.notImplementedLayer(w)
}
func (a *API) ConfigLayerDiag(w http.ResponseWriter, r *http.Request) { a.notImplementedLayer(w) }

// Заготовка RED-коммита: переключатель флага появится в задаче 3.
func (a *API) SettingsConfigLayer(w http.ResponseWriter, r *http.Request) { a.notImplementedLayer(w) }
