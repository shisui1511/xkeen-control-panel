package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

type XKeenService struct {
	BinaryPath string
	dataDir    string
	logMu      sync.Mutex
	restartLog []RestartLogEntry
}

func NewXKeenService(binary, dataDir string) *XKeenService {
	svc := &XKeenService{BinaryPath: binary, dataDir: dataDir}
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
	cmd := exec.Command(s.BinaryPath, "-status")
	out, err := cmd.CombinedOutput()
	output := utils.StripANSI(string(out))
	if err != nil {
		return output, err
	}
	return strings.TrimSpace(output), nil
}

func (s *XKeenService) Start() (string, error) {
	out, err := s.runWithTimeout("-start", 30*time.Second)
	s.RecordAction("start", out, err)
	return out, err
}

func (s *XKeenService) Stop() (string, error) {
	out, err := s.runWithTimeout("-stop", 30*time.Second)
	s.RecordAction("stop", out, err)
	return out, err
}

func (s *XKeenService) Restart() (string, error) {
	out, err := s.runWithTimeout("-restart", 45*time.Second)
	s.RecordAction("restart", out, err)
	return out, err
}

func (s *XKeenService) SwitchKernel(name string) (string, error) {
	var out string
	var err error
	if name == "xray" {
		out, err = s.runWithTimeout("-xray", 30*time.Second)
	} else if name == "mihomo" {
		out, err = s.runWithTimeout("-mihomo", 30*time.Second)
	} else {
		return "", fmt.Errorf("invalid kernel: %s", name)
	}
	s.RecordAction("switch_kernel:"+name, out, err)
	return out, err
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

func (s *XKeenService) runWithTimeout(action string, timeout time.Duration) (string, error) {
	// INVARIANT: no shell interpreter — exec.Command receives the binary path directly,
	// never via "sh -c", so action cannot trigger shell injection.
	cmd := exec.Command(s.BinaryPath, action)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Start()
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
		output := utils.StripANSI(out.String())
		// If it was a start/restart, check if it actually started despite the timeout
		if strings.Contains(action, "start") || strings.Contains(action, "restart") {
			status, _ := s.Status()
			if strings.Contains(status, "running") || strings.Contains(status, "активен") {
				return output, nil
			}
		}
		return output, fmt.Errorf("timeout exceeded")
	case err := <-done:
		output := utils.StripANSI(out.String())
		if err != nil && (strings.Contains(action, "start") || strings.Contains(action, "restart")) {
			// Check if it's running despite the error code
			status, _ := s.Status()
			if strings.Contains(status, "running") || strings.Contains(status, "активен") {
				return output, nil
			}
		}
		return output, err
	}
}
