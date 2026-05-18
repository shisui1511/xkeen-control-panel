package services

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestSubscriptionService_New(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestSubscriptionService_Add(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray")

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
	svc := NewSubscriptionService(tmp, "/opt/etc/xray")

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

// TestBase64FallbackURLRaw: VMess link encoded with RawURLEncoding is parsed correctly.
func TestBase64FallbackURLRaw(t *testing.T) {
	vmess := map[string]interface{}{
		"ps":   "test-server",
		"add":  "example.com",
		"port": "443",
		"id":   "550e8400-e29b-41d4-a716-446655440000",
		"aid":  "0",
		"net":  "tcp",
		"type": "none",
		"host": "",
		"path": "",
		"tls":  "tls",
	}
	data, _ := json.Marshal(vmess)
	// Encode with RawURLEncoding (no padding)
	b64 := base64.RawURLEncoding.EncodeToString(data)
	link := "vmess://" + b64

	ob := parseVMessLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound for RawURLEncoding VMess link, got nil")
	}
	if ob.Tag != "test-server" {
		t.Errorf("expected tag 'test-server', got %q", ob.Tag)
	}
}

// TestBase64FallbackInvalid: truly invalid base64 returns nil.
func TestBase64FallbackInvalid(t *testing.T) {
	link := "vmess://!!!not-valid-base64!!!"
	ob := parseVMessLink(link)
	if ob != nil {
		t.Errorf("expected nil for invalid base64, got %+v", ob)
	}
}

// TestPortIsInteger: port field in generated Outbound settings is int, not string.
func TestPortIsInteger(t *testing.T) {
	vmess := map[string]interface{}{
		"ps":   "port-test",
		"add":  "1.2.3.4",
		"port": "8443",
		"id":   "550e8400-e29b-41d4-a716-446655440000",
		"aid":  "0",
		"net":  "tcp",
		"type": "none",
		"host": "",
		"path": "",
		"tls":  "",
	}
	data, _ := json.Marshal(vmess)
	b64 := base64.StdEncoding.EncodeToString(data)
	link := "vmess://" + b64

	ob := parseVMessLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}

	vnext, ok := ob.Settings["vnext"].([]map[string]interface{})
	if !ok || len(vnext) == 0 {
		t.Fatal("expected vnext array in settings")
	}

	port, ok := vnext[0]["port"]
	if !ok {
		t.Fatal("expected 'port' field in vnext[0]")
	}

	switch v := port.(type) {
	case int:
		if v != 8443 {
			t.Errorf("expected port 8443, got %d", v)
		}
	default:
		t.Errorf("expected port to be int, got %T: %v", port, port)
	}
}

// TestDownloadAndParseSchemeValidation: file:// scheme returns error.
func TestDownloadAndParseSchemeValidation(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray")
	_, err := svc.downloadAndParse("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected error for file:// URL, got nil")
	}
}

// TestFilterTransport: FilterTransport filters outbounds by transport type.
func TestFilterTransport(t *testing.T) {
	svc := &SubscriptionService{}
	sub := &Subscription{FilterTransport: "ws"}
	outbounds := []Outbound{
		{Tag: "ws-proxy", Protocol: "vmess", StreamSettings: map[string]interface{}{"network": "ws"}},
		{Tag: "tcp-proxy", Protocol: "vmess", StreamSettings: map[string]interface{}{"network": "tcp"}},
		{Tag: "no-stream", Protocol: "trojan"},
	}
	result := svc.applyFilters(outbounds, sub)
	if len(result) != 1 {
		t.Errorf("expected 1 outbound after FilterTransport=ws, got %d", len(result))
	}
	if len(result) > 0 && result[0].Tag != "ws-proxy" {
		t.Errorf("expected 'ws-proxy', got %q", result[0].Tag)
	}
}

func TestSubscriptionService_Persistence(t *testing.T) {
	tmp := t.TempDir()

	svc1 := NewSubscriptionService(tmp, "/opt/etc/xray")
	sub := Subscription{
		Name:    "Persistent Sub",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	svc1.Add(&sub)

	svc2 := NewSubscriptionService(tmp, "/opt/etc/xray")
	subs := svc2.List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after reload, got %d", len(subs))
	}
	if subs[0].Name != "Persistent Sub" {
		t.Fatalf("expected name 'Persistent Sub', got %s", subs[0].Name)
	}
}
