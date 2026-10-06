package configlayer

import (
	"encoding/json"
	"testing"
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
