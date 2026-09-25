package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

func TestBackupFileName(t *testing.T) {
	now := time.Unix(1800000000, 0)
	cases := map[string]string{
		"v0.29.0":                   "xcp.bak.1800000000.0.29.0",
		"0.29.0-rc.2":               "xcp.bak.1800000000.0.29.0-rc.2",
		"v0.29.0-dev.5+g1a2b3c4d":   "xcp.bak.1800000000.0.29.0-dev.5",
		"":                          "xcp.bak.1800000000",
		"weird/version":             "xcp.bak.1800000000",
		"dev":                       "xcp.bak.1800000000.dev",
		"v0.29.0-dev.5+g1a2b.dirty": "xcp.bak.1800000000.0.29.0-dev.5",
	}
	for version, want := range cases {
		if got := backupFileName(now, version); got != want {
			t.Errorf("backupFileName(%q) = %q, want %q", version, got, want)
		}
	}
}

func writeBackup(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte("x"), size), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestListBackups(t *testing.T) {
	dir := t.TempDir()
	writeBackup(t, dir, "xcp.bak.1700000000", 10)             // старая копия без версии
	writeBackup(t, dir, "xcp.bak.1800000000.0.29.0-rc.1", 20) // новая
	writeBackup(t, dir, "xcp.bak.1750000000.0.28.0", 30)
	writeBackup(t, dir, "notes.txt", 1) // посторонний файл
	if err := os.Mkdir(filepath.Join(dir, "xcp.bak.1900000000"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := listBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []UpdateBackup{
		{Name: "xcp.bak.1800000000.0.29.0-rc.1", Version: "0.29.0-rc.1", CreatedAt: 1800000000, Size: 20},
		{Name: "xcp.bak.1750000000.0.28.0", Version: "0.28.0", CreatedAt: 1750000000, Size: 30},
		{Name: "xcp.bak.1700000000", CreatedAt: 1700000000, Size: 10},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d backups: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("backup %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if missing, err := listBackups(filepath.Join(dir, "nope")); err != nil || len(missing) != 0 {
		t.Fatalf("missing dir: %v, %v", missing, err)
	}
}

func TestResolveBackup(t *testing.T) {
	dir := t.TempDir()
	writeBackup(t, dir, "xcp.bak.1700000000", 1)
	writeBackup(t, dir, "xcp.bak.1800000000.0.29.0", 1)

	if p, err := resolveBackup(dir, ""); err != nil || filepath.Base(p) != "xcp.bak.1800000000.0.29.0" {
		t.Fatalf("latest: %q, %v", p, err)
	}
	if p, err := resolveBackup(dir, "xcp.bak.1700000000"); err != nil || filepath.Base(p) != "xcp.bak.1700000000" {
		t.Fatalf("by name: %q, %v", p, err)
	}
	for _, bad := range []string{"../config.json", "xcp.bak.1700000000/../../etc", "config.json", "xcp.bak.123", "/etc/passwd"} {
		if _, err := resolveBackup(dir, bad); err == nil {
			t.Errorf("resolveBackup(%q) must fail", bad)
		}
	}
	if _, err := resolveBackup(t.TempDir(), ""); err == nil {
		t.Error("empty dir must fail")
	}
}

func TestUpdateBackupsHandler(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backup")
	if err := os.Mkdir(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBackup(t, backupDir, "xcp.bak.1800000000.0.29.0", 5)
	api := &API{cfg: &config.Config{DataDir: dataDir}}

	rec := httptest.NewRecorder()
	api.UpdateBackups(rec, httptest.NewRequest(http.MethodGet, "/api/update/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Data []UpdateBackup `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Version != "0.29.0" {
		t.Fatalf("unexpected backups: %+v", resp.Data)
	}
}

// TestUpdateRollback_UnknownBackup verifies that a rollback to a backup that is
// not in the backup directory is refused before anything is touched.
func TestUpdateRollback_UnknownBackup(t *testing.T) {
	setUpdateState(UpdateStatus{Status: "idle"})
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "backup"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBackup(t, filepath.Join(dataDir, "backup"), "xcp.bak.1800000000", 1)
	api := &API{cfg: &config.Config{DataDir: dataDir}}

	for _, body := range []string{`{"backup":"../../config.json"}`, `{"backup":"xcp.bak.1"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/update/rollback", strings.NewReader(body))
		api.UpdateRollback(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("body %s: status %d, want 404", body, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/update/rollback", strings.NewReader(`{bad`))
	api.UpdateRollback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status %d, want 400", rec.Code)
	}
}

func TestUpdateRollback_BusyState(t *testing.T) {
	setUpdateState(UpdateStatus{Status: "downloading"})
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	api := &API{cfg: &config.Config{DataDir: t.TempDir()}}
	rec := httptest.NewRecorder()
	api.UpdateRollback(rec, httptest.NewRequest(http.MethodPost, "/api/update/rollback", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
}

func TestReleaseNotesBetween(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v0.29.0-dev", Prerelease: true, Body: "dev"},
		{TagName: "v0.29.0-rc.2", Prerelease: true, Body: "rc2"},
		{TagName: "v0.28.0", Body: "28"},
		{TagName: "v0.27.3", Body: "27.3"},
		{TagName: "v0.27.2", Body: "27.2"},
		{TagName: "v0.27.1", Body: "27.1"},
	}
	versions := func(notes []ReleaseNote) string {
		var vs []string
		for _, n := range notes {
			vs = append(vs, n.Version)
		}
		return strings.Join(vs, ",")
	}

	if got := versions(releaseNotesBetween(releases, "stable", "0.27.1", "0.28.0")); got != "0.28.0,0.27.3,0.27.2" {
		t.Errorf("stable skip: %s", got)
	}
	if got := versions(releaseNotesBetween(releases, "beta", "0.28.0", "0.29.0-rc.2")); got != "0.29.0-rc.2,0.29.0-dev" {
		t.Errorf("beta from stable: %s", got)
	}
	// Dev-сборка той же версии: rolling-тег v0.29.0-dev не новее dev.5
	if got := versions(releaseNotesBetween(releases, "beta", "0.29.0-dev.5", "0.29.0-rc.2")); got != "0.29.0-rc.2" {
		t.Errorf("beta from dev build: %s", got)
	}
}

func TestReleaseAssetSize(t *testing.T) {
	rel := githubRelease{TagName: "v0.29.0"}
	rel.Assets = append(rel.Assets,
		struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}{"xcp_v0.29.0_arm64", 19000000},
		struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		}{"xcp_v0.29.0_arm64.gz", 7000000},
	)
	old := githubRelease{TagName: "v0.28.0"}
	old.Assets = append(old.Assets, struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}{"xcp_v0.28.0_arm64", 19500000})
	releases := []githubRelease{rel, old}

	if got := releaseAssetSize(releases, "0.29.0", "xcp_v0.29.0_arm64"); got != 7000000 {
		t.Errorf("gz size = %d", got)
	}
	if got := releaseAssetSize(releases, "0.28.0", "xcp_v0.28.0_arm64"); got != 19500000 {
		t.Errorf("plain size = %d", got)
	}
	if got := releaseAssetSize(releases, "0.30.0", "xcp_v0.30.0_arm64"); got != 0 {
		t.Errorf("unknown size = %d", got)
	}
}

func TestProgressReader(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 3*progressStep+10)
	var calls [][2]int64
	pr := &progressReader{r: bytes.NewReader(data), total: int64(len(data)), fn: func(done, total int64) {
		calls = append(calls, [2]int64{done, total})
	}}
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 || calls[len(calls)-1][0] != int64(len(data)) {
		t.Fatalf("last report must be the full size: %v", calls)
	}
	if len(calls) > 5 {
		t.Fatalf("too many reports (%d), throttling broken", len(calls))
	}
}
