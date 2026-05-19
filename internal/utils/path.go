package utils

import (
	"errors"
	"path/filepath"
	"strings"
)

type PathValidator struct {
	AllowedRoots []string
}

func NewPathValidator(roots []string) *PathValidator {
	return &PathValidator{AllowedRoots: roots}
}

func (v *PathValidator) Validate(path string) (string, error) {
	cleanPath := filepath.Clean(path)

	// Resolve symlinks on the parent directory.
	// We use the parent so that new (not-yet-existing) files can still be validated.
	parentDir := filepath.Dir(cleanPath)
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", errors.New("path traversal detected or path not allowed")
	}
	// Reconstruct the full path with the resolved parent and the original base name.
	resolved := filepath.Join(resolvedParent, filepath.Base(cleanPath))

	for _, root := range v.AllowedRoots {
		cleanRoot := filepath.Clean(root)
		if resolved == cleanRoot || strings.HasPrefix(resolved, cleanRoot+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", errors.New("path traversal detected or path not allowed")
}
