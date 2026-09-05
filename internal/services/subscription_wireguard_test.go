package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireguardNodeToOutbound(t *testing.T) {
	// 1. Full node test
	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE=" // 32 'a's
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI=" // 32 bytes
	keyC := "Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2M=" // 32 'c's

	fullNode := &SubscriptionNode{
		Tag:            "wg-node-1",
		Protocol:       "wireguard",
		Server:         "1.2.3.4:51820",
		PublicKey:      keyB,
		SecretKey:      keyA,
		PreSharedKey:   keyC,
		KeepAlive:      25,
		AllowedIPs:     []string{"0.0.0.0/0", "::/0"},
		LocalAddresses: []string{"10.0.0.2", "fd00::2"},
		MTU:            1420,
		Reserved:       []int{1, 2, 3},
	}

	ob, reason := wireguardNodeToOutbound(fullNode)
	if ob == nil || reason != "" {
		t.Fatalf("expected successful conversion, got reason: %s", reason)
	}

	if ob.Protocol != "wireguard" || ob.Tag != "wg-node-1" {
		t.Errorf("unexpected protocol/tag: %s / %s", ob.Protocol, ob.Tag)
	}

	jsonBytes, err := json.Marshal(ob.Settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}
	var settings map[string]interface{}
	_ = json.Unmarshal(jsonBytes, &settings)

	if settings["secretKey"] != keyA {
		t.Errorf("expected secretKey %s, got %v", keyA, settings["secretKey"])
	}
	if settings["mtu"] != float64(1420) {
		t.Errorf("expected mtu 1420, got %v", settings["mtu"])
	}

	peers, ok := settings["peers"].([]interface{})
	if !ok || len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %+v", settings["peers"])
	}
	peer := peers[0].(map[string]interface{})
	if peer["endpoint"] != "1.2.3.4:51820" || peer["publicKey"] != keyB {
		t.Errorf("unexpected peer endpoint/pubkey: %+v", peer)
	}
	if peer["preSharedKey"] != keyC {
		t.Errorf("expected preSharedKey %s, got %v", keyC, peer["preSharedKey"])
	}

	addrs, ok := settings["address"].([]interface{})
	if !ok || len(addrs) != 2 {
		t.Fatalf("expected 2 addresses, got %+v", settings["address"])
	}
	if addrs[0] != "10.0.0.2/32" || addrs[1] != "fd00::2/128" {
		t.Errorf("expected normalized CIDRs, got %+v", addrs)
	}

	// 2. Incomplete node test -> skip reason, no error, nil outbound
	noSecret := &SubscriptionNode{
		Tag:       "wg-missing-sec",
		Server:    "1.2.3.4:51820",
		PublicKey: "pub",
	}
	obNoSec, reasonNoSec := wireguardNodeToOutbound(noSecret)
	if obNoSec != nil || reasonNoSec == "" {
		t.Errorf("expected nil outbound and non-empty reason for missing secretKey")
	}

	noEndpoint := &SubscriptionNode{
		Tag:       "wg-missing-end",
		SecretKey: "sec",
		PublicKey: "pub",
	}
	obNoEnd, reasonNoEnd := wireguardNodeToOutbound(noEndpoint)
	if obNoEnd != nil || reasonNoEnd == "" {
		t.Errorf("expected nil outbound and non-empty reason for missing endpoint")
	}

	noPub := &SubscriptionNode{
		Tag:       "wg-missing-pub",
		SecretKey: "sec",
		Server:    "1.2.3.4:51820",
	}
	obNoPub, reasonNoPub := wireguardNodeToOutbound(noPub)
	if obNoPub != nil || reasonNoPub == "" {
		t.Errorf("expected nil outbound and non-empty reason for missing publicKey")
	}

	// 3. Optional fields omitted when empty
	minimalNode := &SubscriptionNode{
		Tag:       "wg-minimal",
		Server:    "1.2.3.4:51820",
		PublicKey: keyB,
		SecretKey: keyA,
	}
	obMin, reasonMin := wireguardNodeToOutbound(minimalNode)
	if obMin == nil || reasonMin != "" {
		t.Fatalf("expected minimal node success: %s", reasonMin)
	}

	minJSON, _ := json.Marshal(obMin.Settings)
	var minSettings map[string]interface{}
	_ = json.Unmarshal(minJSON, &minSettings)

	for _, key := range []string{"mtu", "reserved", "address"} {
		if _, exists := minSettings[key]; exists {
			t.Errorf("key %s should not exist in minimal settings", key)
		}
	}
	minPeer := minSettings["peers"].([]interface{})[0].(map[string]interface{})
	for _, key := range []string{"preSharedKey", "keepAlive", "allowedIPs"} {
		if _, exists := minPeer[key]; exists {
			t.Errorf("key %s should not exist in minimal peer", key)
		}
	}

	// 4. Invalid keys tests
	invalidSecretNode := &SubscriptionNode{
		Tag:       "wg-invalid-sec",
		Server:    "1.2.3.4:51820",
		PublicKey: keyB,
		SecretKey: "not-base64!",
	}
	obInvSec, reasonInvSec := wireguardNodeToOutbound(invalidSecretNode)
	if obInvSec != nil || !strings.Contains(reasonInvSec, "secretKey") {
		t.Errorf("expected rejection of invalid secretKey, got ob: %v, reason: %s", obInvSec, reasonInvSec)
	}

	invalidPubNode := &SubscriptionNode{
		Tag:       "wg-invalid-pub",
		Server:    "1.2.3.4:51820",
		PublicKey: "short",
		SecretKey: keyA,
	}
	obInvPub, reasonInvPub := wireguardNodeToOutbound(invalidPubNode)
	if obInvPub != nil || !strings.Contains(reasonInvPub, "publicKey") {
		t.Errorf("expected rejection of invalid publicKey, got ob: %v, reason: %s", obInvPub, reasonInvPub)
	}

	invalidPskNode := &SubscriptionNode{
		Tag:          "wg-invalid-psk",
		Server:       "1.2.3.4:51820",
		PublicKey:    keyB,
		SecretKey:    keyA,
		PreSharedKey: "wrong-length-psk",
	}
	obInvPsk, reasonInvPsk := wireguardNodeToOutbound(invalidPskNode)
	if obInvPsk != nil || !strings.Contains(reasonInvPsk, "preSharedKey") {
		t.Errorf("expected rejection of invalid preSharedKey, got ob: %v, reason: %s", obInvPsk, reasonInvPsk)
	}
}

func TestWireguardReserved(t *testing.T) {
	// From slice
	res1 := decodeWireguardReserved([]int{1, 2, 3})
	if len(res1) != 3 || res1[0] != 1 || res1[1] != 2 || res1[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", res1)
	}

	// From base64 3-byte string
	b64Valid := base64.StdEncoding.EncodeToString([]byte{10, 20, 30})
	res2 := decodeWireguardReserved(b64Valid)
	if len(res2) != 3 || res2[0] != 10 || res2[1] != 20 || res2[2] != 30 {
		t.Errorf("expected [10 20 30] from base64, got %v", res2)
	}

	// Invalid length string
	b64Invalid := base64.StdEncoding.EncodeToString([]byte{10, 20})
	if res := decodeWireguardReserved(b64Invalid); res != nil {
		t.Errorf("expected nil for 2-byte base64, got %v", res)
	}

	// Nil / empty
	if res := decodeWireguardReserved(nil); res != nil {
		t.Errorf("expected nil for nil input")
	}
}

func TestWireguardLocalAddress(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"10.0.0.2", "10.0.0.2/32"},
		{"10.0.0.2/24", "10.0.0.2/24"},
		{"fd00::2", "fd00::2/128"},
		{"fd00::2/64", "fd00::2/64"},
		{"", ""},
		{"invalid-ip", "invalid-ip"},
	}

	for _, tc := range cases {
		got := normalizeWireguardLocalAddress(tc.input)
		if got != tc.want {
			t.Errorf("normalizeWireguardLocalAddress(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseWireguard_Clash(t *testing.T) {
	// 1. Clash block with preshared-key and allowed-ips
	clashYAML1 := `
  - name: WG-Clash-1
    type: wireguard
    server: wg.example.com
    port: 51820
    ip: 10.0.0.2
    ipv6: fd00::2
    public-key: peer-pub-123
    private-key: priv-key-abc
    preshared-key: psk-xyz
    mtu: 1420
    keepalive: 25
    reserved: [1, 2, 3]
    allowed-ips: ["0.0.0.0/0", "::/0"]
`
	node1 := ParseClashProxyNode(clashYAML1)
	if node1.Protocol != "wireguard" {
		t.Fatalf("expected wireguard protocol, got %s", node1.Protocol)
	}
	if node1.PublicKey != "peer-pub-123" || node1.SecretKey != "priv-key-abc" || node1.PreSharedKey != "psk-xyz" {
		t.Errorf("key mismatch: pub=%s, sec=%s, psk=%s", node1.PublicKey, node1.SecretKey, node1.PreSharedKey)
	}
	if len(node1.LocalAddresses) != 2 || node1.LocalAddresses[0] != "10.0.0.2" || node1.LocalAddresses[1] != "fd00::2" {
		t.Errorf("local addresses mismatch: %v", node1.LocalAddresses)
	}
	if node1.MTU != 1420 || node1.KeepAlive != 25 {
		t.Errorf("mtu/keepalive mismatch: mtu=%d, ka=%d", node1.MTU, node1.KeepAlive)
	}
	if len(node1.Reserved) != 3 || node1.Reserved[0] != 1 || node1.Reserved[1] != 2 || node1.Reserved[2] != 3 {
		t.Errorf("reserved mismatch: %v", node1.Reserved)
	}
	if len(node1.AllowedIPs) != 2 || node1.AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("allowed-ips mismatch: %v", node1.AllowedIPs)
	}

	// 2. Clash block with alternate spelling pre-shared-key and allowed-subnets
	clashYAML2 := `
  - name: WG-Clash-2
    type: wireguard
    server: 1.2.3.4
    port: 51820
    ip: 10.0.0.3
    public-key: pub2
    private-key: priv2
    pre-shared-key: psk2
    allowed-subnets: ["10.0.0.0/8"]
`
	node2 := ParseClashProxyNode(clashYAML2)
	if node2.PreSharedKey != "psk2" {
		t.Errorf("expected pre-shared-key psk2, got %s", node2.PreSharedKey)
	}
	if len(node2.AllowedIPs) != 1 || node2.AllowedIPs[0] != "10.0.0.0/8" {
		t.Errorf("expected allowed-subnets 10.0.0.0/8, got %v", node2.AllowedIPs)
	}
}

func TestParseWireguard_SingBox(t *testing.T) {
	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI="
	keyC := "Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2M="

	singBoxJSON := fmt.Sprintf(`[
  {
    "type": "wireguard",
    "tag": "wg-singbox",
    "server": "singbox.example.com",
    "server_port": 51820,
    "local_address": ["10.0.0.5/32"],
    "private_key": "%s",
    "peer_public_key": "%s",
    "pre_shared_key": "%s",
    "mtu": 1280,
    "reserved": [0, 0, 0]
  }
]`, keyA, keyB, keyC)
	outbounds, err := parseSingBoxJSON([]byte(singBoxJSON))
	if err != nil {
		t.Fatalf("parseSingBoxJSON failed: %v", err)
	}
	if len(outbounds) != 1 {
		t.Fatalf("expected 1 outbound, got %d", len(outbounds))
	}
	ob := outbounds[0]
	if ob.Protocol != "wireguard" || ob.Tag != "wg-singbox" {
		t.Errorf("unexpected protocol/tag: %s / %s", ob.Protocol, ob.Tag)
	}
	if ob.Settings["secretKey"] != keyA {
		t.Errorf("expected secretKey %s, got %v", keyA, ob.Settings["secretKey"])
	}
	peers := ob.Settings["peers"].([]interface{})
	peer := peers[0].(map[string]interface{})
	if peer["endpoint"] != "singbox.example.com:51820" || peer["publicKey"] != keyB || peer["preSharedKey"] != keyC {
		t.Errorf("peer mismatch: %+v", peer)
	}
}

func TestWireguardShareLink(t *testing.T) {
	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI="
	keyC := "Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2M="

	// 1. Valid link
	validLink := fmt.Sprintf("wireguard://%s@1.2.3.4:51820?publickey=%s&presharedkey=%s&ip=10.0.0.2&reserved=1,2,3&mtu=1420#MyWireGuard", keyA, keyB, keyC)
	ob := parseShareLink(validLink)
	if ob == nil {
		t.Fatalf("expected parseShareLink to succeed for valid link")
	}
	if ob.Protocol != "wireguard" || ob.Tag != "MyWireGuard" {
		t.Errorf("unexpected outbound: %+v", ob)
	}
	if ob.Settings["secretKey"] != keyA {
		t.Errorf("expected secretKey %s, got %v", keyA, ob.Settings["secretKey"])
	}

	// 2. Broken links -> returns nil, skip reason non-empty
	brokenLinks := []string{
		fmt.Sprintf("wireguard://%s@1.2.3.4:51820?ip=10.0.0.2", keyA),                          // missing public key
		fmt.Sprintf("wireguard://%s@1.2.3.4?publickey=%s", keyA, keyB),                          // missing port
		fmt.Sprintf("wireguard://%s@1.2.3.4:99999?publickey=%s", keyA, keyB),                    // port out of range
		"wireguard://:::invalid-url-here",                                                        // malformed URL
		fmt.Sprintf("wireguard://not-valid-base64@1.2.3.4:51820?publickey=%s", keyB),            // invalid secret key base64
		fmt.Sprintf("wireguard://%s@1.2.3.4:51820?publickey=short", keyA),                       // truncated public key
		fmt.Sprintf("wireguard://%s@1.2.3.4:51820?publickey=%s&presharedkey=short", keyA, keyB), // truncated preshared key
	}

	for _, bl := range brokenLinks {
		parsedOb, reason := parseWireGuardLink(bl)
		if parsedOb != nil {
			t.Errorf("expected nil for broken link %s, got %+v", bl, parsedOb)
		}
		if reason == "" {
			t.Errorf("expected non-empty reason for broken link %s", bl)
		}

		// Also check skipReasonForScheme
		sr := skipReasonForScheme(bl)
		if sr == "" || sr == "неподдерживаемый протокол или невалидный URL" {
			t.Errorf("expected wireguard specific skip reason for %s, got %s", bl, sr)
		}
	}
}

func TestParseWireguard_Mixed(t *testing.T) {
	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI="
	mixedContent := fmt.Sprintf(`
vless://uuid-1@server1.com:443?security=none#VLESS-1
wireguard://%s@1.2.3.4:99999?publickey=%s#Broken-WG
trojan://pass-2@server2.com:443#Trojan-2
`, keyA, keyB)
	sub := &Subscription{}
	outbounds, skipReasons, err := parseSubscriptionBody([]byte(mixedContent), "", sub)
	if err != nil {
		t.Fatalf("parseSubscriptionBody failed: %v", err)
	}
	if len(outbounds) != 2 {
		t.Errorf("expected 2 valid outbounds, got %d", len(outbounds))
	}
	if len(skipReasons) != 1 {
		t.Errorf("expected 1 skipped item, got %d", len(skipReasons))
	}
	if len(skipReasons) > 0 && skipReasons[0].Reason == "" {
		t.Errorf("expected non-empty skip reason")
	}
}

func TestParseWireguard_Empty(t *testing.T) {
	sub := &Subscription{}
	outbounds, skipReasons, err := parseSubscriptionBody([]byte(""), "", sub)
	if err != nil {
		t.Fatalf("parseSubscriptionBody on empty failed: %v", err)
	}
	if len(outbounds) != 0 {
		t.Errorf("expected 0 outbounds for empty input, got %d", len(outbounds))
	}
	if len(skipReasons) != 0 {
		t.Errorf("expected 0 skipped for empty input, got %d", len(skipReasons))
	}
}

func TestWireguardOrderStable(t *testing.T) {
	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI="
	keyC := "Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2M="
	keyD := "ZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGQ="

	content := fmt.Sprintf(`
wireguard://%s@1.2.3.4:51820?publickey=%s#WG-Alpha
wireguard://%s@5.6.7.8:51820?publickey=%s#WG-Beta
`, keyA, keyB, keyC, keyD)
	sub1 := &Subscription{}
	res1, _, _ := parseSubscriptionBody([]byte(content), "", sub1)
	sub2 := &Subscription{}
	res2, _, _ := parseSubscriptionBody([]byte(content), "", sub2)

	if len(res1) != 2 || len(res2) != 2 {
		t.Fatalf("expected 2 nodes, got %d and %d", len(res1), len(res2))
	}

	if res1[0].Tag != res2[0].Tag || res1[1].Tag != res2[1].Tag {
		t.Errorf("order unstable: run1=[%s, %s], run2=[%s, %s]",
			res1[0].Tag, res1[1].Tag, res2[0].Tag, res2[1].Tag)
	}
}

func TestOutboundsToNodesWireguard(t *testing.T) {
	svc := &SubscriptionService{}
	sub := &Subscription{ID: "sub-1"}

	// 1. Full wireguard outbound
	outbounds := []Outbound{
		{
			Tag:      "wg-out",
			Protocol: "wireguard",
			Settings: map[string]interface{}{
				"secretKey": "my-secret",
				"mtu":       float64(1420),
				"reserved":  []interface{}{1, 2, 3},
				"address":   []interface{}{"10.0.0.2/32"},
				"peers": []interface{}{
					map[string]interface{}{
						"endpoint":     "1.2.3.4:51820",
						"publicKey":    "my-pub",
						"preSharedKey": "my-psk",
					},
				},
			},
		},
	}

	nodes := svc.outboundsToNodes(outbounds, sub)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Protocol != "wireguard" || n.Server != "1.2.3.4:51820" || n.PublicKey != "my-pub" || n.SecretKey != "my-secret" {
		t.Errorf("node fields mismatch: %+v", n)
	}
	if n.PreSharedKey != "my-psk" || n.MTU != 1420 || len(n.Reserved) != 3 || len(n.LocalAddresses) != 1 {
		t.Errorf("node optional fields mismatch: %+v", n)
	}

	// 2. Wireguard outbound without peers -> no panic, empty server
	emptyPeerOutbounds := []Outbound{
		{
			Tag:      "wg-no-peers",
			Protocol: "wireguard",
			Settings: map[string]interface{}{
				"secretKey": "sec",
			},
		},
	}
	nodesNoPeers := svc.outboundsToNodes(emptyPeerOutbounds, sub)
	if len(nodesNoPeers) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodesNoPeers))
	}
	if nodesNoPeers[0].Server != "" {
		t.Errorf("expected empty server when no peers, got %s", nodesNoPeers[0].Server)
	}
}

func TestExtractServerWireguard(t *testing.T) {
	// With peer
	obWithPeer := &Outbound{
		Protocol: "wireguard",
		Settings: map[string]interface{}{
			"peers": []interface{}{
				map[string]interface{}{
					"endpoint": "2.3.4.5:51820",
				},
			},
		},
	}
	if ep := extractServer(obWithPeer); ep != "2.3.4.5:51820" {
		t.Errorf("extractServer = %q, want 2.3.4.5:51820", ep)
	}

	// Without peers
	obNoPeers := &Outbound{
		Protocol: "wireguard",
		Settings: map[string]interface{}{},
	}
	if ep := extractServer(obNoPeers); ep != "" {
		t.Errorf("extractServer = %q, want empty", ep)
	}
}

func TestWriteFragmentWireguard(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subs")
	configDir := filepath.Join(tmpDir, "configs")
	svc := NewSubscriptionService(subDir, configDir, "")

	sub := &Subscription{ID: "sub-wg"}
	outbounds := []Outbound{
		{
			Tag:      "wg-node-1",
			Protocol: "wireguard",
			Settings: map[string]interface{}{
				"secretKey": "sec-1",
				"peers": []interface{}{
					map[string]interface{}{
						"endpoint":  "1.2.3.4:51820",
						"publicKey": "pub-1",
					},
				},
			},
		},
		{
			Tag:      "unsupported-node",
			Protocol: "unsupported_protocol_xyz",
			Settings: map[string]interface{}{},
		},
	}

	fragPath := filepath.Join(configDir, "04_outbounds.sub_wg.json")
	nodes, err := svc.writeFragment(fragPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 metadata nodes, got %d", len(nodes))
	}

	// Read written fragment from disk
	savedBytes, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}
	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(savedBytes, &wrapper); err != nil {
		t.Fatalf("failed to parse fragment JSON: %v", err)
	}

	// Only wireguard should be in allowedOutbounds
	if len(wrapper.Outbounds) != 1 {
		t.Fatalf("expected 1 allowed outbound, got %d", len(wrapper.Outbounds))
	}
	if wrapper.Outbounds[0].Protocol != "wireguard" || wrapper.Outbounds[0].Tag != "wg-node-1" {
		t.Errorf("unexpected outbound saved: %+v", wrapper.Outbounds[0])
	}
}

func TestWireguardHealthNotApplicable(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subs")
	configDir := filepath.Join(tmpDir, "configs")
	subSvc := NewSubscriptionService(subDir, configDir, "")

	sub := &Subscription{
		ID: "sub-health",
		Nodes: []SubscriptionNode{
			{
				Tag:      "wg-health-node",
				Protocol: "wireguard",
				Server:   "1.2.3.4:51820",
			},
		},
	}
	subSvc.subscriptions = []Subscription{*sub}

	hSvc := NewSubscriptionHealthService(tmpDir, subSvc)

	// 1. ForceCheckNode
	h, ok := hSvc.ForceCheckNode("sub-health", "wg-health-node")
	if !ok {
		t.Fatalf("expected ok=true for ForceCheckNode")
	}
	if h.LatencyMs != -2 {
		t.Errorf("expected LatencyMs=-2 for WireGuard, got %d", h.LatencyMs)
	}

	// 2. checkSubscription
	hSvc.checkSubscription(sub)
	allHealth := hSvc.GetHealth("sub-health")
	if nodeH, exists := allHealth["wg-health-node"]; !exists {
		t.Errorf("expected health entry for wg-health-node")
	} else if nodeH.LatencyMs != -2 {
		t.Errorf("expected LatencyMs=-2 in batch check, got %d", nodeH.LatencyMs)
	}
}




