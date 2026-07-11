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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var procDir = "/proc"

// allowedKernelRoots are the only directories where kernel binaries and backups may live.
var allowedKernelRoots = []string{
	"/opt/sbin/",
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

var semverRegexp = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([^+]+))?(?:\+(.+))?$`)

func isValidSemver(v string) bool {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	return semverRegexp.MatchString(v)
}

func comparePrerelease(pre1, pre2 string) int {
	parts1 := strings.Split(pre1, ".")
	parts2 := strings.Split(pre2, ".")

	isNumeric := func(s string) bool {
		if s == "" {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}

	minLen := len(parts1)
	if len(parts2) < minLen {
		minLen = len(parts2)
	}

	for i := 0; i < minLen; i++ {
		p1 := parts1[i]
		p2 := parts2[i]

		if p1 == p2 {
			continue
		}

		num1 := isNumeric(p1)
		num2 := isNumeric(p2)

		if num1 && num2 {
			val1, _ := strconv.Atoi(p1)
			val2, _ := strconv.Atoi(p2)
			if val1 != val2 {
				if val1 > val2 {
					return 1
				}
				return -1
			}
		} else if num1 {
			return -1
		} else if num2 {
			return 1
		} else {
			res := strings.Compare(p1, p2)
			if res != 0 {
				return res
			}
		}
	}

	if len(parts1) > len(parts2) {
		return 1
	} else if len(parts1) < len(parts2) {
		return -1
	}
	return 0
}

func compareSemver(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.TrimPrefix(v1, "v"), "V")
	v2 = strings.TrimPrefix(strings.TrimPrefix(v2, "v"), "V")

	m1 := semverRegexp.FindStringSubmatch(v1)
	m2 := semverRegexp.FindStringSubmatch(v2)

	if m1 == nil && m2 == nil {
		return 0
	}
	if m1 == nil {
		return -1
	}
	if m2 == nil {
		return 1
	}

	// Compare major
	major1, _ := strconv.Atoi(m1[1])
	major2, _ := strconv.Atoi(m2[1])
	if major1 != major2 {
		if major1 > major2 {
			return 1
		}
		return -1
	}

	// Compare minor
	minor1, _ := strconv.Atoi(m1[2])
	minor2, _ := strconv.Atoi(m2[2])
	if minor1 != minor2 {
		if minor1 > minor2 {
			return 1
		}
		return -1
	}

	// Compare patch
	patch1, _ := strconv.Atoi(m1[3])
	patch2, _ := strconv.Atoi(m2[3])
	if patch1 != patch2 {
		if patch1 > patch2 {
			return 1
		}
		return -1
	}

	// Compare prerelease
	pre1 := m1[4]
	pre2 := m2[4]

	if pre1 != "" && pre2 == "" {
		return -1
	}
	if pre1 == "" && pre2 != "" {
		return 1
	}
	if pre1 != "" && pre2 != "" {
		return comparePrerelease(pre1, pre2)
	}

	return 0
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
	PID            int    `json:"pid,omitempty"`
	Uptime         string `json:"uptime,omitempty"`
	APIAddr        string `json:"api_addr,omitempty"`

	// binaryPathCachedAt records when BinaryPath was last resolved via auto-detection.
	// Access must be protected by the KernelService mutex.
	binaryPathCachedAt time.Time

	// verCache caches the result of detectVersion for 60 seconds to avoid
	// repeatedly spawning a subprocess on every status poll.
	// Must be a pointer so that copying KernelInfo does not copy the embedded mutex.
	verCache *versionCache
}

func isShortLivedOrHelperProcess(pidStr string) bool {
	cmdlinePath := filepath.Join(procDir, pidStr, "cmdline")
	data, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return true
	}
	args := strings.Split(string(data), "\x00")
	blacklist := map[string]bool{
		"version":   true,
		"-v":        true,
		"-t":        true,
		"-test":     true,
		"--version": true,
		"-version":  true,
		"-h":        true,
		"--help":    true,
	}
	for _, arg := range args {
		if blacklist[strings.TrimSpace(arg)] {
			return true
		}
	}
	return false
}

// kernelProcessStatus detects whether the kernel process is running.
// Method 1: scan procDir/*/exe readlinks for the binary basename.
// Method 2 (fallback): run pidof <basename> if procDir appears empty.
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

	// Method 1: procDir/*/exe readlink (no external tools required)
	matches, _ := filepath.Glob(filepath.Join(procDir, "*/exe"))
	for _, link := range matches {
		target, err := os.Readlink(link)
		if err == nil {
			target = strings.TrimSuffix(target, " (deleted)")
			if filepath.Base(target) == base {
				pidStr := filepath.Base(filepath.Dir(link))
				if isShortLivedOrHelperProcess(pidStr) {
					continue
				}
				return "running"
			}
		}
	}

	// Method 2: pidof fallback when procDir gives no entries
	if len(matches) == 0 {
		out, err := exec.Command("pidof", base).Output()
		if err == nil {
			pids := strings.Fields(strings.TrimSpace(string(out)))
			hasRunning := false
			for _, pidStr := range pids {
				if !isShortLivedOrHelperProcess(pidStr) {
					hasRunning = true
					break
				}
			}
			if hasRunning {
				return "running"
			}
			return "stopped"
		}
		// pidof itself unavailable — cannot determine state
		return "unknown"
	}

	return "stopped"
}

func kernelProcessStatusDetailed(binaryPath string) (status string, pid int, uptime string) {
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "not_installed", 0, ""
	}

	// Check if the binary is accessible (readable/executable)
	f, err := os.Open(binaryPath)
	if err != nil {
		return "not_accessible", 0, ""
	}
	f.Close()

	base := filepath.Base(binaryPath)

	// Method 1: procDir/*/exe readlink (no external tools required)
	matches, _ := filepath.Glob(filepath.Join(procDir, "*/exe"))
	for _, link := range matches {
		target, err := os.Readlink(link)
		if err == nil {
			target = strings.TrimSuffix(target, " (deleted)")
			if filepath.Base(target) == base {
				pidStr := filepath.Base(filepath.Dir(link))
				if isShortLivedOrHelperProcess(pidStr) {
					continue
				}
				if p, err := strconv.Atoi(pidStr); err == nil {
					uptimeStr := getProcUptime(pidStr)
					return "running", p, uptimeStr
				}
				return "running", 0, ""
			}
		}
	}

	// Method 2: pidof fallback when procDir gives no entries
	if len(matches) == 0 {
		out, err := exec.Command("pidof", base).Output()
		if err == nil {
			pids := strings.Fields(strings.TrimSpace(string(out)))
			for _, pidStr := range pids {
				if !isShortLivedOrHelperProcess(pidStr) {
					if p, err := strconv.Atoi(pidStr); err == nil {
						uptimeStr := getProcUptime(pidStr)
						return "running", p, uptimeStr
					}
				}
			}
		}
	}

	return "stopped", 0, ""
}

func getProcUptime(pidStr string) string {
	procPath := filepath.Join(procDir, pidStr)
	st, err := os.Stat(procPath)
	if err != nil {
		return ""
	}
	duration := time.Since(st.ModTime())
	return formatUptimeRu(duration)
}

func formatUptimeRu(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dд %dч %dм", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dч %dм", hours, minutes)
	}
	return fmt.Sprintf("%dм", minutes)
}

// KernelService manages proxy kernels (xray, mihomo)
type KernelService struct {
	kernels      map[string]*KernelInfo
	mu           sync.RWMutex
	installLocks sync.Map // per-kernel install lock; key: string, value: *sync.Mutex

	// statFunc is used to check if a file exists; defaults to os.Stat.
	// Overridable in tests to verify TTL caching without touching the filesystem.
	statFunc func(string) (os.FileInfo, error)

	testClient    *http.Client
	githubAPIBase string
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
		xrayPath = "/opt/sbin/xray" // fallback default for display and install
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
		mihomoPath = "/opt/sbin/mihomo" // fallback default for display and install
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
	order := []string{"xray", "mihomo"}
	snapshots := make([]KernelInfo, 0, len(order))
	for _, name := range order {
		if k, ok := s.kernels[name]; ok {
			snapshots = append(snapshots, *k)
		}
	}
	s.mu.Unlock()

	// Resolve live data outside the global lock to avoid blocking Install/CheckLatest
	for i := range snapshots {
		snapshots[i].CurrentVersion = s.detectVersion(&snapshots[i])
		status, pid, uptime := kernelProcessStatusDetailed(snapshots[i].BinaryPath)
		snapshots[i].ProcessStatus = status
		snapshots[i].PID = pid
		snapshots[i].Uptime = uptime
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
	status, pid, uptime := kernelProcessStatusDetailed(snap.BinaryPath)
	snap.ProcessStatus = status
	snap.PID = pid
	snap.Uptime = uptime
	return &snap
}

func (s *KernelService) GetActiveKernel() string {
	for _, info := range s.List() {
		if info.ProcessStatus == "running" {
			return info.Name
		}
	}
	return ""
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

	githubBase := "https://api.github.com"
	if s.githubAPIBase != "" {
		githubBase = s.githubAPIBase
	}

	apiURL := fmt.Sprintf("%s/repos/%s/releases/latest", githubBase, repo)
	if channel != "stable" {
		apiURL = fmt.Sprintf("%s/repos/%s/releases?per_page=5", githubBase, repo)
	}

	var client *http.Client
	if s.testClient != nil {
		client = s.testClient
	} else {
		client = utils.SafeHTTPClient(15 * time.Second)
	}
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
		if isValidSemver(currentVersion) {
			kk.HasUpdate = latestVersion != "" && compareSemver(latestVersion, currentVersion) > 0
		} else {
			kk.HasUpdate = latestVersion != ""
		}
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
	// Use name and timestamp in backup name to prevent cross-kernel backup collisions
	backupName := fmt.Sprintf("%s.bak.%d", name, time.Now().Unix())
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

	// Prune old backups — keep at most 3 most recent for this kernel
	_ = pruneBackups(backupDir, name+".bak.", 3)

	// Verify new version and update metadata under lock
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		// Reset binary path cache so the next List/Get call re-detects the actual install location
		kk.binaryPathCachedAt = time.Time{}
		// Re-resolve path immediately so we report the correct location
		s.resolveBinaryPath(kk)
		kk.CurrentVersion = s.detectVersion(kk)
		if isValidSemver(kk.CurrentVersion) {
			kk.HasUpdate = latestVersion != "" && compareSemver(latestVersion, kk.CurrentVersion) > 0
		} else {
			kk.HasUpdate = latestVersion != ""
		}
		kk.Status = "done"
		kk.Message = "Updated to " + kk.CurrentVersion
	}
	s.mu.Unlock()

	return nil
}

// Rollback restores the kernel binary from the latest backup.
func (s *KernelService) Rollback(name string) error {
	s.mu.Lock()
	k, ok := s.kernels[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("kernel not found: %s", name)
	}
	s.resolveBinaryPath(k)
	binaryPath := k.BinaryPath
	s.mu.Unlock()

	backupDir := filepath.Join(filepath.Dir(binaryPath), ".backup")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("read backup dir: %w", err)
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), name+".bak.") {
			backups = append(backups, filepath.Join(backupDir, e.Name()))
		}
	}

	// Fallback to legacy format "kernel.bak." only if no new backups exist
	if len(backups) == 0 {
		log.Printf("[Kernel] No backups found with prefix %s.bak., trying legacy format kernel.bak.", name)
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "kernel.bak.") {
				backups = append(backups, filepath.Join(backupDir, e.Name()))
			}
		}
	}

	if len(backups) == 0 {
		return fmt.Errorf("no backup found for kernel %s", name)
	}

	// Latest backup is the last one (since names contain timestamps and os.ReadDir sorts by name)
	latestBackup := backups[len(backups)-1]

	// Atomic replace
	tempDest := filepath.Join(filepath.Dir(binaryPath), filepath.Base(binaryPath)+".new")
	if err := validateKernelPath(tempDest); err != nil {
		return err
	}

	src, err := os.Open(latestBackup)
	if err != nil {
		return fmt.Errorf("open backup file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(tempDest)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy backup: %w", err)
	}

	if err := dst.Close(); err != nil {
		return err
	}
	if err := src.Close(); err != nil {
		return err
	}

	if err := os.Chmod(tempDest, 0755); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := os.Rename(tempDest, binaryPath); err != nil {
		return fmt.Errorf("rename to target path: %w", err)
	}

	// Reset cache under lock
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.binaryPathCachedAt = time.Time{}
		s.resolveBinaryPath(kk)
		kk.verCache = &versionCache{} // clear version cache
		kk.CurrentVersion = s.detectVersion(kk)
		kk.Status = "idle"
		kk.Message = "Rolled back to backup"
	}
	s.mu.Unlock()

	return nil
}

// pruneBackups removes oldest backup files in dir with the given prefix, keeping only the `keep` most recent.
// Files are sorted by name (timestamp suffix ensures lexicographic order = chronological order).
// Errors are logged but do not fail the caller.
func pruneBackups(dir string, prefix string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Filter to backup files only
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
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

// FetchBinary downloads the latest version archive for the given kernel,
// extracts the binary, reads it into memory, and returns the bytes and
// a safe filename. Nothing on the router filesystem is modified.
func (s *KernelService) FetchBinary(name string) ([]byte, string, error) {
	s.mu.RLock()
	k, ok := s.kernels[name]
	if !ok {
		s.mu.RUnlock()
		return nil, "", fmt.Errorf("kernel not found: %s", name)
	}
	if k.LatestVersion == "" {
		s.mu.RUnlock()
		return nil, "", fmt.Errorf("latest version unknown for kernel %s; run check first", name)
	}
	snap := *k
	s.mu.RUnlock()

	arch := runtime.GOARCH
	if arch == "mipsle" || arch == "mipsel" {
		arch = "mipsle-softfloat"
	} else if arch == "mips" {
		arch = "mips-softfloat"
	}

	downloadURL, filename := s.buildDownloadURL(&snap, arch)
	if downloadURL == "" {
		return nil, "", fmt.Errorf("unsupported architecture: %s", arch)
	}

	tempFile, err := safeTempPath(filename)
	if err != nil {
		return nil, "", fmt.Errorf("invalid filename: %w", err)
	}
	defer os.Remove(tempFile)

	if err := s.downloadFile(context.Background(), downloadURL, tempFile); err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
	}

	extractedPath := tempFile
	if strings.HasSuffix(tempFile, ".zip") {
		extracted, err := s.extractZip(tempFile, name)
		if err != nil {
			return nil, "", fmt.Errorf("extract failed: %w", err)
		}
		extractedPath = extracted
		defer os.Remove(extractedPath)
	} else if strings.HasSuffix(tempFile, ".gz") {
		extracted, err := s.extractGz(tempFile)
		if err != nil {
			return nil, "", fmt.Errorf("extract failed: %w", err)
		}
		extractedPath = extracted
		defer os.Remove(extractedPath)
	}

	data, err := os.ReadFile(extractedPath)
	if err != nil {
		return nil, "", fmt.Errorf("read binary failed: %w", err)
	}

	return data, name, nil
}
