package handlers

import (
	"io"
	"net/http"
	"os"
)

const maxConfigBytes = 1 * 1024 * 1024 // 1 MB

func (a *API) ConfigList(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = a.cfg.XRayConfigDir
	}

	cleanDir, err := a.pathVal.Validate(dir)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	files, err := a.configSvc.List(cleanDir)
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
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBytes)
	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		if err.Error() == "http: request body too large" {
			a.errorResponse(w, "request body too large (max 1 MB)", http.StatusRequestEntityTooLarge)
			return
		}
		a.errorResponse(w, a.t(r, "config.write_error"), http.StatusInternalServerError)
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
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
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
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Create(cleanPath); err != nil {
		if os.IsExist(err) {
			a.errorResponse(w, a.t(r, "config.file_exists"), http.StatusConflict)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Delete(cleanPath); err != nil {
		if os.IsNotExist(err) {
			a.errorResponse(w, a.t(r, "config.file_not_found"), http.StatusNotFound)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	oldPath := r.URL.Query().Get("old")
	newPath := r.URL.Query().Get("new")

	cleanOldPath, err := a.pathVal.Validate(oldPath)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	cleanNewPath, err := a.pathVal.Validate(newPath)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	if err := a.configSvc.Rename(cleanOldPath, cleanNewPath); err != nil {
		if os.IsNotExist(err) {
			a.errorResponse(w, a.t(r, "config.file_not_found"), http.StatusNotFound)
			return
		}
		if os.IsExist(err) {
			a.errorResponse(w, a.t(r, "config.file_exists"), http.StatusConflict)
			return
		}
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}
