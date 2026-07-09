package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestBase64FallbackInvalid(t *testing.T) {
	link := "vmess://!!!not-valid-base64!!!"
	ob := parseVMessLink(link)
	if ob != nil {
		t.Errorf("expected nil for invalid base64, got %+v", ob)
	}
}

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

func TestDownloadAndParseSchemeValidation(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, "/opt/etc/xray", "/opt/etc/mihomo")
	_, _, _, _, err := svc.downloadAndParse(context.Background(), "file:///etc/passwd", &Subscription{})
	if err == nil {
		t.Fatal("expected error for file:// URL, got nil")
	}
}

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

func TestParseLinks(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	links := []string{
		"vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#vlessnode",
		"socks5://user:pass@socks.example.com:1080#socksnode",
		"notaprotocol://garbage",
		"",
	}

	results := svc.ParseLinks(links)

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

func TestSubscriptionEntryLimit(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

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

	_, _, _, _, err := svc.downloadAndParse(context.Background(), ts5001.URL, &Subscription{})
	if err == nil {
		t.Error("expected error for 5001 entries, got nil")
	}

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

	_, _, _, _, err = svc.downloadAndParse(context.Background(), ts5000.URL, &Subscription{})
	if err != nil {
		t.Errorf("expected no error for 5000 entries, got: %v", err)
	}
}

func TestDownloadAndParse_NetworkError(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer ts.Close()

	_, _, _, _, err := svc.downloadAndParse(context.Background(), ts.URL, &Subscription{})
	if err == nil {
		t.Error("expected error for connection reset, got nil")
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
	body := []byte(`[{"tag":"node1","protocol":"vless","settings":{}}]`)
	if outs := parseXrayConfigArray(body); len(outs) != 0 {
		t.Errorf("expected 0, got %d outbounds for plain outbound array", len(outs))
	}

	body2 := []byte(`{"outbounds":[{"tag":"t","protocol":"vless","settings":{}}]}`)
	if outs := parseXrayConfigArray(body2); len(outs) != 0 {
		t.Errorf("expected 0, got %d outbounds for object format", len(outs))
	}
}

func TestParseShareLink_VmessTooBig(t *testing.T) {
	tooBigLink := "vmess://" + strings.Repeat("A", 8200)
	ob := parseShareLink(tooBigLink)
	if ob != nil {
		t.Error("expected nil for oversized vmess:// link")
	}

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

	longVless := "vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#" + strings.Repeat("A", 9000)
	obVless := parseShareLink(longVless)
	if obVless == nil {
		t.Error("expected non-nil for long vless link")
	} else if obVless.Tag != strings.Repeat("A", 9000) {
		t.Error("long vless link parsed incorrectly")
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
		wantType string
	}{
		{"empty_string", "", true, ""},
		{"only_scheme", "vless://", true, ""},
		{"no_scheme", "host:443", true, ""},
		{"double_scheme", "vless://vless://host:443", true, ""},
		{"null_bytes", "vless://\x00@host:443#tag", true, ""},

		{"port_zero", "vless://uuid@host:0#tag", true, ""},
		{"port_overflow", "vless://uuid@host:99999#tag", true, ""},
		{"port_negative", "vless://uuid@host:-1#tag", true, ""},
		{"port_non_numeric", "vless://uuid@host:abc#tag", true, ""},

		{"vmess_invalid_b64", "vmess://not-valid-base64!", true, ""},
		{"vmess_empty_json", "vmess://e30=", true, ""},
		{"vmess_json_array", "vmess://W10=", true, ""},

		{"vless_ipv6_brackets", "vless://uuid@[::1]:443?security=none#ipv6", false, "vless"},
		{"trojan_ipv6", "trojan://pass@[::1]:443#ipv6", false, "trojan"},

		{"trojan_encoded_pass", "trojan://p%40ss@host:443#tag", false, "trojan"},

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
