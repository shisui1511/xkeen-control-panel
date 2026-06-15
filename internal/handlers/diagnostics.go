package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxLogSize = 512 * 1024 // 512 KB

var sensitiveYAMLKeys = map[string]bool{
	"password": true,
	"secret":   true,
	"uuid":     true,
	"public-key": true,
}

var sensitiveJSONKeys = map[string]bool{
	"id":            true,
	"secret":        true,
	"password":      true,
	"publicKey":     true,
	"password_hash": true,
	"passwordHash":  true,
}

// sanitizeYAML parses YAML data, recursively replaces sensitive values with *REDACTED*,
// and marshals it back. If parsing fails, it returns the original data without error.
func sanitizeYAML(data []byte) ([]byte, error) {
	var root interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return data, nil
	}
	sanitizeNode(root)
	return yaml.Marshal(root)
}

func sanitizeNode(v interface{}) {
	switch node := v.(type) {
	case map[string]interface{}:
		for k, val := range node {
			if sensitiveYAMLKeys[strings.ToLower(k)] {
				node[k] = "*REDACTED*"
			} else {
				sanitizeNode(val)
			}
		}
	case map[interface{}]interface{}:
		for k, val := range node {
			if strK, ok := k.(string); ok {
				if sensitiveYAMLKeys[strings.ToLower(strK)] {
					node[k] = "*REDACTED*"
				} else {
					sanitizeNode(val)
				}
			} else {
				sanitizeNode(val)
			}
		}
	case []interface{}:
		for _, item := range node {
			sanitizeNode(item)
		}
	}
}

// sanitizeJSON parses JSON data, recursively replaces sensitive values with *REDACTED*,
// and marshals it back. If parsing fails, it returns the original data without error.
func sanitizeJSON(data []byte) ([]byte, error) {
	var root interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return data, nil
	}
	sanitizeJSONNode(root)
	return json.MarshalIndent(root, "", "  ")
}

func sanitizeJSONNode(v interface{}) {
	switch node := v.(type) {
	case map[string]interface{}:
		for k, val := range node {
			if sensitiveJSONKeys[k] {
				node[k] = "*REDACTED*"
			} else {
				sanitizeJSONNode(val)
			}
		}
	case []interface{}:
		for _, item := range node {
			sanitizeJSONNode(item)
		}
	}
}

// shouldExcludeFromDiagnostics returns true if the path contains a "subscriptions" segment
// or ends with a case-insensitive ".txt" suffix.
func shouldExcludeFromDiagnostics(path string) bool {
	if strings.HasSuffix(strings.ToLower(path), ".txt") {
		return true
	}
	clean := filepath.Clean(path)
	clean = strings.ReplaceAll(clean, "\\", "/")
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "subscriptions" {
			return true
		}
	}
	return false
}

// addFileToTar builds a tar header and writes the file content into the tar writer.
func addFileToTar(tw *tar.Writer, name string, content []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0644,
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(content)
	return err
}

// DiagnosticsDownload streams a .tar.gz archive containing sanitized configurations, logs, and iptables rules.
func (a *API) DiagnosticsDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := "xcp-diagnostics-" + timestamp + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	gw := gzip.NewWriter(w)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// 1. Collect Logs
	var logPaths []string
	seenLogs := make(map[string]bool)
	addLog := func(p string) {
		if p == "" {
			return
		}
		clean, err := a.pathVal.Validate(p)
		if err != nil {
			return
		}
		if !seenLogs[clean] {
			seenLogs[clean] = true
			logPaths = append(logPaths, clean)
		}
	}

	for _, src := range a.cfg.LogSources {
		addLog(src)
	}
	addLog(a.cfg.LogPath)
	for _, p := range []string{
		"/opt/var/log/xray/access.log",
		"/opt/var/log/xray/error.log",
		"/opt/var/log/xkeen-detached.log",
		"/opt/var/log/mihomo.log",
	} {
		addLog(p)
	}

	for _, path := range logPaths {
		if _, err := os.Stat(path); err == nil {
			f, err := os.Open(path)
			if err == nil {
				content, readErr := io.ReadAll(io.LimitReader(f, maxLogSize))
				f.Close()
				if readErr == nil {
					_ = addFileToTar(tw, "logs/"+filepath.Base(path), content)
				}
			}
		}
	}

	// 2. iptables-save
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "iptables-save").Output()
	if err != nil {
		out = []byte("iptables-save unavailable: " + err.Error())
	}
	_ = addFileToTar(tw, "iptables-rules.txt", out)

	// 3. Config Files
	walkConfigDir := func(dir string, prefix string) {
		if dir == "" {
			return
		}
		cleanDir, err := a.pathVal.Validate(dir)
		if err != nil {
			return
		}

		subDir := filepath.Join(a.cfg.DataDir, "subscriptions")
		cleanSubDir, _ := a.pathVal.Validate(subDir)

		_ = filepath.WalkDir(cleanDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}

			cleanPath, valErr := a.pathVal.Validate(path)
			if valErr != nil {
				return nil
			}

			if cleanSubDir != "" && (cleanPath == cleanSubDir || strings.HasPrefix(cleanPath, cleanSubDir+string(filepath.Separator))) {
				return nil
			}

			if shouldExcludeFromDiagnostics(cleanPath) {
				return nil
			}

			data, readErr := os.ReadFile(cleanPath)
			if readErr != nil {
				return nil
			}

			basename := filepath.Base(cleanPath)
			var sanitized []byte
			var sanitizeErr error
			if strings.Contains(basename, ".json") {
				sanitized, sanitizeErr = sanitizeJSON(data)
			} else if strings.Contains(basename, ".yaml") || strings.Contains(basename, ".yml") {
				sanitized, sanitizeErr = sanitizeYAML(data)
			} else {
				sanitized = data
			}

			if sanitizeErr != nil {
				sanitized = data
			}

			rel, relErr := filepath.Rel(cleanDir, cleanPath)
			if relErr != nil {
				rel = filepath.Base(cleanPath)
			}
			_ = addFileToTar(tw, "configs/"+prefix+"/"+rel, sanitized)
			return nil
		})
	}

	walkConfigDir(a.cfg.MihomoConfigDir, "mihomo")
	walkConfigDir(a.cfg.XRayConfigDir, "xray")

	// 4. XCP Config
	if a.cfg.ConfigPath != "" {
		if cleanXcpPath, err := a.pathVal.Validate(a.cfg.ConfigPath); err == nil {
			if _, statErr := os.Stat(cleanXcpPath); statErr == nil {
				if xcpData, readErr := os.ReadFile(cleanXcpPath); readErr == nil {
					sanitized, sanitizeErr := sanitizeJSON(xcpData)
					if sanitizeErr == nil {
						_ = addFileToTar(tw, "configs/xcp-config.json", sanitized)
					} else {
						_ = addFileToTar(tw, "configs/xcp-config.json", xcpData)
					}
				}
			}
		}
	}
}
