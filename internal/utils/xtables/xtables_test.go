package xtables

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeIptablesDialect(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "iptables")
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binPath
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWaitArgs_FullDialect(t *testing.T) {
	ResetForTest()
	script := "#!/bin/sh\nexit 0\n"
	writeFakeIptablesDialect(t, script)

	args := WaitArgs(context.Background())
	expected := []string{"-w", "5"}
	if !equalSlices(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}

	mu.Lock()
	good := haveGood["iptables"]
	mu.Unlock()
	if !good {
		t.Fatal("expected haveGood=true after successful probe")
	}
}

func TestWaitArgs_142xDialect(t *testing.T) {
	ResetForTest()
	script := `#!/bin/sh
PREV=""
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
exit 0
`
	writeFakeIptablesDialect(t, script)

	args := WaitArgs(context.Background())
	expected := []string{"-w"}
	if !equalSlices(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}

	mu.Lock()
	good := haveGood["iptables"]
	mu.Unlock()
	if !good {
		t.Fatal("expected haveGood=true after successful probe")
	}
}

func TestWaitArgs_LockBusy(t *testing.T) {
	ResetForTest()
	busyScript := `#!/bin/sh
echo "Another app is currently holding the xtables lock. Perhaps you want to use the -w option?" >&2
exit 4
`
	writeFakeIptablesDialect(t, busyScript)

	args := WaitArgs(context.Background())
	// (1) WaitArgs does not return a result richer than first failed variant (returns [])
	if len(args) > 0 {
		t.Fatalf("expected empty/unranked args on lock contention, got %v", args)
	}

	// (2) internal flag haveGood remains false
	mu.Lock()
	good := haveGood["iptables"]
	mu.Unlock()
	if good {
		t.Fatal("expected haveGood=false when probe hit xtables lock busy")
	}

	// (3) after swapping with always-successful script, next call re-probes and returns full dialect
	okScript := "#!/bin/sh\nexit 0\n"
	writeFakeIptablesDialect(t, okScript)

	args2 := WaitArgs(context.Background())
	expected := []string{"-w", "5"}
	if !equalSlices(args2, expected) {
		t.Fatalf("expected %v after recovery from busy lock, got %v", expected, args2)
	}

	mu.Lock()
	good2 := haveGood["iptables"]
	mu.Unlock()
	if !good2 {
		t.Fatal("expected haveGood=true after successful recovery probe")
	}
}

func TestWaitArgs_MissingBinary(t *testing.T) {
	ResetForTest()
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	args := WaitArgs(context.Background())
	if len(args) != 0 {
		t.Fatalf("expected empty slice when iptables is missing, got %v", args)
	}

	mu.Lock()
	good := haveGood["iptables"]
	mu.Unlock()
	if good {
		t.Fatal("expected haveGood=false when iptables is missing")
	}

	// Next call should try to probe again (not cached)
	args2 := WaitArgs(context.Background())
	if len(args2) != 0 {
		t.Fatalf("expected empty slice on second attempt when iptables is missing, got %v", args2)
	}
}

func TestWaitArgs_CachesAfterSuccess(t *testing.T) {
	ResetForTest()
	dir := t.TempDir()
	counterFile := filepath.Join(dir, "invocations.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"run\" >> %s\nexit 0\n", counterFile)
	writeFakeIptablesDialect(t, script)

	args1 := WaitArgs(context.Background())
	expected := []string{"-w", "5"}
	if !equalSlices(args1, expected) {
		t.Fatalf("first call: expected %v, got %v", expected, args1)
	}

	args2 := WaitArgs(context.Background())
	if !equalSlices(args2, expected) {
		t.Fatalf("second call: expected %v, got %v", expected, args2)
	}

	data, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 probe execution, got %d (invocations: %v)", len(lines), lines)
	}
}

// TestWaitArgs_AdvancesOnUnrecognizedError verifies WR-01:
// When the probe candidate fails with an error text that is not a standard arg-parse
// string (e.g. 1.4.21 treating '5' as a non-existent chain name), the probe ladder
// must not bail out immediately — it must continue to the next candidate (e.g. -w).
func TestWaitArgs_AdvancesOnUnrecognizedError(t *testing.T) {
	ResetForTest()
	script := `#!/bin/sh
PREV=""
for arg in "$@"; do
    if [ "$PREV" = "-w" ] && [ "$arg" = "5" ]; then
        echo "iptables: No chain/target/match by that name." >&2
        exit 1
    fi
    PREV="$arg"
done
exit 0
`
	writeFakeIptablesDialect(t, script)

	args := WaitArgs(context.Background())
	expected := []string{"-w"}
	if !equalSlices(args, expected) {
		t.Fatalf("expected %v when -w 5 produces non-arg-parse error, got %v", expected, args)
	}

	mu.Lock()
	good := haveGood["iptables"]
	mu.Unlock()
	if !good {
		t.Fatal("expected haveGood=true after fallback to -w succeeded")
	}
}

// TestWaitArgsFor_IndependentPerBinary verifies WR-03:
// WaitArgsFor probes and caches per-binary independently so that differing
// dialects between iptables (e.g. -w 5) and ip6tables (e.g. -w) do not
// overwrite each other.
func TestWaitArgsFor_IndependentPerBinary(t *testing.T) {
	ResetForTest()
	dir := t.TempDir()

	// fake iptables supports -w 5
	v4Script := "#!/bin/sh\nexit 0\n"
	v4Bin := filepath.Join(dir, "iptables")
	if err := os.WriteFile(v4Bin, []byte(v4Script), 0755); err != nil {
		t.Fatal(err)
	}

	// fake ip6tables rejects 5 after -w, accepts -w
	v6Script := `#!/bin/sh
PREV=""
for arg in "$@"; do
    if [ "$PREV" = "-w" ] && [ "$arg" = "5" ]; then
        echo "Bad argument '5'" >&2
        exit 2
    fi
    PREV="$arg"
done
exit 0
`
	v6Bin := filepath.Join(dir, "ip6tables")
	if err := os.WriteFile(v6Bin, []byte(v6Script), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	argsV4 := WaitArgsFor(context.Background(), "iptables")
	expectedV4 := []string{"-w", "5"}
	if !equalSlices(argsV4, expectedV4) {
		t.Fatalf("expected iptables args %v, got %v", expectedV4, argsV4)
	}

	argsV6 := WaitArgsFor(context.Background(), "ip6tables")
	expectedV6 := []string{"-w"}
	if !equalSlices(argsV6, expectedV6) {
		t.Fatalf("expected ip6tables args %v, got %v", expectedV6, argsV6)
	}

	// Re-check iptables from cache to ensure not overwritten by ip6tables probe
	argsV4Cached := WaitArgsFor(context.Background(), "iptables")
	if !equalSlices(argsV4Cached, expectedV4) {
		t.Fatalf("expected cached iptables args %v, got %v", expectedV4, argsV4Cached)
	}
}

// TestWaitArgsFor_ConcurrentProbesDifferentBinaries verifies WR-03:
// Cold probing of one binary (e.g. slow iptables) must not block cold probing
// or cached access of another binary (e.g. fast ip6tables) by holding a global lock.
func TestWaitArgsFor_ConcurrentProbesDifferentBinaries(t *testing.T) {
	ResetForTest()
	dir := t.TempDir()

	// Slow iptables: sleeps 300ms
	slowScript := "#!/bin/sh\nsleep 0.3\nexit 0\n"
	slowBin := filepath.Join(dir, "iptables")
	if err := os.WriteFile(slowBin, []byte(slowScript), 0755); err != nil {
		t.Fatal(err)
	}

	// Fast ip6tables: exits immediately
	fastScript := "#!/bin/sh\nexit 0\n"
	fastBin := filepath.Join(dir, "ip6tables")
	if err := os.WriteFile(fastBin, []byte(fastScript), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Launch slow probe in background
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		WaitArgsFor(context.Background(), "iptables")
	}()

	// Brief pause to ensure the slow probe has started exec.CommandContext
	time.Sleep(50 * time.Millisecond)

	// Probe ip6tables while iptables is still probing
	start := time.Now()
	argsV6 := WaitArgsFor(context.Background(), "ip6tables")
	elapsed := time.Since(start)

	if len(argsV6) == 0 {
		t.Fatal("expected non-empty args for ip6tables")
	}

	// If global lock was held across the slow probe, elapsed would be >= 250ms.
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("WR-03 regression: WaitArgsFor(ip6tables) was blocked by slow iptables probe (%v)", elapsed)
	}

	<-slowDone
}
