package services

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// RestartLogEntry records one service lifecycle event.
type RestartLogEntry struct {
	Timestamp int64  `json:"timestamp"`
	Action    string `json:"action"` // "start", "stop", "restart", "switch_kernel"
	Success   bool   `json:"success"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"` // last 50 lines of combined stdout+stderr
}

// xkeenRestartWindow is the grace window during Restart and SwitchKernel (D-06).
const xkeenRestartWindow = 60 * time.Second

type XKeenService struct {
	// dnsProbe checks that the router resolves names; nil uses 127.0.0.1:53.
	dnsProbe func(ctx context.Context) error

	BinaryPath string
	// InitScript — init-скрипт XKeen; пусто — /opt/etc/init.d/S05xkeen
	InitScript string
	dataDir    string
	logMu      sync.Mutex
	restartLog []RestartLogEntry

	stateMu           sync.Mutex
	intentionalStop   bool
	inRestartUntil    time.Time
	kernelStartedHook func()
	now               func() time.Time
}

func NewXKeenService(binary, dataDir string) *XKeenService {
	svc := &XKeenService{
		BinaryPath: binary,
		dataDir:    dataDir,
		now:        time.Now,
	}
	svc.loadRestartLog()
	return svc
}

// --- Restart log ---

func (s *XKeenService) restartLogPath() string {
	dir := filepath.Join(s.dataDir, "data")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "restart_log.json")
}

func (s *XKeenService) loadRestartLog() {
	if s.dataDir == "" {
		return
	}
	data, err := os.ReadFile(s.restartLogPath())
	if err != nil {
		return
	}
	var entries []RestartLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	s.restartLog = entries
}

func (s *XKeenService) saveRestartLog() {
	if s.dataDir == "" {
		return
	}
	data, err := json.Marshal(s.restartLog)
	if err != nil {
		return
	}
	if err := utils.AtomicWriteFile(s.restartLogPath(), data, 0600); err != nil {
		log.Printf("xkeen: failed to save restart log: %v", err)
	}
}

func (s *XKeenService) RecordAction(action, output string, err error) {
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	entry := RestartLogEntry{
		Timestamp: time.Now().Unix(),
		Action:    action,
		Success:   err == nil,
		ExitCode:  exitCode,
		Output:    lastNLines(output, 50),
	}
	s.logMu.Lock()
	s.restartLog = append(s.restartLog, entry)
	// Keep only last 100 entries
	if len(s.restartLog) > 100 {
		s.restartLog = s.restartLog[len(s.restartLog)-100:]
	}
	s.saveRestartLog()
	s.logMu.Unlock()
}

func (s *XKeenService) GetRestartLog() []RestartLogEntry {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	result := make([]RestartLogEntry, len(s.restartLog))
	copy(result, s.restartLog)
	return result
}

func lastNLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return strings.TrimSpace(s)
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// --- Service control ---

func (s *XKeenService) GetVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.BinaryPath, "-v")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	output := strings.TrimSpace(utils.StripANSI(string(out)))
	if output == "" {
		return "unknown"
	}
	// "  Версия XKeen 2.0 Beta (время сборки: ...)\nЯдро ..."
	// Take only first line, then extract short version after "Версия XKeen "
	firstLine := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	firstLine = strings.TrimPrefix(firstLine, "Версия XKeen ")
	firstLine = strings.TrimPrefix(firstLine, "Версия ")
	if idx := strings.Index(firstLine, " ("); idx != -1 {
		firstLine = firstLine[:idx]
	}
	return strings.TrimSpace(firstLine)
}

func (s *XKeenService) Status() (string, error) {
	out, err := s.runWithTimeout("-status", 5*time.Second)
	output := utils.StripANSI(out)
	if err != nil {
		return output, err
	}
	return strings.TrimSpace(output), nil
}

func (s *XKeenService) Start() (string, error) {
	s.stateMu.Lock()
	s.intentionalStop = false
	hook := s.kernelStartedHook
	s.stateMu.Unlock()

	out, err := s.runWithTimeout("-start", 30*time.Second)
	s.RecordAction("start", out, err)
	if err == nil && hook != nil {
		hook()
	}
	return out, err
}

func (s *XKeenService) Stop() (string, error) {
	s.stateMu.Lock()
	s.intentionalStop = true
	s.stateMu.Unlock()

	out, err := s.runWithTimeout("-stop", 30*time.Second)
	s.RecordAction("stop", out, err)
	return out, err
}

func (s *XKeenService) Restart() (string, error) {
	s.stateMu.Lock()
	s.intentionalStop = false
	s.inRestartUntil = s.now().Add(xkeenRestartWindow)
	s.stateMu.Unlock()

	out, err := s.runWithTimeout("-restart", 45*time.Second)
	s.RecordAction("restart", out, err)
	return out, err
}

func (s *XKeenService) SwitchKernel(name string) (string, error) {
	if name != "xray" && name != "mihomo" {
		return "", fmt.Errorf("invalid kernel: %s", name)
	}

	s.stateMu.Lock()
	s.intentionalStop = false
	s.inRestartUntil = s.now().Add(xkeenRestartWindow)
	hook := s.kernelStartedHook
	s.stateMu.Unlock()

	var out string
	var err error
	if name == "xray" {
		out, err = s.runWithTimeout("-xray", 30*time.Second)
	} else {
		out, err = s.runWithTimeout("-mihomo", 30*time.Second)
	}
	s.RecordAction("switch_kernel:"+name, out, err)
	if err == nil && name == "mihomo" && hook != nil {
		hook()
	}
	return out, err
}

// SetKernelStartedHook configures a callback invoked when a kernel is successfully
// started via Start() or switched to mihomo via SwitchKernel("mihomo") (D-24).
// The hook is invoked outside stateMu to prevent lock inversion.
func (s *XKeenService) SetKernelStartedHook(fn func()) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.kernelStartedHook = fn
}

// IntentionalStop reports whether the kernel was stopped intentionally via panel Stop() (D-01).
// Read by WatchdogService to distinguish planned downtime from crashes.
func (s *XKeenService) IntentionalStop() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.intentionalStop
}

// InRestart reports whether a planned restart window (Restart / SwitchKernel) is currently active (D-06).
// Read by WatchdogService to avoid spurious failure increments during kernel restart.
func (s *XKeenService) InRestart() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return !s.inRestartUntil.IsZero() && s.now().Before(s.inRestartUntil)
}

// ClearIntentionalStop resets the intentional stop flag (D-03, D-07).
// Called by WatchdogService when interception rules are detected during stopped state
// or when the kernel recovers to healthy state.
func (s *XKeenService) ClearIntentionalStop() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.intentionalStop = false
}

// ValidateXrayConfig inspects the Xray config directory for outbound configuration quality.
// It scans 04_outbounds*.json files and warns if no real proxy protocols are found.
// Always returns Valid=true and nil error (Xray has only warnings per design).
func (s *XKeenService) ValidateXrayConfig(configDir string) (PreflightResult, error) {
	realProtocols := map[string]bool{
		"vless":       true,
		"vmess":       true,
		"trojan":      true,
		"shadowsocks": true,
		"socks":       true,
	}

	pattern := filepath.Join(configDir, "04_outbounds*.json")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		// No files found or glob error — treat as no real outbounds.
		return PreflightResult{
			Valid: true,
			Warnings: []PreflightIssue{
				{Code: "no_real_outbounds", Message: "no real proxy outbounds found in Xray config"},
			},
		}, nil
	}

	hasReal := false
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue // skip unreadable files silently
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(data, &obj); err != nil {
			continue // skip unparseable files silently
		}
		outbounds, ok := obj["outbounds"].([]interface{})
		if !ok {
			continue
		}
		for _, item := range outbounds {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			protocol, _ := entry["protocol"].(string)
			if realProtocols[protocol] {
				hasReal = true
				break
			}
		}
		if hasReal {
			break
		}
	}

	var warnings []PreflightIssue
	if !hasReal {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_real_outbounds",
			Message: "no real proxy outbounds found in Xray config",
		})
	}

	return PreflightResult{
		Valid:    true,
		Warnings: warnings,
	}, nil
}

const defaultXKeenInitScript = "/opt/etc/init.d/S05xkeen"

var nameClientRe = regexp.MustCompile(`(?m)^\s*name_client="?(xray|mihomo)"?\s*$`)

// ConfiguredKernel — ядро, которое запускает XKeen (name_client в init-скрипте;
// `xkeen -xray`/`-mihomo` переписывают его). Пусто, если XKeen не настроен.
func (s *XKeenService) ConfiguredKernel() string {
	path := s.InitScript
	if path == "" {
		path = defaultXKeenInitScript
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := nameClientRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

func (s *XKeenService) IsDNSProxyingEnabled() bool {
	data, err := os.ReadFile("/opt/etc/init.d/S05xkeen")
	if err != nil {
		return false
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "proxy_dns") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				if key == "proxy_dns" && val == "on" {
					return true
				}
			}
		}
	}
	return false
}

// ErrDNSRolledBack means DNS redirection was switched on, the router stopped
// resolving names and the change was undone.
var ErrDNSRolledBack = errors.New("dns redirection rolled back: the router could not resolve names")

// dnsProbeWindow is how long a freshly restarted core gets to start
// answering DNS before redirection is rolled back.
var dnsProbeWindow = 20 * time.Second

// SetDNSProbe overrides the resolver health check (tests).
func (s *XKeenService) SetDNSProbe(probe func(ctx context.Context) error) {
	s.stateMu.Lock()
	s.dnsProbe = probe
	s.stateMu.Unlock()
}

// probeRouterDNS resolves a well-known name through the router's own DNS
// on 127.0.0.1:53, i.e. the path LAN clients use.
func probeRouterDNS(ctx context.Context) error {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, "127.0.0.1:53")
		},
	}
	_, err := r.LookupHost(ctx, "www.google.com")
	return err
}

// SetDNSProxying switches XKeen's DNS redirection into the proxy core. When
// switching it on, the router must still resolve names afterwards;
// otherwise redirection is switched off again and ErrDNSRolledBack returned,
// so a broken DNS setup never leaves the whole network without DNS.
func (s *XKeenService) SetDNSProxying(enabled bool) (string, error) {
	arg := "off"
	if enabled {
		arg = "on"
	}
	out, err := s.runWithTimeoutArgs(30*time.Second, "-dns", arg)
	if err != nil {
		s.RecordAction("dns_redirect:"+arg, out, err)
		return out, err
	}
	restartOut, restartErr := s.Restart()
	combinedOut := out + "\n" + restartOut
	if restartErr != nil || !enabled {
		s.RecordAction("dns_redirect:"+arg, combinedOut, restartErr)
		return combinedOut, restartErr
	}

	s.stateMu.Lock()
	probe := s.dnsProbe
	s.stateMu.Unlock()
	if probe == nil {
		probe = probeRouterDNS
	}
	ctx, cancel := context.WithTimeout(context.Background(), dnsProbeWindow)
	defer cancel()
	var probeErr error
	for {
		attempt, stop := context.WithTimeout(ctx, 3*time.Second)
		probeErr = probe(attempt)
		stop()
		if probeErr == nil || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
		if ctx.Err() != nil {
			break
		}
	}
	if probeErr == nil {
		s.RecordAction("dns_redirect:on", combinedOut, nil)
		return combinedOut, nil
	}

	offOut, _ := s.runWithTimeoutArgs(30*time.Second, "-dns", "off")
	backOut, _ := s.Restart()
	combinedOut += "\n" + offOut + "\n" + backOut
	err = fmt.Errorf("%w: %v", ErrDNSRolledBack, probeErr)
	s.RecordAction("dns_redirect:on", combinedOut, err)
	return combinedOut, err
}

func (s *XKeenService) runWithTimeout(action string, timeout time.Duration) (string, error) {
	return s.runWithTimeoutArgs(timeout, action)
}

// Installed сообщает, установлен ли XKeen (бинарник находится в PATH).
func (s *XKeenService) Installed() bool {
	return binaryAvailable(s.BinaryPath)
}

// binaryAvailable resolves a bare name ("xkeen" in config.json) through PATH
// exactly like exec.Command does; os.Stat would look in the working directory.
func binaryAvailable(path string) bool {
	_, err := exec.LookPath(path)
	return err == nil
}

func (s *XKeenService) isLocalhost() bool {
	// 1. If binary cannot be found
	if !binaryAvailable(s.BinaryPath) {
		// If we are running unit tests, only bypass if the path explicitly contains "xkeen-control-panel" or "local_dev"
		if flag.Lookup("test.v") != nil {
			return strings.Contains(s.BinaryPath, "xkeen-control-panel") || strings.Contains(s.BinaryPath, "local_dev")
		}
		return true
	}
	// 2. If binary path contains development directory names
	if strings.Contains(s.BinaryPath, "xkeen-control-panel") || strings.Contains(s.BinaryPath, "local_dev") {
		return true
	}
	return false
}

func (s *XKeenService) runWithTimeoutArgs(timeout time.Duration, args ...string) (string, error) {
	if s.isLocalhost() {
		// For commands starting, stopping, or restarting services, return mock success
		isLifecycleCmd := false
		for _, arg := range args {
			if arg == "-start" || arg == "-stop" || arg == "-restart" || arg == "-xray" || arg == "-mihomo" {
				isLifecycleCmd = true
				break
			}
		}
		if isLifecycleCmd {
			log.Printf("xkeen: bypassing service lifecycle command %v on localhost (Rule #3)", args)
			return fmt.Sprintf("Bypassed service command %q on localhost (Rule #3)", strings.Join(args, " ")), nil
		}
	}

	// INVARIANT: no shell interpreter — exec.Command receives the binary path directly,
	// never via "sh -c", so action cannot trigger shell injection.
	cmd := exec.Command(s.BinaryPath, args...)
	// Вывод идёт через собственный пайп: Wait не ждёт фоновое ядро,
	// унаследовавшее stdout, а ядро не получает SIGPIPE после выхода скрипта
	out, err := attachScriptOutput(cmd)
	if err != nil {
		return "", err
	}

	err = cmd.Start()
	out.started()
	if err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done
		output := utils.StripANSI(out.snapshot(scriptOutputGrace))
		isStart := false
		for _, arg := range args {
			if strings.Contains(arg, "start") || strings.Contains(arg, "restart") {
				isStart = true
				break
			}
		}
		if isStart {
			status, _ := s.Status()
			if IsKernelStatusHealthy(status) {
				return output, nil
			}
		}
		return output, fmt.Errorf("timeout exceeded")
	case err := <-done:
		output := utils.StripANSI(out.snapshot(scriptOutputGrace))
		isStart := false
		for _, arg := range args {
			if strings.Contains(arg, "start") || strings.Contains(arg, "restart") {
				isStart = true
				break
			}
		}
		if err != nil && isStart {
			status, _ := s.Status()
			if IsKernelStatusHealthy(status) {
				return output, nil
			}
		}
		return output, err
	}
}
