package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// LogEntry represents a structured, redacted log entry.
type LogEntry struct {
	ID        int64             `json:"id"`
	Timestamp string            `json:"timestamp"`
	Source    string            `json:"source"`    // "mihomo", "xray", "xkeen", "syslog", "xcp"
	Level     string            `json:"level"`     // "debug", "info", "warning", "error", "fatal"
	Subsystem string            `json:"subsystem"` // e.g. "DNS", "TUN", "TCP", "Fake-IP", etc.
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// LogRateLimit constants define throttling parameters for LogDispatcher (LOGHUB-07).
const (
	// LogRateLimitMaxPerWindow is the maximum number of logs allowed per source per window.
	LogRateLimitMaxPerWindow = 100
	// LogRateLimitWindowDuration is the duration of a rate limit window (1 second).
	LogRateLimitWindowDuration = 1 * time.Second
	// LogBatchFlushInterval defines the broadcast cadence for subscribers (120ms, within 100-150ms).
	LogBatchFlushInterval = 120 * time.Millisecond
)

// FlashHealthInfo represents the status of flash memory and log files.
type FlashHealthInfo struct {
	TotalLogsBytes   int64  `json:"total_logs_bytes"`
	FreeSpaceBytes   uint64 `json:"free_space_bytes"`
	TotalSpaceBytes  uint64 `json:"total_space_bytes"`
	IsUnderPressure  bool   `json:"is_under_pressure"`
	EmergencyActions int    `json:"emergency_actions"`
	SuppressedLogs   int64  `json:"suppressed_logs"`
}

type rateLimitState struct {
	mu          sync.Mutex
	windowStart time.Time
	passCount   int
	suppressed  int
}

// RingBuffer stores a fixed-capacity circular buffer of LogEntry items in RAM.
type RingBuffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []LogEntry
	head     int
	count    int
}

// NewRingBuffer initializes a new circular buffer with fixed capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		capacity: capacity,
		entries:  make([]LogEntry, capacity),
	}
}

// Add appends a new entry to the circular buffer.
func (rb *RingBuffer) Add(e LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries[rb.head] = e
	rb.head = (rb.head + 1) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	}
}

// GetAll returns entries in chronological order.
func (rb *RingBuffer) GetAll() []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	res := make([]LogEntry, rb.count)
	if rb.count < rb.capacity {
		copy(res, rb.entries[:rb.count])
		return res
	}

	// Buffer has wrapped around
	firstPart := rb.entries[rb.head:]
	secondPart := rb.entries[:rb.head]
	copy(res, firstPart)
	copy(res[len(firstPart):], secondPart)
	return res
}

// Clear resets the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.count = 0
}

// LogDispatcher manages log collection, ring buffers, streaming and flash guard.
type LogDispatcher struct {
	mu          sync.RWMutex
	buffers     map[string]*RingBuffer
	priorityBuf *RingBuffer
	idCounter   atomic.Int64

	subscribers map[chan []LogEntry]bool
	batchQueue  []LogEntry
	batchMu     sync.Mutex

	logSources []string
	logDir     string
	mihomoAPI  string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	emergencyActions atomic.Int32
	rateLimiters     map[string]*rateLimitState
	suppressedTotal  atomic.Int64
	nowFunc          func() time.Time
}

var (
	subsystemRegex = regexp.MustCompile(`^\[([a-zA-Z0-9_\-\.\+]+)\]`)
	timestampRegex = regexp.MustCompile(`^(\d{4}[-/]\d{2}[-/]\d{2}[T\s]|\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}\s*)?(\d{2}:\d{2}:\d{2})`)
)

// NewLogDispatcher creates a new LogDispatcher instance with bounded ring buffers.
func NewLogDispatcher(logSources []string, logDir string, mihomoAPI string) *LogDispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	d := &LogDispatcher{
		buffers: map[string]*RingBuffer{
			"mihomo":      NewRingBuffer(500),
			"xray-error":  NewRingBuffer(500),
			"xray-access": NewRingBuffer(300),
			"xkeen":       NewRingBuffer(500),
			"syslog":      NewRingBuffer(300),
			"xcp":         NewRingBuffer(300),
		},
		priorityBuf: NewRingBuffer(200), // Dedicated buffer for Fatal/Error logs across all sources
		subscribers: make(map[chan []LogEntry]bool),
		logSources:  logSources,
		logDir:      logDir,
		mihomoAPI:   mihomoAPI,
		ctx:         ctx,
		cancel:      cancel,
		rateLimiters: map[string]*rateLimitState{
			"mihomo":      {},
			"xray-error":  {},
			"xray-access": {},
			"xkeen":       {},
			"syslog":      {},
			"xcp":         {},
		},
		nowFunc: time.Now,
	}

	return d
}

// Start begins background log tailers, flash guard and WS batch flusher.
func (d *LogDispatcher) Start() {
	d.wg.Add(3)
	go d.batchFlusherLoop()
	go d.flashGuardWorkerLoop()
	go d.startFileConnectors()
}

// Stop gracefully terminates all background workers and connectors.
func (d *LogDispatcher) Stop() {
	d.cancel()
	d.wg.Wait()

	d.mu.Lock()
	for ch := range d.subscribers {
		close(ch)
		delete(d.subscribers, ch)
	}
	d.mu.Unlock()
}

// Subscribe registers a channel for batched LogEntry broadcasts.
func (d *LogDispatcher) Subscribe() chan []LogEntry {
	ch := make(chan []LogEntry, 50)
	d.mu.Lock()
	d.subscribers[ch] = true
	d.mu.Unlock()
	return ch
}

// Unsubscribe removes a broadcast channel.
func (d *LogDispatcher) Unsubscribe(ch chan []LogEntry) {
	d.mu.Lock()
	if d.subscribers[ch] {
		delete(d.subscribers, ch)
		close(ch)
	}
	d.mu.Unlock()
}

// checkRateLimit evaluates per-source throttling against LogRateLimitMaxPerWindow.
// If window has rolled and logs were suppressed, returns an aggregate LogEntry for batchQueue.
func (d *LogDispatcher) checkRateLimit(bufKey string) (bool, *LogEntry) {
	d.mu.RLock()
	limiter, ok := d.rateLimiters[bufKey]
	if !ok {
		limiter = d.rateLimiters["xkeen"]
	}
	d.mu.RUnlock()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := time.Now()
	if d.nowFunc != nil {
		now = d.nowFunc()
	}

	if limiter.windowStart.IsZero() {
		limiter.windowStart = now
	}

	var aggregate *LogEntry
	if now.Sub(limiter.windowStart) >= LogRateLimitWindowDuration {
		if limiter.suppressed > 0 {
			aggregate = &LogEntry{
				ID:        d.idCounter.Add(1),
				Timestamp: now.Format("15:04:05"),
				Source:    bufKey,
				Level:     "warning",
				Subsystem: "system",
				Message:   fmt.Sprintf("[system] %d messages suppressed (rate limit)", limiter.suppressed),
			}
		}
		limiter.windowStart = now
		limiter.passCount = 0
		limiter.suppressed = 0
	}

	if limiter.passCount < LogRateLimitMaxPerWindow {
		limiter.passCount++
		return true, aggregate
	}

	limiter.suppressed++
	d.suppressedTotal.Add(1)
	return false, aggregate
}

// IngestLine parses, redacts, stores and queues a log line.
func (d *LogDispatcher) IngestLine(rawLine string, fallbackSource string) LogEntry {
	redacted := RedactSensitiveText(rawLine)
	entry := d.parseRedactedLine(redacted, fallbackSource)

	entry.ID = d.idCounter.Add(1)

	// Save to source ring buffer
	d.mu.RLock()
	bufKey := entry.Source
	if entry.Source == "xray" {
		if strings.Contains(strings.ToLower(rawLine), "access") {
			bufKey = "xray-access"
		} else {
			bufKey = "xray-error"
		}
	}
	buf, ok := d.buffers[bufKey]
	if !ok {
		buf = d.buffers["xkeen"]
	}
	d.mu.RUnlock()

	// Check per-source rate limit (LOGHUB-07)
	allowed, aggregate := d.checkRateLimit(bufKey)
	if aggregate != nil {
		d.batchMu.Lock()
		d.batchQueue = append(d.batchQueue, *aggregate)
		d.batchMu.Unlock()
	}

	if !allowed {
		return entry
	}

	if buf != nil {
		buf.Add(entry)
	}

	// If Fatal/Error, also save to Priority Queue to prevent Buffer Starvation
	if entry.Level == "error" || entry.Level == "fatal" {
		d.priorityBuf.Add(entry)
	}

	// Enqueue for batch broadcast
	d.batchMu.Lock()
	d.batchQueue = append(d.batchQueue, entry)
	d.batchMu.Unlock()

	return entry
}

func (d *LogDispatcher) parseRedactedLine(line string, fallbackSource string) LogEntry {
	text := strings.TrimSpace(line)
	source := fallbackSource
	level := "info"
	subsystem := ""
	timestamp := time.Now().Format("15:04:05")

	// 1. Source prefix check: [source]
	if strings.HasPrefix(text, "[") {
		idx := strings.Index(text, "]")
		if idx > 1 {
			tag := strings.ToLower(text[1:idx])
			if strings.Contains(tag, "xray") || strings.Contains(tag, "access") || strings.Contains(tag, "error.log") {
				source = "xray"
			} else if strings.Contains(tag, "mihomo") {
				source = "mihomo"
			} else if strings.Contains(tag, "xkeen") || strings.Contains(tag, "zkeen") {
				source = "xkeen"
			} else if strings.Contains(tag, "syslog") || strings.Contains(tag, "messages") {
				source = "syslog"
			} else if strings.Contains(tag, "xcp") {
				source = "xcp"
			}
			text = strings.TrimSpace(text[idx+1:])
		}
	}

	// 2. Timestamp extraction
	if tsMatch := timestampRegex.FindStringSubmatch(text); len(tsMatch) > 2 && tsMatch[2] != "" {
		timestamp = tsMatch[2]
		text = strings.TrimSpace(text[len(tsMatch[0]):])
	}

	// 3. Extract bracket tags for Level & Subsystem
	for {
		match := subsystemRegex.FindStringSubmatch(text)
		if len(match) < 2 {
			break
		}
		tag := match[1]
		lowerTag := strings.ToLower(tag)

		if lowerTag == "info" || lowerTag == "inf" {
			level = "info"
		} else if lowerTag == "warning" || lowerTag == "warn" || lowerTag == "wrn" {
			level = "warning"
		} else if lowerTag == "error" || lowerTag == "err" || lowerTag == "fatal" {
			level = "error"
		} else if lowerTag == "debug" || lowerTag == "dbg" {
			level = "debug"
		} else if subsystem == "" {
			subsystem = tag
		}

		newText := strings.TrimSpace(strings.TrimPrefix(text, match[0]))
		if newText == text {
			break
		}
		text = newText
	}

	// 4. Heuristic Level detection if still default
	if level == "info" {
		lowerText := strings.ToLower(text)
		if strings.Contains(lowerText, "error") || strings.Contains(lowerText, "fatal") || strings.Contains(lowerText, "panic") || strings.Contains(lowerText, "failed") {
			level = "error"
		} else if strings.Contains(lowerText, "warn") {
			level = "warning"
		} else if strings.Contains(lowerText, "debug") {
			level = "debug"
		}
	}

	if source == "" {
		source = "xkeen"
	}

	return LogEntry{
		Timestamp: timestamp,
		Source:    source,
		Level:     level,
		Subsystem: subsystem,
		Message:   text,
	}
}

// batchFlusherLoop flushes queued logs to WebSocket clients every 120ms.
func (d *LogDispatcher) batchFlusherLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(LogBatchFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.batchMu.Lock()
			if len(d.batchQueue) == 0 {
				d.batchMu.Unlock()
				continue
			}
			batch := d.batchQueue
			d.batchQueue = nil
			d.batchMu.Unlock()

			d.mu.RLock()
			for ch := range d.subscribers {
				select {
				case ch <- batch:
				default:
					// Slow consumer: drop batch to avoid blocking dispatcher
				}
			}
			d.mu.RUnlock()
		}
	}
}

// flashGuardWorkerLoop monitors /opt filesystem space and truncates logs if free < 10MB.
func (d *LogDispatcher) flashGuardWorkerLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.CheckFlashPressure()
		}
	}
}

// CheckFlashPressure checks disk space and truncates logs if under critical pressure.
func (d *LogDispatcher) CheckFlashPressure() {
	targetDir := d.logDir
	if targetDir == "" {
		targetDir = "/opt/var/log"
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(targetDir, &stat); err != nil {
		return
	}

	freeBytes := stat.Bavail * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	minFree := uint64(10 * 1024 * 1024) // 10 MB

	if totalBytes > 0 && freeBytes < minFree {
		d.emergencyTruncate(targetDir, freeBytes)
	}
}

func truncateLogTail(filePath string, maxBytes int64) (err error) {
	f, openErr := os.OpenFile(filePath, os.O_RDWR, 0644)
	if openErr != nil {
		return openErr
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	buf := make([]byte, maxBytes)
	if _, err = f.Seek(-maxBytes, io.SeekEnd); err != nil {
		return err
	}
	n, readErr := f.Read(buf)
	if readErr != nil && readErr != io.EOF {
		return readErr
	}
	if err = f.Truncate(0); err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = f.Write(buf[:n]); err != nil {
		return err
	}
	return f.Sync()
}

func (d *LogDispatcher) emergencyTruncate(logDir string, freeBytes uint64) {
	d.emergencyActions.Add(1)
	alertMsg := fmt.Sprintf("[SYSTEM ALERT] Flash Guard Emergency Truncate: Free disk space on %s is low (%d KB). Truncating oversized logs to 500 KB.", logDir, freeBytes/1024)
	log.Println(alertMsg)
	d.IngestLine(alertMsg, "xcp")

	const maxFileSize = int64(500 * 1024) // 500 KB

	_ = filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Size() > maxFileSize {
			if terr := truncateLogTail(path, maxFileSize); terr != nil {
				log.Printf("Flash Guard: failed to truncate log %s: %v", path, terr)
			}
		}
		return nil
	})
}

// GetFlashHealth returns current log storage metrics and filesystem pressure status.
func (d *LogDispatcher) GetFlashHealth() FlashHealthInfo {
	targetDir := d.logDir
	if targetDir == "" {
		targetDir = "/opt/var/log"
	}

	var totalLogs int64
	_ = filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			totalLogs += info.Size()
		}
		return nil
	})

	var stat syscall.Statfs_t
	var freeBytes, totalBytes uint64
	if err := syscall.Statfs(targetDir, &stat); err == nil {
		freeBytes = stat.Bavail * uint64(stat.Bsize)
		totalBytes = stat.Blocks * uint64(stat.Bsize)
	}

	isUnderPressure := totalBytes > 0 && freeBytes < 10*1024*1024

	return FlashHealthInfo{
		TotalLogsBytes:   totalLogs,
		FreeSpaceBytes:   freeBytes,
		TotalSpaceBytes:  totalBytes,
		IsUnderPressure:  isUnderPressure,
		EmergencyActions: int(d.emergencyActions.Load()),
		SuppressedLogs:   d.suppressedTotal.Load(),
	}
}

// SuppressedTotal returns total number of suppressed log entries across all sources.
func (d *LogDispatcher) SuppressedTotal() int64 {
	return d.suppressedTotal.Load()
}

// GetHistory returns historical entries for a given source and level filter.
func (d *LogDispatcher) GetHistory(source string, level string, limit int) []LogEntry {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var all []LogEntry
	if source == "priority" || source == "errors" {
		all = d.priorityBuf.GetAll()
	} else if source != "" && source != "all" {
		if buf, ok := d.buffers[source]; ok {
			all = buf.GetAll()
		}
	} else {
		// Collect from all buffers
		for _, buf := range d.buffers {
			all = append(all, buf.GetAll()...)
		}
	}

	if level != "" && level != "all" {
		filtered := make([]LogEntry, 0, len(all))
		for _, e := range all {
			if strings.EqualFold(e.Level, level) {
				filtered = append(filtered, e)
			}
		}
		all = filtered
	}

	// Buffers live in a map, so entries from several sources arrive in random
	// order; IDs are assigned monotonically at ingest time.
	sort.SliceStable(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	if len(all) > limit {
		all = all[len(all)-limit:]
	}

	return all
}

// ClearBuffers clears all ring buffers.
func (d *LogDispatcher) ClearBuffers(source string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if source == "all" || source == "" {
		for _, buf := range d.buffers {
			buf.Clear()
		}
		d.priorityBuf.Clear()
	} else if buf, ok := d.buffers[source]; ok {
		buf.Clear()
	}
}

// SetLogLevel changes runtime log-level for Mihomo via REST API without restart.
func (d *LogDispatcher) SetLogLevel(source string, level string) error {
	if source == "mihomo" && d.mihomoAPI != "" {
		payload, _ := json.Marshal(map[string]string{"log-level": level})
		req, err := http.NewRequest(http.MethodPatch, d.mihomoAPI+"/configs", strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("mihomo API returned status %d", resp.StatusCode)
		}
	}
	return nil
}

// startFileConnectors tails standard log files and feeds entries into LogDispatcher.
func (d *LogDispatcher) startFileConnectors() {
	defer d.wg.Done()

	sources := []string{
		"/opt/var/log/xkeen.log",
		"/opt/var/log/xkeen-detached.log",
		"/opt/var/log/xray/access.log",
		"/opt/var/log/xray/error.log",
		"/opt/var/log/mihomo.log",
		"/opt/var/log/xcp.log",
	}
	for _, s := range d.logSources {
		if s != "" {
			sources = append(sources, s)
		}
	}

	// History from before the start is read synchronously and ingested in
	// time order: concurrent tails would interleave IDs of different
	// sources, and the UI orders entries by ID.
	var backfill []backfillLine
	offsets := make(map[string]int64)
	seen := make(map[string]bool)
	var files []string
	for _, src := range sources {
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		// Хвост ведётся и для ещё не созданных файлов: лог ядра появляется
		// после старта панели (XKeen стартует позже, смена Xray ↔ Mihomo).
		// Такой файл новый — читается с начала.
		files = append(files, src)
		if _, err := os.Stat(src); err != nil {
			offsets[src] = 0
			continue
		}
		lines, offset, err := readFileTail(src, fileTailBytes)
		if err != nil {
			continue
		}
		offsets[src] = offset
		source := fileFallbackSource(src)
		for _, l := range lines {
			backfill = append(backfill, backfillLine{raw: l, source: source})
		}
	}

	_, lookErr := exec.LookPath("logread")
	useLogread := lookErr == nil
	rciLastID := int64(-1)
	if !useLogread {
		if entries, err := fetchRCILog(d.ctx, defaultRCILogURL, 50); err == nil {
			for _, e := range entries {
				backfill = append(backfill, backfillLine{raw: rciLogLine(e), source: "syslog"})
				rciLastID = e.ID
			}
		}
	}

	d.ingestBackfill(backfill, time.Now())

	for _, src := range files {
		start, ok := offsets[src]
		if !ok {
			start = -1
		}
		d.wg.Add(1)
		go d.tailFileFrom(src, start)
	}

	// Syslog connector: logread -f, or the Keenetic RCI.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if useLogread {
			d.followLogread()
			return
		}
		d.pollRCILogFrom(defaultRCILogURL, 5*time.Second, rciLastID)
	}()
}

// fileTailBytes is how much of each log file is shown on start.
const fileTailBytes = 8192

// maxPartialLineBytes ограничивает недописанную строку: если перевода строки
// так и нет, накопленное уходит в журнал как есть.
const maxPartialLineBytes = 64 * 1024

type backfillLine struct {
	raw    string
	source string
}

// readFileTail returns the complete lines of the last n bytes of path and
// the offset the follower continues from.
func readFileTail(path string, n int64) ([]string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := stat.Size()
	start := int64(0)
	if size > n {
		start = size - n
	}
	buf := make([]byte, size-start)
	if _, err := file.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, 0, err
	}
	text := string(buf)
	if start > 0 {
		// The window starts mid-line; drop the partial fragment.
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			return nil, size, nil
		}
		text = text[i+1:]
	}
	// An unterminated last line is left to the follower.
	end := strings.LastIndexByte(text, '\n')
	if end < 0 {
		return nil, size - int64(len(text)), nil
	}
	offset := size - int64(len(text)-end-1)
	var lines []string
	for _, l := range strings.Split(text[:end], "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, offset, nil
}

// ingestBackfill ingests historical lines ordered by their time of day.
// Times later than now belong to the previous day.
func (d *LogDispatcher) ingestBackfill(lines []backfillLine, now time.Time) {
	nowSec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	keys := make([]int, len(lines))
	for i, l := range lines {
		e := d.parseRedactedLine(RedactSensitiveText(l.raw), l.source)
		keys[i] = nowSec
		if t, err := time.Parse("15:04:05", e.Timestamp); err == nil {
			sec := t.Hour()*3600 + t.Minute()*60 + t.Second()
			if sec > nowSec+60 {
				sec -= 86400
			}
			keys[i] = sec
		}
	}
	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return keys[order[a]] < keys[order[b]] })
	for _, i := range order {
		d.IngestLine(lines[i].raw, lines[i].source)
	}
}

func fileFallbackSource(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "xray") || strings.Contains(path, "xray"):
		return "xray"
	case strings.Contains(base, "mihomo"):
		return "mihomo"
	case strings.Contains(base, "xcp"):
		return "xcp"
	default:
		return "xkeen"
	}
}

func (d *LogDispatcher) tailFile(path string) {
	d.tailFileFrom(path, -1)
}

// tailFileFrom follows path starting at offset start; a negative start
// shows the recent tail first.
func (d *LogDispatcher) tailFileFrom(path string, start int64) {
	defer d.wg.Done()

	fallbackSource := fileFallbackSource(path)

	reopened := false
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		file, err := os.Open(path)
		if err != nil {
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		stat, statErr := file.Stat()
		reader := bufio.NewReader(file)
		// On the first open continue after the backfill or show the recent
		// tail; a file that shrank since was rotated. After a rotation the
		// new file is read from its start so no lines are lost or repeated.
		if !reopened && start >= 0 && statErr == nil && start <= stat.Size() {
			_, _ = file.Seek(start, io.SeekStart)
		} else if !reopened && start < 0 && statErr == nil && stat.Size() > fileTailBytes {
			_, _ = file.Seek(-fileTailBytes, io.SeekEnd)
			// The seek lands mid-line; drop the partial fragment.
			_, _ = reader.ReadString('\n')
		}
		reopened = true

		// Строка, которую ядро дописало не до конца (два write()), копится
		// до перевода строки, а не уходит в журнал обрывками
		var partial string
		flushPartial := func() {
			if partial != "" {
				d.IngestLine(partial, fallbackSource)
				partial = ""
			}
		}

		for {
			select {
			case <-d.ctx.Done():
				file.Close()
				return
			default:
			}

			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				if readErr == io.EOF && !strings.HasSuffix(line, "\n") {
					partial += line
					if len(partial) > maxPartialLineBytes {
						flushPartial()
					}
				} else {
					d.IngestLine(partial+line, fallbackSource)
					partial = ""
				}
			}

			if readErr != nil {
				if readErr == io.EOF {
					// Reopen when the path now points to a different file
					// (rename-based rotation) or the file was truncated.
					if curStat, err := os.Stat(path); err == nil && statErr == nil {
						pos, _ := file.Seek(0, io.SeekCurrent)
						pos -= int64(reader.Buffered())
						if !os.SameFile(stat, curStat) || curStat.Size() < pos {
							flushPartial()
							file.Close()
							break
						}
					}
					select {
					case <-d.ctx.Done():
						file.Close()
						return
					case <-time.After(500 * time.Millisecond):
					}
					continue
				}
				flushPartial()
				file.Close()
				break
			}
		}
	}
}

// followLogread streams "logread -f" and restarts it when it exits (syslog
// restart, oversized line) instead of silently losing the system log.
func (d *LogDispatcher) followLogread() {
	for {
		cmd := exec.CommandContext(d.ctx, "logread", "-f")
		stdout, err := cmd.StdoutPipe()
		if err == nil && cmd.Start() == nil {
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			for scanner.Scan() {
				d.IngestLine(scanner.Text(), "syslog")
			}
			_ = cmd.Wait()
		}
		select {
		case <-d.ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

const defaultRCILogURL = "http://127.0.0.1:79/rci/"

type rciLogEntry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Ident     string `json:"ident"`
	Message   struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	} `json:"message"`
}

// fetchRCILog asks the Keenetic RCI for the last lines of the system log.
func fetchRCILog(ctx context.Context, rciURL string, lines int) ([]rciLogEntry, error) {
	body := fmt.Sprintf(`{"show":{"log":{"max-lines":%d}}}`, lines)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rciURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rci returned %d", resp.StatusCode)
	}
	var out struct {
		Show struct {
			Log struct {
				Log map[string]rciLogEntry `json:"log"`
			} `json:"log"`
		} `json:"show"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	entries := make([]rciLogEntry, 0, len(out.Show.Log.Log))
	for _, e := range out.Show.Log.Log {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// rciLogLine formats an RCI entry for IngestLine: "[syslog] 09:07:48 [error] [dropbear] msg".
func rciLogLine(e rciLogEntry) string {
	ts := e.Timestamp
	if i := strings.LastIndexByte(ts, ' '); i >= 0 {
		ts = ts[i+1:]
	}
	ident := e.Ident
	if i := strings.IndexByte(ident, '['); i >= 0 {
		ident = ident[:i]
	}
	ident = strings.Map(func(r rune) rune {
		if r == ' ' || r == ']' || r == '[' {
			return '_'
		}
		return r
	}, ident)
	level := strings.ToLower(e.Message.Level)
	if level == "" {
		level = "info"
	}
	line := "[syslog] " + ts + " [" + level + "]"
	if ident != "" {
		line += " [" + ident + "]"
	}
	return line + " " + e.Message.Message
}

// pollRCILog ingests new Keenetic system log entries every interval.
func (d *LogDispatcher) pollRCILog(rciURL string, interval time.Duration) {
	d.pollRCILogFrom(rciURL, interval, -1)
}

// pollRCILogFrom polls the RCI log for entries newer than lastID; -1 starts
// with the recent entries.
func (d *LogDispatcher) pollRCILogFrom(rciURL string, interval time.Duration, lastID int64) {
	wait := time.Duration(0)
	if lastID >= 0 {
		wait = interval
	}
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = interval
		lines := 100
		if lastID < 0 {
			lines = 50
		}
		entries, err := fetchRCILog(d.ctx, rciURL, lines)
		if err != nil {
			// Not a Keenetic or RCI unavailable: retry rarely.
			wait = time.Minute
			continue
		}
		if n := len(entries); n > 0 && entries[n-1].ID < lastID {
			lastID = -1 // log was cleared or the router rebooted
		}
		for _, e := range entries {
			if e.ID > lastID {
				d.IngestLine(rciLogLine(e), "syslog")
				lastID = e.ID
			}
		}
	}
}
