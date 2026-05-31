package services

import (
	"bytes"
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

// TrafficPeaks holds peak upload and download rates over calendar periods
type TrafficPeaks struct {
	PeakHourUp   int64 `json:"peak_hour_up"`
	PeakHourDown int64 `json:"peak_hour_down"`
	PeakDayUp    int64 `json:"peak_day_up"`
	PeakDayDown  int64 `json:"peak_day_down"`
	PeakWeekUp   int64 `json:"peak_week_up"`
	PeakWeekDown int64 `json:"peak_week_down"`
	HourStart    int64 `json:"hour_start"`
	DayStart     int64 `json:"day_start"`
	WeekStart    int64 `json:"week_start"`
}

// TrafficStore is the on-disk format
type TrafficStore struct {
	Quotas         []TrafficQuota           `json:"quotas"`
	ProxyStats     map[string]*ProxyTraffic `json:"proxy_stats"`
	Peaks          TrafficPeaks             `json:"peaks"`
	ResetTime      int64                    `json:"reset_time"`
	BlockedProxies map[string]string        `json:"blocked_proxies"`
}

// saveLockThrottle is the minimum interval between background/periodic saves
// (CRUD-triggered saves always use force=true and bypass throttling).
const saveLockThrottle = 1 * time.Minute

// maxTrafficFileSize is the rotation threshold for traffic.json.
const maxTrafficFileSize = 5 * 1024 * 1024 // 5 MB

// mihomoConnMetadata holds metadata about connection protocol
type mihomoConnMetadata struct {
	Network string `json:"network"`
}

// mihomoConn is a single connection entry from the Mihomo /connections stream.
type mihomoConn struct {
	ID       string             `json:"id"`
	Chains   []string           `json:"chains"`
	Upload   int64              `json:"upload"`
	Download int64              `json:"download"`
	Metadata mihomoConnMetadata `json:"metadata"`
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

	// fan-out: подписчики получают raw JSON-снимки подключений
	connSubs   map[chan []byte]struct{}
	connSubsMu sync.RWMutex

	peaks             TrafficPeaks
	activeConnsCount  int64
	tcpConnsCount     int64
	udpConnsCount     int64
	trafficSubs       map[chan []byte]struct{}
	trafficSubsMu     sync.RWMutex

	httpClient         *http.Client
	blockedProxies     map[string]string
	resetTime          int64
	trackerInitialized bool
}

func NewTrafficQuotaService(dataDir, mihomoURL, secret string) *TrafficQuotaService {
	svc := &TrafficQuotaService{
		dataDir:        dataDir,
		mihomoURL:      mihomoURL,
		secret:         secret,
		quotas:         []TrafficQuota{},
		proxyStats:     make(map[string]*ProxyTraffic),
		alerts:         []TrafficAlert{},
		stopCh:         make(chan struct{}),
		connSubs:       make(map[chan []byte]struct{}),
		trafficSubs:    make(map[chan []byte]struct{}),
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		blockedProxies: make(map[string]string),
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

	s.mu.Lock()
	_ = s.saveLocked(true)
	s.mu.Unlock()
}

func (s *TrafficQuotaService) storePath() string {
	dir := filepath.Join(s.dataDir, "traffic")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "traffic.json")
}

func (s *TrafficQuotaService) load() {
	data, err := os.ReadFile(s.storePath())
	if err != nil {
		s.mu.Lock()
		s.resetTime = time.Now().Unix()
		s.mu.Unlock()
		return
	}
	var store TrafficStore
	if err := json.Unmarshal(data, &store); err != nil {
		s.mu.Lock()
		s.resetTime = time.Now().Unix()
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.quotas = store.Quotas
	if store.ProxyStats != nil {
		s.proxyStats = store.ProxyStats
	} else {
		s.proxyStats = make(map[string]*ProxyTraffic)
	}
	s.peaks = store.Peaks
	s.resetTime = store.ResetTime
	if s.resetTime == 0 {
		s.resetTime = time.Now().Unix()
	}
	if store.BlockedProxies != nil {
		s.blockedProxies = store.BlockedProxies
	} else {
		s.blockedProxies = make(map[string]string)
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
		Quotas:         s.quotas,
		ProxyStats:     s.proxyStats,
		Peaks:          s.peaks,
		ResetTime:      s.resetTime,
		BlockedProxies: s.blockedProxies,
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

	// Write pruned state back to disk immediately so traffic.json exists
	store := TrafficStore{
		Quotas:         s.quotas,
		ProxyStats:     s.proxyStats,
		Peaks:          s.peaks,
		ResetTime:      s.resetTime,
		BlockedProxies: s.blockedProxies,
	}
	data, marshalErr := json.MarshalIndent(store, "", "  ")
	if marshalErr != nil {
		log.Printf("traffic: rotate marshal failed: %v", marshalErr)
		return
	}
	if writeErr := utils.AtomicWriteFile(s.storePath(), data, 0600); writeErr != nil {
		log.Printf("traffic: rotate write failed: %v", writeErr)
		return
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
		"peaks":          s.peaks,
		"reset_time":     s.resetTime,
	}
}

func (s *TrafficQuotaService) ResetStats() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.proxyStats = make(map[string]*ProxyTraffic)

	now := time.Now()
	s.resetTime = now.Unix()
	s.peaks = TrafficPeaks{
		HourStart: now.Truncate(time.Hour).Unix(),
		DayStart:  time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix(),
	}
	offset := int(now.Weekday() - time.Monday)
	if offset < 0 {
		offset += 7
	}
	s.peaks.WeekStart = time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, now.Location()).Unix()

	for i := range s.quotas {
		s.quotas[i].CurrentBytes = 0
		s.quotas[i].LastReset = now.Unix()
	}

	return s.saveLocked(true)
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
	s.wg.Add(2)
	go s.connectionsWSLoop()
	go s.trafficWSLoop()

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

	done := make(chan struct{})
	defer close(done)

	// Close the WebSocket when the service stops so ReadJSON/ReadMessage unblocks.
	go func() {
		select {
		case <-s.stopCh:
			conn.Close()
		case <-done:
		}
	}()

	log.Printf("TrafficQuota: WebSocket connected to %s", wsURL)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-s.stopCh:
				return nil // graceful shutdown
			default:
				return fmt.Errorf("read: %w", err)
			}
		}
		var payload struct {
			Connections []mihomoConn `json:"connections"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		s.broadcastConnections(raw)
		s.processConnSnapshot(payload.Connections)
	}
}

// broadcastConnections рассылает raw JSON-снимок всем подписчикам WebSocket.
func (s *TrafficQuotaService) broadcastConnections(raw []byte) {
	s.connSubsMu.RLock()
	defer s.connSubsMu.RUnlock()
	for ch := range s.connSubs {
		select {
		case ch <- raw:
		default: // медленный клиент — пропускаем
		}
	}
}

// SubscribeConnections регистрирует канал для получения снимков подключений.
// Возвращает канал и функцию отписки, которую вызывают при закрытии WebSocket.
func (s *TrafficQuotaService) SubscribeConnections() (ch chan []byte, unsub func()) {
	ch = make(chan []byte, 4)
	s.connSubsMu.Lock()
	s.connSubs[ch] = struct{}{}
	s.connSubsMu.Unlock()
	unsub = func() {
		s.connSubsMu.Lock()
		delete(s.connSubs, ch)
		s.connSubsMu.Unlock()
	}
	return
}

// processConnSnapshot computes per-proxy traffic deltas from a Mihomo
// connections snapshot and accumulates them into proxyStats.
func (s *TrafficQuotaService) processConnSnapshot(connections []mihomoConn) {
	s.mu.Lock()

	activeIDs := make(map[string]bool, len(connections))
	isFirstSnapshot := !s.trackerInitialized
	s.trackerInitialized = true

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
			if !isFirstSnapshot {
				// New connection: treat current bytes as the delta.
				deltaUp = conn.Upload
				deltaDown = conn.Download
			}
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

			for i := range s.quotas {
				q := &s.quotas[i]
				if !q.Enabled {
					continue
				}
				match := false
				if q.TargetType == "global" {
					match = true
				} else if q.TargetType == "proxy" && q.TargetID == proxyName {
					match = true
				}
				if match {
					q.CurrentBytes += (deltaUp + deltaDown)
				}
			}
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

	var activeCount, tcpCount, udpCount int64
	activeCount = int64(len(connections))
	for _, conn := range connections {
		net := strings.ToUpper(conn.Metadata.Network)
		if net == "TCP" {
			tcpCount++
		} else if net == "UDP" {
			udpCount++
		}
	}
	s.activeConnsCount = activeCount
	s.tcpConnsCount = tcpCount
	s.udpConnsCount = udpCount

	if err := s.saveLocked(false); err != nil {
		log.Printf("TrafficQuota: failed to save stats: %v", err)
	}
	s.mu.Unlock()

	s.checkQuotas()
}

// SubscribeTraffic регистрирует канал для получения снимков трафика.
func (s *TrafficQuotaService) SubscribeTraffic() (ch chan []byte, unsub func()) {
	ch = make(chan []byte, 4)
	s.trafficSubsMu.Lock()
	s.trafficSubs[ch] = struct{}{}
	s.trafficSubsMu.Unlock()
	unsub = func() {
		s.trafficSubsMu.Lock()
		delete(s.trafficSubs, ch)
		s.trafficSubsMu.Unlock()
	}
	return
}

// broadcastTraffic рассылает raw JSON-снимок трафика всем подписчикам WebSocket.
func (s *TrafficQuotaService) broadcastTraffic(raw []byte) {
	s.trafficSubsMu.RLock()
	defer s.trafficSubsMu.RUnlock()
	for ch := range s.trafficSubs {
		select {
		case ch <- raw:
		default: // медленный клиент — пропускаем
		}
	}
}

func (s *TrafficQuotaService) trafficWSLoop() {
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
		err := s.streamTraffic()
		if err == nil {
			// Graceful shutdown via stopCh.
			return
		}

		// If the session ran for more than 30 s it was healthy — reset backoff.
		if time.Since(start) > 30*time.Second {
			backoff = 5 * time.Second
		}

		log.Printf("TrafficQuota: WS traffic stream ended: %v — retry in %s", err, backoff)

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

type mihomoTraffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

func (s *TrafficQuotaService) streamTraffic() error {
	wsURL := httpToWS(strings.TrimRight(s.mihomoURL, "/")) + "/traffic"

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

	done := make(chan struct{})
	defer close(done)

	// Close the WebSocket when the service stops so ReadMessage unblocks.
	go func() {
		select {
		case <-s.stopCh:
			conn.Close()
		case <-done:
		}
	}()

	log.Printf("TrafficQuota: WebSocket traffic connected to %s", wsURL)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-s.stopCh:
				return nil // graceful shutdown
			default:
				return fmt.Errorf("read: %w", err)
			}
		}
		var payload mihomoTraffic
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		s.processTrafficSnapshot(payload.Up, payload.Down)
	}
}

func (s *TrafficQuotaService) processTrafficSnapshot(up, down int64) {
	s.mu.Lock()

	now := time.Now()
	currentHourStart := now.Truncate(time.Hour).Unix()
	currentDayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()

	offset := int(now.Weekday() - time.Monday)
	if offset < 0 {
		offset += 7
	}
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, now.Location()).Unix()

	// Проверяем календарные интервалы
	if s.peaks.HourStart != currentHourStart {
		s.peaks.PeakHourUp = 0
		s.peaks.PeakHourDown = 0
		s.peaks.HourStart = currentHourStart
	}
	if s.peaks.DayStart != currentDayStart {
		s.peaks.PeakDayUp = 0
		s.peaks.PeakDayDown = 0
		s.peaks.DayStart = currentDayStart
	}
	if s.peaks.WeekStart != currentWeekStart {
		s.peaks.PeakWeekUp = 0
		s.peaks.PeakWeekDown = 0
		s.peaks.WeekStart = currentWeekStart
	}

	// Обновляем пики
	if up > s.peaks.PeakHourUp {
		s.peaks.PeakHourUp = up
	}
	if down > s.peaks.PeakHourDown {
		s.peaks.PeakHourDown = down
	}
	if up > s.peaks.PeakDayUp {
		s.peaks.PeakDayUp = up
	}
	if down > s.peaks.PeakDayDown {
		s.peaks.PeakDayDown = down
	}
	if up > s.peaks.PeakWeekUp {
		s.peaks.PeakWeekUp = up
	}
	if down > s.peaks.PeakWeekDown {
		s.peaks.PeakWeekDown = down
	}

	conns := s.activeConnsCount
	tcp := s.tcpConnsCount
	udp := s.udpConnsCount
	peaksCopy := s.peaks

	_ = s.saveLocked(false)
	s.mu.Unlock()

	payload := map[string]interface{}{
		"up":              up,
		"down":            down,
		"connections":     conns,
		"tcp_connections": tcp,
		"udp_connections": udp,
		"peaks":           peaksCopy,
	}
	raw, err := json.Marshal(payload)
	if err == nil {
		s.broadcastTraffic(raw)
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
			// Reset on calendar week boundary (Monday 00:00).
			lastOffset := int(lastReset.Weekday() - time.Monday)
			if lastOffset < 0 {
				lastOffset += 7
			}
			lastWeekStart := time.Date(lastReset.Year(), lastReset.Month(), lastReset.Day()-lastOffset, 0, 0, 0, 0, lastReset.Location()).Unix()

			nowOffset := int(now.Weekday() - time.Monday)
			if nowOffset < 0 {
				nowOffset += 7
			}
			nowWeekStart := time.Date(now.Year(), now.Month(), now.Day()-nowOffset, 0, 0, 0, 0, now.Location()).Unix()

			shouldReset = lastWeekStart != nowWeekStart
		case "monthly":
			shouldReset = lastReset.Year() != now.Year() || lastReset.Month() != now.Month()
		}

		if shouldReset {
			q.CurrentBytes = 0
			q.LastReset = now.Unix()
			changed = true
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

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

type mihomoProxy struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

func (s *TrafficQuotaService) getMihomoProxies() (map[string]mihomoProxy, error) {
	url := fmt.Sprintf("%s/proxies", strings.TrimRight(s.mihomoURL, "/"))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if s.secret != "" {
		req.Header.Set("Authorization", "Bearer "+s.secret)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mihomo API returned %d", resp.StatusCode)
	}

	var payload struct {
		Proxies map[string]mihomoProxy `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Proxies, nil
}

func (s *TrafficQuotaService) applyProxyToGroup(groupName, proxyName string) error {
	url := fmt.Sprintf("%s/proxies/%s", strings.TrimRight(s.mihomoURL, "/"), groupName)
	bodyMap := map[string]string{"name": proxyName}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.secret != "" {
		req.Header.Set("Authorization", "Bearer "+s.secret)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mihomo API returned %d", resp.StatusCode)
	}
	return nil
}

func (s *TrafficQuotaService) checkQuotas() {
	s.mu.RLock()
	quotasCopy := make([]TrafficQuota, len(s.quotas))
	copy(quotasCopy, s.quotas)

	proxyStatsCopy := make(map[string]int64)
	for name, stat := range s.proxyStats {
		proxyStatsCopy[name] = stat.TotalBytes
	}
	s.mu.RUnlock()

	mihomoProxies, err := s.getMihomoProxies()
	hasMihomo := err == nil
	if err != nil {
		log.Printf("TrafficQuota: failed to fetch Mihomo proxies: %v", err)
	}

	shouldBlock := make(map[string]string)
	alertsToCreate := make([]struct {
		quotaID  string
		severity string
		message  string
	}, 0)

	for i := range quotasCopy {
		q := &quotasCopy[i]
		if !q.Enabled || q.LimitBytes <= 0 {
			continue
		}

		current := q.CurrentBytes
		percent := float64(current) / float64(q.LimitBytes) * 100

		if percent >= 100 {
			alertsToCreate = append(alertsToCreate, struct {
				quotaID  string
				severity string
				message  string
			}{
				quotaID:  q.ID,
				severity: "critical",
				message:  fmt.Sprintf("Лимит '%s' превышен: %s из %s (%.0f%%)", q.Name, formatBytes(current), formatBytes(q.LimitBytes), percent),
			})

			var fallback string
			if q.Action == "block" {
				fallback = "REJECT"
			} else if q.Action == "redirect_direct" {
				fallback = "DIRECT"
			}

			if fallback != "" && hasMihomo {
				var groupName string
				if q.TargetType == "proxy" {
					groupName = q.TargetID
				} else {
					groupName = "GLOBAL"
				}

				if group, ok := mihomoProxies[groupName]; ok {
					if contains(group.All, fallback) {
						shouldBlock[groupName] = fallback
					} else {
						if globalGroup, ok := mihomoProxies["GLOBAL"]; ok && contains(globalGroup.All, fallback) {
							shouldBlock["GLOBAL"] = fallback
						}
					}
				}
			}
		} else if q.AlertThreshold > 0 && percent >= float64(q.AlertThreshold) {
			alertsToCreate = append(alertsToCreate, struct {
				quotaID  string
				severity string
				message  string
			}{
				quotaID:  q.ID,
				severity: "warning",
				message:  fmt.Sprintf("Лимит '%s' на %.0f%%: %s из %s", q.Name, percent, formatBytes(current), formatBytes(q.LimitBytes)),
			})
		}
	}

	s.mu.Lock()

	var restoreActions []struct {
		groupName     string
		originalProxy string
	}
	if hasMihomo {
		for groupName, originalProxy := range s.blockedProxies {
			if _, needed := shouldBlock[groupName]; !needed {
				restoreActions = append(restoreActions, struct {
					groupName     string
					originalProxy string
				}{groupName, originalProxy})
			}
		}
	}

	var blockActions []struct {
		groupName string
		fallback  string
	}
	if hasMihomo {
		for groupName, fallback := range shouldBlock {
			group, ok := mihomoProxies[groupName]
			if !ok {
				continue
			}
			if group.Now != fallback {
				if _, saved := s.blockedProxies[groupName]; !saved {
					if group.Now != "DIRECT" && group.Now != "REJECT" {
						s.blockedProxies[groupName] = group.Now
					}
				}
				blockActions = append(blockActions, struct {
					groupName string
					fallback  string
				}{groupName, fallback})
			}
		}
	}

	s.mu.Unlock()

	successfulRestores := make([]string, 0)
	if hasMihomo {
		for _, action := range restoreActions {
			if err := s.applyProxyToGroup(action.groupName, action.originalProxy); err != nil {
				log.Printf("TrafficQuota: failed to restore group %s to %s: %v", action.groupName, action.originalProxy, err)
			} else {
				successfulRestores = append(successfulRestores, action.groupName)
			}
		}

		for _, action := range blockActions {
			if err := s.applyProxyToGroup(action.groupName, action.fallback); err != nil {
				log.Printf("TrafficQuota: failed to block group %s with %s: %v", action.groupName, action.fallback, err)
			}
		}
	}

	s.mu.Lock()
	if hasMihomo {
		for _, groupName := range successfulRestores {
			delete(s.blockedProxies, groupName)
		}
	}

	for _, alert := range alertsToCreate {
		var quotaPtr *TrafficQuota
		for i := range s.quotas {
			if s.quotas[i].ID == alert.quotaID {
				quotaPtr = &s.quotas[i]
				break
			}
		}
		if quotaPtr != nil {
			s.addAlert(quotaPtr, alert.severity, alert.message)
		}
	}

	s.mu.Unlock()
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
