package utils

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRotateWriter_BasicWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rotate-writer-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "test.log")
	w, err := NewRotateWriter(filePath, 100)
	if err != nil {
		t.Fatalf("failed to create NewRotateWriter: %v", err)
	}
	defer w.Close()

	data := []byte("hello world")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected n to be %d, got %d", len(data), n)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Errorf("expected content %q, got %q", data, content)
	}
}

func TestRotateWriter_Rotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rotate-writer-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "test.log")
	// maxSize = 10 bytes
	w, err := NewRotateWriter(filePath, 10)
	if err != nil {
		t.Fatalf("failed to create NewRotateWriter: %v", err)
	}
	defer w.Close()

	// 1. Write 6 bytes (no rotation)
	_, err = w.Write([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}

	// 2. Write 5 bytes (exceeds 10 bytes limit: 6+5=11 > 10). Should rotate.
	// Since lastRotation is zero (never rotated before), rotation flooding is not active.
	_, err = w.Write([]byte("abcde"))
	if err != nil {
		t.Fatal(err)
	}

	// Verify test.log contains "abcde"
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abcde" {
		t.Errorf("expected test.log to be %q, got %q", "abcde", string(content))
	}

	// Verify test.log.1 contains "123456"
	content1, err := os.ReadFile(filePath + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(content1) != "123456" {
		t.Errorf("expected test.log.1 to be %q, got %q", "123456", string(content1))
	}

	// 3. Bypass flood protection manually
	w.lastRotation = time.Now().Add(-11 * time.Second)

	// Write 6 bytes to "abcde" (size is now 5. 5+6=11 > 10). Should rotate.
	_, err = w.Write([]byte("vwxyz!"))
	if err != nil {
		t.Fatal(err)
	}

	// Verify files:
	// test.log -> vwxyz!
	// test.log.1 -> abcde
	// test.log.2 -> 123456
	content, _ = os.ReadFile(filePath)
	if string(content) != "vwxyz!" {
		t.Errorf("expected test.log to be %q, got %q", "vwxyz!", string(content))
	}

	content1, _ = os.ReadFile(filePath + ".1")
	if string(content1) != "abcde" {
		t.Errorf("expected test.log.1 to be %q, got %q", "abcde", string(content1))
	}

	content2, _ := os.ReadFile(filePath + ".2")
	if string(content2) != "123456" {
		t.Errorf("expected test.log.2 to be %q, got %q", "123456", string(content2))
	}
}

func TestRotateWriter_FloodProtection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rotate-writer-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "test.log")
	w, err := NewRotateWriter(filePath, 10)
	if err != nil {
		t.Fatalf("failed to create NewRotateWriter: %v", err)
	}
	defer w.Close()

	// Write 6 bytes
	_, _ = w.Write([]byte("123456"))

	// Trigger rotation (6+5=11 > 10). This sets w.lastRotation = time.Now()
	_, _ = w.Write([]byte("abcde"))

	// Capture stderr
	oldStderr := os.Stderr
	r, writePipe, _ := os.Pipe()
	os.Stderr = writePipe

	// Trigger another rotation immediately (5+6=11 > 10) -> interval < 10s. Should trigger flood protection.
	_, err = w.Write([]byte("vwxyz!"))
	if err != nil {
		t.Fatal(err)
	}

	writePipe.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	stderrOutput := buf.String()

	// Verify vwxyz! went to stderr, not file
	if !bytes.Contains([]byte(stderrOutput), []byte("vwxyz!")) {
		t.Errorf("expected stderr to contain 'vwxyz!', got: %q", stderrOutput)
	}

	// Verify test.log still contains "abcde" (did not get updated to "vwxyz!" or rotated)
	content, _ := os.ReadFile(filePath)
	if string(content) != "abcde" {
		t.Errorf("expected test.log to remain 'abcde', got: %q", string(content))
	}
}

func TestRotateWriter_Concurrency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rotate-writer-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "test.log")
	w, err := NewRotateWriter(filePath, 100000)
	if err != nil {
		t.Fatalf("failed to create NewRotateWriter: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	workers := 10
	iterations := 100
	data := []byte("line\n")

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = w.Write(data)
			}
		}()
	}

	wg.Wait()

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	expectedLen := workers * iterations * len(data)
	if len(content) != expectedLen {
		t.Errorf("expected file length %d, got %d", expectedLen, len(content))
	}
}
