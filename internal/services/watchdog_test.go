package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchdogService_New(t *testing.T) {
	tmpDir := t.TempDir()
	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	if w == nil {
		t.Fatal("expected non-nil WatchdogService")
	}
	if w.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures on a fresh watchdog, got %d", w.ConsecutiveFailures())
	}
}

func TestWatchdogService_CheckHealth_Healthy(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is running\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 0 {
		t.Fatalf("expected 0 consecutive failures after a healthy check, got %d", got)
	}
}

func TestWatchdogService_CheckHealth_FailureCounterAndTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	// Exit non-zero and print an unhealthy status so isKernelStatusHealthy() is false.
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	for i := 1; i <= watchdogMaxFailures; i++ {
		w.CheckHealth()
		if got := w.ConsecutiveFailures(); got != i {
			t.Fatalf("after check %d: expected %d consecutive failures, got %d", i, i, got)
		}
	}

	w.mu.Lock()
	disarmed := w.disarmed
	w.mu.Unlock()
	if !disarmed {
		t.Fatalf("expected watchdog to be disarmed after %d consecutive failures", watchdogMaxFailures)
	}

	// Give the async EmergencyDisarmTProxy() goroutine a moment to run to
	// completion so it doesn't leak past the end of the test (iptables is
	// almost certainly unavailable/unauthorized in CI, so it returns fast).
	time.Sleep(50 * time.Millisecond)
}

func TestWatchdogService_CheckHealth_RecoveryResetsCounter(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	w.CheckHealth()
	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", got)
	}

	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is running\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 0 {
		t.Fatalf("expected counter reset to 0 after recovery, got %d", got)
	}
}

func TestIsKernelStatusHealthy(t *testing.T) {
	cases := map[string]bool{
		"XKeen is running":     true,
		"Ядро активен":         true,
		"XKeen is not running": false,
		"":                     false,
		"stopped":              false,
	}
	for status, want := range cases {
		if got := isKernelStatusHealthy(status); got != want {
			t.Errorf("isKernelStatusHealthy(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestEnsureDefaultMihomoConfig_CreatesWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	if err := EnsureDefaultMihomoConfig(tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(tmpDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected config.yaml to be created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty default config.yaml")
	}
}

func TestEnsureDefaultMihomoConfig_NoOpWhenConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	original := []byte("mixed-port: 1234\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefaultMihomoConfig(tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("expected existing config.yaml to be left untouched")
	}
}

func TestEnsureDefaultMihomoConfig_NoOpWhenYmlExists(t *testing.T) {
	tmpDir := t.TempDir()
	ymlPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(ymlPath, []byte("mixed-port: 1234\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefaultMihomoConfig(tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		t.Fatal("expected config.yaml NOT to be created when config.yml already exists")
	}
}

func TestEnsureDefaultMihomoConfig_NoOpWhenDirMissing(t *testing.T) {
	tmpDir := t.TempDir()
	missing := filepath.Join(tmpDir, "does-not-exist")

	if err := EnsureDefaultMihomoConfig(missing); err != nil {
		t.Fatalf("expected nil error for a missing directory, got %v", err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("expected EnsureDefaultMihomoConfig not to create the directory itself")
	}
}

func TestValidateXrayRoutingTags_NoIssues(t *testing.T) {
	tmpDir := t.TempDir()
	outbounds := `{"outbounds":[{"tag":"PROXY_TAG","protocol":"freedom"},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}`
	routing := `{"routing":{"rules":[{"type":"field","ip":["geoip:private"],"outboundTag":"direct"},{"type":"field","domain":["geosite:category-ads-all"],"outboundTag":"block"}]}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "04_outbounds.json"), []byte(outbounds), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "05_routing.json"), []byte(routing), 0644); err != nil {
		t.Fatal(err)
	}

	if issues := ValidateXrayRoutingTags(tmpDir); len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestValidateXrayRoutingTags_DetectsDanglingTag(t *testing.T) {
	tmpDir := t.TempDir()
	outbounds := `{"outbounds":[{"tag":"direct","protocol":"freedom"}]}`
	routing := `{"routing":{"rules":[{"type":"field","domain":["geosite:geolocation-!cn"],"outboundTag":"PROXY_TAG_TYPO"}]}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "04_outbounds.json"), []byte(outbounds), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "05_routing.json"), []byte(routing), 0644); err != nil {
		t.Fatal(err)
	}

	issues := ValidateXrayRoutingTags(tmpDir)
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %v", issues)
	}
}

func TestValidateXrayRoutingTags_NoOutboundFragments(t *testing.T) {
	tmpDir := t.TempDir()
	if issues := ValidateXrayRoutingTags(tmpDir); len(issues) != 0 {
		t.Fatalf("expected no issues when there are no outbound fragments, got %v", issues)
	}
}
