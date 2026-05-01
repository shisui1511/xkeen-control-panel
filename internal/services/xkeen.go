package services

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type XKeenService struct {
	BinaryPath string
}

func NewXKeenService(binary string) *XKeenService {
	return &XKeenService{BinaryPath: binary}
}

func (s *XKeenService) Status() (string, error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s status", s.BinaryPath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *XKeenService) Start() (string, error) {
	return s.runWithTimeout("start", 30*time.Second)
}

func (s *XKeenService) Stop() (string, error) {
	return s.runWithTimeout("stop", 30*time.Second)
}

func (s *XKeenService) Restart() (string, error) {
	return s.runWithTimeout("restart", 45*time.Second)
}

func (s *XKeenService) runWithTimeout(action string, timeout time.Duration) (string, error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %s", s.BinaryPath, action))
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
		cmd.Process.Kill()
		return out.String(), fmt.Errorf("timeout exceeded")
	case err := <-done:
		return out.String(), err
	}
}
