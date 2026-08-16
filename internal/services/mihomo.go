package services

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// ControllerInfo contains parsed information about Mihomo external controller connection.
type ControllerInfo struct {
	Type       string // "unix" | "tcp" | "none"
	Target     string // "/opt/var/run/mihomo.sock" or "127.0.0.1:9090" / "0.0.0.0:9090"
	Secret     string
	IsInsecure bool // true if TCP 0.0.0.0 or :port
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

func (s *MihomoService) ParseControllerConfig() (ControllerInfo, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	file, err := os.Open(configPath)
	if err != nil {
		return ControllerInfo{Type: "none"}, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	var unixCtrl, tcpCtrl, secret string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = stripComment(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "external-controller-unix:") {
			val := strings.TrimPrefix(line, "external-controller-unix:")
			unixCtrl = cleanYamlValue(val)
		} else if strings.HasPrefix(line, "external-controller:") {
			val := strings.TrimPrefix(line, "external-controller:")
			tcpCtrl = cleanYamlValue(val)
		} else if strings.HasPrefix(line, "external-controller-secret:") {
			val := strings.TrimPrefix(line, "external-controller-secret:")
			secret = cleanYamlValue(val)
		} else if strings.HasPrefix(line, "secret:") {
			val := strings.TrimPrefix(line, "secret:")
			secret = cleanYamlValue(val)
		}
	}

	if err := scanner.Err(); err != nil {
		return ControllerInfo{Type: "none"}, fmt.Errorf("scanner error: %w", err)
	}

	if unixCtrl != "" {
		return ControllerInfo{
			Type:       "unix",
			Target:     unixCtrl,
			Secret:     secret,
			IsInsecure: false,
		}, nil
	}

	if tcpCtrl != "" {
		isInsecure := strings.HasPrefix(tcpCtrl, "0.0.0.0:") || strings.HasPrefix(tcpCtrl, ":") || tcpCtrl == "0.0.0.0"
		return ControllerInfo{
			Type:       "tcp",
			Target:     tcpCtrl,
			Secret:     secret,
			IsInsecure: isInsecure,
		}, nil
	}

	return ControllerInfo{
		Type:       "none",
		Target:     "",
		Secret:     secret,
		IsInsecure: false,
	}, nil
}

func (s *MihomoService) ParseConfig() (controller string, secret string, err error) {
	info, err := s.ParseControllerConfig()
	if err != nil {
		return "", "", err
	}
	return info.Target, info.Secret, nil
}

// EnsureSocketDir ensures that the directory for Unix domain socket exists.
func (s *MihomoService) EnsureSocketDir() error {
	dir := "/opt/var/run"
	info, err := s.ParseControllerConfig()
	if err == nil && info.Type == "unix" && info.Target != "" {
		dir = filepath.Dir(info.Target)
	}
	return os.MkdirAll(dir, 0755)
}

// CleanupStaleSocket removes the socket file if Mihomo process is not currently running.
func (s *MihomoService) CleanupStaleSocket() error {
	info, err := s.ParseControllerConfig()
	if err != nil || info.Type != "unix" || info.Target == "" {
		return nil
	}
	status, err := s.Status()
	if err == nil && strings.Contains(status, "running") {
		// Process is running, do not delete active socket
		return nil
	}
	if _, err := os.Stat(info.Target); err == nil {
		return os.Remove(info.Target)
	}
	return nil
}

// GetDialContext returns a dialer function matching the configured controller type (unix or tcp).
func (s *MihomoService) GetDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	info, err := s.ParseControllerConfig()
	if err == nil && info.Type == "unix" && info.Target != "" {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", info.Target)
		}
	}
	return (&net.Dialer{
		Timeout: 30 * time.Second,
	}).DialContext
}

// GetHTTPTransport creates an *http.Transport tailored for communicating with Mihomo API.
func (s *MihomoService) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           s.GetDialContext(),
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
}

// GetHTTPClient returns an *http.Client with 30s timeout using GetHTTPTransport.
func (s *MihomoService) GetHTTPClient() *http.Client {
	return &http.Client{
		Transport: s.GetHTTPTransport(),
		Timeout:   30 * time.Second,
	}
}

// ProbeAPI checks if Mihomo API is reachable and authenticated by requesting /version.
func (s *MihomoService) ProbeAPI(secret string) (reachable bool, authenticated bool) {
	info, err := s.ParseControllerConfig()
	var reqURL string
	if err == nil && info.Type == "unix" {
		reqURL = "http://localhost/version"
	} else if err == nil && info.Type == "tcp" && info.Target != "" {
		target := info.Target
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			target = "http://" + target
		}
		reqURL = strings.TrimRight(target, "/") + "/version"
	} else {
		return false, false
	}

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return false, false
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	transport := &http.Transport{
		DialContext:           s.GetDialContext(),
		ResponseHeaderTimeout: 3 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, true
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return true, false
	}
	return true, false
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

	// Check external-controller-unix or external-controller
	unixVal, hasUnix := cfg["external-controller-unix"]
	tcpVal, hasTCP := cfg["external-controller"]

	var unixStr, tcpStr string
	if hasUnix {
		if s, ok := unixVal.(string); ok {
			unixStr = strings.TrimSpace(s)
		}
	}
	if hasTCP {
		if s, ok := tcpVal.(string); ok {
			tcpStr = strings.TrimSpace(s)
		}
	}

	if unixStr != "" {
		// Valid unix socket configured
	} else if tcpStr != "" {
		// Valid TCP configured, but check if insecure (listening on 0.0.0.0 or :port)
		if strings.HasPrefix(tcpStr, "0.0.0.0:") || strings.HasPrefix(tcpStr, ":") || tcpStr == "0.0.0.0" {
			warnings = append(warnings, PreflightIssue{
				Code:    "insecure_external_controller",
				Message: "external-controller listens on 0.0.0.0; recommend migrating to Unix domain socket",
			})
		}
	} else {
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
