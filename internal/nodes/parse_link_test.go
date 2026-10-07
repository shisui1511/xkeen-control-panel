package nodes

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	testUUID1 = "11111111-1111-4111-8111-111111111111"
	testUUID2 = "22222222-2222-4222-8222-222222222222"
)

func mustParse(t *testing.T, name string) Node {
	t.Helper()
	n, err := ParseLink(readLink(t, name))
	if err != nil {
		t.Fatalf("ParseLink(%s): %v", name, err)
	}
	if n.Origin != OriginLink || n.Format != "link" || n.Text == "" {
		t.Fatalf("%s: метаданные узла: %+v", name, n)
	}
	return n
}

func TestLink_Schemes(t *testing.T) {
	cases := []struct {
		file  string
		check func(t *testing.T, n Node)
	}{
		{"vmess_ws_tls", func(t *testing.T, n Node) {
			if n.Protocol != "vmess" || n.Host != "node2.example" || n.Port != 443 || n.UUID != testUUID2 {
				t.Errorf("vmess: %+v", n)
			}
			if n.AlterID != 0 || n.Security != "auto" || n.Name != "VMess WS" {
				t.Errorf("vmess: aid/security/name: %+v", n)
			}
			if n.Transport.Network != "ws" || n.Transport.Path != "/ws" || n.Transport.Host != "cdn.example" {
				t.Errorf("vmess: транспорт %+v", n.Transport)
			}
			if n.TLS == nil || n.TLS.SNI != "cdn.example" || len(n.TLS.ALPN) != 2 || n.TLS.Fingerprint != "chrome" {
				t.Errorf("vmess: tls %+v", n.TLS)
			}
		}},
		{"vmess_grpc", func(t *testing.T, n Node) {
			if n.Port != 8443 || n.AlterID != 2 || n.Security != "aes-128-gcm" {
				t.Errorf("vmess grpc: %+v", n)
			}
			if n.Transport.Network != "grpc" || n.Transport.ServiceName != "svc.example" || n.Transport.Mode != "multi" {
				t.Errorf("vmess grpc: транспорт %+v", n.Transport)
			}
		}},
		{"vmess_h2", func(t *testing.T, n Node) {
			if n.Transport.Network != "h2" || n.Transport.Path != "/h2" || n.Transport.Host != "cdn.example" {
				t.Errorf("vmess h2: транспорт %+v", n.Transport)
			}
			if n.TLS == nil || n.TLS.SNI != "cdn.example" {
				t.Errorf("vmess h2: sni должен браться из host: %+v", n.TLS)
			}
		}},
		{"trojan_ws", func(t *testing.T, n Node) {
			if n.Protocol != "trojan" || n.Password != "trojan-pass" || n.Port != 443 {
				t.Errorf("trojan: %+v", n)
			}
			if n.Transport.Network != "ws" || n.Transport.Path != "/tr" || n.Transport.Host != "cdn.example" {
				t.Errorf("trojan: транспорт %+v", n.Transport)
			}
			if n.TLS == nil || !n.TLS.Insecure || n.TLS.SNI != "cdn.example" || n.TLS.Fingerprint != "firefox" ||
				!reflect.DeepEqual(n.TLS.ALPN, []string{"h2"}) {
				t.Errorf("trojan: tls %+v", n.TLS)
			}
		}},
		{"trojan_grpc", func(t *testing.T, n Node) {
			if n.Transport.Network != "grpc" || n.Transport.ServiceName != "trsvc" {
				t.Errorf("trojan grpc: %+v", n.Transport)
			}
			if n.TLS == nil || !n.TLS.Enabled || n.TLS.Insecure {
				t.Errorf("trojan grpc: tls %+v", n.TLS)
			}
		}},
		{"ss_sip002", func(t *testing.T, n Node) {
			if n.Protocol != "shadowsocks" || n.Method != "aes-256-gcm" || n.Password != "ss-pass-1" ||
				n.Host != "node4.example" || n.Port != 8388 || n.Name != "SS SIP002" {
				t.Errorf("ss sip002: %+v", n)
			}
		}},
		{"ss_plain", func(t *testing.T, n Node) {
			if n.Method != "chacha20-ietf-poly1305" || n.Password != "ss-pass-2" {
				t.Errorf("ss plain: %+v", n)
			}
		}},
		{"ss_plugin", func(t *testing.T, n Node) {
			if n.Plugin != "obfs-local" || n.PluginOpts["obfs"] != "http" || n.PluginOpts["obfs-host"] != "cdn.example" {
				t.Errorf("ss plugin: %q %v", n.Plugin, n.PluginOpts)
			}
			if n.Method != "aes-128-gcm" || n.Password != "ss-pass-3" {
				t.Errorf("ss plugin: %+v", n)
			}
		}},
		{"ss_legacy", func(t *testing.T, n Node) {
			if n.Method != "aes-256-gcm" || n.Password != "ss-pass-4" || n.Host != "node4.example" || n.Port != 8389 ||
				n.Name != "SS legacy" {
				t.Errorf("ss legacy: %+v", n)
			}
		}},
		{"hysteria2", func(t *testing.T, n Node) {
			if n.Protocol != "hysteria2" || n.Password != "hy2-pass" || n.Port != 443 {
				t.Errorf("hysteria2: %+v", n)
			}
			if n.Hy2 == nil || n.Hy2.Obfs != "salamander" || n.Hy2.ObfsPassword != "obfs-pass" || n.Hy2.Ports != "20000-30000" {
				t.Errorf("hysteria2: hy2 %+v", n.Hy2)
			}
			if n.TLS == nil || n.TLS.SNI != "cdn.example" || !n.TLS.Insecure ||
				!reflect.DeepEqual(n.TLS.PinSHA256, []string{"AA:BB:CC"}) || !reflect.DeepEqual(n.TLS.ALPN, []string{"h3"}) {
				t.Errorf("hysteria2: tls %+v", n.TLS)
			}
		}},
		{"hy2_alias", func(t *testing.T, n Node) {
			if n.Protocol != "hysteria2" || n.Password != "hy2-pass" {
				t.Errorf("hy2: %+v", n)
			}
		}},
		{"hysteria_v1", func(t *testing.T, n Node) {
			if n.Protocol != "hysteria" || n.Password != "hy1-pass" || n.Hy2 == nil ||
				n.Hy2.Up != "10" || n.Hy2.Down != "50" || n.Hy2.Obfs != "xplus" || n.Hy2.ObfsPassword != "obfs-v1" {
				t.Errorf("hysteria v1: %+v %+v", n, n.Hy2)
			}
			if n.TLS == nil || n.TLS.SNI != "cdn.example" || !n.TLS.Insecure {
				t.Errorf("hysteria v1: tls %+v", n.TLS)
			}
		}},
		{"tuic", func(t *testing.T, n Node) {
			if n.Protocol != "tuic" || n.UUID != testUUID1 || n.Password != "tuic-pass" || n.Port != 443 {
				t.Errorf("tuic: %+v", n)
			}
			if n.TUIC == nil || n.TUIC.CongestionControl != "bbr" || n.TUIC.UDPRelayMode != "native" {
				t.Errorf("tuic: %+v", n.TUIC)
			}
			if n.TLS == nil || n.TLS.SNI != "cdn.example" || !reflect.DeepEqual(n.TLS.ALPN, []string{"h3"}) {
				t.Errorf("tuic: tls %+v", n.TLS)
			}
		}},
		{"socks5", func(t *testing.T, n Node) {
			if n.Protocol != "socks" || n.UUID != "sock-user" || n.Password != "sock-pass" || n.Port != 1080 {
				t.Errorf("socks5: %+v", n)
			}
		}},
		{"socks", func(t *testing.T, n Node) {
			if n.Protocol != "socks" || n.UUID != "sock-user" || n.Password != "sock-pass" || n.Port != 1081 {
				t.Errorf("socks (base64): %+v", n)
			}
		}},
		{"http_proxy", func(t *testing.T, n Node) {
			if n.Protocol != "http" || n.UUID != "http-user" || n.Password != "http-pass" || n.Port != 8080 {
				t.Errorf("http: %+v", n)
			}
		}},
		{"wireguard", func(t *testing.T, n Node) {
			if n.Protocol != "wireguard" || n.WG == nil || n.Port != 51820 {
				t.Fatalf("wireguard: %+v", n)
			}
			if n.WG.PrivateKey == "" || n.WG.PeerPublicKey == "" || n.WG.PreSharedKey == "" {
				t.Errorf("wireguard: ключи %+v", n.WG)
			}
			if !reflect.DeepEqual(n.WG.Address, []string{"10.0.0.2/32"}) || n.WG.MTU != 1280 ||
				!reflect.DeepEqual(n.WG.Reserved, []int{1, 2, 3}) {
				t.Errorf("wireguard: address/mtu/reserved %+v", n.WG)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			c.check(t, mustParse(t, c.file))
		})
	}

	t.Run("wg_alias", func(t *testing.T) {
		line := strings.Replace(readLink(t, "wireguard"), "wireguard://", "wg://", 1)
		n, err := ParseLink(line)
		if err != nil || n.Protocol != "wireguard" {
			t.Fatalf("wg://: %v %+v", err, n)
		}
	})
}

func TestLink_VlessTransports(t *testing.T) {
	ws := mustParse(t, "vless_ws")
	if ws.Transport.Network != "ws" || ws.Transport.Path != "/vl" || ws.Transport.Host != "cdn.example" {
		t.Errorf("ws: %+v", ws.Transport)
	}
	grpc := mustParse(t, "vless_grpc")
	if grpc.Transport.Network != "grpc" || grpc.Transport.ServiceName != "vlsvc" || grpc.Transport.Mode != "multi" {
		t.Errorf("grpc: %+v", grpc.Transport)
	}
	hu := mustParse(t, "vless_httpupgrade")
	if hu.Transport.Network != "httpupgrade" || hu.Transport.Path != "/hu" || hu.Transport.Host != "cdn.example" {
		t.Errorf("httpupgrade: %+v", hu.Transport)
	}
	xh := mustParse(t, "vless_xhttp")
	if xh.Transport.Network != "xhttp" || xh.Transport.Path != "/xh" || xh.Transport.Mode != "packet-up" {
		t.Errorf("xhttp: %+v", xh.Transport)
	}
	if xh.Transport.XHTTP["xPaddingBytes"] != "100-1000" || xh.Transport.XHTTP["noGRPCHeader"] != true {
		t.Errorf("xhttp extra: %v", xh.Transport.XHTTP)
	}
	if _, ok := xh.Transport.XHTTP["xmux"].(map[string]any); !ok {
		t.Errorf("xhttp extra: xmux потерян: %v", xh.Transport.XHTTP)
	}
	kcp := mustParse(t, "vless_kcp")
	if kcp.Transport.Network != "kcp" || kcp.Transport.HeaderType != "wechat-video" || kcp.Transport.Path != "kcp-seed" {
		t.Errorf("kcp: %+v", kcp.Transport)
	}
	if kcp.TLS != nil || kcp.Reality != nil {
		t.Errorf("kcp: security=none не должен давать TLS: %+v", kcp.TLS)
	}
	tls := mustParse(t, "vless_tls_alpn")
	if tls.TLS == nil || !reflect.DeepEqual(tls.TLS.ALPN, []string{"h2", "http/1.1"}) || tls.TLS.Fingerprint != "hellochrome_131" {
		t.Errorf("tls alpn: %+v", tls.TLS)
	}
}

func TestLink_Errors(t *testing.T) {
	asCode := func(t *testing.T, err error) *SourceError {
		t.Helper()
		var se *SourceError
		if !errors.As(err, &se) {
			t.Fatalf("ошибка не *SourceError: %v", err)
		}
		return se
	}

	_, err := ParseLink("foo://x")
	if se := asCode(t, err); se.Code != CodeUnknownScheme || se.Params["scheme"] != "foo" {
		t.Errorf("foo://x: %+v", se)
	}

	secretLine := "vmess://не-base64-секрет"
	_, err = ParseLink(secretLine)
	se := asCode(t, err)
	if se.Code != CodeBadBase64 {
		t.Errorf("vmess не-base64: %+v", se)
	}
	if strings.Contains(se.Error(), "секрет") || strings.Contains(se.Detail, "секрет") || se.Fragment != "" {
		t.Errorf("текст ссылки попал в ошибку: %+v", se)
	}

	_, err = ParseLink("vless://" + testUUID1 + "@node1.example#name")
	if se := asCode(t, err); se.Code != CodeUnrecognized {
		t.Errorf("vless без порта: %+v", se)
	}

	_, err = ParseLink("vless://" + testUUID1 + "@node1.example:443?x=" + strings.Repeat("a", maxLinkBytes))
	if se := asCode(t, err); se.Code != CodeTooLarge {
		t.Errorf("длинная ссылка: %+v", se)
	}

	_, err = ParseLink("просто текст без схемы")
	if se := asCode(t, err); se.Code != CodeUnrecognized {
		t.Errorf("текст без схемы: %+v", se)
	}

	_, err = ParseLink("wireguard://AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=@node10.example:51820?address=10.0.0.2%2F32")
	if se := asCode(t, err); se.Code != CodeWGMissingKey {
		t.Errorf("wireguard без publickey: %+v", se)
	}
}
