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

	merged, err := SmartMergeMihomo(existingYAML, templateYAML, userRules)
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

	merged, err := SmartMergeXray(existingConfig, templateContent, "05_routing.json", "vless-reality-node", userRules)
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

