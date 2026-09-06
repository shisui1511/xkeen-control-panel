package services

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionSockoptFields(t *testing.T) {
	sub := &Subscription{
		ID:              "sub-test",
		Name:            "Test Sub",
		SockoptMark:     255,
		SockoptFastOpen: true,
		SockoptMptcp:    true,
		Nodes: []SubscriptionNode{
			{
				Tag:         "sub-test-1",
				Name:        "Node 1",
				Protocol:    "vless",
				DialerProxy: "other-node-tag",
			},
		},
	}

	data, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("failed to marshal subscription: %v", err)
	}

	var decoded Subscription
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal subscription: %v", err)
	}

	if decoded.SockoptMark != 255 {
		t.Errorf("expected SockoptMark=255, got %d", decoded.SockoptMark)
	}
	if !decoded.SockoptFastOpen {
		t.Errorf("expected SockoptFastOpen=true")
	}
	if !decoded.SockoptMptcp {
		t.Errorf("expected SockoptMptcp=true")
	}
	if len(decoded.Nodes) != 1 || decoded.Nodes[0].DialerProxy != "other-node-tag" {
		t.Errorf("expected DialerProxy=other-node-tag, got %+v", decoded.Nodes)
	}
}

func TestSubscriptionBackwardCompat(t *testing.T) {
	// JSON payload generated before Phase 104 without any sockopt or dialer_proxy fields
	oldJSON := `{
		"id": "old-sub",
		"name": "Legacy Subscription",
		"url": "https://example.com/sub",
		"enabled": true,
		"enable_xray": true,
		"nodes": [
			{
				"tag": "old-sub-1",
				"name": "Legacy Node",
				"protocol": "vmess",
				"server": "1.2.3.4:443"
			}
		]
	}`

	var sub Subscription
	if err := json.Unmarshal([]byte(oldJSON), &sub); err != nil {
		t.Fatalf("failed to unmarshal legacy subscription: %v", err)
	}

	if sub.SockoptMark != 0 {
		t.Errorf("expected zero SockoptMark, got %d", sub.SockoptMark)
	}
	if sub.SockoptFastOpen {
		t.Errorf("expected false SockoptFastOpen, got true")
	}
	if sub.SockoptMptcp {
		t.Errorf("expected false SockoptMptcp, got true")
	}
	if len(sub.Nodes) != 1 || sub.Nodes[0].DialerProxy != "" {
		t.Errorf("expected empty DialerProxy, got %q", sub.Nodes[0].DialerProxy)
	}
}

func setupTestStorage(t *testing.T) (*SubscriptionService, string) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	mihomoDir := filepath.Join(tmpDir, "mihomo")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(mihomoDir, 0755)

	svc := NewSubscriptionService(tmpDir, xrayDir, mihomoDir)
	return svc, tmpDir
}

func TestSetNodeDialerProxy(t *testing.T) {
	svc, _ := setupTestStorage(t)

	// Create sub1 with node1 and node2
	sub1 := &Subscription{
		ID:         "sub1",
		Name:       "Sub 1",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node2", Name: "Node 2", Protocol: "vless"},
		},
	}
	if err := svc.Add(sub1); err != nil {
		t.Fatalf("failed to add sub1: %v", err)
	}

	// Create sub2 with node3 and node4
	sub2 := &Subscription{
		ID:         "sub2",
		Name:       "Sub 2",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node3", Name: "Node 3", Protocol: "vmess"},
			{Tag: "node4", Name: "Node 4", Protocol: "vmess"},
		},
	}
	if err := svc.Add(sub2); err != nil {
		t.Fatalf("failed to add sub2: %v", err)
	}

	// 1. Success: set node1 dialerProxy to node3 (cross-subscription)
	if err := svc.SetNodeDialerProxy("sub1", "node1", "node3"); err != nil {
		t.Fatalf("unexpected error setting dialerProxy: %v", err)
	}
	updatedSub1 := svc.Get("sub1")
	if updatedSub1 == nil || updatedSub1.Nodes[0].DialerProxy != "node3" {
		t.Errorf("expected DialerProxy=node3, got %+v", updatedSub1)
	}

	// 2. Success: clear dialerProxy with empty string
	if err := svc.SetNodeDialerProxy("sub1", "node1", ""); err != nil {
		t.Fatalf("unexpected error clearing dialerProxy: %v", err)
	}
	clearedSub1 := svc.Get("sub1")
	if clearedSub1 == nil || clearedSub1.Nodes[0].DialerProxy != "" {
		t.Errorf("expected empty DialerProxy, got %q", clearedSub1.Nodes[0].DialerProxy)
	}

	// 3. Error: source node not found
	if err := svc.SetNodeDialerProxy("sub1", "nonexistent-node", "node3"); err == nil || err.Error() != "node not found" {
		t.Errorf("expected 'node not found', got: %v", err)
	}

	// 4. Error: cannot cascade node to itself
	if err := svc.SetNodeDialerProxy("sub1", "node1", "node1"); err == nil || err.Error() != "cannot cascade node to itself" {
		t.Errorf("expected 'cannot cascade node to itself', got: %v", err)
	}

	// 5. Error: target not available (nonexistent target)
	if err := svc.SetNodeDialerProxy("sub1", "node1", "phantom-node"); err == nil || err.Error() != "target not available" {
		t.Errorf("expected 'target not available', got: %v", err)
	}

	// 6. Error: target already has its own dialerProxy ("chain limited to one level")
	if err := svc.SetNodeDialerProxy("sub2", "node3", "node4"); err != nil {
		t.Fatalf("failed to set node3 dialerProxy: %v", err)
	}
	if err := svc.SetNodeDialerProxy("sub1", "node1", "node3"); err == nil || err.Error() != "chain limited to one level" {
		t.Errorf("expected 'chain limited to one level', got: %v", err)
	}

	// 7. Error: source node is already used as a proxy target
	_ = svc.SetNodeDialerProxy("sub2", "node3", "")
	if err := svc.SetNodeDialerProxy("sub1", "node1", "node3"); err != nil {
		t.Fatalf("failed to set node1 -> node3: %v", err)
	}
	if err := svc.SetNodeDialerProxy("sub2", "node3", "node4"); err == nil || err.Error() != "cannot cascade node that is already used as a proxy target" {
		t.Errorf("expected 'cannot cascade node that is already used as a proxy target', got: %v", err)
	}
}

func TestDialerProxyTargets(t *testing.T) {
	svc, _ := setupTestStorage(t)

	// sub1: node1 (source), node2
	sub1 := &Subscription{
		ID:         "sub1",
		Name:       "Sub 1",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node2", Name: "Node 2", Protocol: "vless"},
		},
	}
	_ = svc.Add(sub1)

	// sub2: node3 (available), node4 (has its own dialerProxy -> excluded)
	sub2 := &Subscription{
		ID:         "sub2",
		Name:       "Sub 2",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node3", Name: "Node 3", Protocol: "vmess"},
			{Tag: "node4", Name: "Node 4", Protocol: "vmess", DialerProxy: "node2"},
		},
	}
	_ = svc.Add(sub2)

	// sub3: disabled Xray -> all nodes excluded
	sub3 := &Subscription{
		ID:         "sub3",
		Name:       "Sub 3",
		Enabled:    true,
		EnableXray: false,
		Nodes: []SubscriptionNode{
			{Tag: "node5", Name: "Node 5", Protocol: "trojan"},
		},
	}
	_ = svc.Add(sub3)

	targets, err := svc.DialerProxyTargets("sub1", "node1")
	if err != nil {
		t.Fatalf("DialerProxyTargets failed: %v", err)
	}

	// Expected targets: node2 (same sub), node3 (other active Xray sub)
	// Excluded: node1 (source itself), node4 (has own cascade), node5 (sub3 has Xray disabled)
	if len(targets) != 2 {
		t.Fatalf("expected exactly 2 targets, got %d: %+v", len(targets), targets)
	}
	if targets[0].Tag != "node2" || targets[1].Tag != "node3" {
		t.Errorf("unexpected targets list: %+v", targets)
	}

	// Test determinism: consecutive call produces identical output
	targets2, _ := svc.DialerProxyTargets("sub1", "node1")
	if len(targets) != len(targets2) || targets[0].Tag != targets2[0].Tag || targets[1].Tag != targets2[1].Tag {
		t.Errorf("targets call is non-deterministic: %+v vs %+v", targets, targets2)
	}

	// Test single sub with no other nodes:
	svcEmpty, _ := setupTestStorage(t)
	singleSub := &Subscription{
		ID:         "single",
		Name:       "Single",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "only-node", Name: "Only Node", Protocol: "vless"},
		},
	}
	_ = svcEmpty.Add(singleSub)

	emptyTargets, err := svcEmpty.DialerProxyTargets("single", "only-node")
	if err != nil {
		t.Fatalf("unexpected error for single node: %v", err)
	}
	if len(emptyTargets) != 0 {
		t.Errorf("expected 0 targets for single node, got %d: %+v", len(emptyTargets), emptyTargets)
	}
}

func TestWriteFragmentSockoptEmpty(t *testing.T) {
	svc, tmpDir := setupTestStorage(t)
	fragPath := filepath.Join(tmpDir, "xray", "02_sub_empty.json")

	sub := &Subscription{
		ID:         "sub-empty",
		Name:       "Empty Sockopt Sub",
		Enabled:    true,
		EnableXray: true,
	}
	outbounds := []Outbound{
		{
			Tag:      "node-1",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
		},
		{
			Tag:      "node-2",
			Protocol: "vmess",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
			StreamSettings: map[string]interface{}{
				"network": "ws",
			},
		},
	}

	nodes, err := svc.writeFragment(fragPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal written fragment: %v", err)
	}

	for i, ob := range parsed.Outbounds {
		if ss, ok := ob["streamSettings"].(map[string]interface{}); ok {
			if _, hasSockopt := ss["sockopt"]; hasSockopt {
				t.Errorf("outbound %d has unexpected sockopt property: %+v", i, ss)
			}
		}
	}
}

func TestWriteFragmentSockopt(t *testing.T) {
	svc, tmpDir := setupTestStorage(t)
	fragPath := filepath.Join(tmpDir, "xray", "02_sub_sockopt.json")

	sub := &Subscription{
		ID:              "sub-sockopt",
		Name:            "Sockopt Sub",
		Enabled:         true,
		EnableXray:      true,
		SockoptMark:     255,
		SockoptFastOpen: true,
		SockoptMptcp:    true,
	}
	outbounds := []Outbound{
		{
			Tag:      "node-tcp",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
		},
		{
			Tag:      "node-ws",
			Protocol: "vmess",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
			StreamSettings: map[string]interface{}{
				"network": "ws",
				"wsSettings": map[string]interface{}{
					"path": "/ws",
				},
			},
		},
	}

	_, err := svc.writeFragment(fragPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal written fragment: %v", err)
	}

	if len(parsed.Outbounds) != 2 {
		t.Fatalf("expected 2 outbounds, got %d", len(parsed.Outbounds))
	}

	for i, ob := range parsed.Outbounds {
		ss, ok := ob["streamSettings"].(map[string]interface{})
		if !ok {
			t.Fatalf("outbound %d missing streamSettings", i)
		}
		sockopt, ok := ss["sockopt"].(map[string]interface{})
		if !ok {
			t.Fatalf("outbound %d missing sockopt", i)
		}

		mark, ok := sockopt["mark"].(float64)
		if !ok || int(mark) != 255 {
			t.Errorf("outbound %d expected mark=255, got %v", i, sockopt["mark"])
		}
		if tfo, ok := sockopt["tcpFastOpen"].(bool); !ok || !tfo {
			t.Errorf("outbound %d expected tcpFastOpen=true, got %v", i, sockopt["tcpFastOpen"])
		}
		if mptcp, ok := sockopt["tcpMptcp"].(bool); !ok || !mptcp {
			t.Errorf("outbound %d expected tcpMptcp=true, got %v", i, sockopt["tcpMptcp"])
		}
	}

	// Verify preservation of other streamSettings keys on node-ws
	wsOb := parsed.Outbounds[1]
	wsSS := wsOb["streamSettings"].(map[string]interface{})
	if wsSS["network"] != "ws" {
		t.Errorf("expected network=ws preserved, got %v", wsSS["network"])
	}
	if wsSS["wsSettings"] == nil {
		t.Errorf("expected wsSettings preserved")
	}
}

func TestWriteFragmentDialerProxy(t *testing.T) {
	svc, tmpDir := setupTestStorage(t)

	// sub2 contains target node
	sub2 := &Subscription{
		ID:         "sub2",
		Name:       "Sub 2",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "target-node", Name: "Target Node", Protocol: "vmess"},
		},
	}
	_ = svc.Add(sub2)

	fragPath := filepath.Join(tmpDir, "xray", "02_sub1.json")
	sub1 := &Subscription{
		ID:         "sub1",
		Name:       "Sub 1",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node-with-cascade", Name: "Node 1", Protocol: "vless", DialerProxy: "target-node"},
			{Tag: "node-direct", Name: "Node 2", Protocol: "vless"},
		},
	}
	_ = svc.Add(sub1)

	outbounds := []Outbound{
		{Tag: "node-with-cascade", Protocol: "vless", Settings: map[string]interface{}{"vnext": []interface{}{}}},
		{Tag: "node-direct", Protocol: "vless", Settings: map[string]interface{}{"vnext": []interface{}{}}},
	}

	_, err := svc.writeFragment(fragPath, outbounds, sub1)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// First outbound should have dialerProxy
	ob1SS := parsed.Outbounds[0]["streamSettings"].(map[string]interface{})
	ob1Sockopt := ob1SS["sockopt"].(map[string]interface{})
	if ob1Sockopt["dialerProxy"] != "target-node" {
		t.Errorf("expected dialerProxy=target-node, got %v", ob1Sockopt["dialerProxy"])
	}

	// Second outbound should NOT have dialerProxy (no sockopt at all since sub1 has no sockopt flags)
	if ob2SS, ok := parsed.Outbounds[1]["streamSettings"].(map[string]interface{}); ok {
		if _, hasSockopt := ob2SS["sockopt"]; hasSockopt {
			t.Errorf("unexpected sockopt in direct node: %+v", ob2SS)
		}
	}
}

func TestWriteFragmentDialerProxy_MissingTarget(t *testing.T) {
	svc, tmpDir := setupTestStorage(t)
	fragPath := filepath.Join(tmpDir, "xray", "02_sub_missing.json")

	sub := &Subscription{
		ID:         "sub-missing",
		Name:       "Sub Missing",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless", DialerProxy: "phantom-target"},
		},
	}
	outbounds := []Outbound{
		{Tag: "node1", Protocol: "vless", Settings: map[string]interface{}{"vnext": []interface{}{}}},
	}

	// Capture log output
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	_, err := svc.writeFragment(fragPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Target was missing -> dialerProxy must NOT be present in sockopt
	if ss, ok := parsed.Outbounds[0]["streamSettings"].(map[string]interface{}); ok {
		if sockopt, ok := ss["sockopt"].(map[string]interface{}); ok {
			if dp, exists := sockopt["dialerProxy"]; exists {
				t.Errorf("expected dialerProxy omitted for missing target, got %v", dp)
			}
		}
	}

	// Warning must be logged
	logStr := logBuf.String()
	if !strings.Contains(logStr, "phantom-target") || !strings.Contains(logStr, "not found in active Xray subscriptions") {
		t.Errorf("expected warning in log for missing target, got: %s", logStr)
	}
}

func TestWriteFragmentDialerProxy_ProxySettingsConflict(t *testing.T) {
	svc, tmpDir := setupTestStorage(t)
	fragPath := filepath.Join(tmpDir, "xray", "02_sub_conflict.json")

	sub := &Subscription{
		ID:         "sub-conflict",
		Name:       "Sub Conflict",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless", DialerProxy: "valid-target"},
		},
	}
	outbounds := []Outbound{
		{
			Tag:      "node1",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
			StreamSettings: map[string]interface{}{
				"proxySettings": map[string]interface{}{
					"tag": "existing-proxy",
				},
			},
		},
		{
			Tag:      "valid-target",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
		},
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	_, err := svc.writeFragment(fragPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	_ = json.Unmarshal(data, &parsed)

	ss := parsed.Outbounds[0]["streamSettings"].(map[string]interface{})
	if sockopt, ok := ss["sockopt"].(map[string]interface{}); ok {
		if dp, exists := sockopt["dialerProxy"]; exists {
			t.Errorf("expected dialerProxy omitted due to proxySettings conflict, got %v", dp)
		}
	}

	if !strings.Contains(logBuf.String(), "already has proxySettings configured") {
		t.Errorf("expected conflict log, got: %s", logBuf.String())
	}
}

func TestRefreshXrayFragmentOnDialerProxyAndSockoptUpdate(t *testing.T) {
	svc, _ := setupTestStorage(t)

	sub := &Subscription{
		ID:         "sub-frag",
		Name:       "Sub Fragment",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node-src", Name: "Src Node", Protocol: "vless"},
			{Tag: "node-dst", Name: "Dst Node", Protocol: "vless"},
		},
	}
	if err := svc.Add(sub); err != nil {
		t.Fatalf("failed to add sub: %v", err)
	}

	fragPath := svc.getFragmentPath(sub)
	outbounds := []Outbound{
		{
			Tag:      "node-src",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
		},
		{
			Tag:      "node-dst",
			Protocol: "vless",
			Settings: map[string]interface{}{"vnext": []interface{}{}},
		},
	}
	if _, err := svc.writeFragment(fragPath, outbounds, sub); err != nil {
		t.Fatalf("initial writeFragment failed: %v", err)
	}

	// 1. Update dialerProxy via SetNodeDialerProxy and check fragment on disk
	if err := svc.SetNodeDialerProxy("sub-frag", "node-src", "node-dst"); err != nil {
		t.Fatalf("SetNodeDialerProxy failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment after dialerProxy update: %v", err)
	}
	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal fragment failed: %v", err)
	}

	srcOutbound := parsed.Outbounds[0]
	ss, ok := srcOutbound["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected streamSettings in fragment for node-src")
	}
	sockopt, ok := ss["sockopt"].(map[string]interface{})
	if !ok || sockopt["dialerProxy"] != "node-dst" {
		t.Errorf("expected dialerProxy=node-dst in fragment, got %+v", sockopt)
	}

	// 2. Update sockopt via Update and check fragment on disk
	updatedSub := *sub
	updatedSub.SockoptFastOpen = true
	updatedSub.SockoptMark = 123
	if err := svc.Update("sub-frag", &updatedSub); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data2, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("failed to read fragment after sockopt update: %v", err)
	}
	var parsed2 struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data2, &parsed2); err != nil {
		t.Fatalf("unmarshal fragment2 failed: %v", err)
	}

	ss2 := parsed2.Outbounds[0]["streamSettings"].(map[string]interface{})
	sockopt2 := ss2["sockopt"].(map[string]interface{})
	if sockopt2["tcpFastOpen"] != true {
		t.Errorf("expected tcpFastOpen=true in fragment, got %v", sockopt2["tcpFastOpen"])
	}
	if int(sockopt2["mark"].(float64)) != 123 {
		t.Errorf("expected mark=123 in fragment, got %v", sockopt2["mark"])
	}
	// dialerProxy should still be retained
	if sockopt2["dialerProxy"] != "node-dst" {
		t.Errorf("expected dialerProxy=node-dst preserved, got %v", sockopt2["dialerProxy"])
	}
}

func TestXrayFragmentPreservesUnknownOutboundFields(t *testing.T) {
	svc, _ := setupTestStorage(t)
	sub := &Subscription{
		ID:         "sub-custom",
		Name:       "Custom Sub",
		Enabled:    true,
		EnableXray: true,
		Nodes: []SubscriptionNode{
			{Tag: "node-1", Protocol: "vless"},
			{Tag: "node-2", Protocol: "vless"},
		},
	}
	if err := svc.Add(sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	fragPath := svc.getFragmentPath(sub)
	initialFrag := `{
  "outbounds": [
    {
      "tag": "node-1",
      "protocol": "vless",
      "mux": {"enabled": true, "concurrency": 8},
      "sendThrough": "192.168.1.50",
      "customProperty": "must_survive",
      "settings": {}
    },
    {
      "tag": "node-2",
      "protocol": "vless",
      "settings": {}
    }
  ]
}`
	if err := os.WriteFile(fragPath, []byte(initialFrag), 0600); err != nil {
		t.Fatalf("failed to write initial fragment: %v", err)
	}

	// Update dialerProxy which calls refreshXrayFragmentLocked
	if err := svc.SetNodeDialerProxy("sub-custom", "node-1", "node-2"); err != nil {
		t.Fatalf("SetNodeDialerProxy failed: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("read fragment failed: %v", err)
	}
	var parsed struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal fragment failed: %v", err)
	}

	node1 := parsed.Outbounds[0]
	if node1["sendThrough"] != "192.168.1.50" {
		t.Errorf("expected sendThrough='192.168.1.50' preserved, got %v", node1["sendThrough"])
	}
	if node1["customProperty"] != "must_survive" {
		t.Errorf("expected customProperty='must_survive' preserved, got %v", node1["customProperty"])
	}
	mux, ok := node1["mux"].(map[string]interface{})
	if !ok || mux["enabled"] != true || int(mux["concurrency"].(float64)) != 8 {
		t.Errorf("expected mux preserved, got %+v", node1["mux"])
	}

	ss := node1["streamSettings"].(map[string]interface{})
	sockopt := ss["sockopt"].(map[string]interface{})
	if sockopt["dialerProxy"] != "node-2" {
		t.Errorf("expected dialerProxy='node-2', got %v", sockopt["dialerProxy"])
	}
}
