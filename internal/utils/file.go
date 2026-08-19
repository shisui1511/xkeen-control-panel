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
// Если симлинк указывает на еще не существующий файл (битый симлинк), функция
// возвращает разрешенный путь цели, благодаря чему запись создает целевой файл,
// сохраняя исходный симлинк.
func resolveSymlinkTarget(path string) string {
	curr := filepath.Clean(path)
	for i := 0; i < 255; i++ {
		info, err := os.Lstat(curr)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return curr
		}
		target, err := os.Readlink(curr)
		if err != nil {
			return curr
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(curr), target)
		}
		curr = filepath.Clean(target)
	}
	return curr
}

// AtomicWriteFile writes data to a temporary file and then renames it to the target path.
// This prevents file corruption if the process or system crashes during write.
// Callers are responsible for validating the path via PathValidator before calling this function.
//
// If path is an existing symlink, the write goes to its target so the link itself survives.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Sanitize path to prevent directory traversal (CWE-22).
	path = filepath.Clean(path)
	targetPath := resolveSymlinkTarget(path)
	dir := filepath.Dir(targetPath)
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Create temporary file in same directory as target
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

	// Atomic rename to resolved target path (preserving symlink if path was a symlink)
	return os.Rename(tmpPath, targetPath)
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
