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
				{Name: "Start", Description: "Запуск прокси-клиента", Command: "-start"},
				{Name: "Stop", Description: "Остановка прокси-клиента", Command: "-stop"},
				{Name: "Restart", Description: "Перезапуск прокси-клиента", Command: "-restart"},
				{Name: "Status", Description: "Статус работы", Command: "-status"},
				{Name: "Toggle Auto", Description: "Вкл/Выкл автозапуск", Command: "-auto"},
				{Name: "Diag", Description: "Выполнить диагностику", Command: "-diag"},
				{Name: "Switch Xray", Description: "Переключить на ядро Xray", Command: "-xray", Dangerous: true},
				{Name: "Switch Mihomo", Description: "Переключить на ядро Mihomo", Command: "-mihomo", Dangerous: true},
			},
		},
		{
			Name: "update",
			Commands: []CommandDef{
				{Name: "Update XKeen", Description: "Обновление XKeen", Command: "-uk"},
				{Name: "Update Geo", Description: "Обновление GeoFile/GeoIPSET", Command: "-ug"},
				{Name: "Update Xray", Description: "Обновление Xray", Command: "-ux"},
				{Name: "Update Mihomo", Description: "Обновление Mihomo", Command: "-um"},
				{Name: "Channel", Description: "Переключить канал (Stable/Dev)", Command: "-channel"},
			},
		},
		{
			Name: "backup",
			Commands: []CommandDef{
				{Name: "Backup XKeen", Description: "Создать резервную копию XKeen", Command: "-kb"},
				{Name: "Restore XKeen", Description: "Восстановить XKeen", Command: "-kbr", Dangerous: true},
				{Name: "Backup Xray", Description: "Создать резервную копию Xray", Command: "-xb"},
				{Name: "Restore Xray", Description: "Восстановить Xray", Command: "-xbr", Dangerous: true},
				{Name: "Backup Mihomo", Description: "Создать резервную копию Mihomo", Command: "-mb"},
				{Name: "Restore Mihomo", Description: "Восстановить Mihomo", Command: "-mbr", Dangerous: true},
			},
		},
		{
			Name: "network",
			Commands: []CommandDef{
				{Name: "Ports & Gateway", Description: "Порты, шлюз и протокол", Command: "-tp"},
				{Name: "Toggle IPv6", Description: "Вкл/Выкл протокол IPv6", Command: "-ipv6"},
				{Name: "Toggle DNS", Description: "Вкл/Выкл перенаправление DNS", Command: "-dns"},
				{Name: "View Ports", Description: "Посмотреть проксируемые порты", Command: "-cp"},
				{Name: "View Excl. Ports", Description: "Посмотреть исключенные порты", Command: "-cpe"},
			},
		},
		{
			Name: "system",
			Commands: []CommandDef{
				{Name: "Version", Description: "Версия XKeen", Command: "-v"},
				{Name: "Help", Description: "Справка XKeen", Command: "-h"},
				{Name: "About", Description: "О программе", Command: "-about"},
			},
		},
	}
}

// Execute runs an XKeen command and returns its output
func (s *ConsoleService) Execute(command string) (*CommandResult, error) {
	// Validate command against whitelist
	if !s.isAllowedCommand(command) {
		return &CommandResult{
			Success: false,
			Error:   fmt.Sprintf("command %q is not allowed", command),
		}, nil
	}

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

// isAllowedCommand checks if a command is in the whitelist
func (s *ConsoleService) isAllowedCommand(command string) bool {
	for _, cat := range s.GetCommands() {
		for _, cmd := range cat.Commands {
			if cmd.Command == command {
				return true
			}
		}
	}
	return false
}
