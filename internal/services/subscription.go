package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// maxSubscriptionBytes caps the download size to 10 MB
const maxSubscriptionBytes = 10 * 1024 * 1024

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
	httpClient    *http.Client
}

func NewSubscriptionService(dataDir, configDir string) *SubscriptionService {
	svc := &SubscriptionService{
		dataDir:    dataDir,
		configDir:  configDir,
		httpClient: utils.SafeHTTPClient(30 * time.Second),
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
	return utils.AtomicWriteFile(path, data, 0600)
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
			copy := s.subscriptions[i]
			return &copy
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
	// C-6: validate URL scheme
	parsed, err := url.Parse(subURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("only http and https URLs are allowed for subscriptions")
	}

	resp, err := s.httpClient.Get(subURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// C-4: cap download size to 10 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes))
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

	// Count non-empty lines and enforce 500-entry limit
	lines := strings.Split(content, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 500 {
		return nil, fmt.Errorf("subscription too large: %d entries (max 500)", nonEmpty)
	}

	// Parse share links (one per line)
	var outbounds []Outbound
	for _, line := range lines {
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
		if sub.FilterTransport != "" {
			transport := ""
			if ob.StreamSettings != nil {
				if net, ok := ob.StreamSettings["network"].(string); ok {
					transport = net
				}
			}
			if !strings.EqualFold(transport, sub.FilterTransport) {
				continue
			}
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

	// Add tag prefix and deduplicate tags
	seen := make(map[string]int)
	for i := range outbounds {
		if sub.TagPrefix != "" {
			outbounds[i].Tag = fmt.Sprintf("%s-%s", sub.TagPrefix, outbounds[i].Tag)
		}
		tag := outbounds[i].Tag
		if count, exists := seen[tag]; exists {
			outbounds[i].Tag = fmt.Sprintf("%s-%d", tag, count)
			seen[tag]++
		} else {
			seen[tag] = 1
		}
	}

	data, err := json.MarshalIndent(outbounds, "", "  ")
	if err != nil {
		return err
	}

	return utils.AtomicWriteFile(path, data, 0600)
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

	// hy2:// (Hysteria2)
	if strings.HasPrefix(link, "hy2://") {
		return parseHysteria2Link(link)
	}

	// tuic:// (TUIC)
	if strings.HasPrefix(link, "tuic://") {
		return parseTUICLink(link)
	}

	return nil
}

func parseVMessLink(link string) *Outbound {
	// vmess://base64(json) — some clients use URL-safe base64 without padding
	b64 := strings.TrimPrefix(link, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Try URL-safe base64 with padding
		padded := b64
		if rem := len(padded) % 4; rem != 0 {
			padded += strings.Repeat("=", 4-rem)
		}
		var err2 error
		data, err2 = base64.URLEncoding.DecodeString(padded)
		if err2 != nil {
			// Try raw URL-safe base64 (no padding required)
			data, err2 = base64.RawURLEncoding.DecodeString(b64)
			if err2 != nil {
				return nil
			}
		}
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
		Sni  string `json:"sni"`
	}

	if err := json.Unmarshal(data, &vmess); err != nil {
		return nil
	}

	portInt, err := strconv.Atoi(vmess.Port)
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	// Build StreamSettings from VMess JSON fields
	streamSettings := map[string]interface{}{}
	if vmess.Net != "" {
		streamSettings["network"] = vmess.Net
	}
	switch vmess.Net {
	case "ws":
		wsSettings := map[string]interface{}{}
		if vmess.Path != "" {
			wsSettings["path"] = vmess.Path
		}
		if vmess.Host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": vmess.Host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	case "grpc":
		if vmess.Path != "" {
			streamSettings["grpcSettings"] = map[string]interface{}{"serviceName": vmess.Path}
		}
	}
	if vmess.TLS == "tls" {
		tlsSettings := map[string]interface{}{}
		sni := vmess.Sni
		if sni == "" {
			sni = vmess.Host
		}
		if sni != "" {
			tlsSettings["serverName"] = sni
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	ob := &Outbound{
		Tag:      vmess.PS,
		Protocol: "vmess",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": vmess.Add,
					"port":    portInt,
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
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
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

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build user entry
	user := map[string]interface{}{
		"id":         id,
		"encryption": "none",
	}
	// flow parameter
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}

	// Build StreamSettings from query params; unknown keys are silently ignored
	streamSettings := map[string]interface{}{}
	network := q.Get("type")
	if network != "" {
		streamSettings["network"] = network
	}
	security := q.Get("security")
	if security != "" {
		streamSettings["security"] = security
	}

	switch security {
	case "reality":
		realitySettings := map[string]interface{}{}
		if pbk := q.Get("pbk"); pbk != "" {
			realitySettings["publicKey"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			realitySettings["shortId"] = sid
		}
		if sni := q.Get("sni"); sni != "" {
			realitySettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			realitySettings["fingerprint"] = fp
		}
		if len(realitySettings) > 0 {
			streamSettings["realitySettings"] = realitySettings
		}
	case "tls":
		tlsSettings := map[string]interface{}{}
		if sni := q.Get("sni"); sni != "" {
			tlsSettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			tlsSettings["fingerprint"] = fp
		}
		if alpnStr := q.Get("alpn"); alpnStr != "" {
			tlsSettings["alpn"] = strings.Split(alpnStr, ",")
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	// WebSocket settings (network=ws)
	if network == "ws" {
		wsSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			wsSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	}

	ob := &Outbound{
		Tag:      tag,
		Protocol: "vless",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": u.Hostname(),
					"port":    portInt,
					"users":   []map[string]interface{}{user},
				},
			},
		},
	}
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
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

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build StreamSettings from query params; unknown keys are silently ignored
	security := q.Get("security")
	if security == "" {
		security = "tls" // default for trojan
	}
	streamSettings := map[string]interface{}{
		"security": security,
	}
	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		tlsSettings["fingerprint"] = fp
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}
	if len(tlsSettings) > 0 {
		streamSettings["tlsSettings"] = tlsSettings
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "trojan",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"password": password,
				},
			},
		},
		StreamSettings: streamSettings,
	}
}

func parseHysteria2Link(link string) *Outbound {
	// hy2://password@host:port?sni=...&obfs=...&obfs-password=...&insecure=...#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if insecure := q.Get("insecure"); insecure == "1" || insecure == "true" {
		tlsSettings["allowInsecure"] = true
	}

	streamSettings := map[string]interface{}{
		"network":     "tcp",
		"security":    "tls",
		"tlsSettings": tlsSettings,
	}

	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address":  u.Hostname(),
				"port":     portInt,
				"password": password,
			},
		},
	}

	// obfs settings placed in settings; unknown params silently ignored
	if obfs := q.Get("obfs"); obfs != "" {
		obfsMap := map[string]interface{}{"type": obfs}
		if obfsPass := q.Get("obfs-password"); obfsPass != "" {
			obfsMap["password"] = obfsPass
		}
		settings["hysteria2Settings"] = map[string]interface{}{
			"obfs": obfsMap,
		}
	}

	return &Outbound{
		Tag:            tag,
		Protocol:       "hysteria2",
		Settings:       settings,
		StreamSettings: streamSettings,
	}
}

func parseTUICLink(link string) *Outbound {
	// tuic://uuid:password@host:port?sni=...&congestion_control=...&alpn=...#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	uuid := ""
	password := ""
	if u.User != nil {
		uuid = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}

	server := map[string]interface{}{
		"address":  u.Hostname(),
		"port":     portInt,
		"uuid":     uuid,
		"password": password,
	}
	// unknown params silently ignored
	if cc := q.Get("congestion_control"); cc != "" {
		server["congestionControl"] = cc
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "tuic",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
		StreamSettings: map[string]interface{}{
			"network":     "udp",
			"security":    "tls",
			"tlsSettings": tlsSettings,
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

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "shadowsocks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"method":   method,
					"password": password,
				},
			},
		},
	}
}

// isDue returns true if a subscription is overdue for a refresh.
// A subscription is due when it is enabled, has a non-zero interval, and
// the elapsed time since LastUpdate exceeds Interval hours.
func (s *SubscriptionService) isDue(sub *Subscription, now time.Time) bool {
	return sub.Enabled && sub.Interval > 0 && now.Sub(sub.LastUpdate) >= time.Duration(sub.Interval)*time.Hour
}

// checkAndRefreshDue scans all subscriptions and launches a goroutine for
// each one that is due at the given point in time.
func (s *SubscriptionService) checkAndRefreshDue(now time.Time) {
	subs := s.List()
	for _, sub := range subs {
		if s.isDue(&sub, now) {
			go func(id string) {
				_ = s.Refresh(id) // errors are logged inside Refresh
			}(sub.ID)
		}
	}
}

// RunScheduler starts a background loop that refreshes overdue subscriptions
// every checkInterval. It exits cleanly when ctx is cancelled.
func (s *SubscriptionService) RunScheduler(ctx context.Context, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.checkAndRefreshDue(t)
		}
	}
}
