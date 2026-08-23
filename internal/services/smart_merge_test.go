package services

import (
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
