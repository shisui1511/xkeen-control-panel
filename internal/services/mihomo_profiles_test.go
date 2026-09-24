package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newProfilesFixture(t *testing.T, managed bool) (*MihomoProfileService, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "mihomo")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if managed {
		if err := os.WriteFile(filepath.Join(dir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("mode: global\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewMihomoProfileService(dir, filepath.Join(root, "xcp")), dir
}

func readConfig(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProfiles_ListCreateRenameDelete(t *testing.T) {
	svc, dir := newProfilesFixture(t, true)

	st, err := svc.List()
	if err != nil || !st.Managed || st.Active != "default" || len(st.Profiles) != 1 || !st.Profiles[0].Active {
		t.Fatalf("initial list: %+v %v", st, err)
	}

	if err := svc.Create("work", "", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create("blank", "", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create("work", "", false); !errors.Is(err, ErrProfileExists) {
		t.Fatalf("duplicate: %v", err)
	}
	for _, bad := range []string{"../x", "a/b", "", ".hidden", "x y"} {
		if err := svc.Create(bad, "", true); !errors.Is(err, ErrProfileInvalidName) {
			t.Errorf("name %q: %v", bad, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "profiles", "work.yaml")); string(data) != "mode: rule\n" {
		t.Fatalf("copy of active: %q", data)
	}

	if err := svc.Delete("default"); !errors.Is(err, ErrProfileActive) {
		t.Fatalf("delete active: %v", err)
	}
	if err := svc.Delete("blank"); err != nil {
		t.Fatal(err)
	}
	if backups, _ := filepath.Glob(filepath.Join(svc.backupDir, "blank.*.yaml")); len(backups) != 1 {
		t.Fatalf("expected backup of deleted profile, got %v", backups)
	}

	// Renaming the active profile keeps config.yaml pointing at it.
	if err := svc.Rename("default", "main"); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.List()
	if st.Active != "main" || readConfig(t, dir) != "mode: rule\n" {
		t.Fatalf("after rename: %+v", st)
	}
}

func TestProfiles_ActivateAndRollback(t *testing.T) {
	svc, dir := newProfilesFixture(t, true)
	if err := os.WriteFile(filepath.Join(dir, "profiles", "good.yaml"), []byte("mode: global\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "broken.yaml"), []byte("bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	restarts := 0
	healthy := true
	svc.Validate = func(path string) error {
		if filepath.Base(path) == "broken.yaml" {
			return errors.New("parse error")
		}
		return nil
	}
	svc.CoreActive = func() bool { return true }
	svc.Restart = func() error { restarts++; return nil }
	svc.Healthy = func() bool { return healthy }

	res, err := svc.Activate("broken")
	if err != nil || res.Error == "" || res.Active != "default" || restarts != 0 {
		t.Fatalf("invalid profile must not be activated: %+v %v", res, err)
	}

	res, err = svc.Activate("good")
	if err != nil || res.Active != "good" || !res.Restarted || res.RolledBack || readConfig(t, dir) != "mode: global\n" {
		t.Fatalf("activate: %+v %v", res, err)
	}

	// Core fails to come up: previous profile is restored and restarted.
	healthy = false
	res, err = svc.Activate("default")
	if err != nil || !res.RolledBack || res.Active != "good" || readConfig(t, dir) != "mode: global\n" || restarts != 3 {
		t.Fatalf("rollback: %+v %v restarts=%d", res, err, restarts)
	}
}

func TestProfiles_AdoptPlainConfig(t *testing.T) {
	svc, dir := newProfilesFixture(t, false)
	st, _ := svc.List()
	if st.Managed {
		t.Fatal("plain config reported as managed")
	}
	if _, err := svc.Activate("x"); !errors.Is(err, ErrProfileNotFound) && !errors.Is(err, ErrProfilesUnmanaged) {
		t.Fatalf("activate unmanaged: %v", err)
	}
	if err := svc.Adopt("default"); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.List()
	if !st.Managed || st.Active != "default" || readConfig(t, dir) != "mode: global\n" {
		t.Fatalf("after adopt: %+v", st)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "config.yaml")); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config.yaml must become a symlink")
	}
}
