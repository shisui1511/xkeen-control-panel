package services

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	_, err := svc.downloadAndParse("file:///etc/passwd", &Subscription{})
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

	// Build 501 vless lines
	lines501 := make([]string, 501)
	for i := range lines501 {
		lines501[i] = "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#tag"
	}
	body501 := strings.Join(lines501, "\n")

	ts501 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body501))
	}))
	defer ts501.Close()

	_, err := svc.downloadAndParse(ts501.URL, &Subscription{})
	if err == nil {
		t.Error("expected error for 501 entries, got nil")
	}

	// Build exactly 500 vless lines
	lines500 := make([]string, 500)
	for i := range lines500 {
		lines500[i] = "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#tag"
	}
	body500 := strings.Join(lines500, "\n")

	ts500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body500))
	}))
	defer ts500.Close()

	_, err = svc.downloadAndParse(ts500.URL, &Subscription{})
	if err != nil {
		t.Errorf("expected no error for 500 entries, got: %v", err)
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
	if err := svc.writeFragment(path, outbounds, sub); err != nil {
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

	_, err := svc.downloadAndParse(ts.URL, &Subscription{})
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
	if err := svc.writeFragment(path, outbounds, got); err != nil {
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

// TestMihomoSubscriptionType: refresh of type "mihomo" writes a YAML provider file.
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
		Name:    "Mihomo Sub",
		URL:     ts.URL,
		Type:    "mihomo",
		Enabled: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	id := svc.List()[0].ID
	if err := svc.Refresh(id); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Provider file must exist
	got := svc.List()[0]
	provPath := svc.getMihomoProviderPath(&got)
	data, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatalf("expected provider file at %s: %v", provPath, err)
	}
	if !strings.Contains(string(data), "proxies:") {
		t.Error("expected 'proxies:' in provider file")
	}

	// ProxyCount should reflect one proxy
	if got.ProxyCount != 1 {
		t.Errorf("expected ProxyCount=1, got %d", got.ProxyCount)
	}
}

func TestSubscriptionTrafficAndRules(t *testing.T) {
	// 1. Test parseSubscriptionUserinfo
	upload, download, total := parseSubscriptionUserinfo("upload=1073741824; download=5368709120; total=107374182400; expire=1700000000")
	if upload != 1073741824 || download != 5368709120 || total != 107374182400 {
		t.Errorf("parseSubscriptionUserinfo failed: upload=%d, download=%d, total=%d", upload, download, total)
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
		Name:    "Traffic Sub",
		URL:     ts.URL,
		Type:    "mihomo",
		Enabled: true,
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
