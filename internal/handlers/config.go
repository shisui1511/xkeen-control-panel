package handlers

import (
	"io"
	"net/http"
	"os"
)

func (a *API) ConfigList(w http.ResponseWriter, r *http.Request) {
	files, err := a.configSvc.List()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.jsonResponse(w, files)
}

func (a *API) ConfigRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	data, err := a.configSvc.Read(cleanPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (a *API) ConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = a.configSvc.Save(cleanPath, data)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigBackups(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	backups, err := a.configSvc.ListBackups(cleanPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, backups)
}

func (a *API) ConfigCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Create(cleanPath); err != nil {
		if os.IsExist(err) {
			a.errorResponse(w, "File already exists", http.StatusConflict)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Delete(cleanPath); err != nil {
		if os.IsNotExist(err) {
			a.errorResponse(w, "File not found", http.StatusNotFound)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	oldPath := r.URL.Query().Get("old")
	newPath := r.URL.Query().Get("new")

	cleanOldPath, err := a.pathVal.Validate(oldPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	cleanNewPath, err := a.pathVal.Validate(newPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Rename(cleanOldPath, cleanNewPath); err != nil {
		if os.IsNotExist(err) {
			a.errorResponse(w, "File not found", http.StatusNotFound)
			return
		}
		if os.IsExist(err) {
			a.errorResponse(w, "Target file already exists", http.StatusConflict)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}
