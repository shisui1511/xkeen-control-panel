package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStore_DraftPersists(t *testing.T) {
	dir := t.TempDir()

	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ev, err := s.EditDraft(0, "diag", json.RawMessage(`{"enabled":true}`))
	if err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	if ev.DraftRevision != 1 {
		t.Fatalf("revision in event = %d, want 1", ev.DraftRevision)
	}
	if got := s.DraftRevision(); got != 1 {
		t.Fatalf("DraftRevision = %d, want 1", got)
	}

	// Повторное открытие на том же каталоге (перезапуск панели).
	s2, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("reopen OpenStore: %v", err)
	}
	snap := s2.Snapshot()
	if snap.DraftRevision != 1 {
		t.Fatalf("revision after reopen = %d, want 1", snap.DraftRevision)
	}
	if !SectionEqual(snap.Draft["diag"], json.RawMessage(`{"enabled":true}`)) {
		t.Fatalf("draft[diag] after reopen = %s", snap.Draft["diag"])
	}
}

// writeRawState кладёт в каталог данных файл состояния с заданным текстом.
func writeRawState(t *testing.T, dir, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, StateFileName), []byte(raw), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}

func readStateFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, StateFileName))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	return data
}

func TestStore_DraftConflict(t *testing.T) {
	dir := t.TempDir()
	broker := NewBroker()
	defer broker.Close()
	ch, cancel, err := broker.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	s, err := OpenStore(dir, broker)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := s.EditDraft(0, "diag", json.RawMessage(`{"enabled":true}`)); err != nil {
		t.Fatalf("first EditDraft: %v", err)
	}
	recvEvent(t, ch) // первое событие draft

	before := s.Snapshot()
	fileBefore := readStateFile(t, dir)

	_, err = s.EditDraft(0, "diag", json.RawMessage(`{"enabled":false}`))
	if !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("second EditDraft(0) err = %v, want ErrDraftConflict", err)
	}
	if after := s.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed after conflict:\nbefore=%+v\nafter=%+v", before, after)
	}
	if fileAfter := readStateFile(t, dir); !bytes.Equal(fileBefore, fileAfter) {
		t.Fatalf("state file changed after conflict")
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event after conflict: %+v", ev)
	default:
	}
}

func TestStore_InvalidSection(t *testing.T) {
	s, err := OpenStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	bad := []string{"Diag", "a b", "", "1abc", "a-b", strings.Repeat("a", 33)}
	for _, name := range bad {
		if _, err := s.EditDraft(0, name, json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidSection) {
			t.Errorf("EditDraft(%q) err = %v, want ErrInvalidSection", name, err)
		}
	}
	if got := s.DraftRevision(); got != 0 {
		t.Errorf("revision after invalid edits = %d, want 0", got)
	}
	// Граница: ровно 32 символа допустимы.
	if _, err := s.EditDraft(0, strings.Repeat("a", 32), json.RawMessage(`{}`)); err != nil {
		t.Errorf("EditDraft with 32-char name: %v", err)
	}
}

func TestStore_DraftChanges(t *testing.T) {
	t.Run("equal after compact, extra section in draft", func(t *testing.T) {
		dir := t.TempDir()
		writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 0,
  "draft":   {"a": { "x" : 1 }, "b": {}},
  "applied": {"a": {"x":1}},
  "manifest": {}, "notices": {}
}`)
		s, err := OpenStore(dir, nil)
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		if got := s.DraftChanges(); got != 1 {
			t.Fatalf("DraftChanges = %d, want 1", got)
		}
	})
	t.Run("section removed from draft counts", func(t *testing.T) {
		dir := t.TempDir()
		writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 0,
  "draft":   {"a": {"x":1}},
  "applied": {"a": {"x":1}, "c": {"y":2}},
  "manifest": {}, "notices": {}
}`)
		s, err := OpenStore(dir, nil)
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		if got := s.DraftChanges(); got != 1 {
			t.Fatalf("DraftChanges = %d, want 1", got)
		}
	})
	t.Run("different value counts", func(t *testing.T) {
		dir := t.TempDir()
		writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 0,
  "draft":   {"a": {"x":2}},
  "applied": {"a": {"x":1}},
  "manifest": {}, "notices": {}
}`)
		s, err := OpenStore(dir, nil)
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		if got := s.DraftChanges(); got != 1 {
			t.Fatalf("DraftChanges = %d, want 1", got)
		}
	})
}

func TestStore_ResetDraft(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 0,
  "draft": {}, "applied": {"a": {"x":1}},
  "manifest": {}, "notices": {}
}`)
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := s.EditDraft(0, "b", json.RawMessage(`{"y":2}`)); err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	if s.DraftChanges() == 0 {
		t.Fatalf("DraftChanges = 0 after edit, want > 0")
	}

	ev, err := s.ResetDraft(1)
	if err != nil {
		t.Fatalf("ResetDraft(1): %v", err)
	}
	if ev.DraftRevision != 2 || ev.DraftChanges != 0 {
		t.Fatalf("event = %+v, want revision 2, changes 0", ev)
	}
	snap := s.Snapshot()
	if snap.DraftRevision != 2 {
		t.Fatalf("revision = %d, want 2", snap.DraftRevision)
	}
	if len(snap.Draft) != 1 || !SectionEqual(snap.Draft["a"], json.RawMessage(`{"x":1}`)) {
		t.Fatalf("draft after reset = %v, want applied copy", snap.Draft)
	}
	if got := s.DraftChanges(); got != 0 {
		t.Fatalf("DraftChanges after reset = %d, want 0", got)
	}

	if _, err := s.ResetDraft(1); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("ResetDraft(stale) err = %v, want ErrDraftConflict", err)
	}
}

func TestStore_UpdatePersistFailureKeepsMemory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root обходит права на каталог")
	}
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	before := s.Snapshot()

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err = s.Update(func(st *State) error {
		st.Notices.SchemaReset = true
		return nil
	})
	if err == nil {
		t.Fatal("Update on read-only dir returned nil error")
	}
	if after := s.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("memory changed after failed persist:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestStore_UpdateFnErrorKeepsMemory(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	before := s.Snapshot()
	fileBefore := readStateFile(t, dir)

	boom := errors.New("boom")
	err = s.Update(func(st *State) error {
		st.Manifest["xray:a.json"] = ManifestEntry{Kernel: KernelXray, RelPath: "a.json"}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Update err = %v, want boom", err)
	}
	if after := s.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("memory changed after fn error")
	}
	if !bytes.Equal(fileBefore, readStateFile(t, dir)) {
		t.Fatalf("file changed after fn error")
	}
}

func TestStore_UpdatePersists(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	key := ManifestKey(KernelXray, `conf\04_outbounds.json`)
	err = s.Update(func(st *State) error {
		st.Manifest[key] = ManifestEntry{Kernel: KernelXray, RelPath: "04_outbounds.json", Kind: KindXrayJSON, Hash: "h", Status: StatusManaged}
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	s2, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if e, ok := s2.Snapshot().Manifest[key]; !ok || e.Hash != "h" {
		t.Fatalf("manifest entry after reopen = %+v, ok=%v", e, ok)
	}
}

func TestStore_DismissNotice(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 0,
  "draft": {}, "applied": {}, "manifest": {},
  "notices": {"schema_reset": true, "schema_reset_backup": "/x/state.1.json", "recovered_from_journal": true}
}`)
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	if err := s.DismissNotice("schema_reset"); err != nil {
		t.Fatalf("DismissNotice(schema_reset): %v", err)
	}
	n := s.Snapshot().Notices
	if n.SchemaReset || n.SchemaResetBackup != "" || !n.RecoveredFromJournal {
		t.Fatalf("notices after dismissing schema_reset = %+v", n)
	}
	s2, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := s2.Snapshot().Notices; got.SchemaReset || got.SchemaResetBackup != "" {
		t.Fatalf("dismiss not persisted: %+v", got)
	}

	if err := s.DismissNotice("recovered_from_journal"); err != nil {
		t.Fatalf("DismissNotice(recovered_from_journal): %v", err)
	}
	if s.Snapshot().Notices.RecoveredFromJournal {
		t.Fatal("recovered_from_journal still set")
	}

	if err := s.DismissNotice("nope"); !errors.Is(err, ErrUnknownNotice) {
		t.Fatalf("DismissNotice(nope) err = %v, want ErrUnknownNotice", err)
	}
}

func TestStore_Reload(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	writeRawState(t, dir, `{
  "schema_version": 1, "draft_revision": 5,
  "draft": {"z": {"k":1}}, "applied": {}, "manifest": {}, "notices": {}
}`)
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	snap := s.Snapshot()
	if snap.DraftRevision != 5 || !SectionEqual(snap.Draft["z"], json.RawMessage(`{"k":1}`)) {
		t.Fatalf("snapshot after Reload = %+v", snap)
	}
}

func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		fi, err := os.Stat(filepath.Join(dir, StateFileName))
		if err != nil {
			t.Fatalf("%s: stat: %v", stage, err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s: state file perm = %o, want 600", stage, perm)
		}
	}
	check("after open")
	if _, err := s.EditDraft(0, "diag", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	check("after edit")
}

func backupDir(dataDir string) string {
	return filepath.Join(dataDir, "backup", "config-layer", "state")
}

func listBackups(t *testing.T, dataDir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(backupDir(dataDir), "state.*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return files
}

// assertCleanStart проверяет чистый старт после сброса: копия с исходными
// байтами, пустое состояние, флаг уведомления и новый файл схемы 1.
func assertCleanStart(t *testing.T, dir string, s *Store, original []byte) {
	t.Helper()
	backups := listBackups(t, dir)
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want exactly 1", backups)
	}
	copied, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(copied, original) {
		t.Fatalf("backup bytes differ from the original file")
	}
	base := filepath.Base(backups[0])
	if !strings.HasPrefix(base, "state.") || !strings.HasSuffix(base, ".json") {
		t.Fatalf("backup name = %q, want state.<unix>.json", base)
	}

	snap := s.Snapshot()
	if len(snap.Draft) != 0 || len(snap.Applied) != 0 || len(snap.Manifest) != 0 || snap.DraftRevision != 0 {
		t.Fatalf("state not empty after reset: %+v", snap)
	}
	if snap.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d, want %d", snap.SchemaVersion, SchemaVersion)
	}
	if !snap.Notices.SchemaReset || snap.Notices.SchemaResetBackup != backups[0] {
		t.Fatalf("notices = %+v, want schema_reset with backup %q", snap.Notices, backups[0])
	}

	var onDisk State
	if err := json.Unmarshal(readStateFile(t, dir), &onDisk); err != nil {
		t.Fatalf("new state file is not valid JSON: %v", err)
	}
	if onDisk.SchemaVersion != SchemaVersion || !onDisk.Notices.SchemaReset {
		t.Fatalf("new state file = %+v", onDisk)
	}
}

const foreignSchemaState = `{
  "schema_version": 999,
  "draft_revision": 7,
  "draft": {"a": {"x": 1}},
  "applied": {"a": {"x": 0}},
  "manifest": {"xray:a.json": {"kernel": "xray", "rel_path": "a.json"}},
  "notices": {}
}`

func TestStore_SchemaMismatchResets(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, foreignSchemaState)

	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	assertCleanStart(t, dir, s, []byte(foreignSchemaState))
}

func TestStore_CorruptFileResets(t *testing.T) {
	dir := t.TempDir()
	const corrupt = "{ not json"
	writeRawState(t, dir, corrupt)

	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	assertCleanStart(t, dir, s, []byte(corrupt))
}

func TestStore_BackupDirPermissions(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, foreignSchemaState)
	if _, err := OpenStore(dir, nil); err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	di, err := os.Stat(backupDir(dir))
	if err != nil {
		t.Fatalf("stat backup dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("backup dir perm = %o, want 700", perm)
	}
	backups := listBackups(t, dir)
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want 1", backups)
	}
	fi, err := os.Stat(backups[0])
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("backup perm = %o, want 600", perm)
	}
}

func TestStore_ReloadSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	writeRawState(t, dir, foreignSchemaState)
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	assertCleanStart(t, dir, s, []byte(foreignSchemaState))
}

func TestStore_BackupWriteFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	writeRawState(t, dir, foreignSchemaState)
	// Каталог копий создать нельзя: на месте backup лежит обычный файл.
	if err := os.WriteFile(filepath.Join(dir, "backup"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	if _, err := OpenStore(dir, nil); err == nil {
		t.Fatal("OpenStore succeeded, want error when backup cannot be written")
	}
	if got := readStateFile(t, dir); !bytes.Equal(got, []byte(foreignSchemaState)) {
		t.Fatalf("original state file was modified: %s", got)
	}
}

func TestStore_BackupNameCollision(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	s.now = func() time.Time { return time.Unix(1700000000, 0) }

	first := `{ broken one`
	second := `{ broken two`
	for _, raw := range []string{first, second} {
		writeRawState(t, dir, raw)
		if err := s.Reload(); err != nil {
			t.Fatalf("Reload: %v", err)
		}
	}

	backups := listBackups(t, dir)
	if len(backups) != 2 {
		t.Fatalf("backups = %v, want 2 (second reset in the same second must not overwrite the first)", backups)
	}
	seen := map[string]bool{}
	for _, b := range backups {
		data, err := os.ReadFile(b)
		if err != nil {
			t.Fatalf("read %s: %v", b, err)
		}
		seen[string(data)] = true
	}
	if !seen[first] || !seen[second] {
		t.Fatalf("backup contents = %v, want both originals", seen)
	}
}
