package services

import (
	"bytes"
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

// ProfileMode defines how a profile operates
type ProfileMode string

const (
	ModeTimeBased  ProfileMode = "time-based"
	ModeFailover   ProfileMode = "auto-failover"
	ModeRoundRobin ProfileMode = "round-robin"
	ModeGeo        ProfileMode = "geo-aware"
)

// Profile represents a proxy switching profile (time-based)
type Profile struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Enabled      bool        `json:"enabled"`
	Mode         ProfileMode `json:"mode"` // "time-based"
	Schedule     [][]bool    `json:"schedule"` // 7x24 grid: [day][hour]
	GroupName    string      `json:"group_name"` // Mihomo proxy group name
	ProxyName    string      `json:"proxy_name"` // Primary proxy to select
	LastApplied  int64       `json:"last_applied"` // Unix timestamp
	ApplyCount   int         `json:"apply_count"`
	CurrentProxy string      `json:"current_proxy"`
}

// SmartProxyService manages time-based proxy profiles
type SmartProxyService struct {
	dataDir    string
	profiles   []Profile
	mu         sync.RWMutex
	mihomoURL  string
	stopCh     chan struct{}
	wg         sync.WaitGroup
	httpClient *http.Client
}

// ProfileStore is the on-disk format
type ProfileStore struct {
	Profiles []Profile `json:"profiles"`
}

func NewSmartProxyService(dataDir, mihomoURL string) *SmartProxyService {
	svc := &SmartProxyService{
		dataDir:    dataDir,
		profiles:   []Profile{},
		mihomoURL:  mihomoURL,
		stopCh:     make(chan struct{}),
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
	dir := filepath.Join(s.dataDir, "smartproxy")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "profiles.json")
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

	return utils.AtomicWriteFile(s.storePath(), data, 0600)
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
			copy := s.profiles[i]
			return &copy
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

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("SmartProxy: schedulerLoop panic: %v — restarting in 5s", r)
				}
			}()

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
		}()

		// Check if we should stop before restarting after a panic
		select {
		case <-s.stopCh:
			return
		default:
		}

		time.Sleep(5 * time.Second)
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

		if p.Mode == ModeTimeBased {
			s.evaluateTimeBased(&p)
		}
	}
}

func (s *SmartProxyService) evaluateTimeBased(p *Profile) {
	now := time.Now()
	day := int(now.Weekday())
	hour := now.Hour()

	if day >= len(p.Schedule) || hour >= len(p.Schedule[day]) || !p.Schedule[day][hour] {
		return
	}

	if err := s.applyProfile(p); err != nil {
		log.Printf("SmartProxy: failed to apply profile %s: %v", p.Name, err)
	}
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
	currentHour := now.Hour()

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

		if p.Mode != ModeTimeBased {
			continue
		}

		isActive := false
		if currentDay < len(p.Schedule) && currentHour < len(p.Schedule[currentDay]) {
			isActive = p.Schedule[currentDay][currentHour]
		}

		if isActive {
			activeProfiles = append(activeProfiles, p)
		} else {
			// Find if there is an active slot later today
			hasFutureSlot := false
			if currentDay < len(p.Schedule) {
				for h := currentHour + 1; h < len(p.Schedule[currentDay]); h++ {
					if p.Schedule[currentDay][h] {
						hasFutureSlot = true
						break
					}
				}
			}
			if hasFutureSlot {
				nextProfiles = append(nextProfiles, p)
			}
		}
	}

	return map[string]interface{}{
		"active": activeProfiles,
		"next":   nextProfiles,
		"time":   currentTime,
		"day":    currentDay,
	}
}
