package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// TrafficQuota represents a traffic limit
type TrafficQuota struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	TargetType     string `json:"target_type"` // "proxy", "global"
	TargetID       string `json:"target_id"`   // proxy name or empty
	LimitBytes     int64  `json:"limit_bytes"`
	Period         string `json:"period"` // "daily", "weekly", "monthly"
	Enabled        bool   `json:"enabled"`
	AlertThreshold int    `json:"alert_threshold"` // 0-100, percent
	Action         string `json:"action"`          // "notify", "throttle", "log_only", "block"
	CurrentBytes   int64  `json:"current_bytes"`
	LastReset      int64  `json:"last_reset"`
}

// ProxyTraffic holds accumulated traffic per proxy
type ProxyTraffic struct {
	ProxyName     string `json:"proxy_name"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
}

// TrafficAlert represents an alert when quota is exceeded
type TrafficAlert struct {
	QuotaID   string `json:"quota_id"`
	QuotaName string `json:"quota_name"`
	Severity  string `json:"severity"` // "warning", "critical"
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// TrafficStore is the on-disk format
type TrafficStore struct {
	Quotas     []TrafficQuota           `json:"quotas"`
	ProxyStats map[string]*ProxyTraffic `json:"proxy_stats"`
}

// saveLockThrottle is the minimum interval between background/periodic saves
// (CRUD-triggered saves always use force=true and bypass throttling).
const saveLockThrottle = 5 * time.Second

// maxTrafficFileSize is the rotation threshold for traffic.json.
const maxTrafficFileSize = 5 * 1024 * 1024 // 5 MB

// mihomoConn is a single connection entry from the Mihomo /connections stream.
type mihomoConn struct {
	ID       string   `json:"id"`
	Chains   []string `json:"chains"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
}

// TrafficQuotaService manages traffic accounting and quotas
type TrafficQuotaService struct {
	dataDir    string
	mihomoURL  string
	secret     string
	quotas     []TrafficQuota
	proxyStats map[string]*ProxyTraffic
	alerts     []TrafficAlert
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
	lastSave   time.Time // time of last successful disk write (protected by mu)

	// connectionTracker maps connection ID -> last seen {upload, download}
	connectionTracker sync.Map
}

func NewTrafficQuotaService(dataDir, mihomoURL, secret string) *TrafficQuotaService {
	svc := &TrafficQuotaService{
		dataDir:    dataDir,
		mihomoURL:  mihomoURL,
		secret:     secret,
		quotas:     []TrafficQuota{},
		proxyStats: make(map[string]*ProxyTraffic),
		alerts:     []TrafficAlert{},
		stopCh:     make(chan struct{}),
	}
	svc.load()
	return svc
}

func (s *TrafficQuotaService) Start() {
	s.wg.Add(1)
	go s.collectorLoop()
}

func (s *TrafficQuotaService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *TrafficQuotaService) storePath() string {
	return filepath.Join(s.dataDir, "traffic.json")
}

func (s *TrafficQuotaService) load() {
	data, err := os.ReadFile(s.storePath())
	if err != nil {
		return
	}
	var store TrafficStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}
	s.mu.Lock()
	s.quotas = store.Quotas
	if store.ProxyStats != nil {
		s.proxyStats = store.ProxyStats
	} else {
		s.proxyStats = make(map[string]*ProxyTraffic)
	}
	s.mu.Unlock()
}

// saveLocked writes state to disk. Caller MUST hold s.mu (write lock).
// If force is false the write is skipped when a previous write happened
// within saveLockThrottle — suitable for high-frequency periodic saves.
// Pass force=true for CRUD operations (quota add/update/delete/reset)
// so user changes are always persisted immediately.
func (s *TrafficQuotaService) saveLocked(force bool) error {
	if !force && !s.lastSave.IsZero() && time.Since(s.lastSave) < saveLockThrottle {
		return nil
	}
	store := TrafficStore{
		Quotas:     s.quotas,
		ProxyStats: s.proxyStats,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := utils.AtomicWriteFile(s.storePath(), data, 0600); err != nil {
		return err
	}
	s.lastSave = time.Now()
	s.rotateIfNeeded()
	return nil
}

// rotateIfNeeded renames traffic.json to a timestamped .bak when it exceeds
// maxTrafficFileSize and purges orphaned proxyStats entries to reclaim space.
// Caller MUST hold s.mu (write lock).
func (s *TrafficQuotaService) rotateIfNeeded() {
	info, err := os.Stat(s.storePath())
	if err != nil || info.Size() < maxTrafficFileSize {
		return
	}
	bakPath := fmt.Sprintf("%s.%s.bak", s.storePath(), time.Now().Format("20060102-150405"))
	if err := os.Rename(s.storePath(), bakPath); err != nil {
		log.Printf("traffic: rotate failed: %v", err)
		return
	}
	log.Printf("traffic: traffic.json exceeded 5 MB, rotated → %s", bakPath)

	// Keep only proxyStats entries referenced by active quotas.
	active := make(map[string]bool)
	for _, q := range s.quotas {
		if q.TargetType == "proxy" && q.TargetID != "" {
			active[q.TargetID] = true
		}
	}
	for name := range s.proxyStats {
		if !active[name] {
			delete(s.proxyStats, name)
		}
	}
}

// --- CRUD for quotas ---

func (s *TrafficQuotaService) ListQuotas() []TrafficQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]TrafficQuota, len(s.quotas))
	copy(result, s.quotas)
	return result
}

func (s *TrafficQuotaService) GetQuota(id string) (TrafficQuota, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			return s.quotas[i], true
		}
	}
	return TrafficQuota{}, false
}

func (s *TrafficQuotaService) AddQuota(q *TrafficQuota) error {
	if q.ID == "" {
		q.ID = fmt.Sprintf("quota_%d", time.Now().UnixNano())
	}
	q.LastReset = time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotas = append(s.quotas, *q)
	return s.saveLocked(true)
}

func (s *TrafficQuotaService) UpdateQuota(id string, q *TrafficQuota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			s.quotas[i] = *q
			s.quotas[i].ID = id
			return s.saveLocked(true)
		}
	}
	return fmt.Errorf("quota not found")
}

func (s *TrafficQuotaService) DeleteQuota(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.quotas {
		if q.ID == id {
			s.quotas = append(s.quotas[:i], s.quotas[i+1:]...)
			return s.saveLocked(true)
		}
	}
	return fmt.Errorf("quota not found")
}

func (s *TrafficQuotaService) SetQuotaEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			s.quotas[i].Enabled = enabled
			return s.saveLocked(true)
		}
	}
	return fmt.Errorf("quota not found")
}

func (s *TrafficQuotaService) ResetQuota(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			s.quotas[i].CurrentBytes = 0
			s.quotas[i].LastReset = time.Now().Unix()
			return s.saveLocked(true)
		}
	}
	return fmt.Errorf("quota not found")
}

// --- Stats & Alerts ---

func (s *TrafficQuotaService) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proxyList := make([]*ProxyTraffic, 0, len(s.proxyStats))
	var totalUpload, totalDownload int64
	for _, stat := range s.proxyStats {
		proxyList = append(proxyList, stat)
		totalUpload += stat.UploadBytes
		totalDownload += stat.DownloadBytes
	}

	return map[string]interface{}{
		"proxies":        proxyList,
		"total_upload":   totalUpload,
		"total_download": totalDownload,
		"total":          totalUpload + totalDownload,
	}
}

func (s *TrafficQuotaService) GetAlerts() []TrafficAlert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]TrafficAlert, len(s.alerts))
	copy(result, s.alerts)
	return result
}

func (s *TrafficQuotaService) ClearAlerts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = []TrafficAlert{}
}

// --- Collector ---

// httpToWS converts an http(s):// URL to ws(s)://.
func httpToWS(rawURL string) string {
	switch {
	case strings.HasPrefix(rawURL, "https://"):
		return "wss://" + rawURL[len("https://"):]
	case strings.HasPrefix(rawURL, "http://"):
		return "ws://" + rawURL[len("http://"):]
	}
	return rawURL
}

func (s *TrafficQuotaService) collectorLoop() {
	defer s.wg.Done()

	// Periodic housekeeping: quota resets and threshold checks.
	resetTicker := time.NewTicker(1 * time.Minute)
	defer resetTicker.Stop()

	s.checkResets()

	// WebSocket stream runs in its own goroutine with reconnect logic.
	s.wg.Add(1)
	go s.connectionsWSLoop()

	for {
		select {
		case <-resetTicker.C:
			s.checkResets()
			s.checkQuotas()
		case <-s.stopCh:
			return
		}
	}
}

// connectionsWSLoop connects to Mihomo's /connections WebSocket endpoint and
// processes real-time connection snapshots. Reconnects automatically with
// exponential backoff (5 s → 60 s) when the stream is interrupted.
func (s *TrafficQuotaService) connectionsWSLoop() {
	defer s.wg.Done()

	backoff := 5 * time.Second
	const maxBackoff = 60 * time.Second

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		start := time.Now()
		err := s.streamConnections()
		if err == nil {
			// Graceful shutdown via stopCh.
			return
		}

		// If the session ran for more than 30 s it was healthy — reset backoff.
		if time.Since(start) > 30*time.Second {
			backoff = 5 * time.Second
		}

		log.Printf("TrafficQuota: WS connections stream ended: %v — retry in %s", err, backoff)

		select {
		case <-time.After(backoff):
		case <-s.stopCh:
			return
		}

		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// streamConnections opens a single WebSocket session with Mihomo's /connections
// endpoint and processes every snapshot it receives. Returns nil on graceful
// shutdown (stopCh closed) and a non-nil error when the connection breaks.
func (s *TrafficQuotaService) streamConnections() error {
	wsURL := httpToWS(strings.TrimRight(s.mihomoURL, "/")) + "/connections"

	header := http.Header{}
	if s.secret != "" {
		header.Set("Authorization", "Bearer "+s.secret)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.Close()

	// Close the WebSocket when the service stops so ReadJSON unblocks.
	go func() {
		<-s.stopCh
		conn.Close()
	}()

	log.Printf("TrafficQuota: WebSocket connected to %s", wsURL)

	for {
		var payload struct {
			Connections []mihomoConn `json:"connections"`
		}
		if err := conn.ReadJSON(&payload); err != nil {
			select {
			case <-s.stopCh:
				return nil // graceful shutdown
			default:
				return fmt.Errorf("read: %w", err)
			}
		}
		s.processConnSnapshot(payload.Connections)
	}
}

// processConnSnapshot computes per-proxy traffic deltas from a Mihomo
// connections snapshot and accumulates them into proxyStats.
func (s *TrafficQuotaService) processConnSnapshot(connections []mihomoConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	activeIDs := make(map[string]bool, len(connections))

	for _, conn := range connections {
		if len(conn.Chains) == 0 {
			continue
		}
		proxyName := conn.Chains[len(conn.Chains)-1]
		if proxyName == "" || proxyName == "DIRECT" || proxyName == "REJECT" {
			continue
		}

		activeIDs[conn.ID] = true

		var deltaUp, deltaDown int64
		if val, ok := s.connectionTracker.Load(conn.ID); ok {
			last := val.(connStats)
			deltaUp = conn.Upload - last.Upload
			deltaDown = conn.Download - last.Download
		} else {
			// New connection: treat current bytes as the delta.
			deltaUp = conn.Upload
			deltaDown = conn.Download
		}

		if deltaUp < 0 {
			deltaUp = 0
		}
		if deltaDown < 0 {
			deltaDown = 0
		}

		if deltaUp > 0 || deltaDown > 0 {
			stat, ok := s.proxyStats[proxyName]
			if !ok {
				stat = &ProxyTraffic{ProxyName: proxyName}
				s.proxyStats[proxyName] = stat
			}
			stat.UploadBytes += deltaUp
			stat.DownloadBytes += deltaDown
			stat.TotalBytes = stat.UploadBytes + stat.DownloadBytes
		}

		s.connectionTracker.Store(conn.ID, connStats{Upload: conn.Upload, Download: conn.Download})
	}

	// Clean up closed connections from the tracker.
	s.connectionTracker.Range(func(key, value interface{}) bool {
		if !activeIDs[key.(string)] {
			s.connectionTracker.Delete(key)
		}
		return true
	})

	if err := s.saveLocked(false); err != nil {
		log.Printf("TrafficQuota: failed to save stats: %v", err)
	}
}

func (s *TrafficQuotaService) checkResets() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for i := range s.quotas {
		q := &s.quotas[i]
		if !q.Enabled {
			continue
		}
		lastReset := time.Unix(q.LastReset, 0)
		shouldReset := false

		switch q.Period {
		case "daily":
			shouldReset = lastReset.Year() != now.Year() || lastReset.YearDay() != now.YearDay()
		case "weekly":
			shouldReset = lastReset.AddDate(0, 0, 7).Before(now)
		case "monthly":
			shouldReset = lastReset.Year() != now.Year() || lastReset.Month() != now.Month()
		}

		if shouldReset {
			q.CurrentBytes = 0
			q.LastReset = now.Unix()
			changed = true
			// Reset accumulated proxy stats so checkQuotas reads from zero
			// after the period boundary, not from historical cumulative totals.
			switch q.TargetType {
			case "proxy":
				if stat, ok := s.proxyStats[q.TargetID]; ok {
					stat.UploadBytes = 0
					stat.DownloadBytes = 0
					stat.TotalBytes = 0
				}
			case "global":
				for _, stat := range s.proxyStats {
					stat.UploadBytes = 0
					stat.DownloadBytes = 0
					stat.TotalBytes = 0
				}
			}
		}
	}

	if changed {
		// Background periodic save — use force=false to throttle disk I/O.
		_ = s.saveLocked(false)
	}
}

// checkQuotas checks all quotas against current stats

// connStats holds last seen bytes for a connection
type connStats struct {
	Upload   int64
	Download int64
}

func (s *TrafficQuotaService) checkQuotas() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.quotas {
		q := &s.quotas[i]
		if !q.Enabled || q.LimitBytes <= 0 {
			continue
		}

		var current int64
		switch q.TargetType {
		case "proxy":
			if stat, ok := s.proxyStats[q.TargetID]; ok {
				current = stat.TotalBytes
			}
		case "global":
			for _, stat := range s.proxyStats {
				current += stat.TotalBytes
			}
		}

		q.CurrentBytes = current
		percent := float64(current) / float64(q.LimitBytes) * 100

		if percent >= 100 {
			s.addAlert(q, "critical", fmt.Sprintf("Лимит '%s' превышен: %s из %s (%.0f%%)",
				q.Name, formatBytes(current), formatBytes(q.LimitBytes), percent))
		} else if q.AlertThreshold > 0 && percent >= float64(q.AlertThreshold) {
			s.addAlert(q, "warning", fmt.Sprintf("Лимит '%s' на %.0f%%: %s из %s",
				q.Name, percent, formatBytes(current), formatBytes(q.LimitBytes)))
		}
	}
}

func (s *TrafficQuotaService) addAlert(q *TrafficQuota, severity, message string) {
	// Deduplicate: don't add same alert within 1 hour
	for _, a := range s.alerts {
		if a.QuotaID == q.ID && a.Severity == severity {
			if time.Now().Unix()-a.Timestamp < 3600 {
				return
			}
		}
	}

	// Cap alerts list to 100 items to prevent memory leak
	if len(s.alerts) >= 100 {
		s.alerts = s.alerts[1:]
	}

	s.alerts = append(s.alerts, TrafficAlert{
		QuotaID:   q.ID,
		QuotaName: q.Name,
		Severity:  severity,
		Message:   message,
		Timestamp: time.Now().Unix(),
	})
}

func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
