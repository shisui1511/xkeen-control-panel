package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

var updateState = UpdateStatus{Status: "idle"}

func (a *API) UpdateCheck(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	currentVersion := strings.TrimPrefix(a.srv.GetVersion(), "v")

	info, err := fetchLatestRelease(channel)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
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

	a.jsonResponse(w, info)
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

	if updateState.Status != "idle" && updateState.Status != "failed" {
		a.errorResponse(w, "Update already in progress", http.StatusConflict)
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	// Run update in background
	go a.performUpdate(channel)

	updateState.Status = "checking"
	updateState.Progress = 5
	updateState.Timestamp = time.Now().Unix()

	a.jsonResponse(w, updateState)
}

func (a *API) UpdateRollback(w http.ResponseWriter, r *http.Request) {
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	binPath := filepath.Join(filepath.Dir(a.cfg.DataDir), "bin/xkeen-control-panel")

	// Find latest backup
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) == 0 {
		a.errorResponse(w, "No backup found", http.StatusNotFound)
		return
	}

	latestBackup := filepath.Join(backupDir, backups[len(backups)-1].Name())

	// Stop current binary
	updateState.Status = "restoring"
	updateState.Progress = 10
	updateState.Timestamp = time.Now().Unix()

	// Replace with backup
	if err := os.Rename(latestBackup, binPath); err != nil {
		updateState.Status = "failed"
		updateState.Message = "Rollback failed: " + err.Error()
		a.errorResponse(w, updateState.Message, http.StatusInternalServerError)
		return
	}

	// Restart
	updateState.Status = "restarting"
	updateState.Progress = 90

	go a.restartProcess(binPath, a.cfg.DataDir)

	a.jsonResponse(w, updateState)
}

func (a *API) UpdateStatusEndpoint(w http.ResponseWriter, r *http.Request) {
	a.jsonResponse(w, updateState)
}

func (a *API) performUpdate(channel string) {
	defer func() {
		if r := recover(); r != nil {
			updateState.Status = "failed"
			updateState.Message = fmt.Sprintf("Panic: %v", r)
			updateState.Timestamp = time.Now().Unix()
		}
	}()

	// Step 1: Check latest release
	updateState.Status = "checking"
	updateState.Progress = 10
	updateState.Message = "Checking for updates..."

	info, err := fetchLatestRelease(channel)
	if err != nil {
		updateState.Status = "failed"
		updateState.Message = "Failed to check updates: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
		return
	}

	currentVersion := strings.TrimPrefix(a.srv.GetVersion(), "v")
	if info.LatestVersion == currentVersion {
		updateState.Status = "done"
		updateState.Progress = 100
		updateState.Message = "Already up to date"
		updateState.Timestamp = time.Now().Unix()
		return
	}

	// Step 2: Download
	updateState.Status = "downloading"
	updateState.Progress = 30
	updateState.Message = "Downloading update..."

	arch := runtime.GOARCH
	if arch == "mipsle" || arch == "mipsel" {
		arch = "mipsle"
	}

	downloadURL := fmt.Sprintf("%s/%s/xkeen-control-panel-linux-%s",
		githubDownloadURL, info.LatestVersion, arch)

	tempFile := filepath.Join(os.TempDir(), "xkeen-control-panel.new")
	if err := downloadFile(downloadURL, tempFile); err != nil {
		updateState.Status = "failed"
		updateState.Message = "Download failed: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
		return
	}

	if err := os.Chmod(tempFile, 0755); err != nil {
		updateState.Status = "failed"
		updateState.Message = "Failed to set permissions: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
		return
	}

	// Step 3: Backup current binary
	updateState.Status = "installing"
	updateState.Progress = 60
	updateState.Message = "Creating backup..."

	binPath := filepath.Join(filepath.Dir(a.cfg.DataDir), "bin/xkeen-control-panel")
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	os.MkdirAll(backupDir, 0755)

	backupPath := filepath.Join(backupDir, fmt.Sprintf("xkeen-control-panel.bak.%d", time.Now().Unix()))
	if err := copyFile(binPath, backupPath); err != nil {
		updateState.Status = "failed"
		updateState.Message = "Backup failed: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
		return
	}

	// Step 4: Atomic replace
	updateState.Progress = 75
	updateState.Message = "Installing update..."

	if err := os.Rename(tempFile, binPath); err != nil {
		// Try to restore backup
		os.Rename(backupPath, binPath)
		updateState.Status = "failed"
		updateState.Message = "Install failed: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
		return
	}

	// Step 5: Restart
	updateState.Status = "restarting"
	updateState.Progress = 90
	updateState.Message = "Restarting..."
	updateState.Timestamp = time.Now().Unix()

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
		updateState.Status = "failed"
		updateState.Message = "Restart failed: " + err.Error()
		updateState.Timestamp = time.Now().Unix()
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
			updateState.Status = "done"
			updateState.Progress = 100
			updateState.Message = "Update complete"
			updateState.Timestamp = time.Now().Unix()

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
	updateState.Status = "failed"
	updateState.Message = "Health check failed, rollback required"
	updateState.Timestamp = time.Now().Unix()
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
