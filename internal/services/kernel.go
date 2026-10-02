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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var procDir = "/proc"

// kernelPathRegex — допустимые символы пути ядра: буквы и цифры любого алфавита
// (имена временных каталогов бывают не ASCII), «_», «-», «.», «/», «+».
var kernelPathRegex = regexp.MustCompile(`^[\p{L}\p{N}_\-./+]+$`)

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
	// LatestTag — тег релиза с LatestVersion. У плавающих pre-release (mihomo
	// Prerelease-Alpha) он не совпадает с "v"+версия
	LatestTag string `json:"latest_tag,omitempty"`
	HasUpdate bool   `json:"has_update"`
	// AheadOfLatest — установленная сборка новее последнего stable-релиза
	// (например, pre-release на канале «Стабильный»): обновлений нет, но и
	// «актуально» это не значит.
	AheadOfLatest bool   `json:"ahead_of_latest"`
	HasBackup     bool   `json:"has_backup"`
	Channel       string `json:"channel"` // stable, preview
	Repo          string `json:"repo"`
	Status        string `json:"status"`         // idle, checking, downloading, installing, done, failed
	ProcessStatus string `json:"process_status"` // running, stopped, not_installed, not_accessible, unknown
	Message       string `json:"message"`
	PID           int    `json:"pid,omitempty"`
	Uptime        string `json:"uptime,omitempty"`
	APIAddr       string `json:"api_addr,omitempty"`

	// Stage — этап установки внутри status downloading/installing: starting,
	// downloading, extracting, replacing. Коды, а не текст: подписи переводит фронтенд.
	Stage string `json:"stage,omitempty"`
	// ResultKind и ResultVersion — итог последней операции (установка, откат,
	// загрузка файла): installed, updated, reinstalled, rolled_back, uploaded и
	// версия, которая теперь стоит. Message остаётся английским для логов.
	ResultKind    string `json:"result_kind,omitempty"`
	ResultVersion string `json:"result_version,omitempty"`
	// ErrorKind — код вида ошибки установки при status failed (D-10): по нему
	// фронтенд подбирает перевод. Пусто вне failed и для сбоев без своего кода
	// (скачивание, распаковка, замена) — их показывает текст Message.
	ErrorKind string `json:"error_kind,omitempty"`
	// BackupVersion — версия последнего бэкапа («Откатить на vX»). Читается из
	// имени файла в .backup, без запуска бинарников; пусто, если версия в имени
	// не закодирована.
	BackupVersion string `json:"backup_version,omitempty"`

	// binaryPathCachedAt records when BinaryPath was last resolved via auto-detection.
	// Access must be protected by the KernelService mutex.
	binaryPathCachedAt time.Time

	// latestCheckedAt — когда последний раз запускалась проверка релиза (ручная
	// или автоматическая из списка ядер). Не сериализуется; доступ под s.mu.
	latestCheckedAt time.Time

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
		// Разовая конвертация MRS, которую панель сама запускает (route_matcher):
		// не ядро и не должна давать ложный конфликт.
		"convert-ruleset": true,
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

// Этапы установки ядра (KernelInfo.Stage).
const (
	KernelStageStarting    = "starting"
	KernelStageDownloading = "downloading"
	KernelStageExtracting  = "extracting"
	KernelStageReplacing   = "replacing"
)

// Итоги операций над ядром (KernelInfo.ResultKind).
const (
	KernelResultInstalled   = "installed"
	KernelResultUpdated     = "updated"
	KernelResultReinstalled = "reinstalled"
	KernelResultRolledBack  = "rolled_back"
	KernelResultUploaded    = "uploaded"
)

// Виды ошибки установки (KernelInfo.ErrorKind, D-10).
const (
	// KernelErrorReleaseLookup — проверка релиза не удалась (лимит GitHub, таймаут, блокировка).
	KernelErrorReleaseLookup = "release_lookup_failed"
	// KernelErrorNoRelease — проверка прошла, но релиза для канала нет.
	KernelErrorNoRelease = "no_release"
	// KernelErrorUnsupportedArch — версия известна, а сборки для архитектуры нет.
	KernelErrorUnsupportedArch = "unsupported_arch"
)

// ErrKernelBusy — над ядром уже идёт установка, откат или загрузка файла.
// Текст сохранён: на него смотрят обработчики и тесты.
var ErrKernelBusy = errors.New("install already in progress")

// ErrKernelNotFound — ядра с таким именем нет.
var ErrKernelNotFound = errors.New("kernel not found")

// ErrInvalidChannel — канал обновлений не stable и не preview.
var ErrInvalidChannel = errors.New("invalid channel: must be 'stable' or 'preview'")

// KernelService manages proxy kernels (xray, mihomo)
type KernelService struct {
	kernels      map[string]*KernelInfo
	mu           sync.RWMutex
	installLocks sync.Map // per-kernel install lock; key: string, value: *sync.Mutex
	dataDir      string

	// persistMu сериализует сохранение каналов: снимок каналов снимается внутри
	// него, поэтому запись со старым снимком не затирает более новую.
	// Порядок замков: persistMu → s.mu; обратного порядка в коде нет.
	persistMu sync.Mutex

	// opsActive — число идущих операций над ядрами (установка, откат, загрузка
	// файла) по всем ядрам; читается без замков (Busy). busyKernels — те же
	// операции по именам ядер, под s.mu.
	opsActive   atomic.Int32
	busyKernels map[string]bool

	// statFunc is used to check if a file exists; defaults to os.Stat.
	// Overridable in tests to verify TTL caching without touching the filesystem.
	statFunc func(string) (os.FileInfo, error)

	testClient    *http.Client
	githubAPIBase string

	// downloadFn и installArch подменяют загрузку ассета и архитектуру при
	// установке (только тесты, через SetInstallSource); в продакшене пусты.
	downloadFn  func(ctx context.Context, url, dest string) error
	installArch string

	// mihomoConfigDir — каталог конфигурации Mihomo, создаваемый при установке ядра.
	mihomoConfigDir string

	// stageHook (только тесты) зовётся перед каждой сменой статуса установки, вне s.mu.
	stageHook func(status, stage string)

	// activeMu защищает запасные источники активного ядра (ActiveState).
	// Замки не вкладываются: ActiveState отпускает activeMu до вызова
	// ProcessStates (он берёт s.mu), поэтому порядок activeMu/s.mu не важен.
	activeMu sync.Mutex
	// freshRawFn — свежий снимок `xkeen -status` (текст, свежесть); configuredFn —
	// ядро из name_client init-скрипта. Подключаются SetActiveFallbacks.
	freshRawFn   func() (string, bool)
	configuredFn func() string
	// Кэш ActiveState: activeCache действителен, пока activeFresh и не истёк
	// activeStateTTL. activeGen растёт при сбросе, чтобы расчёт, начатый до
	// сброса, не записал устаревшее состояние в кэш.
	activeCache ActiveKernelState
	activeAt    time.Time
	activeFresh bool
	activeGen   uint64
	// processStatesFn подменяет ProcessStates (только тесты), под s.mu.
	processStatesFn func() []KernelProcessState
}

// SetReleaseSource подменяет источник релизов (базовый URL GitHub API и HTTP-клиент).
// Нужен тестам, чтобы проверка релизов не ходила в реальную сеть; в продакшене
// не вызывается.
func (s *KernelService) SetReleaseSource(apiBase string, client *http.Client) {
	s.mu.Lock()
	s.githubAPIBase = apiBase
	s.testClient = client
	s.mu.Unlock()
}

// SetInstallSource подменяет загрузку ассета и архитектуру установки. Нужен
// тестам: на amd64 у ядер нет ассетов, а сеть в тестах запрещена. Пустой arch
// оставляет архитектуру по умолчанию, nil download — штатную загрузку.
func (s *KernelService) SetInstallSource(arch string, download func(ctx context.Context, url, dest string) error) {
	s.mu.Lock()
	s.installArch = arch
	s.downloadFn = download
	s.mu.Unlock()
}

// SetMihomoConfigDir задаёт каталог конфигурации Mihomo, который создаётся
// пустым после успешной установки ядра (D-11). Пустая строка — не создавать.
func (s *KernelService) SetMihomoConfigDir(dir string) {
	s.mu.Lock()
	s.mihomoConfigDir = dir
	s.mu.Unlock()
}

// ensureMihomoConfigDir создаёт пустой каталог конфигурации Mihomo (0755): у
// свежей установки он должен существовать, но конфиг появляется только из
// конструктора или Редактора, поэтому ничего в него не пишется. Путь идёт через
// sanitizeKernelPath; отказ и ошибка создания логируются и установку не проваливают.
func (s *KernelService) ensureMihomoConfigDir() {
	s.mu.RLock()
	dir := s.mihomoConfigDir
	s.mu.RUnlock()
	if dir == "" {
		return
	}
	clean, err := sanitizeKernelPath(dir)
	if err != nil {
		log.Printf("Kernel: mihomo config dir rejected: %s", utils.SanitizeLogInput(dir))
		return
	}
	_, statErr := os.Stat(clean)
	if statErr == nil {
		return
	}
	if err := os.MkdirAll(clean, 0755); err != nil {
		log.Printf("Kernel: failed to create mihomo config dir %s: %v", utils.SanitizeLogInput(clean), err)
		return
	}
	// umask не должен урезать права нового каталога.
	if err := os.Chmod(clean, 0755); err != nil {
		log.Printf("Kernel: failed to set mode of mihomo config dir %s: %v", utils.SanitizeLogInput(clean), err)
	}
}

// kernelChannelStore is the on-disk format used to persist per-kernel update
// channel selection (SRV channel setting survives xcp restarts/deploys).
type kernelChannelStore struct {
	Channels map[string]string `json:"channels"`
}

func NewKernelService(dataDir string) *KernelService {
	svc := &KernelService{
		kernels:     make(map[string]*KernelInfo),
		busyKernels: make(map[string]bool),
		statFunc:    os.Stat,
		dataDir:     dataDir,
	}

	now := time.Now()

	// Register known kernels with auto-detected binary paths
	xrayPath := findKernelBinary("xray")
	if xrayPath == "" {
		log.Printf("Kernel: Xray binary not found (not installed yet); checked: %s", strings.Join(xrayProbePaths, ", "))
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
		log.Printf("Kernel: Mihomo binary not found (not installed yet); checked: %s", strings.Join(mihomoProbePaths, ", "))
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
		snapshots[i].recomputeUpdateFlags()
		status, pid, uptime := kernelProcessStatusDetailed(snapshots[i].BinaryPath)
		snapshots[i].ProcessStatus = status
		snapshots[i].PID = pid
		snapshots[i].Uptime = uptime
		snapshots[i].fillBackup()
	}
	return snapshots
}

// recomputeUpdateFlags пересчитывает HasUpdate и AheadOfLatest по полям снимка.
// Вызывается после detectVersion: хранимая версия могла остаться нераспознанной
// (error после таймаута при старте), и флаги, посчитанные по ней, устарели.
func (k *KernelInfo) recomputeUpdateFlags() {
	k.HasUpdate = kernelHasUpdate(k.LatestVersion, k.CurrentVersion)
	k.AheadOfLatest = kernelAheadOfLatest(k.Channel, k.LatestVersion, k.CurrentVersion)
}

// fillBackup заполняет HasBackup и BackupVersion по каталогу .backup (без exec).
func (k *KernelInfo) fillBackup() {
	_, version, ok := latestBackup(k.Name, k.BinaryPath)
	k.HasBackup = ok
	k.BackupVersion = version
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
	snap.recomputeUpdateFlags()
	status, pid, uptime := kernelProcessStatusDetailed(snap.BinaryPath)
	snap.ProcessStatus = status
	snap.PID = pid
	snap.Uptime = uptime
	snap.fillBackup()
	return &snap
}

// SetChannel переключает канал обновлений ядра (stable/preview) и сохраняет выбор
// на диск, чтобы он пережил рестарт xcp (T-CH-02). Ошибки: ErrInvalidChannel,
// ErrKernelNotFound, ErrKernelBusy (над ядром идёт установка, откат или загрузка
// файла: URL скачивания строится из канала, смена дала бы сборку другого канала).
func (s *KernelService) SetChannel(name, channel string) error {
	if channel != "stable" && channel != "preview" {
		return ErrInvalidChannel
	}
	s.mu.Lock()
	k, ok := s.kernels[name]
	if !ok {
		s.mu.Unlock()
		return ErrKernelNotFound
	}
	if s.busyLocked(name) {
		s.mu.Unlock()
		return ErrKernelBusy
	}
	k.Channel = channel
	// Latest* принадлежали прежнему каналу: сбрасываем, пока перепроверка
	// нового канала не вернёт результат.
	k.LatestVersion = ""
	k.LatestTag = ""
	k.HasUpdate = false
	k.AheadOfLatest = false
	k.Message = ""
	k.ErrorKind = ""
	k.Status = "checking"
	s.mu.Unlock()

	// Снимок каналов снимается внутри persistMu: иначе поздняя запись со старым
	// снимком затёрла бы выбор, сделанный параллельным SetChannel.
	s.persistMu.Lock()
	s.mu.RLock()
	channels := make(map[string]string, len(s.kernels))
	for n, kk := range s.kernels {
		channels[n] = kk.Channel
	}
	s.mu.RUnlock()
	err := s.persistChannels(channels)
	s.persistMu.Unlock()
	if err != nil {
		log.Printf("WARNING: failed to persist kernel channel selection: %v", err)
	}
	return nil
}

// kernelVersionTimeout — предел ожидания `<ядро> version`. Пакетная переменная:
// тест понижает её, чтобы не ждать секунды.
var kernelVersionTimeout = 5 * time.Second

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

	// Зависший бинарник не должен держать List()/Get() (и мьютекс версии) бесконечно.
	ctx, cancel := context.WithTimeout(context.Background(), kernelVersionTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch k.Name {
	case "xray":
		cmd = exec.CommandContext(ctx, k.BinaryPath, "version")
	case "mihomo":
		cmd = exec.CommandContext(ctx, k.BinaryPath, "-v")
	default:
		return "unknown"
	}
	// Пайп вывода может унаследовать внук процесса: без WaitDelay CombinedOutput
	// ждал бы его закрытия даже после убийства бинарника по таймауту.
	cmd.WaitDelay = time.Second

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

// mihomoAlphaVersionRe — версия alpha-сборки в выводе `mihomo -v`.
var mihomoAlphaVersionRe = regexp.MustCompile(`\b(alpha-[0-9a-f]{7,})\b`)

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
		// Alpha-сборка: Mihomo Meta alpha-f103639 linux arm64 ...
		if m := mihomoAlphaVersionRe.FindStringSubmatch(output); m != nil {
			return m[1]
		}
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

// githubKernelRelease — релиз ядра в ответе GitHub API (канал preview).
type githubKernelRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// mihomoAlphaAssetRe — ассет плавающего pre-release mihomo: mihomo-linux-<arch>-alpha-<sha>.gz
var mihomoAlphaAssetRe = regexp.MustCompile(`^mihomo-linux-[a-z0-9-]+-(alpha-[0-9a-f]+)\.gz$`)

// previewVersion — версия pre-release: semver из тега, а для плавающего тега
// mihomo (Prerelease-Alpha) — alpha-<sha> из имени ассета.
func previewVersion(name string, rel githubKernelRelease) string {
	v := strings.TrimPrefix(rel.TagName, "v")
	if isValidSemver(v) {
		return v
	}
	if name == "mihomo" {
		for _, a := range rel.Assets {
			if m := mihomoAlphaAssetRe.FindStringSubmatch(a.Name); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// isRollingBuild — версия плавающей сборки (alpha-<sha>), не semver.
func isRollingBuild(v string) bool {
	return strings.HasPrefix(v, "alpha-")
}

// kernelHasUpdate — предлагать ли latest вместо current. У плавающей сборки нет
// порядка версий: обновление — любой другой коммит.
func kernelHasUpdate(latest, current string) bool {
	switch {
	case latest == "":
		return false
	case current == "error" || current == "unknown" || current == "":
		// Нераспознанная версия не должна превращаться в предложение обновления:
		// установленная pre-release при таймауте старта выглядела бы «старой» (G2).
		return false
	case isRollingBuild(latest):
		return latest != current
	case isValidSemver(current):
		return compareSemver(latest, current) > 0
	default:
		return true
	}
}

// kernelAheadOfLatest — установленная сборка новее последнего stable-релиза.
// Только для канала stable и только для semver: у плавающих alpha-сборок нет
// порядка версий, а на preview «новее latest» — это просто другая ветка релизов.
func kernelAheadOfLatest(channel, latest, current string) bool {
	return channel == "stable" &&
		latest != "" &&
		!isRollingBuild(latest) &&
		isValidSemver(current) &&
		compareSemver(current, latest) > 0
}

// refreshInstalledVersion перечитывает версию после замены бинарника (установка,
// откат, загрузка) и пересчитывает HasUpdate. Кеш версии сбрасывается: он
// держит версию прежнего бинарника до 60 с, и новое ядро считалось старым —
// «v26.9.9 → v26.9.9, Обновить». Вызывается под s.mu.
func (s *KernelService) refreshInstalledVersion(kk *KernelInfo) {
	kk.verCache = &versionCache{}
	kk.CurrentVersion = s.detectVersion(kk)
	kk.HasUpdate = kernelHasUpdate(kk.LatestVersion, kk.CurrentVersion)
	kk.AheadOfLatest = kernelAheadOfLatest(kk.Channel, kk.LatestVersion, kk.CurrentVersion)
}

// githubReleaseBodyLimit — предел тела ответа GitHub: с запасом для списка 30
// релизов со всеми ассетами (обрезанный JSON дал бы ошибку разбора, а не «актуально»).
const githubReleaseBodyLimit = 16 << 20

// ClaimLatestCheck атомарно занимает автоматическую проверку релиза: true, если
// ядро известно, latest пуст и проверка не запускалась дольше ttl. Метка времени
// ставится сразу, поэтому параллельные и повторные вызовы в пределах ttl не
// порождают лишних запросов к GitHub (лимит анонимного API).
func (s *KernelService) ClaimLatestCheck(name string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.kernels[name]
	if k == nil || k.LatestVersion != "" {
		return false
	}
	if !k.latestCheckedAt.IsZero() && time.Since(k.latestCheckedAt) < ttl {
		return false
	}
	k.latestCheckedAt = time.Now()
	return true
}

// CheckLatest queries GitHub API for latest release.
// ctx is used to cancel the HTTP request (e.g. on service shutdown).
func (s *KernelService) CheckLatest(ctx context.Context, name string) error {
	return s.checkLatest(ctx, name, false)
}

// checkLatest — проверка последнего релиза. При quiet=true Status и Message не
// меняются ни при старте, ни при ошибке, ни при успехе (вызов из установки, где
// статус ведёт сам установщик); Latest*, HasUpdate и AheadOfLatest пишутся как обычно.
func (s *KernelService) checkLatest(ctx context.Context, name string, quiet bool) error {
	s.mu.Lock()
	k := s.kernels[name]
	if k == nil {
		s.mu.Unlock()
		return fmt.Errorf("kernel not found: %s", name)
	}
	k.latestCheckedAt = time.Now()
	// Занятому ядру статус не меняем: им управляет установщик (G5-WR01).
	if !quiet && !s.busyLocked(name) {
		k.Status = "checking"
		k.Message = "Checking for updates..."
		k.ErrorKind = ""
	}
	// Snapshot fields needed for the HTTP call
	repo := k.Repo
	channel := k.Channel
	apiBase := s.githubAPIBase
	testClient := s.testClient
	// Копия записи и указатель кэша версии: свежую версию читаем вне s.mu.
	verSnap := *k
	verCacheAtStart := k.verCache
	s.mu.Unlock()

	// fail фиксирует ошибку проверки в статусе ядра, если канал не сменился
	// (результат устаревшей проверки не должен портить состояние нового канала).
	fail := func(message string, err error) error {
		if quiet {
			return err
		}
		s.mu.Lock()
		if kk := s.kernels[name]; kk != nil && kk.Channel == channel && !s.busyLocked(name) {
			kk.Status = "failed"
			kk.Message = message
			kk.ErrorKind = ""
		}
		s.mu.Unlock()
		return err
	}

	githubBase := "https://api.github.com"
	if apiBase != "" {
		githubBase = apiBase
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
	if testClient != nil {
		client = testClient
	} else {
		client = utils.SafeHTTPClient(15 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fail("Request error: "+err.Error(), err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail("GitHub API error: "+err.Error(), err)
	}
	defer resp.Body.Close()

	// Лимит анонимного API (403), 404 и прочие статусы — ошибка проверки, а не «актуально».
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Sprintf("GitHub API HTTP %d", resp.StatusCode), fmt.Errorf("github api: HTTP %d", resp.StatusCode))
	}
	body := io.LimitReader(resp.Body, githubReleaseBodyLimit)

	var latestVersion, latestTag string
	if channel == "stable" {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(body).Decode(&release); err != nil {
			return fail("Parse error: "+err.Error(), err)
		}
		if release.TagName == "" {
			return fail("GitHub API: empty release tag", errors.New("github api: empty release tag"))
		}
		latestVersion = strings.TrimPrefix(release.TagName, "v")
		latestTag = release.TagName
	} else {
		var releases []githubKernelRelease
		if err := json.NewDecoder(body).Decode(&releases); err != nil {
			return fail("Parse error: "+err.Error(), err)
		}
		for _, rel := range releases {
			if channel == "preview" && rel.Prerelease {
				if v := previewVersion(name, rel); v != "" {
					latestVersion, latestTag = v, rel.TagName
					break
				}
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

	// Свежая версия бинарника вне s.mu: хранимая могла остаться нераспознанной
	// (error после таймаута при старте), и флаги по ней ложны (G2).
	freshVersion := s.detectVersion(&verSnap)

	s.mu.Lock()
	// Канал сменился, пока шёл запрос: результат относится к прежнему каналу.
	if kk := s.kernels[name]; kk != nil && kk.Channel == channel {
		// Указатель verCache меняет только refreshInstalledVersion (установка,
		// откат, загрузка): совпадение значит «бинарник не менялся за время
		// проверки», и прочитанная до замены версия не затрёт новую.
		if knownKernelVersion(freshVersion) && kk.verCache == verCacheAtStart {
			kk.CurrentVersion = freshVersion
		}
		kk.LatestVersion = latestVersion
		kk.LatestTag = latestTag
		kk.HasUpdate = kernelHasUpdate(latestVersion, kk.CurrentVersion)
		kk.AheadOfLatest = kernelAheadOfLatest(channel, latestVersion, kk.CurrentVersion)
		if !quiet && !s.busyLocked(name) {
			kk.Status = "idle"
			kk.Message = resultMessage
			kk.ErrorKind = ""
		}
	}
	s.mu.Unlock()
	return nil
}

// lockKernel берёт замок операций над ядром (установка, откат, загрузка файла).
// Занят — ErrKernelBusy. Единственный вход в замок: проверка «идёт ли операция»
// отдельно от захвата давала гонку. Возвращает release — идемпотентную функцию
// снятия замка (sync.Once): повторный вызов не уводит счётчик Busy() ниже нуля.
//
// Порядок замков: мьютекс ядра берётся через TryLock до s.mu и отпускается после
// снятия отметки под s.mu, поэтому взаимоблокировки между ними нет.
func (s *KernelService) lockKernel(name string) (release func(), err error) {
	actual, _ := s.installLocks.LoadOrStore(name, &sync.Mutex{})
	installMu := actual.(*sync.Mutex)
	if !installMu.TryLock() {
		return nil, ErrKernelBusy
	}

	s.mu.Lock()
	s.busyKernels[name] = true
	s.mu.Unlock()
	s.opsActive.Add(1)

	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.busyKernels, name)
			s.mu.Unlock()
			s.opsActive.Add(-1)
			installMu.Unlock()
		})
	}, nil
}

// Busy сообщает, идёт ли хоть одна операция над ядрами (установка, откат,
// загрузка файла). Без замков: вызывается сторожевым таймером на каждой проверке.
func (s *KernelService) Busy() bool {
	return s.opsActive.Load() > 0
}

// KernelBusy сообщает, идёт ли операция над конкретным ядром.
func (s *KernelService) KernelBusy(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.busyLocked(name)
}

// busyLocked — то же, что KernelBusy, для вызывающих, уже держащих s.mu.
func (s *KernelService) busyLocked(name string) bool {
	return s.busyKernels[name]
}

// setStage обновляет статус, этап и сообщение ядра под замком s.mu.
func (s *KernelService) setStage(name, status, stage, message string) {
	s.notifyStage(status, stage)
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.Status = status
		kk.Stage = stage
		kk.Message = message
		kk.ErrorKind = ""
	}
	s.mu.Unlock()
}

// setFailed переводит ядро в failed с сообщением и видом ошибки (пустой вид —
// сбой без собственного кода, см. KernelInfo.ErrorKind).
func (s *KernelService) setFailed(name, message, kind string) {
	s.notifyStage("failed", "")
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.Status = "failed"
		kk.Stage = ""
		kk.Message = message
		kk.ErrorKind = kind
	}
	s.mu.Unlock()
}

// notifyStage сообщает тестовому хуку о предстоящей смене статуса (вне s.mu).
func (s *KernelService) notifyStage(status, stage string) {
	s.mu.RLock()
	hook := s.stageHook
	s.mu.RUnlock()
	if hook != nil {
		hook(status, stage)
	}
}

// beginInstallLocked синхронно переводит ядро в «Старт…». Вызывается с уже
// взятым замком ядра: ответ клиенту уходит только после этого, поэтому первый же
// опрос статуса видит переходное состояние, а не прежний idle.
func (s *KernelService) beginInstallLocked(name string) error {
	s.notifyStage("downloading", KernelStageStarting)
	s.mu.Lock()
	defer s.mu.Unlock()
	kk := s.kernels[name]
	if kk == nil {
		return fmt.Errorf("kernel not found: %s", name)
	}
	kk.Status = "downloading"
	kk.Stage = KernelStageStarting
	kk.ResultKind = ""
	kk.ResultVersion = ""
	kk.ErrorKind = ""
	kk.Message = "Starting..."
	return nil
}

// BeginInstall запускает установку ядра в фоне. Замок берётся и статус
// downloading/starting выставляется синхронно, до возврата: при занятом замке —
// ErrKernelBusy. onDone (если задан) вызывается по завершении с итоговой ошибкой.
func (s *KernelService) BeginInstall(name string, onDone func(error)) error {
	name, err := canonicalKernelName(name)
	if err != nil {
		return err
	}
	s.mu.RLock()
	_, kernelExists := s.kernels[name]
	s.mu.RUnlock()
	if !kernelExists {
		return fmt.Errorf("kernel not found: %s", name)
	}

	release, err := s.lockKernel(name)
	if err != nil {
		return err
	}
	if err := s.beginInstallLocked(name); err != nil {
		release()
		return err
	}

	go func() {
		defer release()
		err := s.runInstall(name)
		if onDone != nil {
			onDone(err)
		}
	}()
	return nil
}

// Install — синхронная установка ядра (BeginInstall без фона).
func (s *KernelService) Install(name string) error {
	name, err := canonicalKernelName(name)
	if err != nil {
		return err
	}
	s.mu.RLock()
	_, kernelExists := s.kernels[name]
	s.mu.RUnlock()
	if !kernelExists {
		return fmt.Errorf("kernel not found: %s", name)
	}

	release, err := s.lockKernel(name)
	if err != nil {
		return err
	}
	defer release()
	if err := s.beginInstallLocked(name); err != nil {
		return err
	}
	return s.runInstall(name)
}

// knownKernelVersion — версия, которую удалось определить (не служебная строка).
func knownKernelVersion(v string) bool {
	switch v {
	case "", "error", "unknown", "not installed":
		return false
	}
	return true
}

// runInstall — исполняющая часть установки; вызывается с уже взятым замком ядра.
// Порядок: скачивание → распаковка → бэкап → замена. Проверка релиза внутри —
// тихая (checkLatest quiet): она не возвращает статус в idle посреди скачивания.
func (s *KernelService) runInstall(name string) error {
	fail := func(message string, err error) error {
		s.setFailed(name, message, "")
		return err
	}
	// failKind — сбой с кодом вида: фронтенд переводит его по error_kind.
	failKind := func(message, kind string, err error) error {
		s.setFailed(name, message, kind)
		return err
	}

	s.setStage(name, "downloading", KernelStageDownloading, "Downloading...")

	s.mu.Lock()
	k := s.kernels[name]
	if k == nil {
		s.mu.Unlock()
		return fmt.Errorf("kernel not found: %s", name)
	}
	s.resolveBinaryPath(k)
	binaryPath := k.BinaryPath
	latestVersion := k.LatestVersion
	arch := s.installArch
	download := s.downloadFn
	s.mu.Unlock()

	// If latestVersion is unknown, check latest or fallback to current version for reinstall
	var checkErr error
	if latestVersion == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		checkErr = s.checkLatest(ctx, name, true)
		cancel()
		s.mu.Lock()
		if kk := s.kernels[name]; kk != nil {
			latestVersion = kk.LatestVersion
			if latestVersion == "" && knownKernelVersion(kk.CurrentVersion) {
				latestVersion = strings.TrimPrefix(kk.CurrentVersion, "v")
				kk.LatestVersion = latestVersion
			}
		}
		s.mu.Unlock()
	}
	// Версии нет: причина — сбой проверки релиза или отсутствие релиза канала,
	// а не архитектура (G5-WR02).
	if latestVersion == "" {
		s.mu.RLock()
		channel := s.kernels[name].Channel
		s.mu.RUnlock()
		if checkErr != nil {
			return failKind("Release lookup failed: "+checkErr.Error(), KernelErrorReleaseLookup, checkErr)
		}
		return failKind("No release found for channel "+channel, KernelErrorNoRelease,
			fmt.Errorf("no release found for channel %s", channel))
	}

	if arch == "" {
		arch = kernelAssetArch(runtime.GOARCH)
	}
	if download == nil {
		download = s.downloadFile
	}

	// Build a temporary KernelInfo for buildDownloadURL (only needs Name, Repo, LatestVersion, Channel)
	s.mu.RLock()
	snap := *s.kernels[name]
	snap.LatestVersion = latestVersion
	s.mu.RUnlock()

	downloadURL, filename := s.buildDownloadURL(&snap, arch)
	if downloadURL == "" {
		return failKind("Unsupported architecture: "+arch, KernelErrorUnsupportedArch, fmt.Errorf("unsupported architecture: %s", arch))
	}

	tempFile, err := safeTempPath(filename)
	if err != nil {
		return fail("Invalid filename: "+err.Error(), err)
	}
	defer os.Remove(tempFile) // Cleanup archive after extraction

	if err := download(context.Background(), downloadURL, tempFile); err != nil {
		return fail("Download failed: "+err.Error(), err)
	}

	// Extract if needed
	extractedPath := tempFile
	if strings.HasSuffix(tempFile, ".zip") {
		s.setStage(name, "installing", KernelStageExtracting, "Extracting...")
		extracted, err := s.extractZip(tempFile, name)
		if err != nil {
			return fail("Extract failed: "+err.Error(), err)
		}
		extractedPath = extracted
	} else if strings.HasSuffix(tempFile, ".gz") {
		s.setStage(name, "installing", KernelStageExtracting, "Extracting...")
		extracted, err := s.extractGz(tempFile)
		if err != nil {
			return fail("Extract failed: "+err.Error(), err)
		}
		extractedPath = extracted
	}

	// Ensure extracted file is cleaned up if rename fails or it's not moved
	if extractedPath != tempFile {
		defer os.Remove(extractedPath)
	}

	s.setStage(name, "installing", KernelStageReplacing, "Replacing...")

	safeBinaryPath, err := sanitizeKernelPath(binaryPath)
	if err != nil {
		return fail("Invalid binary path: "+err.Error(), err)
	}

	// Итог считается до замены: был ли бинарник и какой версии.
	hadBinary, prevVersion := s.inspectInstalled(name, safeBinaryPath)

	// Бэкап прежнего бинарника. Не удался — замены нет: без копии откат невозможен.
	if hadBinary {
		if err := backupKernelBinary(name, safeBinaryPath, prevVersion); err != nil {
			return fail("Backup failed: "+err.Error(), err)
		}
	}

	// Make executable and replace
	safeExtracted, err := sanitizeKernelPath(extractedPath)
	if err != nil {
		return fail("Invalid extracted path: "+err.Error(), err)
	}
	if err := os.Chmod(safeExtracted, 0755); err != nil {
		return fail("Chmod failed: "+err.Error(), err)
	}

	// Atomic replace
	tempDest, err := sanitizeKernelPath(filepath.Join(filepath.Dir(safeBinaryPath), filepath.Base(safeBinaryPath)+".new"))
	if err != nil {
		return fail("Invalid temp dest path: "+err.Error(), err)
	}
	if err := moveKernelFile(safeExtracted, tempDest); err != nil {
		return fail("Replace failed: "+err.Error(), err)
	}
	if err := os.Rename(tempDest, safeBinaryPath); err != nil {
		// Rename в пределах каталога атомарен: при ошибке рабочее ядро не
		// тронуто, откатывать нечего — убрать только недоустановленный файл
		_ = os.Remove(tempDest)
		return fail("Replace failed: "+err.Error(), err)
	}

	if name == "mihomo" {
		s.ensureMihomoConfigDir()
	}

	// Verify new version and update metadata under lock
	s.notifyStage("done", "")
	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		// Reset binary path cache so the next List/Get call re-detects the actual install location
		kk.binaryPathCachedAt = time.Time{}
		// Re-resolve path immediately so we report the correct location
		s.resolveBinaryPath(kk)
		s.refreshInstalledVersion(kk)
		var kind, message string
		switch {
		case !hadBinary:
			kind, message = KernelResultInstalled, "Installed "+kk.CurrentVersion
		case knownKernelVersion(prevVersion) && prevVersion == kk.CurrentVersion:
			kind, message = KernelResultReinstalled, "Reinstalled "+kk.CurrentVersion
		default:
			kind, message = KernelResultUpdated, "Updated to "+kk.CurrentVersion
		}
		kk.Status = "done"
		kk.Stage = ""
		kk.ResultKind = kind
		kk.ResultVersion = kk.CurrentVersion
		kk.ErrorKind = ""
		kk.Message = message
	}
	s.mu.Unlock()

	return nil
}

// backupsKept — сколько последних бэкапов хранится на ядро.
const backupsKept = 3

// backupVersionMaxLen — предел длины версии в имени бэкапа.
const backupVersionMaxLen = 64

// backupVersionRe — допустимая версия в имени бэкапа: semver или alpha-<hex>,
// только символы [0-9A-Za-z.-]. Имя файла попадает в путь, поэтому всё прочее
// («error», «unknown», обход пути, произвольный текст) в него не пускается.
var backupVersionRe = regexp.MustCompile(`^(?:\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?|alpha-[0-9a-f]+)$`)

// validBackupVersion возвращает версию, если её можно записать в имя бэкапа, иначе "".
func validBackupVersion(v string) string {
	if len(v) > backupVersionMaxLen || !backupVersionRe.MatchString(v) {
		return ""
	}
	return v
}

// backupFileName — имя бэкапа `<ядро>.bak.<unix>[.<версия>]`. Версия — после
// десятизначной метки, поэтому порядок по метке не зависит от неё.
func backupFileName(kernel string, ts int64, version string) string {
	name := fmt.Sprintf("%s.bak.%d", kernel, ts)
	if v := validBackupVersion(version); v != "" {
		name += "." + v
	}
	return name
}

// parseBackupName разбирает имя бэкапа. Невалидная версия отбрасывается (файл
// остаётся бэкапом без версии); чужое ядро и нечисловая метка — ok=false.
func parseBackupName(fileName, kernel string) (ts int64, version string, ok bool) {
	prefix := kernel + ".bak."
	if !strings.HasPrefix(fileName, prefix) {
		return 0, "", false
	}
	tsPart, verPart := fileName[len(prefix):], ""
	if i := strings.IndexByte(tsPart, '.'); i >= 0 {
		tsPart, verPart = tsPart[:i], tsPart[i+1:]
	}
	if tsPart == "" || len(tsPart) > 18 {
		return 0, "", false
	}
	for _, r := range tsPart {
		if r < '0' || r > '9' {
			return 0, "", false
		}
	}
	ts, err := strconv.ParseInt(tsPart, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return ts, validBackupVersion(verPart), true
}

// kernelBackup — бэкап ядра в каталоге .backup рядом с бинарником.
type kernelBackup struct {
	path    string
	version string
	ts      int64
}

// kernelBackups читает каталог .backup рядом с binaryPath, без exec. Учитываются
// только обычные файлы с корректно разобранным именем этого ядра. Порядок —
// от старых к новым (по метке, при равенстве — по имени).
func kernelBackups(name, binaryPath string) []kernelBackup {
	if binaryPath == "" {
		return nil
	}
	dir, err := sanitizeKernelPath(filepath.Join(filepath.Dir(binaryPath), ".backup"))
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var backups []kernelBackup
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		ts, version, ok := parseBackupName(e.Name(), name)
		if !ok {
			continue
		}
		path, err := sanitizeKernelPath(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		backups = append(backups, kernelBackup{path: path, version: version, ts: ts})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].ts != backups[j].ts {
			return backups[i].ts < backups[j].ts
		}
		return backups[i].path < backups[j].path
	})
	return backups
}

// latestBackupEntry — самый новый бэкап ядра.
func latestBackupEntry(name, binaryPath string) (kernelBackup, bool) {
	backups := kernelBackups(name, binaryPath)
	if len(backups) == 0 {
		return kernelBackup{}, false
	}
	return backups[len(backups)-1], true
}

// latestBackup — путь и версия последнего бэкапа ядра (версия может быть пустой).
func latestBackup(name, binaryPath string) (path, version string, ok bool) {
	b, ok := latestBackupEntry(name, binaryPath)
	return b.path, b.version, ok
}

// inspectInstalled определяет, стоит ли бинарник ядра, и его версию. Версия — свежим
// запуском, в обход кеша: кеш мог пережить замену файла извне.
func (s *KernelService) inspectInstalled(name, safeBinaryPath string) (bool, string) {
	if _, err := os.Stat(safeBinaryPath); err != nil {
		return false, ""
	}
	s.mu.RLock()
	var probe KernelInfo
	if k := s.kernels[name]; k != nil {
		probe = *k
	}
	s.mu.RUnlock()
	probe.Name = name
	probe.BinaryPath = safeBinaryPath
	probe.verCache = &versionCache{}
	return true, s.detectVersion(&probe)
}

// backupKernelBinary копирует действующий бинарник в .backup рядом с ним: имя
// несёт версию (D-12), права копии — как у оригинала. Копия не создаётся, если
// последний бэкап уже той же известной версии. Хранятся backupsKept последних.
// Ошибка означает, что бинарник заменять нельзя. Вызывается под замком ядра.
func backupKernelBinary(name, safeBinaryPath, prevVersion string) error {
	version := validBackupVersion(prevVersion)
	last, hasLast := latestBackupEntry(name, safeBinaryPath)
	if hasLast && version != "" && last.version == version {
		return nil
	}
	backupDir, err := sanitizeKernelPath(filepath.Join(filepath.Dir(safeBinaryPath), ".backup"))
	if err != nil {
		return fmt.Errorf("invalid backup dir: %w", err)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("backup dir: %w", err)
	}
	// Метка строго растёт: две замены в одну секунду не должны смешать порядок.
	ts := time.Now().Unix()
	if hasLast && ts <= last.ts {
		ts = last.ts + 1
	}
	backupPath, err := sanitizeKernelPath(filepath.Join(backupDir, backupFileName(name, ts, version)))
	if err != nil {
		return fmt.Errorf("invalid backup path: %w", err)
	}
	if err := copyKernelFile(safeBinaryPath, backupPath); err != nil {
		return err
	}
	pruneBackups(name, safeBinaryPath, backupsKept)
	return nil
}

// Rollback восстанавливает бинарник из последнего бэкапа. Берёт тот же замок, что
// установка и загрузка файла (занят — ErrKernelBusy). Применённый бэкап
// расходуется: следующий откат идёт к более старой копии.
func (s *KernelService) Rollback(name string) error {
	name, err := canonicalKernelName(name)
	if err != nil {
		return err
	}
	s.mu.RLock()
	_, kernelExists := s.kernels[name]
	s.mu.RUnlock()
	if !kernelExists {
		return fmt.Errorf("kernel not found: %s", name)
	}

	// lockKernel делает TryLock(): при идущей установке — сразу ErrKernelBusy, без ожидания.
	release, err := s.lockKernel(name)
	if err != nil {
		return err
	}
	defer release()

	s.mu.Lock()
	k := s.kernels[name]
	s.resolveBinaryPath(k)
	binaryPath := k.BinaryPath
	s.mu.Unlock()

	backup, ok := latestBackupEntry(name, binaryPath)
	if !ok {
		return fmt.Errorf("no backup found for kernel %s", name)
	}

	safeBinaryPath, err := sanitizeKernelPath(binaryPath)
	if err != nil {
		return fmt.Errorf("invalid binary path: %w", err)
	}
	// Atomic replace
	tempDest, err := sanitizeKernelPath(filepath.Join(filepath.Dir(safeBinaryPath), filepath.Base(safeBinaryPath)+".new"))
	if err != nil {
		return err
	}
	if err := copyKernelFile(backup.path, tempDest); err != nil {
		_ = os.Remove(tempDest)
		return fmt.Errorf("copy backup: %w", err)
	}
	if err := os.Chmod(tempDest, 0755); err != nil {
		_ = os.Remove(tempDest)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tempDest, safeBinaryPath); err != nil {
		_ = os.Remove(tempDest)
		return fmt.Errorf("rename to target path: %w", err)
	}
	// Применённая копия израсходована: повторный откат не вернёт ту же версию.
	if err := os.Remove(backup.path); err != nil {
		log.Printf("Kernel: failed to remove applied backup %s: %v", utils.SanitizeLogInput(backup.path), err)
	}

	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.binaryPathCachedAt = time.Time{}
		s.resolveBinaryPath(kk)
		s.refreshInstalledVersion(kk)
		kk.Status = "done"
		kk.Stage = ""
		kk.ResultKind = KernelResultRolledBack
		kk.ResultVersion = kk.CurrentVersion
		kk.ErrorKind = ""
		kk.Message = "Rolled back to " + kk.CurrentVersion
	}
	s.mu.Unlock()

	return nil
}

// pruneBackups удаляет самые старые бэкапы ядра, оставляя keep последних. Порядок
// — по метке из имени, а не по строке имени: версия в имени его не определяет.
// Ошибки удаления логируются и не проваливают вызывающего.
func pruneBackups(name, binaryPath string, keep int) {
	backups := kernelBackups(name, binaryPath)
	if len(backups) <= keep {
		return
	}
	for _, old := range backups[:len(backups)-keep] {
		if err := os.Remove(old.path); err != nil {
			log.Printf("pruneBackups: failed to remove %s: %v", utils.SanitizeLogInput(old.path), err)
		}
	}
}

// kernelAssetArch переводит GOARCH панели в суффикс архитектуры релизных
// ассетов ядер. Роутеры Keenetic/Netcraze на MIPS не имеют FPU, поэтому для mips/mipsle
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

	// Тег релиза: у плавающих pre-release он свой, иначе "v"+версия
	tag := k.LatestTag
	if tag == "" {
		tag = "v" + version
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
		return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", k.Repo, tag, file), file

	case "mihomo":
		// Mihomo: mihomo-linux-arm64-v1.18.0.gz or mihomo-linux-mipsle-softfloat-v1.18.0.gz
		var file string
		switch arch {
		case "arm64", "mipsle-softfloat", "mips-softfloat":
			if isRollingBuild(version) {
				// Alpha: mihomo-linux-arm64-alpha-f103639.gz
				file = fmt.Sprintf("mihomo-linux-%s-%s.gz", arch, version)
			} else {
				file = fmt.Sprintf("mihomo-linux-%s-v%s.gz", arch, version)
			}
		default:
			return "", ""
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", k.Repo, tag, file), file
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

func copyKernelFile(src, dst string) (err error) {
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
	// Те же проверки, что и в sanitizeKernelPath, повторены на месте записи:
	// статический анализатор не переносит гарантию через границу функции, а
	// запись — самая опасная операция. Порядок: «..», набор символов, корень.
	if strings.Contains(safeDst, "..") || !kernelPathRegex.MatchString(safeDst) {
		return fmt.Errorf("invalid dst path: %s", safeDst)
	}
	if !strings.HasPrefix(safeDst, "/opt/sbin/") && !strings.HasPrefix(safeDst, "/opt/bin/") &&
		!strings.HasPrefix(safeDst, "/opt/etc/") && !strings.HasPrefix(safeDst, os.TempDir()+"/") {
		return fmt.Errorf("invalid dst path: %s is outside allowed directories", safeDst)
	}
	// Права источника (в т.ч. бит исполнения) переносятся на копию: иначе
	// os.Create дал бы 0666 и скопированное ядро не запустилось бы
	d, err := os.OpenFile(safeDst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	// Ошибка Close записываемого файла означает недописанную копию ядра —
	// её нельзя терять, иначе moveKernelFile удалит исходник
	defer func() {
		if cerr := d.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

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

	release, err := s.lockKernel(name)
	if err != nil {
		return err
	}
	defer release()

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

	safeBinaryPath, err := sanitizeKernelPath(binaryPath)
	if err != nil {
		return fmt.Errorf("invalid binary path: %w", err)
	}

	// Backup current binary if exists. Ошибка копирования прерывает замену: без
	// копии откат невозможен (так же, как при установке).
	hadBinary, prevVersion := s.inspectInstalled(name, safeBinaryPath)
	if hadBinary {
		if err := backupKernelBinary(name, safeBinaryPath, prevVersion); err != nil {
			return fmt.Errorf("backup failed: %w", err)
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

	if name == "mihomo" {
		s.ensureMihomoConfigDir()
	}

	s.mu.Lock()
	if kk := s.kernels[name]; kk != nil {
		kk.binaryPathCachedAt = time.Time{}
		s.resolveBinaryPath(kk)
		s.refreshInstalledVersion(kk)
		kk.Status = "done"
		kk.Stage = ""
		kk.ResultKind = KernelResultUploaded
		kk.ResultVersion = kk.CurrentVersion
		kk.ErrorKind = ""
		kk.Message = "Uploaded " + kk.CurrentVersion
	}
	s.mu.Unlock()

	return nil
}
