package handlers

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const (
	githubAPIReleases = "https://api.github.com/repos/shisui1511/xkeen-control-panel/releases"
	githubDownloadURL = "https://github.com/shisui1511/xkeen-control-panel/releases/download"
)

type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	Channel        string `json:"channel"`
	DownloadURL    string `json:"download_url,omitempty"`
	Changelog      string `json:"changelog,omitempty"`
}

type UpdateStatus struct {
	Status    string `json:"status"` // idle, checking, downloading, installing, restarting, done, failed
	Message   string `json:"message"`
	Progress  int    `json:"progress"` // 0-100
	Timestamp int64  `json:"timestamp"`
}

var (
	updateState   = UpdateStatus{Status: "idle"}
	updateStateMu sync.RWMutex
)

func getUpdateState() UpdateStatus {
	updateStateMu.RLock()
	defer updateStateMu.RUnlock()
	return updateState
}

func setUpdateState(s UpdateStatus) {
	updateStateMu.Lock()
	defer updateStateMu.Unlock()
	updateState = s
}

func (a *API) UpdateCheck(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	currentVersion := strings.TrimPrefix(a.srv.GetVersion(), "v")

	info, err := fetchLatestRelease(channel)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	info.CurrentVersion = currentVersion
	info.Channel = channel

	// Compare versions (simple string comparison for now)
	info.HasUpdate = info.LatestVersion != "" && info.LatestVersion != currentVersion

	if info.HasUpdate {
		arch := runtime.GOARCH
		if arch == "mipsle" || arch == "mipsel" {
			arch = "mipsle"
		}
		info.DownloadURL = fmt.Sprintf("%s/%s/xkeen-control-panel-linux-%s",
			githubDownloadURL, info.LatestVersion, arch)
	}

	JSONSuccess(w, info)
}

func (a *API) UpdateChangelog(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		a.errorResponse(w, "Version required", http.StatusBadRequest)
		return
	}

	changelog, err := fetchChangelog(version)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(changelog))
}

func (a *API) UpdateInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	st := getUpdateState()
	if st.Status != "idle" && st.Status != "failed" {
		JSONError(w, http.StatusConflict, "Update already in progress")
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	// Run update in background
	go a.performUpdate(channel)

	st.Status = "checking"
	st.Progress = 5
	st.Timestamp = time.Now().Unix()
	setUpdateState(st)

	JSONSuccess(w, getUpdateState())
}

func (a *API) UpdateRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	binPath := filepath.Join(filepath.Dir(a.cfg.DataDir), "bin/xkeen-control-panel")

	// Find latest backup
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) == 0 {
		JSONError(w, http.StatusNotFound, "No backup found")
		return
	}

	latestBackup := filepath.Join(backupDir, backups[len(backups)-1].Name())

	// Stop current binary
	st := UpdateStatus{
		Status:    "restoring",
		Progress:  10,
		Timestamp: time.Now().Unix(),
	}
	setUpdateState(st)

	// Replace with backup
	if err := os.Rename(latestBackup, binPath); err != nil {
		st = getUpdateState()
		st.Status = "failed"
		st.Message = "Rollback failed: " + err.Error()
		setUpdateState(st)
		JSONError(w, http.StatusInternalServerError, st.Message)
		return
	}

	// Restart
	st = getUpdateState()
	st.Status = "restarting"
	st.Progress = 90
	setUpdateState(st)

	go a.restartProcess(binPath, a.cfg.DataDir)

	JSONSuccess(w, getUpdateState())
}

func (a *API) UpdateStatusEndpoint(w http.ResponseWriter, r *http.Request) {
	JSONSuccess(w, getUpdateState())
}

func (a *API) performUpdate(channel string) {
	defer func() {
		if r := recover(); r != nil {
			setUpdateState(UpdateStatus{
				Status:    "failed",
				Message:   fmt.Sprintf("Panic: %v", r),
				Timestamp: time.Now().Unix(),
			})
		}
	}()

	// Step 1: Check latest release
	setUpdateState(UpdateStatus{
		Status:   "checking",
		Progress: 10,
		Message:  "Checking for updates...",
	})

	info, err := fetchLatestRelease(channel)
	if err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Failed to check updates: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	currentVersion := strings.TrimPrefix(a.srv.GetVersion(), "v")
	if info.LatestVersion == currentVersion {
		setUpdateState(UpdateStatus{
			Status:    "done",
			Progress:  100,
			Message:   "Already up to date",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Step 2: Download
	setUpdateState(UpdateStatus{
		Status:   "downloading",
		Progress: 30,
		Message:  "Downloading update...",
	})

	arch := runtime.GOARCH
	if arch == "mipsle" || arch == "mipsel" {
		arch = "mipsle"
	}

	downloadURL := fmt.Sprintf("%s/%s/xkeen-control-panel-linux-%s",
		githubDownloadURL, info.LatestVersion, arch)

	tempFile := filepath.Join(os.TempDir(), "xkeen-control-panel.new")
	if err := downloadFile(downloadURL, tempFile); err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Download failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Step 2b: Verify SHA-256 checksum if checksums.txt is available
	checksumsURL := fmt.Sprintf("%s/%s/checksums.txt", githubDownloadURL, info.LatestVersion)
	binaryName := fmt.Sprintf("xkeen-control-panel-linux-%s", arch)
	if err := verifyFileChecksum(tempFile, binaryName, checksumsURL); err != nil {
		_ = os.Remove(tempFile)
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Checksum verification failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	if err := os.Chmod(tempFile, 0755); err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Failed to set permissions: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Step 3: Backup current binary
	setUpdateState(UpdateStatus{
		Status:   "installing",
		Progress: 60,
		Message:  "Creating backup...",
	})

	binPath := filepath.Join(filepath.Dir(a.cfg.DataDir), "bin/xkeen-control-panel")
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	os.MkdirAll(backupDir, 0755)

	backupPath := filepath.Join(backupDir, fmt.Sprintf("xkeen-control-panel.bak.%d", time.Now().Unix()))
	if err := copyFile(binPath, backupPath); err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Backup failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Step 4: Atomic replace
	st := getUpdateState()
	st.Progress = 75
	st.Message = "Installing update..."
	setUpdateState(st)

	if err := os.Rename(tempFile, binPath); err != nil {
		// Try to restore backup
		os.Rename(backupPath, binPath)
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Install failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Prune old backups, keep latest 5
	if err := pruneBackupsDir(backupDir, 5); err != nil {
		log.Printf("Update: pruneBackups warning: %v", err)
	}

	// Step 5: Restart
	setUpdateState(UpdateStatus{
		Status:    "restarting",
		Progress:  90,
		Message:   "Restarting...",
		Timestamp: time.Now().Unix(),
	})

	// Give time for response to be sent
	time.Sleep(500 * time.Millisecond)

	go a.restartProcess(binPath, a.cfg.DataDir)
}

func (a *API) restartProcess(binPath string, dataDir string) {
	// Fork new process
	configPath := filepath.Join(dataDir, "config.json")
	cmd := exec.Command(binPath, "-config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Restart failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Health check
	time.Sleep(2 * time.Second)

	port := a.cfg.Port
	healthURL := fmt.Sprintf("http://localhost:%d/api/version", port)

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 10; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			setUpdateState(UpdateStatus{
				Status:    "done",
				Progress:  100,
				Message:   "Update complete",
				Timestamp: time.Now().Unix(),
			})

			// Exit old process
			time.Sleep(1 * time.Second)
			os.Exit(0)
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}

	// Health check failed - rollback
	setUpdateState(UpdateStatus{
		Status:    "failed",
		Message:   "Health check failed, rollback required",
		Timestamp: time.Now().Unix(),
	})
}

func fetchLatestRelease(channel string) (*UpdateInfo, error) {
	client := utils.SafeHTTPClient(15 * time.Second)
	resp, err := client.Get(githubAPIReleases + "?per_page=10")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Body       string `json:"body"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	for _, rel := range releases {
		tag := strings.TrimPrefix(rel.TagName, "v")

		if channel == "stable" && !rel.Prerelease {
			return &UpdateInfo{
				LatestVersion: tag,
				Changelog:     rel.Body,
			}, nil
		}
		if channel == "beta" && (strings.Contains(tag, "beta") || !rel.Prerelease) {
			return &UpdateInfo{
				LatestVersion: tag,
				Changelog:     rel.Body,
			}, nil
		}
		if channel == "dev" {
			return &UpdateInfo{
				LatestVersion: tag,
				Changelog:     rel.Body,
			}, nil
		}
	}

	return nil, fmt.Errorf("no release found for channel %s", channel)
}

func fetchChangelog(version string) (string, error) {
	client := utils.SafeHTTPClient(15 * time.Second)
	resp, err := client.Get(githubAPIReleases + "/tags/v" + version)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var release struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.Body, nil
}

func downloadFile(url, filepath string) error {
	client := utils.SafeHTTPClient(300 * time.Second)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// verifyFileChecksum downloads checksums.txt from the release and verifies the SHA-256
// of the given filePath against the entry for binaryName.
// If checksums.txt returns 404, it logs a warning and returns nil (backward compat).
// If the checksum does not match, returns an error.
func verifyFileChecksum(filePath, binaryName, checksumsURL string) error {
	return verifyFileChecksumWithClient(filePath, binaryName, checksumsURL, utils.SafeHTTPClient(30*time.Second))
}

// verifyFileChecksumWithClient is the testable variant that accepts an explicit *http.Client.
func verifyFileChecksumWithClient(filePath, binaryName, checksumsURL string, client *http.Client) error {
	resp, err := client.Get(checksumsURL)
	if err != nil {
		log.Printf("Update: could not download checksums.txt: %v — skipping verification", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("Update: checksums.txt not found for this release — skipping verification (backward compat)")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Update: checksums.txt HTTP %d — skipping verification", resp.StatusCode)
		return nil
	}

	// Parse "sha256sum  filename" lines
	expectedHash := ""
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[1] == binaryName || strings.HasSuffix(parts[1], "/"+binaryName) {
			expectedHash = strings.ToLower(parts[0])
			break
		}
	}

	if expectedHash == "" {
		log.Printf("Update: no checksum entry found for %s in checksums.txt — skipping verification", binaryName)
		return nil
	}

	// Compute SHA-256 of downloaded file
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open binary for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("compute sha256: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	log.Printf("Update: SHA-256 checksum verified OK for %s", binaryName)
	return nil
}

// pruneBackupsDir keeps the most recent `keep` files in dir, removing older ones.
func pruneBackupsDir(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// os.ReadDir returns entries sorted by name (ascending); timestamp suffix ensures order.
	if len(entries) <= keep {
		return nil
	}
	toRemove := entries[:len(entries)-keep]
	for _, e := range toRemove {
		p := filepath.Join(dir, e.Name())
		if err := os.Remove(p); err != nil {
			log.Printf("Update: failed to remove old backup %s: %v", p, err)
		}
	}
	return nil
}
