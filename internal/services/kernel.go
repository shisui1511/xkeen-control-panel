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

// sanitizeKernelPath возвращает очищенный путь, лежащий внутри allowedKernelRoots.
// Файловые операции должны использовать именно возвращённое значение, а не исходное:
// иначе проверка не видна анализаторам потока данных и не считается защитой.
func sanitizeKernelPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	// Reject raw paths containing ".." components to prevent traversal regardless of Clean result.
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return "", errors.New("path traversal detected")
		}
	}
	clean := filepath.Clean(path)
	// Ensure the cleaned path actually starts with one of the allowed roots
	for _, root := range allowedKernelRoots {
		if strings.HasPrefix(clean+"/", root) || strings.HasPrefix(clean, root) {
			return clean, nil
		}
	}
	return "", errors.New("path is outside allowed directories")
}

// canonicalKernelName сводит имя ядра из запроса к литералу: дальше в путях
// используется только константа, а не пришедшая извне строка.
func canonicalKernelName(name string) (string, error) {
	switch name {
	case "xray":
		return "xray", nil
	case "mihomo":
		return "mihomo", nil
	default:
		return "", fmt.Errorf("invalid kernel name: %s", name)
	}
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
	HasBackup      bool   `json:"has_backup"`
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

// clockTicksPerSecond is USER_HZ, 100 on every Linux architecture routers use.
const clockTicksPerSecond = 100

// getProcUptime returns how long a process has been running. The mtime of
// /proc/<pid> is when procfs instantiated the entry, not when the process
// started, so the start time is taken from /proc/<pid>/stat (field 22,
// clock ticks since boot) and compared with /proc/uptime.
func getProcUptime(pidStr string) string {
	d, ok := procAge(pidStr)
	if !ok {
		return ""
	}
	return formatUptimeRu(d)
}

func procAge(pidStr string) (time.Duration, bool) {
	stat, err := os.ReadFile(filepath.Join(procDir, pidStr, "stat"))
	if err != nil {
		return 0, false
	}
	// comm (field 2) may contain spaces and parentheses: fields after the
	// last ')' start at field 3.
	rest := string(stat)
	if i := strings.LastIndexByte(rest, ')'); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest)
	const startTimeIdx = 22 - 3
	if len(fields) <= startTimeIdx {
		return 0, false
	}
	startTicks, err := strconv.ParseFloat(fields[startTimeIdx], 64)
	if err != nil {
		return 0, false
	}
	up, err := os.ReadFile(filepath.Join(procDir, "uptime"))
	if err != nil {
		return 0, false
	}
	upFields := strings.Fields(string(up))
	if len(upFields) == 0 {
		return 0, false
	}
	sysUp, err := strconv.ParseFloat(upFields[0], 64)
	if err != nil {
		return 0, false
	}
	age := sysUp - startTicks/clockTicksPerSecond
	if age < 0 {
		age = 0
	}
	return time.Duration(age * float64(time.Second)), true
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
	dataDir      string

	// statFunc is used to check if a file exists; defaults to os.Stat.
	// Overridable in tests to verify TTL caching without touching the filesystem.
	statFunc func(string) (os.FileInfo, error)

	testClient    *http.Client
	githubAPIBase string
}

// kernelChannelStore is the on-disk format used to persist per-kernel update
// channel selection (SRV channel setting survives xcp restarts/deploys).
type kernelChannelStore struct {
	Channels map[string]string `json:"channels"`
}

func NewKernelService(dataDir string) *KernelService {
	svc := &KernelService{
		kernels:  make(map[string]*KernelInfo),
		statFunc: os.Stat,
		dataDir:  dataDir,
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

	// Restore persisted channel selection (falls back to "stable" defaults above
	// if no store exists yet, e.g. first run or an xcp build predating this feature).
	svc.loadChannels()

	// Detect current versions (outside lock — no concurrent calls yet)
	for _, k := range svc.kernels {
		k.CurrentVersion = svc.detectVersion(k)
	}

	return svc
}

// channelStorePath returns the path to the on-disk kernel channel store,
// creating its parent directory if needed.
func (s *KernelService) channelStorePath() string {
	dir := filepath.Join(s.dataDir, "kernels")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "channels.json")
}

// loadChannels restores previously persisted channel selections. Missing or
// unreadable stores are silently ignored — kernels keep their "stable" default.
func (s *KernelService) loadChannels() {
	data, err := os.ReadFile(s.channelStorePath())
	if err != nil {
		return
	}
	var store kernelChannelStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, channel := range store.Channels {
		if channel != "stable" && channel != "preview" {
			continue
		}
		if k, ok := s.kernels[name]; ok {
			k.Channel = channel
		}
	}
}

// persistChannels writes the given name->channel map to disk atomically.
func (s *KernelService) persistChannels(channels map[string]string) error {
	data, err := json.MarshalIndent(kernelChannelStore{Channels: channels}, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(s.channelStorePath(), data, 0600)
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
		// Других ядер не бывает: путь из имени не собираем, чтобы имя из запроса не попадало в файловые операции
		return
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
		snapshots[i].HasBackup = s.hasBackup(snapshots[i].Name, snapshots[i].BinaryPath)
	}
	return snapshots
}

func (s *KernelService) hasBackup(name, binaryPath string) bool {
	if binaryPath == "" {
		return false
	}
	backupDir := filepath.Join(filepath.Dir(binaryPath), ".backup")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && (strings.HasPrefix(e.Name(), name+".bak.") || strings.HasPrefix(e.Name(), "kernel.bak.")) {
			return true
		}
	}
	return false
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
	snap.HasBackup = s.hasBackup(snap.Name, snap.BinaryPath)
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

// SetChannel switches a kernel's update channel (stable/preview) and persists
// the selection to disk so it survives xcp restarts (T-CH-02). Returns false
// if the channel value is invalid or the kernel is unknown.
func (s *KernelService) SetChannel(name, channel string) bool {
	if channel != "stable" && channel != "preview" {
		return false
	}
	s.mu.Lock()
	k, ok := s.kernels[name]
	if !ok {
		s.mu.Unlock()
		return false
	}
	k.Channel = channel
	channels := make(map[string]string, len(s.kernels))
	for n, kk := range s.kernels {
		channels[n] = kk.Channel
	}
	s.mu.Unlock()

	if err := s.persistChannels(channels); err != nil {
		log.Printf("WARNING: failed to persist kernel channel selection: %v", err)
	}
	return true
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

	// previewReleaseWindow bounds how many recent releases are scanned for a
	// prerelease tag on the "preview" channel. 5 was too narrow: both Xray-core
	// and mihomo can publish several stable point releases between prereleases,
	// which produced a false "up to date" instead of surfacing the real preview.
	const previewReleaseWindow = 30

	apiURL := fmt.Sprintf("%s/repos/%s/releases/latest", githubBase, repo)
	if channel != "stable" {
		apiURL = fmt.Sprintf("%s/repos/%s/releases?per_page=%d", githubBase, repo, previewReleaseWindow)
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

	// On the preview channel, an empty result only means no prerelease tag was
	// found within the scanned window — not that the current build is confirmed
	// up to date. Say so explicitly instead of silently reporting "actual".
	resultMessage := ""
	if channel != "stable" && latestVersion == "" {
		resultMessage = fmt.Sprintf("No prerelease found in the last %d releases of %s", previewReleaseWindow, repo)
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
		kk.Message = resultMessage
	}
	s.mu.Unlock()
	return nil
}

// Install downloads and installs the kernel
func (s *KernelService) Install(name string) error {
	name, err := canonicalKernelName(name)
	if err != nil {
		return err
	}

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
	binaryPath := k.BinaryPath
	latestVersion := k.LatestVersion
	s.mu.Unlock()

	// If latestVersion is unknown, check latest or fallback to current version for reinstall
	if latestVersion == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.CheckLatest(ctx, name)
		cancel()
		s.mu.RLock()
		if s.kernels[name] != nil {
			latestVersion = s.kernels[name].LatestVersion
			if latestVersion == "" && s.kernels[name].CurrentVersion != "" && s.kernels[name].CurrentVersion != "not installed" {
				latestVersion = strings.TrimPrefix(s.kernels[name].CurrentVersion, "v")
				s.kernels[name].LatestVersion = latestVersion
			}
		}
		s.mu.RUnlock()
	}

	arch := kernelAssetArch(runtime.GOARCH)

	// Build a temporary KernelInfo for buildDownloadURL (only needs Name, Repo, LatestVersion, Channel)
	s.mu.RLock()
	snap := *s.kernels[name]
	snap.LatestVersion = latestVersion
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
	backupPath, err := sanitizeKernelPath(filepath.Join(backupDir, backupName))
	if err != nil {
		setStatus("failed", "Invalid backup path: "+err.Error())
		return err
	}

	if _, err := os.Stat(binaryPath); err == nil {
		// Копия с правами оригинала: откат на неё должен дать запускаемое ядро
		if err := copyKernelFile(binaryPath, backupPath); err != nil {
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
	safeExtracted, err := sanitizeKernelPath(extractedPath)
	if err != nil {
		setStatus("failed", "Invalid extracted path: "+err.Error())
		return err
	}
	if err := os.Chmod(safeExtracted, 0755); err != nil {
		setStatus("failed", "Chmod failed: "+err.Error())
		return err
	}

	// Atomic replace
	tempDest, err := sanitizeKernelPath(filepath.Join(filepath.Dir(binaryPath), filepath.Base(binaryPath)+".new"))
	if err != nil {
		setStatus("failed", "Invalid temp dest path: "+err.Error())
		return err
	}
	safeBinaryPath, err := sanitizeKernelPath(binaryPath)
	if err != nil {
		setStatus("failed", "Invalid binary path: "+err.Error())
		return err
	}
	if err := moveKernelFile(safeExtracted, tempDest); err != nil {
		setStatus("failed", "Replace failed: "+err.Error())
		return err
	}
	if err := os.Rename(tempDest, safeBinaryPath); err != nil {
		// Rename в пределах каталога атомарен: при ошибке рабочее ядро не
		// тронуто, откатывать нечего — убрать только недоустановленный файл
		_ = os.Remove(tempDest)
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
		kk.HasBackup = true
	}
	s.mu.Unlock()

	return nil
}

// Rollback restores the kernel binary from the latest backup.
func (s *KernelService) Rollback(name string) error {
	name, err := canonicalKernelName(name)
	if err != nil {
		return err
	}

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
		log.Printf("[Kernel] No backups found with prefix %s.bak., trying legacy format kernel.bak.", utils.SanitizeLogInput(name))
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
	tempDest, err := sanitizeKernelPath(filepath.Join(filepath.Dir(binaryPath), filepath.Base(binaryPath)+".new"))
	if err != nil {
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
		kk.HasBackup = s.hasBackup(name, kk.BinaryPath)
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

// kernelAssetArch переводит GOARCH панели в суффикс архитектуры релизных
// ассетов ядер. Роутеры Keenetic на MIPS не имеют FPU, поэтому для mips/mipsle
// выбираются softfloat-сборки.
func kernelAssetArch(goarch string) string {
	switch goarch {
	case "mipsle":
		return "mipsle-softfloat"
	case "mips":
		return "mips-softfloat"
	}
	return goarch
}

// zipPreferSoftfloat: на MIPS из архива Xray берётся xray_softfloat, если он есть.
var zipPreferSoftfloat = runtime.GOARCH == "mips" || runtime.GOARCH == "mipsle"

func (s *KernelService) buildDownloadURL(k *KernelInfo, arch string) (string, string) {
	version := k.LatestVersion
	if version == "" {
		return "", ""
	}

	switch k.Name {
	case "xray":
		// Xray: Xray-linux-arm64-v8a.zip, Xray-linux-mips32le.zip or Xray-linux-mips32.zip
		// (MIPS-архивы содержат и xray, и xray_softfloat — выбор в extractZip)
		var file string
		switch arch {
		case "arm64":
			file = "Xray-linux-arm64-v8a.zip"
		case "mipsle-softfloat":
			file = "Xray-linux-mips32le.zip"
		case "mips-softfloat":
			file = "Xray-linux-mips32.zip"
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

	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// maxKernelExtractBytes caps the size of decompressed kernel binaries (100 MB).
const maxKernelExtractBytes = 100 * 1024 * 1024

func (s *KernelService) extractZip(zipPath, binaryName string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	var match *zip.File
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		baseName := filepath.Base(f.Name)
		if zipPreferSoftfloat && baseName == binaryName+"_softfloat" {
			match = f
			break
		}
		if match == nil && (f.Name == binaryName || f.Name == binaryName+"-linux-"+runtime.GOARCH ||
			baseName == binaryName || strings.HasPrefix(baseName, binaryName+"-linux-")) {
			match = f
		}
	}

	if match == nil {
		return "", fmt.Errorf("binary not found in archive")
	}

	rc, err := match.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	outPath, err := safeTempPath(binaryName + ".new")
	if err != nil {
		return "", err
	}
	outPath, err = sanitizeKernelPath(outPath)
	if err != nil {
		return "", err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, copyErr := io.Copy(out, io.LimitReader(rc, maxKernelExtractBytes))
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return outPath, nil
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
	outPath, err = sanitizeKernelPath(outPath)
	if err != nil {
		return "", err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, copyErr := io.Copy(out, io.LimitReader(gr, maxKernelExtractBytes))
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return outPath, nil
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

	arch := kernelAssetArch(runtime.GOARCH)

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

func isELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic == [4]byte{0x7f, 'E', 'L', 'F'}
}

func copyKernelFile(src, dst string) error {
	safeSrc, err := sanitizeKernelPath(src)
	if err != nil {
		return fmt.Errorf("invalid src path: %w", err)
	}
	safeDst, err := sanitizeKernelPath(dst)
	if err != nil {
		return fmt.Errorf("invalid dst path: %w", err)
	}

	s, err := os.Open(safeSrc)
	if err != nil {
		return err
	}
	defer s.Close()

	info, err := s.Stat()
	if err != nil {
		return err
	}
	// Права источника (в т.ч. бит исполнения) переносятся на копию: иначе
	// os.Create дал бы 0666 и скопированное ядро не запустилось бы
	d, err := os.OpenFile(safeDst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	if err := d.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	return d.Sync()
}

// moveKernelFile переносит файл: rename, а если источник на другой файловой
// системе (/tmp — tmpfs, /opt — накопитель на роутере) — копирование.
func moveKernelFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyKernelFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// kernelUploadMaxBytes — предел загружаемого ядра или архива с ним.
const kernelUploadMaxBytes = 100 << 20

// UploadBinary saves an uploaded kernel binary or archive (.zip/.gz) to the router,
// validates that it is a valid Linux ELF executable, creates a backup of the current binary,
// and replaces the kernel binary atomically.
func (s *KernelService) UploadBinary(requestedName string, src io.Reader, filename string) error {
	name, err := canonicalKernelName(requestedName)
	if err != nil {
		return err
	}

	s.mu.RLock()
	k, kernelExists := s.kernels[name]
	s.mu.RUnlock()
	if !kernelExists {
		return fmt.Errorf("kernel not found: %s", name)
	}

	mu := &sync.Mutex{}
	actual, _ := s.installLocks.LoadOrStore(name, mu)
	installMu := actual.(*sync.Mutex)
	if !installMu.TryLock() {
		return fmt.Errorf("install already in progress")
	}
	defer installMu.Unlock()

	s.mu.Lock()
	s.resolveBinaryPath(k)
	binaryPath := k.BinaryPath
	s.mu.Unlock()

	// Create temp file for upload streaming
	tempFile, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("upload-%s-*.tmp", name))
	if err != nil {
		return fmt.Errorf("failed to create temp upload file: %w", err)
	}
	tempUploadPath := tempFile.Name()
	defer os.Remove(tempUploadPath)

	// Лимит проверяется явно: молча обрезанный файл с заголовком ELF
	// прошёл бы проверку isELF и встал бы ядром, которое не запустится
	n, err := io.Copy(tempFile, io.LimitReader(src, kernelUploadMaxBytes+1))
	_ = tempFile.Close()
	if err != nil {
		return fmt.Errorf("failed to save upload: %w", err)
	}
	if n > kernelUploadMaxBytes {
		return fmt.Errorf("uploaded file exceeds %d MB", kernelUploadMaxBytes>>20)
	}

	extractedPath := tempUploadPath
	lowerFilename := strings.ToLower(filename)
	if strings.HasSuffix(lowerFilename, ".zip") {
		extracted, err := s.extractZip(tempUploadPath, name)
		if err != nil {
			return fmt.Errorf("extract zip failed: %w", err)
		}
		extractedPath = extracted
		defer os.Remove(extractedPath)
	} else if strings.HasSuffix(lowerFilename, ".gz") {
		extracted, err := s.extractGz(tempUploadPath)
		if err != nil {
			return fmt.Errorf("extract gz failed: %w", err)
		}
		extractedPath = extracted
		defer os.Remove(extractedPath)
	}

	// Validate that extractedPath is a Linux ELF binary
	if !isELF(extractedPath) {
		return fmt.Errorf("uploaded file is not a valid Linux ELF binary")
	}

	// Backup current binary if exists
	backupDir := filepath.Join(filepath.Dir(binaryPath), ".backup")
	_ = os.MkdirAll(backupDir, 0755)
	backupName := fmt.Sprintf("%s.bak.%d", name, time.Now().Unix())
	backupPath, err := sanitizeKernelPath(filepath.Join(backupDir, backupName))
	if err != nil {
		return fmt.Errorf("invalid backup path: %w", err)
	}
	safeBinaryPath, err := sanitizeKernelPath(binaryPath)
	if err != nil {
		return fmt.Errorf("invalid binary path: %w", err)
	}

	if _, err := os.Stat(safeBinaryPath); err == nil {
		if err := copyKernelFile(safeBinaryPath, backupPath); err == nil {
			_ = pruneBackups(backupDir, name+".bak.", 3)
		}
	}

	safeExtracted, err := sanitizeKernelPath(extractedPath)
	if err != nil {
		return fmt.Errorf("invalid extracted path: %w", err)
	}
	if err := os.Chmod(safeExtracted, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replace
	tempDest, err := sanitizeKernelPath(filepath.Join(filepath.Dir(binaryPath), filepath.Base(binaryPath)+".new"))
	if err != nil {
		return err
	}
	if err := moveKernelFile(safeExtracted, tempDest); err != nil {
		return fmt.Errorf("replace failed: %w", err)
	}
	if err := os.Rename(tempDest, safeBinaryPath); err != nil {
		// Рабочее ядро не тронуто (rename атомарен) — убрать только .new
		_ = os.Remove(tempDest)
		return fmt.Errorf("final replace failed: %w", err)
	}

	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.binaryPathCachedAt = time.Time{}
		s.resolveBinaryPath(kk)
		kk.verCache = &versionCache{}
		kk.CurrentVersion = s.detectVersion(kk)
		kk.Status = "done"
		kk.Message = "Installed: " + kk.CurrentVersion
		kk.HasBackup = true
	}
	s.mu.Unlock()

	return nil
}
