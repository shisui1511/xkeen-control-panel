package services

import (
	"bufio"
	"fmt"
	"os"
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

func (s *MihomoService) ParseConfig() (controller string, secret string, err error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	file, err := os.Open(configPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = stripComment(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "external-controller:") {
			val := strings.TrimPrefix(line, "external-controller:")
			controller = cleanYamlValue(val)
		} else if strings.HasPrefix(line, "external-controller-secret:") {
			val := strings.TrimPrefix(line, "external-controller-secret:")
			secret = cleanYamlValue(val)
		} else if strings.HasPrefix(line, "secret:") {
			val := strings.TrimPrefix(line, "secret:")
			secret = cleanYamlValue(val)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("scanner error: %w", err)
	}

	return controller, secret, nil
}

func stripComment(line string) string {
	inDoubleQuotes := false
	inSingleQuotes := false
	for i, char := range line {
		if char == '"' && (i == 0 || line[i-1] != '\\') {
			inDoubleQuotes = !inDoubleQuotes
		} else if char == '\'' && (i == 0 || line[i-1] != '\\') {
			inSingleQuotes = !inSingleQuotes
		} else if char == '#' && !inDoubleQuotes && !inSingleQuotes {
			return line[:i]
		}
	}
	return line
}

func cleanYamlValue(val string) string {
	val = strings.TrimSpace(val)
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return val
}
