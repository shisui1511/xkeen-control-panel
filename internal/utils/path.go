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
	for _, root := range v.AllowedRoots {
		if strings.HasPrefix(cleanPath, filepath.Clean(root)) {
			return cleanPath, nil
		}
	}
	return "", errors.New("path traversal detected or path not allowed")
}
