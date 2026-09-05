package services

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeeneticFakeIPExclusions defines standard domains that must never be assigned Fake-IP.
var KeeneticFakeIPExclusions = []string{
	"+.keenetic.pro",
	"+.keenetic.net",
	"+.keenetic.link",
	"+.keenetic.io",
	"192.168.*",
	"172.16.*",
	"10.*",
	"localhost",
	"*.lan",
	"*.local",
}

// SafetyDirectPorts defines ports that must default to DIRECT bypass to avoid breaking LAN & remote work.
var SafetyDirectPorts = []string{"3389", "22", "445", "1194", "51820"}

// MergeStats contains counters calculated from the actual smart merge result.
type MergeStats struct {
	Proxies        int `json:"proxies"`
	ProxyProviders int `json:"proxy_providers"`
	UserRules      int `json:"user_rules"`
	Rules          int `json:"rules"`
}

// SmartMergeMihomo merges a configuration template into an existing Mihomo YAML config.
// It strictly preserves user proxies, proxy-providers, ports and secrets, while applying
// template rules, rule-providers, proxy-groups, and injecting safety bypasses & user rules.
// When templateOwnsNodes is true and templateYAML contains non-empty proxies or proxy-providers,
// those template nodes are preserved in the result instead of being overwritten by existing nodes.
func SmartMergeMihomo(existingYAML string, templateYAML string, userRules []UserRule, templateOwnsNodes bool) (string, MergeStats, error) {
	var existing map[string]interface{}
	var tmpl map[string]interface{}
	var stats MergeStats

	if strings.TrimSpace(existingYAML) != "" {
		if err := yaml.Unmarshal([]byte(existingYAML), &existing); err != nil {
			// If existing is corrupted, initialize clean map
			existing = make(map[string]interface{})
		}
	} else {
		existing = make(map[string]interface{})
	}
	if existing == nil {
		existing = make(map[string]interface{})
	}

	if err := yaml.Unmarshal([]byte(templateYAML), &tmpl); err != nil {
		return "", stats, fmt.Errorf("failed to parse template YAML: %w", err)
	}

	result := make(map[string]interface{})

	// 1. Copy template baseline
	for k, v := range tmpl {
		result[k] = v
	}

	// 2. Preserve essential runtime and server settings from existing config
	preservedKeys := []string{
		"secret",
		"external-controller",
		"external-ui",
		"port",
		"socks-port",
		"redir-port",
		"tproxy-port",
		"mixed-port",
		"allow-lan",
		"bind-address",
		"mode",
		"log-level",
		"ipv6",
	}
	for _, key := range preservedKeys {
		if val, exists := existing[key]; exists && val != nil && val != "" {
			result[key] = val
		}
	}

	// 3. Preserve User Proxies and Proxy Providers
	tmplHasProxies := false
	if tp, ok := tmpl["proxies"].([]interface{}); ok && len(tp) > 0 {
		tmplHasProxies = true
	}
	if !templateOwnsNodes || !tmplHasProxies {
		if existingProxies, ok := existing["proxies"]; ok && existingProxies != nil {
			if exSlice, isSlice := existingProxies.([]interface{}); isSlice && len(exSlice) > 0 {
				result["proxies"] = existingProxies
			}
		}
	}

	tmplHasProviders := false
	if tprov, ok := tmpl["proxy-providers"].(map[string]interface{}); ok && len(tprov) > 0 {
		tmplHasProviders = true
	}
	if !templateOwnsNodes || !tmplHasProviders {
		if existingProviders, ok := existing["proxy-providers"]; ok && existingProviders != nil {
			if exMap, isMap := existingProviders.(map[string]interface{}); isMap && len(exMap) > 0 {
				result["proxy-providers"] = existingProviders
			}
		}
	}

	// 4. Ensure Proxy Groups validity
	// If template has proxy-groups referencing proxy-providers that exist in existing config, ensure they are linked.
	if groups, ok := result["proxy-groups"].([]interface{}); ok {
		// Collect all available proxy / provider names
		var availableProxies []string
		if pSlice, ok := result["proxies"].([]interface{}); ok {
			for _, p := range pSlice {
				if pMap, ok := p.(map[string]interface{}); ok {
					if name, ok := pMap["name"].(string); ok && name != "" {
						availableProxies = append(availableProxies, name)
					}
				}
			}
		}

		var availableProviders []string
		if pMap, ok := result["proxy-providers"].(map[string]interface{}); ok {
			for name := range pMap {
				availableProviders = append(availableProviders, name)
			}
		}

		// Ensure every group has proxies or use-providers
		for i, g := range groups {
			if gMap, ok := g.(map[string]interface{}); ok {
				existingList, _ := gMap["proxies"].([]interface{})
				useProviders, _ := gMap["use"].([]interface{})

				if len(existingList) == 0 && len(useProviders) == 0 {
					// Fallback: attach available providers or DIRECT
					if len(availableProviders) > 0 {
						var useList []interface{}
						for _, prov := range availableProviders {
							useList = append(useList, prov)
						}
						gMap["use"] = useList
					} else if len(availableProxies) > 0 {
						var pList []interface{}
						for _, p := range availableProxies {
							pList = append(pList, p)
						}
						gMap["proxies"] = pList
					} else {
						gMap["proxies"] = []interface{}{"DIRECT"}
					}
				}
				groups[i] = gMap
			}
		}
		result["proxy-groups"] = groups
	}

	// 5. Enhance DNS fake-ip-filter with Keenetic exclusions
	if dns, ok := result["dns"].(map[string]interface{}); ok {
		existingFilter, _ := dns["fake-ip-filter"].([]interface{})
		filterSet := make(map[string]bool)
		for _, f := range existingFilter {
			if str, ok := f.(string); ok {
				filterSet[str] = true
			}
		}
		for _, exc := range KeeneticFakeIPExclusions {
			if !filterSet[exc] {
				existingFilter = append(existingFilter, exc)
				filterSet[exc] = true
			}
		}
		dns["fake-ip-filter"] = existingFilter
		result["dns"] = dns
	}

	// 6. Build Rules: [Safety Bypasses] -> [User Rules] -> [Template Rules]
	var finalRules []interface{}

	// A) Safety direct ports (RDP, SMB, SSH, VPN)
	for _, port := range SafetyDirectPorts {
		finalRules = append(finalRules, fmt.Sprintf("DST-PORT,%s,DIRECT", port))
	}

	// B) User Custom Rules
	var proxyGroupName = "PROXY"
	if groups, ok := result["proxy-groups"].([]interface{}); ok && len(groups) > 0 {
		if firstGroup, ok := groups[0].(map[string]interface{}); ok {
			if name, ok := firstGroup["name"].(string); ok && name != "" {
				proxyGroupName = name
			}
		}
	}

	userRulesCount := 0
	for _, ur := range userRules {
		if !ur.Enabled || strings.TrimSpace(ur.Value) == "" {
			continue
		}
		userRulesCount++
		target := strings.ToUpper(ur.Target)
		if target == "PROXY" {
			target = proxyGroupName
		}
		val := strings.TrimSpace(ur.Value)
		switch ur.Type {
		case "domain":
			finalRules = append(finalRules, fmt.Sprintf("DOMAIN,%s,%s", val, target))
		case "domain_suffix", "suffix":
			finalRules = append(finalRules, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", val, target))
		case "domain_keyword", "keyword":
			finalRules = append(finalRules, fmt.Sprintf("DOMAIN-KEYWORD,%s,%s", val, target))
		case "ip_cidr", "ip":
			finalRules = append(finalRules, fmt.Sprintf("IP-CIDR,%s,%s,no-resolve", val, target))
		case "port":
			finalRules = append(finalRules, fmt.Sprintf("DST-PORT,%s,%s", val, target))
		default:
			if strings.Contains(val, "/") {
				finalRules = append(finalRules, fmt.Sprintf("IP-CIDR,%s,%s,no-resolve", val, target))
			} else {
				finalRules = append(finalRules, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", val, target))
			}
		}
	}

	// C) Template Rules
	if tmplRules, ok := tmpl["rules"].([]interface{}); ok {
		for _, r := range tmplRules {
			if rStr, ok := r.(string); ok {
				// Avoid duplicate safety rules
				isDupe := false
				for _, port := range SafetyDirectPorts {
					if strings.EqualFold(rStr, fmt.Sprintf("DST-PORT,%s,DIRECT", port)) {
						isDupe = true
						break
					}
				}
				if !isDupe {
					finalRules = append(finalRules, r)
				}
			}
		}
	}

	result["rules"] = finalRules

	if pSlice, ok := result["proxies"].([]interface{}); ok {
		stats.Proxies = len(pSlice)
	}
	if pMap, ok := result["proxy-providers"].(map[string]interface{}); ok {
		stats.ProxyProviders = len(pMap)
	}
	stats.UserRules = userRulesCount
	stats.Rules = len(finalRules)

	out, err := yaml.Marshal(result)
	if err != nil {
		return "", stats, fmt.Errorf("failed to marshal merged YAML: %w", err)
	}

	return string(out), stats, nil
}

// SmartMergeXray merges an Xray routing template into an existing Xray configuration.
// It auto-detects modular routing vs monolithic files, auto-replaces "PROXY_TAG" with active outbound tag,
// and injects Keenetic DNS-over-VLESS protection (127.0.0.53 DIRECT) and user rules.
func SmartMergeXray(existingContent string, templateContent string, targetFilename string, activeOutboundTag string, userRules []UserRule) (string, MergeStats, error) {
	var stats MergeStats
	if activeOutboundTag == "" {
		activeOutboundTag = "proxy"
	}

	// Replace placeholder PROXY_TAG in template content
	templateContent = strings.ReplaceAll(templateContent, "PROXY_TAG", activeOutboundTag)

	var tmplObj map[string]interface{}
	if err := json.Unmarshal([]byte(templateContent), &tmplObj); err != nil {
		return "", stats, fmt.Errorf("invalid template JSON: %w", err)
	}

	// Extract routing object from template
	var routingMap map[string]interface{}
	if r, ok := tmplObj["routing"].(map[string]interface{}); ok {
		routingMap = r
	} else if _, hasRules := tmplObj["rules"]; hasRules {
		routingMap = tmplObj
	} else {
		routingMap = tmplObj
	}

	// Extract and prepare rules
	var templateRules []interface{}
	if rulesSlice, ok := routingMap["rules"].([]interface{}); ok {
		templateRules = rulesSlice
	}

	var finalRules []interface{}

	// Check if api routing rule or inbound was present in existing configuration or template
	hasAPIRule := false
	checkHasAPIRule := func(rules []interface{}) bool {
		for _, r := range rules {
			if m, ok := r.(map[string]interface{}); ok {
				if outTag, _ := m["outboundTag"].(string); outTag == "api" {
					return true
				}
				if inb, ok := m["inboundTag"].([]interface{}); ok {
					for _, tag := range inb {
						if tag == "api" {
							return true
						}
					}
				} else if inb, ok := m["inboundTag"].(string); ok && inb == "api" {
					return true
				}
			}
		}
		return false
	}

	if checkHasAPIRule(templateRules) {
		hasAPIRule = true
	} else if strings.TrimSpace(existingContent) != "" {
		var existObj map[string]interface{}
		if err := json.Unmarshal([]byte(existingContent), &existObj); err == nil && existObj != nil {
			if r, ok := existObj["routing"].(map[string]interface{}); ok && r != nil {
				if rs, ok := r["rules"].([]interface{}); ok {
					if checkHasAPIRule(rs) {
						hasAPIRule = true
					}
				}
			} else if rs, ok := existObj["rules"].([]interface{}); ok {
				if checkHasAPIRule(rs) {
					hasAPIRule = true
				}
			}
			if !hasAPIRule {
				if inbs, ok := existObj["inbounds"].([]interface{}); ok {
					for _, inb := range inbs {
						if m, ok := inb.(map[string]interface{}); ok {
							if tag, _ := m["tag"].(string); tag == "api" {
								hasAPIRule = true
								break
							}
						}
					}
				}
			}
		}
	}

	if hasAPIRule {
		finalRules = append(finalRules, map[string]interface{}{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		})
	}

	// 1. Mandatory Keenetic DNS-over-VLESS Resolver Protection (127.0.0.53 DIRECT)
	finalRules = append(finalRules, map[string]interface{}{
		"type":        "field",
		"outboundTag": "direct",
		"ip":          []string{"127.0.0.53", "127.0.0.1", "geoip:private"},
		"port":        "53",
	})

	// 2. Mandatory LAN / Remote services DIRECT bypass (RDP, SMB, SSH, VPN)
	finalRules = append(finalRules, map[string]interface{}{
		"type":        "field",
		"outboundTag": "direct",
		"port":        strings.Join(SafetyDirectPorts, ","),
	})

	// 3. User Custom Rules
	userRulesCount := 0
	for _, ur := range userRules {
		if !ur.Enabled || strings.TrimSpace(ur.Value) == "" {
			continue
		}
		userRulesCount++
		target := strings.ToLower(ur.Target)
		if target == "proxy" {
			target = activeOutboundTag
		}
		val := strings.TrimSpace(ur.Value)
		switch ur.Type {
		case "domain":
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"full:" + val},
			})
		case "domain_suffix", "suffix":
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"domain:" + val},
			})
		case "domain_keyword", "keyword":
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"keyword:" + val},
			})
		case "ip_cidr", "ip":
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"ip":          []string{val},
			})
		case "port":
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"port":        val,
			})
		default:
			finalRules = append(finalRules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{val},
			})
		}
	}

	// 4. Append template rules
	for _, tr := range templateRules {
		if hasAPIRule {
			if m, ok := tr.(map[string]interface{}); ok {
				if outTag, _ := m["outboundTag"].(string); outTag == "api" {
					continue
				}
			}
		}
		finalRules = append(finalRules, tr)
	}

	routingMap["rules"] = finalRules

	// Detect if target is modular 05_routing.json or monolithic config
	isModular := strings.Contains(strings.ToLower(targetFilename), "05_routing") ||
		strings.Contains(strings.ToLower(targetFilename), "routing")

	var outputObj interface{}
	if isModular {
		// Output clean modular routing structure
		if _, hasRoutingKey := tmplObj["routing"]; hasRoutingKey {
			outputObj = map[string]interface{}{"routing": routingMap}
		} else {
			outputObj = routingMap
		}
	} else {
		// Monolithic config
		var existingObj map[string]interface{}
		if strings.TrimSpace(existingContent) != "" {
			_ = json.Unmarshal([]byte(existingContent), &existingObj)
		}
		if existingObj == nil {
			existingObj = make(map[string]interface{})
		}
		for k, v := range tmplObj {
			existingObj[k] = v
		}
		existingObj["routing"] = routingMap
		outputObj = existingObj
	}

	stats.UserRules = userRulesCount
	stats.Rules = len(finalRules)

	out, err := json.MarshalIndent(outputObj, "", "  ")
	if err != nil {
		return "", stats, fmt.Errorf("failed to marshal merged Xray JSON: %w", err)
	}

	return string(out), stats, nil
}

// CheckPortAvailable tests whether the given TCP port can be bound on loopback.
func CheckPortAvailable(port int) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use (%w). Known busy ports: 10808/10809 (XKeen), 9090 (Mihomo API), 8088/8090/8091 (Web UI)", port, err)
	}
	_ = ln.Close()
	return nil
}

// ProvisionXrayAPIBlock adds or updates the Xray gRPC api inbound, stats/policy counters,
// and the mandatory routing rule (as rule 0) in the given JSON configuration.
// It is idempotent: repeated calls do not duplicate the api inbound or rule.
func ProvisionXrayAPIBlock(existingContent string, apiPort int) (string, error) {
	if apiPort <= 0 || apiPort > 65535 {
		return existingContent, fmt.Errorf("invalid api port: %d", apiPort)
	}

	// 1. Parse existing JSON
	var root map[string]interface{}
	trimmed := strings.TrimSpace(existingContent)
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			return "", fmt.Errorf("invalid existing Xray JSON: %w", err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}

	// 2. Check if API inbound is already configured on this port
	alreadyConfigured := false
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		for _, inb := range inbs {
			if m, ok := inb.(map[string]interface{}); ok {
				if tag, _ := m["tag"].(string); tag == "api" {
					switch p := m["port"].(type) {
					case float64:
						if int(p) == apiPort {
							alreadyConfigured = true
						}
					case int:
						if p == apiPort {
							alreadyConfigured = true
						}
					case string:
						if pInt, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && pInt == apiPort {
							alreadyConfigured = true
						}
					}
				}
			}
		}
	}

	// 3. Port availability test only if port is not already configured
	if !alreadyConfigured {
		if err := CheckPortAvailable(apiPort); err != nil {
			return existingContent, err
		}
	}

	// 3. Ensure "stats": {}
	if _, ok := root["stats"]; !ok {
		root["stats"] = map[string]interface{}{}
	}

	// 4. Set "api" object with tag "api" and services
	root["api"] = map[string]interface{}{
		"tag": "api",
		"services": []string{
			"StatsService",
			"RoutingService",
			"LoggerService",
		},
	}

	// 5. Ensure "policy.system" with stats flags
	var policyMap map[string]interface{}
	if p, ok := root["policy"].(map[string]interface{}); ok && p != nil {
		policyMap = p
	} else {
		policyMap = make(map[string]interface{})
		root["policy"] = policyMap
	}
	var systemMap map[string]interface{}
	if s, ok := policyMap["system"].(map[string]interface{}); ok && s != nil {
		systemMap = s
	} else {
		systemMap = make(map[string]interface{})
		policyMap["system"] = systemMap
	}
	systemMap["statsInboundUplink"] = true
	systemMap["statsInboundDownlink"] = true
	systemMap["statsOutboundUplink"] = true
	systemMap["statsOutboundDownlink"] = true

	// 6. Inbound for dokodemo-door on 127.0.0.1:apiPort
	apiInbound := map[string]interface{}{
		"tag":      "api",
		"listen":   "127.0.0.1",
		"port":     apiPort,
		"protocol": "dokodemo-door",
		"settings": map[string]interface{}{
			"address": "127.0.0.1",
		},
	}

	var inbounds []interface{}
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		inbounds = inbs
	}
	replacedInb := false
	for i, inb := range inbounds {
		if m, ok := inb.(map[string]interface{}); ok {
			if tag, ok := m["tag"].(string); ok && tag == "api" {
				inbounds[i] = apiInbound
				replacedInb = true
				break
			}
		}
	}
	if !replacedInb {
		inbounds = append(inbounds, apiInbound)
	}
	root["inbounds"] = inbounds

	// 7. Routing rule: inboundTag ["api"] -> outboundTag "api", MUST be rule 0
	apiRule := map[string]interface{}{
		"type":        "field",
		"inboundTag":  []string{"api"},
		"outboundTag": "api",
	}

	var routingMap map[string]interface{}
	if r, ok := root["routing"].(map[string]interface{}); ok && r != nil {
		routingMap = r
	} else {
		routingMap = make(map[string]interface{})
		root["routing"] = routingMap
	}

	var rules []interface{}
	if rs, ok := routingMap["rules"].([]interface{}); ok {
		rules = rs
	}

	// Filter out any existing rule that routes api inbound
	var filteredRules []interface{}
	for _, r := range rules {
		if rm, ok := r.(map[string]interface{}); ok {
			isAPIRule := false
			if inbTags, ok := rm["inboundTag"].([]interface{}); ok {
				for _, t := range inbTags {
					if s, ok := t.(string); ok && s == "api" {
						isAPIRule = true
						break
					}
				}
			} else if inbTag, ok := rm["inboundTag"].(string); ok && inbTag == "api" {
				isAPIRule = true
			}
			if isAPIRule {
				continue
			}
		}
		filteredRules = append(filteredRules, r)
	}

	newRules := make([]interface{}, 0, len(filteredRules)+1)
	newRules = append(newRules, apiRule)
	newRules = append(newRules, filteredRules...)
	routingMap["rules"] = newRules

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal Xray config with api block: %w", err)
	}
	return string(out), nil
}

// DeprovisionXrayAPIBlock removes the api inbound and routing rule from Xray JSON config.
// The stats and policy blocks are preserved for future use.
func DeprovisionXrayAPIBlock(existingContent string) (string, error) {
	trimmed := strings.TrimSpace(existingContent)
	if trimmed == "" {
		return existingContent, nil
	}

	var root map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return "", fmt.Errorf("invalid existing Xray JSON: %w", err)
	}
	if root == nil {
		return existingContent, nil
	}

	// 1. Remove api inbound
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		var newInbounds []interface{}
		for _, inb := range inbs {
			if m, ok := inb.(map[string]interface{}); ok {
				if tag, ok := m["tag"].(string); ok && tag == "api" {
					continue
				}
			}
			newInbounds = append(newInbounds, inb)
		}
		root["inbounds"] = newInbounds
	}

	// 2. Remove api routing rule
	if r, ok := root["routing"].(map[string]interface{}); ok && r != nil {
		if rs, ok := r["rules"].([]interface{}); ok {
			var newRules []interface{}
			for _, rule := range rs {
				if rm, ok := rule.(map[string]interface{}); ok {
					isAPIRule := false
					if inbTags, ok := rm["inboundTag"].([]interface{}); ok {
						for _, t := range inbTags {
							if s, ok := t.(string); ok && s == "api" {
								isAPIRule = true
								break
							}
						}
					} else if inbTag, ok := rm["inboundTag"].(string); ok && inbTag == "api" {
						isAPIRule = true
					}
					if isAPIRule && rm["outboundTag"] == "api" {
						continue
					}
				}
				newRules = append(newRules, rule)
			}
			r["rules"] = newRules
		}
	}

	// 3. Remove api object
	delete(root, "api")

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal deprovisioned Xray config: %w", err)
	}
	return string(out), nil
}

