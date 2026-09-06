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

func TestRateLimit_AtThresholdPasses(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return fixedTime }

	for i := 0; i < LogRateLimitMaxPerWindow; i++ {
		d.IngestLine("[mihomo] test message", "mihomo")
	}

	history := d.GetHistory("mihomo", "", 200)
	if len(history) != LogRateLimitMaxPerWindow {
		t.Fatalf("expected exactly %d entries, got %d", LogRateLimitMaxPerWindow, len(history))
	}
	if d.SuppressedTotal() != 0 {
		t.Errorf("expected 0 suppressed, got %d", d.SuppressedTotal())
	}
}

func TestRateLimit_OverThresholdSuppressed(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return fixedTime }

	for i := 0; i < LogRateLimitMaxPerWindow+1; i++ {
		d.IngestLine("[mihomo] test message", "mihomo")
	}

	history := d.GetHistory("mihomo", "", 200)
	if len(history) != LogRateLimitMaxPerWindow {
		t.Fatalf("expected exactly %d entries in buffer, got %d", LogRateLimitMaxPerWindow, len(history))
	}
	if d.SuppressedTotal() != 1 {
		t.Fatalf("expected 1 suppressed message, got %d", d.SuppressedTotal())
	}
}

func TestRateLimit_SuppressedNotInBuffers(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return fixedTime }

	// 250 messages including error messages
	for i := 0; i < 250; i++ {
		d.IngestLine("[mihomo] [ERROR] critical error", "mihomo")
	}

	history := d.GetHistory("mihomo", "", 300)
	if len(history) != LogRateLimitMaxPerWindow {
		t.Fatalf("expected exactly %d entries in source buffer, got %d", LogRateLimitMaxPerWindow, len(history))
	}

	priority := d.GetHistory("priority", "", 300)
	if len(priority) != LogRateLimitMaxPerWindow {
		t.Fatalf("expected exactly %d entries in priority buffer, got %d", LogRateLimitMaxPerWindow, len(priority))
	}

	if d.SuppressedTotal() != 150 {
		t.Fatalf("expected 150 suppressed messages, got %d", d.SuppressedTotal())
	}
}

func TestRateLimit_AggregateEmittedOnWindowRoll(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	currTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return currTime }

	// Send 150 logs in window 1 (50 suppressed)
	for i := 0; i < 150; i++ {
		d.IngestLine("[mihomo] storm message", "mihomo")
	}

	// Advance time past window duration (e.g. +2 seconds)
	currTime = currTime.Add(2 * time.Second)

	// Ingest 1 line in window 2 — this should roll the window and emit the aggregate
	d.IngestLine("[mihomo] next window message", "mihomo")

	// Check batchQueue for aggregate
	d.batchMu.Lock()
	queue := make([]LogEntry, len(d.batchQueue))
	copy(queue, d.batchQueue)
	d.batchMu.Unlock()

	foundAggregate := false
	for _, entry := range queue {
		if strings.Contains(entry.Message, "suppressed (rate limit)") {
			foundAggregate = true
			if !strings.Contains(entry.Message, "50 messages suppressed") {
				t.Errorf("expected 50 suppressed in message, got %s", entry.Message)
			}
			if entry.Level != "warning" {
				t.Errorf("expected aggregate level warning, got %s", entry.Level)
			}
			if entry.Subsystem != "system" {
				t.Errorf("expected aggregate subsystem system, got %s", entry.Subsystem)
			}
		}
	}

	if !foundAggregate {
		t.Fatal("expected aggregate entry in batchQueue upon window roll")
	}

	// Verify aggregate is NOT in source ring buffer
	history := d.GetHistory("mihomo", "", 200)
	for _, entry := range history {
		if strings.Contains(entry.Message, "suppressed (rate limit)") {
			t.Errorf("aggregate entry leaked into ring buffer: %s", entry.Message)
		}
	}
}

func TestRateLimit_NoAggregateWithoutSuppression(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	currTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return currTime }

	// Send 10 logs (no suppression)
	for i := 0; i < 10; i++ {
		d.IngestLine("[mihomo] normal message", "mihomo")
	}

	// Advance time
	currTime = currTime.Add(2 * time.Second)

	// Ingest 1 line in next window
	d.IngestLine("[mihomo] new window message", "mihomo")

	d.batchMu.Lock()
	queue := make([]LogEntry, len(d.batchQueue))
	copy(queue, d.batchQueue)
	d.batchMu.Unlock()

	for _, entry := range queue {
		if strings.Contains(entry.Message, "suppressed") {
			t.Errorf("unexpected aggregate entry when no suppression happened: %s", entry.Message)
		}
	}
}

func TestRateLimit_PerSourceIsolation(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return fixedTime }

	// 200 messages to mihomo (100 suppressed)
	for i := 0; i < 200; i++ {
		d.IngestLine("[mihomo] mihomo storm", "mihomo")
	}

	// 5 messages to xcp (none should be suppressed)
	for i := 0; i < 5; i++ {
		d.IngestLine("[xcp] xcp message", "xcp")
	}

	xcpHistory := d.GetHistory("xcp", "", 10)
	if len(xcpHistory) != 5 {
		t.Fatalf("expected all 5 xcp messages to pass, got %d", len(xcpHistory))
	}

	mihomoHistory := d.GetHistory("mihomo", "", 250)
	if len(mihomoHistory) != LogRateLimitMaxPerWindow {
		t.Fatalf("expected %d mihomo entries, got %d", LogRateLimitMaxPerWindow, len(mihomoHistory))
	}

	if d.SuppressedTotal() != 100 {
		t.Errorf("expected 100 total suppressed, got %d", d.SuppressedTotal())
	}
}

func TestRateLimit_ErrorLevelAlsoSuppressed(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	fixedTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return fixedTime }

	// First send 100 info messages
	for i := 0; i < 100; i++ {
		d.IngestLine("[mihomo] [INFO] message", "mihomo")
	}

	// 101st is error — should be suppressed and not reach priority buffer
	d.IngestLine("[mihomo] [ERROR] late error message", "mihomo")

	priority := d.GetHistory("priority", "", 10)
	if len(priority) != 0 {
		t.Fatalf("expected priority buffer to be empty for suppressed error, got %d", len(priority))
	}
}

func TestBatchFlusherCadenceUnchanged(t *testing.T) {
	if LogBatchFlushInterval < 100*time.Millisecond || LogBatchFlushInterval > 150*time.Millisecond {
		t.Fatalf("LogBatchFlushInterval must be within 100-150ms range, got %v", LogBatchFlushInterval)
	}
	if LogBatchFlushInterval != 120*time.Millisecond {
		t.Fatalf("LogBatchFlushInterval expected 120ms, got %v", LogBatchFlushInterval)
	}
}

func TestSuppressedTotalAccumulates(t *testing.T) {
	d := NewLogDispatcher(nil, t.TempDir(), "")
	defer d.Stop()

	currTime := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d.nowFunc = func() time.Time { return currTime }

	// Window 1: 120 messages -> 20 suppressed
	for i := 0; i < 120; i++ {
		d.IngestLine("[mihomo] msg", "mihomo")
	}
	if d.SuppressedTotal() != 20 {
		t.Fatalf("expected 20 suppressed, got %d", d.SuppressedTotal())
	}

	// Advance time to Window 2: 130 messages -> 30 suppressed
	currTime = currTime.Add(2 * time.Second)
	for i := 0; i < 130; i++ {
		d.IngestLine("[mihomo] msg", "mihomo")
	}

	// Total should be 20 + 30 = 50, not reset
	if d.SuppressedTotal() != 50 {
		t.Fatalf("expected cumulative 50 suppressed, got %d", d.SuppressedTotal())
	}
}
