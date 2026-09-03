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

// FlashHealthInfo represents the status of flash memory and log files.
type FlashHealthInfo struct {
	TotalLogsBytes   int64 `json:"total_logs_bytes"`
	FreeSpaceBytes   uint64 `json:"free_space_bytes"`
	TotalSpaceBytes  uint64 `json:"total_space_bytes"`
	IsUnderPressure  bool   `json:"is_under_pressure"`
	EmergencyActions int    `json:"emergency_actions"`
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
	ticker := time.NewTicker(120 * time.Millisecond)
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
			// Read tail and rewrite
			f, openErr := os.OpenFile(path, os.O_RDWR, 0644)
			if openErr == nil {
				defer f.Close()
				buf := make([]byte, maxFileSize)
				_, _ = f.Seek(-maxFileSize, io.SeekEnd)
				n, _ := f.Read(buf)
				_ = f.Truncate(0)
				_, _ = f.Seek(0, io.SeekStart)
				_, _ = f.Write(buf[:n])
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
	}
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

	seen := make(map[string]bool)
	for _, src := range sources {
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		if _, err := os.Stat(src); err == nil {
			d.wg.Add(1)
			go d.tailFile(src)
		}
	}

	// Syslog connector via logread -f
	d.wg.Add(1)
	go d.tailSyslog()
}

func (d *LogDispatcher) tailFile(path string) {
	defer d.wg.Done()

	var fallbackSource string
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "xray") || strings.Contains(path, "xray") {
		fallbackSource = "xray"
	} else if strings.Contains(base, "mihomo") {
		fallbackSource = "mihomo"
	} else if strings.Contains(base, "xcp") {
		fallbackSource = "xcp"
	} else {
		fallbackSource = "xkeen"
	}

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

		// Seek to near the end for initial tail
		stat, statErr := file.Stat()
		if statErr == nil && stat.Size() > 8192 {
			_, _ = file.Seek(-8192, io.SeekEnd)
		}

		reader := bufio.NewReader(file)
		for {
			select {
			case <-d.ctx.Done():
				file.Close()
				return
			default:
			}

			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				d.IngestLine(line, fallbackSource)
			}

			if readErr != nil {
				if readErr == io.EOF {
					// Check if file was rotated or truncated
					if curStat, err := os.Stat(path); err == nil {
						if curStat.Size() < stat.Size() {
							// Truncated, reopen
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
				file.Close()
				break
			}
		}
	}
}

func (d *LogDispatcher) tailSyslog() {
	defer d.wg.Done()

	// Check if logread command exists
	if _, err := exec.LookPath("logread"); err != nil {
		return
	}

	cmd := exec.CommandContext(d.ctx, "logread", "-f")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		d.IngestLine(line, "syslog")
	}
	_ = cmd.Wait()
}
