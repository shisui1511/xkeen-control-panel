package templates_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestZkeenYamlStructure(t *testing.T) {
	data, err := os.ReadFile("mihomo/zkeen.yaml")
	if err != nil {
		t.Fatalf("Failed to read zkeen.yaml: %v", err)
	}

	rawText := string(data)
	if strings.Contains(rawText, "external-ui") {
		t.Error("zkeen.yaml raw text must not contain external-ui")
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse zkeen.yaml as YAML: %v", err)
	}

	// Assert top-level fields
	redirPort, ok := parsed["redir-port"].(int)
	if !ok || redirPort != 5000 {
		t.Errorf("Expected redir-port: 5000, got: %v", parsed["redir-port"])
	}

	tproxyPort, ok := parsed["tproxy-port"].(int)
	if !ok || tproxyPort != 5001 {
		t.Errorf("Expected tproxy-port: 5001, got: %v", parsed["tproxy-port"])
	}

	routingMark, ok := parsed["routing-mark"].(int)
	if !ok || routingMark != 255 {
		t.Errorf("Expected routing-mark: 255, got: %v", parsed["routing-mark"])
	}

	sniffer, ok := parsed["sniffer"].(map[string]interface{})
	if !ok {
		t.Error("Missing sniffer block or not a map")
	} else {
		enable, _ := sniffer["enable"].(bool)
		if !enable {
			t.Error("Expected sniffer.enable: true")
		}
	}

	// Assert rule-providers
	ruleProviders, ok := parsed["rule-providers"].(map[string]interface{})
	if !ok {
		t.Error("Missing rule-providers block or not a map")
	} else {
		if _, ok := ruleProviders["quic@inline"]; !ok {
			t.Error("Missing quic@inline in rule-providers")
		}
		if _, ok := ruleProviders["netbios@inline"]; !ok {
			t.Error("Missing netbios@inline in rule-providers")
		}
	}

	// Assert rules
	rules, ok := parsed["rules"].([]interface{})
	if !ok {
		t.Error("Missing rules list or not a slice")
	} else {
		foundQuic := false
		foundNetbios := false
		for _, ruleVal := range rules {
			ruleStr, ok := ruleVal.(string)
			if !ok {
				continue
			}
			if strings.HasPrefix(ruleStr, "RULE-SET,quic@inline,REJECT") {
				foundQuic = true
			}
			if strings.HasPrefix(ruleStr, "RULE-SET,netbios@inline,REJECT") {
				foundNetbios = true
			}
		}
		if !foundQuic {
			t.Error("Missing RULE-SET,quic@inline,REJECT in rules")
		}
		if !foundNetbios {
			t.Error("Missing RULE-SET,netbios@inline,REJECT in rules")
		}
	}
}

func verifyBlockingRules(t *testing.T, rules []interface{}, filename string) {
	foundQuic := false
	foundNetbios := false
	for _, ruleVal := range rules {
		rule, ok := ruleVal.(map[string]interface{})
		if !ok {
			continue
		}
		port, _ := rule["port"].(string)
		network, _ := rule["network"].(string)
		outboundTag, _ := rule["outboundTag"].(string)
		if port == "443" && network == "udp" && outboundTag == "block" {
			foundQuic = true
		}
		if port == "135,137,138,139" && network == "udp" && outboundTag == "block" {
			foundNetbios = true
		}
	}
	if !foundQuic {
		t.Errorf("%s: missing UDP 443 block rule", filename)
	}
	if !foundNetbios {
		t.Errorf("%s: missing UDP 135,137,138,139 block rule", filename)
	}
}

func TestXrayTemplatesStructure(t *testing.T) {
	// 1. zkeen-routing.json
	routingData, err := os.ReadFile("xray/zkeen-routing.json")
	if err != nil {
		t.Fatalf("Failed to read zkeen-routing.json: %v", err)
	}

	rawRouting := string(routingData)
	if !strings.Contains(rawRouting, "ext:zkeen.dat") {
		t.Error("zkeen-routing.json must contain literal ext:zkeen.dat")
	}

	var routingParsed map[string]interface{}
	if err := json.Unmarshal(routingData, &routingParsed); err != nil {
		t.Fatalf("Failed to parse zkeen-routing.json as JSON: %v", err)
	}

	routing, ok := routingParsed["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("zkeen-routing.json: missing routing object")
	}
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		t.Fatal("zkeen-routing.json: missing routing.rules array")
	}

	verifyBlockingRules(t, rules, "zkeen-routing.json")

	foundTelegram := false
	for _, ruleVal := range rules {
		rule, ok := ruleVal.(map[string]interface{})
		if !ok {
			continue
		}
		ips, _ := rule["ip"].([]interface{})
		hasTelegram := false
		for _, ipVal := range ips {
			if ipStr, ok := ipVal.(string); ok && ipStr == "ext:zkeenip.dat:telegram" {
				hasTelegram = true
				break
			}
		}
		if hasTelegram {
			foundTelegram = true
			ports, _ := rule["port"].(string)
			if ports != "80,443,596-599,1400,5222" {
				t.Errorf("Expected Telegram port list '80,443,596-599,1400,5222', got '%s'", ports)
			}
		}
	}
	if !foundTelegram {
		t.Error("zkeen-routing.json: routing rule for ext:zkeenip.dat:telegram not found")
	}

	// 2. observatory.json
	obsData, err := os.ReadFile("xray/observatory.json")
	if err != nil {
		t.Fatalf("Failed to read observatory.json: %v", err)
	}

	var obsParsed map[string]interface{}
	if err := json.Unmarshal(obsData, &obsParsed); err != nil {
		t.Fatalf("Failed to parse observatory.json as JSON: %v", err)
	}

	obs, ok := obsParsed["observatory"].(map[string]interface{})
	if !ok {
		t.Error("observatory.json: missing observatory object")
	} else {
		subSel, _ := obs["subjectSelector"].([]interface{})
		if len(subSel) == 0 {
			t.Error("observatory.json: subjectSelector must be non-empty")
		}
	}

	routingObj, ok := obsParsed["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: missing routing object")
	}
	balancers, ok := routingObj["balancers"].([]interface{})
	if !ok || len(balancers) == 0 {
		t.Fatal("observatory.json: missing routing.balancers array")
	}

	firstBalancer, ok := balancers[0].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: balancer is not a map")
	}
	strategy, ok := firstBalancer["strategy"].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: balancer missing strategy")
	}
	strategyType, _ := strategy["type"].(string)
	if strategyType != "leastPing" {
		t.Errorf("Expected strategy.type leastPing, got '%s'", strategyType)
	}

	obsRules, ok := routingObj["rules"].([]interface{})
	if !ok {
		t.Fatal("observatory.json: missing routing.rules array")
	}

	verifyBlockingRules(t, obsRules, "observatory.json")

	foundCatchAll := false
	for _, ruleVal := range obsRules {
		rule, ok := ruleVal.(map[string]interface{})
		if !ok {
			continue
		}
		inboundTags, _ := rule["inboundTag"].([]interface{})
		hasRedirectTproxy := false
		for _, tagVal := range inboundTags {
			tagStr, _ := tagVal.(string)
			if tagStr == "redirect" || tagStr == "tproxy" {
				hasRedirectTproxy = true
			}
		}
		if hasRedirectTproxy {
			foundCatchAll = true
			if _, ok := rule["outboundTag"]; ok {
				t.Error("observatory.json: catch-all inbound rule must use balancerTag only, not outboundTag")
			}
			if _, ok := rule["balancerTag"]; !ok {
				t.Error("observatory.json: catch-all inbound rule must have balancerTag")
			}
		}
	}
	if !foundCatchAll {
		t.Error("observatory.json: catch-all rule with inboundTag redirect/tproxy not found")
	}
}

type CatalogTemplate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Filename    string `json:"filename"`
}

type Catalog struct {
	Version   string            `json:"version"`
	Templates []CatalogTemplate `json:"templates"`
}

func TestPresetsConsistency(t *testing.T) {
	// 1. Read catalog.json
	catalogData, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatalf("Failed to read catalog.json: %v", err)
	}

	var cat Catalog
	if err := json.Unmarshal(catalogData, &cat); err != nil {
		t.Fatalf("Failed to parse catalog.json: %v", err)
	}

	// 2. Read default_assets.json
	assetsData, err := os.ReadFile("../services/assets/default_assets.json")
	if err != nil {
		t.Fatalf("Failed to read default_assets.json: %v", err)
	}

	var assetsMap map[string]interface{}
	if err := json.Unmarshal(assetsData, &assetsMap); err != nil {
		t.Fatalf("Failed to parse default_assets.json: %v", err)
	}

	// Helper to find a preset by ID/name in default assets
	findPreset := func(templateType, presetID string) map[string]interface{} {
		typeMap, ok := assetsMap[templateType].(map[string]interface{})
		if !ok {
			return nil
		}
		presets, ok := typeMap["presets"].([]interface{})
		if !ok {
			return nil
		}
		for _, p := range presets {
			pMap, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			if pMap["id"] == presetID {
				return pMap
			}
		}
		return nil
	}

	// Loop over all templates in the catalog
	for _, tmpl := range cat.Templates {
		t.Run(tmpl.Name, func(t *testing.T) {
			// Read template file content
			tmplPath := tmpl.Type + "/" + tmpl.Filename
			tmplData, err := os.ReadFile(tmplPath)
			if err != nil {
				t.Fatalf("Failed to read template file %s: %v", tmplPath, err)
			}
			tmplContent := string(tmplData)

			// Try to find the corresponding preset in assets
			// We map filename without extension to preset id (with zkeen.yaml mapping to zkeen-selective)
			presetID := strings.TrimSuffix(tmpl.Filename, filepath.Ext(tmpl.Filename))
			if tmpl.Filename == "zkeen.yaml" {
				presetID = "zkeen-selective"
			}
			preset := findPreset(tmpl.Type, presetID)
			if preset == nil {
				t.Logf("No matching preset found in default_assets.json for template %s (preset ID: %s)", tmpl.Filename, presetID)
			}

			// We will test replacing PROXY_TAG with standard tags
			testTags := []string{"proxy", "direct", "block", "vless-reality", "shadowsocks"}

			for _, tag := range testTags {
				// Apply replacement
				replacedContent := strings.ReplaceAll(tmplContent, "PROXY_TAG", tag)

				if tmpl.Type == "xray" {
					var config map[string]interface{}
					if err := json.Unmarshal([]byte(replacedContent), &config); err != nil {
						t.Fatalf("Failed to parse applied Xray template as JSON (tag: %s): %v", tag, err)
					}

					// Referential integrity check
					// Extract outbounds
					outboundsMap := make(map[string]bool)
					if outbounds, ok := config["outbounds"].([]interface{}); ok {
						for _, obVal := range outbounds {
							if ob, ok := obVal.(map[string]interface{}); ok {
								if tagStr, ok := ob["tag"].(string); ok {
									outboundsMap[tagStr] = true
								}
							}
						}
					}
					// Add reserved outbounds
					outboundsMap["direct"] = true
					outboundsMap["block"] = true
					outboundsMap["proxy"] = true
					outboundsMap["vless-reality"] = true
					outboundsMap["shadowsocks"] = true

					// Extract balancers
					balancersMap := make(map[string]bool)
					if routing, ok := config["routing"].(map[string]interface{}); ok {
						if balancers, ok := routing["balancers"].([]interface{}); ok {
							for _, bVal := range balancers {
								if b, ok := bVal.(map[string]interface{}); ok {
									if tagStr, ok := b["tag"].(string); ok {
										balancersMap[tagStr] = true
									}
								}
							}
						}

						// Verify routing rules
						if rules, ok := routing["rules"].([]interface{}); ok {
							for _, rVal := range rules {
								if r, ok := rVal.(map[string]interface{}); ok {
									if obTag, ok := r["outboundTag"].(string); ok && obTag != "" {
										if !outboundsMap[obTag] {
											t.Errorf("Rule targets undeclared outboundTag: %s (template: %s)", obTag, tmpl.Filename)
										}
									}
									if balTag, ok := r["balancerTag"].(string); ok && balTag != "" {
										if !balancersMap[balTag] {
											t.Errorf("Rule targets undeclared balancerTag: %s (template: %s)", balTag, tmpl.Filename)
										}
									}
								}
							}
						}
					}
				} else if tmpl.Type == "mihomo" {
					var config map[string]interface{}
					if err := yaml.Unmarshal([]byte(replacedContent), &config); err != nil {
						t.Fatalf("Failed to parse applied Mihomo template as YAML (tag: %s): %v", tag, err)
					}

					// Referential integrity check
					// Extract proxy-groups
					groupsMap := make(map[string]bool)
					if groups, ok := config["proxy-groups"].([]interface{}); ok {
						for _, gVal := range groups {
							if g, ok := gVal.(map[string]interface{}); ok {
								if nameStr, ok := g["name"].(string); ok {
									groupsMap[nameStr] = true
								}
							}
						}
					}

					// Extract proxies
					proxiesMap := make(map[string]bool)
					if proxies, ok := config["proxies"].([]interface{}); ok {
						for _, pVal := range proxies {
							if p, ok := pVal.(map[string]interface{}); ok {
								if nameStr, ok := p["name"].(string); ok {
									proxiesMap[nameStr] = true
								}
							}
						}
					}

					// Declared outbounds includes groups, proxies, builtins and standard tags
					declared := make(map[string]bool)
					for k := range groupsMap {
						declared[k] = true
					}
					for k := range proxiesMap {
						declared[k] = true
					}
					declared["DIRECT"] = true
					declared["REJECT"] = true
					declared["PASS"] = true
					for _, tVal := range testTags {
						declared[tVal] = true
					}

					// Verify groups dependencies
					if groups, ok := config["proxy-groups"].([]interface{}); ok {
						for _, gVal := range groups {
							if g, ok := gVal.(map[string]interface{}); ok {
								if proxiesList, ok := g["proxies"].([]interface{}); ok {
									for _, pNameVal := range proxiesList {
										if pName, ok := pNameVal.(string); ok {
											if !declared[pName] {
												t.Errorf("Group %v targets undeclared proxy/group: %s (template: %s)", g["name"], pName, tmpl.Filename)
											}
										}
									}
								}
							}
						}
					}

					// Verify rules
					if rules, ok := config["rules"].([]interface{}); ok {
						for _, ruleVal := range rules {
							if ruleStr, ok := ruleVal.(string); ok {
								outbound := extractMihomoRuleOutbound(ruleStr)
								if outbound != "" && !declared[outbound] {
									t.Errorf("Rule %q targets undeclared outbound: %s (template: %s)", ruleStr, outbound, tmpl.Filename)
								}
							}
						}
					}
				}
			}
		})
	}
}

func extractMihomoRuleOutbound(ruleStr string) string {
	// If rule contains `),` split it and inspect the rest
	if strings.Contains(ruleStr, "),") {
		parts := strings.Split(ruleStr, "),")
		rest := parts[len(parts)-1]
		outboundParts := strings.Split(rest, ",")
		return strings.TrimSpace(outboundParts[0])
	}
	parts := strings.Split(ruleStr, ",")
	if len(parts) < 2 {
		return ""
	}
	lastPart := strings.TrimSpace(parts[len(parts)-1])
	if lastPart == "no-resolve" {
		if len(parts) >= 3 {
			return strings.TrimSpace(parts[len(parts)-2])
		}
		return ""
	}
	return lastPart
}

func TestCatalogMihomoTemplatesResolve(t *testing.T) {
	data, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatalf("Failed to read catalog.json: %v", err)
	}

	var catalog struct {
		Templates []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Filename string `json:"filename"`
		} `json:"templates"`
	}

	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("Failed to unmarshal catalog.json: %v", err)
	}

	for _, tmpl := range catalog.Templates {
		if tmpl.Type != "mihomo" {
			continue
		}

		t.Run(tmpl.Name, func(t *testing.T) {
			path := filepath.Join("mihomo", tmpl.Filename)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Referenced file %s not found: %v", path, err)
			}

			var parsed map[string]interface{}
			if err := yaml.Unmarshal(content, &parsed); err != nil {
				t.Fatalf("Failed to parse %s as YAML: %v", path, err)
			}

			if _, hasProxies := parsed["proxies"]; hasProxies {
				t.Errorf("Template %s must not declare hardcoded proxies section", path)
			}

			rulesVal, ok := parsed["rules"]
			if !ok {
				t.Fatalf("Template %s is missing rules section", path)
			}

			rulesList, ok := rulesVal.([]interface{})
			if !ok || len(rulesList) == 0 {
				t.Fatalf("Template %s rules must be a non-empty list", path)
			}

			lastRule, ok := rulesList[len(rulesList)-1].(string)
			if !ok || !strings.HasPrefix(lastRule, "MATCH,") {
				t.Errorf("Template %s last rule must start with MATCH,, got: %v", path, rulesList[len(rulesList)-1])
			}
		})
	}
}

func TestReferenceTemplatesTMPL05(t *testing.T) {
	data, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatalf("Failed to read catalog.json: %v", err)
	}

	var catalog struct {
		Templates []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Filename string `json:"filename"`
		} `json:"templates"`
	}

	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("Failed to unmarshal catalog.json: %v", err)
	}

	expectedReference := []string{
		"Mihomo: Умный Антифильтр",
		"Mihomo: Раздельный по сервисам",
		"Mihomo: Мульти-подписка с Remnawave HWID",
		"Mihomo: Gaming & Low-Latency",
		"Mihomo: Облегченный Low-RAM для MIPS/128MB",
		"Mihomo: Полный туннель",
	}

	historicalGeneric := []string{
		"global-proxy.yaml",
		"rule-based.yaml",
		"zkeen.yaml",
	}

	catalogMap := make(map[string]string)
	for _, tmpl := range catalog.Templates {
		catalogMap[tmpl.Name] = tmpl.Filename
	}

	// 1. Проверяем наличие всех 6 эталонных шаблонов
	for _, name := range expectedReference {
		if _, exists := catalogMap[name]; !exists {
			t.Errorf("Catalog missing expected reference template: %s", name)
		}
	}

	// 2. Проверяем сохранность 3 исторических generic шаблонов
	for _, genFile := range historicalGeneric {
		found := false
		for _, fn := range catalogMap {
			if fn == genFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Catalog missing historical generic template: %s", genFile)
		}
	}

	// 3. Проверяем конвенции для каждого из 6 эталонных файлов
	referenceFiles := []string{
		"smart-antifilter.yaml",
		"split-services.yaml",
		"low-memory.yaml",
		"multi-sub-hwid.yaml",
		"gaming-low-latency.yaml",
		"full-tunnel.yaml",
	}

	for _, fn := range referenceFiles {
		t.Run(fn, func(t *testing.T) {
			path := filepath.Join("mihomo", fn)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", path, err)
			}

			var parsed map[string]interface{}
			if err := yaml.Unmarshal(content, &parsed); err != nil {
				t.Fatalf("Failed to parse %s as YAML: %v", path, err)
			}

			// Ни в одном из шести нет ключа proxies
			if _, hasProxies := parsed["proxies"]; hasProxies {
				t.Errorf("%s must not declare proxies", fn)
			}

			// Наличие DST-PORT,3389,DIRECT
			rulesList, ok := parsed["rules"].([]interface{})
			if !ok {
				t.Fatalf("%s rules must be a slice", fn)
			}
			found3389 := false
			for _, r := range rulesList {
				if rStr, ok := r.(string); ok && strings.TrimSpace(rStr) == "DST-PORT,3389,DIRECT" {
					found3389 = true
					break
				}
			}
			if !found3389 {
				t.Errorf("%s must include DST-PORT,3389,DIRECT for admin access protection", fn)
			}

			// Проверка заголовков proxy-providers (списки строк по TMPL-02)
			if providersVal, ok := parsed["proxy-providers"]; ok {
				if providersMap, ok := providersVal.(map[string]interface{}); ok {
					for pName, pData := range providersMap {
						if pMap, ok := pData.(map[string]interface{}); ok {
							if headerVal, ok := pMap["header"]; ok {
								if headerMap, ok := headerVal.(map[string]interface{}); ok {
									for hKey, hVal := range headerMap {
										if _, isList := hVal.([]interface{}); !isList {
											t.Errorf("%s provider %s header %s must be a list of strings, got %T", fn, pName, hKey, hVal)
										}
									}
								}
							}
						}
					}
				}
			}

			// Наличие правила защиты системного резолвера Keenetic (DNS-over-VLESS)
			foundDNSProtect := false
			for _, r := range rulesList {
				if rStr, ok := r.(string); ok && (strings.TrimSpace(rStr) == "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve" || strings.TrimSpace(rStr) == "IP-CIDR,127.0.0.53/32,DIRECT,no-resolve") {
					foundDNSProtect = true
					break
				}
			}
			if !foundDNSProtect {
				t.Errorf("%s must include IP-CIDR,127.0.0.0/8,DIRECT,no-resolve for DNS-over-VLESS resolver protection", fn)
			}

			// Для low-memory.yaml проверяем ровно 1 rule-provider
			if fn == "low-memory.yaml" {
				rpVal, ok := parsed["rule-providers"]
				if !ok {
					t.Errorf("%s must have rule-providers", fn)
				} else if rpMap, ok := rpVal.(map[string]interface{}); !ok || len(rpMap) != 1 {
					t.Errorf("%s must have exactly 1 rule-provider, got %d", fn, len(rpMap))
				}
			}
		})
	}
}

func TestReferenceTemplatesPassPreflightDnsOverVless(t *testing.T) {
	referenceFiles := []string{
		"smart-antifilter.yaml",
		"split-services.yaml",
		"low-memory.yaml",
		"multi-sub-hwid.yaml",
		"gaming-low-latency.yaml",
		"full-tunnel.yaml",
	}

	for _, fn := range referenceFiles {
		t.Run(fn, func(t *testing.T) {
			path := filepath.Join("mihomo", fn)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", path, err)
			}

			res := services.ValidateConfigContent("mihomo", fn, string(content))
			for _, w := range res.Warnings {
				if w.Code == "preflight.dns_over_vless" {
					t.Errorf("Template %s failed preflight with warning: %s", fn, w.Message)
				}
			}
		})
	}
}
