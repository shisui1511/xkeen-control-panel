package handlers

import (
	"archive/tar"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var sensitiveYAMLKeys = map[string]bool{
	"password": true,
	"secret":   true,
	"uuid":     true,
	"public-key": true,
}

var sensitiveJSONKeys = map[string]bool{
	"id":        true,
	"secret":    true,
	"password":  true,
	"publicKey": true,
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
