package handlers

import (
	"bytes"
	"context"
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

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"0.15.0", "0.15.1", -1},
		{"0.16.0", "0.15.9", 1},
		{"0.15.0", "0.15.0", 0},
		{"0.15.0-beta.1", "0.15.0", -1},
		{"0.15.0", "0.15.0-beta.1", 1},
		{"0.15.0-beta.1", "0.15.0-beta.2", -1},
		{"0.15.0-beta.2", "0.15.0-beta.1", 1},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.9.9", 1},
	}

	for _, tc := range tests {
		got := compareSemver(tc.a, tc.b)
		if got != tc.expected {
			t.Errorf("compareSemver(%q, %q) = %d, expected %d", tc.a, tc.b, got, tc.expected)
		}
	}
}

func TestPickLatestRelease(t *testing.T) {
	// Порядок как у GitHub API: по дате публикации, пересобранный dev-релиз сверху
	republishedDev := []githubRelease{
		{TagName: "v0.25.0-dev", Prerelease: true, Body: "dev"},
		{TagName: "v0.25.1", Body: "stable 0.25.1"},
		{TagName: "v0.25.0", Body: "stable 0.25.0"},
	}
	newerDev := []githubRelease{
		{TagName: "v0.25.1"},
		{TagName: "v0.25.2-dev", Prerelease: true},
		{TagName: "v0.25.0"},
	}
	stableOnly := []githubRelease{
		{TagName: "v0.25.0"},
		{TagName: "v0.25.1"},
	}

	tests := []struct {
		name     string
		releases []githubRelease
		channel  string
		want     string
		wantErr  bool
	}{
		{"stable пропускает pre-release", republishedDev, "stable", "0.25.1", false},
		{"beta не берёт устаревший pre-release", republishedDev, "beta", "0.25.1", false},
		{"beta берёт более новый pre-release", newerDev, "beta", "0.25.2-dev", false},
		{"stable не зависит от порядка", stableOnly, "stable", "0.25.1", false},
		{"beta без pre-release отдаёт stable", stableOnly, "beta", "0.25.1", false},
		{"пустой список", nil, "stable", "", true},
		{"stable без stable-релизов", []githubRelease{{TagName: "v0.1.0-dev", Prerelease: true}}, "stable", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickLatestRelease(tc.releases, tc.channel)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ожидалась ошибка, получено %q", got.LatestVersion)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got.LatestVersion != tc.want {
				t.Errorf("LatestVersion = %q, ожидалось %q", got.LatestVersion, tc.want)
			}
		})
	}

	got, _ := pickLatestRelease(republishedDev, "stable")
	if got.Changelog != "stable 0.25.1" {
		t.Errorf("Changelog = %q, ожидался changelog выбранного релиза", got.Changelog)
	}
}

func TestUpdateAvailable(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"0.25.1", "0.25.0", true},
		{"0.25.1", "0.25.1", false},
		{"0.25.0", "0.25.1", false},
		{"", "0.25.0", false},
		// Rolling dev-сборка пересобирается под тем же тегом
		{"0.25.2-dev", "0.25.2-dev", true},
		{"0.25.2", "0.25.2-dev", true},
		{"0.26.0-dev", "0.25.2-dev", true},
		// Даунгрейд с dev-сборки не предлагается
		{"0.25.1", "0.25.2-dev", false},
		{"0.25.0-dev", "0.25.2-dev", false},
		{"", "0.25.2-dev", false},
		// Stable-сборка обновляется только на более новую версию
		{"0.25.2-dev", "0.25.1", true},
		{"0.25.1-dev", "0.25.1", false},
	}

	for _, tc := range tests {
		if got := updateAvailable(tc.latest, tc.current); got != tc.want {
			t.Errorf("updateAvailable(%q, %q) = %v, ожидалось %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestUpdateChannel_Handlers(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	cfg := &config.Config{
		ConfigPath:    cfgPath,
		UpdateChannel: "stable",
	}
	_ = config.Save(cfgPath, cfg)
	api := &API{cfg: cfg}

	// 1. UpdateChannelGet
	reqPostGet := httptest.NewRequest(http.MethodPost, "/api/update/channel", nil)
	recPostGet := httptest.NewRecorder()
	api.UpdateChannelGet(recPostGet, reqPostGet)
	if recPostGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST to UpdateChannelGet, got %d", recPostGet.Code)
	}

	reqGet := httptest.NewRequest(http.MethodGet, "/api/update/channel", nil)
	recGet := httptest.NewRecorder()
	api.UpdateChannelGet(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for UpdateChannelGet, got %d", recGet.Code)
	}

	// 2. UpdateChannelSet
	reqGetSet := httptest.NewRequest(http.MethodGet, "/api/update/channel", nil)
	recGetSet := httptest.NewRecorder()
	api.UpdateChannelSet(recGetSet, reqGetSet)
	if recGetSet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET to UpdateChannelSet, got %d", recGetSet.Code)
	}

	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/update/channel", bytes.NewBufferString("{invalid"))
	recBadJSON := httptest.NewRecorder()
	api.UpdateChannelSet(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	reqBadChannel := httptest.NewRequest(http.MethodPost, "/api/update/channel", bytes.NewBufferString(`{"channel":"alpha"}`))
	recBadChannel := httptest.NewRecorder()
	api.UpdateChannelSet(recBadChannel, reqBadChannel)
	if recBadChannel.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid channel alpha, got %d", recBadChannel.Code)
	}

	reqValidBeta := httptest.NewRequest(http.MethodPost, "/api/update/channel", bytes.NewBufferString(`{"channel":"beta"}`))
	recValidBeta := httptest.NewRecorder()
	api.UpdateChannelSet(recValidBeta, reqValidBeta)
	if recValidBeta.Code != http.StatusOK {
		t.Errorf("expected 200 for setting beta, got %d", recValidBeta.Code)
	}
	if api.cfg.UpdateChannel != "beta" {
		t.Errorf("expected channel beta, got %s", api.cfg.UpdateChannel)
	}

	// 3. UpdateChannelHandler
	reqRouterDelete := httptest.NewRequest(http.MethodDelete, "/api/update/channel", nil)
	recRouterDelete := httptest.NewRecorder()
	api.UpdateChannelHandler(recRouterDelete, reqRouterDelete)
	if recRouterDelete.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for DELETE router, got %d", recRouterDelete.Code)
	}

	reqRouterGet := httptest.NewRequest(http.MethodGet, "/api/update/channel", nil)
	recRouterGet := httptest.NewRecorder()
	api.UpdateChannelHandler(recRouterGet, reqRouterGet)
	if recRouterGet.Code != http.StatusOK {
		t.Errorf("expected 200 for router GET, got %d", recRouterGet.Code)
	}
}

func TestUpdateStatusEndpoint(t *testing.T) {
	api := &API{}

	reqPost := httptest.NewRequest(http.MethodPost, "/api/update/status", nil)
	recPost := httptest.NewRecorder()
	api.UpdateStatusEndpoint(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	reqGet := httptest.NewRequest(http.MethodGet, "/api/update/status", nil)
	recGet := httptest.NewRecorder()
	api.UpdateStatusEndpoint(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}
}

func TestCopyFile_And_PruneBackupsDir(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.bin")
	dst := filepath.Join(tmpDir, "dst.bin")

	content := []byte("hello binary world")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	copied, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(copied) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(copied), string(content))
	}

	// copyFile error on nonexistent src
	if err := copyFile(filepath.Join(tmpDir, "nonexistent"), dst); err == nil {
		t.Error("expected error copying nonexistent file, got nil")
	}

	// pruneBackupsDir
	backupsDir := filepath.Join(tmpDir, "backups")
	_ = os.MkdirAll(backupsDir, 0755)
	for i := 1; i <= 5; i++ {
		p := filepath.Join(backupsDir, fmt.Sprintf("xcp-backup-%02d", i))
		_ = os.WriteFile(p, []byte("data"), 0644)
	}

	if err := pruneBackupsDir(backupsDir, 2); err != nil {
		t.Fatalf("pruneBackupsDir failed: %v", err)
	}

	entries, _ := os.ReadDir(backupsDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 backups remaining, got %d", len(entries))
	}
}

func TestUpdateEndpoints_Validation(t *testing.T) {
	api := &API{}

	// 1. UpdateCheck: Method Not Allowed (POST)
	reqCheckPost := httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	recCheckPost := httptest.NewRecorder()
	api.UpdateCheck(recCheckPost, reqCheckPost)
	if recCheckPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST check, got %d", recCheckPost.Code)
	}

	// 2. UpdateChangelog: Method Not Allowed (POST)
	reqChangePost := httptest.NewRequest(http.MethodPost, "/api/update/changelog", nil)
	recChangePost := httptest.NewRecorder()
	api.UpdateChangelog(recChangePost, reqChangePost)
	if recChangePost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST changelog, got %d", recChangePost.Code)
	}

	// 3. UpdateChangelog: Missing version query param
	reqChangeEmpty := httptest.NewRequest(http.MethodGet, "/api/update/changelog", nil)
	recChangeEmpty := httptest.NewRecorder()
	api.UpdateChangelog(recChangeEmpty, reqChangeEmpty)
	if recChangeEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty version changelog, got %d", recChangeEmpty.Code)
	}

	// 4. UpdateInstall: Method Not Allowed (GET)
	reqInstallGet := httptest.NewRequest(http.MethodGet, "/api/update/install", nil)
	recInstallGet := httptest.NewRecorder()
	api.UpdateInstall(recInstallGet, reqInstallGet)
	if recInstallGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET install, got %d", recInstallGet.Code)
	}
}

func TestUpdateEventsSSE(t *testing.T) {
	api := &API{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/update/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	api.UpdateEventsSSE(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for UpdateEventsSSE, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", rec.Header().Get("Content-Type"))
	}
}
