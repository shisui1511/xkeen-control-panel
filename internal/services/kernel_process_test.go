package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKernelService_ProcessStates: порядок [xray, mihomo], статусы по наличию
// бинарника, и ни один бинарник ядра не запускается (версия не определяется).
func TestKernelService_ProcessStates(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "invoked.log")
	xrayPath := filepath.Join(dir, "xray")
	script := "#!/bin/sh\necho invoked >> " + logPath + "\necho 'Xray 1.8.0'\n"
	if err := os.WriteFile(xrayPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService(t.TempDir())
	svc.mu.Lock()
	svc.kernels["xray"].BinaryPath = xrayPath
	svc.kernels["mihomo"].BinaryPath = filepath.Join(dir, "mihomo-missing")
	svc.mu.Unlock()

	states := svc.ProcessStates()
	if len(states) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(states), states)
	}
	if states[0].Name != "xray" || states[1].Name != "mihomo" {
		t.Errorf("порядок = %s, %s; want xray, mihomo", states[0].Name, states[1].Name)
	}
	if states[0].Status != "stopped" || states[0].PID != 0 {
		t.Errorf("xray: status=%q pid=%d, want stopped/0", states[0].Status, states[0].PID)
	}
	if states[1].Status != "not_installed" || states[1].PID != 0 {
		t.Errorf("mihomo: status=%q pid=%d, want not_installed/0", states[1].Status, states[1].PID)
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Error("бинарник ядра был запущен: ProcessStates не должен определять версию")
	}
}
