package services

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseSingBoxJSON_VLESSReality(t *testing.T) {
	body := []byte(`{
  "outbounds": [
    {
      "type": "vless",
      "tag": "🇩🇪 Germany",
      "server": "de.example.com",
      "server_port": 443,
      "uuid": "abc-123",
      "flow": "xtls-rprx-vision",
      "tls": {
        "enabled": true,
        "server_name": "www.cloudflare.com",
        "reality": {
          "enabled": true,
          "public_key": "PUB_KEY_HERE",
          "short_id": "deadbeef"
        },
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      }
    }
  ]
}`)

	outs, err := parseSingBoxJSON(body)
	if err != nil {
		t.Fatalf("parseSingBoxJSON returned error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outbound, got %d", len(outs))
	}

	ob := outs[0]
	if ob.Protocol != "vless" {
		t.Errorf("expected protocol vless, got %s", ob.Protocol)
	}
	if ob.Tag != "🇩🇪 Germany" {
		t.Errorf("expected tag 🇩🇪 Germany, got %s", ob.Tag)
	}

	vnext, ok := ob.Settings["vnext"].([]map[string]interface{})
	if !ok || len(vnext) != 1 {
		t.Fatalf("expected vnext[1], got %v", ob.Settings["vnext"])
	}
	if vnext[0]["address"] != "de.example.com" || vnext[0]["port"] != 443 {
		t.Errorf("wrong address/port: %v", vnext[0])
	}

	if ob.StreamSettings["security"] != "reality" {
		t.Errorf("expected security=reality, got %v", ob.StreamSettings["security"])
	}
	reality, ok := ob.StreamSettings["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected realitySettings map, got %T", ob.StreamSettings["realitySettings"])
	}
	if reality["publicKey"] != "PUB_KEY_HERE" {
		t.Errorf("expected publicKey, got %v", reality["publicKey"])
	}
	if reality["shortId"] != "deadbeef" {
		t.Errorf("expected shortId=deadbeef, got %v", reality["shortId"])
	}
	if reality["fingerprint"] != "chrome" {
		t.Errorf("expected fingerprint=chrome, got %v", reality["fingerprint"])
	}
}

func TestParseSingBoxJSON_VLESSWebSocket(t *testing.T) {
	body := []byte(`[
  {
    "type": "vless",
    "tag": "WS-TLS",
    "server": "ws.example.com",
    "server_port": 443,
    "uuid": "xxx",
    "tls": {"enabled": true, "server_name": "ws.example.com"},
    "transport": {
      "type": "ws",
      "path": "/api/ws",
      "host": "ws.example.com",
      "headers": {"X-Custom": "value"}
    }
  }
]`)

	outs, err := parseSingBoxJSON(body)
	if err != nil {
		t.Fatalf("parseSingBoxJSON returned error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outbound, got %d", len(outs))
	}

	ss := outs[0].StreamSettings
	if ss["network"] != "ws" {
		t.Errorf("expected network=ws, got %v", ss["network"])
	}
	ws, ok := ss["wsSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected wsSettings map, got %T", ss["wsSettings"])
	}
	if ws["path"] != "/api/ws" {
		t.Errorf("expected path=/api/ws, got %v", ws["path"])
	}
	headers, ok := ws["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected headers map, got %T", ws["headers"])
	}
	if headers["Host"] != "ws.example.com" {
		t.Errorf("expected Host=ws.example.com, got %v", headers["Host"])
	}
	if headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom=value, got %v", headers["X-Custom"])
	}
}

func TestParseSingBoxJSON_SkipsNonProxyOutbounds(t *testing.T) {
	body := []byte(`{
  "outbounds": [
    {"type": "direct", "tag": "direct"},
    {"type": "block", "tag": "block"},
    {"type": "selector", "tag": "auto", "outbounds": ["a", "b"]},
    {"type": "vless", "tag": "real", "server": "x.com", "server_port": 443, "uuid": "x"}
  ]
}`)

	outs, err := parseSingBoxJSON(body)
	if err != nil {
		t.Fatalf("parseSingBoxJSON returned error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 outbound (vless only), got %d", len(outs))
	}
	if outs[0].Tag != "real" {
		t.Errorf("expected only vless outbound, got %s", outs[0].Tag)
	}
}

func TestLooksLikeSingBoxJSON(t *testing.T) {
	if !looksLikeSingBoxJSON([]byte(`{"outbounds":[{"server_port":443}]}`)) {
		t.Error("should detect server_port as sing-box marker")
	}
	if looksLikeSingBoxJSON([]byte(`{"outbounds":[{"port":443}]}`)) {
		t.Error("should NOT detect xray-json (no server_port)")
	}
}

func TestApplySubscriptionHeaders(t *testing.T) {
	sub := &Subscription{}
	h := http.Header{}
	h.Set("Subscription-Userinfo", "upload=100; download=200; total=1000; expire=1700000000")
	// "My VPN" в base64 = TXkgVlBO
	h.Set("profile-title", "TXkgVlBO")
	h.Set("profile-update-interval", "12")
	h.Set("support-url", "https://t.me/support_bot")
	h.Set("profile-web-page-url", "https://sub.example.com/abc123")

	applySubscriptionHeaders(h, sub)

	if sub.Upload != 100 || sub.Download != 200 || sub.Total != 1000 {
		t.Errorf("userinfo not parsed: %+v", sub)
	}
	if sub.Expire != 1700000000 {
		t.Errorf("expire not parsed: %d", sub.Expire)
	}
	if sub.ProfileTitle != "My VPN" {
		t.Errorf("expected ProfileTitle=My VPN, got %q", sub.ProfileTitle)
	}
	if sub.ProfileUpdateHours != 12 {
		t.Errorf("expected ProfileUpdateHours=12, got %d", sub.ProfileUpdateHours)
	}
	if sub.SupportURL != "https://t.me/support_bot" {
		t.Errorf("SupportURL wrong: %q", sub.SupportURL)
	}
	if sub.ProfileWebPageURL != "https://sub.example.com/abc123" {
		t.Errorf("ProfileWebPageURL wrong: %q", sub.ProfileWebPageURL)
	}
}

func TestApplySubscriptionHeaders_Base64Prefix(t *testing.T) {
	// Некоторые провайдеры добавляют префикс "base64:" перед значением.
	sub := &Subscription{}
	h := http.Header{}
	h.Set("profile-title", "base64:TXkgVlBO")
	applySubscriptionHeaders(h, sub)
	if sub.ProfileTitle != "My VPN" {
		t.Errorf("expected ProfileTitle=My VPN, got %q", sub.ProfileTitle)
	}
}

func TestSubscriptionUserAgent(t *testing.T) {
	svc := &SubscriptionService{} // без kernelSvc — используется fallback-версии
	if ua := svc.subscriptionUserAgent("mihomo"); !strings.HasPrefix(ua, "mihomo/") {
		t.Errorf("mihomo subscription should get mihomo/* UA, got %q", ua)
	}
	if ua := svc.subscriptionUserAgent("xray"); !strings.HasPrefix(ua, "v2rayN/") {
		t.Errorf("xray subscription should get v2rayN/* UA, got %q", ua)
	}
	if ua := svc.subscriptionUserAgent(""); !strings.HasPrefix(ua, "v2rayN/") {
		t.Errorf("default subscription should get v2rayN/* UA, got %q", ua)
	}
}
