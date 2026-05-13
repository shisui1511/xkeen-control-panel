package services

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type MihomoService struct {
	BinaryPath string
	XKeenPath  string
	ConfigDir  string
}

func NewMihomoService(binary, xkeenPath, configDir string) *MihomoService {
	return &MihomoService{
		BinaryPath: binary,
		XKeenPath:  xkeenPath,
		ConfigDir:  configDir,
	}
}

func (s *MihomoService) Status() (string, error) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("pidof %s", filepath.Base(s.BinaryPath)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped", nil
	}
	if len(out) > 0 {
		return "running (pid: " + strings.TrimSpace(string(out)) + ")", nil
	}
	return "stopped", nil
}

func (s *MihomoService) Start() (string, error) {
	// Use xkeen to manage the service correctly as a daemon
	return s.runXKeenCommand("-ms")
}

func (s *MihomoService) Stop() (string, error) {
	return s.runXKeenCommand("-mq")
}

func (s *MihomoService) Restart() (string, error) {
	// Stop then start to ensure clean restart via xkeen
	_, _ = s.Stop()
	time.Sleep(1 * time.Second)
	return s.Start()
}

func (s *MihomoService) runXKeenCommand(action string) (string, error) {
	if s.XKeenPath == "" {
		s.XKeenPath = "/opt/sbin/xkeen"
	}
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %s", s.XKeenPath, action))
	out, err := cmd.CombinedOutput()
	output := utils.StripANSI(string(out))

	// If it's a start command, wait a bit and check if it's running
	// because xkeen might return non-zero if already running or just being slow
	if strings.Contains(action, "-ms") {
		time.Sleep(2 * time.Second)
		status, _ := s.Status()
		if strings.Contains(status, "running") {
			return output, nil
		}
	}

	if err != nil {
		return output, err
	}
	return strings.TrimSpace(output), nil
}
