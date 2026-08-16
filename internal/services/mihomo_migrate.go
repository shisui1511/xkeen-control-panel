package services

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const DefaultMihomoSocketPath = "/opt/var/run/mihomo.sock"

type MigrationPreview struct {
	CurrentController string `json:"current_controller"`
	CurrentType       string `json:"current_type"`
	TargetSocket      string `json:"target_socket"`
	IsInsecure        bool   `json:"is_insecure"`
	AlreadyMigrated   bool   `json:"already_migrated"`
	DiffOld           string `json:"diff_old"`
	DiffNew           string `json:"diff_new"`
}

type MigrationResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	BackupPath string `json:"backup_path,omitempty"`
	SocketPath string `json:"socket_path,omitempty"`
	Error      string `json:"error,omitempty"`
	RolledBack bool   `json:"rolled_back,omitempty"`
}

// GetMigrationPreview inspects the current configuration and returns diff and migration details.
func (s *MihomoService) GetMigrationPreview() (*MigrationPreview, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	var currentCtrl string
	var currentType = "none"
	var diffOld string
	var alreadyMigrated bool

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		stripped := stripComment(line)
		trimmed := strings.TrimSpace(stripped)

		if strings.HasPrefix(trimmed, "external-controller-unix:") {
			val := cleanYamlValue(strings.TrimPrefix(trimmed, "external-controller-unix:"))
			currentCtrl = val
			currentType = "unix"
			alreadyMigrated = true
			diffOld = line
			break
		} else if strings.HasPrefix(trimmed, "external-controller:") {
			val := cleanYamlValue(strings.TrimPrefix(trimmed, "external-controller:"))
			currentCtrl = val
			currentType = "tcp"
			diffOld = line
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	isInsecure := false
	if currentType == "tcp" {
		isInsecure = strings.HasPrefix(currentCtrl, "0.0.0.0:") || strings.HasPrefix(currentCtrl, ":") || currentCtrl == "0.0.0.0"
	}

	diffNew := fmt.Sprintf("external-controller-unix: %s", DefaultMihomoSocketPath)
	if alreadyMigrated {
		diffNew = diffOld
	}

	return &MigrationPreview{
		CurrentController: currentCtrl,
		CurrentType:       currentType,
		TargetSocket:      DefaultMihomoSocketPath,
		IsInsecure:        isInsecure,
		AlreadyMigrated:   alreadyMigrated,
		DiffOld:           diffOld,
		DiffNew:           diffNew,
	}, nil
}

// MigrateToSocket migrates Mihomo configuration from TCP port to Unix domain socket.
func (s *MihomoService) MigrateToSocket(xkeenSvc *XKeenService) (*MigrationResult, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	origBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 1. Create backup with timestamp
	backupName := fmt.Sprintf("%s.bak.%d", filepath.Base(configPath), time.Now().Unix())
	backupPath := filepath.Join(s.ConfigDir, backupName)
	if err := os.WriteFile(backupPath, origBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to create config backup: %w", err)
	}

	// 2. Ensure socket dir and cleanup stale socket
	_ = s.EnsureSocketDir()
	_ = s.CleanupStaleSocket()

	// 3. Transform config lines
	lines := strings.Split(string(origBytes), "\n")
	var newLines []string
	replaced := false

	for _, line := range lines {
		stripped := stripComment(line)
		trimmed := strings.TrimSpace(stripped)

		if strings.HasPrefix(trimmed, "external-controller-unix:") {
			// Already has unix socket directive, update to default socket path
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			newLines = append(newLines, indent+"external-controller-unix: "+DefaultMihomoSocketPath)
			replaced = true
		} else if strings.HasPrefix(trimmed, "external-controller:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			newLines = append(newLines, indent+"external-controller-unix: "+DefaultMihomoSocketPath)
			replaced = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !replaced {
		newLines = append([]string{"external-controller-unix: " + DefaultMihomoSocketPath}, newLines...)
	}

	newContent := strings.Join(newLines, "\n")
	tmpFile := filepath.Join(s.ConfigDir, ".config.yaml.tmp")
	if err := os.WriteFile(tmpFile, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp config: %w", err)
	}
	if err := os.Rename(tmpFile, configPath); err != nil {
		_ = os.Remove(tmpFile)
		return nil, fmt.Errorf("failed to replace config: %w", err)
	}

	// 4. Validate config with mihomo -t if binary is available
	if s.BinaryPath != "" {
		if _, err := os.Stat(s.BinaryPath); err == nil {
			cmd := exec.Command(s.BinaryPath, "-t", "-d", s.ConfigDir, "-f", configPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				// Rollback
				_ = os.WriteFile(configPath, origBytes, 0644)
				return &MigrationResult{
					Success:    false,
					RolledBack: true,
					BackupPath: backupName,
					Error:      fmt.Sprintf("Mihomo configuration test failed: %s", strings.TrimSpace(string(out))),
				}, nil
			}
		}
	}

	// 5. Check if process was running; if so, restart and probe socket
	status, err := s.Status()
	isRunning := err == nil && strings.Contains(status, "running")

	if isRunning && xkeenSvc != nil {
		if _, err := xkeenSvc.Restart(); err != nil {
			// Rollback
			_ = os.WriteFile(configPath, origBytes, 0644)
			_, _ = xkeenSvc.Restart()
			return &MigrationResult{
				Success:    false,
				RolledBack: true,
				BackupPath: backupName,
				Error:      fmt.Sprintf("failed to restart service: %v", err),
			}, nil
		}

		// Read secret for probe
		_, secret, _ := s.ParseConfig()

		// Poll socket up to 10 seconds (20 iterations * 500ms)
		socketReachable := false
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			reachable, _ := s.ProbeAPI(secret)
			if reachable {
				socketReachable = true
				break
			}
		}

		if !socketReachable {
			// Rollback to backup
			_ = os.WriteFile(configPath, origBytes, 0644)
			_, _ = xkeenSvc.Restart()
			return &MigrationResult{
				Success:    false,
				RolledBack: true,
				BackupPath: backupName,
				Error:      "Unix domain socket unreachable after 10s timeout; configuration rolled back",
			}, nil
		}
	}

	return &MigrationResult{
		Success:    true,
		Message:    "Mihomo successfully migrated to Unix domain socket",
		BackupPath: backupName,
		SocketPath: DefaultMihomoSocketPath,
	}, nil
}
