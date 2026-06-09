package utils

import (
	"errors"
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

	// Resolve symlinks for the target path. If it does not exist, resolve the parent directory.
	var resolved string
	if rp, err := filepath.EvalSymlinks(absPath); err == nil {
		resolved = rp
	} else {
		parentDir := filepath.Dir(absPath)
		resolvedParent, err := filepath.EvalSymlinks(parentDir)
		if err != nil {
			return "", errors.New("path traversal detected or path not allowed")
		}
		resolved = filepath.Join(resolvedParent, filepath.Base(absPath))
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
