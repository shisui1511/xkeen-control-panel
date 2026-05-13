package services

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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
	// Status checks if the binary is in the process list
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
