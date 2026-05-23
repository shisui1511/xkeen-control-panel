package services

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type XKeenService struct {
	BinaryPath string
}

func NewXKeenService(binary string) *XKeenService {
	return &XKeenService{BinaryPath: binary}
}

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
	return output
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
	return s.runWithTimeout("-start", 30*time.Second)
}

func (s *XKeenService) Stop() (string, error) {
	return s.runWithTimeout("-stop", 30*time.Second)
}

func (s *XKeenService) Restart() (string, error) {
	return s.runWithTimeout("-restart", 45*time.Second)
}

func (s *XKeenService) SwitchKernel(name string) (string, error) {
	if name == "xray" {
		return s.runWithTimeout("-xray", 30*time.Second)
	} else if name == "mihomo" {
		return s.runWithTimeout("-mihomo", 30*time.Second)
	}
	return "", fmt.Errorf("invalid kernel: %s", name)
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
