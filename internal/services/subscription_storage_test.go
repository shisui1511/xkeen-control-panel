package services

import (
	"os"
	"path/filepath"
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

func TestSubscriptionService_MigrateFromLegacyUI(t *testing.T) {
	tmp := t.TempDir()
	xcpDir := filepath.Join(tmp, "xcp")
	xkeenUIDir := filepath.Join(tmp, "legacy-ui")

	err := os.MkdirAll(xkeenUIDir, 0755)
	if err != nil {
		t.Fatalf("failed to create legacy-ui dir: %v", err)
	}

	// 1. Создаем тестовый файл mihomo_subscriptions.json
	mihomoData := `{
		"version": 1,
		"subscriptions": [
			{
				"id": "mihomo_sub1",
				"url": "https://example.com/mihomo1",
				"tag": "Mihomo Tag",
				"enabled": true,
				"interval_hours": 12
			}
		]
	}`
	err = os.WriteFile(filepath.Join(xkeenUIDir, "mihomo_subscriptions.json"), []byte(mihomoData), 0600)
	if err != nil {
		t.Fatalf("failed to write mihomo state: %v", err)
	}

	// 2. Создаем тестовый файл xray_subscriptions.json
	xrayData := `{
		"version": 1,
		"subscriptions": [
			{
				"id": "xray_sub1",
				"url": "https://example.com/xray1",
				"tag": "Xray Tag",
				"enabled": false,
				"interval_hours": 6
			},
			{
				"id": "duplicate_sub",
				"url": "https://example.com/mihomo1",
				"tag": "Duplicate",
				"enabled": true,
				"interval_hours": 12
			}
		]
	}`
	err = os.WriteFile(filepath.Join(xkeenUIDir, "xray_subscriptions.json"), []byte(xrayData), 0600)
	if err != nil {
		t.Fatalf("failed to write xray state: %v", err)
	}

	// 3. Создаем сервис подписок. Он должен автоматически выполнить миграцию.
	svc := NewSubscriptionService(xcpDir, filepath.Join(tmp, "xray"), filepath.Join(tmp, "mihomo"))
	if svc == nil {
		t.Fatal("expected non-nil service")
	}

	subs := svc.List()
	if len(subs) != 2 {
		t.Fatalf("expected 2 migrated subscriptions (1 duplicate merged), got %d", len(subs))
	}

	var subMihomo, subXray *Subscription
	for i := range subs {
		if subs[i].URL == "https://example.com/mihomo1" {
			subMihomo = &subs[i]
		} else if subs[i].URL == "https://example.com/xray1" {
			subXray = &subs[i]
		}
	}

	if subMihomo == nil {
		t.Fatal("mihomo subscription not found")
	}
	if subMihomo.Name != "Mihomo Tag" {
		t.Errorf("expected name 'Mihomo Tag', got %s", subMihomo.Name)
	}
	if !subMihomo.EnableMihomo || !subMihomo.EnableXray {
		t.Errorf("expected subscription to be enabled for both Mihomo and Xray due to merge, got EnableMihomo=%t, EnableXray=%t", subMihomo.EnableMihomo, subMihomo.EnableXray)
	}
	if subMihomo.Interval != 12 {
		t.Errorf("expected interval 12, got %d", subMihomo.Interval)
	}
	if !subMihomo.Enabled {
		t.Errorf("expected sub to be enabled")
	}

	if subXray == nil {
		t.Fatal("xray subscription not found")
	}
	if subXray.Name != "Xray Tag" {
		t.Errorf("expected name 'Xray Tag', got %s", subXray.Name)
	}
	if subXray.EnableMihomo || !subXray.EnableXray {
		t.Errorf("expected only Xray to be enabled, got EnableMihomo=%t, EnableXray=%t", subXray.EnableMihomo, subXray.EnableXray)
	}
	if subXray.Interval != 6 {
		t.Errorf("expected interval 6, got %d", subXray.Interval)
	}
	if subXray.Enabled {
		t.Errorf("expected sub to be disabled")
	}
}

func TestSubscriptionService_MigrateFromMihomoConfig(t *testing.T) {
	tmp := t.TempDir()
	xcpDir := filepath.Join(tmp, "xcp")
	mihomoDir := filepath.Join(tmp, "mihomo")

	err := os.MkdirAll(mihomoDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mihomo dir: %v", err)
	}

	// 1. Создаем тестовый config.yaml
	configYAML := `
tproxy-port: 5001
redir-port: 5000

proxy-providers:
  Legacy-UI-Provider:
    type: http
    path: ./proxy_providers/Legacy-UI-Provider.yaml
    url: "http://127.0.0.1:8088/mihomo/provider.yaml?url=https%3A%2F%2Fexample.com%2Fmy-Clean-Sub&insecure=1"
    interval: 7200
    health-check:
      enable: true
      url: 'https://www.gstatic.com/generate_204'
      interval: 300
  local-provider:
    type: file
    path: ./local.yaml
`
	err = os.WriteFile(filepath.Join(mihomoDir, "config.yaml"), []byte(configYAML), 0600)
	if err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	// 2. Создаем сервис подписок. Он должен автоматически импортировать провайдер.
	svc := NewSubscriptionService(xcpDir, filepath.Join(tmp, "xray"), mihomoDir)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}

	subs := svc.List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 migrated subscription, got %d", len(subs))
	}

	sub := &subs[0]
	if sub.Name != "Legacy-UI-Provider" {
		t.Errorf("expected name 'Legacy-UI-Provider', got %s", sub.Name)
	}
	if sub.URL != "https://example.com/my-Clean-Sub" {
		t.Errorf("expected clean URL 'https://example.com/my-Clean-Sub', got %s", sub.URL)
	}
	if sub.Interval != 2 { // 7200 / 3600
		t.Errorf("expected interval 2, got %d", sub.Interval)
	}
	if !sub.EnableMihomo || sub.EnableXray {
		t.Errorf("expected Mihomo enabled and Xray disabled, got EnableMihomo=%t, EnableXray=%t", sub.EnableMihomo, sub.EnableXray)
	}
	if !sub.Enabled {
		t.Errorf("expected subscription to be enabled")
	}
	if !sub.MihomoIntegrated {
		t.Errorf("expected subscription to be MihomoIntegrated")
	}
}
