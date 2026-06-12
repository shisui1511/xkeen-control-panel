package handlers

import (
	"net/http"
)

// TemplateUpdate вызывает FetchOnlineUpdates для загрузки актуального каталога
// шаблонов из онлайн-репозитория и возвращает количество обновлённых шаблонов.
// Требует метода POST (CSRF проверяется middleware HandleProtected).
func (a *API) TemplateUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}
	count, err := a.templateSvc.FetchOnlineUpdates()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadGateway)
		return
	}
	a.jsonResponse(w, map[string]int{"updated": count})
}

func (a *API) TemplateList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.templateSvc.List())
}

func (a *API) TemplateFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		a.errorResponse(w, "Name parameter is required", http.StatusBadRequest)
		return
	}

	content, err := a.templateSvc.FetchByName(name)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, map[string]string{"content": content})
}

// TemplateStatus возвращает статус и версию конфигурационных шаблонов.
func (a *API) TemplateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.templateSvc.GetStatus())
}

// TemplateCheck проверяет наличие обновлений шаблонов на удаленном сервере.
func (a *API) TemplateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}
	hasUpdate, err := a.templateSvc.CheckForUpdates()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadGateway)
		return
	}
	a.jsonResponse(w, map[string]interface{}{"has_update": hasUpdate})
}
