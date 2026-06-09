package services

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PreflightIssue represents a single validation issue (error or warning) from a preflight check.
type PreflightIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PreflightResult holds the outcome of a preflight validation.
type PreflightResult struct {
	Valid    bool
	Errors   []PreflightIssue
	Warnings []PreflightIssue
}

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
	cmd := exec.Command("pidof", filepath.Base(s.BinaryPath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped", nil
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	var activePids []string
	for _, pidStr := range pids {
		if !isShortLivedOrHelperProcess(pidStr) {
			activePids = append(activePids, pidStr)
		}
	}
	if len(activePids) > 0 {
		return "running (pid: " + strings.Join(activePids, " ") + ")", nil
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

// ValidateMihomoConfig inspects the Mihomo config.yaml and returns a PreflightResult
// with blocking errors and non-blocking warnings. On read or parse failure, returns
// a non-nil error (the handler converts this to a safe valid:true response).
func (s *MihomoService) ValidateMihomoConfig() (PreflightResult, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("failed to read mihomo config: %w", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return PreflightResult{}, fmt.Errorf("failed to parse mihomo config: %w", err)
	}

	var errors []PreflightIssue
	var warnings []PreflightIssue

	// ERROR: external-controller is required for the panel API to communicate with Mihomo.
	val, ok := cfg["external-controller"]
	if !ok {
		errors = append(errors, PreflightIssue{
			Code:    "no_external_controller",
			Message: "external-controller is not configured; Clash API will be unavailable",
		})
	} else if strVal, ok := val.(string); ok && strings.TrimSpace(strVal) == "" {
		errors = append(errors, PreflightIssue{
			Code:    "no_external_controller",
			Message: "external-controller is not configured; Clash API will be unavailable",
		})
	}

	// WARNING: proxy-groups absence means no routing groups defined.
	if _, ok := cfg["proxy-groups"]; !ok {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_proxy_groups",
			Message: "no proxy-groups defined in config",
		})
	}

	// WARNING: rules absence means no routing rules.
	if _, ok := cfg["rules"]; !ok {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_rules",
			Message: "no rules defined in config",
		})
	}

	// WARNING: no proxies AND no proxy-providers.
	_, hasProxies := cfg["proxies"]
	_, hasProxyProviders := cfg["proxy-providers"]
	if !hasProxies && !hasProxyProviders {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_proxies_or_providers",
			Message: "neither proxies nor proxy-providers are defined in config",
		})
	}

	return PreflightResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
	}, nil
}
