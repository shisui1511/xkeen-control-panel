package services

import "testing"

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

	f.Fuzz(func(t *testing.T, link string) {
		parseTrojanLink(link)
	})
}

// FuzzParseSSLink fuzzes the Shadowsocks parser.
func FuzzParseSSLink(f *testing.F) {
	f.Add("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNzd29yZA==@server.example.com:8388#tag")
	f.Add("ss://") // missing userinfo
	f.Add("ss://notbase64!!!@server.example.com:8388#tag")

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

	f.Fuzz(func(t *testing.T, link string) {
		parseHysteria2Link(link)
	})
}

// FuzzParseTUICLink fuzzes the TUIC parser.
func FuzzParseTUICLink(f *testing.F) {
	f.Add("tuic://550e8400-e29b-41d4-a716-446655440000:password@host.example.com:443?congestion_control=bbr&sni=example.com#tag")
	f.Add("tuic://")
	f.Add("tuic://nodots@host.example.com:443")

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

	f.Fuzz(func(t *testing.T, link string) {
		parseHTTPProxyLink(link)
	})
}
