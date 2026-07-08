package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RotateWriter implements io.WriteCloser with size-based log rotation,
// keeping up to 2 backups (file.1, file.2) and protecting against flooding.
type RotateWriter struct {
	mu           sync.Mutex
	filePath     string
	maxSize      int64
	file         *os.File
	currentSize  int64
	lastRotation time.Time
}

// NewRotateWriter creates a new RotateWriter. If the parent directory doesn't exist, it creates it.
func NewRotateWriter(filePath string, maxSize int64) (*RotateWriter, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	w := &RotateWriter{
		filePath: filePath,
		maxSize:  maxSize,
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	w.file = f

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w.currentSize = info.Size()

	return w, nil
}

// Write writes data to the log file, performing rotation if size limit is reached.
func (w *RotateWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	writeSize := int64(len(p))

	if w.currentSize > 0 && w.currentSize+writeSize > w.maxSize {
		now := time.Now()
		if !w.lastRotation.IsZero() && now.Sub(w.lastRotation) < 10*time.Second {
			// Rotation Flooding protection: redirect to os.Stderr
			_, _ = fmt.Fprintln(os.Stderr, "RotateWriter: Rotation flooding detected (interval < 10s), redirecting write to Stderr")
			return os.Stderr.Write(p)
		}

		if err := w.rotate(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "RotateWriter: rotation failed: %v\n", err)
		} else {
			w.lastRotation = now
		}
	}

	if w.file == nil {
		// Fallback if file was closed or not opened
		return os.Stderr.Write(p)
	}

	n, err = w.file.Write(p)
	w.currentSize += int64(n)
	return n, err
}

// rotate shifts files: file.1 -> file.2, file -> file.1, and opens a new truncated file.
func (w *RotateWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	log1 := w.filePath + ".1"
	log2 := w.filePath + ".2"

	// Shift log.1 to log.2
	if _, err := os.Stat(log1); err == nil {
		_ = os.Remove(log2)
		if err := os.Rename(log1, log2); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "RotateWriter: failed to rename %s to %s: %v\n", log1, log2, err)
		}
	}

	// Shift current log to log.1
	if _, err := os.Stat(w.filePath); err == nil {
		if err := os.Rename(w.filePath, log1); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "RotateWriter: failed to rename %s to %s: %v\n", w.filePath, log1, err)
		}
	}

	// Open new truncated log file
	f, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.currentSize = 0
	return nil
}

// Close closes the current log file.
func (w *RotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
