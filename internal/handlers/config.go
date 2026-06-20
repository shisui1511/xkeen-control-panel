package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 1 * 1024 * 1024 // 1 MB

var configPathRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.\/]+$`)

type ConfigFileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (a *API) ConfigList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
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

	res := make([]ConfigFileInfo, 0, len(files))
	for _, f := range files {
		cleanF, err := a.pathVal.Validate(f)
		if err != nil {
			continue
		}
		// Explicit inline validation for CodeQL path sanitization
		var pathAllowed bool
		for _, root := range a.cfg.AllowedRoots {
			cleanRoot := filepath.Clean(root)
			if cleanF == cleanRoot || strings.HasPrefix(cleanF, cleanRoot+string(filepath.Separator)) {
				pathAllowed = true
				break
			}
		}
		if !pathAllowed {
			continue
		}
		// Strict validation against path traversal and characters to satisfy static analyzers (CWE-22)
		if strings.Contains(cleanF, "..") {
			continue
		}
		if !configPathRegex.MatchString(cleanF) {
			continue
		}
		// codeql[go/path-injection] - cleanF validated via PathValidator.Validate + strings.HasPrefix check above.
		info, statErr := os.Stat(cleanF)
		var size int64
		if statErr == nil {
			size = info.Size()
		}
		// Use original glob path (f) for Name/Path so symlinks are shown by their
		// link name (e.g. config.yaml), not the resolved target (e.g. profiles/default.yaml).
		// cleanF is used only for the security check and os.Stat above.
		res = append(res, ConfigFileInfo{
			Name: filepath.Base(f),
			Path: f,
			Size: size,
		})
	}

	a.jsonResponse(w, res)
}

func (a *API) ConfigRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Query().Get("path")

	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	data, err := a.configSvc.Read(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			a.errorResponse(w, a.t(r, "config.file_not_found"), http.StatusNotFound)
			return
		}
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

	mihomoConfigPath := filepath.Clean(filepath.Join(a.cfg.MihomoConfigDir, "config.yaml"))
	mihomoConfigPathYml := filepath.Clean(filepath.Join(a.cfg.MihomoConfigDir, "config.yml"))
	isMihomoConfig := (cleanPath == mihomoConfigPath || cleanPath == mihomoConfigPathYml)

	if isMihomoConfig && a.subscriptionSvc != nil {
		a.subscriptionSvc.LockMihomo()
		defer a.subscriptionSvc.UnlockMihomo()
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

	var backupData []byte
	var backupExists bool
	if _, statErr := os.Stat(cleanPath); statErr == nil {
		if d, readErr := os.ReadFile(cleanPath); readErr == nil {
			backupData = d
			backupExists = true
		}
	}

	err = a.configSvc.Save(cleanPath, data)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if errStr := a.validateConfigAndRollback(r, cleanPath, data, backupExists, backupData); errStr != "" {
		a.errorResponse(w, errStr, http.StatusUnprocessableEntity)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) ConfigBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
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
	if backups == nil {
		backups = []string{}
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

	JSONSuccess(w, nil)
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

	JSONSuccess(w, nil)
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

	JSONSuccess(w, nil)
}

type ConfigValidateRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type ConfigValidateResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error"`
}

// PreflightIssue is the JSON representation of a single preflight issue.
// The canonical type is services.PreflightIssue; this mirrors its JSON shape.
type PreflightIssueJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ConfigPreflightResponse is the JSON body for GET /api/config/preflight.
type ConfigPreflightResponse struct {
	Valid    bool                 `json:"valid"`
	Errors   []PreflightIssueJSON `json:"errors"`
	Warnings []PreflightIssueJSON `json:"warnings"`
}

// ConfigPreflight handles GET /api/config/preflight?kernel=mihomo|xray.
// It runs a pre-flight validation of the kernel config and returns blocking errors
// and non-blocking warnings. On service read/parse failure it returns valid:true
// with empty arrays (silent safe fallback — must never block the user).
func (a *API) ConfigPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	kernel := r.URL.Query().Get("kernel")
	if kernel != "mihomo" && kernel != "xray" {
		a.errorResponse(w, "kernel must be 'mihomo' or 'xray'", http.StatusBadRequest)
		return
	}

	safeResp := ConfigPreflightResponse{
		Valid:    true,
		Errors:   []PreflightIssueJSON{},
		Warnings: []PreflightIssueJSON{},
	}

	switch kernel {
	case "mihomo":
		result, err := a.mihomoSvc.ValidateMihomoConfig()
		if err != nil {
			// T-22-05: swallow fs/parse errors — never leak raw error text to client.
			a.jsonResponse(w, safeResp)
			return
		}
		resp := ConfigPreflightResponse{
			Valid:    result.Valid,
			Errors:   mapIssues(result.Errors),
			Warnings: mapIssues(result.Warnings),
		}
		a.jsonResponse(w, resp)

	case "xray":
		result, err := a.xkeenSvc.ValidateXrayConfig(a.cfg.XRayConfigDir)
		if err != nil {
			a.jsonResponse(w, safeResp)
			return
		}
		resp := ConfigPreflightResponse{
			Valid:    result.Valid,
			Errors:   mapIssues(result.Errors),
			Warnings: mapIssues(result.Warnings),
		}
		a.jsonResponse(w, resp)
	}
}

// mapIssues converts services.PreflightIssue slice to handler JSON types.
// Always returns a non-nil slice so JSON serializes as [] not null.
func mapIssues(issues []services.PreflightIssue) []PreflightIssueJSON {
	result := make([]PreflightIssueJSON, 0, len(issues))
	for _, iss := range issues {
		result = append(result, PreflightIssueJSON{
			Code:    iss.Code,
			Message: iss.Message,
		})
	}
	return result
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

func copyDirRecursive(src, dst string) error {
	err := os.MkdirAll(dst, 0755)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			err = copyDirRecursive(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			err = os.WriteFile(dstPath, data, 0644)
			if err != nil {
				return err
			}
		}
	}
	return nil
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
			if entry.Name() == "rules" || entry.Name() == "providers" {
				if err := copyDirRecursive(filepath.Join(srcDir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
					return err
				}
			}
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".json" && ext != ".yaml" && ext != ".yml" && ext != ".dat" && ext != ".metadb" {
			continue
		}

		srcFile := filepath.Join(srcDir, entry.Name())
		dstFile := filepath.Join(dstDir, entry.Name())

		if entry.Name() == targetFilename {
			if err := os.WriteFile(dstFile, []byte(newContent), 0644); err != nil {
				return err
			}
		} else {
			if ext == ".dat" || ext == ".metadb" {
				if err := os.Symlink(srcFile, dstFile); err != nil {
					// Fallback to copy if symlink fails
					data, err := os.ReadFile(srcFile)
					if err != nil {
						return err
					}
					if err := os.WriteFile(dstFile, data, 0644); err != nil {
						return err
					}
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

type MihomoMergeRequest struct {
	Path     string            `json:"path"`
	Sections map[string]string `json:"sections"`
}

func (a *API) MihomoMergeSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBytes)
	var req MihomoMergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			a.errorResponse(w, "request body too large (max 1 MB)", http.StatusRequestEntityTooLarge)
			return
		}
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

	ext := filepath.Ext(cleanPath)
	if ext != ".yaml" && ext != ".yml" {
		a.errorResponse(w, "only .yaml, .yml files are allowed for merge", http.StatusForbidden)
		return
	}

	mihomoConfigPath := filepath.Clean(filepath.Join(a.cfg.MihomoConfigDir, "config.yaml"))
	mihomoConfigPathYml := filepath.Clean(filepath.Join(a.cfg.MihomoConfigDir, "config.yml"))
	isMihomoConfig := (cleanPath == mihomoConfigPath || cleanPath == mihomoConfigPathYml)

	if isMihomoConfig && a.subscriptionSvc != nil {
		a.subscriptionSvc.LockMihomo()
		defer a.subscriptionSvc.UnlockMihomo()
	}

	data, err := a.configSvc.Read(cleanPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	content := string(data)

	for sectionName, newSecContent := range req.Sections {
		if sectionName != "proxy-groups" && sectionName != "rule-providers" && sectionName != "rules" &&
			sectionName != "proxies" && sectionName != "dns" && sectionName != "tun" &&
			sectionName != "proxy-providers" {
			a.errorResponse(w, "invalid section name: "+sectionName, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(newSecContent) != "" {
			var temp interface{}
			if err := yaml.Unmarshal([]byte(newSecContent), &temp); err != nil {
				a.errorResponse(w, "invalid YAML syntax in section "+sectionName+": "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		content = services.ReplaceMihomoTopLevelSection(content, sectionName, newSecContent)
	}

	var resultTemp interface{}
	if err := yaml.Unmarshal([]byte(content), &resultTemp); err != nil {
		a.errorResponse(w, "invalid resulting YAML config: "+err.Error(), http.StatusBadRequest)
		return
	}

	var backupData []byte
	var backupExists bool
	if _, statErr := os.Stat(cleanPath); statErr == nil {
		if d, readErr := os.ReadFile(cleanPath); readErr == nil {
			backupData = d
			backupExists = true
		}
	}

	err = a.configSvc.Save(cleanPath, []byte(content))
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if errStr := a.validateConfigAndRollback(r, cleanPath, []byte(content), backupExists, backupData); errStr != "" {
		a.errorResponse(w, errStr, http.StatusUnprocessableEntity)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) validateConfigAndRollback(r *http.Request, cleanPath string, data []byte, backupExists bool, backupData []byte) string {
	ext := filepath.Ext(cleanPath)
	var kernelType string
	if strings.Contains(cleanPath, "xray") || ext == ".json" {
		kernelType = "xray"
	} else if strings.Contains(cleanPath, "mihomo") || ext == ".yaml" || ext == ".yml" {
		kernelType = "mihomo"
	} else {
		kernelType = "xray"
	}

	binaryPath := a.getBinaryPath(kernelType)
	if binaryPath == "" {
		return ""
	}

	tempDir, err := os.MkdirTemp("", "xcp-save-val-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tempDir)

	origDir := filepath.Dir(cleanPath)
	filename := filepath.Base(cleanPath)
	if err := copyDirConfigs(origDir, tempDir, filename, string(data), a.cfg.AllowedRoots); err != nil {
		return ""
	}

	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if kernelType == "xray" {
		cmd = exec.CommandContext(ctx, binaryPath, "-test", "-confdir", tempDir)
	} else {
		cmd = exec.CommandContext(ctx, binaryPath, "-t", "-d", tempDir, "-f", filepath.Join(tempDir, filename))
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Rollback!
		if backupExists {
			_ = utils.AtomicWriteFile(cleanPath, backupData, 0644)
		} else {
			_ = os.Remove(cleanPath)
		}
		return strings.TrimSpace(string(out))
	}

	return ""
}
