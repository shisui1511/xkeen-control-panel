package services

import (
	"strings"
	"testing"
)

func TestFindTopLevelSection_Basic(t *testing.T) {
	yaml := `port: 7890
proxies:
  - name: A
  - name: B
proxy-groups:
  - name: PROXY
`
	lines := strings.Split(yaml, "\n")
	start, end, indent := findTopLevelSection(lines, "proxies")
	if start != 1 {
		t.Errorf("expected start=1, got %d", start)
	}
	if end != 4 {
		t.Errorf("expected end=4, got %d", end)
	}
	if indent != 2 {
		t.Errorf("expected indent=2, got %d", indent)
	}
}

func TestFindTopLevelSection_NotFound(t *testing.T) {
	yaml := `port: 7890
rules: []
`
	lines := strings.Split(yaml, "\n")
	start, _, _ := findTopLevelSection(lines, "proxies")
	if start != -1 {
		t.Errorf("expected start=-1, got %d", start)
	}
}

func TestExtractProxyBlocks(t *testing.T) {
	yaml := `port: 7890
proxies:
  - name: Alpha
    type: vless
    server: a.com
  - name: 'Beta Proxy'
    type: trojan
    server: b.com
  - name: "Gamma"
    type: vmess
rules: []
`
	lines := strings.Split(yaml, "\n")
	start, end, indent := findTopLevelSection(lines, "proxies")
	blocks := extractProxyBlocks(lines, start, end, indent)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Name != "Alpha" {
		t.Errorf("expected first name=Alpha, got %q", blocks[0].Name)
	}
	if blocks[1].Name != "Beta Proxy" {
		t.Errorf("expected second name=Beta Proxy, got %q", blocks[1].Name)
	}
	if blocks[2].Name != "Gamma" {
		t.Errorf("expected third name=Gamma, got %q", blocks[2].Name)
	}
}

func TestReplaceMihomoProxies_RemoveAndAdd(t *testing.T) {
	yaml := `port: 7890
proxies:
  - name: Manual
    type: ss
    server: keep.example.com
  - name: SUB-A
    type: vless
    server: old.example.com
  - name: SUB-B
    type: vless
    server: old2.example.com
rules: []
`
	newBlocks := []string{
		`  - name: SUB-A
    type: vless
    server: new.example.com
    port: 443`,
		`  - name: SUB-C
    type: trojan
    server: new3.example.com`,
	}

	result := ReplaceMihomoProxies(yaml, []string{"SUB-A", "SUB-B"}, newBlocks)

	// Manual должен остаться.
	if !strings.Contains(result, "- name: Manual") {
		t.Error("Manual proxy should be preserved")
	}
	if !strings.Contains(result, "keep.example.com") {
		t.Error("Manual proxy server should be preserved")
	}

	// Старые SUB-A и SUB-B должны быть удалены.
	if strings.Contains(result, "old.example.com") {
		t.Error("old SUB-A server should be removed")
	}
	if strings.Contains(result, "old2.example.com") {
		t.Error("old SUB-B server should be removed")
	}

	// Новые SUB-A и SUB-C должны быть добавлены.
	if !strings.Contains(result, "new.example.com") {
		t.Error("new SUB-A should be added")
	}
	if !strings.Contains(result, "new3.example.com") {
		t.Error("SUB-C should be added")
	}

	// rules: [] должны остаться.
	if !strings.Contains(result, "rules: []") {
		t.Error("rules section should be preserved")
	}
}

func TestReplaceMihomoProxies_NoProxiesSection(t *testing.T) {
	yaml := `port: 7890
rules: []
`
	newBlocks := []string{
		`  - name: New
    type: vless
    server: x.com`,
	}
	result := ReplaceMihomoProxies(yaml, nil, newBlocks)
	if !strings.Contains(result, "proxies:") {
		t.Error("proxies: section should be added")
	}
	if !strings.Contains(result, "- name: New") {
		t.Error("new block should be added")
	}
}

func TestUpdateMihomoGroupProxies_AddRemove(t *testing.T) {
	yaml := `proxies:
  - name: P1
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - P1
      - P2
      - DIRECT
rules: []
`
	result := UpdateMihomoGroupProxies(yaml, "PROXY", []string{"P3", "P1"}, []string{"P2"})

	// P2 удалён.
	if strings.Contains(result, "      - P2") {
		t.Error("P2 should be removed from group")
	}
	// P1 не дублирован.
	count := strings.Count(result, "      - P1")
	if count != 1 {
		t.Errorf("P1 should appear exactly once, got %d", count)
	}
	// P3 добавлен.
	if !strings.Contains(result, "- P3") {
		t.Error("P3 should be added to group")
	}
	// DIRECT остался.
	if !strings.Contains(result, "- DIRECT") {
		t.Error("DIRECT should be preserved")
	}
}

func TestUpdateMihomoGroupProxies_GroupNotFound(t *testing.T) {
	yaml := `proxy-groups:
  - name: AUTO
    type: url-test
`
	result := UpdateMihomoGroupProxies(yaml, "MISSING", []string{"X"}, nil)
	if result != yaml {
		t.Error("content should be unchanged if group not found")
	}
}

func TestUpdateMihomoGroupProxies_EmptyProxies(t *testing.T) {
	yaml := `proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - P1
      - P2
`
	result := UpdateMihomoGroupProxies(yaml, "PROXY", nil, []string{"P1", "P2"})
	if strings.Contains(result, "proxies:") {
		t.Error("proxies: section should be completely removed if empty")
	}
	if !strings.Contains(result, "name: PROXY") {
		t.Error("group name should be preserved")
	}
}


func TestYamlSafeScalar(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Simple", "Simple"},
		{"With Space", "'With Space'"},
		{"With:Colon", "'With:Colon'"},
		{"123", "'123'"},
		{"true", "'true'"},
		{"null", "'null'"},
		{"", "''"},
		{"with'quote", "'with''quote'"},
		{"🇷🇺 Russia", "'🇷🇺 Russia'"},
	}
	for _, tt := range tests {
		got := yamlSafeScalar(tt.in)
		if got != tt.want {
			t.Errorf("yamlSafeScalar(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReplaceMihomoProxyProvider_AddUpdateDelete(t *testing.T) {
	yaml := `port: 7890
proxy-providers:
  sub_1:
    type: http
    url: http://example.com/1
  sub_2:
    type: http
    url: http://example.com/2
rules: []
`
	// Test update existing provider
	newBlock := `  sub_2:
    type: http
    url: http://example.com/2_new`
	result := ReplaceMihomoProxyProvider(yaml, "sub_2", newBlock)
	if !strings.Contains(result, "http://example.com/2_new") {
		t.Error("sub_2 should be updated")
	}
	if strings.Contains(result, "http://example.com/2\n") {
		t.Error("old sub_2 url should be replaced")
	}
	if !strings.Contains(result, "sub_1:") {
		t.Error("sub_1 should be preserved")
	}

	// Test add new provider
	newBlock3 := `  sub_3:
    type: http
    url: http://example.com/3`
	result = ReplaceMihomoProxyProvider(yaml, "sub_3", newBlock3)
	if !strings.Contains(result, "sub_3:") || !strings.Contains(result, "http://example.com/3") {
		t.Error("sub_3 should be added")
	}

	// Test delete provider
	result = ReplaceMihomoProxyProvider(yaml, "sub_2", "")
	if strings.Contains(result, "sub_2:") {
		t.Error("sub_2 should be deleted")
	}
	if !strings.Contains(result, "sub_1:") {
		t.Error("sub_1 should be preserved")
	}
}

func TestReplaceMihomoProxyProvider_NoSection(t *testing.T) {
	yaml := `port: 7890
rules: []
`
	newBlock := `  sub_1:
    type: http
    url: http://example.com/1`
	result := ReplaceMihomoProxyProvider(yaml, "sub_1", newBlock)
	if !strings.Contains(result, "proxy-providers:") {
		t.Error("proxy-providers section should be created")
	}
	if !strings.Contains(result, "sub_1:") {
		t.Error("sub_1 should be added")
	}
}

func TestReplaceMihomoProxyProvider_DeleteLast(t *testing.T) {
	yaml := `port: 7890
proxy-providers:
  sub_1:
    type: http
    url: http://example.com/1
rules: []
`
	result := ReplaceMihomoProxyProvider(yaml, "sub_1", "")
	if strings.Contains(result, "proxy-providers:") {
		t.Error("proxy-providers section should be completely removed if empty")
	}
	if !strings.Contains(result, "port: 7890") {
		t.Error("other options should be preserved")
	}
}


func TestUpdateMihomoGroupProviders(t *testing.T) {
	yaml := `proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
    use:
      - sub_1
      - sub_2
rules: []
`
	// Test delete provider from use
	result := UpdateMihomoGroupProviders(yaml, "PROXY", "sub_2", true)
	if strings.Contains(result, "      - sub_2") {
		t.Error("sub_2 should be removed from group use list")
	}
	if !strings.Contains(result, "      - sub_1") {
		t.Error("sub_1 should be preserved in use list")
	}

	// Test add provider to use
	result = UpdateMihomoGroupProviders(yaml, "PROXY", "sub_3", false)
	if !strings.Contains(result, "      - sub_3") {
		t.Error("sub_3 should be added to use list")
	}

	// Test add duplicate provider
	result = UpdateMihomoGroupProviders(yaml, "PROXY", "sub_1", false)
	count := strings.Count(result, "      - sub_1")
	if count != 1 {
		t.Errorf("sub_1 should appear exactly once, got %d", count)
	}

	// Test delete last provider from use (section use: should be deleted)
	yamlOne := `proxy-groups:
  - name: PROXY
    type: select
    use:
      - sub_1
`
	result = UpdateMihomoGroupProviders(yamlOne, "PROXY", "sub_1", true)
	if strings.Contains(result, "use:") {
		t.Error("use: section should be completely removed if empty")
	}
}
