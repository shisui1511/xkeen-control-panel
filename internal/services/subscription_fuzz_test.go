package services

import (
	"encoding/base64"
	"strings"
	"testing"
)

// FuzzParseShareLink ensures the main dispatch function never panics on arbitrary input.
func FuzzParseShareLink(f *testing.F) {
	f.Add("vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=tls&sni=example.com&type=tcp#tag")
	f.Add("vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=reality&pbk=abc&sid=01&fp=chrome&sni=example.com#tag")
	f.Add("vmess://eyJhZGQiOiIxMC4wLjAuMSIsInBvcnQiOiI0NDMiLCJpZCI6IjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMCIsInYiOiIyIiwicHMiOiJ0YWcifQ==")
	f.Add("trojan://password@host.example.com:443?sni=example.com#tag")
	f.Add("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNzd29yZA==@server.example.com:8388#tag")
	f.Add("hysteria2://password@host.example.com:443?sni=example.com#tag")
	f.Add("tuic://550e8400-e29b-41d4-a716-446655440000:password@host.example.com:443?congestion_control=bbr&sni=example.com#tag")
	f.Add("socks5://user:pass@socks.example.com:1080#tag")
	f.Add("socks://socks.example.com:1080")
	f.Add("http-proxy://user:pass@http.example.com:3128#tag")
	f.Add("garbage")
	f.Add("")
	f.Add("://")
	f.Add("vless://")
	f.Add("vmess://!!!notbase64!!!")

	// Task 1: Additional seed inputs for robust coverage
	f.Add(strings.Repeat("a", 8192))
	f.Add("vless://\x00\x00@host:443#tag")
	f.Add("vless://uuid@host:443#тег-🌍")
	f.Add("vless://vless://host:443")
	f.Add("vleshost:443")
	f.Add("vmess://====")
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte("{\"add\":\"1.1.1.1\",\"port\":\"443\",\"id\":\"uuid\",\"v\":\"2\",\"trash\":\""+strings.Repeat("x", 2000)+"\"}")))

	f.Fuzz(func(t *testing.T, link string) {
		// Must not panic on any input.
		parseShareLink(link)
	})
}

// FuzzParseVLESSLink fuzzes only the VLESS parser.
func FuzzParseVLESSLink(f *testing.F) {
	f.Add("vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=none#tag")
	f.Add("vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=reality&pbk=abc&sid=01&fp=chrome&sni=example.com&flow=xtls-rprx-vision#tag")
	f.Add("vless://550e8400-e29b-41d4-a716-446655440000@host.example.com:443?security=tls&type=ws&host=example.com&path=%2Fpath#tag")
	f.Add("vless://")
	f.Add("vless://invalid")

	// Task 1: Additional seed inputs
	f.Add("vless://uuid@host:99999#tag")
	f.Add("vless://uuid@host:0#tag")
	f.Add("vless://uuid@host:-1#tag")
	f.Add("vless://uuid@::1:443#tag")
	f.Add("vless://uuid@[::1]:443#tag")
	f.Add("vless://@host:443#tag")
	f.Add("vless://uuid@host:443?type=ws&path=" + strings.Repeat("/a", 500) + "#tag")
	f.Add("vless://uuid@host:443?security=tls&security=none#tag")
	f.Add("vless://uuid@host :443#tag")

	f.Fuzz(func(t *testing.T, link string) {
		parseVLESSLink(link)
	})
}

// FuzzParseVMessLink fuzzes the VMess parser (base64 JSON envelope).
func FuzzParseVMessLink(f *testing.F) {
	f.Add("vmess://eyJhZGQiOiIxMC4wLjAuMSIsInBvcnQiOiI0NDMiLCJpZCI6IjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMCIsInYiOiIyIiwicHMiOiJ0YWcifQ==")
	f.Add("vmess://e30=") // empty JSON object
	f.Add("vmess://")
	f.Add("vmess://notbase64!!!")
	f.Add("vmess://e30K") // trailing newline in base64

	// Task 1: Additional seed inputs
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2"}`)))
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"host","port":"abc","id":"uuid","v":"2"}`)))
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":{"nested":"obj"},"port":"443","id":"uuid","v":"2"}`)))
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`[1,2,3]`)))
	f.Add("vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"1.1.1.1","port":"443","id":"uuid","v":"2","ps":"`+strings.Repeat("a", 10000)+`"}`)))

	f.Fuzz(func(t *testing.T, link string) {
		parseVMessLink(link)
	})
}

// FuzzParseTrojanLink fuzzes the Trojan parser.
func FuzzParseTrojanLink(f *testing.F) {
	f.Add("trojan://password@host.example.com:443#tag")
	f.Add("trojan://password@host.example.com:443?sni=sni.example.com&type=tcp#tag")
	f.Add("trojan://")
	f.Add("trojan://password@invalidhost")

	// Task 1: Additional seed inputs
	f.Add("trojan://p%40ss%23word@host:443#tag")
	f.Add("trojan://@host:443#tag")
	f.Add("trojan://pass@host 443#tag")

	f.Fuzz(func(t *testing.T, link string) {
		parseTrojanLink(link)
	})
}

// FuzzParseSSLink fuzzes the Shadowsocks parser.
func FuzzParseSSLink(f *testing.F) {
	f.Add("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNzd29yZA==@server.example.com:8388#tag")
	f.Add("ss://") // missing userinfo
	f.Add("ss://notbase64!!!@server.example.com:8388#tag")

	// Task 1: Additional seed inputs
	f.Add("ss://method:password@host:8388#tag")
	f.Add("ss://" + base64.StdEncoding.EncodeToString([]byte("invalidmethod:password")) + "@host:8388#tag")
	f.Add("ss://@host:8388#tag")

	f.Fuzz(func(t *testing.T, link string) {
		parseSSLink(link)
	})
}

// FuzzParseHysteria2Link fuzzes the Hysteria2 parser.
func FuzzParseHysteria2Link(f *testing.F) {
	f.Add("hysteria2://password@host.example.com:443?sni=example.com&insecure=1#tag")
	f.Add("hy2://password@host.example.com:443#tag")
	f.Add("hysteria2://")
	f.Add("hysteria2://password@host:notaport#tag")

	// Task 1: Additional seed inputs
	f.Add("hysteria2://pass@host:443?obfs=salamander&obfs-password=secret#tag")
	f.Add("hysteria2://@host:443#tag")

	f.Fuzz(func(t *testing.T, link string) {
		parseHysteria2Link(link)
	})
}

// FuzzParseTUICLink fuzzes the TUIC parser.
func FuzzParseTUICLink(f *testing.F) {
	f.Add("tuic://550e8400-e29b-41d4-a716-446655440000:password@host.example.com:443?congestion_control=bbr&sni=example.com#tag")
	f.Add("tuic://")
	f.Add("tuic://nodots@host.example.com:443")

	// Task 1: Additional seed inputs
	f.Add("tuic://uuid@host:443#tag")
	f.Add("tuic://uuid:pass@host:443?congestion_control=invalid#tag")

	f.Fuzz(func(t *testing.T, link string) {
		parseTUICLink(link)
	})
}

// FuzzParseSOCKSLink fuzzes the SOCKS parser.
func FuzzParseSOCKSLink(f *testing.F) {
	f.Add("socks5://user:pass@socks.example.com:1080#tag")
	f.Add("socks://socks.example.com:1080")
	f.Add("socks5://")
	f.Add("socks://host:notaport")

	// Task 1: Additional seed inputs
	f.Add("socks5://us%40er:p%23ss@host:1080#tag")
	f.Add("socks5://user:pass@10.0.0.1")

	f.Fuzz(func(t *testing.T, link string) {
		parseSOCKSLink(link)
	})
}

// FuzzParseHTTPProxyLink fuzzes the HTTP proxy parser.
func FuzzParseHTTPProxyLink(f *testing.F) {
	f.Add("http-proxy://user:pass@http.example.com:3128#tag")
	f.Add("http-proxy://http.example.com:3128")
	f.Add("http-proxy://")
	f.Add("http-proxy://host:notaport")

	// Task 1: Additional seed inputs
	f.Add("http-proxy://us%40er:p%23ss@host:3128#tag")
	f.Add("http-proxy://user:pass@10.0.0.1")

	f.Fuzz(func(t *testing.T, link string) {
		parseHTTPProxyLink(link)
	})
}
