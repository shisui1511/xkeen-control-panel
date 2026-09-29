package services

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
	svc := NewKernelService(t.TempDir())

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
	svc := NewKernelService(t.TempDir())
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestKernelService_List(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	kernels := svc.List()
	if len(kernels) == 0 {
		t.Fatal("expected at least one kernel")
	}
}

func TestKernelService_Get(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	kernel := svc.Get("xray")
	if kernel == nil {
		t.Fatal("expected xray kernel to exist")
	}
	if kernel.Name != "xray" {
		t.Fatalf("expected kernel name 'xray', got %s", kernel.Name)
	}
}

func TestKernelService_GetActiveKernel(t *testing.T) {
	tmpDir := t.TempDir()
	origProcDir := procDir
	procDir = tmpDir
	defer func() { procDir = origProcDir }()

	mihomoBin := filepath.Join(tmpDir, "mihomo")
	xrayBin := filepath.Join(tmpDir, "xray")

	if err := os.WriteFile(mihomoBin, []byte("fake binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xrayBin, []byte("fake binary"), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService(t.TempDir())
	svc.kernels["mihomo"].BinaryPath = mihomoBin
	svc.kernels["xray"].BinaryPath = xrayBin

	// Case 1: No running kernels
	active := svc.GetActiveKernel()
	if active != "" {
		t.Errorf("expected no active kernel, got %q", active)
	}

	// Case 2: Only mihomo is running
	pid1 := "1000"
	pidDir1 := filepath.Join(tmpDir, pid1)
	if err := os.MkdirAll(pidDir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir1, "cmdline"), []byte(mihomoBin+"\x00-d\x00/opt/etc/mihomo\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mihomoBin, filepath.Join(pidDir1, "exe")); err != nil {
		t.Fatal(err)
	}

	active = svc.GetActiveKernel()
	if active != "mihomo" {
		t.Errorf("expected active kernel 'mihomo', got %q", active)
	}

	// Case 3: Both running (order is xray first in List() / GetActiveKernel)
	pid2 := "1001"
	pidDir2 := filepath.Join(tmpDir, pid2)
	if err := os.MkdirAll(pidDir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir2, "cmdline"), []byte(xrayBin+"\x00-config\x00/opt/etc/xray.json\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(xrayBin, filepath.Join(pidDir2, "exe")); err != nil {
		t.Fatal(err)
	}

	active = svc.GetActiveKernel()
	if active != "xray" {
		t.Errorf("expected active kernel 'xray', got %q", active)
	}
}

func TestKernelService_Get_Unknown(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	kernel := svc.Get("unknown")
	if kernel != nil {
		t.Fatal("expected nil for unknown kernel")
	}
}

func TestKernelService_SetChannel(t *testing.T) {
	svc := NewKernelService(t.TempDir())

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

// TestKernelService_ChannelPersistsAcrossRestart verifies channel selection
// survives process restarts (e.g. the mandatory redeploy after every build),
// since it is now persisted to <dataDir>/kernels/channels.json instead of
// living only in the in-memory KernelInfo struct.
func TestKernelService_ChannelPersistsAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()

	svc := NewKernelService(dataDir)
	if !svc.SetChannel("mihomo", "preview") {
		t.Fatal("expected SetChannel to succeed")
	}

	restarted := NewKernelService(dataDir)
	if got := restarted.Get("mihomo").Channel; got != "preview" {
		t.Fatalf("expected persisted channel 'preview' after restart, got %q", got)
	}
	if got := restarted.Get("xray").Channel; got != "stable" {
		t.Fatalf("expected untouched xray channel to stay 'stable', got %q", got)
	}
}

// TestKernelService_ChannelStore_IgnoresGarbage verifies a corrupt or
// tampered channels.json falls back to defaults instead of failing startup.
func TestKernelService_ChannelStore_IgnoresGarbage(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "kernels"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "kernels", "channels.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService(dataDir)
	if got := svc.Get("xray").Channel; got != "stable" {
		t.Fatalf("expected default channel 'stable' with corrupt store, got %q", got)
	}
}

func TestKernelService_DetectVersion_Xray(t *testing.T) {
	tmpDir := t.TempDir()
	xrayPath := filepath.Join(tmpDir, "xray")
	os.WriteFile(xrayPath, []byte("#!/bin/sh\necho \"Xray 1.8.24 (Xray, Penetrates Everything.)\"\n"), 0755)

	svc := NewKernelService(t.TempDir())
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

	svc := NewKernelService(t.TempDir())
	svc.kernels["mihomo"].BinaryPath = mihomoPath

	v := svc.detectVersion(svc.kernels["mihomo"])
	if v != "1.18.0" {
		t.Fatalf("expected version 1.18.0, got %s", v)
	}
}

// TestDetectVersion_Timeout: зависший бинарник не держит detectVersion дольше
// таймаута, а результат-ошибка не кэшируется.
func TestDetectVersion_Timeout(t *testing.T) {
	origTimeout := kernelVersionTimeout
	kernelVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { kernelVersionTimeout = origTimeout })

	tmpDir := t.TempDir()
	xrayPath := filepath.Join(tmpDir, "xray")
	// sleep наследует пайп вывода — проверяем и убийство процесса, и WaitDelay
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\nexec sleep 5\n"), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService(t.TempDir())
	svc.kernels["xray"].BinaryPath = xrayPath

	for i := 0; i < 2; i++ {
		start := time.Now()
		v := svc.detectVersion(svc.kernels["xray"])
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("call %d: detectVersion took %v, want < 2s", i, elapsed)
		}
		if v != "error" {
			t.Fatalf("call %d: version = %q, want error", i, v)
		}
	}

	// Ошибка не кэшируется: после починки бинарника версия читается сразу.
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\necho \"Xray 1.8.24 (Xray, Penetrates Everything.)\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if v := svc.detectVersion(svc.kernels["xray"]); v != "1.8.24" {
		t.Fatalf("after fix: version = %q, want 1.8.24 (error must not be cached)", v)
	}
}

// TestNewKernelService_NoWarningWhenNotInstalled: отсутствие ядер на чистой
// системе — штатная ситуация, в логе это info без слова WARNING.
func TestNewKernelService_NoWarningWhenNotInstalled(t *testing.T) {
	origXray, origMihomo := xrayProbePaths, mihomoProbePaths
	xrayProbePaths = []string{"/nonexistent/xray-does-not-exist"}
	mihomoProbePaths = []string{"/nonexistent/mihomo-does-not-exist"}
	t.Setenv("PATH", t.TempDir())

	var buf bytes.Buffer
	origOut, origFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
		xrayProbePaths, mihomoProbePaths = origXray, origMihomo
	})

	NewKernelService(t.TempDir())

	out := buf.String()
	if strings.Contains(out, "WARNING") {
		t.Errorf("лог содержит WARNING про отсутствующие ядра: %q", out)
	}
	for _, want := range []string{"Xray binary not found (not installed yet)", "Mihomo binary not found (not installed yet)"} {
		if !strings.Contains(out, want) {
			t.Errorf("в логе нет %q: %q", want, out)
		}
	}
}

func TestKernelService_DetectVersion_NotInstalled(t *testing.T) {
	svc := NewKernelService(t.TempDir())
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
		{"/opt/bin/.backup/xray.bak.123", false},
		{"/opt/bin/.backup/mihomo.bak.123", false},
		{"/opt/etc/mihomo/config.yaml", false},
		{"/opt/bin/../etc/passwd", true}, // traversal
		{"/home/user/evil", true},        // outside allowed roots
		{"relative/path", true},          // not absolute
		{"", true},                       // empty
	}

	for _, tc := range cases {
		clean, err := sanitizeKernelPath(tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("path %q: expected error, got nil", tc.path)
			}
			if clean != "" {
				t.Errorf("path %q: expected empty path on error, got %q", tc.path, clean)
			}
			continue
		}
		if err != nil {
			t.Errorf("path %q: unexpected error: %v", tc.path, err)
			continue
		}
		if clean != filepath.Clean(tc.path) {
			t.Errorf("path %q: got %q, expected cleaned path", tc.path, clean)
		}
	}
}

// TestSetChannel_InvalidValue: invalid channel name returns false.
func TestSetChannel_InvalidValue(t *testing.T) {
	svc := NewKernelService(t.TempDir())
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
	svc := NewKernelService(t.TempDir())

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
	svc := NewKernelService(t.TempDir())

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
	logPath := filepath.Join(tmpDir, "calls.log")
	scriptPath := filepath.Join(tmpDir, "xray")

	script := fmt.Sprintf("#!/bin/sh\necho x >> %s\necho \"Xray 1.9.0 (Xray, Penetrates Everything.)\"\n", logPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService(t.TempDir())
	k := svc.kernels["xray"]
	k.BinaryPath = scriptPath

	readCalls := func() int {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}

	// First call — runs binary, caches result
	v1 := svc.detectVersion(k)
	if v1 != "1.9.0" {
		t.Fatalf("first call: expected 1.9.0, got %s", v1)
	}
	if calls := readCalls(); calls != 1 {
		t.Fatalf("expected 1 binary execution, got %d", calls)
	}

	// Second call within TTL — should return cached value without executing binary
	v2 := svc.detectVersion(k)
	if v2 != "1.9.0" {
		t.Fatalf("cached call: expected 1.9.0, got %s", v2)
	}
	if calls := readCalls(); calls != 1 {
		t.Fatalf("expected still 1 binary execution (cached), got %d", calls)
	}

	// Force expire the cache and call again — should execute binary again
	k.verCache.mu.Lock()
	k.verCache.expires = time.Now().Add(-1 * time.Second)
	k.verCache.mu.Unlock()

	v3 := svc.detectVersion(k)
	if v3 != "1.9.0" {
		t.Fatalf("after expiry: expected 1.9.0, got %s", v3)
	}
	if calls := readCalls(); calls != 2 {
		t.Fatalf("expected 2 binary executions after cache expiry, got %d", calls)
	}
}

// TestKernelVersionRegex_VPrefix: parseVersion must strip leading 'v'/'V' prefix.
func TestKernelVersionRegex_VPrefix(t *testing.T) {
	svc := NewKernelService(t.TempDir())

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

	svc := NewKernelService(t.TempDir())
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
		{"1.18.0+entware", "1.18.0", 0},
		{"1.18.0", "1.18.0+entware", 0},
		{"1.18.0-rc.10", "1.18.0-rc.2", 1},
		{"1.18.0-rc.2", "1.18.0-rc.10", -1},
		{"1.18.0-rc.2", "1.18.0-rc.2", 0},
		{"1.18.0-rc.2+build1", "1.18.0-rc.2+build2", 0},
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
	svc := NewKernelService(t.TempDir())
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
	if !svc.kernels["xray"].AheadOfLatest {
		t.Errorf("expected AheadOfLatest = true for current 1.18.1 and latest 1.18.0")
	}

	// Scenario 2: CurrentVersion = "1.17.0", latestVersion = "1.18.0" -> HasUpdate == true
	svc = NewKernelService(t.TempDir())
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
	svc = NewKernelService(t.TempDir())
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

// newReleaseServer поднимает httptest-сервер, отвечающий на любые запросы
// фиксированным статусом и телом.
func newReleaseServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// newCheckLatestService — сервис с подключённым локальным источником релизов.
func newCheckLatestService(t *testing.T, server *httptest.Server, channel, current string) *KernelService {
	t.Helper()
	svc := NewKernelService(t.TempDir())
	svc.SetReleaseSource(server.URL, server.Client())
	k := svc.kernels["xray"]
	k.CurrentVersion = current
	k.Channel = channel
	k.Repo = "some/repo"
	return svc
}

// TestCheckLatest_Ahead: третье состояние «установлена новее последнего stable».
func TestCheckLatest_Ahead(t *testing.T) {
	cases := []struct {
		name       string
		kernel     string
		channel    string
		body       string
		current    string
		wantUpdate bool
		wantAhead  bool
	}{
		{"stable, установлена новее latest", "xray", "stable", `{"tag_name":"v26.3.27"}`, "26.9.8", false, true},
		{"stable, версии равны", "xray", "stable", `{"tag_name":"v26.3.27"}`, "26.3.27", false, false},
		{"stable, установлена старее", "xray", "stable", `{"tag_name":"v26.3.27"}`, "26.1.1", true, false},
		{"preview, установлена новее latest", "xray", "preview", `[{"tag_name":"v26.9.9","prerelease":true}]`, "26.9.10", false, false},
		{"preview, плавающая alpha-сборка", "mihomo", "preview", `[{"tag_name":"Prerelease-Alpha","prerelease":true,"assets":[{"name":"mihomo-linux-arm64-alpha-abc1234.gz"}]}]`, "1.19.0", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newReleaseServer(t, http.StatusOK, tc.body)
			svc := newCheckLatestService(t, server, tc.channel, tc.current)
			name := tc.kernel
			if name != "xray" {
				svc.kernels[name].CurrentVersion = tc.current
				svc.kernels[name].Channel = tc.channel
				svc.kernels[name].Repo = "some/repo"
			}
			if err := svc.CheckLatest(context.Background(), name); err != nil {
				t.Fatalf("CheckLatest error: %v", err)
			}
			k := svc.kernels[name]
			if k.HasUpdate != tc.wantUpdate {
				t.Errorf("HasUpdate = %v, want %v", k.HasUpdate, tc.wantUpdate)
			}
			if k.AheadOfLatest != tc.wantAhead {
				t.Errorf("AheadOfLatest = %v, want %v", k.AheadOfLatest, tc.wantAhead)
			}
			if k.Status != "idle" {
				t.Errorf("Status = %q, want idle", k.Status)
			}
		})
	}
}

// TestCheckLatest_HTTPError: ответ GitHub с HTTP != 200 — это failed, а не «актуально».
func TestCheckLatest_HTTPError(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprintf("HTTP %d", code), func(t *testing.T) {
			server := newReleaseServer(t, code, `{"message":"rate limit exceeded"}`)
			svc := newCheckLatestService(t, server, "stable", "1.17.0")

			if err := svc.CheckLatest(context.Background(), "xray"); err == nil {
				t.Fatal("expected error for non-200 GitHub response")
			}
			k := svc.kernels["xray"]
			if want := fmt.Sprintf("GitHub API HTTP %d", code); k.Status != "failed" || k.Message != want {
				t.Errorf("Status=%q Message=%q, want failed / %q", k.Status, k.Message, want)
			}
			if k.LatestVersion != "" || k.HasUpdate || k.AheadOfLatest {
				t.Errorf("Latest* must stay empty on error: latest=%q has_update=%v ahead=%v", k.LatestVersion, k.HasUpdate, k.AheadOfLatest)
			}
		})
	}
}

// TestCheckLatest_EmptyStableTag: пустой tag_name на stable — ошибка, а не «актуально».
func TestCheckLatest_EmptyStableTag(t *testing.T) {
	server := newReleaseServer(t, http.StatusOK, `{"tag_name":""}`)
	svc := newCheckLatestService(t, server, "stable", "1.17.0")

	if err := svc.CheckLatest(context.Background(), "xray"); err == nil {
		t.Fatal("expected error for empty release tag")
	}
	k := svc.kernels["xray"]
	if k.Status != "failed" || k.Message != "GitHub API: empty release tag" {
		t.Errorf("Status=%q Message=%q, want failed / empty release tag", k.Status, k.Message)
	}
}

// TestCheckLatest_QuietKeepsStatus: тихая проверка (шов для установки) не трогает
// Status и Message ни при успехе, ни при ошибке GitHub.
func TestCheckLatest_QuietKeepsStatus(t *testing.T) {
	t.Run("успех", func(t *testing.T) {
		server := newReleaseServer(t, http.StatusOK, `{"tag_name":"v1.18.0"}`)
		svc := newCheckLatestService(t, server, "stable", "1.17.0")
		svc.kernels["xray"].Status = "downloading"
		svc.kernels["xray"].Message = "Downloading..."

		if err := svc.checkLatest(context.Background(), "xray", true); err != nil {
			t.Fatalf("checkLatest error: %v", err)
		}
		k := svc.kernels["xray"]
		if k.Status != "downloading" || k.Message != "Downloading..." {
			t.Errorf("Status=%q Message=%q changed by quiet check", k.Status, k.Message)
		}
		if k.LatestVersion != "1.18.0" || !k.HasUpdate {
			t.Errorf("Latest* not recorded: latest=%q has_update=%v", k.LatestVersion, k.HasUpdate)
		}
	})
	t.Run("ошибка 403", func(t *testing.T) {
		server := newReleaseServer(t, http.StatusForbidden, `{}`)
		svc := newCheckLatestService(t, server, "stable", "1.17.0")
		svc.kernels["xray"].Status = "downloading"
		svc.kernels["xray"].Message = "Downloading..."

		if err := svc.checkLatest(context.Background(), "xray", true); err == nil {
			t.Fatal("expected error")
		}
		k := svc.kernels["xray"]
		if k.Status != "downloading" || k.Message != "Downloading..." {
			t.Errorf("Status=%q Message=%q changed by quiet check", k.Status, k.Message)
		}
	})
}

// TestCheckLatest_DropsResultAfterChannelSwitch: результат проверки, начатой до
// смены канала, не перезаписывает Latest* нового канала.
func TestCheckLatest_DropsResultAfterChannelSwitch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.18.0"}`))
	}))
	defer server.Close()

	svc := newCheckLatestService(t, server, "stable", "1.17.0")

	done := make(chan error, 1)
	go func() { done <- svc.CheckLatest(context.Background(), "xray") }()

	<-started
	if !svc.SetChannel("xray", "preview") {
		t.Fatal("SetChannel returned false")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}

	k := svc.Get("xray")
	if k.LatestVersion != "" || k.LatestTag != "" || k.HasUpdate {
		t.Errorf("устаревший результат записан: latest=%q tag=%q has_update=%v", k.LatestVersion, k.LatestTag, k.HasUpdate)
	}
}

// TestSetChannel_Recompute: смена канала сбрасывает результаты проверки прежнего
// канала до перепроверки нового.
func TestSetChannel_Recompute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.18.0"}`))
	}))
	defer server.Close()

	svc := NewKernelService(t.TempDir())
	svc.SetReleaseSource(server.URL, server.Client())
	svc.kernels["xray"].CurrentVersion = "1.17.0"
	svc.kernels["xray"].Channel = "stable"
	svc.kernels["xray"].Repo = "some/repo"

	if err := svc.CheckLatest(context.Background(), "xray"); err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	if got := svc.Get("xray"); got.LatestVersion != "1.18.0" || !got.HasUpdate {
		t.Fatalf("precondition: latest=%q has_update=%v", got.LatestVersion, got.HasUpdate)
	}

	if !svc.SetChannel("xray", "preview") {
		t.Fatal("SetChannel returned false")
	}
	got := svc.Get("xray")
	if got.LatestVersion != "" || got.LatestTag != "" || got.HasUpdate {
		t.Errorf("Latest* not reset: latest=%q tag=%q has_update=%v", got.LatestVersion, got.LatestTag, got.HasUpdate)
	}
	if got.Status != "checking" {
		t.Errorf("status = %q, want checking", got.Status)
	}
	if got.Channel != "preview" {
		t.Errorf("channel = %q, want preview", got.Channel)
	}
}

// TestCheckLatest_PreviewNoPrereleaseFound verifies that when the "preview"
// channel finds no prerelease tag in the scanned release window, this is
// reported via Message rather than silently looking identical to "up to date".
func TestCheckLatest_PreviewNoPrereleaseFound(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"v1.18.0","prerelease":false},{"tag_name":"v1.17.0","prerelease":false}]`))
	}))
	defer server.Close()

	svc := NewKernelService(t.TempDir())
	svc.testClient = server.Client()
	svc.githubAPIBase = server.URL
	svc.kernels["xray"].CurrentVersion = "1.18.0"
	svc.kernels["xray"].Channel = "preview"
	svc.kernels["xray"].Repo = "some/repo"

	if err := svc.CheckLatest(context.Background(), "xray"); err != nil {
		t.Fatalf("CheckLatest error: %v", err)
	}
	k := svc.kernels["xray"]
	if k.HasUpdate {
		t.Errorf("expected HasUpdate = false when no prerelease was found")
	}
	if k.Message == "" {
		t.Errorf("expected a non-empty Message explaining no prerelease was found")
	}
	if gotQuery != "per_page=30" {
		t.Errorf("expected per_page=30 request for preview channel, got query %q", gotQuery)
	}
}

func TestIsShortLivedOrHelperProcess(t *testing.T) {
	tmpDir := t.TempDir()
	origProcDir := procDir
	procDir = tmpDir
	defer func() { procDir = origProcDir }()

	// Case 1: Helper process with "version" flag
	pid1 := "123"
	if err := os.MkdirAll(filepath.Join(tmpDir, pid1), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, pid1, "cmdline"), []byte("/opt/sbin/xray\x00version\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 2: Helper process with "-v" flag
	pid2 := "456"
	if err := os.MkdirAll(filepath.Join(tmpDir, pid2), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, pid2, "cmdline"), []byte("/opt/sbin/mihomo\x00-v\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 3: Regular daemon process
	pid3 := "789"
	if err := os.MkdirAll(filepath.Join(tmpDir, pid3), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, pid3, "cmdline"), []byte("/opt/sbin/xray\x00-config\x00/opt/etc/xray.json\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 4: Non-existent PID
	pid4 := "999"

	if !isShortLivedOrHelperProcess(pid1) {
		t.Errorf("expected PID %s to be classified as short lived/helper", pid1)
	}
	if !isShortLivedOrHelperProcess(pid2) {
		t.Errorf("expected PID %s to be classified as short lived/helper", pid2)
	}
	if isShortLivedOrHelperProcess(pid3) {
		t.Errorf("expected PID %s to be classified as daemon process", pid3)
	}
	if !isShortLivedOrHelperProcess(pid4) {
		t.Errorf("expected PID %s (non-existent) to return true", pid4)
	}
}

func TestKernelService_List_Order(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	list := svc.List()
	if len(list) < 2 {
		t.Fatalf("expected at least 2 kernels, got %d", len(list))
	}
	if list[0].Name != "xray" {
		t.Errorf("expected first kernel to be xray, got %s", list[0].Name)
	}
	if list[1].Name != "mihomo" {
		t.Errorf("expected second kernel to be mihomo, got %s", list[1].Name)
	}
}

// Uptime comes from /proc/<pid>/stat starttime, not the mtime of /proc/<pid>.
func TestGetProcUptime_FromStatStartTime(t *testing.T) {
	tmpDir := t.TempDir()
	origProcDir := procDir
	procDir = tmpDir
	defer func() { procDir = origProcDir }()

	if err := os.MkdirAll(filepath.Join(tmpDir, "4242"), 0o755); err != nil {
		t.Fatal(err)
	}
	// comm with spaces and parentheses; starttime (field 22) = 22623900 ticks.
	stat := "4242 (my (odd) proc) S 1 1 1 0 -1 4194560 0 0 0 0 10 5 0 0 20 0 8 0 22623900 1000 100\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "4242", "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "uptime"), []byte("301764.85 580624.92\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 301764.85 - 226239 = 75525.85 s = 20h 58m
	if got := getProcUptime("4242"); got != "20ч 58м" {
		t.Fatalf("uptime = %q, want 20ч 58м", got)
	}
	if got := getProcUptime("9999"); got != "" {
		t.Fatalf("missing process: %q", got)
	}
}

// TestBuildDownloadURL_AllArches: имена ассетов совпадают с релизами Xray и Mihomo
// для всех архитектур сборки панели.
func TestBuildDownloadURL_AllArches(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	cases := []struct {
		kernel, repo, goarch, wantFile string
	}{
		{"xray", "XTLS/Xray-core", "arm64", "Xray-linux-arm64-v8a.zip"},
		{"xray", "XTLS/Xray-core", "mipsle", "Xray-linux-mips32le.zip"},
		{"xray", "XTLS/Xray-core", "mips", "Xray-linux-mips32.zip"},
		{"mihomo", "MetaCubeX/mihomo", "arm64", "mihomo-linux-arm64-v1.19.31.gz"},
		{"mihomo", "MetaCubeX/mihomo", "mipsle", "mihomo-linux-mipsle-softfloat-v1.19.31.gz"},
		{"mihomo", "MetaCubeX/mihomo", "mips", "mihomo-linux-mips-softfloat-v1.19.31.gz"},
	}
	for _, c := range cases {
		t.Run(c.kernel+"/"+c.goarch, func(t *testing.T) {
			k := &KernelInfo{Name: c.kernel, Repo: c.repo, LatestVersion: "1.19.31"}
			url, file := svc.buildDownloadURL(k, kernelAssetArch(c.goarch))
			if file != c.wantFile {
				t.Fatalf("file = %q, want %q", file, c.wantFile)
			}
			want := "https://github.com/" + c.repo + "/releases/download/v1.19.31/" + c.wantFile
			if url != want {
				t.Errorf("url = %q, want %q", url, want)
			}
		})
	}

	k := &KernelInfo{Name: "xray", Repo: "XTLS/Xray-core", LatestVersion: "1.0.0"}
	if url, _ := svc.buildDownloadURL(k, kernelAssetArch("amd64")); url != "" {
		t.Errorf("unsupported arch must yield empty url, got %q", url)
	}
}

// TestExtractZip_PrefersSoftfloatOnMIPS: MIPS-архив Xray содержит xray и
// xray_softfloat; на MIPS должен распаковываться softfloat-вариант.
func TestExtractZip_PrefersSoftfloatOnMIPS(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range []struct{ name, body string }{
		{"geoip.dat", "geo"},
		{"xray_softfloat", "soft"},
		{"xray", "hard"},
	} {
		fw, err := w.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "Xray-linux-mips32le.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	orig := zipPreferSoftfloat
	t.Cleanup(func() { zipPreferSoftfloat = orig })
	svc := NewKernelService(t.TempDir())

	for _, c := range []struct {
		preferSoft bool
		want       string
	}{{true, "soft"}, {false, "hard"}} {
		zipPreferSoftfloat = c.preferSoft
		out, err := svc.extractZip(zipPath, "xray")
		if err != nil {
			t.Fatalf("preferSoft=%v: %v", c.preferSoft, err)
		}
		got, err := os.ReadFile(out)
		os.Remove(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("preferSoft=%v: extracted %q, want %q", c.preferSoft, got, c.want)
		}
	}
}

// TestCopyKernelFile_PreservesExecBit: копия ядра (запасной путь при
// переносе между файловыми системами и резервная копия) остаётся исполняемой.
func TestCopyKernelFile_PreservesExecBit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "xray.new")
	dst := filepath.Join(dir, "xray")
	if err := os.WriteFile(src, []byte("\x7fELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyKernelFile(src, dst); err != nil {
		t.Fatalf("copyKernelFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("copied kernel is not executable: mode %v", info.Mode().Perm())
	}
}

// TestMoveKernelFile_MovesAndRemovesSource: перенос кладёт файл на место
// назначения и не оставляет источник.
func TestMoveKernelFile_MovesAndRemovesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mihomo.extracted")
	dst := filepath.Join(dir, "mihomo.new")
	if err := os.WriteFile(src, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := moveKernelFile(src, dst); err != nil {
		t.Fatalf("moveKernelFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still exists after move: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "payload" {
		t.Errorf("destination content = %q, err = %v", got, err)
	}
}

// TestUploadBinary_OversizedRejected: файл сверх лимита отклоняется с ошибкой,
// а не обрезается до ELF-похожего огрызка, который встал бы ядром.
func TestUploadBinary_OversizedRejected(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	src := io.MultiReader(
		strings.NewReader("\x7fELF"),
		io.LimitReader(zeroReader{}, kernelUploadMaxBytes),
	)
	err := svc.UploadBinary("xray", src, "xray")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized upload must be rejected, got %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// TestCheckLatest_MihomoRollingAlpha: pre-release mihomo — плавающий тег
// Prerelease-Alpha, версия берётся из имени ассета, загрузка — по этому тегу.
func TestCheckLatest_MihomoRollingAlpha(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"tag_name":"Prerelease-Alpha","prerelease":true,"assets":[
			{"name":"mihomo-linux-arm64-alpha-f103639.deb"},
			{"name":"mihomo-linux-arm64-alpha-f103639.gz"}]},
			{"tag_name":"v1.19.31","prerelease":false}]`))
	}))
	defer server.Close()

	for _, tc := range []struct {
		current string
		want    bool
	}{
		{"1.19.31", true},        // stable → alpha
		{"alpha-f103639", false}, // та же alpha
		{"alpha-0000000", true},  // старая alpha
	} {
		svc := NewKernelService(t.TempDir())
		svc.testClient = server.Client()
		svc.githubAPIBase = server.URL
		k := svc.kernels["mihomo"]
		k.CurrentVersion = tc.current
		k.Channel = "preview"
		k.Repo = "MetaCubeX/mihomo"

		if err := svc.CheckLatest(context.Background(), "mihomo"); err != nil {
			t.Fatalf("CheckLatest: %v", err)
		}
		if k.LatestVersion != "alpha-f103639" || k.LatestTag != "Prerelease-Alpha" {
			t.Fatalf("latest = %q tag %q", k.LatestVersion, k.LatestTag)
		}
		if k.HasUpdate != tc.want {
			t.Errorf("current %q: HasUpdate = %v, want %v", tc.current, k.HasUpdate, tc.want)
		}
		url, file := svc.buildDownloadURL(k, "arm64")
		if file != "mihomo-linux-arm64-alpha-f103639.gz" ||
			url != "https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/"+file {
			t.Errorf("download = %q (%q)", url, file)
		}
	}
}

// TestCheckLatest_StableAfterAlpha: с alpha на канале stable предлагается stable.
func TestCheckLatest_StableAfterAlpha(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.19.31"}`))
	}))
	defer server.Close()
	svc := NewKernelService(t.TempDir())
	svc.testClient = server.Client()
	svc.githubAPIBase = server.URL
	k := svc.kernels["mihomo"]
	k.CurrentVersion = "alpha-f103639"
	k.Channel = "stable"
	k.Repo = "MetaCubeX/mihomo"
	if err := svc.CheckLatest(context.Background(), "mihomo"); err != nil {
		t.Fatal(err)
	}
	url, _ := svc.buildDownloadURL(k, "arm64")
	if !k.HasUpdate || url != "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.31/mihomo-linux-arm64-v1.19.31.gz" {
		t.Errorf("HasUpdate=%v url=%q", k.HasUpdate, url)
	}
}

func TestParseVersion_MihomoAlpha(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	out := "Mihomo Meta alpha-f103639 linux arm64 with go1.26.8 Mon Sep 14 13:20:46 UTC 2026\nUse tags: with_gvisor"
	if got := svc.parseVersion("mihomo", out); got != "alpha-f103639" {
		t.Errorf("alpha: got %q", got)
	}
	out = "Mihomo Meta v1.19.31 linux arm64 with go1.26.8 Mon Sep 14 13:20:46 UTC 2026"
	if got := svc.parseVersion("mihomo", out); got != "1.19.31" {
		t.Errorf("stable: got %q", got)
	}
}

// TestRefreshInstalledVersion: после замены бинарника версия перечитывается в
// обход кеша — иначе новое ядро считалось старым и снова предлагалось обновить.
func TestRefreshInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray")
	write := func(v string) {
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"Xray "+v+" (Xray, Penetrates Everything.)\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write("26.9.8")
	svc := NewKernelService(t.TempDir())
	k := svc.kernels["xray"]
	k.BinaryPath = bin
	k.LatestVersion = "26.9.9"
	if v := svc.detectVersion(k); v != "26.9.8" {
		t.Fatalf("old version: %q", v)
	}

	write("26.9.9")
	svc.refreshInstalledVersion(k)
	if k.CurrentVersion != "26.9.9" || k.HasUpdate {
		t.Errorf("after install: current=%q hasUpdate=%v, want 26.9.9/false", k.CurrentVersion, k.HasUpdate)
	}
}

// --- Установка ядра (KERN-02, D-09/D-10) ---

// mihomoScript — stub бинарника mihomo с версией ver (формат вывода как у `mihomo -v`).
func mihomoScript(ver string) []byte {
	return []byte("#!/bin/sh\necho \"Mihomo Version v" + ver + "\"\n")
}

// gzBytes упаковывает content в gz-архив (ассет mihomo).
func gzBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newInstallTestService — сервис с mihomo в t.TempDir() (внутри разрешённого корня),
// без сети и без системных бинарников: PATH пуст, пробные пути ведут в каталог теста.
// installedVersion == "" — бинарника нет. Возвращает сервис и путь бинарника.
func newInstallTestService(t *testing.T, installedVersion string) (*KernelService, string) {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mihomo")
	if installedVersion != "" {
		if err := os.WriteFile(binPath, mihomoScript(installedVersion), 0755); err != nil {
			t.Fatal(err)
		}
	}
	origProbe := mihomoProbePaths
	mihomoProbePaths = []string{binPath}
	t.Cleanup(func() { mihomoProbePaths = origProbe })
	t.Setenv("PATH", t.TempDir())

	svc := NewKernelService(t.TempDir())
	svc.mu.Lock()
	k := svc.kernels["mihomo"]
	k.BinaryPath = binPath
	k.binaryPathCachedAt = time.Now()
	k.LatestVersion = "1.19.0"
	svc.mu.Unlock()
	return svc, binPath
}

// writeGzDownload — шов загрузки: кладёт в dest gz-архив со stub-mihomo версии ver.
func writeGzDownload(t *testing.T, ver string) func(ctx context.Context, url, dest string) error {
	t.Helper()
	data := gzBytes(t, mihomoScript(ver))
	return func(_ context.Context, _, dest string) error {
		return os.WriteFile(dest, data, 0644)
	}
}

// stageRecorder собирает последовательность (status/stage) из хука установки.
type stageRecorder struct {
	mu   sync.Mutex
	list []string
}

func (r *stageRecorder) hook(status, stage string) {
	r.mu.Lock()
	r.list = append(r.list, status+"/"+stage)
	r.mu.Unlock()
}

func (r *stageRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.list...)
}

func waitKernelStatus(t *testing.T, svc *KernelService, name, want string) *KernelInfo {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		k := svc.Get(name)
		if k.Status == want {
			return k
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %q not reached in 5s; last: status=%q stage=%q msg=%q", want, k.Status, k.Stage, k.Message)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestInstall_StagesAndResult: этапы идут starting → downloading → extracting →
// replacing → done; проверка релиза внутри установки тихая и не возвращает статус
// в idle; итог несёт result_kind и result_version.
func TestInstall_StagesAndResult(t *testing.T) {
	svc, _ := newInstallTestService(t, "1.18.0")

	// Версия релиза неизвестна: установка сама спросит релиз (quiet), а не сбросит статус.
	server := newReleaseServer(t, http.StatusOK, `{"tag_name":"v1.19.0"}`)
	svc.SetReleaseSource(server.URL, server.Client())
	svc.mu.Lock()
	svc.kernels["mihomo"].LatestVersion = ""
	svc.mu.Unlock()

	rec := &stageRecorder{}
	release := make(chan struct{})
	gz := writeGzDownload(t, "1.19.0")
	svc.mu.Lock()
	svc.stageHook = rec.hook
	svc.mu.Unlock()
	svc.SetInstallSource("arm64", func(ctx context.Context, url, dest string) error {
		<-release
		return gz(ctx, url, dest)
	})

	done := make(chan error, 1)
	if err := svc.BeginInstall("mihomo", func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}

	// Пока загрузка держится, статус — переходный, а не idle.
	deadline := time.Now().Add(5 * time.Second)
	for {
		k := svc.Get("mihomo")
		if k.Status != "downloading" {
			t.Fatalf("status during download = %q, want downloading (stage %q)", k.Status, k.Stage)
		}
		if k.Stage == KernelStageDownloading {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stage downloading not reached; stage=%q", k.Stage)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("install failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("install did not finish in 5s")
	}

	want := []string{
		"downloading/starting",
		"downloading/downloading",
		"installing/extracting",
		"installing/replacing",
		"done/",
	}
	got := rec.snapshot()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stages = %v, want %v", got, want)
	}

	k := svc.Get("mihomo")
	if k.Status != "done" || k.Stage != "" {
		t.Errorf("final status=%q stage=%q, want done/empty", k.Status, k.Stage)
	}
	if k.ResultKind != KernelResultUpdated || k.ResultVersion != "1.19.0" {
		t.Errorf("result = %q/%q, want updated/1.19.0", k.ResultKind, k.ResultVersion)
	}
	if !strings.Contains(k.Message, "1.19.0") {
		t.Errorf("message should stay English and carry the version: %q", k.Message)
	}
}

// TestInstall_ResultKinds: installed (бинарника не было), updated (версия
// изменилась), reinstalled (та же версия).
func TestInstall_ResultKinds(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		want      string
	}{
		{"нет бинарника", "", KernelResultInstalled},
		{"другая версия", "1.18.0", KernelResultUpdated},
		{"та же версия", "1.19.0", KernelResultReinstalled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, binPath := newInstallTestService(t, tc.installed)
			svc.SetInstallSource("arm64", writeGzDownload(t, "1.19.0"))

			if err := svc.Install("mihomo"); err != nil {
				t.Fatalf("install: %v", err)
			}
			k := svc.Get("mihomo")
			if k.Status != "done" || k.ResultKind != tc.want || k.ResultVersion != "1.19.0" {
				t.Errorf("status=%q kind=%q version=%q, want done/%s/1.19.0", k.Status, k.ResultKind, k.ResultVersion, tc.want)
			}
			if k.CurrentVersion != "1.19.0" {
				t.Errorf("current version = %q, want 1.19.0", k.CurrentVersion)
			}
			if _, err := os.Stat(binPath); err != nil {
				t.Errorf("binary missing after install: %v", err)
			}
		})
	}
}

// TestInstall_FailureKeepsBinary: ошибка загрузки — status failed без этапа,
// рабочий бинарник не тронут.
func TestInstall_FailureKeepsBinary(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")
	svc.SetInstallSource("arm64", func(context.Context, string, string) error {
		return errors.New("boom")
	})
	if err := svc.Install("mihomo"); err == nil {
		t.Fatal("expected download error")
	}
	k := svc.Get("mihomo")
	if k.Status != "failed" || k.Stage != "" || !strings.Contains(k.Message, "Download failed") {
		t.Errorf("status=%q stage=%q message=%q", k.Status, k.Stage, k.Message)
	}
	got, _ := os.ReadFile(binPath)
	if string(got) != string(mihomoScript("1.18.0")) {
		t.Error("binary must stay untouched after a failed download")
	}
}

// TestBeginInstall_ReportsStartingSynchronously: сразу после BeginInstall статус
// уже downloading со stage starting — первый же опрос клиента не видит idle.
func TestBeginInstall_ReportsStartingSynchronously(t *testing.T) {
	svc, _ := newInstallTestService(t, "1.18.0")
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	svc.mu.Lock()
	// Хук держит горутину установки на первом переходе, чтобы Get увидел starting.
	svc.stageHook = func(status, stage string) {
		if stage == KernelStageDownloading {
			once.Do(func() { close(entered) })
			<-release
		}
	}
	svc.mu.Unlock()
	svc.SetInstallSource("arm64", func(context.Context, string, string) error { return errors.New("stop") })

	done := make(chan error, 1)
	if err := svc.BeginInstall("mihomo", func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	<-entered
	k := svc.Get("mihomo")
	if k.Status != "downloading" || k.Stage != KernelStageStarting {
		t.Errorf("right after BeginInstall: status=%q stage=%q, want downloading/starting", k.Status, k.Stage)
	}
	close(release)
	<-done
}

// TestBeginInstall_ConcurrentOnlyOneWins: из N одновременных запросов замок
// берёт ровно один, остальные получают ErrKernelBusy (проверка идёт под -race).
func TestBeginInstall_ConcurrentOnlyOneWins(t *testing.T) {
	svc, _ := newInstallTestService(t, "1.18.0")
	release := make(chan struct{})
	svc.SetInstallSource("arm64", func(context.Context, string, string) error {
		<-release
		return errors.New("stop")
	})

	const n = 16
	var wg sync.WaitGroup
	results := make(chan error, n)
	finished := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- svc.BeginInstall("mihomo", func(err error) { finished <- err })
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	wins, busy := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrKernelBusy):
			busy++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || busy != n-1 {
		t.Fatalf("wins=%d busy=%d, want 1/%d", wins, busy, n-1)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("winning install did not finish")
	}
}

// --- Бэкапы с версией и откат (KERN-02, D-12) ---

// backupFiles — имена файлов в .backup рядом с бинарником (отсортированы).
func backupFiles(t *testing.T, binPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(binPath), ".backup"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// installVersion ставит mihomo версии ver поверх текущего бинарника.
func installVersion(t *testing.T, svc *KernelService, ver string) {
	t.Helper()
	svc.mu.Lock()
	svc.kernels["mihomo"].LatestVersion = ver
	svc.mu.Unlock()
	svc.SetInstallSource("arm64", writeGzDownload(t, ver))
	if err := svc.Install("mihomo"); err != nil {
		t.Fatalf("install %s: %v", ver, err)
	}
}

func TestParseBackupName(t *testing.T) {
	cases := []struct {
		file, kernel string
		ts           int64
		version      string
		ok           bool
	}{
		{"xray.bak.1759100000.26.9.8", "xray", 1759100000, "26.9.8", true},
		{"xray.bak.1759100000", "xray", 1759100000, "", true},
		{"xray.bak.1759100000.1.8.24-rc1", "xray", 1759100000, "1.8.24-rc1", true},
		{"mihomo.bak.1759100000.alpha-f103639", "mihomo", 1759100000, "alpha-f103639", true},
		{"mihomo.bak.1759100000.1.19.1", "xray", 0, "", false},
		{"xray.bak.abc", "xray", 0, "", false},
		{"xray.bak.", "xray", 0, "", false},
		{"kernel.bak.12345", "xray", 0, "", false},
		{"xray.bak.1759100000.../../x", "xray", 1759100000, "", true},
		{"xray.bak.1759100000.1.2.3/../../x", "xray", 1759100000, "", true},
		{"xray.bak.1759100000." + strings.Repeat("1", 70), "xray", 1759100000, "", true},
		{"xray.bak.1759100000.error", "xray", 1759100000, "", true},
	}
	for _, tc := range cases {
		ts, version, ok := parseBackupName(tc.file, tc.kernel)
		if ts != tc.ts || version != tc.version || ok != tc.ok {
			t.Errorf("parseBackupName(%q, %q) = (%d, %q, %v), want (%d, %q, %v)",
				tc.file, tc.kernel, ts, version, ok, tc.ts, tc.version, tc.ok)
		}
	}
}

// TestBackup_NameCarriesVersion: версия кодируется в имени только допустимого вида.
func TestBackup_NameCarriesVersion(t *testing.T) {
	if got := backupFileName("xray", 1759100000, "26.9.8"); got != "xray.bak.1759100000.26.9.8" {
		t.Errorf("backupFileName = %q", got)
	}
	for _, bad := range []string{"error", "unknown", "not installed", "1.2.3/../../x", "1.2.3 x", strings.Repeat("1", 65), ""} {
		if got := backupFileName("xray", 1759100000, bad); got != "xray.bak.1759100000" {
			t.Errorf("backupFileName(%q) = %q, want no version suffix", bad, got)
		}
	}

	svc, binPath := newInstallTestService(t, "1.18.0")
	installVersion(t, svc, "1.19.0")

	files := backupFiles(t, binPath)
	if len(files) != 1 {
		t.Fatalf("backups = %v, want exactly one", files)
	}
	if _, v, ok := parseBackupName(files[0], "mihomo"); !ok || v != "1.18.0" {
		t.Errorf("backup %q parsed as version %q ok=%v, want 1.18.0", files[0], v, ok)
	}
	k := svc.Get("mihomo")
	if !k.HasBackup || k.BackupVersion != "1.18.0" {
		t.Errorf("HasBackup=%v BackupVersion=%q, want true/1.18.0", k.HasBackup, k.BackupVersion)
	}
}

// TestBackup_UnknownVersionHasNoSuffix: версию прежнего бинарника определить не
// удалось — бэкап всё равно есть, без суффикса версии, итог updated.
func TestBackup_UnknownVersionHasNoSuffix(t *testing.T) {
	svc, binPath := newInstallTestService(t, "")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho garbage\n"), 0755); err != nil {
		t.Fatal(err)
	}
	installVersion(t, svc, "1.19.0")

	files := backupFiles(t, binPath)
	if len(files) != 1 {
		t.Fatalf("backups = %v, want one", files)
	}
	if _, v, ok := parseBackupName(files[0], "mihomo"); !ok || v != "" {
		t.Errorf("backup %q: version %q ok=%v, want empty version", files[0], v, ok)
	}
	k := svc.Get("mihomo")
	if k.ResultKind != KernelResultUpdated || !k.HasBackup || k.BackupVersion != "" {
		t.Errorf("kind=%q hasBackup=%v backupVersion=%q, want updated/true/empty", k.ResultKind, k.HasBackup, k.BackupVersion)
	}
}

// TestBackup_SkipsDuplicateVersion: переустановка версии, что уже лежит в
// последнем бэкапе, новую копию не создаёт.
func TestBackup_SkipsDuplicateVersion(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.19.0")
	backupDir := filepath.Join(filepath.Dir(binPath), ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(backupDir, "mihomo.bak.1759100000.1.19.0")
	if err := os.WriteFile(existing, mihomoScript("1.19.0"), 0755); err != nil {
		t.Fatal(err)
	}

	installVersion(t, svc, "1.19.0")

	files := backupFiles(t, binPath)
	if len(files) != 1 || files[0] != "mihomo.bak.1759100000.1.19.0" {
		t.Fatalf("backups = %v, want only the existing one", files)
	}
	if k := svc.Get("mihomo"); k.ResultKind != KernelResultReinstalled {
		t.Errorf("kind = %q, want reinstalled", k.ResultKind)
	}
}

// TestBackup_KeepsThree: хранятся 3 последних бэкапа, самый старый удаляется.
func TestBackup_KeepsThree(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.0.0")
	for _, v := range []string{"1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		installVersion(t, svc, v)
	}

	files := backupFiles(t, binPath)
	if len(files) != 3 {
		t.Fatalf("backups = %v, want 3", files)
	}
	var versions []string
	for _, f := range files {
		_, v, ok := parseBackupName(f, "mihomo")
		if !ok {
			t.Fatalf("unparsable backup %q", f)
		}
		versions = append(versions, v)
	}
	// Порядок файлов не гарантирован: проверяем множество.
	joined := "," + strings.Join(versions, ",") + ","
	for _, want := range []string{"1.1.0", "1.2.0", "1.3.0"} {
		if !strings.Contains(joined, ","+want+",") {
			t.Errorf("versions %v miss %s", versions, want)
		}
	}
	if strings.Contains(joined, ",1.0.0,") {
		t.Errorf("oldest backup 1.0.0 must be pruned: %v", versions)
	}
	if k := svc.Get("mihomo"); k.BackupVersion != "1.3.0" {
		t.Errorf("BackupVersion = %q, want the newest backup 1.3.0", k.BackupVersion)
	}
}

// TestRollback_ConsumesAppliedBackup: откат восстанавливает последний бэкап,
// удаляет применённую копию, следующий откат идёт глубже.
func TestRollback_ConsumesAppliedBackup(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")
	installVersion(t, svc, "1.19.0")
	installVersion(t, svc, "1.20.0")

	if k := svc.Get("mihomo"); k.BackupVersion != "1.19.0" || k.CurrentVersion != "1.20.0" {
		t.Fatalf("before rollback: backup=%q current=%q", k.BackupVersion, k.CurrentVersion)
	}

	if err := svc.Rollback("mihomo"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	k := svc.Get("mihomo")
	if k.Status != "done" || k.Stage != "" || k.ResultKind != KernelResultRolledBack || k.ResultVersion != "1.19.0" {
		t.Errorf("after rollback: status=%q stage=%q kind=%q version=%q", k.Status, k.Stage, k.ResultKind, k.ResultVersion)
	}
	if k.CurrentVersion != "1.19.0" {
		t.Errorf("current = %q, want 1.19.0", k.CurrentVersion)
	}
	if k.BackupVersion != "1.18.0" || !k.HasBackup {
		t.Errorf("next backup = %q hasBackup=%v, want 1.18.0/true", k.BackupVersion, k.HasBackup)
	}
	if len(backupFiles(t, binPath)) != 1 {
		t.Errorf("applied backup must be consumed: %v", backupFiles(t, binPath))
	}

	if err := svc.Rollback("mihomo"); err != nil {
		t.Fatalf("second rollback: %v", err)
	}
	k = svc.Get("mihomo")
	if k.CurrentVersion != "1.18.0" || k.HasBackup || k.BackupVersion != "" {
		t.Errorf("after second rollback: current=%q hasBackup=%v backupVersion=%q", k.CurrentVersion, k.HasBackup, k.BackupVersion)
	}

	err := svc.Rollback("mihomo")
	if err == nil || !strings.Contains(err.Error(), "no backup found") {
		t.Errorf("third rollback: got %v, want 'no backup found'", err)
	}
}

// TestRollback_BusyWhileInstalling: откат и загрузка файла берут тот же замок,
// что и установка.
func TestRollback_BusyWhileInstalling(t *testing.T) {
	svc, _ := newInstallTestService(t, "1.18.0")
	release := make(chan struct{})
	svc.SetInstallSource("arm64", func(context.Context, string, string) error {
		<-release
		return errors.New("stop")
	})
	done := make(chan error, 1)
	if err := svc.BeginInstall("mihomo", func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}

	if err := svc.Rollback("mihomo"); !errors.Is(err, ErrKernelBusy) {
		t.Errorf("rollback while installing: got %v, want ErrKernelBusy", err)
	}
	if err := svc.UploadBinary("mihomo", strings.NewReader("\x7fELF"), "mihomo"); !errors.Is(err, ErrKernelBusy) {
		t.Errorf("upload while installing: got %v, want ErrKernelBusy", err)
	}
	close(release)
	<-done

	// После завершения замок свободен.
	if err := svc.Rollback("mihomo"); errors.Is(err, ErrKernelBusy) {
		t.Errorf("rollback after install finished must not be busy: %v", err)
	}
}

// TestRollback_IgnoresForeignAndLegacyFiles: общий префикс старого формата и
// бэкапы другого ядра в том же каталоге бэкапом этого ядра не считаются.
func TestRollback_IgnoresForeignAndLegacyFiles(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")
	backupDir := filepath.Join(filepath.Dir(binPath), ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kernel.bak.12345", "xray.bak.1759100000.1.8.24", "mihomo.bak.abc"} {
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Бэкап-каталог с тем же именем — не файл.
	if err := os.Mkdir(filepath.Join(backupDir, "mihomo.bak.1759100001"), 0755); err != nil {
		t.Fatal(err)
	}

	if k := svc.Get("mihomo"); k.HasBackup {
		t.Errorf("HasBackup must be false, BackupVersion=%q", k.BackupVersion)
	}
	err := svc.Rollback("mihomo")
	if err == nil || !strings.Contains(err.Error(), "no backup found") {
		t.Errorf("rollback: got %v, want 'no backup found'", err)
	}
	got, _ := os.ReadFile(binPath)
	if string(got) != string(mihomoScript("1.18.0")) {
		t.Error("binary must stay untouched")
	}
}

// TestBackup_ListDoesNotExecBackups: версия бэкапа читается из имени файла, а
// не запуском бинарников из .backup.
func TestBackup_ListDoesNotExecBackups(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")
	backupDir := filepath.Join(filepath.Dir(binPath), ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	script := "#!/bin/sh\necho ran > " + marker + "\necho \"Mihomo Version v9.9.9\"\n"
	if err := os.WriteFile(filepath.Join(backupDir, "mihomo.bak.1759100000.1.17.0"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	svc.List()
	k := svc.Get("mihomo")
	if k.BackupVersion != "1.17.0" || !k.HasBackup {
		t.Errorf("BackupVersion=%q HasBackup=%v, want 1.17.0/true", k.BackupVersion, k.HasBackup)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("backup binary must not be executed on List/Get")
	}
}

// elfLike — содержимое, проходящее проверку isELF (заголовок), для тестов загрузки.
func elfLike(ver string) []byte {
	return append([]byte{0x7f, 'E', 'L', 'F'}, []byte("\n#!/bin/sh\necho 'Mihomo Version v"+ver+"'\n")...)
}

// TestUploadBinary_BackupFailureAborts: ошибка копирования бэкапа прерывает
// замену — прежний бинарник остаётся на месте.
func TestUploadBinary_BackupFailureAborts(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")
	// .backup — обычный файл: создать в нём копию невозможно.
	if err := os.WriteFile(filepath.Join(filepath.Dir(binPath), ".backup"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	err := svc.UploadBinary("mihomo", bytes.NewReader(elfLike("1.21.0")), "mihomo")
	if err == nil {
		t.Fatal("upload must fail when the backup cannot be created")
	}
	got, _ := os.ReadFile(binPath)
	if string(got) != string(mihomoScript("1.18.0")) {
		t.Error("binary must not be replaced when backup failed")
	}
}

// TestUploadBinary_ResultUploaded: успешная загрузка — result_kind uploaded и
// бэкап прежнего бинарника с версией.
func TestUploadBinary_ResultUploaded(t *testing.T) {
	svc, binPath := newInstallTestService(t, "1.18.0")

	if err := svc.UploadBinary("mihomo", bytes.NewReader(elfLike("1.21.0")), "mihomo"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	k := svc.Get("mihomo")
	if k.Status != "done" || k.Stage != "" || k.ResultKind != KernelResultUploaded {
		t.Errorf("status=%q stage=%q kind=%q, want done/empty/uploaded", k.Status, k.Stage, k.ResultKind)
	}
	if k.ResultVersion != k.CurrentVersion {
		t.Errorf("ResultVersion=%q, CurrentVersion=%q", k.ResultVersion, k.CurrentVersion)
	}
	if !k.HasBackup || k.BackupVersion != "1.18.0" {
		t.Errorf("HasBackup=%v BackupVersion=%q, want true/1.18.0", k.HasBackup, k.BackupVersion)
	}
	if len(backupFiles(t, binPath)) != 1 {
		t.Errorf("backups = %v", backupFiles(t, binPath))
	}
}

// --- Каталог конфигурации Mihomo (KERN-02, D-11) ---

// TestInstall_CreatesMihomoConfigDir: успешная установка mihomo создаёт пустой
// каталог конфигурации 0755 и не пишет в него config.yaml.
func TestInstall_CreatesMihomoConfigDir(t *testing.T) {
	t.Run("создаёт пустой каталог 0755", func(t *testing.T) {
		svc, _ := newInstallTestService(t, "")
		cfgDir := filepath.Join(t.TempDir(), "mihomo")
		svc.SetMihomoConfigDir(cfgDir)
		svc.SetInstallSource("arm64", writeGzDownload(t, "1.19.0"))

		if err := svc.Install("mihomo"); err != nil {
			t.Fatalf("install: %v", err)
		}
		info, err := os.Stat(cfgDir)
		if err != nil {
			t.Fatalf("config dir must exist after install: %v", err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0755 {
			t.Errorf("config dir mode = %v, want directory 0755", info.Mode())
		}
		entries, _ := os.ReadDir(cfgDir)
		if len(entries) != 0 {
			t.Errorf("config dir must stay empty (no seeded config): %v", entries)
		}
	})

	t.Run("существующий каталог не трогается", func(t *testing.T) {
		svc, _ := newInstallTestService(t, "1.18.0")
		cfgDir := filepath.Join(t.TempDir(), "mihomo")
		if err := os.MkdirAll(cfgDir, 0700); err != nil {
			t.Fatal(err)
		}
		keep := filepath.Join(cfgDir, "config.yaml")
		if err := os.WriteFile(keep, []byte("mixed-port: 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		svc.SetMihomoConfigDir(cfgDir)
		svc.SetInstallSource("arm64", writeGzDownload(t, "1.19.0"))
		if err := svc.Install("mihomo"); err != nil {
			t.Fatalf("install: %v", err)
		}
		if data, _ := os.ReadFile(keep); string(data) != "mixed-port: 1\n" {
			t.Errorf("existing config must be untouched, got %q", data)
		}
		if info, _ := os.Stat(cfgDir); info.Mode().Perm() != 0700 {
			t.Errorf("existing dir mode changed to %v", info.Mode().Perm())
		}
	})

	t.Run("пустой путь ничего не создаёт", func(t *testing.T) {
		svc, _ := newInstallTestService(t, "")
		parent := t.TempDir()
		svc.SetMihomoConfigDir("")
		svc.SetInstallSource("arm64", writeGzDownload(t, "1.19.0"))
		if err := svc.Install("mihomo"); err != nil {
			t.Fatalf("install: %v", err)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 0 {
			t.Errorf("nothing must be created: %v", entries)
		}
	})

	t.Run("путь вне разрешённых корней отклоняется, установка проходит", func(t *testing.T) {
		svc, _ := newInstallTestService(t, "")
		outside := "/etc/xcp-test-mihomo-dir"
		svc.SetMihomoConfigDir(outside)
		svc.SetInstallSource("arm64", writeGzDownload(t, "1.19.0"))

		var logBuf bytes.Buffer
		log.SetOutput(&logBuf)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })

		if err := svc.Install("mihomo"); err != nil {
			t.Fatalf("install must succeed despite a rejected dir: %v", err)
		}
		if _, err := os.Stat(outside); err == nil {
			_ = os.Remove(outside)
			t.Fatal("dir outside allowed roots must not be created")
		}
		if !strings.Contains(logBuf.String(), "mihomo config dir rejected") {
			t.Errorf("rejection must be logged, log: %q", logBuf.String())
		}
		if k := svc.Get("mihomo"); k.Status != "done" {
			t.Errorf("status = %q, want done", k.Status)
		}
	})

	t.Run("загрузка файла mihomo тоже создаёт каталог", func(t *testing.T) {
		svc, _ := newInstallTestService(t, "1.18.0")
		cfgDir := filepath.Join(t.TempDir(), "mihomo")
		svc.SetMihomoConfigDir(cfgDir)
		if err := svc.UploadBinary("mihomo", bytes.NewReader(elfLike("1.21.0")), "mihomo"); err != nil {
			t.Fatalf("upload: %v", err)
		}
		if info, err := os.Stat(cfgDir); err != nil || !info.IsDir() {
			t.Errorf("config dir must exist after upload: %v", err)
		}
	})

	t.Run("установка xray каталог mihomo не создаёт", func(t *testing.T) {
		dir := t.TempDir()
		binPath := filepath.Join(dir, "xray")
		origProbe := xrayProbePaths
		xrayProbePaths = []string{binPath}
		t.Cleanup(func() { xrayProbePaths = origProbe })
		t.Setenv("PATH", t.TempDir())

		svc := NewKernelService(t.TempDir())
		svc.mu.Lock()
		k := svc.kernels["xray"]
		k.BinaryPath = binPath
		k.binaryPathCachedAt = time.Now()
		k.LatestVersion = "26.9.9"
		svc.mu.Unlock()
		cfgDir := filepath.Join(t.TempDir(), "mihomo")
		svc.SetMihomoConfigDir(cfgDir)

		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		w, err := zw.Create("xray")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("#!/bin/sh\necho \"Xray 26.9.9 (Xray, Penetrates Everything.)\"\n"))
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		svc.SetInstallSource("arm64", func(_ context.Context, _, dest string) error {
			return os.WriteFile(dest, zipBuf.Bytes(), 0644)
		})

		if err := svc.Install("xray"); err != nil {
			t.Fatalf("install xray: %v", err)
		}
		if k := svc.Get("xray"); k.ResultKind != KernelResultInstalled || k.ResultVersion != "26.9.9" {
			t.Errorf("kind=%q version=%q, want installed/26.9.9", k.ResultKind, k.ResultVersion)
		}
		if _, err := os.Stat(cfgDir); err == nil {
			t.Error("xray install must not create the mihomo config dir")
		}
	})
}
