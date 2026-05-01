package services

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
)

type MihomoService struct {
	BinaryPath string
	ConfigDir  string
}

func NewMihomoService(binary, configDir string) *MihomoService {
	return &MihomoService{
		BinaryPath: binary,
		ConfigDir:  configDir,
	}
}

func (s *MihomoService) Status() (string, error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("pidof %s", s.BinaryPath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped", nil
	}
	if len(out) > 0 {
		return "running (pid: " + string(out) + ")", nil
	}
	return "stopped", nil
}

func (s *MihomoService) Start() (string, error) {
	return s.runWithTimeout("start", 30*time.Second)
}

func (s *MihomoService) Stop() (string, error) {
	return s.runWithTimeout("stop", 30*time.Second)
}

func (s *MihomoService) Restart() (string, error) {
	return s.runWithTimeout("restart", 45*time.Second)
}

func (s *MihomoService) runWithTimeout(action string, timeout time.Duration) (string, error) {
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
