package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestSubscriptionService_UpdateTypeTransition(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(mihomoDir, 0755)

	svc := NewSubscriptionService(tmp, xrayDir, mihomoDir)

	sub := Subscription{
		Name:         "Transition Test",
		URL:          "https://example.com/sub",
		Enabled:      true,
		EnableXray:   true,
		EnableMihomo: false,
	}
	svc.Add(&sub)

	id := svc.List()[0].ID

	// Create a dummy fragment file for xray
	fragmentPath := filepath.Join(xrayDir, fmt.Sprintf("04_outbounds.%s.json", id))
	_ = os.WriteFile(fragmentPath, []byte(`[]`), 0600)

	// Update flags to enable mihomo and disable xray
	updatedSub := sub
	updatedSub.EnableXray = false
	updatedSub.EnableMihomo = true
	err := svc.Update(id, &updatedSub)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify xray fragment was deleted
	if _, err := os.Stat(fragmentPath); !os.IsNotExist(err) {
		t.Error("xray fragment should have been deleted during transition to mihomo")
	}

	// Create dummy config.yaml and provider file for mihomo
	configPath := filepath.Join(mihomoDir, "config.yaml")
	providerName := getMihomoProviderName(sub.Name, sub.URL, id)
	_ = os.WriteFile(configPath, []byte("proxy-providers:\n  "+providerName+":\n    type: http\n"), 0600)
	providerPath := filepath.Join(mihomoDir, "providers", fmt.Sprintf("%s.yaml", providerName))
	_ = os.MkdirAll(filepath.Join(mihomoDir, "providers"), 0755)
	_ = os.WriteFile(providerPath, []byte(""), 0600)

	// Update flags back: enable xray and disable mihomo
	updatedSub2 := updatedSub
	updatedSub2.EnableXray = true
	updatedSub2.EnableMihomo = false
	err = svc.Update(id, &updatedSub2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify mihomo configuration was cleaned up
	if _, err := os.Stat(providerPath); !os.IsNotExist(err) {
		t.Error("mihomo provider file should have been deleted during transition to xray")
	}

	data, err := os.ReadFile(configPath)
	if err == nil && strings.Contains(string(data), "proxy-providers:") {
		t.Error("mihomo config should have proxy-providers section cleared during transition to xray")
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
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")
	_, _, _, _, err := svc.downloadAndParse("file:///etc/passwd", &Subscription{})
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

// TestParseVLESSLink_Reality: VLESS with Reality transport params → StreamSettings populated.
func TestParseVLESSLink_Reality(t *testing.T) {
	link := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?type=tcp&security=reality&pbk=pubkey123&sid=shortid456&sni=sni.example.com&fp=chrome&flow=xtls-rprx-vision#myserver"
	ob := parseVLESSLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}

	ss := ob.StreamSettings
	if ss == nil {
		t.Fatal("expected non-nil StreamSettings")
	}

	if ss["security"] != "reality" {
		t.Errorf("expected security=reality, got %v", ss["security"])
	}

	realitySettings, ok := ss["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected realitySettings map")
	}
	if realitySettings["publicKey"] != "pubkey123" {
		t.Errorf("expected publicKey=pubkey123, got %v", realitySettings["publicKey"])
	}
	if realitySettings["shortId"] != "shortid456" {
		t.Errorf("expected shortId=shortid456, got %v", realitySettings["shortId"])
	}
	if realitySettings["serverName"] != "sni.example.com" {
		t.Errorf("expected serverName=sni.example.com, got %v", realitySettings["serverName"])
	}
	if realitySettings["fingerprint"] != "chrome" {
		t.Errorf("expected fingerprint=chrome, got %v", realitySettings["fingerprint"])
	}

	// Check flow in users
	vnext, ok := ob.Settings["vnext"].([]map[string]interface{})
	if !ok || len(vnext) == 0 {
		t.Fatal("expected vnext in settings")
	}
	users, ok := vnext[0]["users"].([]map[string]interface{})
	if !ok || len(users) == 0 {
		t.Fatal("expected users in vnext[0]")
	}
	if users[0]["flow"] != "xtls-rprx-vision" {
		t.Errorf("expected flow=xtls-rprx-vision, got %v", users[0]["flow"])
	}
}

// TestParseVMessLink_WS: VMess with WebSocket transport → StreamSettings populated.
func TestParseVMessLink_WS(t *testing.T) {
	vmess := map[string]interface{}{
		"ps":   "ws-server",
		"add":  "cdn.example.com",
		"port": "443",
		"id":   "550e8400-e29b-41d4-a716-446655440000",
		"aid":  "0",
		"net":  "ws",
		"type": "none",
		"host": "cdn.example.com",
		"path": "/ws",
		"tls":  "tls",
		"sni":  "cdn.example.com",
	}
	data, _ := json.Marshal(vmess)
	b64 := base64.StdEncoding.EncodeToString(data)
	link := "vmess://" + b64

	ob := parseVMessLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}

	ss := ob.StreamSettings
	if ss == nil {
		t.Fatal("expected non-nil StreamSettings")
	}
	if ss["network"] != "ws" {
		t.Errorf("expected network=ws, got %v", ss["network"])
	}

	wsSettings, ok := ss["wsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected wsSettings map")
	}
	if wsSettings["path"] != "/ws" {
		t.Errorf("expected path=/ws, got %v", wsSettings["path"])
	}
	headers, ok := wsSettings["headers"].(map[string]interface{})
	if !ok {
		t.Fatal("expected headers in wsSettings")
	}
	if headers["Host"] != "cdn.example.com" {
		t.Errorf("expected Host=cdn.example.com, got %v", headers["Host"])
	}

	tlsSettings, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tlsSettings map")
	}
	if tlsSettings["serverName"] != "cdn.example.com" {
		t.Errorf("expected serverName=cdn.example.com, got %v", tlsSettings["serverName"])
	}
}

// TestParseHysteria2Link: hy2:// link with obfs → correct Outbound structure.
func TestParseHysteria2Link(t *testing.T) {
	link := "hy2://mypassword@hy2.example.com:443?sni=hy2.example.com&obfs=salamander&obfs-password=obfspass#hy2server"
	ob := parseHysteria2Link(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}
	if ob.Protocol != "hysteria2" {
		t.Errorf("expected protocol=hysteria2, got %q", ob.Protocol)
	}
	if ob.Tag != "hy2server" {
		t.Errorf("expected tag=hy2server, got %q", ob.Tag)
	}

	servers, ok := ob.Settings["servers"].([]map[string]interface{})
	if !ok || len(servers) == 0 {
		t.Fatal("expected servers in settings")
	}
	if servers[0]["address"] != "hy2.example.com" {
		t.Errorf("expected address=hy2.example.com, got %v", servers[0]["address"])
	}
	if servers[0]["port"] != 443 {
		t.Errorf("expected port=443, got %v", servers[0]["port"])
	}
	if servers[0]["password"] != "mypassword" {
		t.Errorf("expected password=mypassword, got %v", servers[0]["password"])
	}

	ss := ob.StreamSettings
	if ss == nil {
		t.Fatal("expected StreamSettings")
	}
	tlsSettings, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tlsSettings")
	}
	if tlsSettings["serverName"] != "hy2.example.com" {
		t.Errorf("expected serverName=hy2.example.com, got %v", tlsSettings["serverName"])
	}

	hy2Settings, ok := ob.Settings["hysteria2Settings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hysteria2Settings in settings")
	}
	obfsMap, ok := hy2Settings["obfs"].(map[string]interface{})
	if !ok {
		t.Fatal("expected obfs in hysteria2Settings")
	}
	if obfsMap["type"] != "salamander" {
		t.Errorf("expected obfs.type=salamander, got %v", obfsMap["type"])
	}
}

// TestParseTUICLink: tuic:// link → correct Outbound structure with uuid/congestionControl/alpn.
func TestParseTUICLink(t *testing.T) {
	link := "tuic://myuuid:mypassword@tuic.example.com:443?sni=tuic.example.com&congestion_control=bbr&alpn=h3#tuicserver"
	ob := parseTUICLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}
	if ob.Protocol != "tuic" {
		t.Errorf("expected protocol=tuic, got %q", ob.Protocol)
	}
	if ob.Tag != "tuicserver" {
		t.Errorf("expected tag=tuicserver, got %q", ob.Tag)
	}

	servers, ok := ob.Settings["servers"].([]map[string]interface{})
	if !ok || len(servers) == 0 {
		t.Fatal("expected servers in settings")
	}
	if servers[0]["uuid"] != "myuuid" {
		t.Errorf("expected uuid=myuuid, got %v", servers[0]["uuid"])
	}
	if servers[0]["password"] != "mypassword" {
		t.Errorf("expected password=mypassword, got %v", servers[0]["password"])
	}
	if servers[0]["congestionControl"] != "bbr" {
		t.Errorf("expected congestionControl=bbr, got %v", servers[0]["congestionControl"])
	}

	ss := ob.StreamSettings
	if ss == nil {
		t.Fatal("expected StreamSettings")
	}
	if ss["network"] != "udp" {
		t.Errorf("expected network=udp, got %v", ss["network"])
	}
	tlsSettings, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tlsSettings")
	}
	if tlsSettings["serverName"] != "tuic.example.com" {
		t.Errorf("expected serverName=tuic.example.com, got %v", tlsSettings["serverName"])
	}
	alpn, ok := tlsSettings["alpn"].([]string)
	if !ok || len(alpn) == 0 {
		t.Fatal("expected alpn in tlsSettings")
	}
	if alpn[0] != "h3" {
		t.Errorf("expected alpn[0]=h3, got %v", alpn[0])
	}
}

// TestSubscriptionEntryLimit: >500 non-empty lines → error; 500 → success.
func TestSubscriptionEntryLimit(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	// Use plain http.DefaultClient so httptest servers (loopback) are reachable
	svc.httpClient = http.DefaultClient

	// Build 5001 vless lines (сверх нового лимита 5000).
	lines5001 := make([]string, 5001)
	for i := range lines5001 {
		lines5001[i] = "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#tag"
	}
	body5001 := strings.Join(lines5001, "\n")

	ts5001 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body5001))
	}))
	defer ts5001.Close()

	_, _, _, _, err := svc.downloadAndParse(ts5001.URL, &Subscription{})
	if err == nil {
		t.Error("expected error for 5001 entries, got nil")
	}

	// Build exactly 5000 vless lines — должно пройти.
	lines5000 := make([]string, 5000)
	for i := range lines5000 {
		lines5000[i] = "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#tag"
	}
	body5000 := strings.Join(lines5000, "\n")

	ts5000 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body5000))
	}))
	defer ts5000.Close()

	_, _, _, _, err = svc.downloadAndParse(ts5000.URL, &Subscription{})
	if err != nil {
		t.Errorf("expected no error for 5000 entries, got: %v", err)
	}
}

// TestTagDeduplication: writeFragment with duplicate tags → suffixed tags.
func TestTagDeduplication(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	sub := &Subscription{ID: "test", Name: "test"}

	outbounds := []Outbound{
		{Tag: "server", Protocol: "vmess"},
		{Tag: "server", Protocol: "vmess"},
		{Tag: "server", Protocol: "vmess"},
	}

	path := svc.getFragmentPath(sub)
	if _, err := svc.writeFragment(path, outbounds, sub); err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatal(err)
	}
	result := wrapper.Outbounds

	if len(result) != 3 {
		t.Fatalf("expected 3 outbounds, got %d", len(result))
	}
	if result[0].Tag != "server" {
		t.Errorf("expected first tag=server, got %q", result[0].Tag)
	}
	if result[1].Tag != "server-1" {
		t.Errorf("expected second tag=server-1, got %q", result[1].Tag)
	}
	if result[2].Tag != "server-2" {
		t.Errorf("expected third tag=server-2, got %q", result[2].Tag)
	}
}

// TestDownloadAndParse_NetworkError: server closes connection immediately → error returned, no file created.
func TestDownloadAndParse_NetworkError(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	// Use plain http.DefaultClient so httptest servers (loopback) are reachable
	svc.httpClient = http.DefaultClient

	// Server that immediately closes the connection
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			// Fallback: return 500
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer ts.Close()

	_, _, _, _, err := svc.downloadAndParse(ts.URL, &Subscription{})
	if err == nil {
		t.Error("expected error for connection reset, got nil")
	}
}

// TestSubscriptionScheduler_FrozenClock tests checkAndRefreshDue directly without a ticker.
// It verifies that subscriptions with an elapsed interval are refreshed, while
// subscriptions within the interval window are not.
func TestSubscriptionScheduler_FrozenClock(t *testing.T) {
	tmp := t.TempDir()

	// A minimal httptest server so that Refresh() can actually make an HTTP request.
	var refreshCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&refreshCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return an empty outbound list so writeFragment succeeds.
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	// Subscription 1: overdue (last update 2 hours ago, interval 1 hour)
	pastTime := time.Now().Add(-2 * time.Hour)
	sub1 := &Subscription{
		Name:       "overdue",
		URL:        ts.URL,
		TagPrefix:  "s1-",
		Enabled:    true,
		EnableXray: true,
		Interval:   1,
		LastUpdate: pastTime,
	}
	if err := svc.Add(sub1); err != nil {
		t.Fatalf("Add sub1: %v", err)
	}

	// Subscription 2: not yet due (last update 30 min ago, interval 1 hour)
	recentTime := time.Now().Add(-30 * time.Minute)
	sub2 := &Subscription{
		Name:       "not-due",
		URL:        ts.URL,
		TagPrefix:  "s2-",
		Enabled:    true,
		EnableXray: true,
		Interval:   1,
		LastUpdate: recentTime,
	}
	if err := svc.Add(sub2); err != nil {
		t.Fatalf("Add sub2: %v", err)
	}

	// Verify isDue directly (synchronous, no ticker needed)
	now := time.Now()
	if !svc.isRefreshDue(sub1, now) {
		t.Error("expected sub1 (overdue) to be due")
	}
	if svc.isRefreshDue(sub2, now) {
		t.Error("expected sub2 (recent) to NOT be due")
	}

	// Call checkAndRefreshDue and wait for goroutines to finish
	svc.checkAndRefreshDue(now)

	// Give goroutines a short window to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&refreshCount) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got := atomic.LoadInt64(&refreshCount)
	// Only sub1 is due, so exactly 1 refresh should have been triggered
	if got != 1 {
		t.Errorf("expected 1 refresh call, got %d", got)
	}
}

// TestSubscriptionGet_ReturnsCopy (T006): modifying the returned copy must not affect the original.
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

func TestSubscriptionProxyCount(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	sub := Subscription{
		Name:    "Proxy Count Test",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	id := svc.List()[0].ID

	// Before refresh (no fragment file)
	got := svc.Get(id)
	if got.ProxyCount != 0 {
		t.Errorf("expected ProxyCount 0, got %d", got.ProxyCount)
	}

	// Create fragment file manually to simulate refresh
	path := svc.getFragmentPath(got)
	outbounds := []Outbound{
		{Tag: "proxy1", Protocol: "vmess"},
		{Tag: "proxy2", Protocol: "vless"},
	}
	if _, err := svc.writeFragment(path, outbounds, got); err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	// Read again
	got = svc.Get(id)
	if got.ProxyCount != 2 {
		t.Errorf("expected ProxyCount 2, got %d", got.ProxyCount)
	}

	subs := svc.List()
	if subs[0].ProxyCount != 2 {
		t.Errorf("expected ProxyCount 2 in List(), got %d", subs[0].ProxyCount)
	}
}

// TestParseSOCKSLink: socks:// and socks5:// links.
func TestParseSOCKSLink(t *testing.T) {
	tests := []struct {
		name     string
		link     string
		wantTag  string
		wantPort int
		wantUser string
	}{
		{
			name:     "socks5 with auth",
			link:     "socks5://user:pass@socks.example.com:1080#mysocks",
			wantTag:  "mysocks",
			wantPort: 1080,
			wantUser: "user",
		},
		{
			name:     "socks without auth",
			link:     "socks://socks.example.com:1080#sock",
			wantTag:  "sock",
			wantPort: 1080,
			wantUser: "",
		},
		{
			name:     "socks5 fallback tag from hostname",
			link:     "socks5://socks.example.com:443",
			wantTag:  "socks.example.com",
			wantPort: 443,
			wantUser: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ob := parseSOCKSLink(tc.link)
			if ob == nil {
				t.Fatal("expected non-nil Outbound")
			}
			if ob.Protocol != "socks" {
				t.Errorf("expected protocol=socks, got %q", ob.Protocol)
			}
			if ob.Tag != tc.wantTag {
				t.Errorf("expected tag=%q, got %q", tc.wantTag, ob.Tag)
			}
			servers, ok := ob.Settings["servers"].([]map[string]interface{})
			if !ok || len(servers) == 0 {
				t.Fatal("expected servers in settings")
			}
			if servers[0]["port"] != tc.wantPort {
				t.Errorf("expected port=%d, got %v", tc.wantPort, servers[0]["port"])
			}
			if tc.wantUser != "" {
				users, ok := servers[0]["users"].([]map[string]interface{})
				if !ok || len(users) == 0 {
					t.Fatal("expected users in server")
				}
				if users[0]["user"] != tc.wantUser {
					t.Errorf("expected user=%q, got %v", tc.wantUser, users[0]["user"])
				}
			} else {
				if _, hasUsers := servers[0]["users"]; hasUsers {
					t.Error("expected no users in server for anonymous socks")
				}
			}
		})
	}
}

// TestParseHTTPProxyLink: http-proxy:// links.
func TestParseHTTPProxyLink(t *testing.T) {
	link := "http-proxy://proxyuser:proxypass@http.example.com:3128#httpproxy"
	ob := parseHTTPProxyLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound")
	}
	if ob.Protocol != "http" {
		t.Errorf("expected protocol=http, got %q", ob.Protocol)
	}
	if ob.Tag != "httpproxy" {
		t.Errorf("expected tag=httpproxy, got %q", ob.Tag)
	}
	servers, ok := ob.Settings["servers"].([]map[string]interface{})
	if !ok || len(servers) == 0 {
		t.Fatal("expected servers in settings")
	}
	if servers[0]["address"] != "http.example.com" {
		t.Errorf("expected address=http.example.com, got %v", servers[0]["address"])
	}
	if servers[0]["port"] != 3128 {
		t.Errorf("expected port=3128, got %v", servers[0]["port"])
	}
	users, ok := servers[0]["users"].([]map[string]interface{})
	if !ok || len(users) == 0 {
		t.Fatal("expected users in server")
	}
	if users[0]["user"] != "proxyuser" {
		t.Errorf("expected user=proxyuser, got %v", users[0]["user"])
	}
}

// TestParseLinks: batch parse returns correct results and per-link errors.
func TestParseLinks(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	links := []string{
		"vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#vlessnode",
		"socks5://user:pass@socks.example.com:1080#socksnode",
		"notaprotocol://garbage",
		"", // empty — should be skipped
	}

	results := svc.ParseLinks(links)

	// Empty link skipped → 3 results
	if len(results) != 3 {
		t.Fatalf("expected 3 results (empty link skipped), got %d", len(results))
	}

	if results[0].Error != "" {
		t.Errorf("expected no error for vless link, got %q", results[0].Error)
	}
	if results[0].Outbound == nil || results[0].Outbound.Protocol != "vless" {
		t.Errorf("expected vless outbound, got %v", results[0].Outbound)
	}

	if results[1].Error != "" {
		t.Errorf("expected no error for socks5 link, got %q", results[1].Error)
	}
	if results[1].Outbound == nil || results[1].Outbound.Protocol != "socks" {
		t.Errorf("expected socks outbound, got %v", results[1].Outbound)
	}

	if results[2].Error == "" {
		t.Error("expected error for unsupported protocol")
	}
	if results[2].Outbound != nil {
		t.Error("expected nil outbound for unsupported protocol")
	}
}

// TestExponentialBackoff: recordFailure increases delay, clearFailure resets.
func TestExponentialBackoff(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	id := "sub_backoff_test"

	// First failure: delay = backoffBase (5 min)
	svc.recordFailure(id)
	val, ok := svc.retries.Load(id)
	if !ok {
		t.Fatal("expected retry state after first failure")
	}
	rs := val.(*retryState)
	if rs.failCount != 1 {
		t.Errorf("expected failCount=1, got %d", rs.failCount)
	}
	expectedDelay1 := backoffBase
	actualDelay1 := time.Until(rs.nextRetry)
	if actualDelay1 < expectedDelay1-time.Second || actualDelay1 > expectedDelay1+time.Second {
		t.Errorf("expected delay ~%v, got %v", expectedDelay1, actualDelay1)
	}

	// Second failure: delay = backoffBase * 2
	svc.recordFailure(id)
	val, _ = svc.retries.Load(id)
	rs = val.(*retryState)
	if rs.failCount != 2 {
		t.Errorf("expected failCount=2, got %d", rs.failCount)
	}
	expectedDelay2 := backoffBase * 2
	actualDelay2 := time.Until(rs.nextRetry)
	if actualDelay2 < expectedDelay2-time.Second || actualDelay2 > expectedDelay2+time.Second {
		t.Errorf("expected delay ~%v, got %v", expectedDelay2, actualDelay2)
	}

	// clearFailure removes state
	svc.clearFailure(id)
	if _, ok := svc.retries.Load(id); ok {
		t.Error("expected retry state to be cleared after success")
	}
}

// TestBackoffCap: after enough failures, delay caps at backoffMax.
func TestBackoffCap(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	id := "sub_cap_test"

	// Trigger enough failures that 2^(failCount-1) * backoffBase > backoffMax
	for i := 0; i < 20; i++ {
		svc.recordFailure(id)
	}
	val, ok := svc.retries.Load(id)
	if !ok {
		t.Fatal("expected retry state")
	}
	rs := val.(*retryState)
	actualDelay := time.Until(rs.nextRetry)
	if actualDelay > backoffMax+time.Second {
		t.Errorf("expected delay capped at %v, got %v", backoffMax, actualDelay)
	}
}

// TestMihomoSubscriptionType: refresh of type "mihomo" writes proxies into config.yaml in-place.
func TestMihomoSubscriptionType(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	yamlContent := `proxies:
  - name: TestProxy
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: testpass
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(yamlContent))
	}))
	defer ts.Close()

	sub := Subscription{
		Name:         "Mihomo Sub",
		URL:          ts.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	id := svc.List()[0].ID
	if err := svc.Refresh(id); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got := svc.List()[0]

	providerName := getMihomoProviderName(sub.Name, sub.URL, id)
	// config.yaml должен содержать proxy-providers и имя провайдера подписки.
	configPath := filepath.Join(tmp, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config.yaml at %s: %v", configPath, err)
	}
	if !strings.Contains(string(data), "proxy-providers:") {
		t.Error("expected 'proxy-providers:' in config.yaml after refresh")
	}
	if !strings.Contains(string(data), providerName) {
		t.Errorf("expected provider name %q in config.yaml after refresh", providerName)
	}

	// Файл провайдера должен содержать TestProxy
	providerPath := filepath.Join(tmp, "providers", providerName+".yaml")
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("expected provider file at %s: %v", providerPath, err)
	}
	if !strings.Contains(string(providerData), "TestProxy") {
		t.Error("expected 'TestProxy' in provider file after refresh")
	}

	// ProxyCount (via LastCount) must be 1.
	if got.ProxyCount != 1 {
		t.Errorf("expected ProxyCount=1, got %d", got.ProxyCount)
	}
	if len(got.ProxyNames) != 1 || got.ProxyNames[0] != "TestProxy" {
		t.Errorf("expected ProxyNames=[TestProxy], got %v", got.ProxyNames)
	}
}

func TestSubscriptionTrafficAndRules(t *testing.T) {
	// 1. Test parseSubscriptionUserinfo
	upload, download, total, expire := parseSubscriptionUserinfo("upload=1073741824; download=5368709120; total=107374182400; expire=1700000000")
	if upload != 1073741824 || download != 5368709120 || total != 107374182400 || expire != 1700000000 {
		t.Errorf("parseSubscriptionUserinfo failed: upload=%d, download=%d, total=%d, expire=%d", upload, download, total, expire)
	}

	// 2. Test countMihomoRules
	yaml := `
port: 7890
socks-port: 7891
rules:
  - DOMAIN-SUFFIX,google.com,PROXY
  - DOMAIN-KEYWORD,google,PROXY
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`
	rulesCount := countMihomoRules(yaml)
	if rulesCount != 4 {
		t.Errorf("expected 4 rules, got %d", rulesCount)
	}

	// Test case where rules is at the end or followed by another block
	yaml2 := `
rules:
  - DOMAIN,example.com,DIRECT
proxies:
  - name: proxy
    type: socks5
`
	rulesCount2 := countMihomoRules(yaml2)
	if rulesCount2 != 1 {
		t.Errorf("expected 1 rule in yaml2, got %d", rulesCount2)
	}

	// 3. Test Subscription Refresh with Userinfo header
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=1000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("proxies:\n  - name: p1\n    type: ss\nrules:\n  - MATCH,p1"))
	}))
	defer ts.Close()

	sub := Subscription{
		Name:         "Traffic Sub",
		URL:          ts.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatal(err)
	}

	id := svc.List()[0].ID
	if err := svc.Refresh(id); err != nil {
		t.Fatal(err)
	}

	got := svc.Get(id)
	if got.Upload != 100 || got.Download != 200 || got.Total != 1000 {
		t.Errorf("expected traffic values 100, 200, 1000; got %d, %d, %d", got.Upload, got.Download, got.Total)
	}
	if got.RuleCount != 1 {
		t.Errorf("expected RuleCount=1, got %d", got.RuleCount)
	}
}

func TestRoutingFragmentAutoMode(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "xray")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	outbounds := []Outbound{
		{Tag: "pfx-node1", Protocol: "vless"},
		{Tag: "pfx-node2", Protocol: "vless"},
	}
	sub := &Subscription{ID: "sub1", TagPrefix: "pfx"}
	svc := &SubscriptionService{dataDir: tmp, configDir: configDir}

	routingPath := svc.getRoutingFragmentPath(sub)
	if err := svc.writeRoutingFragment(routingPath, sub, []string{"pfx-node1", "pfx-node2"}); err != nil {
		t.Fatalf("writeRoutingFragment: %v", err)
	}

	data, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatalf("routing fragment not written: %v", err)
	}

	var frag struct {
		Routing struct {
			Balancers []struct {
				Tag      string   `json:"tag"`
				Selector []string `json:"selector"`
			} `json:"balancers"`
			Rules []struct {
				BalancerTag string   `json:"balancerTag"`
				Domain      []string `json:"domain"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &frag); err != nil {
		t.Fatalf("invalid routing JSON: %v", err)
	}

	if len(frag.Routing.Balancers) != 1 {
		t.Fatalf("expected 1 balancer, got %d", len(frag.Routing.Balancers))
	}
	if frag.Routing.Balancers[0].Selector[0] != "pfx-" {
		t.Errorf("selector: %v", frag.Routing.Balancers[0].Selector)
	}
	if len(frag.Routing.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(frag.Routing.Rules))
	}
	if frag.Routing.Rules[0].BalancerTag != "sub1-balancer" {
		t.Errorf("balancerTag: %v", frag.Routing.Rules[0].BalancerTag)
	}

	// Verify domain list contains geolocation-!cn
	hasDomain := false
	for _, d := range frag.Routing.Rules[0].Domain {
		if d == "geosite:geolocation-!cn" {
			hasDomain = true
		}
	}
	if !hasDomain {
		t.Errorf("missing geosite:geolocation-!cn in rule domains: %v", frag.Routing.Rules[0].Domain)
	}

	// Without TagPrefix — should use direct outboundTag
	sub2 := &Subscription{ID: "sub2", TagPrefix: ""}
	path2 := svc.getRoutingFragmentPath(sub2)
	if err := svc.writeRoutingFragment(path2, sub2, []string{"node1"}); err != nil {
		t.Fatalf("writeRoutingFragment no-prefix: %v", err)
	}
	data2, _ := os.ReadFile(path2)
	var frag2 struct {
		Routing struct {
			Rules []struct {
				OutboundTag string `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	json.Unmarshal(data2, &frag2)
	if len(frag2.Routing.Rules) == 0 || frag2.Routing.Rules[0].OutboundTag != "node1" {
		t.Errorf("outboundTag: %v", frag2.Routing.Rules)
	}

	_ = outbounds
}

func TestSubscriptionDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	// Сервер, возвращающий vmess и невалидную строку
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("profile-title", "base64:VGVzdCBPbmU=")
		w.Header().Set("support-url", "https://t.me/support")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("vmess://eyJhZGQiOiIxLjIuMy40IiwicG9ydCI6IjQ0MyIsImlkIjoiNTUwZTg0MDAtZTI5Yi00MWQ0LWE3MTYtNDQ2NjU1NDQwMDAwIiwicHMiOiJ2bWVzcy1ub2RlIiwibmV0IjoidGNwIiwidHlwZSI6Im5vbmUifQ==\ninvalid-line\n"))
	}))
	defer ts.Close()

	sub := Subscription{
		ID:         "diag_test",
		Name:       "Diag Test",
		URL:        ts.URL,
		Enabled:    true,
		EnableXray: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	err := svc.Refresh("diag_test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// 1. Проверяем GetRaw
	body, headers, err := svc.GetRaw("diag_test")
	if err != nil {
		t.Fatalf("GetRaw failed: %v", err)
	}
	if !strings.Contains(body, "vmess://") {
		t.Errorf("expected body to contain vmess, got %q", body)
	}
	if headers["Support-Url"][0] != "https://t.me/support" {
		t.Errorf("expected support-url header, got %v", headers["Support-Url"])
	}

	// 2. Проверяем GetParseReport
	report, err := svc.GetParseReport("diag_test")
	if err != nil {
		t.Fatalf("GetParseReport failed: %v", err)
	}
	if report.ParsedCount != 1 {
		t.Errorf("expected 1 parsed node, got %d", report.ParsedCount)
	}
	if report.SkippedCount != 1 {
		t.Errorf("expected 1 skipped line, got %d", report.SkippedCount)
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("expected 1 skip reason, got %d", len(report.Skipped))
	}
	if report.Skipped[0].Line != 2 {
		t.Errorf("expected line 2 skipped, got %d", report.Skipped[0].Line)
	}
	if report.Skipped[0].Snippet != "invalid-line" {
		t.Errorf("expected snippet 'invalid-line', got %q", report.Skipped[0].Snippet)
	}

	// 3. Проверяем удаление
	if err := svc.Delete("diag_test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, _, err = svc.GetRaw("diag_test")
	if err == nil {
		t.Error("expected error getting raw after delete, got nil")
	}
}

func TestParseXrayConfigArray(t *testing.T) {
	body := []byte(`[
		{
			"remarks": "🇩🇪 Germany",
			"dns": {"servers": ["1.1.1.1"]},
			"outbounds": [
				{
					"tag": "proxy",
					"protocol": "vless",
					"settings": {"vnext": [{"address": "de.example.com", "port": 443, "users": [{"id": "uuid-1", "encryption": "none"}]}]},
					"streamSettings": {"network": "tcp", "security": "reality"}
				},
				{"tag": "direct", "protocol": "freedom", "settings": {}}
			]
		},
		{
			"remarks": "🇳🇱 Netherlands",
			"dns": {"servers": ["8.8.8.8"]},
			"outbounds": [
				{
					"tag": "proxy",
					"protocol": "vless",
					"settings": {"vnext": [{"address": "nl.example.com", "port": 443, "users": [{"id": "uuid-2", "encryption": "none"}]}]},
					"streamSettings": {"network": "tcp"}
				}
			]
		}
	]`)

	outs := parseXrayConfigArray(body)
	if len(outs) != 2 {
		t.Fatalf("expected 2 outbounds, got %d", len(outs))
	}
	if outs[0].Tag != "🇩🇪 Germany" {
		t.Errorf("expected tag '🇩🇪 Germany', got %q", outs[0].Tag)
	}
	if outs[0].Protocol != "vless" {
		t.Errorf("expected protocol vless, got %q", outs[0].Protocol)
	}
	if outs[1].Tag != "🇳🇱 Netherlands" {
		t.Errorf("expected tag '🇳🇱 Netherlands', got %q", outs[1].Tag)
	}
}

func TestParseXrayConfigArray_IgnoresNonConfigArrays(t *testing.T) {
	// Plain outbound array should NOT match (no nested outbounds)
	body := []byte(`[{"tag":"node1","protocol":"vless","settings":{}}]`)
	if outs := parseXrayConfigArray(body); len(outs) != 0 {
		t.Errorf("expected 0, got %d outbounds for plain outbound array", len(outs))
	}

	// Object should NOT match
	body2 := []byte(`{"outbounds":[{"tag":"t","protocol":"vless","settings":{}}]}`)
	if outs := parseXrayConfigArray(body2); len(outs) != 0 {
		t.Errorf("expected 0, got %d outbounds for object format", len(outs))
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

func TestParseShareLink_VmessTooBig(t *testing.T) {
	// 1. vmess:// link > 8192 bytes returns nil
	tooBigLink := "vmess://" + strings.Repeat("A", 8200)
	ob := parseShareLink(tooBigLink)
	if ob != nil {
		t.Error("expected nil for oversized vmess:// link")
	}

	// 2. vmess:// link <= 8192 bytes is not skipped on length (but can be invalid json/b64)
	validJSON := `{"ps":"test","add":"1.2.3.4","port":"443","id":"uuid","net":"tcp"}`
	b64Valid := base64.StdEncoding.EncodeToString([]byte(validJSON))
	normalLink := "vmess://" + b64Valid
	if len(normalLink) <= 8192 {
		obNormal := parseShareLink(normalLink)
		if obNormal == nil {
			t.Error("expected non-nil for valid normal vmess:// link")
		} else if obNormal.Tag != "test" {
			t.Errorf("expected tag 'test', got %q", obNormal.Tag)
		}
	}

	// 3. vless:// link of any length is not skipped on length
	longVless := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#" + strings.Repeat("A", 9000)
	obVless := parseShareLink(longVless)
	if obVless == nil {
		t.Error("expected non-nil for long vless link")
	} else if obVless.Tag != strings.Repeat("A", 9000) {
		t.Error("long vless link parsed incorrectly")
	}
}

func TestSubscriptionService_MihomoProxyProvider(t *testing.T) {
	tmp := t.TempDir()

	// 1. Настроить мок-сервер подписки
	yamlContent := `proxies:
  - name: node1
    type: ss
    server: 1.2.3.4
    port: 443
    cipher: chacha20-ietf-poly1305
    password: test
  - name: node2
    type: vmess
    server: 5.6.7.8
    port: 443
    uuid: uuid
    alterId: 0
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	// 2. Создать mock-xkeen скрипт для ConsoleService
	logFile := filepath.Join(tmp, "xkeen_calls.log")
	scriptContent := fmt.Sprintf("#!/bin/sh\necho \"$1\" >> %q\n", logFile)
	mockXkeenPath := filepath.Join(tmp, "mock-xkeen")
	if err := os.WriteFile(mockXkeenPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	// 3. Инициализировать сервис подписок
	mihomoDir := filepath.Join(tmp, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(mihomoDir, "config.yaml")
	initialConfig := `port: 9090
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}

	svc := NewSubscriptionService(tmp, tmp, mihomoDir)
	svc.httpClient = srv.Client()

	consoleSvc := NewConsoleService(mockXkeenPath)
	svc.SetConsoleService(consoleSvc)

	// 4. Добавить подписку Mihomo
	sub := Subscription{
		ID:           "mihomo-sub",
		Name:         "Mihomo Sub Test",
		URL:          srv.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
		Interval:     1,
		MihomoGroups: []string{"PROXY"},
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("failed to add subscription: %v", err)
	}

	// 5. Запустить Refresh
	if err := svc.Refresh("mihomo-sub"); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// 6. Проверить config.yaml
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configStr := string(configBytes)

	if !strings.Contains(configStr, "proxy-providers:") {
		t.Error("config.yaml should contain proxy-providers:")
	}
	providerName := getMihomoProviderName(sub.Name, sub.URL, sub.ID)
	if !strings.Contains(configStr, providerName+":") {
		t.Errorf("config.yaml should contain provider name %q", providerName)
	}
	if !strings.Contains(configStr, "use:\n      - "+providerName) {
		t.Errorf("config.yaml group should use provider, got:\n%s", configStr)
	}

	// Проверить что файл провайдера записан
	providerFilePath := filepath.Join(mihomoDir, "providers", providerName+".yaml")
	if _, err := os.Stat(providerFilePath); err != nil {
		t.Errorf("provider file should be written at %s: %v", providerFilePath, err)
	}

	// Проверить что произошел restart
	callsBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called")
	}

	// 7. Повторный Refresh без изменений не должен вызывать restart
	if err := os.WriteFile(logFile, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh("mihomo-sub"); err != nil {
		t.Fatal(err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should NOT be called when no changes")
	}

	// 8. Удалить подписку и проверить очистку
	if err := svc.Delete("mihomo-sub"); err != nil {
		t.Fatalf("failed to delete subscription: %v", err)
	}

	configBytes, _ = os.ReadFile(configPath)
	configStr = string(configBytes)
	if strings.Contains(configStr, "mihomo-sub") {
		t.Error("provider and references should be removed from config.yaml after deletion")
	}
	if _, err := os.Stat(providerFilePath); !os.IsNotExist(err) {
		t.Error("provider file should be deleted")
	}
}

func TestRefreshXray_DoesNotRestartXray(t *testing.T) {
	tmp := t.TempDir()

	// 1. Настроить мок-сервер
	vmessJSON1 := `{"ps":"xray-node","add":"1.1.1.1","port":"443","id":"uuid","net":"tcp"}`
	vmessJSON2 := `{"ps":"xray-node-new","add":"2.2.2.2","port":"443","id":"uuid","net":"tcp"}`
	b64_1 := base64.StdEncoding.EncodeToString([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON1))))
	b64_2 := base64.StdEncoding.EncodeToString([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON2))))

	var responseContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseContent))
	}))
	defer srv.Close()

	// 2. Создать mock-xkeen скрипт
	logFile := filepath.Join(tmp, "xkeen_calls.log")
	scriptContent := fmt.Sprintf("#!/bin/sh\necho \"$1\" >> %q\n", logFile)
	mockXkeenPath := filepath.Join(tmp, "mock-xkeen")
	if err := os.WriteFile(mockXkeenPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	xrayConfigDir := filepath.Join(tmp, "xray")
	_ = os.MkdirAll(xrayConfigDir, 0755)

	svc := NewSubscriptionService(tmp, xrayConfigDir, tmp)
	svc.httpClient = srv.Client()

	consoleSvc := NewConsoleService(mockXkeenPath)
	svc.SetConsoleService(consoleSvc)

	// 3. Добавить Xray подписку
	sub := Subscription{
		ID:           "xray-sub",
		Name:         "Xray Sub Test",
		URL:          srv.URL,
		EnableXray:   true,
		EnableMihomo: false,
		Enabled:      true,
		Interval:     1,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatal(err)
	}

	// 4. Первый Refresh (файл записывается впервые)
	responseContent = b64_1
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("first Refresh failed: %v", err)
	}

	// Должен быть рестарт
	callsBytes, _ := os.ReadFile(logFile)
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called on first refresh")
	}

	// 5. Второй Refresh без изменений (хэш совпадает)
	_ = os.WriteFile(logFile, []byte(""), 0600)
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("second Refresh failed: %v", err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should NOT be called if configuration has not changed")
	}

	// 6. Третий Refresh с изменениями (хэш отличается)
	_ = os.WriteFile(logFile, []byte(""), 0600)
	responseContent = b64_2
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("third Refresh failed: %v", err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called when config changed")
	}
}

func TestRefreshMihomo_ConcurrentRace(t *testing.T) {
	yamlContent := `proxies:
  - {name: node1, type: ss, server: 1.2.3.4, port: 443, cipher: chacha20-ietf-poly1305, password: test}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = srv.Client()

	sub := Subscription{
		ID:           "race-sub",
		URL:          srv.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add subscription failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCopy := svc.Get("race-sub")
			if subCopy != nil {
				body, headers, err := svc.downloadRaw(subCopy.URL, subCopy)
				if err == nil {
					_ = svc.refreshMihomo(subCopy, body, headers)
				}
			}
		}()
	}
	wg.Wait()
}

func TestXrayConfigurationHardening(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	sub := &Subscription{ID: "test", Name: "test"}

	outbounds := []Outbound{
		{Tag: "vless-node", Protocol: "vless"},
		{Tag: "hy2-node", Protocol: "hysteria2"},
		{Tag: "tuic-node", Protocol: "tuic"},
		{Tag: "vmess-node", Protocol: "vmess"},
	}

	path := svc.getFragmentPath(sub)
	nodes, err := svc.writeFragment(path, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	// The returned nodes should contain all 4 items (so frontend can import them)
	if len(nodes) != 4 {
		t.Errorf("expected 4 returned nodes, got %d", len(nodes))
	}

	// But the written JSON file should only contain 2 outbounds (vless and vmess)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatal(err)
	}

	if len(wrapper.Outbounds) != 2 {
		t.Errorf("expected only 2 outbounds written to file, got %d", len(wrapper.Outbounds))
	}

	for _, ob := range wrapper.Outbounds {
		if ob.Protocol == "hysteria2" || ob.Protocol == "tuic" {
			t.Errorf("unsupported protocol %s was written to file", ob.Protocol)
		}
	}
}

func TestParseHysteria2Link_Synonyms(t *testing.T) {
	link := "hysteria2://mypassword@hy2.example.com:443?sni=hy2.example.com&obfs=simple&obfs-pass=obfspass&skip-cert-verify=true#hy2server"
	ob := parseShareLink(link)
	if ob == nil {
		t.Fatal("expected non-nil Outbound for hysteria2:// prefix and synonyms")
	}
	if ob.Protocol != "hysteria2" {
		t.Errorf("expected protocol=hysteria2, got %q", ob.Protocol)
	}

	// Test obfs-pass and skip-cert-verify
	hy2Settings, ok := ob.Settings["hysteria2Settings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hysteria2Settings in settings")
	}
	obfsMap, ok := hy2Settings["obfs"].(map[string]interface{})
	if !ok {
		t.Fatal("expected obfs in hysteria2Settings")
	}
	if obfsMap["password"] != "obfspass" {
		t.Errorf("expected obfs password obfspass, got %v", obfsMap["password"])
	}

	tlsSettings, ok := ob.StreamSettings["tlsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected tlsSettings")
	}
	if tlsSettings["allowInsecure"] != true {
		t.Error("expected allowInsecure to be true")
	}
}

func TestParseShareLink_EdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantNil  bool
		wantType string // "" = don't check
	}{
		// Boundary conditions
		{"empty_string", "", true, ""},
		{"only_scheme", "vless://", true, ""},
		{"no_scheme", "host:443", true, ""},
		{"double_scheme", "vless://vless://host:443", true, ""},
		{"null_bytes", "vless://\x00@host:443#tag", true, ""},

		// Port validation
		{"port_zero", "vless://uuid@host:0#tag", true, ""},
		{"port_overflow", "vless://uuid@host:99999#tag", true, ""},
		{"port_negative", "vless://uuid@host:-1#tag", true, ""},
		{"port_non_numeric", "vless://uuid@host:abc#tag", true, ""},

		// VMess base64 edge cases
		{"vmess_invalid_b64", "vmess://not-valid-base64!", true, ""},
		{"vmess_empty_json", "vmess://e30=", true, ""},  // {}
		{"vmess_json_array", "vmess://W10=", true, ""},  // []

		// IPv6
		{"vless_ipv6_brackets", "vless://uuid@[::1]:443?security=none#ipv6", false, "vless"},
		{"trojan_ipv6", "trojan://pass@[::1]:443#ipv6", false, "trojan"},

		// URL encoding
		{"trojan_encoded_pass", "trojan://p%40ss@host:443#tag", false, "trojan"},

		// Unknown scheme
		{"unknown_scheme", "wireguard://key@host:51820#tag", true, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseShareLink(tc.input)
			if tc.wantNil && result != nil {
				t.Errorf("expected nil for %q, got %+v", tc.input, result)
			}
			if !tc.wantNil && result == nil {
				t.Errorf("expected non-nil for %q", tc.input)
			}
		})
	}
}

func TestParseTrojanLink_EdgeCases(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantNil      bool
		wantPassword string
	}{
		{"valid_encoded_password", "trojan://p%40ss%23word@host:443#tag", false, "p@ss#word"},
		{"empty_password", "trojan://@host:443#tag", false, ""},
		{"space_in_port", "trojan://pass@host 443#tag", true, ""},
		{"ipv6_address", "trojan://pass@[::1]:443#tag", false, "pass"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseTrojanLink(tc.input)
			if tc.wantNil {
				if result != nil {
					t.Errorf("expected nil for %q, got %+v", tc.input, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected non-nil for %q", tc.input)
			}
			servers, ok := result.Settings["servers"].([]map[string]interface{})
			if !ok || len(servers) == 0 {
				t.Fatal("expected non-empty servers list in settings")
			}
			gotPassword := servers[0]["password"].(string)
			if gotPassword != tc.wantPassword {
				t.Errorf("expected password %q, got %q", tc.wantPassword, gotPassword)
			}
		})
	}
}

func TestParseSSLink_EdgeCases(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantNil    bool
		wantMethod string
		wantPass   string
	}{
		{"sip002_format", "ss://method:password@host:8388#tag", false, "method", "password"},
		{"legacy_base64_format", "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ=@host:8388#tag", false, "aes-128-gcm", "password"},
		{"legacy_invalid_base64", "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ!@host:8388#tag", false, "YWVzLTEyOC1nY206cGFzc3dvcmQ!", ""},
		{"missing_userinfo", "ss://@host:8388#tag", false, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseSSLink(tc.input)
			if tc.wantNil {
				if result != nil {
					t.Errorf("expected nil for %q, got %+v", tc.input, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected non-nil for %q", tc.input)
			}
			servers, ok := result.Settings["servers"].([]map[string]interface{})
			if !ok || len(servers) == 0 {
				t.Fatal("expected non-empty servers list in settings")
			}
			gotMethod := servers[0]["method"].(string)
			gotPass := servers[0]["password"].(string)
			if gotMethod != tc.wantMethod {
				t.Errorf("expected method %q, got %q", tc.wantMethod, gotMethod)
			}
			if gotPass != tc.wantPass {
				t.Errorf("expected password %q, got %q", tc.wantPass, gotPass)
			}
		})
	}
}

func TestSubscriptionService_MihomoAPIProviderReload(t *testing.T) {
	var calledPath string
	var calledMethod string
	var authHeader string

	// 1. Mock Mihomo REST API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		calledMethod = r.Method
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer apiServer.Close()

	tmp := t.TempDir()
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(mihomoDir, 0755)
	configPath := filepath.Join(mihomoDir, "config.yaml")

	// Set initial configuration
	initialConfig := `port: 9090
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
`
	_ = os.WriteFile(configPath, []byte(initialConfig), 0600)

	// Mock subscription source server
	yamlContent := `proxies:
  - name: node1
    type: ss
    server: 1.2.3.4
    port: 443
    cipher: chacha20-ietf-poly1305
    password: test
`
	subServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer subServer.Close()

	svc := NewSubscriptionService(tmp, tmp, mihomoDir)
	svc.httpClient = subServer.Client()
	svc.SetMihomoAPI(apiServer.URL, "test-secret-token")

	sub := Subscription{
		ID:           "mihomo-reload-test",
		Name:         "Mihomo reload test",
		URL:          subServer.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
		Interval:     1,
		MihomoGroups: []string{"PROXY"},
	}
	_ = svc.Add(&sub)

	// Run first refresh to populate config.yaml (this will trigger a restart because config changes, not REST API PUT)
	_ = svc.Refresh("mihomo-reload-test")

	// Clear API server tracker
	calledPath = ""
	calledMethod = ""
	authHeader = ""

	// Run second refresh with identical proxies (no config change, newHash == oldHash) -> this MUST trigger REST API PUT!
	err := svc.Refresh("mihomo-reload-test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	providerName := getMihomoProviderName(sub.Name, sub.URL, sub.ID)
	expectedPath := "/providers/proxies/" + providerName

	if calledMethod != http.MethodPut {
		t.Errorf("expected Method PUT, got %q", calledMethod)
	}
	if calledPath != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, calledPath)
	}
	if authHeader != "Bearer test-secret-token" {
		t.Errorf("expected Authorization header 'Bearer test-secret-token', got %q", authHeader)
	}
}

func TestSubscriptionService_ClashYAMLToXrayOutbounds(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	_ = os.MkdirAll(xrayDir, 0755)

	// Mock Clash YAML subscription source
	yamlContent := `proxies:
  - name: "🇩🇪 Germany VLESS"
    type: vless
    server: de.example.com
    port: 443
    uuid: uuid-vless
    tls: true
    servername: de.example.com
  - name: "🇳🇱 Netherlands VMess"
    type: vmess
    server: nl.example.com
    port: 8080
    uuid: uuid-vmess
    alterId: 0
    network: ws
    ws-opts:
      path: /vmessws
  - name: "🇸🇬 Singapore Shadowsocks"
    type: ss
    server: sg.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-pass
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	svc := NewSubscriptionService(tmp, xrayDir, tmp)
	svc.httpClient = srv.Client()

	sub := Subscription{
		ID:           "clash-to-xray-test",
		Name:         "Clash to Xray Test",
		URL:          srv.URL,
		EnableXray:   true,
		EnableMihomo: false,
		Enabled:      true,
	}
	_ = svc.Add(&sub)

	// Run Refresh. This should download Clash YAML and parse it successfully as Xray outbounds!
	err := svc.Refresh("clash-to-xray-test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Read generated fragment file
	fragmentPath := svc.getFragmentPath(&sub)
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("fragment file not found: %v", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("failed to unmarshal generated outbounds: %v", err)
	}

	// We expect 3 parsed outbounds
	if len(wrapper.Outbounds) != 3 {
		t.Fatalf("expected 3 outbounds, got %d", len(wrapper.Outbounds))
	}

	// Check VLESS
	o0 := wrapper.Outbounds[0]
	if o0.Tag != "🇩🇪 Germany VLESS" || o0.Protocol != "vless" {
		t.Errorf("o0 invalid: Tag=%q, Protocol=%q", o0.Tag, o0.Protocol)
	}
	if o0.StreamSettings["security"] != "tls" {
		t.Errorf("o0 security expected tls, got %v", o0.StreamSettings["security"])
	}

	// Check VMess
	o1 := wrapper.Outbounds[1]
	if o1.Tag != "🇳🇱 Netherlands VMess" || o1.Protocol != "vmess" {
		t.Errorf("o1 invalid: Tag=%q, Protocol=%q", o1.Tag, o1.Protocol)
	}
	if o1.StreamSettings["network"] != "ws" {
		t.Errorf("o1 network expected ws, got %v", o1.StreamSettings["network"])
	}
	wsSettings := o1.StreamSettings["wsSettings"].(map[string]interface{})
	if wsSettings["path"] != "/vmessws" {
		t.Errorf("o1 ws path expected /vmessws, got %v", wsSettings["path"])
	}

	// Check Shadowsocks
	o2 := wrapper.Outbounds[2]
	if o2.Tag != "🇸🇬 Singapore Shadowsocks" || o2.Protocol != "shadowsocks" {
		t.Errorf("o2 invalid: Tag=%q, Protocol=%q", o2.Tag, o2.Protocol)
	}
}

