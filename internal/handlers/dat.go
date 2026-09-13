package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (a *API) DATList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.datSvc.List())
}

func (a *API) DATListTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		JSONError(w, http.StatusBadRequest, "name parameter is required")
		return
	}

	tags, err := a.datSvc.ListTags(name)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	JSONSuccess(w, tags)
}

func (a *API) DATUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		File string `json:"file"`
		Type string `json:"type"`
	}

	if r.Header.Get("Content-Type") == "application/json" && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	var err error
	if req.File != "" {
		err = a.datSvc.UpdateFile(req.File, req.Type)
	} else {
		err = a.datSvc.Update()
	}

	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"success": true,
	})
}

func (a *API) DATRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	err := a.datSvc.Rollback()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"success": true,
	})
}

func (a *API) DATSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	name := r.URL.Query().Get("name")
	tag := r.URL.Query().Get("tag")
	query := r.URL.Query().Get("query")
	pageStr := r.URL.Query().Get("page")

	if name == "" {
		JSONError(w, http.StatusBadRequest, "name parameter is required")
		return
	}
	if tag == "" {
		JSONError(w, http.StatusBadRequest, "tag parameter is required")
		return
	}

	page := 0
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			page = p
		}
	}

	res, err := a.datSvc.SearchTag(name, tag, query, page, 50)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	JSONSuccess(w, res)
}

func (a *API) DATLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("query")
	if strings.TrimSpace(query) == "" {
		JSONError(w, http.StatusBadRequest, "query parameter is required")
		return
	}

	filterType := r.URL.Query().Get("type")
	filesParam := r.URL.Query().Get("files")
	var files []string
	if filesParam != "" {
		for _, f := range strings.Split(filesParam, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				files = append(files, f)
			}
		}
	}

	results, err := a.datSvc.Lookup(query, filterType, files)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, results)
}
