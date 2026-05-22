package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

	// T032: extension whitelist — only .json, .yaml, .yml allowed
	ext := filepath.Ext(cleanPath)
	if ext != ".json" && ext != ".yaml" && ext != ".yml" {
		a.errorResponse(w, "only .json, .yaml, .yml files are allowed", http.StatusForbidden)
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

type ConfigValidateRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ConfigValidateResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error"`
}

func (a *API) getBinaryPath(name string) string {
	if a.kernelSvc != nil {
		if k := a.kernelSvc.Get(name); k != nil && k.BinaryPath != "" {
			if _, err := os.Stat(k.BinaryPath); err == nil {
				return k.BinaryPath
			}
		}
	}

	var fallback string
	if name == "xray" {
		fallback = "/opt/bin/xray"
	} else if name == "mihomo" {
		fallback = a.cfg.MihomoBinary
		if fallback == "" {
			fallback = "/opt/sbin/mihomo"
		}
	}

	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	return ""
}

func copyDirConfigs(srcDir, dstDir string, targetFilename string, newContent string, allowedRoots []string) error {
	// Sanitize and validate targetFilename
	targetFilename = filepath.Base(targetFilename)
	matched, err := regexp.MatchString(`^[a-zA-Z0-9_\-\.]+$`, targetFilename)
	if err != nil || !matched {
		return errors.New("invalid target filename")
	}

	// Sanitize and validate srcDir
	srcDir = filepath.Clean(srcDir)
	if strings.Contains(srcDir, "..") {
		return errors.New("path traversal detected in source directory")
	}

	var srcDirAllowed bool
	for _, root := range allowedRoots {
		cleanRoot := filepath.Clean(root)
		if srcDir == cleanRoot || strings.HasPrefix(srcDir, cleanRoot+string(filepath.Separator)) {
			srcDirAllowed = true
			break
		}
	}
	if !srcDirAllowed {
		return errors.New("source directory is not within allowed roots")
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(filepath.Join(dstDir, targetFilename), []byte(newContent), 0644)
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		srcFile := filepath.Join(srcDir, entry.Name())
		dstFile := filepath.Join(dstDir, entry.Name())

		if entry.Name() == targetFilename {
			if err := os.WriteFile(dstFile, []byte(newContent), 0644); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcFile)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstFile, data, 0644); err != nil {
				return err
			}
		}
	}

	targetPath := filepath.Join(dstDir, targetFilename)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.WriteFile(targetPath, []byte(newContent), 0644); err != nil {
			return err
		}
	}

	return nil
}

func (a *API) ConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req ConfigValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		a.errorResponse(w, "path is required", http.StatusBadRequest)
		return
	}

	cleanPath, err := a.pathVal.Validate(req.Path)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	// Explicit inline validation for CodeQL path sanitization
	var pathAllowed bool
	for _, root := range a.cfg.AllowedRoots {
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
			pathAllowed = true
			break
		}
	}
	if !pathAllowed {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	var kernelType string
	filename := filepath.Base(cleanPath)
	ext := filepath.Ext(cleanPath)

	if strings.Contains(cleanPath, "xray") || ext == ".json" {
		kernelType = "xray"
	} else if strings.Contains(cleanPath, "mihomo") || ext == ".yaml" || ext == ".yml" {
		kernelType = "mihomo"
	} else {
		kernelType = "xray"
	}

	binaryPath := a.getBinaryPath(kernelType)
	if binaryPath == "" {
		a.jsonResponse(w, ConfigValidateResponse{
			Valid: false,
			Error: "validator binary for " + kernelType + " not found on the system",
		})
		return
	}

	tempDir, err := os.MkdirTemp("", "xcp-val-*")
	if err != nil {
		a.errorResponse(w, "failed to create validation temp dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	origDir := filepath.Dir(cleanPath)
	if err := copyDirConfigs(origDir, tempDir, filename, req.Content, a.cfg.AllowedRoots); err != nil {
		a.errorResponse(w, "failed to prepare validation files: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var cmd *exec.Cmd
	if kernelType == "xray" {
		cmd = exec.Command(binaryPath, "-test", "-confdir", tempDir)
	} else {
		cmd = exec.Command(binaryPath, "-t", "-d", tempDir, "-f", filepath.Join(tempDir, filename))
	}

	out, err := cmd.CombinedOutput()
	outputStr := string(out)

	if err != nil {
		a.jsonResponse(w, ConfigValidateResponse{
			Valid: false,
			Error: strings.TrimSpace(outputStr),
		})
		return
	}

	a.jsonResponse(w, ConfigValidateResponse{
		Valid: true,
	})
}
