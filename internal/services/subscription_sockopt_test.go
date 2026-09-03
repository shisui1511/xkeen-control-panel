package services_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestSubscriptionSockoptFields(t *testing.T) {
	sub := &services.Subscription{
		ID:              "sub-test",
		Name:            "Test Sub",
		SockoptMark:     255,
		SockoptFastOpen: true,
		SockoptMptcp:    true,
		Nodes: []services.SubscriptionNode{
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

	var decoded services.Subscription
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

	var sub services.Subscription
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

func setupTestStorage(t *testing.T) (*services.SubscriptionService, string) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	mihomoDir := filepath.Join(tmpDir, "mihomo")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(mihomoDir, 0755)

	svc := services.NewSubscriptionService(tmpDir, xrayDir, mihomoDir)
	return svc, tmpDir
}

func TestSetNodeDialerProxy(t *testing.T) {
	svc, _ := setupTestStorage(t)

	// Create sub1 with node1 and node2
	sub1 := &services.Subscription{
		ID:         "sub1",
		Name:       "Sub 1",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node2", Name: "Node 2", Protocol: "vless"},
		},
	}
	if err := svc.Add(sub1); err != nil {
		t.Fatalf("failed to add sub1: %v", err)
	}

	// Create sub2 with node3 and node4
	sub2 := &services.Subscription{
		ID:         "sub2",
		Name:       "Sub 2",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
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
}

func TestDialerProxyTargets(t *testing.T) {
	svc, _ := setupTestStorage(t)

	// sub1: node1 (source), node2
	sub1 := &services.Subscription{
		ID:         "sub1",
		Name:       "Sub 1",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node1", Name: "Node 1", Protocol: "vless"},
			{Tag: "node2", Name: "Node 2", Protocol: "vless"},
		},
	}
	_ = svc.Add(sub1)

	// sub2: node3 (available), node4 (has its own dialerProxy -> excluded)
	sub2 := &services.Subscription{
		ID:         "sub2",
		Name:       "Sub 2",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
			{Tag: "node3", Name: "Node 3", Protocol: "vmess"},
			{Tag: "node4", Name: "Node 4", Protocol: "vmess", DialerProxy: "node2"},
		},
	}
	_ = svc.Add(sub2)

	// sub3: disabled Xray -> all nodes excluded
	sub3 := &services.Subscription{
		ID:         "sub3",
		Name:       "Sub 3",
		Enabled:    true,
		EnableXray: false,
		Nodes: []services.SubscriptionNode{
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
	singleSub := &services.Subscription{
		ID:         "single",
		Name:       "Single",
		Enabled:    true,
		EnableXray: true,
		Nodes: []services.SubscriptionNode{
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
