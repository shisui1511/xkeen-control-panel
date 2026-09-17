package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUserRules_GroupField(t *testing.T) {
	svc := NewUserRulesService(t.TempDir())
	rules := []UserRule{
		{
			ID:      "rule-1",
			Type:    "domain_suffix",
			Value:   "rutracker.org",
			Target:  "proxy",
			Group:   "NL-Proxy",
			Enabled: true,
		},
		{
			ID:      "rule-2",
			Type:    "domain",
			Value:   "example.com",
			Target:  "proxy",
			Group:   "", // Should fallback to default group
			Enabled: true,
		},
		{
			ID:      "rule-3",
			Type:    "ip_cidr",
			Value:   "1.2.3.4/32",
			Target:  "direct",
			Group:   "",
			Enabled: true,
		},
	}
	if err := svc.Save(rules); err != nil {
		t.Fatalf("failed to save rules: %v", err)
	}

	mihomoRules := svc.ToMihomoRules("DEFAULT-PROXY")
	if len(mihomoRules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(mihomoRules))
	}
	if mihomoRules[0] != "DOMAIN-SUFFIX,rutracker.org,NL-Proxy" {
		t.Errorf("expected NL-Proxy, got %s", mihomoRules[0])
	}
	if mihomoRules[1] != "DOMAIN,example.com,DEFAULT-PROXY" {
		t.Errorf("expected DEFAULT-PROXY fallback, got %s", mihomoRules[1])
	}
	if mihomoRules[2] != "IP-CIDR,1.2.3.4/32,DIRECT,no-resolve" {
		t.Errorf("expected DIRECT, got %s", mihomoRules[2])
	}

	xrayRules := svc.ToXrayRules("xray-proxy")
	if len(xrayRules) != 3 {
		t.Fatalf("expected 3 xray rules, got %d", len(xrayRules))
	}
	if xrayRules[0]["outboundTag"] != "NL-Proxy" {
		t.Errorf("expected NL-Proxy outboundTag, got %v", xrayRules[0]["outboundTag"])
	}
	if xrayRules[0]["user_rule"] != true {
		t.Errorf("expected user_rule flag on xray rule")
	}
	if xrayRules[1]["outboundTag"] != "xray-proxy" {
		t.Errorf("expected xray-proxy fallback, got %v", xrayRules[1]["outboundTag"])
	}
}

func TestInjectMihomoRules(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 1. Initial config with rules and comments, but without markers
	initialYAML := `# Mihomo User Configuration
port: 7890
mode: rule
# DNS section
dns:
  enable: true
  listen: 0.0.0.0:1053

rules:
  - GEOIP,private,DIRECT
  - MATCH,PROXY
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	svc := NewUserRulesService(tmpDir)
	rules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain_suffix",
			Value:   "rutracker.org",
			Target:  "proxy",
			Group:   "MyGroup",
			Enabled: true,
		},
		{
			ID:      "r2",
			Type:    "port",
			Value:   "8080",
			Target:  "direct",
			Enabled: true,
		},
		{
			ID:      "r3",
			Type:    "domain",
			Value:   "disabled.com",
			Target:  "reject",
			Enabled: false, // disabled rule should not be injected
		},
	}
	if err := svc.Save(rules); err != nil {
		t.Fatalf("failed to save rules: %v", err)
	}

	// First injection: should find rules: and insert markers
	if err := svc.InjectMihomoRules(configPath, "PROXY"); err != nil {
		t.Fatalf("InjectMihomoRules failed: %v", err)
	}

	contentBytes, _ := os.ReadFile(configPath)
	content := string(contentBytes)

	if !strings.Contains(content, UserRulesBeginMarker) || !strings.Contains(content, UserRulesEndMarker) {
		t.Errorf("expected markers in config, got:\n%s", content)
	}
	if !strings.Contains(content, "- DOMAIN-SUFFIX,rutracker.org,MyGroup") {
		t.Errorf("expected injected rule with MyGroup, got:\n%s", content)
	}
	if !strings.Contains(content, "- DST-PORT,8080,DIRECT") {
		t.Errorf("expected injected port rule, got:\n%s", content)
	}
	if strings.Contains(content, "disabled.com") {
		t.Errorf("disabled rule should not be injected")
	}
	// Verify comments and other sections are preserved
	if !strings.Contains(content, "# Mihomo User Configuration") || !strings.Contains(content, "GEOIP,private,DIRECT") {
		t.Errorf("comments and original rules lost after injection:\n%s", content)
	}

	// 2. Second injection (idempotence / update): change rules and inject again
	updatedRules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain",
			Value:   "new-domain.com",
			Target:  "proxy",
			Group:   "OtherGroup",
			Enabled: true,
		},
	}
	if err := svc.Save(updatedRules); err != nil {
		t.Fatalf("failed to save updated rules: %v", err)
	}

	if err := svc.InjectMihomoRules(configPath, "PROXY"); err != nil {
		t.Fatalf("second InjectMihomoRules failed: %v", err)
	}

	contentBytes2, _ := os.ReadFile(configPath)
	content2 := string(contentBytes2)

	// Old rule should be replaced
	if strings.Contains(content2, "rutracker.org") {
		t.Errorf("old rule rutracker.org was not removed after update")
	}
	if !strings.Contains(content2, "- DOMAIN,new-domain.com,OtherGroup") {
		t.Errorf("new rule missing after update:\n%s", content2)
	}
	// Ensure markers are not duplicated
	if strings.Count(content2, UserRulesBeginMarker) != 1 || strings.Count(content2, UserRulesEndMarker) != 1 {
		t.Errorf("markers duplicated in config:\n%s", content2)
	}
}

// TestInjectMihomoRules_EscapesYAMLMetacharacters is a regression test for
// CR-01: a rule Value containing YAML-significant characters (here, ": ",
// which turns an unquoted flow scalar into a mapping) must round-trip through
// InjectMihomoRules + yaml.Unmarshal as a plain string rule entry, not a map.
func TestInjectMihomoRules_EscapesYAMLMetacharacters(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `port: 7890
mode: rule
rules:
  - MATCH,PROXY
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	svc := NewUserRulesService(tmpDir)
	rules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain",
			Value:   "note: this looks innocent",
			Target:  "proxy",
			Enabled: true,
		},
	}
	if err := svc.Save(rules); err != nil {
		t.Fatalf("failed to save rules: %v", err)
	}

	if err := svc.InjectMihomoRules(configPath, "PROXY"); err != nil {
		t.Fatalf("InjectMihomoRules failed: %v", err)
	}

	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read injected config: %v", err)
	}

	var parsed struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(contentBytes, &parsed); err != nil {
		t.Fatalf("injected config.yaml did not parse as rules: []string, got error: %v\ncontent:\n%s", err, contentBytes)
	}

	found := false
	for _, r := range parsed.Rules {
		if r == "DOMAIN,note: this looks innocent,PROXY" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected quoted rule string in rules list, got:\n%s", contentBytes)
	}
}

func TestInjectXrayRules(t *testing.T) {
	tmpDir := t.TempDir()
	routingPath := filepath.Join(tmpDir, "05_routing.json")

	initialJSON := `{
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "inboundTag": ["api"],
        "outboundTag": "api"
      },
      {
        "type": "field",
        "outboundTag": "direct",
        "ip": ["127.0.0.53"]
      },
      {
        "type": "field",
        "outboundTag": "direct",
        "domain": ["geosite:cn"]
      }
    ]
  }
}`
	if err := os.WriteFile(routingPath, []byte(initialJSON), 0644); err != nil {
		t.Fatalf("failed to write initial routing: %v", err)
	}

	svc := NewUserRulesService(tmpDir)
	rules := []UserRule{
		{
			ID:      "r1",
			Type:    "domain_suffix",
			Value:   "rutracker.org",
			Target:  "proxy",
			Group:   "SpecialOutbound",
			Enabled: true,
		},
	}
	if err := svc.Save(rules); err != nil {
		t.Fatalf("failed to save rules: %v", err)
	}

	if err := svc.InjectXrayRules(routingPath, "proxy"); err != nil {
		t.Fatalf("InjectXrayRules failed: %v", err)
	}

	data, _ := os.ReadFile(routingPath)
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("failed to parse injected json: %v", err)
	}

	routingObj := root["routing"].(map[string]interface{})
	rulesArr := routingObj["rules"].([]interface{})

	// API and 127.0.0.53 must be at index 0 and 1, user rule at index 2, geosite at index 3
	if len(rulesArr) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(rulesArr))
	}
	rule0 := rulesArr[0].(map[string]interface{})
	if rule0["outboundTag"] != "api" {
		t.Errorf("expected api rule at index 0, got %v", rule0)
	}
	rule1 := rulesArr[1].(map[string]interface{})
	if rule1["outboundTag"] != "direct" {
		t.Errorf("expected direct 127.0.0.53 rule at index 1, got %v", rule1)
	}
	userRule := rulesArr[2].(map[string]interface{})
	if userRule["outboundTag"] != "SpecialOutbound" || userRule["user_rule"] != true {
		t.Errorf("expected user rule at index 2 with SpecialOutbound, got %v", userRule)
	}
	rule3 := rulesArr[3].(map[string]interface{})
	if rule3["outboundTag"] != "direct" {
		t.Errorf("expected geosite rule at index 3, got %v", rule3)
	}

	// Idempotent repeat: update rule
	rules[0].Value = "updated.org"
	_ = svc.Save(rules)
	if err := svc.InjectXrayRules(routingPath, "proxy"); err != nil {
		t.Fatalf("second InjectXrayRules failed: %v", err)
	}

	data2, _ := os.ReadFile(routingPath)
	_ = json.Unmarshal(data2, &root)
	rulesArr2 := root["routing"].(map[string]interface{})["rules"].([]interface{})
	if len(rulesArr2) != 4 {
		t.Fatalf("expected 4 rules after update, got %d", len(rulesArr2))
	}
	userRule2 := rulesArr2[2].(map[string]interface{})
	domains := userRule2["domain"].([]interface{})
	if domains[0] != "domain:updated.org" {
		t.Errorf("expected domain:updated.org, got %v", domains)
	}
}

func TestUserRules_SaveValidation(t *testing.T) {
	svc := NewUserRulesService(t.TempDir())

	tests := []struct {
		name    string
		rules   []UserRule
		wantErr bool
	}{
		{
			name: "valid rule",
			rules: []UserRule{
				{Type: "domain_suffix", Value: "example.com", Target: "proxy", Group: "ProxyGroup", Comment: "test"},
			},
			wantErr: false,
		},
		{
			name: "newline in value",
			rules: []UserRule{
				{Type: "domain_suffix", Value: "example.com\n- evil.org", Target: "proxy"},
			},
			wantErr: true,
		},
		{
			name: "newline in group",
			rules: []UserRule{
				{Type: "domain_suffix", Value: "example.com", Target: "proxy", Group: "Group\r\nInjected"},
			},
			wantErr: true,
		},
		{
			name: "newline in comment",
			rules: []UserRule{
				{Type: "domain_suffix", Value: "example.com", Target: "proxy", Comment: "Line 1\nLine 2"},
			},
			wantErr: true,
		},
		{
			name: "value exceeds length limit",
			rules: []UserRule{
				{Type: "domain_suffix", Value: strings.Repeat("a", 256), Target: "proxy"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Save(tt.rules)
			if (err != nil) != tt.wantErr {
				t.Errorf("Save() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
