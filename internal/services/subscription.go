package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Subscription represents a proxy subscription
type Subscription struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	TagPrefix  string    `json:"tag_prefix"`
	Interval   int       `json:"interval"` // hours
	LastUpdate time.Time `json:"last_update"`
	Enabled    bool      `json:"enabled"`

	// Filters
	FilterName      string `json:"filter_name,omitempty"`
	FilterType      string `json:"filter_type,omitempty"`
	FilterTransport string `json:"filter_transport,omitempty"`
}

// Outbound represents a parsed proxy outbound
type Outbound struct {
	Tag            string                 `json:"tag"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings map[string]interface{} `json:"streamSettings,omitempty"`
}

// SubscriptionService manages subscriptions
type SubscriptionService struct {
	dataDir       string
	configDir     string
	subscriptions []Subscription
	mu            sync.RWMutex
	ongoing       sync.Map // Map of ID -> struct{}{} to track active refreshes
}

func NewSubscriptionService(dataDir, configDir string) *SubscriptionService {
	svc := &SubscriptionService{
		dataDir:   dataDir,
		configDir: configDir,
	}
	svc.load()
	return svc
}

func (s *SubscriptionService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dataDir, "subscriptions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.subscriptions)
}

func (s *SubscriptionService) save() error {
	// Note: mu must be held by caller or we use a separate locked version
	path := filepath.Join(s.dataDir, "subscriptions.json")
	data, err := json.MarshalIndent(s.subscriptions, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0644)
}

func (s *SubscriptionService) List() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subscriptions
}

func (s *SubscriptionService) Get(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			return &s.subscriptions[i]
		}
	}
	return nil
}

func (s *SubscriptionService) Add(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.ID == "" {
		sub.ID = fmt.Sprintf("sub_%d", time.Now().Unix())
	}
	s.subscriptions = append(s.subscriptions, *sub)
	return s.save()
}

func (s *SubscriptionService) Update(id string, sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			s.subscriptions[i] = *sub
			return s.save()
		}
	}
	return fmt.Errorf("subscription not found")
}

func (s *SubscriptionService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Find subscription
	var sub *Subscription
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			sub = &s.subscriptions[i]
			break
		}
	}
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}

	// Remove from list
	newList := make([]Subscription, 0, len(s.subscriptions)-1)
	for _, s := range s.subscriptions {
		if s.ID != id {
			newList = append(newList, s)
		}
	}
	s.subscriptions = newList

	// Delete managed fragment file
	fragmentPath := s.getFragmentPath(sub)
	os.Remove(fragmentPath)

	return s.save()
}

func (s *SubscriptionService) Refresh(id string) error {
	// Prevent concurrent refreshes for the same ID
	if _, loaded := s.ongoing.LoadOrStore(id, struct{}{}); loaded {
		return fmt.Errorf("refresh already in progress for this subscription")
	}
	defer s.ongoing.Delete(id)

	s.mu.Lock()
	sub := s.GetLocked(id)
	if sub == nil {
		s.mu.Unlock()
		return fmt.Errorf("subscription not found")
	}
	s.mu.Unlock()

	// Download subscription (outside of lock to avoid blocking other operations)
	outbounds, err := s.downloadAndParse(sub.URL)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-get sub in case it was modified
	sub = s.GetLocked(id)
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}

	// Apply filters
	outbounds = s.applyFilters(outbounds, sub)

	// Generate fragment file
	fragmentPath := s.getFragmentPath(sub)
	if err := s.writeFragment(fragmentPath, outbounds, sub); err != nil {
		return err
	}

	// Update last update time
	sub.LastUpdate = time.Now()
	return s.save()
}

func (s *SubscriptionService) GetLocked(id string) *Subscription {
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			return &s.subscriptions[i]
		}
	}
	return nil
}

func (s *SubscriptionService) downloadAndParse(subURL string) ([]Outbound, error) {
	client := utils.SafeHTTPClient(30 * time.Second)
	resp, err := client.Get(subURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	content := string(body)
	content = strings.TrimSpace(content)

	// Try JSON array first
	var jsonOutbounds []Outbound
	if err := json.Unmarshal(body, &jsonOutbounds); err == nil && len(jsonOutbounds) > 0 {
		return jsonOutbounds, nil
	}

	// Try JSON object with outbounds field
	var jsonConfig struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &jsonConfig); err == nil && len(jsonConfig.Outbounds) > 0 {
		return jsonConfig.Outbounds, nil
	}

	// Try base64 encoded list (Standard or URL encoding)
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(content)
	}
	if err == nil {
		content = string(decoded)
	}

	// Parse share links (one per line)
	var outbounds []Outbound
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		outbound := parseShareLink(line)
		if outbound != nil {
			outbounds = append(outbounds, *outbound)
		}
	}

	return outbounds, nil
}

func (s *SubscriptionService) applyFilters(outbounds []Outbound, sub *Subscription) []Outbound {
	if sub.FilterName == "" && sub.FilterType == "" && sub.FilterTransport == "" {
		return outbounds
	}

	var filtered []Outbound
	for _, ob := range outbounds {
		if sub.FilterName != "" && !strings.Contains(strings.ToLower(ob.Tag), strings.ToLower(sub.FilterName)) {
			continue
		}
		if sub.FilterType != "" && !strings.EqualFold(ob.Protocol, sub.FilterType) {
			continue
		}
		filtered = append(filtered, ob)
	}

	return filtered
}

func (s *SubscriptionService) getFragmentPath(sub *Subscription) string {
	return filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s.json", sub.ID))
}

func (s *SubscriptionService) writeFragment(path string, outbounds []Outbound, sub *Subscription) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Add tag prefix
	for i := range outbounds {
		if sub.TagPrefix != "" {
			outbounds[i].Tag = fmt.Sprintf("%s-%s", sub.TagPrefix, outbounds[i].Tag)
		}
	}

	data, err := json.MarshalIndent(outbounds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// parseShareLink parses various share link formats
func parseShareLink(link string) *Outbound {
	// vmess://
	if strings.HasPrefix(link, "vmess://") {
		return parseVMessLink(link)
	}

	// vless://
	if strings.HasPrefix(link, "vless://") {
		return parseVLESSLink(link)
	}

	// trojan://
	if strings.HasPrefix(link, "trojan://") {
		return parseTrojanLink(link)
	}

	// ss:// (Shadowsocks)
	if strings.HasPrefix(link, "ss://") {
		return parseSSLink(link)
	}

	return nil
}

func parseVMessLink(link string) *Outbound {
	// vmess://base64(json)
	b64 := strings.TrimPrefix(link, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}

	var vmess struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
		Aid  string `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
	}

	if err := json.Unmarshal(data, &vmess); err != nil {
		return nil
	}

	port := vmess.Port

	return &Outbound{
		Tag:      vmess.PS,
		Protocol: "vmess",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": vmess.Add,
					"port":    port,
					"users": []map[string]interface{}{
						{
							"id":       vmess.ID,
							"alterId":  vmess.Aid,
							"security": "auto",
						},
					},
				},
			},
		},
	}
}

func parseVLESSLink(link string) *Outbound {
	// vless://id@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	id := ""
	if u.User != nil {
		id = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "vless",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": u.Hostname(),
					"port":    u.Port(),
					"users": []map[string]interface{}{
						{
							"id":         id,
							"encryption": "none",
						},
					},
				},
			},
		},
	}
}

func parseTrojanLink(link string) *Outbound {
	// trojan://password@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// In trojan:// URIs, the password is the entire userinfo (before @), not a "password" field
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "trojan",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     u.Port(),
					"password": password,
				},
			},
		},
	}
}

func parseSSLink(link string) *Outbound {
	// ss://method:password@host:port#tag
	// or ss://base64(method:password)@host:port#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// Try to decode base64 user info
	userInfo := u.User.String()
	decoded, err := base64.StdEncoding.DecodeString(userInfo)
	if err == nil {
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			u.User = url.UserPassword(parts[0], parts[1])
		}
	}

	method := ""
	password := ""
	if u.User != nil {
		method = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "shadowsocks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     u.Port(),
					"method":   method,
					"password": password,
				},
			},
		},
	}
}
