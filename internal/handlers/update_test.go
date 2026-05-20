package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// plainHTTPClient returns a standard http.Client without SSRF protection,
// suitable only for tests against httptest servers.
func plainHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// TestUpdateState_ConcurrentAccess verifies that concurrent reads and writes
// to the update state do not race (no data races under -race flag).
func TestUpdateState_ConcurrentAccess(t *testing.T) {
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			setUpdateState(UpdateStatus{
				Status:    "checking",
				Message:   fmt.Sprintf("writer-%d", n),
				Timestamp: time.Now().UnixMilli(),
			})
		}(i)

		go func() {
			defer wg.Done()
			_ = getUpdateState()
		}()
	}

	wg.Wait()
}

// TestSHA256Verification_Mismatch verifies that verifyFileChecksum returns an
// error when the SHA-256 of the downloaded file does not match the checksum
// entry in checksums.txt.
func TestSHA256Verification_Mismatch(t *testing.T) {
	// Create a temp binary file with known content.
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xkeen-control-panel")
	content := []byte("fake binary content")
	if err := os.WriteFile(binPath, content, 0600); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	// Compute the CORRECT hash so we can deliberately provide a WRONG one.
	h := sha256.New()
	h.Write(content)
	correctHash := hex.EncodeToString(h.Sum(nil))
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	_ = correctHash // used only for documentation

	binaryName := filepath.Base(binPath)

	// Serve a checksums.txt that contains the wrong hash for our binary.
	checksumBody := fmt.Sprintf("%s  %s\n", wrongHash, binaryName)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, checksumBody)
	}))
	defer ts.Close()

	err := verifyFileChecksumWithClient(binPath, binaryName, ts.URL, plainHTTPClient())
	if err == nil {
		t.Fatal("expected error for SHA-256 mismatch, got nil")
	}
}

// TestSHA256Verification_Match verifies that verifyFileChecksum returns nil
// when the hash matches.
func TestSHA256Verification_Match(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xkeen-control-panel")
	content := []byte("real binary content")
	if err := os.WriteFile(binPath, content, 0600); err != nil {
		t.Fatalf("write temp binary: %v", err)
	}

	h := sha256.New()
	h.Write(content)
	correctHash := hex.EncodeToString(h.Sum(nil))
	binaryName := filepath.Base(binPath)

	checksumBody := fmt.Sprintf("%s  %s\n", correctHash, binaryName)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, checksumBody)
	}))
	defer ts.Close()

	if err := verifyFileChecksumWithClient(binPath, binaryName, ts.URL, plainHTTPClient()); err != nil {
		t.Fatalf("expected nil for correct SHA-256, got: %v", err)
	}
}

// TestSHA256Verification_404 verifies that verifyFileChecksum gracefully skips
// verification when checksums.txt is not found (backward compatibility).
func TestSHA256Verification_404(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xkeen-control-panel")
	if err := os.WriteFile(binPath, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if err := verifyFileChecksumWithClient(binPath, "xkeen-control-panel", ts.URL, plainHTTPClient()); err != nil {
		t.Fatalf("expected nil for 404 checksums (backward compat), got: %v", err)
	}
}

// TestUpdateRollback_MethodNotAllowed verifies that UpdateRollback rejects non-POST requests.
func TestUpdateRollback_MethodNotAllowed(t *testing.T) {
	api := &API{cfg: &config.Config{DataDir: t.TempDir()}}

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/update/rollback", nil)
			rr := httptest.NewRecorder()
			api.UpdateRollback(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: expected 405, got %d", method, rr.Code)
			}
		})
	}
}
