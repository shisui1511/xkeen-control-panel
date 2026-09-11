package utils

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockWriteCloser struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	closeCount int
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCount++
	return nil
}

func (m *mockWriteCloser) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.String()
}

func (m *mockWriteCloser) Lines() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := strings.TrimRight(m.buf.String(), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Test 1: первая запись сообщения попадает в нижележащий писатель без изменений и без задержки
func TestDeduplicatingWriter_BasicImmediate(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	defer w.Close()

	msg := "2026/09/12 12:00:00 Watchdog: check health ok\n"
	n, err := w.Write([]byte(msg))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("expected written len %d, got %d", len(msg), n)
	}

	if mock.String() != msg {
		t.Fatalf("expected immediately %q, got %q", msg, mock.String())
	}
}

// Test 2: три подряд одинаковых сообщения дают в выводе первую строку и, после смены сообщения, сводку с N = 2
func TestDeduplicatingWriter_ThreeRepeats(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	w.Write([]byte("msg1\n"))
	currTime = currTime.Add(10 * time.Second)
	w.Write([]byte("msg1\n"))
	currTime = currTime.Add(10 * time.Second)
	w.Write([]byte("msg1\n"))

	// Until a new message arrives or Close is called, only the first msg1 has been written
	linesBeforeNew := mock.Lines()
	if len(linesBeforeNew) != 1 || linesBeforeNew[0] != "msg1" {
		t.Fatalf("expected 1 line before new message, got %v", linesBeforeNew)
	}

	// Write msg2
	currTime = currTime.Add(10 * time.Second)
	w.Write([]byte("msg2\n"))

	linesAfterNew := mock.Lines()
	if len(linesAfterNew) != 3 {
		t.Fatalf("expected exactly 3 lines after new message, got %d: %v", len(linesAfterNew), linesAfterNew)
	}

	if linesAfterNew[0] != "msg1" {
		t.Errorf("line 0: expected 'msg1', got %q", linesAfterNew[0])
	}
	expectedSummary := "[dedup] previous message repeated 2 time(s) in 0m 20s"
	if linesAfterNew[1] != expectedSummary {
		t.Errorf("line 1: expected %q, got %q", expectedSummary, linesAfterNew[1])
	}
	if linesAfterNew[2] != "msg2" {
		t.Errorf("line 2: expected 'msg2', got %q", linesAfterNew[2])
	}
}

// Test 3: два подряд одинаковых сообщения дают сводку с N = 1; одиночное сообщение без повторов сводки не порождает вовсе
func TestDeduplicatingWriter_TwoRepeatsAndSingle(t *testing.T) {
	// Case A: two repeats
	mockA := &mockWriteCloser{}
	wA := NewDeduplicatingWriter(mockA)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	wA.now = func() time.Time { return currTime }

	wA.Write([]byte("dup\n"))
	currTime = currTime.Add(5 * time.Second)
	wA.Write([]byte("dup\n"))
	currTime = currTime.Add(2 * time.Second)
	wA.Close()

	linesA := mockA.Lines()
	if len(linesA) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(linesA), linesA)
	}
	if linesA[0] != "dup" {
		t.Errorf("line 0: expected 'dup', got %q", linesA[0])
	}
	if linesA[1] != "[dedup] previous message repeated 1 time(s) in 0m 2s" {
		t.Errorf("line 1: unexpected summary %q", linesA[1])
	}

	// Case B: single message
	mockB := &mockWriteCloser{}
	wB := NewDeduplicatingWriter(mockB)
	wB.now = func() time.Time { return currTime }

	wB.Write([]byte("single\n"))
	wB.Close()

	linesB := mockB.Lines()
	if len(linesB) != 1 || linesB[0] != "single" {
		t.Fatalf("expected exactly 1 line for single message, got %v", linesB)
	}
}

// Test 4: одинаковые по смыслу строки, различающиеся только префиксом даты и времени стандартного логгера, считаются одинаковыми
func TestDeduplicatingWriter_TimestampStripping(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	w.Write([]byte("2026/09/12 12:00:00 Watchdog: failure\n"))
	currTime = currTime.Add(30 * time.Second)
	w.Write([]byte("2026/09/12 12:00:30 Watchdog: failure\n"))
	currTime = currTime.Add(30 * time.Second)
	w.Write([]byte("2026/09/12 12:01:00 Watchdog: failure\n"))
	currTime = currTime.Add(30 * time.Second)
	w.Write([]byte("2026/09/12 12:01:30 Different event\n"))

	lines := mock.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	if lines[0] != "2026/09/12 12:00:00 Watchdog: failure" {
		t.Errorf("expected line 0 original timestamp, got %q", lines[0])
	}
	if lines[1] != "[dedup] previous message repeated 2 time(s) in 1m 0s" {
		t.Errorf("expected line 1 summary, got %q", lines[1])
	}
	if lines[2] != "2026/09/12 12:01:30 Different event" {
		t.Errorf("expected line 2 different event, got %q", lines[2])
	}
}

// Test 5: чередование двух сообщений A, B, A, B при истории в четыре уникальных сообщения схлопывается, а не проходит насквозь как четыре разные строки
func TestDeduplicatingWriter_Interleaving(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	w.Write([]byte("event A\n"))
	w.Write([]byte("event B\n"))
	w.Write([]byte("event A\n"))
	w.Write([]byte("event B\n"))

	// Crucial check: only the initial A and initial B should be written to output so far
	lines := mock.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines written so far (A, B collapsed), got %d: %v", len(lines), lines)
	}
	if lines[0] != "event A" || lines[1] != "event B" {
		t.Fatalf("expected ['event A', 'event B'], got %v", lines)
	}

	// On Close, summaries for A and B are flushed
	w.Close()
	closedLines := mock.Lines()
	if len(closedLines) != 4 {
		t.Fatalf("expected 4 lines after Close (2 initial + 2 summaries), got %d: %v", len(closedLines), closedLines)
	}
}

// Test 6: при непрерывной серии повторов, длящейся дольше периода принудительного сброса, сводка выводится по подставным часам, счётчик после этого продолжается с нуля, а подавление не прекращается
func TestDeduplicatingWriter_PeriodicFlush(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	// T=0: Initial message written
	w.Write([]byte("periodic\n"))

	// T=30s: First repetition
	currTime = currTime.Add(30 * time.Second)
	w.Write([]byte("periodic\n"))

	// Advance time by 10 minutes from first suppression (T=10m30s)
	currTime = currTime.Add(10 * time.Minute)
	w.Write([]byte("periodic\n"))

	// At this point, dedupPeriodicFlush (10m) has elapsed since firstSupp (T=30s)
	// A summary must have been flushed
	lines := mock.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (initial + periodic summary), got %d: %v", len(lines), lines)
	}
	if lines[1] != "[dedup] previous message repeated 2 time(s) in 10m 0s" {
		t.Errorf("unexpected periodic summary: %q", lines[1])
	}

	// Next repeat continues suppression from 0
	currTime = currTime.Add(1 * time.Minute) // T=11m30s
	w.Write([]byte("periodic\n"))

	// Nothing extra written yet
	if len(mock.Lines()) != 2 {
		t.Fatalf("expected still 2 lines, got %d", len(mock.Lines()))
	}

	// Advance 15s and close
	currTime = currTime.Add(15 * time.Second) // T=11m45s
	w.Close()
	finalLines := mock.Lines()
	if len(finalLines) != 3 {
		t.Fatalf("expected 3 lines after close, got %d: %v", len(finalLines), finalLines)
	}
	if finalLines[2] != "[dedup] previous message repeated 1 time(s) in 0m 15s" {
		t.Errorf("unexpected final summary: %q", finalLines[2])
	}
}

// Test 7: Close выводит отложенную сводку и закрывает нижележащий писатель; повторный Close не паникует
func TestDeduplicatingWriter_CloseCascade(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	currTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return currTime }

	w.Write([]byte("cascade\n"))
	currTime = currTime.Add(15 * time.Second)
	w.Write([]byte("cascade\n"))

	if mock.closeCount != 0 {
		t.Fatalf("expected 0 close calls before Close(), got %d", mock.closeCount)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("unexpected Close() error: %v", err)
	}

	if mock.closeCount != 1 {
		t.Fatalf("expected exactly 1 underlying Close call, got %d", mock.closeCount)
	}

	// Repeat Close should be safe and idempotent
	if err := w.Close(); err != nil {
		t.Fatalf("unexpected second Close() error: %v", err)
	}

	if mock.closeCount != 1 {
		t.Fatalf("expected underlying closeCount to remain 1, got %d", mock.closeCount)
	}
}

// Test 8: срез, переданный в Write, после возврата может быть изменён вызывающей стороной, и это не искажает текст будущей сводки
func TestDeduplicatingWriter_BufferMutation(t *testing.T) {
	mock := &mockWriteCloser{}
	w := NewDeduplicatingWriter(mock)
	defer w.Close()

	buf := []byte("original message\n")
	w.Write(buf)

	// Mutate buf
	copy(buf, []byte("CORRUPTED BYTES!\n"))

	// Second write of same original text
	buf2 := []byte("original message\n")
	w.Write(buf2)

	// Verify the first written line was not corrupted
	lines := mock.Lines()
	if len(lines) != 1 || lines[0] != "original message" {
		t.Fatalf("buffer mutation corrupted stored line: %v", lines)
	}
}
