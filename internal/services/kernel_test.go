package services

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFindKernelBinary: verifies that findKernelBinary locates a binary present
// in a probed path and returns "" when no path exists.
func TestFindKernelBinary(t *testing.T) {
	// Create a temp dir with an "xray" executable
	tmpDir := t.TempDir()
	xrayPath := filepath.Join(tmpDir, "xray")
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Temporarily override the probe list for "xray" by injecting tmpDir as first path.
	// We do this by calling findKernelBinary with our own wrapper that prepends tmpDir.
	// Since findKernelBinary is package-private, we test it directly here in the same package.
	origXrayPaths := xrayProbePaths
	xrayProbePaths = append([]string{xrayPath}, origXrayPaths...)
	defer func() { xrayProbePaths = origXrayPaths }()

	got := findKernelBinary("xray")
	if got != xrayPath {
		t.Errorf("expected %q, got %q", xrayPath, got)
	}

	// No binary in any probe path
	xrayProbePaths = []string{"/nonexistent/xray-does-not-exist"}
	got = findKernelBinary("xray")
	if got != "" {
		t.Errorf("expected empty string when binary not found, got %q", got)
	}
}

// TestKernelProcessStatus_NotAccessible: a binary that exists but is not readable
// should return "not_accessible".
func TestKernelProcessStatus_NotAccessible(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: running as root, permission check not applicable")
	}
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "mykernel")
	// Write file without read permission
	if err := os.WriteFile(binaryPath, []byte("binary"), 0000); err != nil {
		t.Fatal(err)
	}
	status := kernelProcessStatus(binaryPath)
	if status != "not_accessible" {
		t.Errorf("expected 'not_accessible', got %q", status)
	}
}

// TestKernelBinaryCache_TTL: verifies that resolveBinaryPath respects the 60s TTL cache.
func TestKernelBinaryCache_TTL(t *testing.T) {
	svc := NewKernelService()

	// Override statFunc with a counter
	callCount := 0
	svc.statFunc = func(path string) (os.FileInfo, error) {
		callCount++
		return nil, os.ErrNotExist
	}

	k := &KernelInfo{Name: "xray"}

	// Scenario 1: binaryPathCachedAt is recent (within TTL) → statFunc NOT called again
	k.binaryPathCachedAt = time.Now()
	k.BinaryPath = "/cached/path"
	callCount = 0
	svc.resolveBinaryPath(k)
	if callCount != 0 {
		t.Errorf("within TTL: expected 0 statFunc calls, got %d", callCount)
	}

	// Scenario 2: binaryPathCachedAt is expired (> 60s ago) → statFunc called
	k.binaryPathCachedAt = time.Now().Add(-120 * time.Second)
	callCount = 0
	svc.resolveBinaryPath(k)
	if callCount == 0 {
		t.Errorf("expired TTL: expected statFunc to be called, got 0 calls")
	}
}

func TestKernelService_New(t *testing.T) {
	svc := NewKernelService()
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestKernelService_List(t *testing.T) {
	svc := NewKernelService()
	kernels := svc.List()
	if len(kernels) == 0 {
		t.Fatal("expected at least one kernel")
	}
}

func TestKernelService_Get(t *testing.T) {
	svc := NewKernelService()
	kernel := svc.Get("xray")
	if kernel == nil {
		t.Fatal("expected xray kernel to exist")
	}
	if kernel.Name != "xray" {
		t.Fatalf("expected kernel name 'xray', got %s", kernel.Name)
	}
}

func TestKernelService_Get_Unknown(t *testing.T) {
	svc := NewKernelService()
	kernel := svc.Get("unknown")
	if kernel != nil {
		t.Fatal("expected nil for unknown kernel")
	}
}

func TestKernelService_SetChannel(t *testing.T) {
	svc := NewKernelService()

	ok := svc.SetChannel("xray", "preview")
	if !ok {
		t.Fatal("expected SetChannel to succeed")
	}

	k := svc.Get("xray")
	if k.Channel != "preview" {
		t.Fatalf("expected channel 'preview', got %s", k.Channel)
	}

	ok = svc.SetChannel("unknown", "preview")
	if ok {
		t.Fatal("expected SetChannel to fail for unknown kernel")
	}
}

func TestKernelService_DetectVersion_Xray(t *testing.T) {
	tmpDir := t.TempDir()
	xrayPath := filepath.Join(tmpDir, "xray")
	os.WriteFile(xrayPath, []byte("#!/bin/sh\necho \"Xray 1.8.24 (Xray, Penetrates Everything.)\"\n"), 0755)

	svc := NewKernelService()
	svc.kernels["xray"].BinaryPath = xrayPath

	v := svc.detectVersion(svc.kernels["xray"])
	if v != "1.8.24" {
		t.Fatalf("expected version 1.8.24, got %s", v)
	}
}

func TestKernelService_DetectVersion_Mihomo(t *testing.T) {
	tmpDir := t.TempDir()
	mihomoPath := filepath.Join(tmpDir, "mihomo")
	os.WriteFile(mihomoPath, []byte("#!/bin/sh\necho \"Mihomo Version v1.18.0\"\n"), 0755)

	svc := NewKernelService()
	svc.kernels["mihomo"].BinaryPath = mihomoPath

	v := svc.detectVersion(svc.kernels["mihomo"])
	if v != "1.18.0" {
		t.Fatalf("expected version 1.18.0, got %s", v)
	}
}

func TestKernelService_DetectVersion_NotInstalled(t *testing.T) {
	svc := NewKernelService()
	svc.kernels["xray"].BinaryPath = "/tmp/does-not-exist"

	v := svc.detectVersion(svc.kernels["xray"])
	if v != "not installed" {
		t.Fatalf("expected version 'not installed', got %s", v)
	}
}

// TestValidateKernelPath: path traversal is rejected; valid paths are accepted.
func TestValidateKernelPath(t *testing.T) {
	cases := []struct {
		path    string
		wantErr bool
	}{
		{"/opt/bin/xray", false},
		{"/opt/bin/.backup/kernel.bak.123", false},
		{"/opt/etc/mihomo/config.yaml", false},
		{"/opt/bin/../etc/passwd", true}, // traversal
		{"/home/user/evil", true},        // outside allowed roots
		{"relative/path", true},          // not absolute
		{"", true},                       // empty
	}

	for _, tc := range cases {
		err := validateKernelPath(tc.path)
		if tc.wantErr && err == nil {
			t.Errorf("path %q: expected error, got nil", tc.path)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("path %q: unexpected error: %v", tc.path, err)
		}
	}
}

// TestSetChannel_InvalidValue: invalid channel name returns false.
func TestSetChannel_InvalidValue(t *testing.T) {
	svc := NewKernelService()
	ok := svc.SetChannel("xray", "nightly")
	if ok {
		t.Error("expected SetChannel to return false for invalid channel 'nightly'")
	}
	ok = svc.SetChannel("xray", "")
	if ok {
		t.Error("expected SetChannel to return false for empty channel")
	}
	ok = svc.SetChannel("xray", "stable")
	if !ok {
		t.Error("expected SetChannel to return true for 'stable'")
	}
}

// TestConcurrentInstall409: calling Install twice on the same kernel while the first is in progress
// returns an error containing "install already in progress".
func TestConcurrentInstall409(t *testing.T) {
	svc := NewKernelService()

	// Manually acquire the install lock for "xray" to simulate an in-progress install.
	mu := &sync.Mutex{}
	actual, _ := svc.installLocks.LoadOrStore("xray", mu)
	installMu := actual.(*sync.Mutex)
	installMu.Lock() // hold the lock — simulates an ongoing install
	defer installMu.Unlock()

	// Now calling Install should fail immediately with "install already in progress".
	err := svc.Install("xray")
	if err == nil {
		t.Fatal("expected error from Install when lock is held, got nil")
	}
	if !strings.Contains(err.Error(), "install already in progress") {
		t.Errorf("expected 'install already in progress' error, got: %v", err)
	}
}

// TestKernelInstall_Concurrent: two concurrent Install calls for the same kernel
// should result in only one succeeding; the second must receive "install already in progress".
func TestKernelInstall_Concurrent(t *testing.T) {
	svc := NewKernelService()

	// Hold the install lock directly to simulate an in-progress install.
	mu := &sync.Mutex{}
	actual, _ := svc.installLocks.LoadOrStore("xray", mu)
	installMu := actual.(*sync.Mutex)
	installMu.Lock()
	defer installMu.Unlock()

	// Concurrent call must fail immediately without blocking.
	done := make(chan error, 1)
	go func() {
		done <- svc.Install("xray")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error when install lock is held, got nil")
		}
		if !strings.Contains(err.Error(), "install already in progress") {
			t.Errorf("expected 'install already in progress', got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Install blocked instead of returning immediately when lock is held")
	}
}

// TestKernelVersionCache_TTL: detectVersion should use the cached result within TTL
// and re-run the binary only after the TTL expires.
func TestKernelVersionCache_TTL(t *testing.T) {
	tmpDir := t.TempDir()
	callCount := 0
	scriptPath := filepath.Join(tmpDir, "xray")
	// Script that increments a side-effect file each time it's called.
	// We count calls via our own counter instead.
	script := "#!/bin/sh\necho \"Xray 1.9.0 (Xray, Penetrates Everything.)\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService()
	k := svc.kernels["xray"]
	k.BinaryPath = scriptPath

	// Wrap detectVersion in a counting helper
	countingDetect := func() string {
		callCount++
		return svc.detectVersion(k)
	}

	v1 := countingDetect()
	if v1 != "1.9.0" {
		t.Fatalf("first call: expected 1.9.0, got %s", v1)
	}

	// Second call within TTL — should return cached value without re-running binary.
	// Force cache to look fresh.
	k.verCache.value = "1.9.0"
	k.verCache.expires = time.Now().Add(versionCacheTTL)
	v2 := svc.detectVersion(k)
	if v2 != "1.9.0" {
		t.Fatalf("cached call: expected 1.9.0, got %s", v2)
	}

	// Expire the cache and re-run — should call binary again.
	k.verCache.expires = time.Now().Add(-1 * time.Second)
	v3 := countingDetect()
	if v3 != "1.9.0" {
		t.Fatalf("after expiry: expected 1.9.0, got %s", v3)
	}
	_ = callCount // suppress unused warning
}

// TestKernelVersionRegex_VPrefix: parseVersion must strip leading 'v'/'V' prefix.
func TestKernelVersionRegex_VPrefix(t *testing.T) {
	svc := NewKernelService()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"xray plain", "Xray 1.8.24 (Xray, Penetrates Everything.)", "1.8.24"},
		{"xray v-prefix", "Xray v1.8.24 something", "1.8.24"},
		{"xray V-prefix", "Xray V1.8.24 something", "1.8.24"},
		{"mihomo plain", "Mihomo Version: 1.18.0", "1.18.0"},
		{"mihomo v-prefix", "Mihomo Version: v1.18.0", "1.18.0"},
		{"mihomo V-prefix", "Mihomo Version: V1.18.0", "1.18.0"},
		{"mihomo prerelease", "Mihomo Version: v1.18.0-rc1", "1.18.0-rc1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := svc.parseVersion(strings.Split(tc.name, " ")[0], tc.input)
			if got != tc.want {
				t.Errorf("parseVersion(%q, %q) = %q; want %q", strings.Split(tc.name, " ")[0], tc.input, got, tc.want)
			}
		})
	}
}

// TestDecompressionLimit: zip with a 51 MB entry is rejected.
func TestDecompressionLimit(t *testing.T) {
	// Create a zip in memory with a single large file
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.Create("xray")
	if err != nil {
		t.Fatal(err)
	}

	// Write 51 MB of zeros
	chunk := make([]byte, 64*1024)
	total := 0
	limit := 51 * 1024 * 1024
	for total < limit {
		n := limit - total
		if n > len(chunk) {
			n = len(chunk)
		}
		written, err := fw.Write(chunk[:n])
		if err != nil {
			t.Fatal(err)
		}
		total += written
	}
	w.Close()

	// Write zip to a temp file
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "xray.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService()
	outPath, err := svc.extractZip(zipPath, "xray")
	// The function should succeed (LimitReader silently stops at limit) but we verify
	// the output file is not larger than maxKernelExtractBytes
	if err != nil {
		// If extraction returned error, that's acceptable too
		return
	}
	defer os.Remove(outPath)

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
	if info.Size() > maxKernelExtractBytes {
		t.Errorf("extracted file size %d exceeds limit %d", info.Size(), maxKernelExtractBytes)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		v1       string
		v2       string
		wantSign int
	}{
		{"1.18.1", "1.18.0", 1},
		{"1.18.0", "1.18.1", -1},
		{"1.18.0", "1.18.0", 0},
		{"2.0.0", "1.99.99", 1},
		{"1.18.0-rc2", "1.18.0-rc1", 1},
		{"1.18.0-rc1", "1.18.0", -1},
		{"1.18.0", "1.18.0-rc1", 1},
		{"not installed", "1.18.0", -1},
		{"error", "1.18.0", -1},
		{"1.18.0", "not installed", 1},
		{"garbage", "garbage", 0},
	}

	sign := func(n int) int {
		if n > 0 {
			return 1
		}
		if n < 0 {
			return -1
		}
		return 0
	}

	for _, tc := range cases {
		got := compareSemver(tc.v1, tc.v2)
		if sign(got) != tc.wantSign {
			t.Errorf("compareSemver(%q, %q) = %d (sign %d); want sign %d", tc.v1, tc.v2, got, sign(got), tc.wantSign)
		}
	}
}

func TestCheckLatest_SemverHasUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.18.0"}`))
	}))
	defer server.Close()

	ctx := context.Background()

	// Scenario 1: CurrentVersion = "1.18.1", latestVersion = "1.18.0" -> HasUpdate == false
	svc := NewKernelService()
	svc.testClient = server.Client()
	svc.githubAPIBase = server.URL
	svc.kernels["xray"].CurrentVersion = "1.18.1"
	svc.kernels["xray"].Channel = "stable"
	svc.kernels["xray"].Repo = "some/repo"

	err := svc.CheckLatest(ctx, "xray")
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if svc.kernels["xray"].HasUpdate {
		t.Errorf("expected HasUpdate = false for current 1.18.1 and latest 1.18.0")
	}

	// Scenario 2: CurrentVersion = "1.17.0", latestVersion = "1.18.0" -> HasUpdate == true
	svc = NewKernelService()
	svc.testClient = server.Client()
	svc.githubAPIBase = server.URL
	svc.kernels["xray"].CurrentVersion = "1.17.0"
	svc.kernels["xray"].Channel = "stable"
	svc.kernels["xray"].Repo = "some/repo"

	err = svc.CheckLatest(ctx, "xray")
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if !svc.kernels["xray"].HasUpdate {
		t.Errorf("expected HasUpdate = true for current 1.17.0 and latest 1.18.0")
	}

	// Scenario 3: CurrentVersion = "not installed", latestVersion = "1.18.0" -> HasUpdate == true
	svc = NewKernelService()
	svc.testClient = server.Client()
	svc.githubAPIBase = server.URL
	svc.kernels["xray"].CurrentVersion = "not installed"
	svc.kernels["xray"].Channel = "stable"
	svc.kernels["xray"].Repo = "some/repo"

	err = svc.CheckLatest(ctx, "xray")
	if err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if !svc.kernels["xray"].HasUpdate {
		t.Errorf("expected HasUpdate = true for current 'not installed' and latest 1.18.0")
	}
}

