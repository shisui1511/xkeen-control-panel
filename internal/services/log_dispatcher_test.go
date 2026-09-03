package services

import (
	"strings"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	rb.Add(LogEntry{ID: 1, Message: "msg1"})
	rb.Add(LogEntry{ID: 2, Message: "msg2"})

	entries := rb.GetAll()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != 1 || entries[1].ID != 2 {
		t.Errorf("unexpected order: %+v", entries)
	}

	// Add 3rd and 4th to cause wrap-around
	rb.Add(LogEntry{ID: 3, Message: "msg3"})
	rb.Add(LogEntry{ID: 4, Message: "msg4"})

	entries = rb.GetAll()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after wrap, got %d", len(entries))
	}
	if entries[0].ID != 2 || entries[1].ID != 3 || entries[2].ID != 4 {
		t.Errorf("unexpected wrapped order: %+v", entries)
	}

	rb.Clear()
	if len(rb.GetAll()) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(rb.GetAll()))
	}
}

func TestLogDispatcher_IngestAndRedact(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	raw := "[mihomo] 2026-08-24 12:00:00 [DNS] query google.com token=mysecret123 from 192.168.1.150"
	entry := d.IngestLine(raw, "mihomo")

	if entry.Source != "mihomo" {
		t.Errorf("expected source mihomo, got %s", entry.Source)
	}
	if entry.Subsystem != "DNS" {
		t.Errorf("expected subsystem DNS, got %s", entry.Subsystem)
	}
	if strings.Contains(entry.Message, "mysecret123") {
		t.Errorf("token leaked in message: %s", entry.Message)
	}
	if strings.Contains(entry.Message, "192.168.1.150") {
		t.Errorf("LAN IP leaked in message: %s", entry.Message)
	}
	if !strings.Contains(entry.Message, "*REDACTED*") {
		t.Errorf("missing redaction marker in message: %s", entry.Message)
	}

	history := d.GetHistory("mihomo", "", 10)
	if len(history) != 1 {
		t.Fatalf("expected 1 entry in history, got %d", len(history))
	}
}

func TestLogDispatcher_PriorityQueue(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	d.IngestLine("[xray] [INFO] Connection opened", "xray")
	d.IngestLine("[xray] [ERROR] Core crashed with panic: invalid memory address", "xray")
	d.IngestLine("[mihomo] [WARNING] Timeout connecting to proxy", "mihomo")

	priorityEntries := d.GetHistory("priority", "", 10)
	if len(priorityEntries) != 1 {
		t.Fatalf("expected 1 error entry in priority buffer, got %d", len(priorityEntries))
	}
	if priorityEntries[0].Level != "error" {
		t.Errorf("expected level error, got %s", priorityEntries[0].Level)
	}
}

func TestLogDispatcher_BatchBroadcast(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	d.Start()
	defer d.Stop()

	ch := d.Subscribe()
	defer d.Unsubscribe(ch)

	d.IngestLine("[xcp] Log message 1", "xcp")
	d.IngestLine("[xcp] Log message 2", "xcp")

	select {
	case batch := <-ch:
		if len(batch) != 2 {
			t.Errorf("expected batch of 2 entries, got %d", len(batch))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for batch broadcast")
	}
}

func TestLogDispatcher_NonPrefixBracketsNoInfiniteLoop(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	// Line where bracket tag is in the middle of a URL/path, not at the beginning
	raw := "2026/09/02 19:23:30 GET /proxies/Node [VLESS]/delay 404 4.521875ms"
	done := make(chan struct{})
	go func() {
		entry := d.IngestLine(raw, "xcp")
		if !strings.Contains(entry.Message, "[VLESS]") {
			t.Errorf("expected message to preserve content, got %s", entry.Message)
		}
		close(done)
	}()

	select {
	case <-done:
		// Success: parsed promptly without hanging
	case <-time.After(1 * time.Second):
		t.Fatal("parseRedactedLine hung in infinite loop on non-prefix bracket")
	}
}
