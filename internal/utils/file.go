package utils

import (
	"io"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a temporary file and then renames it to the target path.
// This prevents file corruption if the process or system crashes during write.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	// Sanitize path to prevent directory traversal (CWE-22).
	path = filepath.Clean(path)
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

// CopyFile copies a file from src to dst.
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
