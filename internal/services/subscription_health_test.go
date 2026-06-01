package services

import (
	"net"
	"testing"
	"time"
)

type healthMockConn struct {
	net.Conn
}

func (m *healthMockConn) Close() error {
	return nil
}

func TestSubscriptionHealthService(t *testing.T) {
	tmpDir := t.TempDir()
	subSvc := NewSubscriptionService(tmpDir, tmpDir, tmpDir)

	sub := &Subscription{
		ID:      "test-sub",
		Name:    "Test Sub",
		Enabled: true,
		Nodes: []SubscriptionNode{
			{Tag: "node-1", Name: "Node 1", Server: "1.1.1.1:80", Protocol: "vless"},
			{Tag: "node-2", Name: "Node 2", Server: "2.2.2.2:80", Protocol: "vless"},
		},
	}
	subSvc.Add(sub)

	healthSvc := NewSubscriptionHealthService(tmpDir, subSvc)

	// Mock successful dial for node-1, failed dial for node-2
	healthSvc.dialFunc = func(network, address string, timeout time.Duration) (net.Conn, error) {
		if address == "1.1.1.1:80" {
			return &healthMockConn{}, nil
		}
		return nil, net.ErrWriteToConnected
	}

	// 1. Test ForceCheckNode for node-1 (alive)
	h1, ok1 := healthSvc.ForceCheckNode("test-sub", "node-1")
	if !ok1 || !h1.Alive {
		t.Errorf("expected node-1 to be alive")
	}

	// 2. Test ForceCheckNode for node-2 (dead)
	h2, ok2 := healthSvc.ForceCheckNode("test-sub", "node-2")
	if !ok2 || h2.Alive {
		t.Errorf("expected node-2 to be dead")
	}

	// 3. Test ForceCheck (all nodes)
	healthSvc.ForceCheck("test-sub")
	cached := healthSvc.GetHealth("test-sub")
	if len(cached) != 2 {
		t.Errorf("expected 2 cached results, got %d", len(cached))
	}
	if !cached["node-1"].Alive {
		t.Error("expected node-1 to be alive in cache")
	}
	if cached["node-2"].Alive {
		t.Error("expected node-2 to be dead in cache")
	}
}
