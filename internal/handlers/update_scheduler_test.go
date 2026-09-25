package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

func TestInstallWindow(t *testing.T) {
	at := func(hm string) time.Time {
		tm, _ := time.Parse("15:04", hm)
		return time.Date(2026, 9, 26, tm.Hour(), tm.Minute(), 0, 0, time.Local)
	}
	cases := []struct {
		window, now string
		want        bool
	}{
		{"03:00-05:00", "03:00", true},
		{"03:00-05:00", "04:59", true},
		{"03:00-05:00", "05:00", false},
		{"03:00-05:00", "02:59", false},
		{"23:00-01:00", "23:30", true}, // через полночь
		{"23:00-01:00", "00:30", true},
		{"23:00-01:00", "01:00", false},
		{"23:00-01:00", "12:00", false},
		{"bad", "03:00", false},
	}
	for _, c := range cases {
		if got := inInstallWindow(at(c.now), c.window); got != c.want {
			t.Errorf("inInstallWindow(%s, %s) = %v, want %v", c.now, c.window, got, c.want)
		}
	}
	for _, bad := range []string{"", "3:00-5:00", "03:00", "25:00-05:00", "03:60-05:00", "03:00-03:00", "aa:bb-cc:dd"} {
		if _, _, err := parseInstallWindow(bad); err == nil {
			t.Errorf("parseInstallWindow(%q) must fail", bad)
		}
	}
}

type schedFixture struct {
	s        *UpdateScheduler
	cfg      *config.Config
	now      time.Time
	version  string
	checks   int
	installs int
	latest   string
	checkErr error
	busy     bool
}

func newSchedFixture(t *testing.T) *schedFixture {
	t.Helper()
	setUpdateState(UpdateStatus{Status: "idle"})
	t.Cleanup(func() { setUpdateState(UpdateStatus{Status: "idle"}) })

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ConfigPath = filepath.Join(cfg.DataDir, "config.json")
	f := &schedFixture{
		cfg:     cfg,
		now:     time.Date(2026, 9, 26, 3, 30, 0, 0, time.Local),
		version: "0.28.0",
		latest:  "0.29.0",
	}
	f.s = NewUpdateScheduler(&API{cfg: cfg})
	f.s.now = func() time.Time { return f.now }
	f.s.version = func() string { return f.version }
	f.s.check = func(channel, current string) (*UpdateInfo, error) {
		f.checks++
		if f.checkErr != nil {
			return nil, f.checkErr
		}
		return &UpdateInfo{LatestVersion: f.latest, HasUpdate: updateAvailable(f.latest, current)}, nil
	}
	f.s.install = func(channel string) bool {
		if f.busy {
			return false
		}
		f.installs++
		return true
	}
	return f
}

func (f *schedFixture) journal(t *testing.T) autoUpdateJournal {
	t.Helper()
	var j autoUpdateJournal
	data, err := os.ReadFile(filepath.Join(f.cfg.DataDir, autoUpdateFile))
	if err != nil {
		return j
	}
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestUpdateScheduler_ChecksPeriodicallyWithoutInstalling(t *testing.T) {
	f := newSchedFixture(t)
	f.s.tick()
	st := f.s.State()
	if f.checks != 1 || !st.HasUpdate || st.LatestVersion != "0.29.0" || f.installs != 0 {
		t.Fatalf("first tick: checks=%d installs=%d state=%+v", f.checks, f.installs, st)
	}

	f.now = f.now.Add(time.Hour)
	f.s.tick()
	if f.checks != 1 {
		t.Fatalf("checked again before the interval: %d", f.checks)
	}
	f.now = f.now.Add(updateCheckInterval)
	f.s.tick()
	if f.checks != 2 {
		t.Fatalf("no recheck after the interval: %d", f.checks)
	}

	f.cfg.UpdateChannel = "beta"
	f.s.tick()
	if f.checks != 3 {
		t.Fatalf("channel change must trigger a check: %d", f.checks)
	}
}

func TestUpdateScheduler_AutoCheckOff(t *testing.T) {
	f := newSchedFixture(t)
	f.cfg.UpdateAutoCheck = false
	f.s.tick()
	if f.checks != 0 {
		t.Fatalf("auto check disabled but checked %d times", f.checks)
	}
}

func TestUpdateScheduler_CheckError(t *testing.T) {
	f := newSchedFixture(t)
	f.checkErr = errors.New("GitHub API: 403 rate limit")
	f.s.tick()
	if st := f.s.State(); st.Error == "" || st.HasUpdate {
		t.Fatalf("error must be kept: %+v", st)
	}
}

func TestUpdateScheduler_AutoInstallOnlyInWindow(t *testing.T) {
	f := newSchedFixture(t)
	f.cfg.UpdateAutoInstall = true
	f.cfg.UpdateInstallWindow = "04:00-05:00"

	f.s.tick() // 03:30 — вне окна
	if f.installs != 0 {
		t.Fatal("installed outside the window")
	}

	f.now = f.now.Add(40 * time.Minute) // 04:10
	f.s.tick()
	if f.installs != 1 {
		t.Fatalf("installs = %d, want 1", f.installs)
	}
	j := f.journal(t)
	if j.Pending == nil || j.Pending.Version != "0.29.0" || j.Pending.From != "0.28.0" {
		t.Fatalf("pending not journaled: %+v", j)
	}

	f.s.tick()
	if f.installs != 1 {
		t.Fatal("started a second install while one is pending")
	}
}

func TestUpdateScheduler_AutoInstallBusyIsRetried(t *testing.T) {
	f := newSchedFixture(t)
	f.cfg.UpdateAutoInstall = true
	f.busy = true
	f.s.tick()
	if f.installs != 0 || f.journal(t).Pending != nil {
		t.Fatal("busy install must not leave a pending record")
	}
	f.busy = false
	f.s.tick()
	if f.installs != 1 {
		t.Fatal("install not retried after the manual update finished")
	}
}

func TestUpdateScheduler_ResolvesPendingAfterRestart(t *testing.T) {
	t.Run("new version runs", func(t *testing.T) {
		f := newSchedFixture(t)
		f.cfg.UpdateAutoInstall = true
		f.s.tick()

		// Новый процесс: версия сменилась
		f.version = "0.29.0"
		next := NewUpdateScheduler(&API{cfg: f.cfg})
		next.version = func() string { return f.version }
		next.now = func() time.Time { return f.now }
		next.loadJournal()
		next.resolvePending(f.version)

		st := next.State()
		if st.LastAutoInstall == nil || !st.LastAutoInstall.OK || st.FailedVersion != "" {
			t.Fatalf("success not recorded: %+v", st)
		}
	})

	t.Run("rolled back to the old version", func(t *testing.T) {
		f := newSchedFixture(t)
		f.cfg.UpdateAutoInstall = true
		f.s.tick()

		next := NewUpdateScheduler(&API{cfg: f.cfg})
		next.version = func() string { return "0.28.0" }
		next.now = func() time.Time { return f.now }
		next.check = f.s.check
		installs := 0
		next.install = func(string) bool { installs++; return true }
		next.loadJournal()
		next.resolvePending("0.28.0")

		st := next.State()
		if st.LastAutoInstall == nil || st.LastAutoInstall.OK || st.FailedVersion != "0.29.0" {
			t.Fatalf("failure not recorded: %+v", st)
		}

		// Та же версия больше не ставится автоматически, более новая — ставится
		next.tick()
		if installs != 0 {
			t.Fatal("failed version installed again")
		}
		f.latest = "0.29.1"
		next.now = func() time.Time { return f.now.Add(24 * time.Hour) } // следующая ночь, снова в окне
		next.tick()
		if installs != 1 {
			t.Fatal("newer version must be installed")
		}
	})
}

func TestUpdateScheduler_FailureBeforeRestart(t *testing.T) {
	f := newSchedFixture(t)
	f.cfg.UpdateAutoInstall = true
	f.s.tick()

	// Скачивание или контрольная сумма упали — процесс не перезапускался
	setUpdateState(UpdateStatus{Status: "failed"})
	f.s.tick()
	if st := f.s.State(); st.FailedVersion != "0.29.0" || f.installs != 1 {
		t.Fatalf("pre-restart failure not recorded: installs=%d state=%+v", f.installs, st)
	}
}

func TestUpdateSettingsHandler(t *testing.T) {
	f := newSchedFixture(t)
	api := f.s.api
	api.SetUpdateScheduler(f.s)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.UpdateSettingsHandler(rec, httptest.NewRequest(http.MethodPost, "/api/update/settings", strings.NewReader(body)))
		return rec
	}

	rec := post(`{"auto_install":true,"install_window":"23:30-01:00"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Data UpdateCheckState `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.AutoInstall || !resp.Data.AutoCheck || resp.Data.InstallWindow != "23:30-01:00" {
		t.Fatalf("unexpected state: %+v", resp.Data)
	}
	saved, err := config.Load(f.cfg.ConfigPath)
	if err != nil || !saved.UpdateAutoInstall || saved.UpdateInstallWindow != "23:30-01:00" {
		t.Fatalf("config not saved: %+v, %v", saved, err)
	}

	if rec := post(`{"install_window":"05:00-05:00"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid window: status %d", rec.Code)
	}
	if rec := post(`{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status %d", rec.Code)
	}
	if f.cfg.UpdateInstallWindow != "23:30-01:00" {
		t.Fatal("rejected request changed the config")
	}
}

func TestConfigDefaults_AutoUpdate(t *testing.T) {
	cfg := config.Default()
	if !cfg.UpdateAutoCheck || cfg.UpdateAutoInstall || cfg.UpdateInstallWindow != "03:00-05:00" {
		t.Fatalf("defaults: check=%v install=%v window=%q", cfg.UpdateAutoCheck, cfg.UpdateAutoInstall, cfg.UpdateInstallWindow)
	}

	// Старый config.json без новых ключей получает значения по умолчанию
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"port": 8090, "update_channel": "beta"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.UpdateAutoCheck || loaded.UpdateAutoInstall || loaded.UpdateInstallWindow != "03:00-05:00" {
		t.Fatalf("legacy config: %+v", loaded)
	}
}
