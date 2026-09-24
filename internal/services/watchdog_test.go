package services

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils/xtables"
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
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptables(t, saveOutput)
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	for i := 1; i <= watchdogMaxFailures; i++ {
		w.CheckHealth()
		if got := w.ConsecutiveFailures(); got != i {
			t.Fatalf("after check %d: expected %d consecutive failures, got %d", i, i, got)
		}
	}

	// EmergencyDisarmTProxy runs asynchronously and only latches `disarmed`
	// once it has confirmed the outcome (CR-01 fix), so poll for it instead
	// of asserting immediately. iptables-save/ip6tables-save are almost
	// certainly unavailable in CI/dev sandboxes, which disarmTProxyFamily
	// treats as "nothing to do here" (ok=true), so this should resolve to
	// disarmed=true quickly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		disarmed := w.disarmed
		inFlight := w.disarmInFlight
		w.mu.Unlock()
		if disarmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected watchdog to be disarmed after %d consecutive failures (disarmInFlight=%v)", watchdogMaxFailures, inFlight)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type fakeDialect int

const (
	dialectWaitSeconds fakeDialect = iota
	dialect1421
	dialectNoWait
	dialectLockBusy
)

type fakeIptablesConfig struct {
	SaveOutputs   []string
	Dialect       fakeDialect
	DeleteFailure string
}

// installFakeIptablesConfig installs a configurable fake iptables-save and iptables in a temp dir.
func installFakeIptablesConfig(t *testing.T, cfg fakeIptablesConfig) (saveBin, delBin, logPath string) {
	t.Helper()
	binDir := t.TempDir()
	logPath = filepath.Join(binDir, "deletions.log")

	saveOutputs := cfg.SaveOutputs
	if len(saveOutputs) == 0 {
		saveOutputs = []string{"*mangle\nCOMMIT\n"}
	}
	for i, out := range saveOutputs {
		outPath := filepath.Join(binDir, fmt.Sprintf("save_out_%d", i))
		if err := os.WriteFile(outPath, []byte(out), 0644); err != nil {
			t.Fatal(err)
		}
	}
	lastOutPath := filepath.Join(binDir, fmt.Sprintf("save_out_%d", len(saveOutputs)-1))

	saveBin = filepath.Join(binDir, "iptables-save")
	counterFile := filepath.Join(binDir, "save_counter")
	saveScript := fmt.Sprintf(`#!/bin/sh
CF="%s"
COUNT=0
if [ -f "$CF" ]; then
    COUNT=$(cat "$CF")
fi
echo "$((COUNT + 1))" > "$CF"
OUT="%s/save_out_$COUNT"
if [ ! -f "$OUT" ]; then
    OUT="%s"
fi
cat "$OUT"
`, counterFile, binDir, lastOutPath)

	if err := os.WriteFile(saveBin, []byte(saveScript), 0755); err != nil {
		t.Fatal(err)
	}

	delBin = filepath.Join(binDir, "iptables")
	delScriptBuilder := strings.Builder{}
	delScriptBuilder.WriteString("#!/bin/sh\n")

	switch cfg.Dialect {
	case dialectLockBusy:
		delScriptBuilder.WriteString(`echo "Another app is currently holding the xtables lock. Perhaps you want to use the -w option?" >&2
exit 4
`)
	case dialectNoWait:
		delScriptBuilder.WriteString(`for arg in "$@"; do
    if [ "$arg" = "-w" ]; then
        echo "iptables: unrecognized option '-w'" >&2
        exit 2
    fi
done
`)
	case dialect1421:
		delScriptBuilder.WriteString(`PREV=""
for arg in "$@"; do
    if [ "$PREV" = "-w" ] && [ "$arg" = "5" ]; then
        cat <<'EOF' >&2
Bad argument '5'
Try ` + "`" + `iptables -h' or 'iptables --help' for more information.
EOF
        exit 2
    fi
    PREV="$arg"
done
`)
	case dialectWaitSeconds:
		// accepts all options
	}

	if cfg.DeleteFailure != "" {
		delScriptBuilder.WriteString(fmt.Sprintf(`for arg in "$@"; do
    if [ "$arg" = "-D" ]; then
        echo "%s" >&2
        exit 1
    fi
done
`, cfg.DeleteFailure))
	}

	delScriptBuilder.WriteString(fmt.Sprintf(`echo "$@" >> "%s"
exit 0
`, logPath))

	if err := os.WriteFile(delBin, []byte(delScriptBuilder.String()), 0755); err != nil {
		t.Fatal(err)
	}

	return saveBin, delBin, logPath
}

// installFakeIptables writes a fake iptables-save (prints saveOutput until
// a -D deletion is logged by fake iptables, then clean mangle table to simulate successful removal)
// and a fake iptables that appends its invocation args as one line to a log file.
func installFakeIptables(t *testing.T, saveOutput string) (saveBin, delBin, logPath string) {
	t.Helper()
	binDir := t.TempDir()
	logPath = filepath.Join(binDir, "deletions.log")

	ruleOutPath := filepath.Join(binDir, "save_rule")
	if err := os.WriteFile(ruleOutPath, []byte(saveOutput), 0644); err != nil {
		t.Fatal(err)
	}
	cleanOutPath := filepath.Join(binDir, "save_clean")
	if err := os.WriteFile(cleanOutPath, []byte("*mangle\nCOMMIT\n"), 0644); err != nil {
		t.Fatal(err)
	}

	saveBin = filepath.Join(binDir, "iptables-save")
	saveScript := fmt.Sprintf(`#!/bin/sh
LP="%s"
if [ -f "$LP" ] && grep -q -- "-D" "$LP" 2>/dev/null; then
    cat "%s"
else
    cat "%s"
fi
`, logPath, cleanOutPath, ruleOutPath)
	if err := os.WriteFile(saveBin, []byte(saveScript), 0755); err != nil {
		t.Fatal(err)
	}

	delBin = filepath.Join(binDir, "iptables")
	delScript := fmt.Sprintf(`#!/bin/sh
echo "$@" >> "%s"
exit 0
`, logPath)
	if err := os.WriteFile(delBin, []byte(delScript), 0755); err != nil {
		t.Fatal(err)
	}

	return saveBin, delBin, logPath
}

// TestDisarmTProxyFamily_RealTargetInCustomChain verifies CR-03: XKeen's
// actual interception (per this project's own stability analysis) is a
// `-j TPROXY --on-port ...` rule, typically inside a custom chain jumped to
// from PREROUTING — not necessarily a chain literally named
// "XKEEN_TPROXY". disarmTProxyFamily must find and remove both the TPROXY
// rule itself and the PREROUTING jump into its custom chain.
func TestDisarmTProxyFamily_RealTargetInCustomChain(t *testing.T) {
	saveOutput := `# Generated by iptables-save
*mangle
:PREROUTING ACCEPT [0:0]
:xkeen - [0:0]
-A PREROUTING -j xkeen
-A xkeen -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
-A PREROUTING -j ACCEPT
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, saveOutput)

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed != 2 {
		t.Fatalf("expected 2 rules removed (the TPROXY rule + the jump into its chain), got %d", removed)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected deletions to have been invoked: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "-D xkeen") {
		t.Errorf("expected the TPROXY rule in the custom chain to be deleted, log:\n%s", log)
	}
	if !strings.Contains(log, "-D PREROUTING -j xkeen") {
		t.Errorf("expected the PREROUTING jump into the custom chain to be deleted, log:\n%s", log)
	}
	if strings.Contains(log, "-D PREROUTING -j ACCEPT") {
		t.Errorf("unrelated PREROUTING -j ACCEPT rule must not be touched, log:\n%s", log)
	}
}

// TestDisarmTProxyFamily_CustomChainGoto verifies IN-02:
// If a custom chain with TPROXY interception is entered via -g (goto) rather than -j,
// both the rule in the custom chain and the -g rule must be removed.
func TestDisarmTProxyFamily_CustomChainGoto(t *testing.T) {
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
:xkeen - [0:0]
-A PREROUTING -g xkeen
-A xkeen -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, saveOutput)

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed != 2 {
		t.Fatalf("expected 2 rules removed (the TPROXY rule + the -g goto into its chain), got %d", removed)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected deletions to have been invoked: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "-D xkeen") {
		t.Errorf("expected the TPROXY rule in the custom chain to be deleted, log:\n%s", log)
	}
	if !strings.Contains(log, "-D PREROUTING -g xkeen") {
		t.Errorf("expected the PREROUTING -g goto into the custom chain to be deleted, log:\n%s", log)
	}
}

// TestDisarmTProxyFamily_LiteralChainMarker verifies the fallback match on
// tproxyChainMarker still works for builds that do name a chain
// "XKEEN_TPROXY" literally.
func TestDisarmTProxyFamily_LiteralChainMarker(t *testing.T) {
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -j XKEEN_TPROXY
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, saveOutput)

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed != 1 {
		t.Fatalf("expected 1 rule removed, got %d", removed)
	}
	logData, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logData), "-D PREROUTING -j XKEEN_TPROXY") {
		t.Errorf("expected the literal XKEEN_TPROXY jump to be deleted, log:\n%s", string(logData))
	}
}

// TestDisarmTProxyFamily_NoMatch verifies unrelated mangle rules are left
// untouched and no deletions are attempted.
func TestDisarmTProxyFamily_NoMatch(t *testing.T) {
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -j ACCEPT
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, saveOutput)

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed != 0 {
		t.Fatalf("expected 0 rules removed, got %d", removed)
	}
	if _, err := os.Stat(logPath); err == nil {
		t.Error("expected no deletions to have been attempted for unrelated rules")
	}
}

// TestWatchdogService_EmergencyDisarmTProxy_FailureDoesNotLatch verifies the
// CR-01 fix: a failed disarm attempt must not permanently latch `disarmed`,
// so the circuit breaker can retry on a later qualifying health check
// instead of silently giving up after one transient failure.
func TestWatchdogService_EmergencyDisarmTProxy_FailureDoesNotLatch(t *testing.T) {
	removed, ok, _ := disarmTProxyFamily(context.Background(), "nonexistent-iptables-save", "iptables", []string{"-w", "5"})
	if removed != 0 {
		t.Fatalf("expected 0 rules removed when iptables-save is unavailable, got %d", removed)
	}
	if !ok {
		t.Fatalf("expected ok=true when the binary is simply missing (not a failure), got false")
	}

	// A binary that exists but always fails (permission error / lock
	// contention analogue) must report ok=false so the caller retries rather
	// than treating the attempt as a confirmed success.
	tmpDir := t.TempDir()
	failingSave := filepath.Join(tmpDir, "iptables-save")
	if err := os.WriteFile(failingSave, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = disarmTProxyFamily(context.Background(), failingSave, "iptables", []string{"-w", "5"})
	if ok {
		t.Fatalf("expected ok=false when %s exits non-zero", failingSave)
	}
}

// TestWatchdogService_Stop_WaitsForInFlightDisarm verifies the WR-01 fix:
// Stop() must wait for an in-flight EmergencyDisarmTProxy goroutine rather
// than abandoning it mid-sequence during graceful shutdown/restart. This is
// verified by pointing PATH at a deliberately slow fake iptables-save so the
// disarm goroutine is still running when Stop() is called, then asserting
// Stop() actually blocked for roughly that duration instead of returning
// immediately.
func TestWatchdogService_Stop_WaitsForInFlightDisarm(t *testing.T) {
	const disarmDelay = 200 * time.Millisecond

	ruleOutput := "*mangle\n:PREROUTING ACCEPT [0:0]\n-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1\nCOMMIT\n"
	binDir := t.TempDir()
	slowSave := filepath.Join(binDir, "iptables-save")
	script := fmt.Sprintf("#!/bin/sh\nsleep %.2f\ncat <<'EOF'\n%s\nEOF\n", disarmDelay.Seconds(), ruleOutput)
	if err := os.WriteFile(slowSave, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	// Trip the disarm directly (no need for Start()'s 30s ticker interval).
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}

	w.mu.Lock()
	inFlight := w.disarmInFlight
	w.mu.Unlock()
	if !inFlight {
		t.Fatal("expected disarmInFlight=true immediately after tripping the circuit breaker")
	}

	start := time.Now()
	w.Stop()
	elapsed := time.Since(start)

	if elapsed < disarmDelay/2 {
		t.Fatalf("Stop() returned after %v — expected it to block for roughly %v while the disarm goroutine was in flight (WR-01 regression: Stop() not waiting on the disarm goroutine)", elapsed, disarmDelay)
	}
}

// TestWatchdogService_Start_RunsFirstCheckImmediately verifies the WR-03
// fix: the first health check must not wait a full watchdogCheckInterval
// (30s) tick — it should run as soon as Start() launches the loop, so a
// kernel that's already wedged at boot is detected promptly.
func TestWatchdogService_Start_RunsFirstCheckImmediately(t *testing.T) {
	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	saveBin, delBin, _ := installFakeIptables(t, ruleOutput)
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	w.Start()
	defer w.Stop()

	deadline := time.Now().Add(1 * time.Second)
	for w.ConsecutiveFailures() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("expected the first health check to run promptly on Start(), well under watchdogCheckInterval")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatchdogService_CheckHealth_RecoveryResetsCounter(t *testing.T) {
	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	saveBin, delBin, _ := installFakeIptables(t, ruleOutput)
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

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
		"Служба XKeen активна": true,
		"Службы активны":       true,
		"Ядро активно":         true,
		"XKeen запущен":        true,
		"Служба запущена":      true,
		"XKeen is not running": false,
		"XKeen не запущен":     false,
		"XKeen незапущен":      false,
		"Служба не запущена":   false,
		"Служба незапущена":    false,
		"Служба не активна":    false,
		"Служба неактивна":     false,
		"Ядро не активно":      false,
		"Ядро неактивно":       false,
		"XKeen не активен":     false,
		"XKeen неактивен":      false,
		"XKeen остановлен":     false,
		"Службы остановлены":   false,
		"":                     false,
		"stopped":              false,
	}
	for status, want := range cases {
		if got := isKernelStatusHealthy(status); got != want {
			t.Errorf("isKernelStatusHealthy(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestSplitIptablesRule(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{
			input: `-A PREROUTING -j xkeen`,
			want:  []string{"-A", "PREROUTING", "-j", "xkeen"},
		},
		{
			input: `-A xkeen -p tcp -m comment --comment "XKeen TProxy Rule" -j TPROXY --on-port 7892`,
			want:  []string{"-A", "xkeen", "-p", "tcp", "-m", "comment", "--comment", "XKeen TProxy Rule", "-j", "TPROXY", "--on-port", "7892"},
		},
		{
			input: `-A xkeen -m comment --comment 'Single Quoted Comment' -j ACCEPT`,
			want:  []string{"-A", "xkeen", "-m", "comment", "--comment", "Single Quoted Comment", "-j", "ACCEPT"},
		},
		{
			input: `-A xkeen -m comment --comment "Escaped \"Quotes\"" -j ACCEPT`,
			want:  []string{"-A", "xkeen", "-m", "comment", "--comment", `Escaped "Quotes"`, "-j", "ACCEPT"},
		},
		{
			input: `  -A   PREROUTING   -j   xkeen  `,
			want:  []string{"-A", "PREROUTING", "-j", "xkeen"},
		},
		{
			input: ``,
			want:  nil,
		},
		{
			// WR-06 regression: an unpaired quote must not silently swallow
			// the rest of the line into one argument — the line is skipped
			// entirely (nil) rather than producing a malformed "-D ..." command.
			input: `-A xkeen -m comment --comment "Unterminated -j ACCEPT`,
			want:  nil,
		},
	}

	for _, c := range cases {
		got := splitIptablesRule(c.input)
		if len(got) != len(c.want) {
			t.Fatalf("splitIptablesRule(%q) length = %d, want %d: %v", c.input, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitIptablesRule(%q)[%d] = %q, want %q", c.input, i, got[i], c.want[i])
			}
		}
	}
}

func TestDisarmTProxyFamily_WithQuotedComments(t *testing.T) {
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -m comment --comment "XKeen TProxy PREROUTING" -j xkeen
-A xkeen -p tcp -m comment --comment "XKeen TProxy Interception" -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, saveOutput)

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed != 2 {
		t.Fatalf("expected 2 rules removed, got %d", removed)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected deletions to have been logged: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "XKeen TProxy Interception") {
		t.Errorf("expected comment to be preserved in deletion arguments, log:\n%s", log)
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

func TestEmergencyDisarmTProxy_EndToEnd_1421Dialect(t *testing.T) {
	xtables.ResetForTest()

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{saveOutput, "*mangle\nCOMMIT\n"},
		Dialect:     dialect1421,
	})

	binDir := filepath.Dir(saveBin)
	// Install ip6tables and ip6tables-save in binDir
	ip6Save := filepath.Join(binDir, "ip6tables-save")
	if err := os.WriteFile(ip6Save, []byte("#!/bin/sh\ncat <<'EOF'\n*mangle\nCOMMIT\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ip6Del := filepath.Join(binDir, "ip6tables")
	if err := os.WriteFile(ip6Del, []byte(fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s\nexit 0\n", logPath)), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tmpDir := t.TempDir()
	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = saveBin
	w.ip6tablesBin = ip6Del
	w.ip6tablesSaveBin = ip6Save

	outcome := w.EmergencyDisarmTProxy()
	if outcome != DisarmDisarmed {
		t.Fatalf("expected DisarmDisarmed on iptables 1.4.21, got %v", outcome)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	lines := strings.Split(string(logData), "\n")
	foundDelete := false
	for _, l := range lines {
		if strings.Contains(l, "-t mangle") && strings.Contains(l, "-D PREROUTING") {
			foundDelete = true
			tokens := strings.Fields(l)
			for i, tok := range tokens {
				if tok == "-w" {
					if i+1 < len(tokens) && tokens[i+1] == "5" {
						t.Fatalf("deletion command contains '5' after '-w': %s", l)
					}
					break
				}
			}
			if !strings.Contains(l, "-j TPROXY") {
				t.Fatalf("deletion command does not contain '-j TPROXY': %s", l)
			}
			break
		}
	}
	if !foundDelete {
		t.Fatalf("expected deletion command in log, got:\n%s", string(logData))
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

func TestEmergencyDisarmTProxy_ConfirmsViaReread(t *testing.T) {
	xtables.ResetForTest()

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	// Fake iptables-save returns the rule on every read, simulating a failure
	// to actually delete or a stubborn rule that persists despite iptables exiting 0.
	saveBin, delBin, _ := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{saveOutput, saveOutput, saveOutput},
		Dialect:     dialectWaitSeconds,
	})

	binDir := filepath.Dir(saveBin)
	ip6Save := filepath.Join(binDir, "ip6tables-save")
	if err := os.WriteFile(ip6Save, []byte("#!/bin/sh\ncat <<'EOF'\n*mangle\nCOMMIT\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ip6Del := filepath.Join(binDir, "ip6tables")
	if err := os.WriteFile(ip6Del, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tmpDir := t.TempDir()
	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = saveBin
	w.ip6tablesBin = ip6Del
	w.ip6tablesSaveBin = ip6Save

	outcome := w.EmergencyDisarmTProxy()
	if outcome != DisarmFailed {
		t.Fatalf("expected DisarmFailed when rule persists in mangle table, got %v", outcome)
	}

	w.mu.Lock()
	disarmed := w.disarmed
	w.mu.Unlock()
	if disarmed {
		t.Fatal("expected w.disarmed to remain false on failed disarm")
	}
}

func TestDisarmTProxyFamily_SecondPassCatchesRace(t *testing.T) {
	saveWithRule := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	cleanMangle := "*mangle\nCOMMIT\n"

	// First read: rule exists. Second read: rule still exists (simulating race/reinstall).
	// Third read: clean.
	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{saveWithRule, saveWithRule, cleanMangle},
		Dialect:     dialectWaitSeconds,
	})

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true after second pass cleared the rule")
	}
	if removed != 2 {
		t.Fatalf("expected removed=2 (1 per pass), got %d", removed)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	var delLines []string
	for _, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		if strings.TrimSpace(line) != "" {
			delLines = append(delLines, line)
		}
	}
	if len(delLines) != 2 {
		t.Fatalf("expected exactly 2 deletion attempts (no infinite retry loop), got %d:\n%s", len(delLines), string(logData))
	}
}

// TestDisarmTProxyFamily_ThreePhysicalDuplicatesFullyCleared verifies WR-02:
// when 3+ physically identical copies of the interception rule are present
// (e.g. XKeen re-installing its rule across a crash-restart loop before the
// watchdog catches up), the old implementation hard-capped retries at one
// extra pass and gave up (DisarmFailed) with a duplicate still active.
// selectTproxyRules dedups matching rule text into a single map entry, so
// each pass issues exactly one "-D" per unique rule text — but "-D" only
// removes one physical match, so 3 duplicates require 3 passes to fully
// clear. This must now loop until clean instead of stopping after 2 passes.
func TestDisarmTProxyFamily_ThreePhysicalDuplicatesFullyCleared(t *testing.T) {
	ruleLine := "-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1"
	mangleWithCopies := func(n int) string {
		var b strings.Builder
		b.WriteString("*mangle\n:PREROUTING ACCEPT [0:0]\n")
		for i := 0; i < n; i++ {
			b.WriteString(ruleLine)
			b.WriteString("\n")
		}
		b.WriteString("COMMIT\n")
		return b.String()
	}

	// Read 0 (initial): 3 physical copies. Read 1 (after pass 1): 2 copies
	// remain. Read 2 (after pass 2): 1 copy remains. Read 3 (after pass 3): clean.
	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{mangleWithCopies(3), mangleWithCopies(2), mangleWithCopies(1), mangleWithCopies(0)},
		Dialect:     dialectWaitSeconds,
	})

	removed, ok, _ := disarmTProxyFamily(context.Background(), saveBin, delBin, []string{"-w", "5"})
	if !ok {
		t.Fatal("expected ok=true once all 3 physical duplicates are cleared across passes")
	}
	if removed != 3 {
		t.Fatalf("expected removed=3 (1 per pass across 3 passes), got %d", removed)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	var delLines []string
	for _, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		if strings.TrimSpace(line) != "" {
			delLines = append(delLines, line)
		}
	}
	if len(delLines) != 3 {
		t.Fatalf("expected exactly 3 deletion attempts (one per physical duplicate), got %d:\n%s", len(delLines), string(logData))
	}
}

func TestDisarmTProxyFamily_MissingIP6Tables(t *testing.T) {
	tmpDir := t.TempDir()
	missingSave := filepath.Join(tmpDir, "nonexistent-ip6tables-save")
	missingDel := filepath.Join(tmpDir, "nonexistent-ip6tables")

	removed, ok, _ := disarmTProxyFamily(context.Background(), missingSave, missingDel, []string{"-w", "5"})
	if removed != 0 {
		t.Fatalf("expected removed=0 for missing binary, got %d", removed)
	}
	if !ok {
		t.Fatal("expected ok=true when ip6tables is not installed on system")
	}

	xtables.ResetForTest()
	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptables(t, saveOutput)
	binDir := filepath.Dir(saveBin)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = saveBin
	w.ip6tablesBin = missingDel
	w.ip6tablesSaveBin = missingSave

	outcome := w.EmergencyDisarmTProxy()
	if outcome != DisarmDisarmed {
		t.Fatalf("expected DisarmDisarmed when ipv4 succeeds even if ipv6 is missing, got %v", outcome)
	}
}

func TestEmergencyDisarmTProxy_AlreadyCleanOutcome(t *testing.T) {
	xtables.ResetForTest()

	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{"*mangle\n:PREROUTING ACCEPT [0:0]\n-A PREROUTING -j ACCEPT\nCOMMIT\n"},
		Dialect:     dialectWaitSeconds,
	})

	binDir := filepath.Dir(saveBin)
	ip6Save := filepath.Join(binDir, "ip6tables-save")
	if err := os.WriteFile(ip6Save, []byte("#!/bin/sh\ncat <<'EOF'\n*mangle\nCOMMIT\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ip6Del := filepath.Join(binDir, "ip6tables")
	if err := os.WriteFile(ip6Del, []byte(fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s\nexit 0\n", logPath)), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tmpDir := t.TempDir()
	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = saveBin
	w.ip6tablesBin = ip6Del
	w.ip6tablesSaveBin = ip6Save

	outcome := w.EmergencyDisarmTProxy()
	if outcome != DisarmAlreadyClean {
		t.Fatalf("expected DisarmAlreadyClean, got %v", outcome)
	}

	countDeletions := func() int {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return 0
		}
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "-D") {
				count++
			}
		}
		return count
	}

	if dels := countDeletions(); dels != 0 {
		t.Fatalf("expected 0 deletions on clean table, got %d", dels)
	}

	logLenBefore := 0
	if data, err := os.ReadFile(logPath); err == nil {
		logLenBefore = len(data)
	}

	// Idempotency: second call returns same outcome without deletions
	outcome2 := w.EmergencyDisarmTProxy()
	if outcome2 != DisarmAlreadyClean {
		t.Fatalf("expected second call to also return DisarmAlreadyClean, got %v", outcome2)
	}
	if dels := countDeletions(); dels != 0 {
		t.Fatalf("expected 0 deletions after second call, got %d", dels)
	}
	logLenAfter := 0
	if data, err := os.ReadFile(logPath); err == nil {
		logLenAfter = len(data)
	}
	if logLenAfter != logLenBefore {
		t.Fatalf("expected second call to produce no additional commands, log length grew from %d to %d", logLenBefore, logLenAfter)
	}

	w.mu.Lock()
	disarmed := w.disarmed
	w.mu.Unlock()
	if disarmed {
		t.Fatal("expected w.disarmed to remain false on direct EmergencyDisarmTProxy call without CheckHealth")
	}
}

// TestWatchdogService_CheckHealth_AlreadyCleanLatchesDisarmed verifies CR-01:
// when the kernel fails health checks but the mangle table is already clean
// at the moment of emergency disarm, CheckHealth() must latch w.disarmed=true
// so it does not spawn a new disarm sequence every 30s.
func TestWatchdogService_CheckHealth_AlreadyCleanLatchesDisarmed(t *testing.T) {
	xtables.ResetForTest()

	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	cleanOutput := "*mangle\nCOMMIT\n"

	tmpDir := t.TempDir()
	cleanSave, delBin, _ := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{ruleOutput, cleanOutput},
		Dialect:     dialectWaitSeconds,
	})

	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = cleanSave
	w.ip6tablesBin = delBin
	w.ip6tablesSaveBin = cleanSave

	for i := 1; i <= watchdogMaxFailures; i++ {
		w.CheckHealth()
	}

	// Wait for the async disarm goroutine to finish and latch disarmed
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		disarmed := w.disarmed
		inFlight := w.disarmInFlight
		w.mu.Unlock()
		if disarmed && !inFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected w.disarmed=true after DisarmAlreadyClean outcome (inFlight=%v)", inFlight)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Next failure check must NOT re-trigger disarm (remains latched)
	w.CheckHealth()
	w.mu.Lock()
	inFlight := w.disarmInFlight
	disarmed := w.disarmed
	w.mu.Unlock()
	if inFlight {
		t.Fatal("expected disarmInFlight=false on subsequent health check when already disarmed")
	}
	if !disarmed {
		t.Fatal("expected disarmed to remain true on subsequent health check")
	}
}

// TestWatchdogService_CheckHealth_ReinstalledRuleUnlatchesDisarmed verifies
// WR-03: once latched disarmed, the watchdog previously stayed silent
// forever unless the kernel reported a full recovery — even if the TPROXY
// interception rule was reinstalled by an external mechanism (e.g. a
// supervisor restart-looping a crashing XKeen) while the kernel remained
// unhealthy throughout. CheckHealth must periodically re-verify the mangle
// table non-destructively and unlatch (allowing a fresh disarm attempt) as
// soon as it finds the rule back, without waiting for xkeen -status to ever
// report healthy again.
func TestWatchdogService_CheckHealth_ReinstalledRuleUnlatchesDisarmed(t *testing.T) {
	xtables.ResetForTest()

	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	cleanOutput := "*mangle\nCOMMIT\n"

	// index0: rule present (consumed by initial CheckHealth check).
	// index1: clean (consumed by the initial DisarmAlreadyClean latch).
	// index2: rule present (consumed by the periodic non-destructive re-check
	//         that must trigger the unlatch).
	// index3: rule present (consumed by the fresh disarm attempt's own read).
	// index4: clean (consumed by that attempt's re-read confirming removal).
	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs: []string{ruleOutput, cleanOutput, ruleOutput, ruleOutput, cleanOutput},
		Dialect:     dialectWaitSeconds,
	})

	// ip6tables is always clean and separate from the shared iptables fake
	// above, so it never consumes from that sequence and never contributes
	// a deletion of its own.
	binDir := filepath.Dir(saveBin)
	ip6Save := filepath.Join(binDir, "ip6tables-save")
	if err := os.WriteFile(ip6Save, []byte("#!/bin/sh\ncat <<'EOF'\n*mangle\nCOMMIT\nEOF\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ip6Del := filepath.Join(binDir, "ip6tables")
	if err := os.WriteFile(ip6Del, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesBin = delBin
	w.iptablesSaveBin = saveBin
	w.ip6tablesBin = ip6Del
	w.ip6tablesSaveBin = ip6Save

	waitForLatch := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			w.mu.Lock()
			disarmed := w.disarmed
			inFlight := w.disarmInFlight
			w.mu.Unlock()
			if disarmed == want && !inFlight {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for disarmed=%v (last disarmed=%v inFlight=%v)", want, disarmed, inFlight)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Three consecutive failures trip the breaker; the mangle table is clean
	// at this point (index0), so the outcome is DisarmAlreadyClean and the
	// watchdog latches disarmed=true.
	for i := 1; i <= watchdogMaxFailures; i++ {
		w.CheckHealth()
	}
	waitForLatch(true)

	// Kernel never recovers, so nothing resets the latch via the healthy
	// branch. Drive disarmedRecheckInterval more unhealthy checks; the
	// disarmedRecheckInterval-th one must perform the non-destructive
	// re-check, find the reinstalled rule (index1), unlatch, and — because
	// consecutiveFailures is already well past watchdogMaxFailures — trigger
	// a fresh EmergencyDisarmTProxy attempt in the same call.
	for i := 1; i <= disarmedRecheckInterval; i++ {
		w.CheckHealth()
	}
	waitForLatch(true)

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read deletion log: %v", err)
	}
	var delLines []string
	for _, line := range strings.Split(strings.TrimSpace(string(logData)), "\n") {
		if strings.Contains(line, "-D") {
			delLines = append(delLines, line)
		}
	}
	if len(delLines) != 1 {
		t.Fatalf("expected exactly 1 deletion command for the reinstalled rule, got %d:\n%s", len(delLines), string(logData))
	}
}

// TestWatchdogService_Stop_Idempotent verifies IN-05:
// Calling Stop() multiple times must not panic on closing stopCh.
func TestWatchdogService_Stop_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	xkeenSvc := NewXKeenService(filepath.Join(tmpDir, "xkeen"), tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.Start()

	// Calling Stop() multiple times must not panic
	w.Stop()
	w.Stop()
	w.Stop()
}

// TestWatchdogService_CheckHealth_EpochDiscardsStaleInFlightDisarm verifies CR-01:
// If the kernel recovers while a slow EmergencyDisarmTProxy goroutine is in flight,
// the in-flight outcome must be discarded instead of latching w.disarmed=true.
// When a subsequent independent failure sequence occurs, a new disarm attempt
// must be permitted.
func TestWatchdogService_CheckHealth_EpochDiscardsStaleInFlightDisarm(t *testing.T) {
	const disarmDelay = 150 * time.Millisecond

	binDir := t.TempDir()
	slowSave := filepath.Join(binDir, "iptables-save")
	ruleOutput := "*mangle\n:PREROUTING ACCEPT [0:0]\n-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1\nCOMMIT\n"
	script := fmt.Sprintf("#!/bin/sh\nsleep %.2f\ncat <<'EOF'\n%s\nEOF\n", disarmDelay.Seconds(), ruleOutput)
	if err := os.WriteFile(slowSave, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "status.txt")
	if err := os.WriteFile(statusFile, []byte("XKeen is not running"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptBin := filepath.Join(tmpDir, "xkeen")
	wrapper := fmt.Sprintf("#!/bin/sh\ncat %s\nif grep -q 'running' %s; then exit 0; else exit 1; fi\n", statusFile, statusFile)
	if err := os.WriteFile(scriptBin, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	xkeenSvc := NewXKeenService(scriptBin, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	// Step 1: Trip the circuit breaker (3 failures)
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}

	w.mu.Lock()
	inFlight := w.disarmInFlight
	w.mu.Unlock()
	if !inFlight {
		t.Fatal("expected disarmInFlight=true after initial 3 failures")
	}

	// Step 2: Kernel recovers while disarm goroutine is still running!
	if err := os.WriteFile(statusFile, []byte("XKeen is running"), 0644); err != nil {
		t.Fatal(err)
	}
	w.CheckHealth()

	w.mu.Lock()
	failures := w.consecutiveFailures
	disarmed := w.disarmed
	w.mu.Unlock()
	if failures != 0 {
		t.Fatalf("expected 0 failures after recovery, got %d", failures)
	}
	if disarmed {
		t.Fatal("expected disarmed=false immediately after recovery")
	}

	// Step 3: Wait for in-flight disarm goroutine to complete
	deadline := time.Now().Add(1 * time.Second)
	for {
		w.mu.Lock()
		inFlight = w.disarmInFlight
		disarmed = w.disarmed
		w.mu.Unlock()
		if !inFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for in-flight disarm to finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Stale outcome must be discarded! disarmed must still be false!
	if disarmed {
		t.Fatal("CR-01 regression: stale in-flight disarm outcome latched disarmed=true after recovery")
	}

	// Step 4: Kernel fails again (new independent failure)
	if err := os.WriteFile(statusFile, []byte("XKeen is not running"), 0644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}

	w.mu.Lock()
	inFlight = w.disarmInFlight
	w.mu.Unlock()
	if !inFlight {
		t.Fatal("CR-01 regression: second failure cycle failed to trigger EmergencyDisarmTProxy because breaker was blocked")
	}

	w.Stop()
}

func waitDisarmSettled(t *testing.T, w *WatchdogService) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		inFlight := w.disarmInFlight
		w.mu.Unlock()
		if !inFlight {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for in-flight disarm to settle")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWatchdogService_GracePeriod(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)

	// Test 1: Service created without Start() (startedAt is zero).
	// 3 consecutive failures immediately trigger disarm attempt (existing behavior preserved).
	w1 := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptables(t, ruleOutput)
	w1.iptablesSaveBin = saveBin
	w1.iptablesBin = delBin
	w1.ip6tablesSaveBin = saveBin
	w1.ip6tablesBin = delBin

	for i := 0; i < watchdogMaxFailures; i++ {
		w1.CheckHealth()
	}
	waitDisarmSettled(t, w1)
	if w1.Snapshot().State != WatchdogStateDisarmed {
		t.Fatalf("expected unstarted watchdog to trigger disarm immediately upon 3 failures, got state %q", w1.Snapshot().State)
	}

	// Test 2: Service started via Start(). Simulated clock starts at t0.
	w2 := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	saveBin2, delBin2, _ := installFakeIptables(t, ruleOutput)
	w2.iptablesSaveBin = saveBin2
	w2.iptablesBin = delBin2
	w2.ip6tablesSaveBin = saveBin2
	w2.ip6tablesBin = delBin2

	simTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w2.now = func() time.Time { return simTime }

	w2.Start()
	defer w2.Stop()

	// Wait for the immediate first check from Start()
	deadline := time.Now().Add(1 * time.Second)
	for w2.ConsecutiveFailures() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for initial health check")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Health check failures within 90s (e.g. at t0 + 10s and t0 + 20s)
	simTime = simTime.Add(10 * time.Second)
	w2.CheckHealth()
	simTime = simTime.Add(10 * time.Second)
	w2.CheckHealth()

	if got := w2.ConsecutiveFailures(); got != 3 {
		t.Fatalf("expected 3 consecutive failures during grace period, got %d", got)
	}
	waitDisarmSettled(t, w2)
	if w2.Snapshot().State != WatchdogStateArmed {
		t.Fatalf("expected state to remain armed during grace period, got %q", w2.Snapshot().State)
	}
	w2.mu.Lock()
	inFlight := w2.disarmInFlight
	disarmed := w2.disarmed
	w2.mu.Unlock()
	if inFlight || disarmed {
		t.Fatalf("expected no disarm attempt triggered within grace period (inFlight=%v, disarmed=%v)", inFlight, disarmed)
	}

	// Test 3: Advance clock past grace period (> 90s, e.g. t0 + 95s).
	// Next failed check should trigger disarm!
	simTime = simTime.Add(75 * time.Second) // total t0 + 95s
	w2.CheckHealth()
	waitDisarmSettled(t, w2)
	if w2.Snapshot().State != WatchdogStateDisarmed {
		t.Fatalf("expected state to become disarmed after grace period expires, got %q", w2.Snapshot().State)
	}
}

func TestWatchdogService_BackoffGrid(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	// Config with DeleteFailure so disarm always fails (returns DisarmFailed)
	saveBin, delBin, _ := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs:   []string{saveOutput},
		Dialect:       dialectWaitSeconds,
		DeleteFailure: "failed to delete rule",
	})
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	t0 := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	currTime := t0
	w.now = func() time.Time { return currTime }

	// Trip circuit breaker with 3 failures
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}
	waitDisarmSettled(t, w)

	// Attempt 1 happened at t0
	snap := w.Snapshot()
	if snap.DisarmAttempts != 1 {
		t.Fatalf("expected 1 disarm attempt, got %d", snap.DisarmAttempts)
	}
	expectedNext1 := t0.Add(30 * time.Second)
	if !snap.NextAttemptAt.Equal(expectedNext1) {
		t.Fatalf("expected next attempt at %v, got %v", expectedNext1, snap.NextAttemptAt)
	}

	// Test 4: check before 30s (e.g. +10s) -> should NOT trigger attempt
	currTime = t0.Add(10 * time.Second)
	w.CheckHealth()
	waitDisarmSettled(t, w)
	if w.Snapshot().DisarmAttempts != 1 {
		t.Fatalf("check before backoff interval must not trigger attempt, got %d attempts", w.Snapshot().DisarmAttempts)
	}

	// Test 4 cont: check after 30s (at +30s) -> should trigger Attempt 2
	currTime = t0.Add(30 * time.Second)
	w.CheckHealth()
	waitDisarmSettled(t, w)
	snap = w.Snapshot()
	if snap.DisarmAttempts != 2 {
		t.Fatalf("expected 2 disarm attempts at +30s, got %d", snap.DisarmAttempts)
	}
	expectedNext2 := currTime.Add(time.Minute) // t0 + 30s + 1m = t0 + 1m30s
	if !snap.NextAttemptAt.Equal(expectedNext2) {
		t.Fatalf("expected next attempt at %v, got %v", expectedNext2, snap.NextAttemptAt)
	}

	// Attempt 3: at +1m30s (t0 + 90s)
	currTime = expectedNext2
	w.CheckHealth()
	waitDisarmSettled(t, w)
	snap = w.Snapshot()
	if snap.DisarmAttempts != 3 {
		t.Fatalf("expected 3 disarm attempts, got %d", snap.DisarmAttempts)
	}
	expectedNext3 := currTime.Add(2 * time.Minute) // t0 + 1m30s + 2m = t0 + 3m30s
	if !snap.NextAttemptAt.Equal(expectedNext3) {
		t.Fatalf("expected next attempt at %v, got %v", expectedNext3, snap.NextAttemptAt)
	}

	// Attempt 4: at +3m30s (t0 + 210s)
	currTime = expectedNext3
	w.CheckHealth()
	waitDisarmSettled(t, w)
	snap = w.Snapshot()
	if snap.DisarmAttempts != 4 {
		t.Fatalf("expected 4 disarm attempts, got %d", snap.DisarmAttempts)
	}
	expectedNext4 := currTime.Add(5 * time.Minute) // t0 + 3m30s + 5m = t0 + 8m30s
	if !snap.NextAttemptAt.Equal(expectedNext4) {
		t.Fatalf("expected next attempt at %v, got %v", expectedNext4, snap.NextAttemptAt)
	}

	// Test 6: IPv4 succeeds, IPv6 fails -> lastDisarmError contains "IPv4 disarmed" and "IPv6 failed"
	cleanSave, cleanDel, _ := installFakeIptables(t, "*mangle\nCOMMIT\n")
	w.iptablesSaveBin = cleanSave
	w.iptablesBin = cleanDel
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	outcome := w.EmergencyDisarmTProxy()
	if outcome != DisarmFailed {
		t.Fatalf("expected DisarmFailed when IPv6 fails, got %v", outcome)
	}
	lastErr := w.Snapshot().LastDisarmError
	if !strings.Contains(lastErr, "IPv4 disarmed") || !strings.Contains(lastErr, "IPv6 failed") {
		t.Fatalf("expected LastDisarmError to contain 'IPv4 disarmed' and 'IPv6 failed', got: %q", lastErr)
	}
}

func TestWatchdogService_Degraded(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, logPath := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs:   []string{saveOutput},
		Dialect:       dialectWaitSeconds,
		DeleteFailure: "failed to delete rule",
	})
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	t0 := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	currTime := t0
	w.now = func() time.Time { return currTime }

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
	})

	// Run health checks through all 5 disarm attempts:
	// Trip breaker: 3 failures
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}
	waitDisarmSettled(t, w)
	// Attempts 2 to 5 using backoff intervals: [30s, 1m, 2m, 5m]
	intervals := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	for i, delay := range intervals {
		currTime = currTime.Add(delay)
		w.CheckHealth()
		waitDisarmSettled(t, w)
		if w.Snapshot().DisarmAttempts != i+2 {
			t.Fatalf("expected attempt %d, got %d", i+2, w.Snapshot().DisarmAttempts)
		}
	}

	// Test 1: Fifth consecutive failed attempt sets degradedAt and State becomes degraded
	snap := w.Snapshot()
	if snap.State != WatchdogStateDegraded {
		t.Fatalf("expected state %q, got %q", WatchdogStateDegraded, snap.State)
	}
	if snap.DegradedAt.IsZero() {
		t.Fatal("expected degradedAt to be non-zero")
	}

	// Test 4: Entering degraded logged exactly one summary line
	logOutput := logBuf.String()
	degradedLogs := 0
	for _, l := range strings.Split(logOutput, "\n") {
		if strings.Contains(l, "entered degraded state after 5 failed emergency disarm attempt(s)") {
			degradedLogs++
			if !strings.Contains(l, snap.LastDisarmError) {
				t.Fatalf("summary line must contain last error text, line: %s", l)
			}
		}
	}
	if degradedLogs != 1 {
		t.Fatalf("expected exactly 1 degraded summary line, got %d", degradedLogs)
	}

	// Record iptables log size and app log lines count before the 20 subsequent checks
	iptablesLogBefore, _ := os.ReadFile(logPath)
	logLinesBefore := len(strings.Split(strings.TrimSpace(logBuf.String()), "\n"))

	// Test 2 & 3: Subsequent 20 failed health checks in degraded do NOT call iptables and add 0 log lines
	for i := 0; i < 20; i++ {
		currTime = currTime.Add(watchdogCheckInterval)
		w.CheckHealth()
	}

	iptablesLogAfter, _ := os.ReadFile(logPath)
	if len(iptablesLogAfter) != len(iptablesLogBefore) {
		t.Fatalf("iptables calls must not grow in degraded state (was %d bytes, now %d bytes)",
			len(iptablesLogBefore), len(iptablesLogAfter))
	}

	logLinesAfter := len(strings.Split(strings.TrimSpace(logBuf.String()), "\n"))
	if logLinesAfter != logLinesBefore {
		t.Fatalf("no log lines should be added in degraded state (was %d lines, now %d lines)",
			logLinesBefore, logLinesAfter)
	}
}

func TestWatchdogService_DegradedRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs:   []string{saveOutput},
		Dialect:       dialectWaitSeconds,
		DeleteFailure: "failed to delete rule",
	})
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	t0 := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	currTime := t0
	w.now = func() time.Time { return currTime }

	// Trip circuit breaker and exhaust all 5 attempts into degraded
	for i := 0; i < watchdogMaxFailures; i++ {
		w.CheckHealth()
	}
	waitDisarmSettled(t, w)
	intervals := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	for _, delay := range intervals {
		currTime = currTime.Add(delay)
		w.CheckHealth()
		waitDisarmSettled(t, w)
	}

	if w.Snapshot().State != WatchdogStateDegraded {
		t.Fatalf("expected state %q, got %q", WatchdogStateDegraded, w.Snapshot().State)
	}

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
	})

	// Test 5: Kernel recovers!
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is running\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	w.CheckHealth()

	snap := w.Snapshot()
	if snap.State != WatchdogStateArmed {
		t.Fatalf("expected state to return to %q, got %q", WatchdogStateArmed, snap.State)
	}
	if !snap.DegradedAt.IsZero() {
		t.Fatalf("expected degradedAt to be cleared, got %v", snap.DegradedAt)
	}
	if snap.DisarmAttempts != 0 {
		t.Fatalf("expected disarmAttempts to be 0, got %d", snap.DisarmAttempts)
	}
	if !snap.NextAttemptAt.IsZero() {
		t.Fatalf("expected nextAttemptAt to be cleared, got %v", snap.NextAttemptAt)
	}
	if snap.LastDisarmError != "" {
		t.Fatalf("expected lastDisarmError to be empty, got %q", snap.LastDisarmError)
	}

	// Assert one recovery line was logged mentioning degraded
	recoveryFound := false
	for _, l := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(l, "kernel recovered from degraded state") {
			recoveryFound = true
			break
		}
	}
	if !recoveryFound {
		t.Fatalf("expected recovery log line mentioning degraded state, got:\n%s", logBuf.String())
	}
}

func TestWatchdogService_StableFailureLine(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptables(t, ruleOutput)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
	})

	stripTimestamp := func(line string) string {
		if len(line) > 20 && line[4] == '/' && line[7] == '/' && line[10] == ' ' && line[13] == ':' {
			return line[20:]
		}
		return line
	}

	collectUniqueFailureLines := func(text string) map[string]bool {
		unique := make(map[string]bool)
		for _, l := range strings.Split(text, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || !strings.Contains(l, "kernel health check failed") {
				continue
			}
			unique[stripTimestamp(l)] = true
		}
		return unique
	}

	// Run 4 failures (past threshold of 3)
	for i := 0; i < 4; i++ {
		w.CheckHealth()
	}
	waitDisarmSettled(t, w)

	uniqueAt4 := collectUniqueFailureLines(logBuf.String())
	if len(uniqueAt4) != watchdogMaxFailures {
		t.Fatalf("expected %d unique failure lines at 4 failures, got %d: %v",
			watchdogMaxFailures, len(uniqueAt4), uniqueAt4)
	}

	// Run 36 more failures (total 40)
	for i := 0; i < 36; i++ {
		w.CheckHealth()
	}
	waitDisarmSettled(t, w)

	uniqueAt40 := collectUniqueFailureLines(logBuf.String())
	if len(uniqueAt40) != len(uniqueAt4) {
		t.Fatalf("failure lines must be stable and not grow: had %d unique lines, now %d: %v",
			len(uniqueAt4), len(uniqueAt40), uniqueAt40)
	}
}

func TestWatchdogService_SteadyStateLogLines(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	saveOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptablesConfig(t, fakeIptablesConfig{
		SaveOutputs:   []string{saveOutput},
		Dialect:       dialectWaitSeconds,
		DeleteFailure: "failed to delete rule",
	})
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
	})

	stripTimestamp := func(line string) string {
		if len(line) > 20 && line[4] == '/' && line[7] == '/' && line[10] == ' ' && line[13] == ':' {
			return line[20:]
		}
		return line
	}

	linesCountAtIteration1880 := 0
	const totalIterations = 2880 // 24 hours * (60m / 0.5m) = 2880 checks at 30s interval

	for i := 1; i <= totalIterations; i++ {
		w.CheckHealth()
		waitDisarmSettled(t, w)
		currTime = currTime.Add(watchdogCheckInterval)

		if i == totalIterations-1000 {
			linesCountAtIteration1880 = len(strings.Split(strings.TrimSpace(logBuf.String()), "\n"))
		}
	}

	allLines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	totalLines := len(allLines)

	// Assertion 1: Total lines <= 40
	if totalLines > 40 {
		t.Fatalf("expected at most 40 log lines over 24h simulation, got %d:\n%s", totalLines, logBuf.String())
	}

	// Assertion 2: 0 lines added during the last 1000 iterations
	if totalLines != linesCountAtIteration1880 {
		t.Fatalf("expected 0 log lines added during the last 1000 iterations, but grew from %d to %d",
			linesCountAtIteration1880, totalLines)
	}

	// Assertion 3: Unique lines check — no growing counter generating unique lines
	uniqueNormalized := make(map[string]bool)
	for _, l := range allLines {
		norm := stripTimestamp(l)
		uniqueNormalized[norm] = true
	}
	if len(uniqueNormalized) > 15 {
		t.Fatalf("too many unique lines (%d), possible growing counter leak: %v",
			len(uniqueNormalized), uniqueNormalized)
	}

	// Assertion 4: Snapshot state is degraded and DisarmAttempts is 5
	snap := w.Snapshot()
	if snap.State != WatchdogStateDegraded {
		t.Fatalf("expected final state %q, got %q", WatchdogStateDegraded, snap.State)
	}
	if snap.DisarmAttempts != 5 {
		t.Fatalf("expected 5 disarm attempts, got %d", snap.DisarmAttempts)
	}
}

// TestWatchdogService_IntentionalStop verifies scenarios 1, 2, and 4:
// A stopped kernel with clean mangle table enters idle state, consecutive failures
// remain zero, no disarm commands are issued, and exactly one log line is emitted over 20 cycles.
func TestWatchdogService_IntentionalStop(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	dummyScript := `#!/bin/sh
if [ "$1" = "-status" ]; then
    echo "XKeen is not running"
    exit 1
fi
exit 0
`
	if err := os.WriteFile(dummy, []byte(dummyScript), 0755); err != nil {
		t.Fatal(err)
	}

	cleanSave, delBin, logPath := installFakeIptables(t, "*mangle\nCOMMIT\n")

	// Scenario 1 & 4: Intentional stop via panel (xkeenSvc.Stop()), clean mangle table
	xkeenSvc := NewXKeenService(dummy, tmpDir)
	if _, err := xkeenSvc.Stop(); err != nil {
		t.Fatal(err)
	}
	if !xkeenSvc.IntentionalStop() {
		t.Fatal("expected IntentionalStop=true after xkeenSvc.Stop()")
	}

	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = cleanSave
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = cleanSave
	w.ip6tablesBin = delBin

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	for i := 0; i < 20; i++ {
		w.CheckHealth()
	}

	if w.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures in idle, got %d", w.ConsecutiveFailures())
	}
	if snap := w.Snapshot(); snap.State != WatchdogStateIdle {
		t.Fatalf("expected state %q, got %q", WatchdogStateIdle, snap.State)
	}

	// Subprocess iptables deletion calls must be 0
	if data, err := os.ReadFile(logPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		t.Fatalf("expected no deletions in idle mode, got: %s", string(data))
	}

	// Exactly one log line across 20 cycles
	logLines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	var idleLines []string
	for _, l := range logLines {
		if strings.Contains(l, "kernel stopped intentionally, disarm disabled") {
			idleLines = append(idleLines, l)
		}
	}
	if len(idleLines) != 1 {
		t.Fatalf("expected exactly 1 idle log line across 20 cycles, got %d:\n%s", len(idleLines), logBuf.String())
	}

	// Scenario 2: Stopped via console without panel (intentionalStop=false), but clean mangle table
	xkeenSvcConsole := NewXKeenService(dummy, tmpDir)
	if xkeenSvcConsole.IntentionalStop() {
		t.Fatal("expected IntentionalStop=false for fresh service instance")
	}
	wConsole := NewWatchdogService(xkeenSvcConsole, tmpDir, tmpDir)
	wConsole.iptablesSaveBin = cleanSave
	wConsole.iptablesBin = delBin
	wConsole.ip6tablesSaveBin = cleanSave
	wConsole.ip6tablesBin = delBin

	wConsole.CheckHealth()
	if wConsole.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 consecutive failures for console stop, got %d", wConsole.ConsecutiveFailures())
	}
	if snap := wConsole.Snapshot(); snap.State != WatchdogStateIdle {
		t.Fatalf("expected idle state for console stop, got %q", snap.State)
	}
}

// TestWatchdogService_IntentionalStopOverriddenByRules verifies scenario 3 (D-03):
// When intentionalStop is active but TPROXY rules are present in mangle,
// intentionalStop is cleared immediately, treated as an incident, and consecutiveFailures
// increments until emergency disarm is triggered.
func TestWatchdogService_IntentionalStopOverriddenByRules(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	dummyScript := `#!/bin/sh
if [ "$1" = "-status" ]; then
    echo "XKeen is not running"
    exit 1
fi
exit 0
`
	if err := os.WriteFile(dummy, []byte(dummyScript), 0755); err != nil {
		t.Fatal(err)
	}

	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, logPath := installFakeIptables(t, ruleOutput)

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	if _, err := xkeenSvc.Stop(); err != nil {
		t.Fatal(err)
	}
	if !xkeenSvc.IntentionalStop() {
		t.Fatal("expected IntentionalStop=true before check")
	}

	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	// Check 1: rules present override intentionalStop
	w.CheckHealth()

	if xkeenSvc.IntentionalStop() {
		t.Fatal("D-03 failure: expected intentionalStop to be cleared by rules presence")
	}
	if snap := w.Snapshot(); snap.State == WatchdogStateIdle {
		t.Fatal("expected state not to be idle when TPROXY rules are active")
	}
	if got := w.ConsecutiveFailures(); got != 1 {
		t.Fatalf("expected failure counter to increment to 1, got %d", got)
	}

	// Check 2 and 3: continue failure streak to trigger disarm
	w.CheckHealth()
	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 3 {
		t.Fatalf("expected failure counter 3, got %d", got)
	}

	waitDisarmSettled(t, w)

	if snap := w.Snapshot(); snap.State != WatchdogStateDisarmed {
		t.Fatalf("expected state %q after emergency disarm, got %q", WatchdogStateDisarmed, snap.State)
	}

	delData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read deletions log: %v", err)
	}
	if !strings.Contains(string(delData), "-D") {
		t.Fatalf("expected -D deletion command in log, got:\n%s", string(delData))
	}

	if !strings.Contains(logBuf.String(), "TPROXY interception rules present while kernel stopped — treating as incident") {
		t.Fatalf("expected D-03 incident log line, got:\n%s", logBuf.String())
	}
}

// TestWatchdogService_RestartWindowSuppressesFailures verifies scenario 5 (D-06):
// During the maintenance restart window, consecutiveFailures does not increment.
// Once the window expires, failures resume incrementing normally.
func TestWatchdogService_RestartWindowSuppressesFailures(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	script := `#!/bin/sh
if [ "$1" = "-restart" ]; then
    echo "Restarting XKeen..."
    exit 0
fi
echo "XKeen is not running"
exit 1
`
	if err := os.WriteFile(dummy, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	ruleOutput := `*mangle
:PREROUTING ACCEPT [0:0]
-A PREROUTING -p tcp -j TPROXY --on-port 7892 --on-ip 127.0.0.1 --tproxy-mark 0x1/0x1
COMMIT
`
	saveBin, delBin, _ := installFakeIptables(t, ruleOutput)

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	simTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	xkeenSvc.now = func() time.Time { return simTime }

	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.now = func() time.Time { return simTime }
	w.iptablesSaveBin = saveBin
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = saveBin
	w.ip6tablesBin = delBin

	// Trigger Restart to activate restart window
	if _, err := xkeenSvc.Restart(); err != nil {
		t.Fatal(err)
	}
	if !xkeenSvc.InRestart() {
		t.Fatal("expected InRestart=true immediately after Restart()")
	}

	// 5 checks during the restart window: consecutiveFailures must remain 0
	for i := 0; i < 5; i++ {
		simTime = simTime.Add(10 * time.Second) // total 50s, window is 60s
		w.CheckHealth()
		if got := w.ConsecutiveFailures(); got != 0 {
			t.Fatalf("expected 0 failures during restart window at step %d, got %d", i, got)
		}
	}

	// Advance time past the 60s restart window
	simTime = simTime.Add(20 * time.Second) // now 70s > 60s
	if xkeenSvc.InRestart() {
		t.Fatal("expected InRestart=false after restart window elapsed")
	}

	// Health checks now increment failure counter
	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 1 {
		t.Fatalf("expected failures=1 after window expired, got %d", got)
	}
	w.CheckHealth()
	if got := w.ConsecutiveFailures(); got != 2 {
		t.Fatalf("expected failures=2, got %d", got)
	}
}

// TestWatchdogService_IdleRecoversOnHealthyKernel verifies scenario 6 (D-07):
// Recovering from idle on a healthy kernel returns state to armed, clears intentionalStop,
// and logs a single line about resuming guard.
func TestWatchdogService_IdleRecoversOnHealthyKernel(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	statusFile := filepath.Join(tmpDir, "status.txt")
	if err := os.WriteFile(statusFile, []byte("XKeen is not running"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptBin := filepath.Join(tmpDir, "xkeen")
	wrapper := fmt.Sprintf("#!/bin/sh\ncat %s\nif grep -q 'running' %s; then exit 0; else exit 1; fi\n", statusFile, statusFile)
	if err := os.WriteFile(scriptBin, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}

	cleanSave, delBin, _ := installFakeIptables(t, "*mangle\nCOMMIT\n")

	xkeenSvc := NewXKeenService(scriptBin, tmpDir)
	if _, err := xkeenSvc.Stop(); err != nil {
		t.Fatal(err)
	}

	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)
	w.iptablesSaveBin = cleanSave
	w.iptablesBin = delBin
	w.ip6tablesSaveBin = cleanSave
	w.ip6tablesBin = delBin

	// Enter idle
	w.CheckHealth()
	if snap := w.Snapshot(); snap.State != WatchdogStateIdle {
		t.Fatalf("expected idle state, got %q", snap.State)
	}
	if !xkeenSvc.IntentionalStop() {
		t.Fatal("expected intentionalStop=true while in idle")
	}

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	// Kernel recovers
	if err := os.WriteFile(statusFile, []byte("XKeen is running"), 0644); err != nil {
		t.Fatal(err)
	}

	w.CheckHealth()

	if snap := w.Snapshot(); snap.State != WatchdogStateArmed {
		t.Fatalf("expected state %q after recovery, got %q", WatchdogStateArmed, snap.State)
	}
	if xkeenSvc.IntentionalStop() {
		t.Fatal("D-07 failure: expected intentionalStop to be cleared upon healthy recovery")
	}

	if !strings.Contains(logBuf.String(), "kernel recovered from idle state — resuming guard") {
		t.Fatalf("expected recovery log line about resuming guard, got:\n%s", logBuf.String())
	}
}

func TestWatchdogService_ConsecutiveFailuresCapped(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is not running\"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	// In degraded state or sustained failure, consecutiveFailures must not grow beyond watchdogMaxFailures
	w.mu.Lock()
	w.degradedAt = time.Now()
	w.consecutiveFailures = watchdogMaxFailures
	w.mu.Unlock()

	for i := 0; i < 10; i++ {
		w.CheckHealth()
	}

	if got := w.ConsecutiveFailures(); got != watchdogMaxFailures {
		t.Fatalf("expected consecutiveFailures capped at %d, got %d", watchdogMaxFailures, got)
	}

	if snap := w.Snapshot(); snap.ConsecutiveFailures != watchdogMaxFailures {
		t.Fatalf("expected snapshot ConsecutiveFailures capped at %d, got %d", watchdogMaxFailures, snap.ConsecutiveFailures)
	}
}

func TestWatchdogService_TryResetClearsInterceptionState(t *testing.T) {
	xtables.ResetForTest()

	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	if err := os.WriteFile(dummy, []byte("#!/bin/sh\necho \"XKeen is running\"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	xkeenSvc := NewXKeenService(dummy, tmpDir)
	w := NewWatchdogService(xkeenSvc, tmpDir, tmpDir)

	w.mu.Lock()
	w.interceptionActive = true
	w.interceptionFamily = "ipv4"
	w.consecutiveFailures = 3
	w.disarmed = true
	w.degradedAt = time.Now()
	w.mu.Unlock()

	snap, err := w.TryReset()
	if err != nil {
		t.Fatalf("unexpected error from TryReset: %v", err)
	}

	if snap.InterceptionActive {
		t.Errorf("expected InterceptionActive=false in TryReset snapshot, got true")
	}
	if snap.InterceptionFamily != "" {
		t.Errorf("expected InterceptionFamily='' in TryReset snapshot, got %q", snap.InterceptionFamily)
	}
	if snap.State != WatchdogStateArmed {
		t.Errorf("expected state %q, got %q", WatchdogStateArmed, snap.State)
	}
	if snap.ConsecutiveFailures != 0 {
		t.Errorf("expected ConsecutiveFailures=0, got %d", snap.ConsecutiveFailures)
	}

	w.wg.Wait()
}

// TestWatchdogService_ReportRoutingIssues_LogsOnlyOnChange: неизменный набор
// проблем маршрутизации пишется в лог один раз, а не на каждой проверке.
func TestWatchdogService_ReportRoutingIssues_LogsOnlyOnChange(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	w := NewWatchdogService(nil, "", "")
	issueA := []string{`05_routing.json: outboundTag "a" not declared`}
	issueB := []string{`05_routing.json: outboundTag "b" not declared`}

	steps := []struct {
		issues  []string
		wantLog string
	}{
		{nil, ""},
		{issueA, "found 1 issue(s)"},
		{issueA, ""},
		{issueA, ""},
		{issueB, `"b"`},
		{nil, "issues resolved"},
		{nil, ""},
	}
	for i, st := range steps {
		buf.Reset()
		w.reportRoutingIssues(st.issues)
		got := buf.String()
		if st.wantLog == "" && got != "" {
			t.Errorf("step %d: expected no log, got %q", i, got)
		}
		if st.wantLog != "" && !strings.Contains(got, st.wantLog) {
			t.Errorf("step %d: expected log containing %q, got %q", i, st.wantLog, got)
		}
	}
}
