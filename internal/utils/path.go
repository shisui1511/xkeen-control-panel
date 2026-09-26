package utils

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PathValidator validates file paths against allowed root directories to prevent path traversal attacks.
type PathValidator struct {
	AllowedRoots []string
}

// NewPathValidator creates a new PathValidator instance with the specified allowed root paths.
func NewPathValidator(roots []string) *PathValidator {
	return &PathValidator{AllowedRoots: roots}
}

// Validate resolves symlinks and cleans the input path, checking if it resides within the allowed roots.
func (v *PathValidator) Validate(path string) (string, error) {
	if path == "" {
		return "", errors.New("path traversal detected or path not allowed")
	}

	// Strict validation against path traversal and characters to satisfy static analyzers (CWE-22)
	if strings.Contains(path, "..") {
		return "", errors.New("path traversal detected or path not allowed")
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_\-\.\/]+$`, path); !matched {
		return "", errors.New("path traversal detected or path not allowed")
	}

	cleanPath := filepath.Clean(path)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}

	// Resolve symlinks for the target path. If it does not exist, resolve the
	// nearest existing ancestor and append the missing tail: missing components
	// cannot be symlinks. So a file in a directory that is yet to be created
	// (/opt/etc/mihomo/config.yaml before the first save) validates too.
	resolved, err := resolveExisting(absPath)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}

	for _, root := range v.AllowedRoots {
		cleanRoot := filepath.Clean(root)
		absRoot, err := filepath.Abs(cleanRoot)
		if err != nil {
			continue
		}

		resolvedRoot := absRoot
		if rr, err := filepath.EvalSymlinks(absRoot); err == nil {
			resolvedRoot = rr
		}

		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
			return filepath.Join(resolvedRoot, rel), nil
		}
	}
	return "", errors.New("path traversal detected or path not allowed")
}

// resolveExisting resolves symlinks of the longest existing prefix of absPath
// and appends the rest unchanged.
func resolveExisting(absPath string) (string, error) {
	tail := ""
	cur := absPath
	for {
		if rp, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(rp, tail), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", os.ErrNotExist
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}
