package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitForHistory(t *testing.T, d *LogDispatcher, cond func([]LogEntry) bool) []LogEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h := d.GetHistory("xcp", "", 1000)
		if cond(h) {
			return h
		}
		time.Sleep(50 * time.Millisecond)
	}
	return d.GetHistory("xcp", "", 1000)
}

func TestTailFile_DropsPartialFirstLineAndFollowsRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xcp.log")

	// Three long lines: the initial 8 KiB tail seek lands inside the first.
	var sb strings.Builder
	for i := 0; i < 3; i++ {
		sb.WriteString("2026/09/23 14:47:14 GET /assets/app.js?" + strings.Repeat("x", 4000) + " 200 1.70006ms\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewLogDispatcher(nil, dir, "")
	d.wg.Add(1)
	go d.tailFile(path)
	defer d.Stop()

	h := waitForHistory(t, d, func(h []LogEntry) bool { return len(h) >= 2 })
	if len(h) != 2 {
		t.Fatalf("expected 2 complete tail lines, got %d", len(h))
	}
	for _, e := range h {
		if !strings.HasPrefix(e.Message, "GET /assets/app.js") {
			t.Fatalf("partial line ingested: %q", e.Message)
		}
	}
	before := len(h)

	// Rename-based rotation: the new file must be read from its start
	// without re-ingesting the old tail.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("2026/09/23 14:50:00 rotated line one\n2026/09/23 14:50:01 rotated line two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h = waitForHistory(t, d, func(h []LogEntry) bool {
		return len(h) >= before+2 && strings.Contains(h[len(h)-1].Message, "rotated line two")
	})
	if len(h) != before+2 {
		t.Fatalf("expected exactly 2 new entries after rotation, got %d", len(h)-before)
	}
	if !strings.Contains(h[len(h)-2].Message, "rotated line one") {
		t.Fatalf("unexpected entry order: %q", h[len(h)-2].Message)
	}
}

func TestGetHistory_AllSourcesSortedByID(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	for i := 0; i < 20; i++ {
		src := "xcp"
		if i%2 == 0 {
			src = "xkeen"
		}
		d.IngestLine("2026/09/23 14:47:14 message", src)
	}
	h := d.GetHistory("all", "", 1000)
	for i := 1; i < len(h); i++ {
		if h[i-1].ID > h[i].ID {
			t.Fatalf("history not ordered by ID at %d: %d > %d", i, h[i-1].ID, h[i].ID)
		}
	}
}
