package handlers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
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
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
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

	info.HasUpdate = updateAvailable(info.LatestVersion, currentVersion)

	if info.HasUpdate {
		arch := runtime.GOARCH
		if arch == "mipsle" || arch == "mipsel" {
			arch = "mipsle"
		}
		info.DownloadURL = fmt.Sprintf("%s/v%s/xcp_v%s_%s",
			githubDownloadURL, info.LatestVersion, info.LatestVersion, arch)
	}

	JSONSuccess(w, info)
}

func (a *API) UpdateChangelog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
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

	// Lock state BEFORE spawning goroutine to prevent race condition
	st.Status = "checking"
	st.Progress = 5
	st.Timestamp = time.Now().Unix()
	setUpdateState(st)

	go a.performUpdate(channel)

	JSONSuccess(w, getUpdateState())
}

func (a *API) UpdateRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	binPath := "/opt/sbin/xcp"
	if exe, err := os.Executable(); err == nil {
		if realPath, err := filepath.EvalSymlinks(exe); err == nil {
			binPath = realPath
		} else {
			binPath = exe
		}
	}

	// Find latest backup by modification time (not by filename)
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) == 0 {
		JSONError(w, http.StatusNotFound, "No backup found")
		return
	}

	var latestBackup string
	var latestModTime time.Time
	for _, entry := range backups {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestModTime) {
			latestModTime = info.ModTime()
			latestBackup = filepath.Join(backupDir, entry.Name())
		}
	}
	if latestBackup == "" {
		JSONError(w, http.StatusNotFound, "No backup found")
		return
	}

	// Stop current binary
	st := UpdateStatus{
		Status:    "restoring",
		Progress:  10,
		Timestamp: time.Now().Unix(),
	}
	setUpdateState(st)

	// Replace with backup via temporary file to preserve backup on disk and set executable permissions
	tempBinPath := binPath + ".rollback"
	if err := copyFile(latestBackup, tempBinPath); err != nil {
		st = getUpdateState()
		st.Status = "failed"
		st.Message = "Rollback failed: " + err.Error()
		setUpdateState(st)
		JSONError(w, http.StatusInternalServerError, st.Message)
		return
	}
	if err := os.Chmod(tempBinPath, 0755); err != nil {
		log.Printf("Rollback: failed to chmod binary: %v", err)
	}

	if err := os.Rename(tempBinPath, binPath); err != nil {
		_ = os.Remove(tempBinPath)
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

	go a.restartProcess(binPath, latestBackup, a.cfg.DataDir, "")

	JSONSuccess(w, getUpdateState())
}

func (a *API) UpdateStatusEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	JSONSuccess(w, getUpdateState())
}

func (a *API) UpdateEventsSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		a.errorResponse(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Сразу отправляем текущий статус
	state := getUpdateState()
	data, err := json.Marshal(state)
	if err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastState := state

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			currentState := getUpdateState()
			if currentState.Status != lastState.Status || currentState.Progress != lastState.Progress || currentState.Message != lastState.Message {
				data, err := json.Marshal(currentState)
				if err == nil {
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
				lastState = currentState
			}
			if currentState.Status == "done" || currentState.Status == "failed" {
				return
			}
		}
	}
}

// UpdateChannelHandler маршрутизирует GET → UpdateChannelGet, POST → UpdateChannelSet.
func (a *API) UpdateChannelHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.UpdateChannelGet(w, r)
	case http.MethodPost:
		a.UpdateChannelSet(w, r)
	default:
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
	}
}

// UpdateChannelGet возвращает сохранённый канал обновлений (stable/beta/dev).
func (a *API) UpdateChannelGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	ch := a.cfg.UpdateChannel
	if ch == "" {
		ch = "stable"
	}
	JSONSuccess(w, map[string]string{"channel": ch})
}

// UpdateChannelSet сохраняет выбранный канал обновлений в config.json.
func (a *API) UpdateChannelSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.errorResponse(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	switch body.Channel {
	case "stable", "beta":
	default:
		a.errorResponse(w, "channel must be stable or beta", http.StatusBadRequest)
		return
	}
	a.cfg.UpdateChannel = body.Channel
	if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
		a.errorResponse(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	JSONSuccess(w, map[string]string{"channel": body.Channel})
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
	if !updateAvailable(info.LatestVersion, currentVersion) {
		setUpdateState(UpdateStatus{
			Status:    "done",
			Progress:  100,
			Message:   "Already up to date",
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Determine binary path early so temp file is on the same filesystem
	binPath := "/opt/sbin/xcp"
	if exe, err := os.Executable(); err == nil {
		if realPath, err := filepath.EvalSymlinks(exe); err == nil {
			binPath = realPath
		} else {
			binPath = exe
		}
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

	downloadURL := fmt.Sprintf("%s/v%s/xcp_v%s_%s",
		githubDownloadURL, info.LatestVersion, info.LatestVersion, arch)

	// Download to the same directory as the binary to avoid cross-device rename
	tempFile := filepath.Join(filepath.Dir(binPath), "xcp.new")
	if err := downloadFile(downloadURL, tempFile); err != nil {
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   "Download failed: " + err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	// Step 2b: Verify SHA-256 checksum if checksums.txt is available
	checksumsURL := fmt.Sprintf("%s/v%s/checksums.txt", githubDownloadURL, info.LatestVersion)
	binaryName := fmt.Sprintf("xcp_v%s_%s", info.LatestVersion, arch)
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
	backupDir := filepath.Join(a.cfg.DataDir, "backup")
	_ = os.MkdirAll(backupDir, 0755)

	backupPath := filepath.Join(backupDir, fmt.Sprintf("xcp.bak.%d", time.Now().Unix()))
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

	go a.restartProcess(binPath, backupPath, a.cfg.DataDir, info.LatestVersion)
}

func (a *API) restartProcess(binPath string, backupPath string, dataDir string, expectedVersion string) {
	// Shutdown the server listener so the new process can bind to the port
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Update: shutdown error: %v", err)
	}

	// Fork new process
	configPath := filepath.Join(dataDir, "config.json")
	cmd := exec.Command(binPath, "-config", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		// If starting the new binary fails, rollback immediately
		msg := "Restart failed: " + err.Error()
		if backupPath != "" {
			if err := copyFile(backupPath, binPath); err == nil {
				// Start the backup binary to restore the server
				rollbackCmd := exec.Command(binPath, "-config", configPath)
				rollbackCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				_ = rollbackCmd.Start()
				msg = "Проверка не удалась, выполнен авто-откат на резервную копию."
			}
		}
		setUpdateState(UpdateStatus{
			Status:    "failed",
			Message:   msg,
			Timestamp: time.Now().Unix(),
		})
		time.Sleep(1 * time.Second)
		os.Exit(1)
	}

	// Health check
	time.Sleep(2 * time.Second)

	port := a.cfg.Port
	var healthURL string
	var client *http.Client
	if a.cfg.HTTPS.Enabled {
		healthURL = fmt.Sprintf("https://localhost:%d/api/version", port)
		client = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // localhost-only health check
			},
		}
	} else {
		healthURL = fmt.Sprintf("http://localhost:%d/api/version", port)
		client = &http.Client{Timeout: 5 * time.Second}
	}
	for i := 0; i < 10; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			ok := true
			if expectedVersion != "" {
				var versionResp struct {
					PanelVersion string `json:"panel_version"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&versionResp)
				ok = versionMatches(expectedVersion, versionResp.PanelVersion)
			}
			resp.Body.Close()
			if ok {
				setUpdateState(UpdateStatus{
					Status:    "done",
					Progress:  100,
					Message:   "Update complete",
					Timestamp: time.Now().Unix(),
				})
				time.Sleep(1 * time.Second)
				os.Exit(0)
			}
		} else if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}

	// Health check failed - rollback
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	msg := "Проверка работоспособности не удалась."
	if backupPath != "" {
		if err := copyFile(backupPath, binPath); err != nil {
			msg = "Проверка не удалась. Откат завершился ошибкой: " + err.Error()
		} else {
			// Start the backup binary to restore the server
			rollbackCmd := exec.Command(binPath, "-config", configPath)
			rollbackCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := rollbackCmd.Start(); err == nil {
				msg = "Проверка не удалась, выполнен авто-откат на резервную копию."
			} else {
				msg = "Проверка не удалась, авто-откат не смог запуститься: " + err.Error()
			}
		}
	}

	setUpdateState(UpdateStatus{
		Status:    "failed",
		Message:   msg,
		Timestamp: time.Now().Unix(),
	})
	time.Sleep(1 * time.Second)
	os.Exit(1)
}

// compareSemver сравнивает две версии без префикса "v".
// Возвращает -1 (a < b), 0 (a == b), 1 (a > b).
// Pre-release суффикс (через "-") считается меньше стабильной версии.
// compareSemver compares two versions per SemVer 2.0: numeric core, then
// pre-release identifiers (numeric ones numerically, "dev.9" < "dev.17"),
// a release ranks above its pre-releases, build metadata is ignored.
func compareSemver(a, b string) int {
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	aCore, aPre, aHasPre := strings.Cut(a, "-")
	bCore, bPre, bHasPre := strings.Cut(b, "-")

	aNums := strings.Split(aCore, ".")
	bNums := strings.Split(bCore, ".")
	for i := 0; i < max(len(aNums), len(bNums)); i++ {
		var an, bn int
		if i < len(aNums) {
			an, _ = strconv.Atoi(aNums[i])
		}
		if i < len(bNums) {
			bn, _ = strconv.Atoi(bNums[i])
		}
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
	}

	switch {
	case aHasPre && !bHasPre:
		return -1
	case !aHasPre && bHasPre:
		return 1
	case !aHasPre && !bHasPre:
		return 0
	}

	aIDs := strings.Split(aPre, ".")
	bIDs := strings.Split(bPre, ".")
	for i := 0; i < min(len(aIDs), len(bIDs)); i++ {
		an, aErr := strconv.Atoi(aIDs[i])
		bn, bErr := strconv.Atoi(bIDs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aErr == nil:
			return -1 // numeric identifiers rank below alphanumeric ones
		case bErr == nil:
			return 1
		default:
			if c := strings.Compare(aIDs[i], bIDs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(aIDs) < len(bIDs):
		return -1
	case len(aIDs) > len(bIDs):
		return 1
	}
	return 0
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Body       string `json:"body"`
}

func fetchLatestRelease(channel string) (*UpdateInfo, error) {
	client := utils.SafeHTTPClient(15 * time.Second)
	resp, err := client.Get(githubAPIReleases + "?per_page=10")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	return pickLatestRelease(releases, channel)
}

// pickLatestRelease выбирает самый новый по semver релиз канала: GitHub сортирует
// релизы по дате публикации, и пересобранный pre-release оказывается выше более
// нового stable. Канал beta включает stable, иначе после релиза, когда dev-сборки
// удалены, beta остаётся без версий.
func pickLatestRelease(releases []githubRelease, channel string) (*UpdateInfo, error) {
	var best *githubRelease
	for i := range releases {
		rel := &releases[i]
		if rel.Prerelease && channel != "beta" {
			continue
		}
		if best == nil || compareSemver(strings.TrimPrefix(rel.TagName, "v"), strings.TrimPrefix(best.TagName, "v")) > 0 {
			best = rel
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no release found for channel %s", channel)
	}
	return &UpdateInfo{
		LatestVersion: strings.TrimPrefix(best.TagName, "v"),
		Changelog:     best.Body,
	}, nil
}

// updateAvailable решает, предлагать ли обновление. Rolling dev-сборка пересобирается
// под тем же тегом, поэтому равная версия для неё тоже считается обновлением;
// версия ниже текущей не предлагается никогда.
func updateAvailable(latest, current string) bool {
	if latest == "" {
		return false
	}
	if isDevVersion(current) {
		return compareSemver(semverCore(latest), semverCore(current)) >= 0
	}
	return compareSemver(latest, current) > 0
}

// isDevVersion reports whether v is a development build: the rolling
// channel "X.Y.Z-dev" or a build of it "X.Y.Z-dev.N+gSHA" (scripts/version.sh).
func isDevVersion(v string) bool {
	v, _, _ = strings.Cut(v, "+")
	_, pre, ok := strings.Cut(v, "-")
	return ok && (pre == "dev" || strings.HasPrefix(pre, "dev."))
}

// versionMatches reports whether the running binary reports the version of
// the release that was installed. Dev releases are published under the
// channel tag "X.Y.Z-dev" while the binary carries "X.Y.Z-dev.N+gSHA".
func versionMatches(expected, actual string) bool {
	expected = strings.TrimPrefix(expected, "v")
	actual = strings.TrimPrefix(actual, "v")
	if actual == expected {
		return true
	}
	build, _, _ := strings.Cut(actual, "+")
	return build == expected || strings.HasPrefix(build, expected+".")
}

func semverCore(v string) string {
	core, _, _ := strings.Cut(v, "-")
	return core
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

func downloadFile(url, destPath string) error {
	client := utils.SafeHTTPClient(300 * time.Second)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, resp.Body)
	// Always close before possible removal
	closeErr := out.Close()

	if copyErr != nil {
		_ = os.Remove(destPath) // cleanup on copy error
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destPath) // cleanup on close error
		return closeErr
	}
	return nil
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

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	// Sync before Close — ensure data is flushed to disk (power-loss safety on router)
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
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
