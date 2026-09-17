package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RouteTraceRequest defines input payload for route testing.
type RouteTraceRequest struct {
	Target string `json:"target"`
	Port   int    `json:"port,omitempty"`
}

// RouteTraceResult defines the step-by-step resolution of a target through the routing rules.
type RouteTraceResult struct {
	Target        string  `json:"target"`
	Matched       bool    `json:"matched"`
	RuleType      string  `json:"rule_type"`
	RulePayload   string  `json:"rule_payload"`
	TargetAction  string  `json:"target_action"`
	TargetGroup   string  `json:"target_group"`
	SelectedProxy string  `json:"selected_proxy"`
	ProxyType     string  `json:"proxy_type"`
	TraceTimeMs   float64 `json:"trace_time_ms"`
	Source        string  `json:"source"` // "user_rule" | "kernel_rule" | "fallback"
}

// RouteTracerService evaluates targets against user custom rules and kernel routing rules.
type RouteTracerService struct {
	userRulesSvc *UserRulesService
	mihomoSvc    *MihomoService
	configDir    string
}

// NewRouteTracerService initializes RouteTracerService.
func NewRouteTracerService(userRulesSvc *UserRulesService, mihomoSvc *MihomoService, configDir string) *RouteTracerService {
	return &RouteTracerService{
		userRulesSvc: userRulesSvc,
		mihomoSvc:    mihomoSvc,
		configDir:    configDir,
	}
}

var safeTargetRe = regexp.MustCompile(`^[a-zA-Z0-9.\-_:/\[\]]+$`)

// normalizeTarget parses raw input (domain, URL, or IP with optional port) into clean host, port, and IP flag.
func normalizeTarget(raw string, defaultPort int) (host string, port int, isIP bool, parsedIP net.IP, err error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", 0, false, nil, fmt.Errorf("target cannot be empty")
	}
	if len(clean) > 255 {
		return "", 0, false, nil, fmt.Errorf("target exceeds 255 characters")
	}

	// Remove scheme if present
	if idx := strings.Index(clean, "://"); idx != -1 {
		clean = clean[idx+3:]
	}
	// Strip trailing path/query/fragment
	if idx := strings.IndexAny(clean, "/?#"); idx != -1 {
		clean = clean[:idx]
	}
	clean = strings.TrimSpace(clean)

	if !safeTargetRe.MatchString(clean) {
		return "", 0, false, nil, fmt.Errorf("target contains invalid characters")
	}

	// Try splitting host and port
	h, pStr, splitErr := net.SplitHostPort(clean)
	if splitErr == nil {
		host = h
		if p, pErr := strconv.Atoi(pStr); pErr == nil && p > 0 && p <= 65535 {
			port = p
		}
	} else {
		host = clean
	}

	// Strip IPv6 brackets if any
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	if port == 0 {
		if defaultPort > 0 && defaultPort <= 65535 {
			port = defaultPort
		} else {
			port = 443
		}
	}

	parsedIP = net.ParseIP(host)
	if parsedIP != nil {
		isIP = true
	}

	return strings.ToLower(host), port, isIP, parsedIP, nil
}

// matchUserRules tests the target against user custom rules in priority order.
func matchUserRules(rules []UserRule, host string, port int, isIP bool, ip net.IP, defaultGroup string) *RouteTraceResult {
	if defaultGroup == "" {
		defaultGroup = "PROXY"
	}

	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		val := strings.TrimSpace(r.Value)
		if val == "" {
			continue
		}

		matched := false
		switch r.Type {
		case "domain":
			if strings.EqualFold(host, val) {
				matched = true
			}
		case "domain_suffix", "suffix":
			lowHost := strings.ToLower(host)
			lowVal := strings.ToLower(strings.TrimPrefix(val, "."))
			if lowHost == lowVal || strings.HasSuffix(lowHost, "."+lowVal) {
				matched = true
			}
		case "domain_keyword", "keyword":
			if strings.Contains(strings.ToLower(host), strings.ToLower(val)) {
				matched = true
			}
		case "ip_cidr", "ip":
			if isIP && ip != nil {
				if strings.Contains(val, "/") {
					if _, ipNet, err := net.ParseCIDR(val); err == nil && ipNet.Contains(ip) {
						matched = true
					}
				} else {
					ruleIP := net.ParseIP(val)
					if ruleIP != nil && ruleIP.Equal(ip) {
						matched = true
					}
				}
			}
		case "port":
			if p, err := strconv.Atoi(val); err == nil && p == port {
				matched = true
			}
		default:
			// Fallback: suffix match
			lowHost := strings.ToLower(host)
			lowVal := strings.ToLower(strings.TrimPrefix(val, "."))
			if lowHost == lowVal || strings.HasSuffix(lowHost, "."+lowVal) {
				matched = true
			}
		}

		if matched {
			action := strings.ToUpper(r.Target)
			group := action
			if action == "PROXY" {
				if r.Group != "" {
					group = r.Group
				} else {
					group = defaultGroup
				}
			}

			return &RouteTraceResult{
				Target:       host,
				Matched:      true,
				RuleType:     r.Type,
				RulePayload:  val,
				TargetAction: action,
				TargetGroup:  group,
				Source:       "user_rule",
			}
		}
	}

	return nil
}

// MihomoKernelRule represents a parsed rule item from Mihomo API or config.yaml.
type MihomoKernelRule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

// fetchKernelRules retrieves rules either from Mihomo REST API or by parsing config.yaml.
func (s *RouteTracerService) fetchKernelRules(ctx context.Context) ([]MihomoKernelRule, string, error) {
	defaultGroup := "PROXY"

	// 1. Try Mihomo REST API /rules
	if s.mihomoSvc != nil {
		info, err := s.mihomoSvc.ParseControllerConfig()
		if err == nil && (info.Type == "unix" || (info.Type == "tcp" && info.Target != "")) {
			var reqURL string
			if info.Type == "unix" {
				reqURL = "http://localhost/rules"
			} else {
				target := info.Target
				if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
					target = "http://" + target
				}
				reqURL = strings.TrimRight(target, "/") + "/rules"
			}

			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
			if reqErr == nil {
				if info.Secret != "" {
					req.Header.Set("Authorization", "Bearer "+info.Secret)
				}
				client := s.mihomoSvc.GetHTTPClient()
				resp, doErr := client.Do(req)
				if doErr == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						var res struct {
							Rules []MihomoKernelRule `json:"rules"`
						}
						if decodeErr := json.NewDecoder(resp.Body).Decode(&res); decodeErr == nil && len(res.Rules) > 0 {
							return res.Rules, defaultGroup, nil
						}
					}
				}
			}
		}
	}

	// 2. Fallback: Parse config.yaml from configDir or s.mihomoSvc.ConfigDir
	cfgDir := s.configDir
	if cfgDir == "" && s.mihomoSvc != nil {
		cfgDir = s.mihomoSvc.ConfigDir
	}
	if cfgDir != "" {
		configPath := filepath.Join(cfgDir, "config.yaml")
		if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
			configPath = filepath.Join(cfgDir, "config.yml")
		}
		data, readErr := os.ReadFile(configPath)
		if readErr == nil {
			var parsed struct {
				ProxyGroups []map[string]interface{} `yaml:"proxy-groups"`
				Rules       []string                 `yaml:"rules"`
			}
			if yamlErr := yaml.Unmarshal(data, &parsed); yamlErr == nil {
				if len(parsed.ProxyGroups) > 0 {
					if name, ok := parsed.ProxyGroups[0]["name"].(string); ok && name != "" {
						defaultGroup = name
					}
				}
				var kRules []MihomoKernelRule
				for _, rStr := range parsed.Rules {
					parts := strings.Split(rStr, ",")
					if len(parts) >= 2 {
						kType := strings.TrimSpace(parts[0])
						kPayload := strings.TrimSpace(parts[1])
						kProxy := ""
						if len(parts) >= 3 {
							kProxy = strings.TrimSpace(parts[2])
						} else {
							// MATCH,PROXY has 2 parts
							if strings.EqualFold(kType, "MATCH") {
								kProxy = kPayload
								kPayload = ""
							}
						}
						kRules = append(kRules, MihomoKernelRule{
							Type:    kType,
							Payload: kPayload,
							Proxy:   kProxy,
						})
					}
				}
				if len(kRules) > 0 {
					return kRules, defaultGroup, nil
				}
			}
		}
	}

	return nil, defaultGroup, fmt.Errorf("no kernel rules available")
}

// matchKernelRules tests the target against Mihomo kernel rules.
func matchKernelRules(rules []MihomoKernelRule, host string, port int, isIP bool, ip net.IP) *RouteTraceResult {
	for _, r := range rules {
		kType := strings.ToUpper(r.Type)
		payload := strings.TrimSpace(r.Payload)
		proxy := strings.TrimSpace(r.Proxy)
		matched := false

		switch kType {
		case "DOMAIN":
			if strings.EqualFold(host, payload) {
				matched = true
			}
		case "DOMAIN-SUFFIX":
			lowHost := strings.ToLower(host)
			lowVal := strings.ToLower(strings.TrimPrefix(payload, "."))
			if lowHost == lowVal || strings.HasSuffix(lowHost, "."+lowVal) {
				matched = true
			}
		case "DOMAIN-KEYWORD":
			if strings.Contains(strings.ToLower(host), strings.ToLower(payload)) {
				matched = true
			}
		case "GEOIP":
			if isIP && ip != nil && strings.EqualFold(payload, "private") {
				if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					matched = true
				}
			}
		case "IP-CIDR", "IP-CIDR6":
			if isIP && ip != nil {
				cidr := payload
				if !strings.Contains(cidr, "/") {
					cidr += "/32"
				}
				if _, ipNet, err := net.ParseCIDR(cidr); err == nil && ipNet.Contains(ip) {
					matched = true
				}
			}
		case "DST-PORT":
			if p, err := strconv.Atoi(payload); err == nil && p == port {
				matched = true
			}
		case "MATCH":
			matched = true
		}

		if matched {
			action := strings.ToUpper(proxy)
			if action != "DIRECT" && action != "REJECT" {
				action = "PROXY"
			}
			return &RouteTraceResult{
				Target:       host,
				Matched:      true,
				RuleType:     r.Type,
				RulePayload:  payload,
				TargetAction: action,
				TargetGroup:  proxy,
				Source:       "kernel_rule",
			}
		}
	}

	return nil
}

// lookupSelectedProxy resolves the currently selected node name and type for a proxy group.
func (s *RouteTracerService) lookupSelectedProxy(ctx context.Context, groupName string) (proxyName string, proxyType string) {
	upper := strings.ToUpper(groupName)
	if upper == "DIRECT" {
		return "DIRECT", "direct"
	}
	if upper == "REJECT" {
		return "REJECT", "reject"
	}

	if s.mihomoSvc == nil {
		return groupName, "proxy"
	}

	info, err := s.mihomoSvc.ParseControllerConfig()
	if err != nil || (info.Type != "unix" && (info.Type != "tcp" || info.Target == "")) {
		return groupName, "proxy"
	}

	client := s.mihomoSvc.GetHTTPClient()
	currentName := groupName
	selected := groupName
	nodeType := "proxy"

	visited := make(map[string]bool)
	for depth := 0; depth < 5; depth++ {
		if currentName == "" || visited[currentName] {
			break
		}
		visited[currentName] = true

		var reqURL string
		if info.Type == "unix" {
			reqURL = "http://localhost/proxies/" + url.PathEscape(currentName)
		} else {
			target := info.Target
			if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
				target = "http://" + target
			}
			reqURL = strings.TrimRight(target, "/") + "/proxies/" + url.PathEscape(currentName)
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if reqErr != nil {
			break
		}
		if info.Secret != "" {
			req.Header.Set("Authorization", "Bearer "+info.Secret)
		}

		resp, doErr := client.Do(req)
		if doErr != nil {
			break
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			break
		}

		var pData struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Now  string `json:"now"`
		}
		decErr := json.NewDecoder(resp.Body).Decode(&pData)
		resp.Body.Close()
		if decErr != nil {
			break
		}

		if pData.Type != "" {
			nodeType = pData.Type
		}
		if pData.Name != "" {
			selected = pData.Name
		}
		if pData.Now != "" {
			selected = pData.Now
			if pData.Now != currentName {
				currentName = pData.Now
				continue
			}
		}
		break
	}

	return selected, nodeType
}

// TraceRoute simulates routing evaluation for the given target and port.
func (s *RouteTracerService) TraceRoute(ctx context.Context, rawTarget string, port int) (*RouteTraceResult, error) {
	start := time.Now()

	host, targetPort, isIP, ip, err := normalizeTarget(rawTarget, port)
	if err != nil {
		return nil, err
	}

	defaultGroup := "PROXY"
	var userRules []UserRule
	if s.userRulesSvc != nil {
		userRules = s.userRulesSvc.List()
	}

	// 1. Check user custom rules first
	res := matchUserRules(userRules, host, targetPort, isIP, ip, defaultGroup)

	// 2. If no user rule matched, check kernel rules
	if res == nil {
		kRules, kDefGroup, _ := s.fetchKernelRules(ctx)
		if kDefGroup != "" {
			defaultGroup = kDefGroup
		}
		if len(kRules) > 0 {
			res = matchKernelRules(kRules, host, targetPort, isIP, ip)
		}
	}

	// 3. Fallback: MATCH -> defaultGroup
	if res == nil {
		res = &RouteTraceResult{
			Target:       host,
			Matched:      true,
			RuleType:     "MATCH",
			RulePayload:  "",
			TargetAction: "PROXY",
			TargetGroup:  defaultGroup,
			Source:       "fallback",
		}
	}

	// 4. Resolve selected proxy node and type
	selectedProxy, proxyType := s.lookupSelectedProxy(ctx, res.TargetGroup)
	res.SelectedProxy = selectedProxy
	res.ProxyType = proxyType
	res.TraceTimeMs = float64(time.Since(start).Microseconds()) / 1000.0

	return res, nil
}
