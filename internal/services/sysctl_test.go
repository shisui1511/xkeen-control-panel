package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubSysctlApply replaces execSysctlApply for the duration of a test so the
// real `sysctl -p` binary is never invoked (it would mutate the actual
// kernel sysctl state of whatever machine runs the test suite).
func stubSysctlApply(t *testing.T) *[]string {
	t.Helper()
	original := execSysctlApply
	var calls []string
	execSysctlApply = func(path string) error {
		calls = append(calls, path)
		return nil
	}
	t.Cleanup(func() { execSysctlApply = original })
	return &calls
}

func TestDeploySysctlProfile_WritesProfile(t *testing.T) {
	calls := stubSysctlApply(t)

	tmpDir := t.TempDir()
	sysctlDir := filepath.Join(tmpDir, "sysctl.d")

	if err := DeploySysctlProfile(sysctlDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(sysctlDir, "99-xkeen.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected profile file to be written: %v", err)
	}

	for _, key := range []string{
		"net.netfilter.nf_conntrack_max",
		"net.netfilter.nf_conntrack_tcp_timeout_established",
		"net.netfilter.nf_conntrack_tcp_timeout_time_wait",
		"net.ipv4.tcp_tw_reuse",
		"net.ipv4.tcp_fin_timeout",
		"fs.file-max",
		"kernel.pid_max",
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("expected profile to contain %q", key)
		}
	}

	if len(*calls) != 1 || (*calls)[0] != path {
		t.Fatalf("expected execSysctlApply to be called once with %s, got %v", path, *calls)
	}
}

func TestDeploySysctlProfile_NoOpWhenNotEntware(t *testing.T) {
	stubSysctlApply(t)

	tmpDir := t.TempDir()
	// Parent directory of sysctlDir does not exist -> treated as non-Entware.
	sysctlDir := filepath.Join(tmpDir, "does-not-exist", "sysctl.d")

	if err := DeploySysctlProfile(sysctlDir); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if _, err := os.Stat(sysctlDir); err == nil {
		t.Fatal("expected sysctlDir not to be created when parent is missing")
	}
}

func TestDeploySysctlProfile_DefaultsToStandardDir(t *testing.T) {
	// Only meaningful assertion without touching the real filesystem: an
	// empty sysctlDir must not error out before the "parent missing" check
	// even runs — DeploySysctlProfile("") should behave exactly like
	// DeploySysctlProfile(defaultSysctlDir). On a dev machine /opt/etc is
	// very unlikely to exist, so this exercises the no-op path.
	stubSysctlApply(t)

	if err := DeploySysctlProfile(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeploySysctlProfile_OverwritesExistingFile(t *testing.T) {
	stubSysctlApply(t)

	tmpDir := t.TempDir()
	sysctlDir := filepath.Join(tmpDir, "sysctl.d")
	if err := os.MkdirAll(sysctlDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sysctlDir, "99-xkeen.conf")
	if err := os.WriteFile(path, []byte("stale content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := DeploySysctlProfile(sysctlDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale content") {
		t.Fatal("expected stale profile content to be overwritten")
	}
	if !strings.Contains(string(data), "nf_conntrack_max") {
		t.Fatal("expected fresh profile content after redeploy")
	}
}
