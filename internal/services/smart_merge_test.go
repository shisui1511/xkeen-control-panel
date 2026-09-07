package services

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestSmartMergeMihomo(t *testing.T) {
	existingYAML := `
secret: "my-existing-secret"
port: 7890
external-controller: 127.0.0.1:9090
proxies:
  - name: "Existing-Node-1"
    type: vless
    server: example.com
    port: 443
proxy-providers:
  my-provider:
    type: http
    url: "https://provider.com/sub"
    path: "./subs/my-provider.yaml"
`

	templateYAML := `
mode: rule
proxy-groups:
  - name: PROXY
    type: select
    proxies: []
rules:
  - GEOSITE,category-ru,DIRECT
  - MATCH,PROXY
dns:
  enable: true
  fake-ip-filter:
    - "*.lan"
`

	userRules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain",
			Value:   "custom-proxy.com",
			Target:  "proxy",
			Enabled: true,
		},
		{
			ID:      "r2",
			Type:    "domain_suffix",
			Value:   "custom-direct.ru",
			Target:  "direct",
			Enabled: true,
		},
	}

	merged, _, err := SmartMergeMihomo(existingYAML, templateYAML, userRules, false)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}

	// 1. Verify secrets and proxies preserved
	if !strings.Contains(merged, "my-existing-secret") {
		t.Errorf("expected existing secret to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "Existing-Node-1") {
		t.Errorf("expected existing proxies to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "my-provider") {
		t.Errorf("expected existing proxy-providers to be preserved, got:\n%s", merged)
	}

	// 2. Verify safety direct ports injected
	if !strings.Contains(merged, "DST-PORT,3389,DIRECT") {
		t.Errorf("expected RDP 3389 DIRECT safety rule, got:\n%s", merged)
	}
	if !strings.Contains(merged, "DST-PORT,445,DIRECT") {
		t.Errorf("expected SMB 445 DIRECT safety rule, got:\n%s", merged)
	}

	// 3. Verify user rules injected
	if !strings.Contains(merged, "DOMAIN,custom-proxy.com,PROXY") {
		t.Errorf("expected user custom proxy rule, got:\n%s", merged)
	}
	if !strings.Contains(merged, "DOMAIN-SUFFIX,custom-direct.ru,DIRECT") {
		t.Errorf("expected user custom direct rule, got:\n%s", merged)
	}

	// 4. Verify Keenetic fake-ip-filter exclusions
	if !strings.Contains(merged, "+.keenetic.pro") {
		t.Errorf("expected Keenetic fake-ip-filter exclusion, got:\n%s", merged)
	}
}

func TestSmartMergeXray(t *testing.T) {
	existingConfig := `{"routing": {"rules": []}}`

	templateContent := `{
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "outboundTag": "PROXY_TAG",
        "domain": ["geosite:google"]
      }
    ]
  }
}`

	userRules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain",
			Value:   "custom.com",
			Target:  "proxy",
			Enabled: true,
		},
	}

	merged, _, err := SmartMergeXray(existingConfig, templateContent, "05_routing.json", "vless-reality-node", userRules)
	if err != nil {
		t.Fatalf("SmartMergeXray failed: %v", err)
	}

	// 1. Verify PROXY_TAG was replaced with active tag
	if strings.Contains(merged, "PROXY_TAG") {
		t.Errorf("expected PROXY_TAG to be replaced, got:\n%s", merged)
	}
	if !strings.Contains(merged, "vless-reality-node") {
		t.Errorf("expected vless-reality-node in merged Xray JSON, got:\n%s", merged)
	}

	// 2. Verify Keenetic 127.0.0.53 DNS protection
	if !strings.Contains(merged, "127.0.0.53") {
		t.Errorf("expected 127.0.0.53 DNS protection rule, got:\n%s", merged)
	}

	// 3. Verify user custom rules
	if !strings.Contains(merged, "full:custom.com") {
		t.Errorf("expected user custom rule full:custom.com, got:\n%s", merged)
	}
}

func TestUserRulesService(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUserRulesService(tmpDir)

	rules := []UserRule{
		{
			ID:      "1",
			Type:    "domain",
			Value:   "example.org",
			Target:  "proxy",
			Enabled: true,
		},
		{
			ID:      "2",
			Type:    "ip_cidr",
			Value:   "1.2.3.4/32",
			Target:  "direct",
			Enabled: true,
		},
	}

	if err := svc.Save(rules); err != nil {
		t.Fatalf("failed to save user rules: %v", err)
	}

	loaded := svc.List()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 rules loaded, got %d", len(loaded))
	}

	mihomoRules := svc.ToMihomoRules("MY-PROXY")
	if len(mihomoRules) != 2 {
		t.Fatalf("expected 2 mihomo rules, got %d", len(mihomoRules))
	}
	if mihomoRules[0] != "DOMAIN,example.org,MY-PROXY" {
		t.Errorf("unexpected mihomo rule: %s", mihomoRules[0])
	}
	if mihomoRules[1] != "IP-CIDR,1.2.3.4/32,DIRECT,no-resolve" {
		t.Errorf("unexpected mihomo rule: %s", mihomoRules[1])
	}
}

func TestSmartMergeXrayAPIBlock(t *testing.T) {
	// Find a free port for testing
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	testPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	initialConfig := `{
  "inbounds": [
    {
      "tag": "socks-in",
      "port": 10808,
      "protocol": "socks"
    }
  ],
  "outbounds": [
    {
      "tag": "direct",
      "protocol": "freedom"
    }
  ],
  "routing": {
    "rules": [
      {
        "type": "field",
        "outboundTag": "direct",
        "ip": ["127.0.0.53"]
      }
    ]
  }
}`

	// 1. Provisioning on existing config
	prov, err := ProvisionXrayAPIBlock(initialConfig, testPort)
	if err != nil {
		t.Fatalf("ProvisionXrayAPIBlock failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(prov), &parsed); err != nil {
		t.Fatalf("failed to parse provisioned json: %v", err)
	}

	// Verify stats
	if _, ok := parsed["stats"]; !ok {
		t.Errorf("expected stats object in config")
	}

	// Verify api object
	apiObj, ok := parsed["api"].(map[string]interface{})
	if !ok || apiObj["tag"] != "api" {
		t.Errorf("expected api object with tag 'api', got: %+v", parsed["api"])
	}

	// Verify policy.system
	policyMap, _ := parsed["policy"].(map[string]interface{})
	systemMap, _ := policyMap["system"].(map[string]interface{})
	if systemMap["statsOutboundUplink"] != true || systemMap["statsOutboundDownlink"] != true {
		t.Errorf("expected statsOutbound flags true in policy.system: %+v", systemMap)
	}

	// Verify inbounds: socks-in preserved, api inbound added
	inbounds, _ := parsed["inbounds"].([]interface{})
	if len(inbounds) != 2 {
		t.Fatalf("expected 2 inbounds, got %d", len(inbounds))
	}
	var apiInb map[string]interface{}
	var socksInb map[string]interface{}
	for _, inb := range inbounds {
		m := inb.(map[string]interface{})
		if m["tag"] == "api" {
			apiInb = m
		} else if m["tag"] == "socks-in" {
			socksInb = m
		}
	}
	if socksInb == nil {
		t.Errorf("user inbound socks-in was lost")
	}
	if apiInb == nil || apiInb["listen"] != "127.0.0.1" || apiInb["protocol"] != "dokodemo-door" {
		t.Errorf("api inbound invalid: %+v", apiInb)
	}

	// Verify rule ordering: rule 0 is api, rule 1 is DNS protection
	routingMap, _ := parsed["routing"].(map[string]interface{})
	rules, _ := routingMap["rules"].([]interface{})
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	rule0 := rules[0].(map[string]interface{})
	if rule0["outboundTag"] != "api" {
		t.Errorf("expected rule 0 outboundTag to be 'api', got %v", rule0["outboundTag"])
	}
	rule1 := rules[1].(map[string]interface{})
	if rule1["outboundTag"] != "direct" {
		t.Errorf("expected rule 1 outboundTag to be 'direct', got %v", rule1["outboundTag"])
	}

	// 2. Double provisioning idempotency test
	prov2, err := ProvisionXrayAPIBlock(prov, testPort)
	if err != nil {
		t.Fatalf("second ProvisionXrayAPIBlock failed: %v", err)
	}

	var parsed2 map[string]interface{}
	_ = json.Unmarshal([]byte(prov2), &parsed2)
	inbounds2, _ := parsed2["inbounds"].([]interface{})
	apiInboundCount := 0
	for _, inb := range inbounds2 {
		if inb.(map[string]interface{})["tag"] == "api" {
			apiInboundCount++
		}
	}
	if apiInboundCount != 1 {
		t.Errorf("expected exactly 1 api inbound after double provisioning, got %d", apiInboundCount)
	}

	rules2, _ := parsed2["routing"].(map[string]interface{})["rules"].([]interface{})
	apiRuleCount := 0
	for _, r := range rules2 {
		if r.(map[string]interface{})["outboundTag"] == "api" {
			apiRuleCount++
		}
	}
	if apiRuleCount != 1 {
		t.Errorf("expected exactly 1 api rule after double provisioning, got %d", apiRuleCount)
	}

	// 3. Busy port test
	busyLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", testPort))
	if err != nil {
		t.Fatalf("failed to hold test port: %v", err)
	}
	_, busyErr := ProvisionXrayAPIBlock(initialConfig, testPort)
	_ = busyLn.Close()
	if busyErr == nil {
		t.Errorf("expected error when port is busy, got nil")
	}

	// 3b. Idempotency test: when port is already configured in config, busy port (e.g. running Xray) should not trigger conflict
	busyLn2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", testPort))
	if err != nil {
		t.Fatalf("failed to hold test port for idempotency test: %v", err)
	}
	_, idemErr := ProvisionXrayAPIBlock(prov, testPort)
	_ = busyLn2.Close()
	if idemErr != nil {
		t.Errorf("expected success on re-provisioning when port is already configured in existingContent, got error: %v", idemErr)
	}

	// 3c. String port idempotency test
	stringPortConfig := fmt.Sprintf(`{"inbounds":[{"tag":"api","port":"%d"}]}`, testPort)
	busyLn3, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", testPort))
	if err == nil {
		_, strPortErr := ProvisionXrayAPIBlock(stringPortConfig, testPort)
		_ = busyLn3.Close()
		if strPortErr != nil {
			t.Errorf("expected success for string-typed port idempotency, got %v", strPortErr)
		}
	}

	// 4. Deprovisioning test
	deprov, err := DeprovisionXrayAPIBlock(prov)
	if err != nil {
		t.Fatalf("DeprovisionXrayAPIBlock failed: %v", err)
	}

	var parsedDeprov map[string]interface{}
	_ = json.Unmarshal([]byte(deprov), &parsedDeprov)

	// Inbound with tag api is gone
	for _, inb := range parsedDeprov["inbounds"].([]interface{}) {
		if inb.(map[string]interface{})["tag"] == "api" {
			t.Errorf("api inbound still present after deprovision")
		}
	}

	// Rule with outboundTag api is gone
	for _, r := range parsedDeprov["routing"].(map[string]interface{})["rules"].([]interface{}) {
		if r.(map[string]interface{})["outboundTag"] == "api" {
			t.Errorf("api rule still present after deprovision")
		}
	}

	// Stats and policy remain
	if _, ok := parsedDeprov["stats"]; !ok {
		t.Errorf("stats object should remain after deprovision")
	}
	if _, ok := parsedDeprov["policy"]; !ok {
		t.Errorf("policy object should remain after deprovision")
	}

	// 5. Empty config provisioning test
	provEmpty, err := ProvisionXrayAPIBlock("", testPort)
	if err != nil {
		t.Fatalf("ProvisionXrayAPIBlock on empty config failed: %v", err)
	}
	var parsedEmpty map[string]interface{}
	if err := json.Unmarshal([]byte(provEmpty), &parsedEmpty); err != nil {
		t.Fatalf("failed to parse empty provisioned config: %v", err)
	}
	if _, ok := parsedEmpty["api"]; !ok {
		t.Errorf("expected api object in empty provisioned config")
	}
}

func TestSmartMergeXray_PreservesAPIRule(t *testing.T) {
	existingWithAPI := `{
  "inbounds": [
    {
      "tag": "api",
      "port": 10085,
      "protocol": "dokodemo-door"
    }
  ],
  "routing": {
    "rules": [
      {
        "type": "field",
        "inboundTag": ["api"],
        "outboundTag": "api"
      }
    ]
  }
}`

	template := `{
  "routing": {
    "rules": [
      {
        "type": "field",
        "outboundTag": "PROXY_TAG",
        "domain": ["geosite:google"]
      }
    ]
  }
}`

	merged, _, err := SmartMergeXray(existingWithAPI, template, "05_routing.json", "my-proxy", nil)
	if err != nil {
		t.Fatalf("SmartMergeXray failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &parsed); err != nil {
		t.Fatalf("failed to parse merged JSON: %v", err)
	}

	routingObj := parsed
	if r, ok := parsed["routing"].(map[string]interface{}); ok {
		routingObj = r
	}
	rules, ok := routingObj["rules"].([]interface{})
	if !ok || len(rules) == 0 {
		t.Fatalf("expected rules in merged routing, got: %+v", routingObj)
	}

	// Rule 0 MUST be the api routing rule
	rule0, ok := rules[0].(map[string]interface{})
	if !ok {
		t.Fatalf("rule 0 is not a map: %+v", rules[0])
	}
	if rule0["outboundTag"] != "api" {
		t.Errorf("expected rule 0 outboundTag to be 'api', got %v", rule0["outboundTag"])
	}
	inbTags, ok := rule0["inboundTag"].([]interface{})
	if !ok || len(inbTags) == 0 || inbTags[0] != "api" {
		t.Errorf("expected rule 0 inboundTag to be ['api'], got %v", rule0["inboundTag"])
	}
}

func TestSmartMergeMihomo_ConstructorOwnsNodes(t *testing.T) {
	existingYAML := `
proxies:
  - name: "Old-Node"
    type: ss
    server: old.com
    port: 8388
proxy-providers:
  old-provider:
    type: http
    url: "https://old.com/sub"
`
	templateYAML := `
proxies:
  - name: "New-Node"
    type: vless
    server: new.com
    port: 443
proxy-providers:
  new-provider:
    type: http
    url: "https://new.com/sub"
rules:
  - MATCH,DIRECT
`
	// 1. When templateOwnsNodes = true (constructor mode), template proxies & providers win
	mergedConstructor, stats, err := SmartMergeMihomo(existingYAML, templateYAML, nil, true)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}
	if !strings.Contains(mergedConstructor, "New-Node") {
		t.Errorf("expected New-Node to be present when templateOwnsNodes=true, got:\n%s", mergedConstructor)
	}
	if strings.Contains(mergedConstructor, "Old-Node") {
		t.Errorf("expected Old-Node to be replaced when templateOwnsNodes=true, got:\n%s", mergedConstructor)
	}
	if !strings.Contains(mergedConstructor, "new-provider") {
		t.Errorf("expected new-provider to be present when templateOwnsNodes=true, got:\n%s", mergedConstructor)
	}
	if strings.Contains(mergedConstructor, "old-provider") {
		t.Errorf("expected old-provider to be replaced when templateOwnsNodes=true, got:\n%s", mergedConstructor)
	}
	if stats.Proxies != 1 || stats.ProxyProviders != 1 {
		t.Errorf("expected 1 proxy and 1 provider in stats, got %+v", stats)
	}

	// 2. When templateOwnsNodes = false (editor mode), existing proxies & providers win
	mergedEditor, _, err := SmartMergeMihomo(existingYAML, templateYAML, nil, false)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}
	if !strings.Contains(mergedEditor, "Old-Node") {
		t.Errorf("expected Old-Node to be preserved when templateOwnsNodes=false, got:\n%s", mergedEditor)
	}
	if strings.Contains(mergedEditor, "New-Node") {
		t.Errorf("expected New-Node to be overwritten when templateOwnsNodes=false, got:\n%s", mergedEditor)
	}
	if !strings.Contains(mergedEditor, "old-provider") {
		t.Errorf("expected old-provider to be preserved when templateOwnsNodes=false, got:\n%s", mergedEditor)
	}
}

func TestSmartMergeMihomo_EmptyAndCorruptExisting(t *testing.T) {
	templateYAML := `
mode: rule
proxies:
  - name: "Node-1"
    type: vless
    server: ex.com
    port: 443
rules:
  - MATCH,DIRECT
`
	userRules := []UserRule{
		{ID: "r1", Type: "domain", Value: "custom.org", Target: "proxy", Enabled: true},
	}

	// Test empty existing
	mergedEmpty, stats, err := SmartMergeMihomo("", templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("failed with empty existing: %v", err)
	}
	if !strings.Contains(mergedEmpty, "Node-1") || !strings.Contains(mergedEmpty, "DOMAIN,custom.org") {
		t.Errorf("empty existing output missing expected rules/proxies:\n%s", mergedEmpty)
	}
	if stats.UserRules != 1 || stats.Proxies != 1 {
		t.Errorf("unexpected stats on empty existing: %+v", stats)
	}

	// Test corrupted existing
	corruptExisting := ":::corrupted YAML content [not valid]:::"
	mergedCorrupt, _, err := SmartMergeMihomo(corruptExisting, templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("failed with corrupt existing: %v", err)
	}
	if !strings.Contains(mergedCorrupt, "Node-1") || !strings.Contains(mergedCorrupt, "DOMAIN,custom.org") {
		t.Errorf("corrupt existing output missing expected rules/proxies:\n%s", mergedCorrupt)
	}
}

func TestSmartMergeMihomo_RuleOrderStable(t *testing.T) {
	templateYAML := `
rules:
  - GEOSITE,category-ru,DIRECT
  - MATCH,PROXY
`
	userRules := []UserRule{
		{ID: "r1", Type: "domain", Value: "first.com", Target: "proxy", Enabled: true},
		{ID: "r2", Type: "domain_suffix", Value: "second.com", Target: "direct", Enabled: true},
	}

	merged1, _, err := SmartMergeMihomo("", templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("merge 1 failed: %v", err)
	}
	merged2, _, err := SmartMergeMihomo("", templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("merge 2 failed: %v", err)
	}

	// Order must be completely stable across multiple merges
	if merged1 != merged2 {
		t.Errorf("repeated merge gave different outputs:\n--- 1 ---\n%s\n--- 2 ---\n%s", merged1, merged2)
	}

	// Check ordering: Safety direct ports -> User rules -> Template rules
	idxSafety := strings.Index(merged1, "DST-PORT,3389,DIRECT")
	idxUser1 := strings.Index(merged1, "DOMAIN,first.com,PROXY")
	idxUser2 := strings.Index(merged1, "DOMAIN-SUFFIX,second.com,DIRECT")
	idxTmpl := strings.Index(merged1, "GEOSITE,category-ru,DIRECT")

	if idxSafety == -1 || idxUser1 == -1 || idxUser2 == -1 || idxTmpl == -1 {
		t.Fatalf("missing one of the expected rules in:\n%s", merged1)
	}

	if !(idxSafety < idxUser1 && idxUser1 < idxUser2 && idxUser2 < idxTmpl) {
		t.Errorf("incorrect rule ordering: safety=%d, user1=%d, user2=%d, tmpl=%d", idxSafety, idxUser1, idxUser2, idxTmpl)
	}
}

func TestSmartMergeMihomo_DuplicateUserRuleKept(t *testing.T) {
	templateYAML := `
rules:
  - DOMAIN,duplicate.com,DIRECT
  - MATCH,PROXY
`
	userRules := []UserRule{
		{ID: "r1", Type: "domain", Value: "duplicate.com", Target: "proxy", Enabled: true},
	}

	merged, _, err := SmartMergeMihomo("", templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Both should be present
	idxUser := strings.Index(merged, "DOMAIN,duplicate.com,PROXY")
	idxTmpl := strings.Index(merged, "DOMAIN,duplicate.com,DIRECT")

	if idxUser == -1 {
		t.Errorf("expected user duplicate rule to be present, got:\n%s", merged)
	}
	if idxTmpl == -1 {
		t.Errorf("expected template duplicate rule to be present, got:\n%s", merged)
	}
	if idxUser >= idxTmpl {
		t.Errorf("expected user rule (%d) to appear BEFORE template rule (%d)", idxUser, idxTmpl)
	}
}

func TestSmartMergeMihomo_Stats(t *testing.T) {
	templateYAML := `
proxies:
  - name: "P1"
    type: direct
  - name: "P2"
    type: direct
proxy-providers:
  sub1:
    type: http
    url: "https://sub1.com"
rules:
  - GEOSITE,google,PROXY
  - MATCH,DIRECT
`
	userRules := []UserRule{
		{ID: "r1", Type: "domain", Value: "u1.com", Target: "proxy", Enabled: true},
		{ID: "r2", Type: "domain", Value: "u2.com", Target: "direct", Enabled: true},
		{ID: "r3", Type: "domain", Value: "", Target: "direct", Enabled: true},        // empty value, should be skipped
		{ID: "r4", Type: "domain", Value: "u4.com", Target: "direct", Enabled: false}, // disabled, should be skipped
	}

	_, stats, err := SmartMergeMihomo("", templateYAML, userRules, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	if stats.Proxies != 2 {
		t.Errorf("expected 2 proxies, got %d", stats.Proxies)
	}
	if stats.ProxyProviders != 1 {
		t.Errorf("expected 1 proxy provider, got %d", stats.ProxyProviders)
	}
	if stats.UserRules != 2 {
		t.Errorf("expected 2 active user rules, got %d", stats.UserRules)
	}
	// Rules: 1 DNS resolver protection + 5 safety ports + 2 user rules + 2 template rules = 10
	expectedRules := 1 + len(SafetyDirectPorts) + 2 + 2
	if stats.Rules != expectedRules {
		t.Errorf("expected %d total rules, got %d", expectedRules, stats.Rules)
	}
}

func TestSmartMergeMihomo_PreservesListeners(t *testing.T) {
	// Case 1: Template contains listeners: -> listeners: appears exactly once and listener is preserved
	existingYAML := `
proxies:
  - name: "P1"
    type: ss
    server: 1.1.1.1
    port: 8388
`
	templateYAML1 := `
proxies:
  - name: "P1"
    type: ss
    server: 1.1.1.1
    port: 8388
listeners:
  - name: "tv-box"
    type: mixed
    port: 7899
    listen: 0.0.0.0
`
	merged1, _, err := SmartMergeMihomo(existingYAML, templateYAML1, nil, true)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}
	if strings.Count(merged1, "listeners:") != 1 {
		t.Errorf("expected listeners: to appear exactly once, got:\n%s", merged1)
	}
	if !strings.Contains(merged1, "tv-box") {
		t.Errorf("expected tv-box listener to be present, got:\n%s", merged1)
	}

	// Case 2: Template contains handwritten exotic block (e.g. type: anytls) -> block is preserved verbatim
	templateYAML2 := `
proxies:
  - name: "P1"
    type: ss
    server: 1.1.1.1
    port: 8388
listeners:
  - name: "exotic-listener"
    type: anytls
    port: 9443
    listen: 0.0.0.0
    certificate: /etc/cert.pem
`
	merged2, _, err := SmartMergeMihomo(existingYAML, templateYAML2, nil, true)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}
	if !strings.Contains(merged2, "type: anytls") || !strings.Contains(merged2, "exotic-listener") {
		t.Errorf("expected exotic anytls listener to be preserved, got:\n%s", merged2)
	}

	// Case 3: Template WITHOUT listeners: and existing config WITH listeners: -> listeners: is preserved (preserve-by-default)
	existingWithListeners := `
proxies:
  - name: "P1"
    type: ss
    server: 1.1.1.1
    port: 8388
listeners:
  - name: "old-listener"
    type: mixed
    port: 7899
`
	templateWithoutListeners := `
proxies:
  - name: "P1"
    type: ss
    server: 1.1.1.1
    port: 8388
`
	merged3, _, err := SmartMergeMihomo(existingWithListeners, templateWithoutListeners, nil, true)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}
	if !strings.Contains(merged3, "listeners:") || !strings.Contains(merged3, "old-listener") {
		t.Errorf("expected existing listeners: to be preserved when template lacks it, got:\n%s", merged3)
	}
}

func TestSmartMergeMihomo_PreserveByDefault(t *testing.T) {
	existingYAML := `
hosts:
  'alpha.local': 192.168.1.50
  'beta.corp': 10.0.0.1
ntp:
  enable: true
  server: time.cloudflare.com
  port: 123
sub-rules:
  my-sub-rule:
    - DOMAIN,internal.corp,DIRECT
    - MATCH,PROXY
tunnels:
  - name: internal-rdp
    address: 192.168.1.100:3389
    target: 10.0.0.15:3389
dns:
  enable: true
  listen: 0.0.0.0:1053
  nameserver-policy:
    'geosite:cn': 223.5.5.5
    '+.corp.internal': 10.0.0.2
  default-nameserver:
    - 77.88.8.8
  fallback:
    - 1.1.1.1
    - 8.8.8.8
  fake-ip-filter:
    - '*.custom.dev'
proxies:
  - name: "Existing-P1"
    type: ss
    server: 1.1.1.1
    port: 8388
`

	templateYAML := `
mode: rule
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Existing-P1
rules:
  - MATCH,PROXY
dns:
  enable: true
  listen: 127.0.0.1:5353
  enhanced-mode: fake-ip
  nameserver:
    - 8.8.8.8
  fake-ip-filter:
    - '*.tmpl.dev'
`

	merged, stats, err := SmartMergeMihomo(existingYAML, templateYAML, nil, false)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}

	// 1. Verify hosts is preserved
	if !strings.Contains(merged, "alpha.local: 192.168.1.50") && !strings.Contains(merged, "alpha.local': 192.168.1.50") {
		t.Errorf("expected hosts to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "beta.corp") {
		t.Errorf("expected beta.corp in hosts to be preserved, got:\n%s", merged)
	}

	// 2. Verify ntp is preserved
	if !strings.Contains(merged, "time.cloudflare.com") {
		t.Errorf("expected ntp server to be preserved, got:\n%s", merged)
	}

	// 3. Verify sub-rules is preserved
	if !strings.Contains(merged, "sub-rules:") || !strings.Contains(merged, "my-sub-rule:") {
		t.Errorf("expected sub-rules to be preserved, got:\n%s", merged)
	}

	// 4. Verify tunnels is preserved
	if !strings.Contains(merged, "tunnels:") || !strings.Contains(merged, "internal-rdp") {
		t.Errorf("expected tunnels to be preserved, got:\n%s", merged)
	}

	// 5. Verify deep dns settings are preserved
	if !strings.Contains(merged, "nameserver-policy:") || !strings.Contains(merged, "+.corp.internal") {
		t.Errorf("expected nameserver-policy to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "default-nameserver:") || !strings.Contains(merged, "77.88.8.8") {
		t.Errorf("expected default-nameserver to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "fallback:") {
		t.Errorf("expected fallback nameservers to be preserved, got:\n%s", merged)
	}

	// 6. Verify combined fake-ip-filter contains custom, template, and Keenetic exclusions
	if !strings.Contains(merged, "*.custom.dev") {
		t.Errorf("expected existing fake-ip-filter to be preserved, got:\n%s", merged)
	}
	if !strings.Contains(merged, "*.tmpl.dev") {
		t.Errorf("expected template fake-ip-filter to be included, got:\n%s", merged)
	}
	if !strings.Contains(merged, "+.keenetic.pro") {
		t.Errorf("expected Keenetic fake-ip exclusion, got:\n%s", merged)
	}

	// 7. Verify dropped keys is empty because all existing keys were preserved
	if len(stats.DroppedKeys) > 0 {
		t.Errorf("expected no dropped keys, got: %v", stats.DroppedKeys)
	}
}

func TestSmartMergeMihomo_RealWorldConfigRoundTrip(t *testing.T) {
	// Full real-world config example with hosts, ntp, sub-rules, listeners, dns policies, etc.
	realWorldConfig := `
port: 7890
socks-port: 7891
redir-port: 7892
tproxy-port: 7893
mixed-port: 7894
allow-lan: true
bind-address: '*'
mode: rule
log-level: info
ipv6: false
secret: 'real-production-secret-xyz'
external-controller: 127.0.0.1:9090

hosts:
  'router.lan': 192.168.1.1
  'storage.nas': 192.168.1.200

ntp:
  enable: true
  server: pool.ntp.org
  port: 123
  interval: 30

sub-rules:
  corp-security:
    - DOMAIN-SUFFIX,corp.internal,DIRECT
    - IP-CIDR,10.100.0.0/16,DIRECT

listeners:
  - name: tv-box-proxy
    type: mixed
    port: 7899
    listen: 0.0.0.0

dns:
  enable: true
  listen: 127.0.0.1:5353
  enhanced-mode: fake-ip
  nameserver-policy:
    'geosite:youtube,google': 8.8.8.8
    'geosite:category-ru': 77.88.8.8
  default-nameserver:
    - 192.168.1.1
  fallback:
    - 9.9.9.9
  fake-ip-filter:
    - '*.lan'
    - 'xbox.*.microsoft.com'

proxies:
  - name: "Prod-WireGuard"
    type: wireguard
    server: 198.51.100.1
    port: 51820
    ip: 10.0.0.2
    public-key: "pubkey123"
    private-key: "privkey123"

proxy-providers:
  premium-sub:
    type: http
    url: "https://example.com/subs/real.yaml"
    path: "./subs/premium.yaml"
    interval: 3600
`

	templateYAML := `
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Prod-WireGuard
    use:
      - premium-sub
rules:
  - GEOSITE,category-ru,DIRECT
  - MATCH,PROXY
`

	merged, stats, err := SmartMergeMihomo(realWorldConfig, templateYAML, nil, false)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed on real-world config: %v", err)
	}

	// Verify all top-level custom sections survived the round-trip verbatim
	requiredStrings := []string{
		"real-production-secret-xyz",
		"router.lan",
		"storage.nas",
		"pool.ntp.org",
		"corp-security",
		"corp.internal",
		"tv-box-proxy",
		"geosite:youtube,google",
		"geosite:category-ru",
		"Prod-WireGuard",
		"premium-sub",
		"+.keenetic.pro",
	}

	for _, s := range requiredStrings {
		if !strings.Contains(merged, s) {
			t.Errorf("round-trip lost critical data: missing %q in merged config:\n%s", s, merged)
		}
	}

	if len(stats.DroppedKeys) > 0 {
		t.Errorf("expected 0 dropped keys on real-world round-trip, got: %v", stats.DroppedKeys)
	}
}

func TestSmartMergeMihomo_DroppedKeys(t *testing.T) {
	// If existing has unknown or obsolete keys that were intentionally stripped or omitted:
	// Here we test that if existing contains keys not retained in result, DroppedKeys tracks them.
	existingWithObsolete := `
secret: "my-secret"
proxies:
  - name: P1
    type: ss
    server: 1.1.1.1
    port: 8388
`
	templateYAML := `
mode: rule
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - P1
rules:
  - MATCH,PROXY
`
	_, stats, err := SmartMergeMihomo(existingWithObsolete, templateYAML, nil, false)
	if err != nil {
		t.Fatalf("SmartMergeMihomo failed: %v", err)
	}

	// Both secret and proxies are preserved, so DroppedKeys should be empty
	if len(stats.DroppedKeys) != 0 {
		t.Errorf("expected empty DroppedKeys when all keys preserved, got: %v", stats.DroppedKeys)
	}
}
