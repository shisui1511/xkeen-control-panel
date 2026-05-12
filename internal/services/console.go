package services

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
)

// CommandCategory represents a group of related commands
type CommandCategory struct {
	Name     string       `json:"name"`
	Commands []CommandDef `json:"commands"`
}

// CommandDef represents a single console command
type CommandDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Dangerous   bool   `json:"dangerous"`
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ConsoleService executes XKeen commands and manages available commands
type ConsoleService struct {
	xkeenPath string
	mu        sync.Mutex
}

func NewConsoleService(xkeenPath string) *ConsoleService {
	return &ConsoleService{
		xkeenPath: xkeenPath,
	}
}

// GetCommands returns available commands grouped by category
func (s *ConsoleService) GetCommands() []CommandCategory {
	return []CommandCategory{
		{
			Name: "service",
			Commands: []CommandDef{
				{Name: "start", Description: "Start XKeen", Command: "start"},
				{Name: "stop", Description: "Stop XKeen", Command: "stop"},
				{Name: "restart", Description: "Restart XKeen", Command: "restart"},
				{Name: "status", Description: "XKeen status", Command: "status"},
			},
		},
		{
			Name: "config",
			Commands: []CommandDef{
				{Name: "check", Description: "Check config", Command: "check"},
				{Name: "show", Description: "Show current config", Command: "show"},
			},
		},
		{
			Name: "network",
			Commands: []CommandDef{
				{Name: "dns", Description: "DNS status", Command: "dns"},
				{Name: "routes", Description: "Show routes", Command: "routes"},
			},
		},
		{
			Name: "update",
			Commands: []CommandDef{
				{Name: "update", Description: "Check for updates", Command: "update"},
			},
		},
		{
			Name: "system",
			Commands: []CommandDef{
				{Name: "version", Description: "Show version", Command: "version"},
				{Name: "info", Description: "System info", Command: "info"},
			},
		},
	}
}

// Execute runs an XKeen command and returns its output
func (s *ConsoleService) Execute(command string) (*CommandResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := exec.Command(s.xkeenPath, command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &CommandResult{
		Output: stdout.String(),
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("%s\n%s", err.Error(), stderr.String())
	} else {
		result.Success = true
		if stderr.Len() > 0 {
			result.Output += "\n" + stderr.String()
		}
	}

	return result, nil
}
