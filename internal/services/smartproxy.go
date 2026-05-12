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
)

// ProfileMode defines how a profile operates
type ProfileMode string

const (
	ModeTimeBased  ProfileMode = "time-based"
	ModeFailover   ProfileMode = "auto-failover"
	ModeRoundRobin ProfileMode = "round-robin"
	ModeGeo        ProfileMode = "geo-aware"
)

// Profile represents a proxy switching profile (time-based, auto-failover or round-robin)
type Profile struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Enabled bool        `json:"enabled"`
	Mode    ProfileMode `json:"mode"` // "time-based" | "auto-failover" | "round-robin"

	// Time-based fields
	DaysOfWeek []int  `json:"days_of_week"` // 0=Sunday, 1=Monday, ... 6=Saturday
	StartTime  string `json:"start_time"`   // HH:MM format
	EndTime    string `json:"end_time"`     // HH:MM format

	// Auto-failover fields
	LatencyThreshold    int    `json:"latency_threshold"`    // ms
	ConsecutiveFailures int    `json:"consecutive_failures"` // count
	FallbackProxy       string `json:"fallback_proxy"`       // proxy name

	// Round-robin fields
	RoundRobinProxies []string `json:"round_robin_proxies"`
	RoundRobinIndex   int      `json:"round_robin_index"`

	// Geo-aware fields
	GeoRegion     string `json:"geo_region"`      // e.g. "asia", "europe", "americas"
	GeoAutoSelect bool   `json:"geo_auto_select"` // auto-select best proxy in region

	// Target
	GroupName string `json:"group_name"` // Mihomo proxy group name
	ProxyName string `json:"proxy_name"` // Primary proxy to select

	// Metadata
	LastApplied int64 `json:"last_applied"` // Unix timestamp
	ApplyCount  int   `json:"apply_count"`

	// Failover tracking (runtime)
	CurrentFailures int    `json:"current_failures"`
	CurrentProxy    string `json:"current_proxy"`
}

// SmartProxyService manages time-based proxy profiles
type SmartProxyService struct {
	dataDir   string
	profiles  []Profile
	mu        sync.RWMutex
	mihomoURL string
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// ProfileStore is the on-disk format
type ProfileStore struct {
	Profiles []Profile `json:"profiles"`
}

func NewSmartProxyService(dataDir, mihomoURL string) *SmartProxyService {
	svc := &SmartProxyService{
		dataDir:   dataDir,
		profiles:  []Profile{},
		mihomoURL: mihomoURL,
		stopCh:    make(chan struct{}),
	}
	svc.load()
	return svc
}

func (s *SmartProxyService) Start() {
	s.wg.Add(1)
	go s.schedulerLoop()
}

func (s *SmartProxyService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *SmartProxyService) storePath() string {
	return filepath.Join(s.dataDir, "profiles.json")
}

func (s *SmartProxyService) load() {
	path := s.storePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return // File doesn't exist yet
	}
	var store ProfileStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}
	s.mu.Lock()
	s.profiles = store.Profiles
	s.mu.Unlock()
}

func (s *SmartProxyService) saveLocked() error {
	store := ProfileStore{Profiles: s.profiles}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	os.MkdirAll(s.dataDir, 0755)
	return os.WriteFile(s.storePath(), data, 0644)
}

func (s *SmartProxyService) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// CRUD operations

func (s *SmartProxyService) List() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Profile, len(s.profiles))
	copy(result, s.profiles)
	return result
}

func (s *SmartProxyService) Get(id string) *Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			return &s.profiles[i]
		}
	}
	return nil
}

func (s *SmartProxyService) Add(p *Profile) error {
	if p.ID == "" {
		p.ID = fmt.Sprintf("profile_%d", time.Now().UnixNano())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = append(s.profiles, *p)
	return s.saveLocked()
}

func (s *SmartProxyService) Update(id string, p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles[i] = *p
			s.profiles[i].ID = id
			return s.saveLocked()
		}
	}
	return fmt.Errorf("profile not found")
}

func (s *SmartProxyService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.profiles {
		if p.ID == id {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("profile not found")
}

func (s *SmartProxyService) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles[i].Enabled = enabled
			return s.saveLocked()
		}
	}
	return fmt.Errorf("profile not found")
}

// Scheduler

func (s *SmartProxyService) schedulerLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run immediately on start
	s.evaluateProfiles()

	for {
		select {
		case <-ticker.C:
			s.evaluateProfiles()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SmartProxyService) evaluateProfiles() {
	s.mu.RLock()
	profiles := make([]Profile, len(s.profiles))
	copy(profiles, s.profiles)
	s.mu.RUnlock()

	for _, p := range profiles {
		if !p.Enabled {
			continue
		}

		switch p.Mode {
		case ModeFailover:
			s.evaluateFailover(&p)
		case ModeRoundRobin:
			s.evaluateRoundRobin(&p)
		case ModeGeo:
			s.evaluateGeo(&p)
		default: // ModeTimeBased
			s.evaluateTimeBased(&p)
		}
	}
}

func (s *SmartProxyService) evaluateTimeBased(p *Profile) {
	now := time.Now()
	currentDay := int(now.Weekday()) // 0=Sunday
	currentTime := now.Format("15:04")

	// Check day of week
	dayMatch := false
	for _, d := range p.DaysOfWeek {
		if d == currentDay {
			dayMatch = true
			break
		}
	}
	if !dayMatch {
		return
	}

	// Check time range
	if currentTime < p.StartTime || currentTime > p.EndTime {
		return
	}

	// Apply profile
	if err := s.applyProfile(p); err != nil {
		log.Printf("SmartProxy: failed to apply profile %s: %v", p.Name, err)
	}
}

// mihomoDelayResponse matches Mihomo delay endpoint response
type mihomoDelayResponse struct {
	Delay int `json:"delay"`
}

func (s *SmartProxyService) testProxyLatency(proxyName string) (int, error) {
	url := fmt.Sprintf("%s/proxies/%s/delay?url=http://www.gstatic.com/generate_204&timeout=5000",
		s.mihomoURL, proxyName)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("delay API returned %d", resp.StatusCode)
	}

	var data mihomoDelayResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return -1, err
	}
	return data.Delay, nil
}

func (s *SmartProxyService) evaluateFailover(p *Profile) {
	// Determine which proxy to test
	testProxy := p.ProxyName
	if p.CurrentProxy != "" {
		testProxy = p.CurrentProxy
	}

	delay, err := s.testProxyLatency(testProxy)
	if err != nil {
		log.Printf("SmartProxy: latency test failed for %s: %v", testProxy, err)
		delay = 9999 // treat as high latency
	}

	s.mu.Lock()
	for i := range s.profiles {
		if s.profiles[i].ID != p.ID {
			continue
		}

		if delay > p.LatencyThreshold {
			s.profiles[i].CurrentFailures++
			log.Printf("SmartProxy: profile %s proxy %s latency %dms > threshold %dms (failures: %d/%d)",
				p.Name, testProxy, delay, p.LatencyThreshold, s.profiles[i].CurrentFailures, p.ConsecutiveFailures)
		} else {
			if s.profiles[i].CurrentFailures > 0 {
				log.Printf("SmartProxy: profile %s proxy %s latency %dms OK, resetting failures", p.Name, testProxy, delay)
			}
			s.profiles[i].CurrentFailures = 0
		}

		// Check if we need to failover
		if s.profiles[i].CurrentFailures >= p.ConsecutiveFailures {
			// Determine fallback target
			var target string
			if s.profiles[i].CurrentProxy == p.ProxyName && p.FallbackProxy != "" {
				target = p.FallbackProxy
				log.Printf("SmartProxy: profile %s failing over %s → %s", p.Name, testProxy, target)
			} else if s.profiles[i].CurrentProxy != "" {
				// Already on fallback, try DIRECT as emergency
				target = "DIRECT"
				log.Printf("SmartProxy: profile %s emergency fallback → DIRECT", p.Name)
			} else {
				target = p.FallbackProxy
				log.Printf("SmartProxy: profile %s failing over %s → %s", p.Name, testProxy, target)
			}

			s.profiles[i].CurrentProxy = target
			s.profiles[i].CurrentFailures = 0
			s.mu.Unlock()

			// Apply failover
			if err := s.applyProxyToGroup(p.GroupName, target); err != nil {
				log.Printf("SmartProxy: failover apply failed: %v", err)
			} else {
				s.updateProfileApplied(p.ID)
			}
			return
		}

		// If latency is OK and we're on fallback, try switching back to primary
		if s.profiles[i].CurrentProxy != "" && s.profiles[i].CurrentProxy != p.ProxyName && delay <= p.LatencyThreshold {
			log.Printf("SmartProxy: profile %s recovering %s → %s", p.Name, s.profiles[i].CurrentProxy, p.ProxyName)
			s.profiles[i].CurrentProxy = ""
			s.mu.Unlock()
			if err := s.applyProxyToGroup(p.GroupName, p.ProxyName); err != nil {
				log.Printf("SmartProxy: recovery apply failed: %v", err)
			} else {
				s.updateProfileApplied(p.ID)
			}
			return
		}

		break
	}
	s.mu.Unlock()
}

func (s *SmartProxyService) evaluateRoundRobin(p *Profile) {
	s.mu.Lock()
	for i := range s.profiles {
		if s.profiles[i].ID != p.ID {
			continue
		}

		if len(s.profiles[i].RoundRobinProxies) == 0 {
			s.mu.Unlock()
			return
		}

		idx := s.profiles[i].RoundRobinIndex % len(s.profiles[i].RoundRobinProxies)
		target := s.profiles[i].RoundRobinProxies[idx]
		s.profiles[i].RoundRobinIndex = (s.profiles[i].RoundRobinIndex + 1) % len(s.profiles[i].RoundRobinProxies)
		s.mu.Unlock()

		log.Printf("SmartProxy: round-robin profile %s switching to %s", p.Name, target)
		if err := s.applyProxyToGroup(p.GroupName, target); err != nil {
			log.Printf("SmartProxy: round-robin apply failed: %v", err)
		} else {
			s.updateProfileApplied(p.ID)
		}
		return
	}
	s.mu.Unlock()
}

func (s *SmartProxyService) evaluateGeo(p *Profile) {
	if !p.GeoAutoSelect {
		return
	}

	// Get all proxies from Mihomo
	proxies, err := s.fetchProxiesList()
	if err != nil {
		log.Printf("SmartProxy: geo profile %s failed to fetch proxies: %v", p.Name, err)
		return
	}

	// Filter proxies by region (using naming convention: proxy name contains region)
	var candidates []string
	for _, proxyName := range proxies {
		if strings.Contains(strings.ToLower(proxyName), strings.ToLower(p.GeoRegion)) {
			candidates = append(candidates, proxyName)
		}
	}

	if len(candidates) == 0 {
		log.Printf("SmartProxy: geo profile %s no proxies found for region %s", p.Name, p.GeoRegion)
		return
	}

	// Test latency for all candidates and pick the best
	bestProxy := ""
	bestDelay := 999999
	for _, candidate := range candidates {
		delay, err := s.testProxyLatency(candidate)
		if err != nil {
			continue
		}
		if delay < bestDelay && delay > 0 {
			bestDelay = delay
			bestProxy = candidate
		}
	}

	if bestProxy == "" {
		log.Printf("SmartProxy: geo profile %s all proxies in region %s are down", p.Name, p.GeoRegion)
		return
	}

	log.Printf("SmartProxy: geo profile %s selected %s (%dms) in region %s", p.Name, bestProxy, bestDelay, p.GeoRegion)
	if err := s.applyProxyToGroup(p.GroupName, bestProxy); err != nil {
		log.Printf("SmartProxy: geo apply failed: %v", err)
	} else {
		s.updateProfileApplied(p.ID)
	}
}

func (s *SmartProxyService) fetchProxiesList() ([]string, error) {
	resp, err := http.Get(s.mihomoURL + "/proxies")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxies API returned %d", resp.StatusCode)
	}

	var data struct {
		Proxies map[string]interface{} `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var names []string
	for name, p := range data.Proxies {
		if proxy, ok := p.(map[string]interface{}); ok {
			if t, ok := proxy["type"].(string); ok && t != "Selector" && t != "URLTest" && t != "Fallback" && t != "LoadBalance" && t != "Direct" && t != "Reject" {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

func (s *SmartProxyService) applyProxyToGroup(groupName, proxyName string) error {
	url := fmt.Sprintf("%s/proxies/%s", s.mihomoURL, groupName)
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

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mihomo API returned %d", resp.StatusCode)
	}
	return nil
}

func (s *SmartProxyService) updateProfileApplied(id string) {
	s.mu.Lock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles[i].LastApplied = time.Now().Unix()
			s.profiles[i].ApplyCount++
			break
		}
	}
	s.mu.Unlock()
	if err := s.save(); err != nil {
		log.Printf("SmartProxy: failed to save profile metadata: %v", err)
	}
}

func (s *SmartProxyService) applyProfile(p *Profile) error {
	// Don't re-apply if already applied in the last 5 minutes
	if time.Now().Unix()-p.LastApplied < 300 {
		return nil
	}

	proxyName := p.ProxyName
	if p.CurrentProxy != "" {
		proxyName = p.CurrentProxy
	}

	if err := s.applyProxyToGroup(p.GroupName, proxyName); err != nil {
		return err
	}

	s.updateProfileApplied(p.ID)
	return nil
}

// CurrentStatus returns which profiles are active right now
func (s *SmartProxyService) CurrentStatus() map[string]interface{} {
	now := time.Now()
	currentDay := int(now.Weekday())
	currentTime := now.Format("15:04")

	s.mu.RLock()
	profiles := make([]Profile, len(s.profiles))
	copy(profiles, s.profiles)
	s.mu.RUnlock()

	var activeProfiles []Profile
	var nextProfiles []Profile

	for _, p := range profiles {
		if !p.Enabled {
			continue
		}

		dayMatch := false
		for _, d := range p.DaysOfWeek {
			if d == currentDay {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			continue
		}

		if currentTime >= p.StartTime && currentTime <= p.EndTime {
			activeProfiles = append(activeProfiles, p)
		} else if currentTime < p.StartTime {
			nextProfiles = append(nextProfiles, p)
		}
	}

	return map[string]interface{}{
		"active": activeProfiles,
		"next":   nextProfiles,
		"time":   currentTime,
		"day":    currentDay,
	}
}
