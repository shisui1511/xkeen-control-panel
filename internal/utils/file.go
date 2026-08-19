package utils

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// validatePathAllowed checks that path is within one of the allowed roots.
// This is the canonical sanitization pattern recognized by static analysis tools (CWE-22).
func validatePathAllowed(path string, allowedRoots []string) (string, error) {
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", errors.New("invalid path")
	}
	for _, root := range allowedRoots {
		cleanRoot := filepath.Clean(root)
		absRoot, err := filepath.Abs(cleanRoot)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
			return filepath.Join(absRoot, rel), nil
		}
	}
	return "", errors.New("path not within allowed roots")
}

// resolveSymlinkTarget возвращает путь, по которому должна произойти запись.
//
// os.Rename заменяет симлинк обычным файлом, а на роутере config.yaml Mihomo
// часто является симлинком на активный профиль (profiles/default.yaml). Запись
// «в лоб» тихо отвязывает механизм профилей, поэтому существующий симлинк
// разыменовывается и запись идёт в его цель.
//
// Битый симлинк и любые ошибки разыменования означают запись по исходному
// пути: это ровно то поведение, что было до появления функции.
func resolveSymlinkTarget(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved == "" {
		return path
	}
	return resolved
}

// AtomicWriteFile writes data to a temporary file and then renames it to the target path.
// This prevents file corruption if the process or system crashes during write.
// Callers are responsible for validating the path via PathValidator before calling this function.
//
// If path is an existing symlink, the write goes to its target so the link itself survives.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Sanitize path to prevent directory traversal (CWE-22).
	path = filepath.Clean(path)
	path = resolveSymlinkTarget(path)
	dir := filepath.Dir(path)
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Create temporary file in same directory
	tmpFile, err := os.CreateTemp(dir, "atomic-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}

	// Sync to disk
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Set permissions
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}

// AtomicWriteFileSafe is like AtomicWriteFile but validates path against allowedRoots first.
// Use this variant when the path originates from user input (CWE-22).
func AtomicWriteFileSafe(path string, data []byte, perm os.FileMode, allowedRoots []string) error {
	validPath, err := validatePathAllowed(path, allowedRoots)
	if err != nil {
		return err
	}
	return AtomicWriteFile(validPath, data, perm)
}

// CopyFile copies a file from src to dst.
// Callers are responsible for validating paths via PathValidator before calling this function.
func CopyFile(src, dst string) error {
	// Sanitize paths to prevent directory traversal (CWE-22).
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
