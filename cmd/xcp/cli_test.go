package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// writeTestConfig создаёт минимальный config.json во временной директории с
// собственным DataDir и XCPLogPath (тоже во временной директории), чтобы
// тесты CLI никогда не трогали /opt/etc/xcp.
func writeTestConfig(t *testing.T, dir, passwordHash string) string {
	t.Helper()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	logPath := filepath.Join(dir, "xcp.log")
	cfgJSON := fmt.Sprintf(`{"data_dir": %q, "xcp_log_path": %q, "auth": {"password_hash": %q}}`,
		dataDir, logPath, passwordHash)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// testDeps — детерминированные cliDeps для тестов: без реального терминала,
// сигналов ОС и /proc. findPIDs и signaled позволяют проверить, кого именно
// уведомил CLI.
func testDeps(stdin string, findPIDs func() ([]int, error), signaled *[]int) cliDeps {
	return cliDeps{
		stdin:  strings.NewReader(stdin),
		stdout: io.Discard,
		stderr: io.Discard,
		isTerminal: func() bool {
			return false
		},
		readPassword: func(string) (string, error) {
			return "", fmt.Errorf("no terminal in test")
		},
		findDaemonPIDs: findPIDs,
		signalPID: func(pid int) error {
			if signaled != nil {
				*signaled = append(*signaled, pid)
			}
			return nil
		},
		selfPID: 200,
		sleep:   func(time.Duration) {},
	}
}

// interactiveDeps — testDeps-аналог для интерактивного режима (isTerminal
// возвращает true): readPassword последовательно отдаёт значения из answers,
// имитируя два запроса «Новый пароль:» / «Повторите пароль:».
func interactiveDeps(answers []string, findPIDs func() ([]int, error)) cliDeps {
	i := 0
	return cliDeps{
		stdin:  strings.NewReader(""),
		stdout: io.Discard,
		stderr: io.Discard,
		isTerminal: func() bool {
			return true
		},
		readPassword: func(string) (string, error) {
			if i >= len(answers) {
				return "", fmt.Errorf("unexpected extra readPassword call")
			}
			a := answers[i]
			i++
			return a, nil
		},
		findDaemonPIDs: findPIDs,
		signalPID:      func(int) error { return nil },
		selfPID:        200,
		sleep:          func(time.Duration) {},
	}
}

func TestResetPassword_InteractiveTwoPrompts(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	deps := interactiveDeps([]string{"new-pass-2026", "new-pass-2026"}, func() ([]int, error) { return nil, nil })
	if code := runResetPassword(cfgPath, false, deps); code != 0 {
		t.Fatalf("expected exit 0 for a matching interactive confirmation, got %d", code)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Auth.PasswordHash), []byte("new-pass-2026")); err != nil {
		t.Fatalf("password hash does not match: %v", err)
	}
}

func TestResetPassword_MismatchedConfirmation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	deps := interactiveDeps([]string{"new-pass-2026", "different-pass-2026"}, func() ([]int, error) { return nil, nil })
	if code := runResetPassword(cfgPath, false, deps); code != 1 {
		t.Fatalf("expected exit 1 for mismatched confirmation, got %d", code)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.PasswordHash != "" {
		t.Fatalf("expected password hash to remain empty after a mismatched confirmation")
	}
}

func TestSetupCode_PrintsCodeWhenNoPassword(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	var out bytes.Buffer
	deps := testDeps("", nil, nil)
	deps.stdout = &out

	if code := runSetupCode(cfgPath, deps); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	first := strings.TrimSpace(out.String())
	if len(first) == 0 {
		t.Fatalf("expected a setup code on stdout, got empty output")
	}

	out.Reset()
	if code := runSetupCode(cfgPath, deps); code != 0 {
		t.Fatalf("expected exit 0 on the second call, got %d", code)
	}
	second := strings.TrimSpace(out.String())
	if second != first {
		t.Fatalf("expected the same setup code across calls, got %q then %q", first, second)
	}
}

func TestSetupCode_PasswordAlreadySet(t *testing.T) {
	dir := t.TempDir()
	hash, err := auth.GeneratePasswordHash("existing-pass-2026")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := writeTestConfig(t, dir, hash)

	var out bytes.Buffer
	deps := testDeps("", nil, nil)
	deps.stdout = &out

	if code := runSetupCode(cfgPath, deps); code != 1 {
		t.Fatalf("expected exit 1 when a password is already set, got %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty stdout when a password is already set, got: %s", out.String())
	}
}

func TestPrintUsage_ContainsResetInstructions(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	for _, want := range []string{"--reset-password", "--password-stdin", "--setup-code", "-config", "Забыли пароль"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected usage text to contain %q, got: %s", want, out)
		}
	}
}

func TestResetPassword_StdinWritesHashAndSignalsDaemon(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	var signaled []int
	deps := testDeps("new-pass-2026\n", func() ([]int, error) {
		// Симулирует findDaemonPIDsDefault: pidof отдаёт "100 200", свой
		// PID (200) исключается — сигнал должен получить только 100.
		return parseDaemonPIDs([]byte("100 200"), 200, nil), nil
	}, &signaled)

	if code := runResetPassword(cfgPath, true, deps); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	if len(signaled) != 1 || signaled[0] != 100 {
		t.Fatalf("expected SIGHUP only to PID 100, got %v", signaled)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Auth.PasswordHash), []byte("new-pass-2026")); err != nil {
		t.Fatalf("password hash does not match: %v", err)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected config.json mode 0600, got %o", info.Mode().Perm())
	}

	logData, err := os.ReadFile(filepath.Join(dir, "xcp.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "[auth]") {
		t.Fatalf("expected [auth] line in xcp.log, got: %s", logData)
	}
	if strings.Contains(string(logData), "new-pass-2026") {
		t.Fatalf("password must never appear in the log")
	}
}

func TestResetPassword_PolicyViolationKeepsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	deps := testDeps("password123\n", func() ([]int, error) { return nil, nil }, nil)
	if code := runResetPassword(cfgPath, true, deps); code != 1 {
		t.Fatalf("expected exit 1 for a blacklisted password, got %d", code)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.PasswordHash != "" {
		t.Fatalf("expected password hash to remain empty after a rejected password")
	}
}

func TestResetPassword_NoTerminalWithoutStdinFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")

	deps := testDeps("", func() ([]int, error) { return nil, nil }, nil)
	if code := runResetPassword(cfgPath, false, deps); code != 1 {
		t.Fatalf("expected exit 1 without a terminal and without --password-stdin, got %d", code)
	}
}

func TestResetPassword_DaemonNotRunningClearsState(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	svc := auth.NewAuthService(auth.Options{DataDir: cfg.DataDir})
	if _, err := svc.CreateSession(); err != nil {
		t.Fatal(err)
	}
	svc.Stop()

	sessionsPath := filepath.Join(cfg.DataDir, "sessions.json")
	before, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `"id"`) {
		t.Fatalf("expected a persisted session before reset, got: %s", before)
	}

	deps := testDeps("new-pass-2026\n", func() ([]int, error) { return nil, nil }, nil)
	if code := runResetPassword(cfgPath, true, deps); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	after, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), `"id"`) {
		t.Fatalf("expected sessions.json cleared after a reset while the panel is not running, got: %s", after)
	}
}

func TestResetPassword_NoPasswordYetRemovesSetupCode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.EnsureSetupCode(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	setupCodePath := filepath.Join(cfg.DataDir, "setup_code")
	if _, err := os.Stat(setupCodePath); err != nil {
		t.Fatalf("expected a setup code file to exist before reset: %v", err)
	}

	deps := testDeps("new-pass-2026\n", func() ([]int, error) { return nil, nil }, nil)
	if code := runResetPassword(cfgPath, true, deps); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	if _, err := os.Stat(setupCodePath); !os.IsNotExist(err) {
		t.Fatalf("expected the setup code file removed once a password is set, stat err=%v", err)
	}

	newCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if newCfg.Auth.PasswordHash == "" {
		t.Fatalf("expected password hash to be set")
	}
}

func TestParseDaemonPIDs_ExcludesSelfAndCLI(t *testing.T) {
	cmdlines := map[int]string{
		100: "xcp\x00-config\x00/opt/etc/xcp/config.json\x00",
		200: "xcp\x00--reset-password\x00--password-stdin\x00",
		300: "xcp\x00--setup-code\x00",
	}
	readCmdline := func(pid int) (string, error) {
		if c, ok := cmdlines[pid]; ok {
			return c, nil
		}
		return "", fmt.Errorf("pid %d not found", pid)
	}

	got := parseDaemonPIDs([]byte("100 200 300 400"), 400, readCmdline)
	if len(got) != 1 || got[0] != 100 {
		t.Fatalf("expected only the daemon PID 100, got %v", got)
	}
}

func TestReloadAuthFromConfig_InvalidatesSessionsAndUpdatesCfg(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}

	oldHash, err := auth.GeneratePasswordHash("old-password-2026")
	if err != nil {
		t.Fatal(err)
	}

	svc := auth.NewAuthService(auth.Options{PasswordHash: oldHash, DataDir: dataDir})
	defer svc.Stop()

	issued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	newHash, err := auth.GeneratePasswordHash("new-password-2026")
	if err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.json")
	cfgJSON := fmt.Sprintf(`{"data_dir": %q, "auth": {"password_hash": %q}}`, dataDir, newHash)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	// liveCfg симулирует собственную копию конфига демона в памяти — на
	// момент SIGHUP она ещё содержит старый хеш, записанный при старте.
	liveCfg := &config.Config{ConfigPath: cfgPath, Auth: config.AuthConfig{PasswordHash: oldHash}}

	if err := reloadAuthFromConfig(cfgPath, liveCfg, svc); err != nil {
		t.Fatal(err)
	}

	if liveCfg.Auth.PasswordHash != newHash {
		t.Fatalf("expected the daemon's in-memory config hash to be updated to the new hash")
	}
	if _, err := svc.ValidateSession(issued.Token); err == nil {
		t.Fatalf("expected the old session to be invalidated after a hot reload")
	}
	if err := svc.VerifyPassword("new-password-2026"); err != nil {
		t.Fatalf("expected the new password to verify: %v", err)
	}
}

// TestReloadAuthFromConfig_RaceWithConcurrentConfigSave locks in the fix for
// 134-REVIEW CR-01: the SIGHUP handler's reloadAuthFromConfig writes
// cfg.Auth.PasswordHash on its own goroutine while HTTP handler goroutines
// (simulated here by directly mutating cfg.Auth.SessionIdleTTLHours and
// calling config.Save, the same pattern used by session_settings.go and
// settings.go) read/write the same shared *config.Config concurrently.
// Before the fix (config.Config had no synchronization), `go test -race`
// flagged a data race here; this test exists to keep it caught if the
// locking is ever removed or bypassed.
func TestReloadAuthFromConfig_RaceWithConcurrentConfigSave(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}

	oldHash, err := auth.GeneratePasswordHash("old-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := auth.GeneratePasswordHash("new-password-2026")
	if err != nil {
		t.Fatal(err)
	}

	svc := auth.NewAuthService(auth.Options{PasswordHash: oldHash, DataDir: dataDir})
	defer svc.Stop()

	cfgPath := filepath.Join(dir, "config.json")
	cfgJSON := fmt.Sprintf(`{"data_dir": %q, "auth": {"password_hash": %q}}`, dataDir, newHash)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	liveCfg := &config.Config{ConfigPath: cfgPath, Auth: config.AuthConfig{PasswordHash: oldHash}}

	const iterations = 200
	var wg sync.WaitGroup

	// Goroutine #1: repeated SIGHUP-style hot password reload.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := reloadAuthFromConfig(cfgPath, liveCfg, svc); err != nil {
				t.Errorf("reloadAuthFromConfig: %v", err)
				return
			}
		}
	}()

	// Goroutine #2: repeated concurrent handler-style field write + save,
	// mirroring internal/handlers/session_settings.go's sessionSettingsUpdate.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			liveCfg.Lock()
			liveCfg.Auth.SessionIdleTTLHours = 1 + i%720
			liveCfg.Unlock()
			if err := config.Save(cfgPath, liveCfg); err != nil {
				t.Errorf("config.Save: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}
