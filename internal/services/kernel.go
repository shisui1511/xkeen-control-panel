package services

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// allowedKernelRoots are the only directories where kernel binaries and backups may live.
var allowedKernelRoots = []string{
	"/opt/bin/",
	"/opt/etc/",
	os.TempDir() + "/",
}

// xrayProbePaths and mihomoProbePaths list directories to search for each kernel binary.
// These are package-level variables so tests can override them.
var xrayProbePaths = []string{
	"/opt/sbin/xray",
	"/opt/bin/xray",
	"/opt/xray/xray",
	"/usr/sbin/xray",
	"/usr/local/bin/xray",
	"/usr/bin/xray",
}

var mihomoProbePaths = []string{
	"/opt/sbin/mihomo",
	"/opt/bin/mihomo",
	"/opt/mihomo/mihomo",
	"/usr/sbin/mihomo",
	"/usr/local/bin/mihomo",
	"/usr/bin/mihomo",
}

// findKernelBinary searches known paths for the kernel binary named `name`.
// Returns the first found path, or "" if not found anywhere.
func findKernelBinary(name string) string {
	var paths []string
	switch name {
	case "xray":
		paths = xrayProbePaths
	case "mihomo":
		paths = mihomoProbePaths
	default:
		paths = []string{
			"/opt/sbin/" + name,
			"/opt/bin/" + name,
			"/usr/sbin/" + name,
			"/usr/local/bin/" + name,
			"/usr/bin/" + name,
		}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Fallback: use PATH lookup
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func validateKernelPath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	// Reject raw paths containing ".." components to prevent traversal regardless of Clean result.
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return errors.New("path traversal detected")
		}
	}
	clean := filepath.Clean(path)
	// Ensure the cleaned path actually starts with one of the allowed roots
	for _, root := range allowedKernelRoots {
		if strings.HasPrefix(clean+"/", root) || strings.HasPrefix(clean, root) {
			return nil
		}
	}
	return errors.New("path is outside allowed directories")
}

func safeTempPath(name string) (string, error) {
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("invalid temp file name")
	}
	return filepath.Join(os.TempDir(), name), nil
}

// versionCache holds the detected version string and its expiry time.
// Access must be protected by the KernelService mutex (or the caller's lock).
type versionCache struct {
	mu      sync.Mutex
	value   string
	expires time.Time
}

// KernelInfo holds info about an installed kernel
type KernelInfo struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	BinaryPath     string `json:"binary_path"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	Channel        string `json:"channel"` // stable, preview
	Repo           string `json:"repo"`
	Status         string `json:"status"`         // idle, checking, downloading, installing, done, failed
	ProcessStatus  string `json:"process_status"` // running, stopped, not_installed, not_accessible, unknown
	Message        string `json:"message"`

	// binaryPathCachedAt records when BinaryPath was last resolved via auto-detection.
	// Access must be protected by the KernelService mutex.
	binaryPathCachedAt time.Time

	// verCache caches the result of detectVersion for 60 seconds to avoid
	// repeatedly spawning a subprocess on every status poll.
	// Must be a pointer so that copying KernelInfo does not copy the embedded mutex.
	verCache *versionCache
}

// kernelProcessStatus detects whether the kernel process is running.
// Method 1: scan /proc/*/exe readlinks for the binary basename.
// Method 2 (fallback): run pidof <basename> if /proc appears empty.
// Returns "not_installed", "not_accessible", "running", "stopped", or "unknown".
func kernelProcessStatus(binaryPath string) string {
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "not_installed"
	}

	// Check if the binary is accessible (readable/executable)
	f, err := os.Open(binaryPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return "not_accessible"
		}
		return "not_accessible"
	}
	f.Close()

	base := filepath.Base(binaryPath)

	// Method 1: /proc/*/exe readlink (no external tools required)
	matches, _ := filepath.Glob("/proc/*/exe")
	for _, link := range matches {
		target, err := os.Readlink(link)
		if err == nil && filepath.Base(target) == base {
			return "running"
		}
	}

	// Method 2: pidof fallback when /proc gives no entries
	if len(matches) == 0 {
		out, err := exec.Command("pidof", base).Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			return "running"
		}
		if err != nil {
			// pidof itself unavailable — cannot determine state
			return "unknown"
		}
	}

	return "stopped"
}

// KernelService manages proxy kernels (xray, mihomo)
type KernelService struct {
	kernels      map[string]*KernelInfo
	mu           sync.RWMutex
	installLocks sync.Map // per-kernel install lock; key: string, value: *sync.Mutex

	// statFunc is used to check if a file exists; defaults to os.Stat.
	// Overridable in tests to verify TTL caching without touching the filesystem.
	statFunc func(string) (os.FileInfo, error)
}

func NewKernelService() *KernelService {
	svc := &KernelService{
		kernels:  make(map[string]*KernelInfo),
		statFunc: os.Stat,
	}

	now := time.Now()

	// Register known kernels with auto-detected binary paths
	xrayPath := findKernelBinary("xray")
	if xrayPath == "" {
		log.Printf("WARNING: failed to auto-detect Xray binary. Checked paths: %s", strings.Join(xrayProbePaths, ", "))
		xrayPath = "/opt/bin/xray" // fallback default for display
	}
	svc.kernels["xray"] = &KernelInfo{
		Name:               "xray",
		DisplayName:        "Xray Core",
		BinaryPath:         xrayPath,
		Channel:            "stable",
		Repo:               "XTLS/Xray-core",
		binaryPathCachedAt: now,
		verCache:           &versionCache{},
	}

	mihomoPath := findKernelBinary("mihomo")
	if mihomoPath == "" {
		log.Printf("WARNING: failed to auto-detect Mihomo binary. Checked paths: %s", strings.Join(mihomoProbePaths, ", "))
		mihomoPath = "/opt/bin/mihomo" // fallback default for display
	}
	svc.kernels["mihomo"] = &KernelInfo{
		Name:               "mihomo",
		DisplayName:        "Mihomo (Clash.Meta)",
		BinaryPath:         mihomoPath,
		Channel:            "stable",
		Repo:               "MetaCubeX/mihomo",
		binaryPathCachedAt: now,
		verCache:           &versionCache{},
	}

	// Detect current versions (outside lock — no concurrent calls yet)
	for _, k := range svc.kernels {
		k.CurrentVersion = svc.detectVersion(k)
	}

	return svc
}

// resolveBinaryPath refreshes k.BinaryPath via auto-detection if the 60s TTL has expired.
// Must be called while holding s.mu (write lock).
func (s *KernelService) resolveBinaryPath(k *KernelInfo) {
	if time.Since(k.binaryPathCachedAt) <= 60*time.Second {
		return
	}
	// Use statFunc (injectable for tests) to probe paths
	found := ""
	var paths []string
	switch k.Name {
	case "xray":
		paths = xrayProbePaths
	case "mihomo":
		paths = mihomoProbePaths
	default:
		paths = []string{
			"/opt/sbin/" + k.Name,
			"/opt/bin/" + k.Name,
			"/usr/sbin/" + k.Name,
			"/usr/local/bin/" + k.Name,
			"/usr/bin/" + k.Name,
		}
	}
	for _, p := range paths {
		if _, err := s.statFunc(p); err == nil {
			found = p
			break
		}
	}
	// Fallback to exec.LookPath if statFunc didn't find anything
	if found == "" {
		if p, err := exec.LookPath(k.Name); err == nil {
			found = p
		}
	}
	// Only update if a path was found; preserve previous working path otherwise
	if found != "" {
		k.BinaryPath = found
	}
	k.binaryPathCachedAt = time.Now()
}

func (s *KernelService) List() []KernelInfo {
	// Resolve binary paths under write lock before taking snapshots
	s.mu.Lock()
	for _, k := range s.kernels {
		s.resolveBinaryPath(k)
	}
	snapshots := make([]KernelInfo, 0, len(s.kernels))
	for _, k := range s.kernels {
		snapshots = append(snapshots, *k)
	}
	s.mu.Unlock()

	// Resolve live data outside the global lock to avoid blocking Install/CheckLatest
	for i := range snapshots {
		snapshots[i].CurrentVersion = s.detectVersion(&snapshots[i])
		snapshots[i].ProcessStatus = kernelProcessStatus(snapshots[i].BinaryPath)
	}
	return snapshots
}

func (s *KernelService) Get(name string) *KernelInfo {
	// Resolve binary path under write lock before taking snapshot
	s.mu.Lock()
	k, ok := s.kernels[name]
	var snap KernelInfo
	if ok {
		s.resolveBinaryPath(k)
		snap = *k
	}
	s.mu.Unlock()

	if !ok {
		return nil
	}
	// Refresh version and process status outside global lock
	snap.CurrentVersion = s.detectVersion(&snap)
	snap.ProcessStatus = kernelProcessStatus(snap.BinaryPath)
	return &snap
}

func (s *KernelService) SetChannel(name, channel string) bool {
	if channel != "stable" && channel != "preview" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.kernels[name]; ok {
		k.Channel = channel
		return true
	}
	return false
}

// versionCacheTTL is the duration for which a detected version string is considered valid.
const versionCacheTTL = 60 * time.Second

// detectVersion runs the binary with a version flag and caches the result for
// versionCacheTTL (60 s) to avoid spawning a subprocess on every poll.
func (s *KernelService) detectVersion(k *KernelInfo) string {
	if k.verCache == nil {
		k.verCache = &versionCache{}
	}
	k.verCache.mu.Lock()
	defer k.verCache.mu.Unlock()

	if k.verCache.value != "" && time.Now().Before(k.verCache.expires) {
		return k.verCache.value
	}

	if _, err := os.Stat(k.BinaryPath); os.IsNotExist(err) {
		return "not installed"
	}

	var cmd *exec.Cmd
	switch k.Name {
	case "xray":
		cmd = exec.Command(k.BinaryPath, "version")
	case "mihomo":
		cmd = exec.Command(k.BinaryPath, "-v")
	default:
		return "unknown"
	}

	out, err := cmd.CombinedOutput()
	output := utils.StripANSI(string(out))
	if err != nil {
		return "error"
	}

	result := s.parseVersion(k.Name, output)
	k.verCache.value = result
	k.verCache.expires = time.Now().Add(versionCacheTTL)
	return result
}

// versionRe is a generic version pattern that matches semver-like strings with an
// optional leading 'v' or 'V' prefix, including pre-release suffixes (e.g. v1.8.24-rc1).
var versionRe = regexp.MustCompile(`[vV]?(\d+\.\d+\.\d+[^\s]*)`)

func (s *KernelService) parseVersion(name, output string) string {
	output = strings.TrimSpace(output)
	switch name {
	case "xray":
		// Xray 1.8.24 (Xray, Penetrates Everything.) ...
		re := regexp.MustCompile(`Xray\s+` + versionRe.String())
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
		// Fallback: generic version pattern
		if m := versionRe.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	case "mihomo":
		// Mihomo Version: v1.18.0 ...
		re := regexp.MustCompile(`(?:Mihomo\s+)?Version[:\s]*` + versionRe.String())
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
		// Fallback: generic version pattern
		if m := versionRe.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	}
	return "unknown"
}

// CheckLatest queries GitHub API for latest release.
// ctx is used to cancel the HTTP request (e.g. on service shutdown).
func (s *KernelService) CheckLatest(ctx context.Context, name string) error {
	s.mu.Lock()
	k := s.kernels[name]
	if k == nil {
		s.mu.Unlock()
		return fmt.Errorf("kernel not found: %s", name)
	}
	k.Status = "checking"
	k.Message = "Checking for updates..."
	// Snapshot fields needed for the HTTP call
	repo := k.Repo
	channel := k.Channel
	currentVersion := k.CurrentVersion
	s.mu.Unlock()

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if channel != "stable" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=5", repo)
	}

	client := utils.SafeHTTPClient(15 * time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		s.mu.Lock()
		if kk := s.kernels[name]; kk != nil {
			kk.Status = "failed"
			kk.Message = "Request error: " + err.Error()
		}
		s.mu.Unlock()
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		s.mu.Lock()
		if kk := s.kernels[name]; kk != nil {
			kk.Status = "failed"
			kk.Message = "GitHub API error: " + err.Error()
		}
		s.mu.Unlock()
		return err
	}
	defer resp.Body.Close()

	var latestVersion string
	if channel == "stable" {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			s.mu.Lock()
			if kk := s.kernels[name]; kk != nil {
				kk.Status = "failed"
				kk.Message = "Parse error: " + err.Error()
			}
			s.mu.Unlock()
			return err
		}
		latestVersion = strings.TrimPrefix(release.TagName, "v")
	} else {
		var releases []struct {
			TagName    string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			s.mu.Lock()
			if kk := s.kernels[name]; kk != nil {
				kk.Status = "failed"
				kk.Message = "Parse error: " + err.Error()
			}
			s.mu.Unlock()
			return err
		}
		for _, rel := range releases {
			if channel == "preview" && rel.Prerelease {
				latestVersion = strings.TrimPrefix(rel.TagName, "v")
				break
			}
		}
	}

	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.LatestVersion = latestVersion
		kk.HasUpdate = latestVersion != "" && latestVersion != currentVersion
		kk.Status = "idle"
		kk.Message = ""
	}
	s.mu.Unlock()
	return nil
}

// Install downloads and installs the kernel
func (s *KernelService) Install(name string) error {
	// Verify kernel exists first
	s.mu.RLock()
	_, kernelExists := s.kernels[name]
	s.mu.RUnlock()
	if !kernelExists {
		return fmt.Errorf("kernel not found: %s", name)
	}

	// Acquire per-kernel install lock using TryLock; return 409-style error if already in progress
	mu := &sync.Mutex{}
	actual, _ := s.installLocks.LoadOrStore(name, mu)
	installMu := actual.(*sync.Mutex)
	if !installMu.TryLock() {
		return fmt.Errorf("install already in progress")
	}
	defer installMu.Unlock()

	// helper to update kernel status under the global lock
	setStatus := func(status, message string) {
		s.mu.Lock()
		if kk := s.kernels[name]; kk != nil {
			kk.Status = status
			kk.Message = message
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	k := s.kernels[name]
	if k == nil {
		s.mu.Unlock()
		return fmt.Errorf("kernel not found: %s", name)
	}
	k.Status = "downloading"
	k.Message = "Downloading..."
	// Snapshot immutable fields needed outside the lock
	binaryPath := k.BinaryPath
	latestVersion := k.LatestVersion
	s.mu.Unlock()

	arch := runtime.GOARCH
	if arch == "mipsle" || arch == "mipsel" {
		arch = "mipsle-softfloat"
	} else if arch == "mips" {
		arch = "mips-softfloat"
	}

	// Build a temporary KernelInfo for buildDownloadURL (only needs Name, Repo, LatestVersion, Channel)
	s.mu.RLock()
	snap := *s.kernels[name]
	s.mu.RUnlock()

	downloadURL, filename := s.buildDownloadURL(&snap, arch)
	if downloadURL == "" {
		setStatus("failed", "Unsupported architecture: "+arch)
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	tempFile, err := safeTempPath(filename)
	if err != nil {
		setStatus("failed", "Invalid filename: "+err.Error())
		return err
	}
	defer os.Remove(tempFile) // Cleanup archive after extraction

	if err := s.downloadFile(context.Background(), downloadURL, tempFile); err != nil {
		setStatus("failed", "Download failed: "+err.Error())
		return err
	}

	// Backup current binary
	setStatus("installing", "Creating backup...")

	backupDir := filepath.Join(filepath.Dir(binaryPath), ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		setStatus("failed", "Backup dir failed: "+err.Error())
		return err
	}
	// Use only timestamp in backup name to avoid tainted data in path
	backupName := fmt.Sprintf("kernel.bak.%d", time.Now().Unix())
	backupPath := filepath.Join(backupDir, backupName)
	if err := validateKernelPath(backupPath); err != nil {
		setStatus("failed", "Invalid backup path: "+err.Error())
		return err
	}

	if _, err := os.Stat(binaryPath); err == nil {
		src, err := os.Open(binaryPath)
		if err != nil {
			setStatus("failed", "Backup failed: "+err.Error())
			return err
		}
		dst, err := os.Create(backupPath)
		if err != nil {
			src.Close()
			setStatus("failed", "Backup failed: "+err.Error())
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			setStatus("failed", "Backup failed: "+err.Error())
			return err
		}
	}

	// Extract if needed
	extractedPath := tempFile
	if strings.HasSuffix(tempFile, ".zip") {
		setStatus("installing", "Extracting...")
		extracted, err := s.extractZip(tempFile, name)
		if err != nil {
			setStatus("failed", "Extract failed: "+err.Error())
			return err
		}
		extractedPath = extracted
	} else if strings.HasSuffix(tempFile, ".gz") {
		setStatus("installing", "Extracting...")
		extracted, err := s.extractGz(tempFile)
		if err != nil {
			setStatus("failed", "Extract failed: "+err.Error())
			return err
		}
		extractedPath = extracted
	}

	// Ensure extracted file is cleaned up if rename fails or it's not moved
	if extractedPath != tempFile {
		defer os.Remove(extractedPath)
	}

	// Make executable and replace
	if err := validateKernelPath(extractedPath); err != nil {
		setStatus("failed", "Invalid extracted path: "+err.Error())
		return err
	}
	if err := os.Chmod(extractedPath, 0755); err != nil {
		setStatus("failed", "Chmod failed: "+err.Error())
		return err
	}

	// Atomic replace
	tempDest := filepath.Join(filepath.Dir(binaryPath), filepath.Base(binaryPath)+".new")
	if err := validateKernelPath(tempDest); err != nil {
		setStatus("failed", "Invalid temp dest path: "+err.Error())
		return err
	}
	if err := os.Rename(extractedPath, tempDest); err != nil {
		setStatus("failed", "Replace failed: "+err.Error())
		return err
	}
	if err := os.Rename(tempDest, binaryPath); err != nil {
		// Try rollback
		_ = os.Rename(backupPath, binaryPath)
		setStatus("failed", "Replace failed: "+err.Error())
		return err
	}

	// Prune old backups — keep at most 3 most recent
	_ = pruneBackups(backupDir, 3)

	// Verify new version and update metadata under lock
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.CurrentVersion = s.detectVersion(kk)
		kk.HasUpdate = kk.CurrentVersion != latestVersion
		kk.Status = "done"
		kk.Message = "Updated to " + kk.CurrentVersion
	}
	s.mu.Unlock()

	return nil
}

// pruneBackups removes oldest backup files in dir, keeping only the `keep` most recent.
// Files are sorted by name (timestamp suffix ensures lexicographic order = chronological order).
// Errors are logged but do not fail the caller.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Filter to backup files only
	var backups []string
	for _, e := range entries {
		if !e.IsDir() {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}

	// Sort ascending by name (oldest first) — names use Unix timestamp suffix
	// so lexicographic order equals chronological order.
	// os.ReadDir already returns entries sorted by name.
	if len(backups) <= keep {
		return nil
	}

	for _, old := range backups[:len(backups)-keep] {
		if err := os.Remove(old); err != nil {
			log.Printf("pruneBackups: failed to remove %s: %v", old, err)
		}
	}
	return nil
}

func (s *KernelService) buildDownloadURL(k *KernelInfo, arch string) (string, string) {
	version := k.LatestVersion
	if version == "" {
		return "", ""
	}

	switch k.Name {
	case "xray":
		// Xray: Xray-linux-arm64-v8a.zip or Xray-linux-mipsle-softfloat.zip
		var file string
		switch arch {
		case "arm64":
			file = fmt.Sprintf("Xray-linux-%s-v8a.zip", arch)
		case "mipsle-softfloat", "mips-softfloat":
			file = fmt.Sprintf("Xray-linux-%s.zip", arch)
		default:
			return "", ""
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", k.Repo, version, file), file

	case "mihomo":
		// Mihomo: mihomo-linux-arm64-v1.18.0.gz or mihomo-linux-mipsle-softfloat-v1.18.0.gz
		var file string
		switch arch {
		case "arm64", "mipsle-softfloat", "mips-softfloat":
			file = fmt.Sprintf("mihomo-linux-%s-v%s.gz", arch, version)
		default:
			return "", ""
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", k.Repo, version, file), file
	}

	return "", ""
}

func (s *KernelService) downloadFile(ctx context.Context, url, filepath string) error {
	client := utils.SafeHTTPClient(120 * time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// maxKernelExtractBytes caps the size of decompressed kernel binaries (50 MB).
const maxKernelExtractBytes = 50 * 1024 * 1024

func (s *KernelService) extractZip(zipPath, binaryName string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == binaryName || f.Name == binaryName+"-linux-"+runtime.GOARCH {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()

			outPath, err := safeTempPath(binaryName + ".new")
			if err != nil {
				return "", err
			}
			if err := validateKernelPath(outPath); err != nil {
				return "", err
			}
			out, err := os.Create(outPath)
			if err != nil {
				return "", err
			}
			defer out.Close()

			_, err = io.Copy(out, io.LimitReader(rc, maxKernelExtractBytes))
			return outPath, err
		}
	}

	return "", fmt.Errorf("binary not found in archive")
}

func (s *KernelService) extractGz(gzPath string) (string, error) {
	f, err := os.Open(gzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gr.Close()

	outPath := strings.TrimSuffix(filepath.Base(gzPath), ".gz")
	outPath, err = safeTempPath(outPath)
	if err != nil {
		return "", err
	}
	if err := validateKernelPath(outPath); err != nil {
		return "", err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, io.LimitReader(gr, maxKernelExtractBytes))
	return outPath, err
}

type KernelPathDebug struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Executable bool   `json:"executable"`
	Error      string `json:"error,omitempty"`
}

type KernelDebugInfo struct {
	XrayPaths   []KernelPathDebug `json:"xray_paths"`
	MihomoPaths []KernelPathDebug `json:"mihomo_paths"`
}

func (s *KernelService) GetDebugInfo() KernelDebugInfo {
	var info KernelDebugInfo
	for _, p := range xrayProbePaths {
		info.XrayPaths = append(info.XrayPaths, s.checkPathDebug(p))
	}
	for _, p := range mihomoProbePaths {
		info.MihomoPaths = append(info.MihomoPaths, s.checkPathDebug(p))
	}
	return info
}

func (s *KernelService) checkPathDebug(p string) KernelPathDebug {
	fi, err := s.statFunc(p)
	if err != nil {
		return KernelPathDebug{
			Path:       p,
			Exists:     false,
			Executable: false,
			Error:      err.Error(),
		}
	}
	// is executable?
	isExec := !fi.IsDir() && (fi.Mode()&0111 != 0)
	return KernelPathDebug{
		Path:       p,
		Exists:     true,
		Executable: isExec,
	}
}
