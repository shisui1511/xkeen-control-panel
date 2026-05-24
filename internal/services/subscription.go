package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	// Type defines the format of the subscription: "xray" (default) or "mihomo"
	Type string `json:"type,omitempty"`

	// Filters (Xray only)
	FilterName      string `json:"filter_name,omitempty"`
	FilterType      string `json:"filter_type,omitempty"`
	FilterTransport string `json:"filter_transport,omitempty"`

	ProxyCount int    `json:"proxy_count"`
	LastError  string `json:"last_error,omitempty"`

	Upload    int64 `json:"upload,omitempty"`
	Download  int64 `json:"download,omitempty"`
	Total     int64 `json:"total,omitempty"`
	RuleCount int   `json:"rule_count,omitempty"`
}

// Outbound represents a parsed proxy outbound
type Outbound struct {
	Tag            string                 `json:"tag"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings map[string]interface{} `json:"streamSettings,omitempty"`
}

// backoff constants for failed auto-refreshes
const (
	backoffBase = 5 * time.Minute
	backoffMax  = 4 * time.Hour
)

// retryState tracks exponential backoff per subscription.
type retryState struct {
	failCount int
	nextRetry time.Time
}

// SubscriptionService manages subscriptions
type SubscriptionService struct {
	dataDir         string
	configDir       string // Xray config dir for fragment files
	mihomoConfigDir string // Mihomo config dir for proxy-provider files
	subscriptions   []Subscription
	mu              sync.RWMutex
	ongoing         sync.Map // Map of ID -> struct{}{} to track active refreshes
	retries         sync.Map // ID -> *retryState for exponential backoff
	httpClient      *http.Client
}

func NewSubscriptionService(dataDir, configDir, mihomoConfigDir string) *SubscriptionService {
	svc := &SubscriptionService{
		dataDir:         dataDir,
		configDir:       configDir,
		mihomoConfigDir: mihomoConfigDir,
		httpClient:      utils.SafeHTTPClient(30 * time.Second),
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
	res := make([]Subscription, len(s.subscriptions))
	for i := range s.subscriptions {
		res[i] = s.subscriptions[i]
		res[i].ProxyCount = s.getProxyCount(&res[i])
	}
	return res
}

func (s *SubscriptionService) Get(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			copy := s.subscriptions[i]
			copy.ProxyCount = s.getProxyCount(&copy)
			return &copy
		}
	}
	return nil
}

func (s *SubscriptionService) getProxyCount(sub *Subscription) int {
	if sub.Type == "mihomo" {
		return s.getMihomoProxyCount(sub)
	}
	path := s.getFragmentPath(sub)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var outbounds []Outbound
	if err := json.Unmarshal(data, &outbounds); err != nil {
		var wrapper struct {
			Outbounds []Outbound `json:"outbounds"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 == nil {
			outbounds = wrapper.Outbounds
		} else {
			return 0
		}
	}
	return len(outbounds)
}

// getMihomoProxyCount counts proxy entries in the saved provider YAML
// by counting lines that match "  - name:" or "- name:".
func (s *SubscriptionService) getMihomoProxyCount(sub *Subscription) int {
	data, err := os.ReadFile(s.getMihomoProviderPath(sub))
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- name:") {
			count++
		}
	}
	return count
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

	// Delete managed fragment / provider file
	if sub.Type == "mihomo" {
		os.Remove(s.getMihomoProviderPath(sub))
	} else {
		fragmentPath := s.getFragmentPath(sub)
		os.Remove(fragmentPath)
	}

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
	subCopy := *sub
	s.mu.Unlock()

	var refreshErr error
	if subCopy.Type == "mihomo" {
		refreshErr = s.refreshMihomo(&subCopy)
	} else {
		refreshErr = s.refreshXray(&subCopy)
	}

	// Persist last_error so frontend can show error state
	s.mu.Lock()
	if live := s.GetLocked(id); live != nil {
		if refreshErr != nil {
			live.LastError = refreshErr.Error()
		} else {
			live.LastError = ""
			live.LastUpdate = subCopy.LastUpdate
			live.Upload = subCopy.Upload
			live.Download = subCopy.Download
			live.Total = subCopy.Total
			live.RuleCount = subCopy.RuleCount
		}
		_ = s.save()
	}
	s.mu.Unlock()

	return refreshErr
}

func (s *SubscriptionService) refreshXray(sub *Subscription) error {
	// Download subscription (outside of lock to avoid blocking other operations)
	outbounds, err := s.downloadAndParse(sub.URL, sub)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-get sub in case it was modified
	live := s.GetLocked(sub.ID)
	if live == nil {
		return fmt.Errorf("subscription not found")
	}

	// Apply filters
	outbounds = s.applyFilters(outbounds, live)

	// Generate fragment file
	fragmentPath := s.getFragmentPath(live)
	if err := s.writeFragment(fragmentPath, outbounds, live); err != nil {
		return err
	}

	sub.LastUpdate = time.Now()
	return nil
}

func (s *SubscriptionService) refreshMihomo(sub *Subscription) error {
	body, err := s.downloadRaw(sub.URL, sub)
	if err != nil {
		return err
	}

	providerPath := s.getMihomoProviderPath(sub)
	if err := os.MkdirAll(filepath.Dir(providerPath), 0755); err != nil {
		return fmt.Errorf("create providers dir: %w", err)
	}

	// Ensure content has a proxies: header; wrap bare lists if needed
	content := strings.TrimSpace(string(body))
	if !strings.HasPrefix(content, "proxies:") && !strings.Contains(content, "\nproxies:") {
		content = "proxies:\n" + content
	}

	if err := utils.AtomicWriteFile(providerPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("write provider file: %w", err)
	}

	sub.RuleCount = countMihomoRules(content)
	sub.LastUpdate = time.Now()
	return nil
}

func (s *SubscriptionService) downloadRaw(subURL string, sub *Subscription) ([]byte, error) {
	parsed, err := url.Parse(subURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("only http and https URLs are allowed for subscriptions")
	}
	resp, err := s.httpClient.Get(subURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if userInfo := resp.Header.Get("Subscription-Userinfo"); userInfo != "" {
		sub.Upload, sub.Download, sub.Total = parseSubscriptionUserinfo(userInfo)
	} else {
		sub.Upload, sub.Download, sub.Total = 0, 0, 0
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes))
}

func (s *SubscriptionService) getMihomoProviderPath(sub *Subscription) string {
	dir := s.mihomoConfigDir
	if dir == "" {
		dir = "/opt/etc/mihomo"
	}
	return filepath.Join(dir, "providers", fmt.Sprintf("xcp_%s.yaml", sub.ID))
}

func (s *SubscriptionService) GetLocked(id string) *Subscription {
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			return &s.subscriptions[i]
		}
	}
	return nil
}

func (s *SubscriptionService) downloadAndParse(subURL string, sub *Subscription) ([]Outbound, error) {
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

	if userInfo := resp.Header.Get("Subscription-Userinfo"); userInfo != "" {
		sub.Upload, sub.Download, sub.Total = parseSubscriptionUserinfo(userInfo)
	} else {
		sub.Upload, sub.Download, sub.Total = 0, 0, 0
	}

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

	wrapper := struct {
		Outbounds []Outbound `json:"outbounds"`
	}{
		Outbounds: outbounds,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
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

	// socks:// or socks5://
	if strings.HasPrefix(link, "socks://") || strings.HasPrefix(link, "socks5://") {
		return parseSOCKSLink(link)
	}

	// http:// proxy (must come after http-based subscription URL check is done)
	if strings.HasPrefix(link, "http-proxy://") {
		return parseHTTPProxyLink(link)
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
	aidInt, _ := strconv.Atoi(vmess.Aid) // Aid=0 if empty/invalid — valid default

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
							"alterId":  aidInt,
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

func parseSOCKSLink(link string) *Outbound {
	// socks:// or socks5://user:pass@host:port#tag
	// Normalise socks5:// to socks:// so url.Parse works uniformly
	link = strings.Replace(link, "socks5://", "socks://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "socks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

// parseHTTPProxyLink parses http-proxy://user:pass@host:port#tag share links.
// Uses the "http-proxy://" scheme to avoid conflicts with http:// subscription URLs.
func parseHTTPProxyLink(link string) *Outbound {
	// Normalise http-proxy:// → http:// so url.Parse can handle it
	link = strings.Replace(link, "http-proxy://", "http://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "http",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

// ParseLinksResult holds the result for a single link parse attempt.
type ParseLinksResult struct {
	Link     string    `json:"link"`
	Outbound *Outbound `json:"outbound,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// ParseLinks parses a slice of share links and returns results for each.
// Unsupported or invalid links are reported as errors, not fatal failures.
func (s *SubscriptionService) ParseLinks(links []string) []ParseLinksResult {
	results := make([]ParseLinksResult, 0, len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		ob := parseShareLink(link)
		if ob == nil {
			results = append(results, ParseLinksResult{
				Link:  link,
				Error: "unsupported or invalid share link format",
			})
		} else {
			results = append(results, ParseLinksResult{
				Link:     link,
				Outbound: ob,
			})
		}
	}
	return results
}

// isRefreshDue returns true if a subscription needs to be refreshed.
// Respects the exponential backoff state for previously-failed refreshes.
func (s *SubscriptionService) isRefreshDue(sub *Subscription, now time.Time) bool {
	if !sub.Enabled || sub.Interval <= 0 {
		return false
	}
	// Check backoff: if a previous attempt failed, wait until nextRetry
	if val, ok := s.retries.Load(sub.ID); ok {
		rs := val.(*retryState)
		if now.Before(rs.nextRetry) {
			return false
		}
	}
	return now.Sub(sub.LastUpdate) >= time.Duration(sub.Interval)*time.Hour
}

// recordFailure increments the failure counter and schedules the next retry
// with exponential backoff capped at backoffMax.
func (s *SubscriptionService) recordFailure(id string) {
	rs := &retryState{failCount: 1}
	if val, ok := s.retries.Load(id); ok {
		rs = val.(*retryState)
		rs.failCount++
	}
	delay := backoffBase * (1 << uint(rs.failCount-1))
	if delay > backoffMax {
		delay = backoffMax
	}
	rs.nextRetry = time.Now().Add(delay)
	s.retries.Store(id, rs)
}

// clearFailure resets the backoff state on a successful refresh.
func (s *SubscriptionService) clearFailure(id string) {
	s.retries.Delete(id)
}

// checkAndRefreshDue scans all subscriptions and launches a goroutine for
// each one that is due at the given point in time. Failed refreshes are
// subject to exponential backoff.
func (s *SubscriptionService) checkAndRefreshDue(now time.Time) {
	subs := s.List()
	for _, sub := range subs {
		if s.isRefreshDue(&sub, now) {
			go func(id string) {
				if err := s.Refresh(id); err != nil {
					// "already in progress" is not a real failure — skip backoff
					if !strings.Contains(err.Error(), "already in progress") {
						s.recordFailure(id)
						fc := 0
						if val, ok := s.retries.Load(id); ok {
							fc = val.(*retryState).failCount
						}
						log.Printf("subscription %s: auto-refresh failed (attempt %d): %v", id, fc, err)
					}
				} else {
					s.clearFailure(id)
				}
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

// parseSubscriptionUserinfo parses values from Subscription-Userinfo header:
// e.g., upload=123; download=456; total=789; expire=0
func parseSubscriptionUserinfo(header string) (upload, download, total int64) {
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		vStr := strings.TrimSpace(kv[1])
		val, err := strconv.ParseInt(vStr, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			upload = val
		case "download":
			download = val
		case "total":
			total = val
		}
	}
	return
}

// countMihomoRules counts the number of rules in a Mihomo config.
// It finds the "rules:" section and counts lines that start with "-" or "  -" inside it.
func countMihomoRules(content string) int {
	lines := strings.Split(content, "\n")
	inRulesSection := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if inRulesSection {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "-") && strings.Contains(line, ":") {
				if !strings.HasPrefix(trimmed, "rules:") {
					inRulesSection = false
				}
			}
		}

		if strings.HasPrefix(trimmed, "rules:") {
			inRulesSection = true
			continue
		}

		if inRulesSection {
			if strings.HasPrefix(trimmed, "-") {
				count++
			}
		}
	}
	return count
}

