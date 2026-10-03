package services

import (
	"encoding/json"
	"testing"
)

// httpupgradeStream достаёт streamSettings узла после JSON-кругового прохода,
// как их увидит Xray при чтении 04_outbounds.json.
func httpupgradeStream(t *testing.T, ob *Outbound) map[string]interface{} {
	t.Helper()
	if ob == nil {
		t.Fatal("узел не сконвертирован")
	}
	raw, err := json.Marshal(ob.StreamSettings)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Запись B31 этапа 10: Xray читает httpupgradeSettings.host как строку, массив
// отвергает вместе со всей конфигурацией.
func TestConvertSubscriptionNode_HTTPUpgradeHostIsString(t *testing.T) {
	node := &SubscriptionNode{
		Protocol:   "vless",
		UUID:       "11111111-2222-3333-4444-555555555555",
		Server:     "hu.example.com:443",
		Transport:  "httpupgrade",
		Security:   "tls",
		ServerName: "cdn.example.com",
		WSPath:     "/up",
	}
	stream := httpupgradeStream(t, convertSubscriptionNodeToOutbound(node))

	hu, ok := stream["httpupgradeSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("httpupgradeSettings отсутствует: %v", stream)
	}
	if host, ok := hu["host"].(string); !ok || host != "cdn.example.com" {
		t.Errorf("httpupgradeSettings.host = %#v, want строка cdn.example.com", hu["host"])
	}
	if hu["path"] != "/up" {
		t.Errorf("httpupgradeSettings.path = %#v, want /up", hu["path"])
	}
}

// У транспорта http (h2) host остаётся массивом строк.
func TestConvertSubscriptionNode_HTTPHostStaysArray(t *testing.T) {
	node := &SubscriptionNode{
		Protocol:   "vmess",
		UUID:       "11111111-2222-3333-4444-555555555555",
		Server:     "h2.example.com:443",
		Transport:  "http",
		Security:   "tls",
		ServerName: "h2.example.com",
		WSPath:     "/h2",
	}
	stream := httpupgradeStream(t, convertSubscriptionNodeToOutbound(node))

	h, ok := stream["httpSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("httpSettings отсутствует: %v", stream)
	}
	hosts, ok := h["host"].([]interface{})
	if !ok || len(hosts) != 1 || hosts[0] != "h2.example.com" {
		t.Errorf("httpSettings.host = %#v, want [h2.example.com]", h["host"])
	}
}

// Путь sing-box: httpupgrade пишется в httpupgradeSettings со строковым host, а
// не в httpSettings, где настройки терялись.
func TestConvertSingBoxStreamSettings_HTTPUpgrade(t *testing.T) {
	sb := &singBoxOutbound{
		Type:      "vless",
		Transport: &singBoxTrans{Type: "httpupgrade", Host: "cdn.example.com", Path: "/up"},
	}
	ss := convertSingBoxStreamSettings(sb)

	if _, wrong := ss["httpSettings"]; wrong {
		t.Errorf("httpupgrade записан в httpSettings: %v", ss)
	}
	hu, ok := ss["httpupgradeSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("httpupgradeSettings отсутствует: %v", ss)
	}
	if host, ok := hu["host"].(string); !ok || host != "cdn.example.com" {
		t.Errorf("httpupgradeSettings.host = %#v, want строка", hu["host"])
	}
	if hu["path"] != "/up" {
		t.Errorf("httpupgradeSettings.path = %#v", hu["path"])
	}
}
