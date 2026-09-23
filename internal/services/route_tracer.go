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
	"sync"
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
	RuleIndex     int     `json:"rule_index,omitempty"`
	// Undetermined is set when a rule before the reported match could not be
	// evaluated by the panel (e.g. an unreadable rule set). Mihomo may route
	// the target by that rule instead; UndeterminedRule names it.
	Undetermined       bool   `json:"undetermined,omitempty"`
	UndeterminedRule   string `json:"undetermined_rule,omitempty"`
	UndeterminedGroup  string `json:"undetermined_group,omitempty"`
	UndeterminedReason string `json:"undetermined_reason,omitempty"`
}

// RouteTracerService evaluates targets against user custom rules and kernel routing rules.
type RouteTracerService struct {
	userRulesSvc *UserRulesService
	mihomoSvc    *MihomoService
	configDir    string

	geo       GeoTagLookup
	mihomoBin string
	cacheDir  string
	convertMu sync.Mutex
}

// SetRuleEvaluation enables GEOSITE/GEOIP checks via geo and RULE-SET checks
// by reading provider files (MRS sets are converted with mihomoBin and
// cached in cacheDir).
func (s *RouteTracerService) SetRuleEvaluation(geo GeoTagLookup, mihomoBin, cacheDir string) {
	s.geo = geo
	s.mihomoBin = mihomoBin
	s.cacheDir = cacheDir
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
	Type    string   `json:"type"`
	Payload string   `json:"payload"`
	Proxy   string   `json:"proxy"`
	Params  []string `json:"-"`
	Extra   struct {
		Disabled bool `json:"disabled"`
	} `json:"extra"`
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
					kType, kPayload, rest := splitRuleLine(rStr)
					if kType == "" {
						continue
					}
					rule := MihomoKernelRule{Type: kType, Payload: kPayload}
					if normalizeRuleType(kType) == "MATCH" {
						// MATCH,TARGET has no payload.
						rule.Payload, rule.Proxy = "", kPayload
					} else if len(rest) > 0 {
						rule.Proxy = rest[0]
						rule.Params = rest[1:]
					}
					kRules = append(kRules, rule)
				}
				if len(kRules) > 0 {
					return kRules, defaultGroup, nil
				}
			}
		}
	}

	return nil, defaultGroup, fmt.Errorf("no kernel rules available")
}

// matchKernelRules walks the rules in order like Mihomo does and returns
// the first rule that certainly matches. Rules the panel cannot evaluate are
// remembered: the first of them is reported as a possible earlier match.
func matchKernelRules(ec *ruleEvalContext, rules []MihomoKernelRule) *RouteTraceResult {
	var pending *RouteTraceResult
	for i, r := range rules {
		if r.Extra.Disabled {
			continue
		}
		verdict, reason := ec.evalRule(r.Type, r.Payload, r.Params)
		switch verdict {
		case verdictUnknown:
			if pending == nil {
				pending = &RouteTraceResult{
					Undetermined:       true,
					UndeterminedRule:   strings.TrimSuffix(r.Type+","+r.Payload, ","),
					UndeterminedGroup:  r.Proxy,
					UndeterminedReason: reason,
				}
			}
		case verdictYes:
			res := &RouteTraceResult{
				Target:       ec.host,
				Matched:      true,
				RuleType:     r.Type,
				RulePayload:  r.Payload,
				TargetAction: routeAction(r.Proxy),
				TargetGroup:  r.Proxy,
				Source:       "kernel_rule",
				RuleIndex:    i + 1,
			}
			if pending != nil {
				res.Undetermined = true
				res.UndeterminedRule = pending.UndeterminedRule
				res.UndeterminedGroup = pending.UndeterminedGroup
				res.UndeterminedReason = pending.UndeterminedReason
			}
			return res
		}
	}
	return pending
}

func routeAction(proxy string) string {
	switch action := strings.ToUpper(strings.TrimSpace(proxy)); action {
	case "DIRECT", "REJECT", "REJECT-DROP":
		return action
	default:
		return "PROXY"
	}
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

// fetchEmptyRuleSets asks the running core which rule providers hold no
// rules. Errors yield an empty map: every set is then read from disk.
func (s *RouteTracerService) fetchEmptyRuleSets(ctx context.Context) map[string]bool {
	empty := map[string]bool{}
	if s.mihomoSvc == nil {
		return empty
	}
	info, err := s.mihomoSvc.ParseControllerConfig()
	if err != nil || (info.Type != "unix" && (info.Type != "tcp" || info.Target == "")) {
		return empty
	}
	reqURL := "http://localhost/providers/rules"
	if info.Type == "tcp" {
		target := info.Target
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			target = "http://" + target
		}
		reqURL = strings.TrimRight(target, "/") + "/providers/rules"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return empty
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}
	resp, err := s.mihomoSvc.GetHTTPClient().Do(req)
	if err != nil {
		return empty
	}
	defer resp.Body.Close()
	var body struct {
		Providers map[string]struct {
			RuleCount int `json:"ruleCount"`
		} `json:"providers"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return empty
	}
	for name, p := range body.Providers {
		if p.RuleCount == 0 {
			empty[name] = true
		}
	}
	return empty
}

// ruleSetStore builds a reader for the rule-providers declared in the
// Mihomo config, or nil when the config cannot be read.
func (s *RouteTracerService) ruleSetStore() *ruleSetStore {
	cfgDir := s.configDir
	if cfgDir == "" && s.mihomoSvc != nil {
		cfgDir = s.mihomoSvc.ConfigDir
	}
	if cfgDir == "" {
		return nil
	}
	configPath := filepath.Join(cfgDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(cfgDir, "config.yml")
	}
	providers, err := loadRuleProviders(configPath)
	if err != nil || len(providers) == 0 {
		return nil
	}
	return &ruleSetStore{providers: providers, homeDir: filepath.Clean(cfgDir), cacheDir: s.cacheDir, mihomoBin: s.mihomoBin, mu: &s.convertMu}
}

// TraceRoute simulates routing evaluation for the given target and port.
func (s *RouteTracerService) TraceRoute(ctx context.Context, rawTarget string, port int) (res *RouteTraceResult, err error) {
	start := time.Now()

	host, targetPort, isIP, ip, nerr := normalizeTarget(rawTarget, port)
	if nerr != nil {
		return nil, nerr
	}

	defaultGroup := "PROXY"
	var userRules []UserRule
	if s.userRulesSvc != nil {
		userRules = s.userRulesSvc.List()
	}

	// 1. Check user custom rules first
	res = matchUserRules(userRules, host, targetPort, isIP, ip, defaultGroup)

	// 2. If no user rule matched, check kernel rules
	if res == nil {
		kRules, kDefGroup, _ := s.fetchKernelRules(ctx)
		if kDefGroup != "" {
			defaultGroup = kDefGroup
		}
		if len(kRules) > 0 {
			ec := &ruleEvalContext{ctx: ctx, host: host, port: targetPort, isIP: isIP, ip: ip, geo: s.geo}
			ec.sets = s.ruleSetStore()
			if ec.sets != nil {
				ec.sets.empty = s.fetchEmptyRuleSets(ctx)
			}
			res = matchKernelRules(ec, kRules)
			if res != nil && !res.Matched {
				// Only undetermined rules and no certain match: keep the
				// warning and fall back to the default group below.
				pending := res
				res = nil
				defer func() {
					if res != nil {
						res.Undetermined = true
						res.UndeterminedRule = pending.UndeterminedRule
						res.UndeterminedGroup = pending.UndeterminedGroup
						res.UndeterminedReason = pending.UndeterminedReason
					}
				}()
			}
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
