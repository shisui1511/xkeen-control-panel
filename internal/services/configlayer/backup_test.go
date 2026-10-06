package configlayer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackup_Retention5(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "backup", "config-layer")
	base := int64(1_700_000_000_000_000_000)
	for i := range 7 {
		dir := filepath.Join(root, fmt.Sprintf("apply-%d", base+int64(i)))
		writeTestFile(t, filepath.Join(dir, "xray", "a.json"), "x")
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(root, "state", "state.1.json"), "{}")
	writeTestFile(t, filepath.Join(dataDir, "backup", "xkeen", "keep.txt"), "k")

	if err := PruneBackups(dataDir, BackupRetention); err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}

	sets := backupSets(t, dataDir)
	if len(sets) != 5 {
		t.Fatalf("наборов = %d, want 5: %v", len(sets), sets)
	}
	for i, want := range []int64{2, 3, 4, 5, 6} {
		if got := filepath.Base(sets[i]); got != fmt.Sprintf("apply-%d", base+want) {
			t.Errorf("набор %d = %s, want apply-%d (остаются новейшие)", i, got, base+want)
		}
	}
	for _, keep := range []string{
		filepath.Join(root, "state", "state.1.json"),
		filepath.Join(dataDir, "backup", "xkeen", "keep.txt"),
	} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("ротация тронула %s: %v", keep, err)
		}
	}
	if err := PruneBackups(filepath.Join(dataDir, "nothing"), 5); err != nil {
		t.Errorf("PruneBackups без каталога копий = %v, want nil", err)
	}
}

func TestBackup_Permissions(t *testing.T) {
	dataDir := t.TempDir()
	roots := newTestRoots(t)
	writeTestFile(t, filepath.Join(roots.Mihomo, "proxy_providers", "xcp-a.yaml"), "proxies: []\n")

	set, err := NewBackupSet(dataDir, time.Unix(1_700_000_000, 0), "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Save(roots, "mihomo:proxy_providers/xcp-a.yaml", KernelMihomo, "proxy_providers/xcp-a.yaml", ReasonOverwrite); err != nil {
		t.Fatal(err)
	}
	if err := set.WriteMeta(); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{
		set.Dir:                          0o700,
		filepath.Join(set.Dir, "mihomo"): 0o700,
		filepath.Join(set.Dir, "mihomo", "proxy_providers"):               0o700,
		filepath.Join(set.Dir, "mihomo", "proxy_providers", "xcp-a.yaml"): 0o600,
		filepath.Join(set.Dir, "meta.json"):                               0o600,
	} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s): %v", path, err)
		}
		if got := st.Mode().Perm(); got != want {
			t.Errorf("%s: права %o, want %o", path, got, want)
		}
	}
	if rel, err := filepath.Rel(dataDir, set.Dir); err != nil || filepath.Dir(rel) != filepath.Join("backup", "config-layer") {
		t.Errorf("набор лежит в %s, want <data_dir>/backup/config-layer/apply-*", set.Dir)
	}
}
