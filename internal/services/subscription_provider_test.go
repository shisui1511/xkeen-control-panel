package services

import "testing"

func TestCountProviderNodes_HysteriaV1ShareLinks(t *testing.T) {
	payload := "hysteria://pass1@host1.example.com:443?sni=host1.example.com#node1\n" +
		"hysteria://pass2@host2.example.com:443?sni=host2.example.com#node2\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 hysteria:// nodes counted, got %d", count)
	}
}

func TestCountProviderNodes_Hysteria2Regression(t *testing.T) {
	payload := "hy2://pass1@host1.example.com:443#node1\n" +
		"hysteria2://pass2@host2.example.com:443#node2\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 hysteria2 nodes counted (regression), got %d", count)
	}
}

func TestCountProviderNodes_MixedHysteriaV1AndV2(t *testing.T) {
	payload := "hysteria://pass1@host1.example.com:443#v1\n" +
		"hysteria2://pass2@host2.example.com:443#v2\n" +
		"hy2://pass3@host3.example.com:443#v2b\n"
	count := countProviderNodes(payload)
	if count != 3 {
		t.Errorf("expected 3 nodes counted (1 hysteria + 2 hysteria2/hy2), got %d", count)
	}
}

func TestCountProviderNodes_ClashYAMLTypeHysteria(t *testing.T) {
	payload := "proxies:\n" +
		"  - name: node1\n" +
		"    type: hysteria\n" +
		"    server: host1.example.com\n" +
		"    port: 443\n" +
		"  - name: node2\n" +
		"    type: hysteria2\n" +
		"    server: host2.example.com\n" +
		"    port: 443\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 nodes counted for Clash YAML type: hysteria/hysteria2 (regression), got %d", count)
	}
}

func TestProviderPayload_XrayJSONWithHysteriaSurvivesEndToEnd(t *testing.T) {
	// Реальный формат провайдера: xray full-config array с protocol "hysteria".
	// Проверяем, что providerPayload не отбрасывает эту ноду и что итоговый
	// YAML (используется как Mihomo provider payload) содержит её.
	body := []byte(`[
		{
			"remarks": "Node HYSTERIA",
			"outbounds": [
				{
					"tag": "proxy",
					"protocol": "hysteria",
					"settings": {"address": "hy.example.com", "port": 9443, "version": 2},
					"streamSettings": {
						"network": "hysteria",
						"hysteriaSettings": {"version": 2, "auth": "secret-auth"},
						"security": "tls",
						"tlsSettings": {"serverName": "hy.example.com"}
					}
				},
				{"tag": "direct", "protocol": "freedom", "settings": {}}
			]
		}
	]`)

	payload, format := providerPayload(body)
	if format != "xray-json" {
		t.Fatalf("expected format=xray-json, got %q", format)
	}
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}

	count := countProviderNodes(string(payload))
	if count == 0 {
		t.Errorf("expected countProviderNodes > 0 for hysteria-only provider payload, got 0 (payload would be treated as empty and dropped by ProviderFetch)")
	}
}
