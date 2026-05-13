package handlers

import (
	"encoding/json"
	"net/http"
)

func (a *API) TemplateList(w http.ResponseWriter, r *http.Request) {
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.templateSvc.List())
}

func (a *API) TemplateFetch(w http.ResponseWriter, r *http.Request) {
	if a.templateSvc == nil {
		a.errorResponse(w, "Template service unavailable", http.StatusServiceUnavailable)
		return
	}

	url := r.URL.Query().Get("url")
	if url == "" {
		a.errorResponse(w, "URL parameter is required", http.StatusBadRequest)
		return
	}

	content, err := a.templateSvc.Fetch(url)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]string{"content": content})
}
