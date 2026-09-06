package services

import (
	"strings"
	"testing"
)

func hasWarningCode(res PreflightResult, code string) bool {
	for _, w := range res.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func getWarningMessage(res PreflightResult, code string) string {
	for _, w := range res.Warnings {
		if w.Code == code {
			return w.Message
		}
	}
	return ""
}

func TestValidateConfigContent_Syntax(t *testing.T) {
	// Empty content
	res := ValidateConfigContent("mihomo", "config.yaml", "   \n\t ")
	if !hasWarningCode(res, "preflight.syntax") {
		t.Errorf("expected preflight.syntax warning for empty content")
	}
	if !res.Valid || len(res.Errors) != 0 {
		t.Errorf("soft gate: Valid must be true and Errors empty, got %+v", res)
	}

	// Broken YAML
	resYaml := ValidateConfigContent("mihomo", "config.yaml", "port: [unclosed")
	if !hasWarningCode(resYaml, "preflight.syntax") {
		t.Errorf("expected preflight.syntax warning for invalid YAML")
	}

	// Broken JSON
	resJSON := ValidateConfigContent("xray", "05_routing.json", "{broken json")
	if !hasWarningCode(resJSON, "preflight.syntax") {
		t.Errorf("expected preflight.syntax warning for invalid JSON")
	}

	// Unknown kernel
	resUnknown := ValidateConfigContent("unknown", "conf", "some text")
	if len(resUnknown.Warnings) != 0 {
		t.Errorf("expected no warnings for unknown kernel, got %+v", resUnknown.Warnings)
	}
}

func TestValidateConfigContent_RemnawaveHeaders(t *testing.T) {
	// String header - should warn
	badConfig := `
proxy-providers:
  provider1:
    type: http
    url: "https://example.com/sub"
    header: "User-Agent: Clash"
`
	res := ValidateConfigContent("mihomo", "config.yaml", badConfig)
	if !hasWarningCode(res, "preflight.header_not_list") {
		t.Errorf("expected preflight.header_not_list warning when header is a string")
	}

	// List header - should pass
	goodConfig := `
proxy-providers:
  provider1:
    type: http
    url: "https://example.com/sub"
    header:
      - "User-Agent: Clash"
`
	resGood := ValidateConfigContent("mihomo", "config.yaml", goodConfig)
	if hasWarningCode(resGood, "preflight.header_not_list") {
		t.Errorf("unexpected preflight.header_not_list warning when header is a slice")
	}
}

func TestValidateConfigContent_DnsLoop(t *testing.T) {
	// Only loopback nameservers
	loopConfig := `
dns:
  enable: true
  listen: "0.0.0.0:1053"
  nameserver:
    - "127.0.0.1"
    - "127.0.0.1:53"
    - "localhost"
`
	res := ValidateConfigContent("mihomo", "config.yaml", loopConfig)
	if !hasWarningCode(res, "preflight.dns_loop") {
		t.Errorf("expected preflight.dns_loop when nameserver contains only loopback")
	}

	// Upstream pointing to own listen port
	ownListenConfig := `
dns:
  enable: true
  listen: "0.0.0.0:1053"
  nameserver:
    - "192.168.1.1:1053"
`
	resOwn := ValidateConfigContent("mihomo", "config.yaml", ownListenConfig)
	if !hasWarningCode(resOwn, "preflight.dns_loop") {
		t.Errorf("expected preflight.dns_loop when nameserver points to listen port")
	}

	// Valid external nameservers
	goodConfig := `
dns:
  enable: true
  listen: "0.0.0.0:1053"
  nameserver:
    - "8.8.8.8"
    - "1.1.1.1"
`
	resGood := ValidateConfigContent("mihomo", "config.yaml", goodConfig)
	if hasWarningCode(resGood, "preflight.dns_loop") {
		t.Errorf("unexpected preflight.dns_loop with valid upstream nameservers")
	}
}

func TestValidateConfigContent_DnsOverVless(t *testing.T) {
	// Mihomo missing 127.0.0.53
	badMihomo := `
rules:
  - 'GEOIP,private,DIRECT'
`
	resMihomo := ValidateConfigContent("mihomo", "config.yaml", badMihomo)
	if !hasWarningCode(resMihomo, "preflight.dns_over_vless") {
		t.Errorf("expected preflight.dns_over_vless when 127.0.0.53/32 rule is missing in Mihomo")
	}

	// Mihomo with 127.0.0.53
	goodMihomo := `
rules:
  - 'IP-CIDR,127.0.0.53/32,DIRECT,no-resolve'
  - 'GEOIP,private,DIRECT'
`
	resGoodMihomo := ValidateConfigContent("mihomo", "config.yaml", goodMihomo)
	if hasWarningCode(resGoodMihomo, "preflight.dns_over_vless") {
		t.Errorf("unexpected preflight.dns_over_vless when 127.0.0.53 rule is present")
	}

	// Xray 05_routing.json missing 127.0.0.53
	badXray := `{
		"routing": {
			"rules": [
				{ "type": "field", "ip": ["geoip:private"], "outboundTag": "direct" }
			]
		}
	}`
	resXray := ValidateConfigContent("xray", "05_routing.json", badXray)
	if !hasWarningCode(resXray, "preflight.dns_over_vless") {
		t.Errorf("expected preflight.dns_over_vless when 127.0.0.53 rule is missing in 05_routing.json")
	}

	// Xray other file (e.g. 03_outbounds.json) should NOT trigger dns_over_vless
	resXrayOut := ValidateConfigContent("xray", "03_outbounds.json", badXray)
	if hasWarningCode(resXrayOut, "preflight.dns_over_vless") {
		t.Errorf("unexpected preflight.dns_over_vless for non-routing file")
	}

	// Xray 05_routing.json with 127.0.0.53
	goodXray := `{
		"routing": {
			"rules": [
				{ "type": "field", "ip": ["127.0.0.53/32"], "outboundTag": "direct" },
				{ "type": "field", "ip": ["geoip:private"], "outboundTag": "direct" },
				{ "type": "field", "port": "3389", "outboundTag": "direct" }
			]
		}
	}`
	resGoodXray := ValidateConfigContent("xray", "05_routing.json", goodXray)
	if hasWarningCode(resGoodXray, "preflight.dns_over_vless") {
		t.Errorf("unexpected preflight.dns_over_vless when 127.0.0.53/32 rule is present")
	}
}

func TestValidateConfigContent_LanRdp(t *testing.T) {
	// Mihomo missing LAN/RDP
	badMihomo := `
rules:
  - 'MATCH,proxy'
`
	resMihomo := ValidateConfigContent("mihomo", "config.yaml", badMihomo)
	if !hasWarningCode(resMihomo, "preflight.lan_rdp") {
		t.Errorf("expected preflight.lan_rdp when LAN/RDP bypass is missing")
	}

	// Mihomo with LAN and RDP
	goodMihomo := `
rules:
  - 'IP-CIDR,127.0.0.53/32,DIRECT,no-resolve'
  - 'GEOIP,private,DIRECT'
  - 'DST-PORT,3389,DIRECT'
  - 'MATCH,proxy'
`
	resGoodMihomo := ValidateConfigContent("mihomo", "config.yaml", goodMihomo)
	if hasWarningCode(resGoodMihomo, "preflight.lan_rdp") {
		t.Errorf("unexpected preflight.lan_rdp when LAN and 3389 are present")
	}

	// Xray missing LAN/RDP
	badXray := `{
		"routing": {
			"rules": [
				{ "type": "field", "outboundTag": "proxy" }
			]
		}
	}`
	resXray := ValidateConfigContent("xray", "05_routing.json", badXray)
	if !hasWarningCode(resXray, "preflight.lan_rdp") {
		t.Errorf("expected preflight.lan_rdp when LAN/RDP rules are missing in Xray")
	}
}

func TestValidateConfigContent_PortConflicts(t *testing.T) {
	// Duplicate ports within Mihomo
	dupMihomo := `
port: 7890
socks-port: 7890
`
	resDup := ValidateConfigContent("mihomo", "config.yaml", dupMihomo)
	if !hasWarningCode(resDup, "preflight.port_conflict") {
		t.Errorf("expected preflight.port_conflict for duplicate ports")
	}

	// Reserved port conflict: 5000 (router admin)
	reservedMihomo := `
tproxy-port: 5000
`
	resRes := ValidateConfigContent("mihomo", "config.yaml", reservedMihomo)
	if !hasWarningCode(resRes, "preflight.port_conflict") {
		t.Errorf("expected preflight.port_conflict for reserved router port 5000")
	}

	// Neighboring ports: 4999 and 5002 - NO conflict
	safeMihomo := `
tproxy-port: 4999
mixed-port: 5002
`
	resSafe := ValidateConfigContent("mihomo", "config.yaml", safeMihomo)
	for _, w := range resSafe.Warnings {
		if w.Code == "preflight.port_conflict" && strings.Contains(w.Message, "reserved") {
			t.Errorf("unexpected reserved port conflict for 4999 / 5002: %s", w.Message)
		}
	}

	// Mixed TProxy and Redir mode
	mixedMode := `
redir-port: 7892
tproxy-port: 7893
`
	resMixed := ValidateConfigContent("mihomo", "config.yaml", mixedMode)
	if !hasWarningCode(resMixed, "preflight.port_conflict") {
		t.Errorf("expected preflight.port_conflict notice for mixed redir and tproxy")
	}
}

func TestValidateConfigContent_AmneziaWgOptions(t *testing.T) {
	// Flat fields on wireguard proxy
	flatAwg := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    jc: 5
    jmin: 40
    jmax: 70
`
	resFlat := ValidateConfigContent("mihomo", "config.yaml", flatAwg)
	if !hasWarningCode(resFlat, "preflight.awg_flat_fields") {
		t.Errorf("expected preflight.awg_flat_fields warning for flat fields")
	}

	// Jmin >= Jmax
	badJmin := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    amnezia-wg-option:
      jc: 5
      jmin: 80
      jmax: 40
      s1: 20
      s2: 100
      h1: 10
      h2: 20
      h3: 30
      h4: 40
`
	resJmin := ValidateConfigContent("mihomo", "config.yaml", badJmin)
	if !hasWarningCode(resJmin, "preflight.awg_flat_fields") || !strings.Contains(getWarningMessage(resJmin, "preflight.awg_flat_fields"), "jmin") {
		t.Errorf("expected preflight.awg_flat_fields warning about jmin >= jmax")
	}

	// S1 + 56 == S2
	badS1S2 := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    amnezia-wg-option:
      jc: 5
      jmin: 10
      jmax: 40
      s1: 100
      s2: 156
      h1: 10
      h2: 20
      h3: 30
      h4: 40
`
	resS1S2 := ValidateConfigContent("mihomo", "config.yaml", badS1S2)
	if !hasWarningCode(resS1S2, "preflight.awg_flat_fields") || !strings.Contains(getWarningMessage(resS1S2, "preflight.awg_flat_fields"), "s1 + 56") {
		t.Errorf("expected preflight.awg_flat_fields warning about s1 + 56 == s2")
	}

	// H <= 4
	badH := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    amnezia-wg-option:
      jc: 5
      jmin: 10
      jmax: 40
      s1: 20
      s2: 100
      h1: 2
      h2: 20
      h3: 30
      h4: 40
`
	resH := ValidateConfigContent("mihomo", "config.yaml", badH)
	if !hasWarningCode(resH, "preflight.awg_flat_fields") || !strings.Contains(getWarningMessage(resH, "preflight.awg_flat_fields"), "> 4") {
		t.Errorf("expected preflight.awg_flat_fields warning about H <= 4")
	}

	// Duplicate H
	dupH := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    amnezia-wg-option:
      jc: 5
      jmin: 10
      jmax: 40
      s1: 20
      s2: 100
      h1: 20
      h2: 20
      h3: 30
      h4: 40
`
	resDupH := ValidateConfigContent("mihomo", "config.yaml", dupH)
	if !hasWarningCode(resDupH, "preflight.awg_flat_fields") || !strings.Contains(getWarningMessage(resDupH, "preflight.awg_flat_fields"), "unique") {
		t.Errorf("expected preflight.awg_flat_fields warning about duplicate H")
	}

	// Valid AmneziaWG options
	goodAwg := `
proxies:
  - name: "wg1"
    type: wireguard
    server: 1.2.3.4
    port: 51820
    amnezia-wg-option:
      jc: 5
      jmin: 10
      jmax: 40
      s1: 20
      s2: 100
      h1: 10
      h2: 20
      h3: 30
      h4: 40
`
	resGood := ValidateConfigContent("mihomo", "config.yaml", goodAwg)
	if hasWarningCode(resGood, "preflight.awg_flat_fields") {
		t.Errorf("unexpected preflight.awg_flat_fields warning for valid AmneziaWG config: %+v", resGood.Warnings)
	}
}
