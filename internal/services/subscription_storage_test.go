package services

import (
	"sync"
	"testing"
	"time"
)

func TestSubscriptionService_New(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestSubscriptionService_Add(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")

	sub := Subscription{
		Name:      "Test Sub",
		URL:       "https://example.com/sub",
		TagPrefix: "test-",
		Enabled:   true,
	}

	err := svc.Add(&sub)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	subs := svc.List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].Name != "Test Sub" {
		t.Fatalf("expected name 'Test Sub', got %s", subs[0].Name)
	}
}

func TestSubscriptionService_Delete(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")

	sub := Subscription{
		Name:    "To Delete",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	svc.Add(&sub)

	subs := svc.List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after add, got %d", len(subs))
	}

	id := subs[0].ID
	err := svc.Delete(id)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	subs = svc.List()
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions after delete, got %d", len(subs))
	}
}

func TestSubscriptionService_Persistence(t *testing.T) {
	tmp := t.TempDir()

	svc1 := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")
	sub := Subscription{
		Name:    "Persistent Sub",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	svc1.Add(&sub)

	svc2 := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")
	subs := svc2.List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after reload, got %d", len(subs))
	}
	if subs[0].Name != "Persistent Sub" {
		t.Fatalf("expected name 'Persistent Sub', got %s", subs[0].Name)
	}
}

func TestSubscriptionGet_ReturnsCopy(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")

	sub := Subscription{
		Name:    "Original",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	id := svc.List()[0].ID
	got := svc.Get(id)
	if got == nil {
		t.Fatal("Get returned nil")
	}

	// Mutate the returned copy
	got.Name = "Modified"

	// The original in the service slice must be unchanged
	original := svc.Get(id)
	if original == nil {
		t.Fatal("second Get returned nil")
	}
	if original.Name != "Original" {
		t.Errorf("expected original name 'Original', got %q (mutation leaked)", original.Name)
	}
}

func TestSubscription_DeepCopy(t *testing.T) {
	sub := Subscription{
		ID:           "test",
		Name:         "original",
		ProxyNames:   []string{"node1", "node2"},
		MihomoGroups: []string{"group1"},
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1"},
		},
	}

	cloned := sub.Clone()
	cloned.ProxyNames[0] = "mutated"
	cloned.MihomoGroups[0] = "mutated"
	cloned.Nodes[0].Name = "mutated"

	if sub.ProxyNames[0] != "node1" {
		t.Error("ProxyNames slice sharing detected")
	}
	if sub.MihomoGroups[0] != "group1" {
		t.Error("MihomoGroups slice sharing detected")
	}
	if sub.Nodes[0].Name != "Node 1" {
		t.Error("Nodes slice sharing detected")
	}
}

func TestSubscription_ConcurrencyRace(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	sub := Subscription{
		ID:           "test",
		Name:         "original",
		Enabled:      true,
		ProxyNames:   []string{"node1"},
		MihomoGroups: []string{"group1"},
		Nodes: []SubscriptionNode{
			{Tag: "node1", Name: "Node 1"},
		},
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				svc.List()
				svc.Get("test")
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s := svc.Get("test")
				if s != nil {
					s.ProxyNames = append(s.ProxyNames, "new")
					s.Nodes = append(s.Nodes, SubscriptionNode{Tag: "new"})
					svc.Update("test", s)
				}
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
