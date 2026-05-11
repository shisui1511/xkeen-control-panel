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
)

// Profile represents a time-based proxy switching profile
type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	// Schedule
	DaysOfWeek  []int  `json:"days_of_week"` // 0=Sunday, 1=Monday, ... 6=Saturday
	StartTime   string `json:"start_time"`   // HH:MM format
	EndTime     string `json:"end_time"`     // HH:MM format
	// Target
	GroupName   string `json:"group_name"`   // Mihomo proxy group name
	ProxyName   string `json:"proxy_name"`   // Proxy to select
	// Metadata
	LastApplied int64  `json:"last_applied"` // Unix timestamp
	ApplyCount  int    `json:"apply_count"`
}

// SmartProxyService manages time-based proxy profiles
type SmartProxyService struct {
	dataDir    string
	profiles   []Profile
	mu         sync.RWMutex
	mihomoURL  string
	stopCh     chan struct{}
	wg         sync.WaitGroup
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

func (s *SmartProxyService) save() error {
	s.mu.RLock()
	store := ProfileStore{Profiles: s.profiles}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	os.MkdirAll(s.dataDir, 0755)
	return os.WriteFile(s.storePath(), data, 0644)
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
	s.profiles = append(s.profiles, *p)
	s.mu.Unlock()
	return s.save()
}

func (s *SmartProxyService) Update(id string, p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles[i] = *p
			s.profiles[i].ID = id
			return s.save()
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
			return s.save()
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
			return s.save()
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

	now := time.Now()
	currentDay := int(now.Weekday()) // 0=Sunday
	currentTime := now.Format("15:04")

	for _, p := range profiles {
		if !p.Enabled {
			continue
		}

		// Check day of week
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

		// Check time range
		if currentTime < p.StartTime || currentTime > p.EndTime {
			continue
		}

		// Apply profile
		if err := s.applyProfile(&p); err != nil {
			log.Printf("SmartProxy: failed to apply profile %s: %v", p.Name, err)
		}
	}
}

func (s *SmartProxyService) applyProfile(p *Profile) error {
	// Don't re-apply if already applied in the last 5 minutes
	if time.Now().Unix()-p.LastApplied < 300 {
		return nil
	}

	url := fmt.Sprintf("%s/proxies/%s", s.mihomoURL, p.GroupName)
	bodyMap := map[string]string{"name": p.ProxyName}
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

	// Update profile metadata
	s.mu.Lock()
	for i := range s.profiles {
		if s.profiles[i].ID == p.ID {
			s.profiles[i].LastApplied = time.Now().Unix()
			s.profiles[i].ApplyCount++
			break
		}
	}
	s.mu.Unlock()
	if err := s.save(); err != nil {
		log.Printf("SmartProxy: failed to save profile metadata: %v", err)
	}

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
