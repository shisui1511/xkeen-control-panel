package utils

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	dedupSummaryFormat = "[dedup] previous message repeated %d time(s) in %s\n"
	dedupPeriodicFlush = 10 * time.Minute
	dedupHistorySize   = 4
)

// dedupEntry tracks repeat counts and suppression timing for a single normalized message.
type dedupEntry struct {
	normalized string
	repeats    int
	firstSupp  time.Time
}

// DeduplicatingWriter wraps an io.WriteCloser and collapses consecutive or interleaved
// duplicate log messages, emitting a summary line when repetitions finish or periodic
// flush interval elapses.
type DeduplicatingWriter struct {
	mu         sync.Mutex
	underlying io.WriteCloser
	pending    []byte
	now        func() time.Time
	history    []*dedupEntry
	closed     bool
}

// NewDeduplicatingWriter creates a new DeduplicatingWriter wrapping w.
func NewDeduplicatingWriter(w io.WriteCloser) *DeduplicatingWriter {
	return &DeduplicatingWriter{
		underlying: w,
		now:        time.Now,
		history:    make([]*dedupEntry, 0, dedupHistorySize),
	}
}

// Write writes data to the deduplicating writer, buffering incomplete lines and
// suppressing repeats for messages present in recent history.
func (w *DeduplicatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("write to closed DeduplicatingWriter")
	}

	w.pending = append(w.pending, p...)

	for {
		idx := bytes.IndexByte(w.pending, '\n')
		if idx == -1 {
			break
		}

		line := string(w.pending[:idx+1])
		w.pending = append([]byte(nil), w.pending[idx+1:]...)

		if err := w.processLineLocked(line); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

// processLineLocked handles a single complete newline-terminated line.
func (w *DeduplicatingWriter) processLineLocked(rawLine string) error {
	norm := stripLogTimestamp(rawLine)
	now := w.now()

	// Check if this matches an entry in recent history
	var matched *dedupEntry
	for _, entry := range w.history {
		if entry.normalized == norm {
			matched = entry
			break
		}
	}

	if matched != nil {
		// Move matched to the end of history (most recently seen)
		for i, e := range w.history {
			if e == matched {
				w.history = append(append(w.history[:i], w.history[i+1:]...), matched)
				break
			}
		}

		matched.repeats++
		if matched.firstSupp.IsZero() {
			matched.firstSupp = now
		}

		// Periodic flush if suppressed continuously for dedupPeriodicFlush
		if now.Sub(matched.firstSupp) >= dedupPeriodicFlush {
			if err := w.flushEntryLocked(matched); err != nil {
				return err
			}
		}
		return nil
	}

	// New unique message: flush any pending summaries from previously suppressed messages
	for _, entry := range w.history {
		if entry.repeats > 0 {
			if err := w.flushEntryLocked(entry); err != nil {
				return err
			}
		}
	}

	// Maintain ring buffer capacity
	if len(w.history) >= dedupHistorySize {
		w.history = w.history[1:]
	}

	w.history = append(w.history, &dedupEntry{
		normalized: norm,
	})

	// First occurrence is always written immediately without delay
	_, err := w.underlying.Write([]byte(rawLine))
	return err
}

// flushEntryLocked writes the summary line for a suppressed entry and resets its repeat count.
func (w *DeduplicatingWriter) flushEntryLocked(entry *dedupEntry) error {
	if entry.repeats <= 0 {
		return nil
	}
	span := w.now().Sub(entry.firstSupp)
	summary := fmt.Sprintf(dedupSummaryFormat, entry.repeats, formatDedupSpan(span))
	entry.repeats = 0
	entry.firstSupp = time.Time{}
	_, err := w.underlying.Write([]byte(summary))
	return err
}

// Flush flushes any pending incomplete lines and all repeat summaries without closing the writer.
func (w *DeduplicatingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	// If there is residual pending data, process it as a line
	if len(w.pending) > 0 {
		line := string(w.pending)
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		w.pending = nil
		_ = w.processLineLocked(line)
	}

	var firstErr error
	for _, entry := range w.history {
		if entry.repeats > 0 {
			if err := w.flushEntryLocked(entry); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Close flushes any pending summaries and incomplete lines, then closes the underlying writer.
// Multiple calls to Close are safe and return nil.
func (w *DeduplicatingWriter) Close() error {
	firstErr := w.Flush()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	if w.underlying != nil {
		if err := w.underlying.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// formatDedupSpan truncates duration to whole seconds and formats it as "Mm Ss".
func formatDedupSpan(d time.Duration) string {
	sec := int64(d / time.Second)
	if sec < 0 {
		sec = 0
	}
	min := sec / 60
	remSec := sec % 60
	return fmt.Sprintf("%dm %ds", min, remSec)
}

// stripLogTimestamp removes standard Go log prefix ("YYYY/MM/DD hh:mm:ss ") if present.
func stripLogTimestamp(line string) string {
	if len(line) >= 20 &&
		isDigit(line[0]) && isDigit(line[1]) && isDigit(line[2]) && isDigit(line[3]) &&
		line[4] == '/' &&
		isDigit(line[5]) && isDigit(line[6]) &&
		line[7] == '/' &&
		isDigit(line[8]) && isDigit(line[9]) &&
		line[10] == ' ' &&
		isDigit(line[11]) && isDigit(line[12]) &&
		line[13] == ':' &&
		isDigit(line[14]) && isDigit(line[15]) &&
		line[16] == ':' &&
		isDigit(line[17]) && isDigit(line[18]) &&
		line[19] == ' ' {
		return strings.TrimRight(line[20:], "\r\n")
	}
	return strings.TrimRight(line, "\r\n")
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
