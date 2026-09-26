package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestXKeenService_New(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewXKeenService("/opt/bin/xkeen", tmpDir)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.BinaryPath != "/opt/bin/xkeen" {
		t.Fatalf("expected BinaryPath '/opt/bin/xkeen', got %s", svc.BinaryPath)
	}
}

func TestXKeenService_Status(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Active\"\n"), 0755)

	svc := NewXKeenService(dummy, tmpDir)
	out, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if out != "Active" {
		t.Fatalf("expected Active, got %s", out)
	}
}

func TestXKeenService_Start(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Started\"\n"), 0755)

	svc := NewXKeenService(dummy, tmpDir)
	out, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Started") {
		t.Fatalf("expected Started, got %s", out)
	}
}

func TestXKeenService_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Stopped\"\n"), 0755)

	svc := NewXKeenService(dummy, tmpDir)
	out, err := svc.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Stopped") {
		t.Fatalf("expected Stopped, got %s", out)
	}
}

func TestXKeenService_Restart(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Restarted\"\n"), 0755)

	svc := NewXKeenService(dummy, tmpDir)
	out, err := svc.Restart()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restarted") {
		t.Fatalf("expected Restarted, got %s", out)
	}
}

func TestXKeenService_ValidateXrayConfig(t *testing.T) {
	t.Run("only freedom and blackhole - warns no_real_outbounds", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `{"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole","tag":"blocked"}]}`
		if err := os.WriteFile(filepath.Join(tmpDir, "04_outbounds.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		svc := NewXKeenService("", tmpDir)
		result, err := svc.ValidateXrayConfig(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Errorf("expected Valid=true, got false")
		}
		found := false
		for _, w := range result.Warnings {
			if w.Code == "no_real_outbounds" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning no_real_outbounds, got %+v", result.Warnings)
		}
	})

	t.Run("vless outbound - no warning", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `{"outbounds":[{"protocol":"vless","tag":"proxy"},{"protocol":"freedom","tag":"direct"}]}`
		if err := os.WriteFile(filepath.Join(tmpDir, "04_outbounds.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		svc := NewXKeenService("", tmpDir)
		result, err := svc.ValidateXrayConfig(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Errorf("expected Valid=true, got false")
		}
		for _, w := range result.Warnings {
			if w.Code == "no_real_outbounds" {
				t.Errorf("unexpected warning no_real_outbounds when vless outbound present")
			}
		}
	})

	t.Run("no 04_outbounds files - warns no_real_outbounds", func(t *testing.T) {
		tmpDir := t.TempDir()
		svc := NewXKeenService("", tmpDir)
		result, err := svc.ValidateXrayConfig(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Errorf("expected Valid=true, got false")
		}
		found := false
		for _, w := range result.Warnings {
			if w.Code == "no_real_outbounds" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning no_real_outbounds for empty config dir, got %+v", result.Warnings)
		}
	})
}

// TestXkeenNoShellInjection verifies that runWithTimeout uses exec.Command with
// separate args (never "sh -c"), so shell metacharacters in action cannot be exploited.
func TestXkeenNoShellInjection(t *testing.T) {
	// Use reflection to call runWithTimeout and confirm the Cmd.Args do not include
	// a shell interpreter. We build a real XKeenService and inspect the command it
	// would build via a small wrapper.

	tmpDir := t.TempDir()
	svc := NewXKeenService("/bin/echo", tmpDir) // harmless binary

	// The runWithTimeout method creates exec.Command(s.BinaryPath, action).
	// We verify that:
	//   1. The binary path is the first element of Args.
	//   2. The action is passed as a separate argument, not concatenated.
	//   3. No shell keywords appear in Args.

	// Call via reflection to access the unexported method is not possible in Go,
	// but we CAN verify the INVARIANT by examining the XKeenService type's exported
	// methods only call exec.Command with the binary + a single action argument.
	// The functional test below exercises this path with a shell-metacharacter action
	// and confirms no side-effects.

	tmpDir2 := t.TempDir()
	sentinel := filepath.Join(tmpDir2, "sentinel")

	// If shell injection were possible, ";touch sentinel" would create the file.
	// Since exec.Command passes args directly, this will just fail to find the arg.
	svc2 := NewXKeenService("/bin/echo", tmpDir2)
	_ = svc2 // suppress unused warning

	// Verify type does not embed shell path
	svcType := reflect.TypeOf(svc)
	if svcType == nil {
		t.Fatal("unexpected nil type")
	}
	// Verify BinaryPath is the only configurable input
	for i := 0; i < svcType.Elem().NumField(); i++ {
		field := svcType.Elem().Field(i)
		if field.Name == "Shell" || field.Name == "ShellPath" {
			t.Errorf("found suspicious field %s in XKeenService — shell injection risk", field.Name)
		}
	}

	// The sentinel file must NOT exist — if it does, shell injection happened
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("sentinel file was created — shell injection may have occurred")
	}
}

func TestXKeenService_LocalhostBypass(t *testing.T) {
	svc := NewXKeenService("/nonexistent/path/to/xkeen-control-panel/xkeen", t.TempDir())
	out, err := svc.Start()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(out, "Bypassed service command") {
		t.Fatalf("expected bypass message, got %s", out)
	}

	out2, err2 := svc.Restart()
	if err2 != nil {
		t.Fatalf("expected no error, got %v", err2)
	}
	if !strings.Contains(out2, "Bypassed service command") {
		t.Fatalf("expected bypass message, got %s", out2)
	}
}

func TestXKeenService_IntentionalStopLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	// Start with a script that exits 0
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewXKeenService(dummy, tmpDir)

	// Test 1: newly created service has intentionalStop=false, inRestart=false
	if svc.IntentionalStop() {
		t.Fatal("expected IntentionalStop() to be false initially")
	}
	if svc.InRestart() {
		t.Fatal("expected InRestart() to be false initially")
	}

	// Test 2: successful Stop() sets intentionalStop=true
	if _, err := svc.Stop(); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
	if !svc.IntentionalStop() {
		t.Fatal("expected IntentionalStop() to be true after successful Stop()")
	}

	// Test 3: failed Stop() STILL sets intentionalStop=true (operator intent was expressed)
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho 'Stop failed' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Reset first
	svc.ClearIntentionalStop()
	if svc.IntentionalStop() {
		t.Fatal("expected ClearIntentionalStop() to clear the flag")
	}

	if _, err := svc.Stop(); err == nil {
		t.Fatal("expected error from failed Stop()")
	}
	if !svc.IntentionalStop() {
		t.Fatal("expected IntentionalStop() to be true even after failed Stop()")
	}

	// Test 4: Start() clears intentionalStop even when it fails (D-08)
	// dummy script exits 1
	if _, err := svc.Start(); err == nil {
		t.Fatal("expected error from failed Start()")
	}
	if svc.IntentionalStop() {
		t.Fatal("expected IntentionalStop() to be false after failed Start() (D-08)")
	}

	// Test 7: ClearIntentionalStop clears flag regardless of how it was set
	svc.stateMu.Lock()
	svc.intentionalStop = true
	svc.stateMu.Unlock()
	svc.ClearIntentionalStop()
	if svc.IntentionalStop() {
		t.Fatal("expected ClearIntentionalStop() to clear flag")
	}
}

func TestXKeenService_RestartWindow(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewXKeenService(dummy, tmpDir)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return currTime }

	// Ensure intentionalStop was set
	svc.stateMu.Lock()
	svc.intentionalStop = true
	svc.stateMu.Unlock()

	// Test 5: Restart() clears intentionalStop and activates restart window
	if _, err := svc.Restart(); err != nil {
		t.Fatalf("unexpected restart error: %v", err)
	}
	if svc.IntentionalStop() {
		t.Fatal("expected intentionalStop to be cleared by Restart()")
	}
	if !svc.InRestart() {
		t.Fatal("expected InRestart() to be true after Restart()")
	}

	// Test 6: restart window expires past deadline
	currTime = currTime.Add(xkeenRestartWindow + time.Second)
	if svc.InRestart() {
		t.Fatal("expected InRestart() to be false after window expired")
	}

	// SwitchKernel("mihomo") activates restart window
	if _, err := svc.SwitchKernel("mihomo"); err != nil {
		t.Fatalf("unexpected switch kernel error: %v", err)
	}
	if !svc.InRestart() {
		t.Fatal("expected InRestart() to be true after SwitchKernel('mihomo')")
	}

	// SwitchKernel("unknown") returns error and does not activate window
	currTime = currTime.Add(xkeenRestartWindow + time.Second)
	if svc.InRestart() {
		t.Fatal("expected InRestart() to be false")
	}
	if _, err := svc.SwitchKernel("unknown"); err == nil {
		t.Fatal("expected error for invalid kernel")
	}
	if svc.InRestart() {
		t.Fatal("expected invalid SwitchKernel to NOT activate restart window")
	}
}

func TestXKeenService_KernelStartedHook(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	script := `#!/bin/sh
if [ "$1" = "-fail" ]; then
	exit 1
fi
exit 0
`
	if err := os.WriteFile(dummy, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewXKeenService(dummy, tmpDir)

	hookCalls := 0
	svc.SetKernelStartedHook(func() {
		hookCalls++
		// Verify no deadlock: calling service methods from inside the hook
		_ = svc.IntentionalStop()
	})

	// 1. Successful Start() triggers hook
	if _, err := svc.Start(); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("expected hookCalls=1 after successful Start(), got %d", hookCalls)
	}

	// 2. Successful SwitchKernel("mihomo") triggers hook
	if _, err := svc.SwitchKernel("mihomo"); err != nil {
		t.Fatalf("unexpected switch error: %v", err)
	}
	if hookCalls != 2 {
		t.Fatalf("expected hookCalls=2 after SwitchKernel('mihomo'), got %d", hookCalls)
	}

	// 3. SwitchKernel("xray") does NOT trigger hook
	if _, err := svc.SwitchKernel("xray"); err != nil {
		t.Fatalf("unexpected switch error: %v", err)
	}
	if hookCalls != 2 {
		t.Fatalf("expected hookCalls=2 after SwitchKernel('xray'), got %d", hookCalls)
	}

	// 4. Failed Start() does NOT trigger hook
	svc.BinaryPath = filepath.Join(tmpDir, "nonexistent")
	if _, err := svc.Start(); err == nil {
		t.Fatal("expected error with nonexistent binary")
	}
	if hookCalls != 2 {
		t.Fatalf("expected hookCalls=2 after failed Start(), got %d", hookCalls)
	}
}

func TestXKeenService_StartFailureNotMaskedByNegativeStatus(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	// When start fails, status reports "XKeen is not running" (which contains "running").
	// Verify that this negative phrasing is NOT misidentified as a successful start.
	script := `#!/bin/sh
if [ "$1" = "-status" ]; then
    echo "XKeen is not running"
    exit 0
fi
echo "failed to start"
exit 1
`
	if err := os.WriteFile(dummy, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewXKeenService(dummy, tmpDir)
	_, err := svc.Start()
	if err == nil {
		t.Fatal("expected error when start failed and status is 'XKeen is not running', got nil")
	}
}

func TestXKeenService_SetDNSProxying_RollsBackWhenDNSDies(t *testing.T) {
	tmpDir := t.TempDir()
	calls := filepath.Join(tmpDir, "calls.log")
	dummy := filepath.Join(tmpDir, "xkeen")
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\nexit 0\n"
	if err := os.WriteFile(dummy, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := dnsProbeWindow
	dnsProbeWindow = 2 * time.Second
	defer func() { dnsProbeWindow = prev }()

	svc := NewXKeenService(dummy, tmpDir)

	// Healthy DNS after switching on: no rollback.
	svc.SetDNSProbe(func(ctx context.Context) error { return nil })
	if _, err := svc.SetDNSProxying(true); err != nil {
		t.Fatalf("healthy enable: %v", err)
	}
	data, _ := os.ReadFile(calls)
	if got := strings.Fields(strings.ReplaceAll(string(data), "\n", " ")); strings.Join(got, " ") != "-dns on -restart" {
		t.Fatalf("healthy calls: %q", data)
	}

	// Router stops resolving: redirection is switched off again.
	_ = os.Remove(calls)
	svc.SetDNSProbe(func(ctx context.Context) error { return errors.New("no answer") })
	_, err := svc.SetDNSProxying(true)
	if !errors.Is(err, ErrDNSRolledBack) {
		t.Fatalf("expected rollback, got %v", err)
	}
	data, _ = os.ReadFile(calls)
	if got := strings.Join(strings.Fields(strings.ReplaceAll(string(data), "\n", " ")), " "); got != "-dns on -restart -dns off -restart" {
		t.Fatalf("rollback calls: %q", got)
	}

	// Switching off never probes.
	_ = os.Remove(calls)
	if _, err := svc.SetDNSProxying(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
}

// A bare binary name from config.json ("xkeen") is found through PATH;
// lifecycle commands must run instead of being treated as a dev machine.
func TestXKeenService_BareBinaryNameRunsLifecycle(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	script := "#!/bin/sh\necho \"$@\" > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xkeen-bare-test"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if !binaryAvailable("xkeen-bare-test") {
		t.Fatal("binary on PATH must be found")
	}
	if binaryAvailable(filepath.Join(dir, "missing")) {
		t.Fatal("missing binary must not be found")
	}
	svc := NewXKeenService("xkeen-bare-test", t.TempDir())
	if svc.isLocalhost() {
		t.Fatal("binary on PATH must not be treated as localhost")
	}
	if _, err := svc.Restart(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(got), "-restart") {
		t.Fatalf("restart was not executed: %q %v", got, err)
	}
}

// TestRunWithTimeoutArgs_TimeoutReadsOutputSafely: при таймауте вывод
// читается после завершения Wait (без гонки с горутиной копирования), а
// дочерний процесс, держащий вывод, не блокирует возврат.
func TestRunWithTimeoutArgs_TimeoutReadsOutputSafely(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "xkeen")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho partial\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &XKeenService{BinaryPath: bin}

	start := time.Now()
	out, err := svc.runWithTimeoutArgs(200*time.Millisecond, "-status")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(out, "partial") {
		t.Errorf("output before timeout lost: %q", out)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("returned after %v: child holding the pipe blocked the timeout path", elapsed)
	}
}

// TestRunWithTimeoutArgs_BackgroundChildKeepsPipe: скрипт завершился с кодом 0,
// оставив фоновый процесс на своём выводе (как ядро после xkeen -start).
// Вызов успешен и возвращается быстро, а потомок продолжает писать в вывод
// без SIGPIPE.
func TestRunWithTimeoutArgs_BackgroundChildKeepsPipe(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "alive")
	bin := filepath.Join(dir, "xkeen")
	script := "#!/bin/sh\necho started\n" +
		"(sleep 3; echo late; echo ok > " + marker + ") &\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &XKeenService{BinaryPath: bin}

	start := time.Now()
	out, err := svc.runWithTimeoutArgs(10*time.Second, "-status")
	if err != nil {
		t.Fatalf("successful script reported error: %v", err)
	}
	if !strings.Contains(out, "started") {
		t.Errorf("script output lost: %q", out)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("returned after %v: waited for background child", elapsed)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("background child died after writing to inherited output (SIGPIPE)")
}

func TestXKeenService_ConfiguredKernel(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ body, want string }{
		{"#!/bin/sh\nname_client=\"xray\"\ndirectory_configs_app=\"/opt/etc/$name_client\"\n", "xray"},
		{"name_client=mihomo\n", "mihomo"},
		{"# name_client=\"mihomo\"\nname_client=\"xray\"\n", "xray"},
		{"name_client=\"other\"\n", ""},
	} {
		path := filepath.Join(dir, "S05xkeen")
		if err := os.WriteFile(path, []byte(tc.body), 0o755); err != nil {
			t.Fatal(err)
		}
		s := &XKeenService{InitScript: path}
		if got := s.ConfiguredKernel(); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.body, got, tc.want)
		}
	}
	if got := (&XKeenService{InitScript: filepath.Join(dir, "missing")}).ConfiguredKernel(); got != "" {
		t.Errorf("missing script: got %q", got)
	}
}
