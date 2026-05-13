package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

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

// TrafficQuotaService manages traffic accounting and quotas
type TrafficQuotaService struct {
	dataDir    string
	mihomoURL  string
	quotas     []TrafficQuota
	proxyStats map[string]*ProxyTraffic
	alerts     []TrafficAlert
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
	httpClient *http.Client

	// connectionTracker maps connection ID -> last seen {upload, download}
	connectionTracker sync.Map
}

func NewTrafficQuotaService(dataDir, mihomoURL string) *TrafficQuotaService {
	svc := &TrafficQuotaService{
		dataDir:    dataDir,
		mihomoURL:  mihomoURL,
		quotas:     []TrafficQuota{},
		proxyStats: make(map[string]*ProxyTraffic),
		alerts:     []TrafficAlert{},
		stopCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 10 * time.Second},
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

// saveLocked writes state to disk. Caller MUST hold s.mu.
func (s *TrafficQuotaService) saveLocked() error {
	store := TrafficStore{
		Quotas:     s.quotas,
		ProxyStats: s.proxyStats,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(s.storePath(), data, 0644)
}

// --- CRUD for quotas ---

func (s *TrafficQuotaService) ListQuotas() []TrafficQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]TrafficQuota, len(s.quotas))
	copy(result, s.quotas)
	return result
}

func (s *TrafficQuotaService) GetQuota(id string) *TrafficQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			return &s.quotas[i]
		}
	}
	return nil
}

func (s *TrafficQuotaService) AddQuota(q *TrafficQuota) error {
	if q.ID == "" {
		q.ID = fmt.Sprintf("quota_%d", time.Now().UnixNano())
	}
	q.LastReset = time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotas = append(s.quotas, *q)
	return s.saveLocked()
}

func (s *TrafficQuotaService) UpdateQuota(id string, q *TrafficQuota) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.quotas {
		if s.quotas[i].ID == id {
			s.quotas[i] = *q
			s.quotas[i].ID = id
			return s.saveLocked()
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
			return s.saveLocked()
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
			return s.saveLocked()
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
			return s.saveLocked()
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

func (s *TrafficQuotaService) collectorLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Reset daily/weekly/monthly quotas if needed
	s.checkResets()

	for {
		select {
		case <-ticker.C:
			s.checkResets()
			s.collectTraffic()
			s.checkQuotas()
		case <-s.stopCh:
			return
		}
	}
}

func (s *TrafficQuotaService) checkResets() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

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
			_, lastWeek := lastReset.ISOWeek()
			_, nowWeek := now.ISOWeek()
			shouldReset = lastWeek != nowWeek
		case "monthly":
			shouldReset = lastReset.Year() != now.Year() || lastReset.Month() != now.Month()
		}

		if shouldReset {
			q.CurrentBytes = 0
			q.LastReset = now.Unix()
		}
	}

	_ = s.saveLocked()
}

// checkQuotas checks all quotas against current stats

// connStats holds last seen bytes for a connection
type connStats struct {
	Upload   int64
	Download int64
}

func (s *TrafficQuotaService) collectTraffic() {
	resp, err := s.httpClient.Get(s.mihomoURL + "/connections")
	if err != nil {
		log.Printf("TrafficQuota: failed to fetch connections: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("TrafficQuota: connections API returned %d", resp.StatusCode)
		return
	}

	var data struct {
		Connections []struct {
			ID       string   `json:"id"`
			Chains   []string `json:"chains"`
			Upload   int64    `json:"upload"`
			Download int64    `json:"download"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("TrafficQuota: failed to decode connections: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	activeIDs := make(map[string]bool)

	for _, conn := range data.Connections {
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
			// New connection, use current as delta
			deltaUp = conn.Upload
			deltaDown = conn.Download
		}

		// Only add positive deltas (Mihomo stats might reset if reconnected with same ID? unlikely but safe)
		if deltaUp < 0 { deltaUp = 0 }
		if deltaDown < 0 { deltaDown = 0 }

		if deltaUp > 0 || deltaDown > 0 {
			stat, ok := s.proxyStats[proxyName]
			if !ok {
				stat = &ProxyTraffic{ProxyName: proxyName}
				s.proxyStats[proxyName] = stat
			}
			stat.UploadBytes += deltaUp
			stat.DownloadBytes += deltaDown
			stat.TotalBytes = stat.UploadBytes + stat.DownloadBytes

			// Update tracker
			s.connectionTracker.Store(conn.ID, connStats{Upload: conn.Upload, Download: conn.Download})
		}
	}

	// Clean up stale connections from tracker
	s.connectionTracker.Range(func(key, value interface{}) bool {
		id := key.(string)
		if !activeIDs[id] {
			s.connectionTracker.Delete(id)
		}
		return true
	})

	if err := s.saveLocked(); err != nil {
		log.Printf("TrafficQuota: failed to save stats: %v", err)
	}
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
