package handlers

import (
	"net/http"
)

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
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]string{"content": content})
}
