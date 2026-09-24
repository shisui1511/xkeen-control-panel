package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestPollRCILog_IngestsNewEntriesOnce(t *testing.T) {
	var round int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&round, 1)
		entries := `"1": {"id": 1, "timestamp": "Sep 24 09:07:48", "ident": "dropbear[25629]", "message": {"level": "Info", "message": "Child connection"}}`
		if n > 1 {
			entries += `, "2": {"id": 2, "timestamp": "Sep 24 09:07:50", "ident": "ndm", "message": {"level": "Error", "message": "Core::Syslog: link down"}}`
		}
		fmt.Fprintf(w, `{"show": {"log": {"log": {%s}}}}`, entries)
	}))
	defer srv.Close()

	d := NewLogDispatcher(nil, t.TempDir(), "")
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.pollRCILog(srv.URL, 50*time.Millisecond)
	}()

	deadline := time.Now().Add(3 * time.Second)
	var h []LogEntry
	for time.Now().Before(deadline) {
		h = d.GetHistory("syslog", "", 100)
		if len(h) >= 2 && atomic.LoadInt32(&round) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.Stop()

	if len(h) != 2 {
		t.Fatalf("expected each entry once, got %d: %+v", len(h), h)
	}
	if h[0].Timestamp != "09:07:48" || h[0].Subsystem != "dropbear" || !strings.Contains(h[0].Message, "Child connection") {
		t.Errorf("first entry: %+v", h[0])
	}
	if h[1].Level != "error" || h[1].Subsystem != "ndm" {
		t.Errorf("second entry: %+v", h[1])
	}
}

func TestReadFileTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	content := "aaaa first\nbbbb second\ncccc third\npartial"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, offset, err := readFileTail(path, int64(len(content)-3))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "bbbb second|cccc third" {
		t.Errorf("lines = %q", lines)
	}
	if want := int64(strings.Index(content, "partial")); offset != want {
		t.Errorf("offset = %d, want %d", offset, want)
	}
}

func TestIngestBackfillOrdersByTime(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()
	now := time.Date(2026, 9, 24, 13, 55, 0, 0, time.Local)
	d.ingestBackfill([]backfillLine{
		{raw: "2026/09/24 13:50:53 GET /a", source: "xcp"},
		{raw: "2026/09/24 13:52:10 GET /b", source: "xcp"},
		{raw: "[syslog] 23:59:58 [info] yesterday", source: "syslog"},
		{raw: "[syslog] 13:51:00 [info] between", source: "syslog"},
	}, now)
	h := d.GetHistory("", "", 100)
	var got []string
	for _, e := range h {
		got = append(got, e.Timestamp)
	}
	if strings.Join(got, " ") != "23:59:58 13:50:53 13:51:00 13:52:10" {
		t.Errorf("order = %v", got)
	}
}
