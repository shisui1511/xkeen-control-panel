package services

import (
	"encoding/base64"
	"testing"
)

func TestLooksLikeWgQuickConf(t *testing.T) {
	validWG := `
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 1.2.3.4:51820
`
	if !looksLikeWgQuickConf(validWG) {
		t.Errorf("expected looksLikeWgQuickConf to return true for valid WG conf")
	}

	validAWG := `
# AmneziaWG configuration
[Interface]
Address = 10.1.0.2/32
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Jc = 4
S1 = 15

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
`
	if !looksLikeWgQuickConf(validAWG) {
		t.Errorf("expected looksLikeWgQuickConf to return true for valid AWG conf")
	}

	jsonConfig := `[{"protocol": "wireguard", "tag": "wg1"}]`
	if looksLikeWgQuickConf(jsonConfig) {
		t.Errorf("expected looksLikeWgQuickConf to return false for JSON array")
	}

	clashYAML := `
proxies:
  - name: "WG Node"
    type: wireguard
    server: 1.2.3.4
    port: 51820
`
	if looksLikeWgQuickConf(clashYAML) {
		t.Errorf("expected looksLikeWgQuickConf to return false for Clash YAML")
	}

	shareLink := "wireguard://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=@1.2.3.4:51820?publickey=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb="
	if looksLikeWgQuickConf(shareLink) {
		t.Errorf("expected looksLikeWgQuickConf to return false for share link")
	}
}

func TestParseWgQuickConf_PureWireGuard(t *testing.T) {
	conf := `
[Interface]
PrivateKey = 0000000000000000000000000000000000000000000=
Address = 10.0.0.5, fd00::5/128
DNS = 1.1.1.1, 8.8.8.8
MTU = 1420

[Peer]
PublicKey = 1111111111111111111111111111111111111111111=
PresharedKey = 2222222222222222222222222222222222222222222=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`
	nodes, err := parseWgQuickConf(conf, "mywg")
	if err != nil {
		t.Fatalf("unexpected error parsing pure WG conf: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if node.Protocol != "wireguard" {
		t.Errorf("expected protocol wireguard, got %s", node.Protocol)
	}
	if node.Tag != "mywg" {
		t.Errorf("expected tag 'mywg', got %s", node.Tag)
	}
	if node.Server != "198.51.100.1:51820" {
		t.Errorf("expected server 198.51.100.1:51820, got %s", node.Server)
	}
	if node.SecretKey != "0000000000000000000000000000000000000000000=" {
		t.Errorf("unexpected SecretKey: %s", node.SecretKey)
	}
	if node.PublicKey != "1111111111111111111111111111111111111111111=" {
		t.Errorf("unexpected PublicKey: %s", node.PublicKey)
	}
	if node.PreSharedKey != "2222222222222222222222222222222222222222222=" {
		t.Errorf("unexpected PreSharedKey: %s", node.PreSharedKey)
	}
	if node.MTU != 1420 {
		t.Errorf("expected MTU 1420, got %d", node.MTU)
	}
	if len(node.LocalAddresses) != 2 || node.LocalAddresses[0] != "10.0.0.5/32" || node.LocalAddresses[1] != "fd00::5/128" {
		t.Errorf("unexpected LocalAddresses: %v", node.LocalAddresses)
	}
	if len(node.DNS) != 2 || node.DNS[0] != "1.1.1.1" || node.DNS[1] != "8.8.8.8" {
		t.Errorf("unexpected DNS: %v", node.DNS)
	}
	if len(node.AllowedIPs) != 2 || node.AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("unexpected AllowedIPs: %v", node.AllowedIPs)
	}
	if node.KeepAlive != 25 {
		t.Errorf("expected KeepAlive 25, got %d", node.KeepAlive)
	}
	if node.AWG != nil {
		t.Errorf("expected node.AWG to be nil for pure WireGuard, got: %+v", node.AWG)
	}
}

func TestParseWgQuickConf_AWG31(t *testing.T) {
	conf := `
# AmneziaWG 3.1 configuration with all 13 parameters
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
DNS = 1.1.1.1
MTU = 1360
Jc = 4
Jmin = 40
Jmax = 70
S1 = 15
S2 = 40
S3 = 20
S4 = 30
H1 = 1000000001
H2 = 1000000002
H3 = 1000000003
H4 = 1000000004
I1 = 0a1b2c # should be normalized to uppercase
I2 = 3d4e5f
I3 = 112233
I4 = 445566
I5 = 778899
Version = 3.1
HeaderProtectionKey = secret-header-key
ContentPaddingAddition = 12
RandomTrailers = true
DisableCookies = false
RekeyAfterTime = 120
CustomFutureOption = future-val-123

# Primary Peer
[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.2:51820
AllowedIPs = 0.0.0.0/0
`
	nodes, err := parseWgQuickConf(conf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if node.AWG == nil {
		t.Fatalf("expected node.AWG to be populated")
	}

	awg := node.AWG
	if awg.Jc == nil || *awg.Jc != 4 {
		t.Errorf("expected Jc=4, got %v", awg.Jc)
	}
	if awg.Jmin == nil || *awg.Jmin != 40 {
		t.Errorf("expected Jmin=40, got %v", awg.Jmin)
	}
	if awg.Jmax == nil || *awg.Jmax != 70 {
		t.Errorf("expected Jmax=70, got %v", awg.Jmax)
	}
	if awg.S1 == nil || *awg.S1 != 15 {
		t.Errorf("expected S1=15, got %v", awg.S1)
	}
	if awg.S2 == nil || *awg.S2 != 40 {
		t.Errorf("expected S2=40, got %v", awg.S2)
	}
	if awg.S3 == nil || *awg.S3 != 20 {
		t.Errorf("expected S3=20, got %v", awg.S3)
	}
	if awg.S4 == nil || *awg.S4 != 30 {
		t.Errorf("expected S4=30, got %v", awg.S4)
	}
	if awg.H1 != "1000000001" || awg.H2 != "1000000002" || awg.H3 != "1000000003" || awg.H4 != "1000000004" {
		t.Errorf("unexpected H1..H4: %s, %s, %s, %s", awg.H1, awg.H2, awg.H3, awg.H4)
	}
	// AWGIN-01: I1..I5 uppercase normalization
	if awg.I1 != "0A1B2C" {
		t.Errorf("expected I1 to be uppercase 0A1B2C, got %s", awg.I1)
	}
	if awg.I2 != "3D4E5F" || awg.I3 != "112233" || awg.I4 != "445566" || awg.I5 != "778899" {
		t.Errorf("unexpected I2..I5: %s, %s, %s, %s", awg.I2, awg.I3, awg.I4, awg.I5)
	}
	if awg.Version != "3.1" {
		t.Errorf("expected Version=3.1, got %s", awg.Version)
	}
	if awg.HeaderProtectionKey != "secret-header-key" {
		t.Errorf("expected HeaderProtectionKey='secret-header-key', got %s", awg.HeaderProtectionKey)
	}
	if awg.ContentPaddingAddition == nil || *awg.ContentPaddingAddition != 12 {
		t.Errorf("expected ContentPaddingAddition=12, got %v", awg.ContentPaddingAddition)
	}
	if awg.RandomTrailers == nil || *awg.RandomTrailers != true {
		t.Errorf("expected RandomTrailers=true, got %v", awg.RandomTrailers)
	}
	if awg.DisableCookies == nil || *awg.DisableCookies != false {
		t.Errorf("expected DisableCookies=false, got %v", awg.DisableCookies)
	}
	if awg.RekeyAfterTime == nil || *awg.RekeyAfterTime != 120 {
		t.Errorf("expected RekeyAfterTime=120, got %v", awg.RekeyAfterTime)
	}
	// AWGIN-03: Raw catch-all options
	if val, ok := awg.RawOptions["CustomFutureOption"]; !ok || val != "future-val-123" {
		t.Errorf("expected RawOptions['CustomFutureOption'] to be 'future-val-123', got %v", val)
	}
}

func TestParseWgQuickConf_MultiplePeers(t *testing.T) {
	conf := `
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
Jc = 3
S1 = 10

# Server Alpha
[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.10:51820
AllowedIPs = 0.0.0.0/0

# Server Beta
[Peer]
PublicKey = ccccccccccccccccccccccccccccccccccccccccccc=
Endpoint = 198.51.100.20:51820
AllowedIPs = 10.0.0.0/8
# Peer override
Jc = 5
`
	nodes, err := parseWgQuickConf(conf, "my-sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	if nodes[0].Tag != "Server Alpha" {
		t.Errorf("expected first node tag 'Server Alpha', got %s", nodes[0].Tag)
	}
	if *nodes[0].AWG.Jc != 3 {
		t.Errorf("expected first node Jc=3, got %d", *nodes[0].AWG.Jc)
	}

	if nodes[1].Tag != "Server Beta" {
		t.Errorf("expected second node tag 'Server Beta', got %s", nodes[1].Tag)
	}
	if *nodes[1].AWG.Jc != 5 {
		t.Errorf("expected second node Jc=5 (peer override), got %d", *nodes[1].AWG.Jc)
	}
}

func TestParseSubscriptionBody_WgQuick(t *testing.T) {
	conf := `
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
Jc = 4
S1 = 15
I1 = 0a1b2c

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0
`
	sub := &Subscription{TagPrefix: "wgtest"}
	outbounds, skips, err := parseSubscriptionBody([]byte(conf), "text/plain", sub)
	if err != nil {
		t.Fatalf("unexpected error from parseSubscriptionBody: %v", err)
	}
	if len(skips) != 0 {
		t.Fatalf("expected 0 skips, got %d", len(skips))
	}
	if len(outbounds) != 1 {
		t.Fatalf("expected 1 outbound, got %d", len(outbounds))
	}
	if sub.DetectedFormat != "wg-quick" {
		t.Errorf("expected DetectedFormat 'wg-quick', got %s", sub.DetectedFormat)
	}

	// Verify outboundsToNodes preserves AWG
	svc := &SubscriptionService{}
	nodes := svc.outboundsToNodes(outbounds, sub)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].AWG == nil {
		t.Fatalf("expected nodes[0].AWG to be populated")
	}
	if *nodes[0].AWG.Jc != 4 || *nodes[0].AWG.S1 != 15 || nodes[0].AWG.I1 != "0A1B2C" {
		t.Errorf("mismatched AWG options in converted node: %+v", nodes[0].AWG)
	}

	// Test base64 encoded wg-quick
	b64Conf := base64.StdEncoding.EncodeToString([]byte(conf))
	subB64 := &Subscription{TagPrefix: "wgb64"}
	outboundsB64, _, err := parseSubscriptionBody([]byte(b64Conf), "", subB64)
	if err != nil {
		t.Fatalf("unexpected error from parseSubscriptionBody on base64: %v", err)
	}
	if len(outboundsB64) != 1 {
		t.Fatalf("expected 1 outbound for b64, got %d", len(outboundsB64))
	}
	if subB64.DetectedFormat != "wg-quick" {
		t.Errorf("expected DetectedFormat 'wg-quick', got %s", subB64.DetectedFormat)
	}
}

func TestParseWgQuickConf_AWG15_And_V3Timing(t *testing.T) {
	conf15 := `
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
Jc = 4
J1 = 50
J2 = 100
J3 = 150
Itime = 500

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0
`
	nodes15, err := parseWgQuickConf(conf15, "test15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes15) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes15))
	}
	n15 := nodes15[0]
	if n15.AWG == nil {
		t.Fatalf("expected AWG to be non-nil")
	}
	if *n15.AWG.J1 != 50 || *n15.AWG.J2 != 100 || *n15.AWG.J3 != 150 || *n15.AWG.Itime != 500 {
		t.Errorf("unexpected AWG 1.5 fields: %+v", n15.AWG)
	}
	if n15.Dialect != "1.5" {
		t.Errorf("expected dialect '1.5', got %s", n15.Dialect)
	}
	if n15.AWG.Version != "1.5" {
		t.Errorf("expected inferred version '1.5', got %s", n15.AWG.Version)
	}

	confV3 := `
[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
HeaderProtectionKey = secret-hpk
RekeyTimeout = 120
RejectAfterTime = 3600
KeepaliveTimeout = 25
MaxHandshakeAttempts = 5

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.2:51820
AllowedIPs = 0.0.0.0/0
`
	nodesV3, err := parseWgQuickConf(confV3, "testv3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodesV3) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodesV3))
	}
	nV3 := nodesV3[0]
	if nV3.AWG == nil {
		t.Fatalf("expected AWG to be non-nil")
	}
	if *nV3.AWG.RekeyTimeout != 120 || *nV3.AWG.RejectAfterTime != 3600 ||
		*nV3.AWG.KeepaliveTimeout != 25 || *nV3.AWG.MaxHandshakeAttempts != 5 {
		t.Errorf("unexpected v3 timing fields: %+v", nV3.AWG)
	}
	if nV3.Dialect != "3.1" {
		t.Errorf("expected dialect '3.1', got %s", nV3.Dialect)
	}
	if nV3.AWG.Version != "3.1" {
		t.Errorf("expected auto-inferred version '3.1', got %s", nV3.AWG.Version)
	}
}

func TestNormalizeAWGInitPacket(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0a1b2c", "0A1B2C"},
		{"0X01", "0X01"},
		{"<b 0xf1a0><c>", "<b 0xf1a0><c>"},
		{"<r 16><t>", "<r 16><t>"},
		{" <b 0xa1><c> ", "<b 0xa1><c>"},
		{"", ""},
	}

	for _, tc := range tests {
		got := normalizeAWGInitPacket(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeAWGInitPacket(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseWgQuickConf_Comments(t *testing.T) {
	conf := `# Interface Header Comment
[Interface]
# Interface internal comment
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32

# Above Peer 1
[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0

[Peer]
# Inside Peer 2
PublicKey = ccccccccccccccccccccccccccccccccccccccccccc=
Endpoint = 198.51.100.2:51820
AllowedIPs = 0.0.0.0/0
`
	nodes, err := parseWgQuickConf(conf, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Name != "Above Peer 1" {
		t.Errorf("expected node 0 name 'Above Peer 1', got %q", nodes[0].Name)
	}
	if nodes[1].Name != "Inside Peer 2" {
		t.Errorf("expected node 1 name 'Inside Peer 2', got %q", nodes[1].Name)
	}
}

func TestParseWgQuickConf_PeerOverrides_VersionAndTiming(t *testing.T) {
	conf := `[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
Version = 2.0
J1 = 10
Itime = 50
RekeyTimeout = 60

[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0
Version = 3.1
J1 = 20
Itime = 100
RekeyTimeout = 120
RejectAfterTime = 500
`
	nodes, err := parseWgQuickConf(conf, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	awg := nodes[0].AWG
	if awg == nil {
		t.Fatalf("expected non-nil AWG")
	}
	if awg.Version != "3.1" {
		t.Errorf("expected Version '3.1', got %q", awg.Version)
	}
	if awg.J1 == nil || *awg.J1 != 20 {
		t.Errorf("expected J1 20, got %v", awg.J1)
	}
	if awg.Itime == nil || *awg.Itime != 100 {
		t.Errorf("expected Itime 100, got %v", awg.Itime)
	}
	if awg.RekeyTimeout == nil || *awg.RekeyTimeout != 120 {
		t.Errorf("expected RekeyTimeout 120, got %v", awg.RekeyTimeout)
	}
	if awg.RejectAfterTime == nil || *awg.RejectAfterTime != 500 {
		t.Errorf("expected RejectAfterTime 500, got %v", awg.RejectAfterTime)
	}
}



