package configlayer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newJournalFixture создаёт набор копий (A существовал, B — новый), рабочие
// файлы с «новыми» байтами и журнал в состоянии.
func newJournalFixture(t *testing.T) (*Store, Roots, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	roots := newTestRoots(t)
	store, err := OpenStore(dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	const relA, relB = "04_outbounds.xcp-a.tail.json", "04_outbounds.xcp-b.tail.json"
	absA := filepath.Join(roots.Xray, relA)
	absB := filepath.Join(roots.Xray, relB)
	writeTestFile(t, absA, "OLD-A")

	set, err := NewBackupSet(dataDir, time.Unix(1_700_000_000, 0), "user")
	if err != nil {
		t.Fatal(err)
	}
	keyA, keyB := ManifestKey(KernelXray, relA), ManifestKey(KernelXray, relB)
	if _, err := set.Save(roots, keyA, KernelXray, relA, ReasonOverwrite); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Save(roots, keyB, KernelXray, relB, ReasonOverwrite); err != nil {
		t.Fatal(err)
	}
	if err := set.WriteMeta(); err != nil {
		t.Fatal(err)
	}
	// Обрыв питания: файлы уже содержат новые байты, журнал не очищен.
	writeTestFile(t, absA, "NEW-A")
	writeTestFile(t, absB, "NEW-B")
	err = store.Update(func(st *State) error {
		st.Journal = &Journal{
			BackupDir: set.Dir, Trigger: "user", StartedAt: time.Unix(1_700_000_000, 0),
			Files: []JournalFile{{Key: keyA, Existed: true, NewHash: HashContent([]byte("NEW-A"))}, {Key: keyB, NewHash: HashContent([]byte("NEW-B"))}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, roots, absA, absB
}

func TestStore_JournalRecovery(t *testing.T) {
	store, roots, absA, absB := newJournalFixture(t)

	recovered, err := RecoverJournal(store, roots)

	if err != nil || !recovered {
		t.Fatalf("RecoverJournal = %v, %v, want true, nil", recovered, err)
	}
	if got := readFileString(t, absA); got != "OLD-A" {
		t.Errorf("файл A = %q, want OLD-A", got)
	}
	requireAbsent(t, absB)
	st := store.Snapshot()
	if st.Journal != nil {
		t.Errorf("Journal = %+v, want nil", st.Journal)
	}
	if !st.Notices.RecoveredFromJournal {
		t.Error("Notices.RecoveredFromJournal = false, want true")
	}

	// Повторный вызов ничего не делает.
	writeTestFile(t, absA, "USER-EDIT")
	again, err := RecoverJournal(store, roots)
	if err != nil || again {
		t.Errorf("повторный RecoverJournal = %v, %v, want false, nil", again, err)
	}
	if got := readFileString(t, absA); got != "USER-EDIT" {
		t.Errorf("повторный вызов изменил файл: %q", got)
	}
}

func TestRecoverJournal_MissingBackupDir(t *testing.T) {
	dataDir := t.TempDir()
	roots := newTestRoots(t)
	store, err := OpenStore(dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Update(func(st *State) error {
		st.Journal = &Journal{BackupDir: filepath.Join(dataDir, "backup", "config-layer", "apply-404"), Trigger: "user"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := RecoverJournal(store, roots)

	if err == nil {
		t.Error("ошибки нет, want ошибка о недоступном наборе копий")
	}
	if recovered {
		t.Error("recovered = true, want false: файлы вернуть не из чего")
	}
	st := store.Snapshot()
	if st.Journal != nil {
		t.Errorf("Journal = %+v, want очищен (иначе каждый старт повторит сбой)", st.Journal)
	}
	if st.Notices.RecoveredFromJournal {
		t.Error("уведомление recovered_from_journal выставлено без восстановления")
	}
}

func TestCleanupStale(t *testing.T) {
	dataDir := t.TempDir()
	roots := newTestRoots(t)
	now := time.Now()
	age := func(path string, d time.Duration) {
		t.Helper()
		ts := now.Add(-d)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	oldTmp := filepath.Join(dataDir, "tmp", "apply-old")
	newTmp := filepath.Join(dataDir, "tmp", "apply-new")
	otherTmp := filepath.Join(dataDir, "tmp", "other-old")
	for _, d := range []string{oldTmp, newTmp, otherTmp} {
		writeTestFile(t, filepath.Join(d, "x.json"), "x")
	}
	age(oldTmp, 2*time.Hour)
	age(newTmp, 5*time.Minute)
	age(otherTmp, 48*time.Hour)

	xOld := filepath.Join(roots.Xray, "atomic-111")
	xNew := filepath.Join(roots.Xray, "atomic-222")
	xForeign := filepath.Join(roots.Xray, "old.json")
	mRoot := filepath.Join(roots.Mihomo, "atomic-333")
	mDir := filepath.Join(roots.Mihomo, "proxy_providers", "atomic-444")
	for _, f := range []string{xOld, xNew, xForeign, mRoot, mDir} {
		writeTestFile(t, f, "x")
	}
	age(xOld, 48*time.Hour)
	age(xNew, time.Hour)
	age(xForeign, 48*time.Hour)
	age(mRoot, 48*time.Hour)
	age(mDir, 48*time.Hour)

	if err := CleanupStale(dataDir, roots, now); err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}

	requireAbsent(t, oldTmp)
	requireAbsent(t, xOld)
	requireAbsent(t, mDir)
	for _, keep := range []string{newTmp, otherTmp, xNew, xForeign, mRoot} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("уборка тронула %s: %v", keep, err)
		}
	}
}
