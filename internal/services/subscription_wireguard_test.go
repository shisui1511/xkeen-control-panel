package services

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestWireguardNodeToOutbound(t *testing.T) {
	// 1. Full node test
	fullNode := &SubscriptionNode{
		Tag:            "wg-node-1",
		Protocol:       "wireguard",
		Server:         "1.2.3.4:51820",
		PublicKey:      "peer-pub-key-123",
		SecretKey:      "priv-key-abc",
		PreSharedKey:   "psk-456",
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

	if settings["secretKey"] != "priv-key-abc" {
		t.Errorf("expected secretKey priv-key-abc, got %v", settings["secretKey"])
	}
	if settings["mtu"] != float64(1420) {
		t.Errorf("expected mtu 1420, got %v", settings["mtu"])
	}

	peers, ok := settings["peers"].([]interface{})
	if !ok || len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %+v", settings["peers"])
	}
	peer := peers[0].(map[string]interface{})
	if peer["endpoint"] != "1.2.3.4:51820" || peer["publicKey"] != "peer-pub-key-123" {
		t.Errorf("unexpected peer endpoint/pubkey: %+v", peer)
	}
	if peer["preSharedKey"] != "psk-456" {
		t.Errorf("expected preSharedKey psk-456, got %v", peer["preSharedKey"])
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
		PublicKey: "peer-pub",
		SecretKey: "my-sec",
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
