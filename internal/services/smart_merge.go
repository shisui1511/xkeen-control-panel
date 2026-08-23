package services

import (
	"encoding/json"
	"fmt"
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

// SmartMergeMihomo merges a configuration template into an existing Mihomo YAML config.
// It strictly preserves user proxies, proxy-providers, ports and secrets, while applying
// template rules, rule-providers, proxy-groups, and injecting safety bypasses & user rules.
func SmartMergeMihomo(existingYAML string, templateYAML string, userRules []UserRule) (string, error) {
	var existing map[string]interface{}
	var tmpl map[string]interface{}

	if strings.TrimSpace(existingYAML) != "" {
		if err := yaml.Unmarshal([]byte(existingYAML), &existing); err != nil {
			// If existing is corrupted, initialize clean map
			existing = make(map[string]interface{})
		}
	} else {
		existing = make(map[string]interface{})
	}

	if err := yaml.Unmarshal([]byte(templateYAML), &tmpl); err != nil {
		return "", fmt.Errorf("failed to parse template YAML: %w", err)
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
	if existingProxies, ok := existing["proxies"]; ok && existingProxies != nil {
		if exSlice, isSlice := existingProxies.([]interface{}); isSlice && len(exSlice) > 0 {
			result["proxies"] = existingProxies
		}
	}

	if existingProviders, ok := existing["proxy-providers"]; ok && existingProviders != nil {
		if exMap, isMap := existingProviders.(map[string]interface{}); isMap && len(exMap) > 0 {
			result["proxy-providers"] = existingProviders
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

	for _, ur := range userRules {
		if !ur.Enabled || strings.TrimSpace(ur.Value) == "" {
			continue
		}
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

	out, err := yaml.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged YAML: %w", err)
	}

	return string(out), nil
}

// SmartMergeXray merges an Xray routing template into an existing Xray configuration.
// It auto-detects modular routing vs monolithic files, auto-replaces "PROXY_TAG" with active outbound tag,
// and injects Keenetic DNS-over-VLESS protection (127.0.0.53 DIRECT) and user rules.
func SmartMergeXray(existingContent string, templateContent string, targetFilename string, activeOutboundTag string, userRules []UserRule) (string, error) {
	if activeOutboundTag == "" {
		activeOutboundTag = "proxy"
	}

	// Replace placeholder PROXY_TAG in template content
	templateContent = strings.ReplaceAll(templateContent, "PROXY_TAG", activeOutboundTag)

	var tmplObj map[string]interface{}
	if err := json.Unmarshal([]byte(templateContent), &tmplObj); err != nil {
		return "", fmt.Errorf("invalid template JSON: %w", err)
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
	for _, ur := range userRules {
		if !ur.Enabled || strings.TrimSpace(ur.Value) == "" {
			continue
		}
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

	out, err := json.MarshalIndent(outputObj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged Xray JSON: %w", err)
	}

	return string(out), nil
}
