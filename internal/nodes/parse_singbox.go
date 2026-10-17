package nodes

import (
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
)

// ParseSingbox разбирает JSON sing-box: объект с outbounds или массив outbound.
// Узлы sing-box не родные ни одному ядру: Raw пуст, оба ядра получают результат
// эмиттера (D-19).
func ParseSingbox(data []byte) (ParseResult, error) {
	v, serr := decodeJSON(data)
	if serr != nil {
		return ParseResult{Format: "singbox"}, serr
	}
	return parseSingboxValue(v)
}

func parseSingboxValue(v any) (ParseResult, error) {
	res := ParseResult{Format: "singbox"}
	var list []any
	switch x := v.(type) {
	case map[string]any:
		if x["outbounds"] == nil {
			return res, NewError(CodeUnrecognized)
		}
		list = listOf(x["outbounds"])
		res.FullConfig = x["inbounds"] != nil || x["route"] != nil || x["dns"] != nil || x["experimental"] != nil
	case []any:
		list = x
	default:
		return res, NewError(CodeUnrecognized)
	}
	if len(list) > maxNodes {
		return res, NewError(CodeTooLarge)
	}
	for i, e := range list {
		ob := mapOf(e)
		if ob == nil {
			res.Skipped = append(res.Skipped, Skipped{Code: CodeUnrecognized,
				Params: map[string]string{"index": strconv.Itoa(i + 1)}})
			continue
		}
		n, ignore, err := singboxOutboundNode(ob)
		if ignore {
			continue
		}
		if err != nil {
			res.Skipped = append(res.Skipped, skippedFrom(err, 0, strOf(ob["tag"]), strOf(ob["type"])))
			res.SkippedSecrets = append(res.SkippedSecrets, credentialValues(ob)...)
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
	return res, nil
}

// singboxServiceType — outbound sing-box, которые не узлы.
func singboxServiceType(t string) bool {
	switch t {
	case "", "direct", "block", "dns", "selector", "urltest":
		return true
	}
	return false
}

func singboxOutboundNode(ob map[string]any) (n Node, ignore bool, err *SourceError) {
	typ := strings.ToLower(strings.TrimSpace(strOf(ob["type"])))
	if singboxServiceType(typ) {
		return Node{}, true, nil
	}
	host, port := strOf(ob["server"]), intOf(ob["server_port"])
	n = Node{Name: strOf(ob["tag"]), Origin: OriginSingbox, Format: "json"}
	alwaysTLS := false
	switch typ {
	case "vless":
		n.Protocol = "vless"
		n.UUID = strOf(ob["uuid"])
		n.Flow = strOf(ob["flow"])
		n.PacketEncoding = strOf(ob["packet_encoding"])
	case "vmess":
		n.Protocol = "vmess"
		n.UUID = strOf(ob["uuid"])
		n.Security = strOf(ob["security"])
		n.AlterID = intOf(ob["alter_id"])
		n.PacketEncoding = strOf(ob["packet_encoding"])
	case "trojan":
		n.Protocol = "trojan"
		n.Password = strOf(ob["password"])
	case "shadowsocks":
		n.Protocol = "shadowsocks"
		n.Method = strOf(ob["method"])
		n.Password = strOf(ob["password"])
		n.Plugin = strOf(ob["plugin"])
		if opts := strOf(ob["plugin_opts"]); opts != "" {
			n.PluginOpts = singboxPluginOpts(opts)
		}
	case "socks", "http":
		n.Protocol = typ
		n.UUID = strOf(ob["username"])
		n.Password = strOf(ob["password"])
	case "hysteria2":
		n.Protocol = "hysteria2"
		n.Password = strOf(ob["password"])
		h := &Hysteria2{}
		if o := mapOf(ob["obfs"]); o != nil {
			h.Obfs = strOf(o["type"])
			h.ObfsPassword = strOf(o["password"])
		}
		h.Up = singboxMbps(ob["up_mbps"])
		h.Down = singboxMbps(ob["down_mbps"])
		var ports []string
		for _, p := range strListOf(ob["server_ports"]) {
			ports = append(ports, strings.ReplaceAll(p, ":", "-"))
		}
		h.Ports = strings.Join(ports, ",")
		n.Hy2 = h
		alwaysTLS = true
	case "tuic":
		n.Protocol = "tuic"
		n.UUID = strOf(ob["uuid"])
		n.Password = strOf(ob["password"])
		n.TUIC = &TUIC{
			CongestionControl: strOf(ob["congestion_control"]),
			UDPRelayMode:      strOf(ob["udp_relay_mode"]),
			ReduceRTT:         truthyOf(ob["zero_rtt_handshake"]),
		}
		alwaysTLS = true
	case "wireguard":
		n.Protocol = "wireguard"
		var h string
		var p int
		n.WG, h, p = singboxWireGuardIR(ob)
		if host == "" {
			host, port = h, p
		}
	default:
		return Node{}, false, NewError(CodeUnsupportedType, "type", fragmentOf(typ))
	}
	if host == "" || port < 1 || port > 65535 {
		return Node{}, false, NewError(CodeUnrecognized, "type", fragmentOf(typ))
	}
	n.Host, n.Port = host, port
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	if typ != "wireguard" {
		singboxTLSIR(&n, ob, alwaysTLS)
		singboxTransportIR(&n, mapOf(ob["transport"]))
	}
	n.Text = marshalIndent(ob)
	return n, false, nil
}

// singboxMbps — скорость sing-box (число Мбит/с) в виде строки Mihomo.
func singboxMbps(v any) string {
	if s := strOf(v); s != "" && s != "0" {
		return s + " Mbps"
	}
	return ""
}

// singboxPluginOpts разбирает plugin_opts вида «k=v;k2=v2;flag».
func singboxPluginOpts(s string) map[string]any {
	out := map[string]any{}
	for _, part := range strings.Split(s, ";") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			out[k] = v
		} else {
			out[part] = true
		}
	}
	return out
}

// singboxTLSIR переносит блок tls: SNI, ALPN, отпечаток uTLS, Reality.
func singboxTLSIR(n *Node, ob map[string]any, alwaysTLS bool) {
	tls := mapOf(ob["tls"])
	enabled := truthyOf(tls["enabled"])
	reality := mapOf(tls["reality"])
	realityOn := reality != nil && truthyOf(reality["enabled"])
	if !enabled && !alwaysTLS && !realityOn {
		return
	}
	t := &TLS{
		Enabled:   true,
		SNI:       strOf(tls["server_name"]),
		ALPN:      strListOf(tls["alpn"]),
		Insecure:  truthyOf(tls["insecure"]),
		PinSHA256: singboxCertPins(strListOf(tls["certificate_sha256"])),
		// закрепление открытого ключа (SPKI) не выражается ни в Xray, ни в Mihomo:
		// хранится отдельно, чтобы не сойти за отпечаток сертификата
		PinPubKeySHA256: strListOf(tls["certificate_public_key_sha256"]),
	}
	if u := mapOf(tls["utls"]); u != nil && (u["enabled"] == nil || truthyOf(u["enabled"])) {
		t.Fingerprint = strOf(u["fingerprint"])
	}
	n.TLS = t
	if realityOn {
		n.Reality = &Reality{PublicKey: strOf(reality["public_key"]), ShortID: strOf(reality["short_id"])}
	}
}

// singboxCertPins переводит certificate_sha256 sing-box (SHA-256 всего
// сертификата в base64) в hex, как его принимают оба ядра. Значение, которое не
// декодируется в 32 байта, остаётся как есть: закрепление задано, но не
// выражается, и правило потери закрепления не даст потерять его молча.
func singboxCertPins(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, singboxPinHex(p))
	}
	return out
}

func singboxPinHex(p string) string {
	p = strings.TrimSpace(p)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(p); err == nil && len(b) == 32 {
			return hex.EncodeToString(b)
		}
	}
	return p
}

// singboxTransportIR переносит блок transport. Тип xhttp — защитная ветка:
// в sing-box он бывает только в форках (A7).
func singboxTransportIR(n *Node, tr map[string]any) {
	t := &n.Transport
	typ := strings.ToLower(strOf(tr["type"]))
	switch typ {
	case "", "tcp":
		t.Network = "tcp"
	case "ws":
		t.Network = "ws"
		t.Path = strOf(tr["path"])
		host, rest := headersOf(tr["headers"])
		t.Host, t.Headers = host, rest
	case "grpc":
		t.Network = "grpc"
		t.ServiceName = strOf(tr["service_name"])
	case "httpupgrade":
		t.Network = "httpupgrade"
		t.Path = strOf(tr["path"])
		t.Host = firstStr(tr["host"])
		if host, rest := headersOf(tr["headers"]); t.Host == "" {
			t.Host, t.Headers = host, rest
		} else {
			t.Headers = rest
		}
	case "http":
		// HTTP-транспорт sing-box с TLS — HTTP/2.
		t.Network = "http"
		if n.TLS != nil {
			t.Network = "h2"
		}
		t.Path = strOf(tr["path"])
		t.Host = firstStr(tr["host"])
	case "xhttp", "splithttp":
		t.Network = "xhttp"
		for k, v := range tr {
			switch k {
			case "type":
			case "path":
				t.Path = strOf(v)
			case "host":
				t.Host = firstStr(v)
			case "mode":
				t.Mode = strOf(v)
			case "headers":
				t.Headers = stringHeaders(v)
			default:
				if t.XHTTP == nil {
					t.XHTTP = map[string]any{}
				}
				t.XHTTP[k] = deepCopy(v)
			}
		}
	default:
		t.Network = typ
	}
}

// awgKeys — ключи AmneziaWG на верхнем уровне outbound sing-box: имя в JSON и
// имя в карте опций Mihomo.
var awgKeys = map[string]string{
	"jc": "jc", "jmin": "jmin", "jmax": "jmax",
	"s1": "s1", "s2": "s2", "s3": "s3", "s4": "s4",
	"h1": "h1", "h2": "h2", "h3": "h3", "h4": "h4",
	"i1": "i1", "i2": "i2", "i3": "i3", "i4": "i4", "i5": "i5",
	"j1": "j1", "j2": "j2", "j3": "j3", "itime": "itime", "version": "version",
	"header_protection_key": "header-protection-key", "content_padding_addition": "content-padding-addition",
	"random_trailers": "random-trailers", "disable_cookies": "disable-cookies",
	"rekey_after_time": "rekey-after-time", "rekey_timeout": "rekey-timeout",
	"reject_after_time": "reject-after-time", "keepalive_timeout": "keepalive-timeout",
	"max_handshake_attempts": "max-handshake-attempts",
}

func singboxWireGuardIR(ob map[string]any) (wg *WireGuard, host string, port int) {
	src := ob
	if peers := listOf(ob["peers"]); len(peers) > 0 {
		if p := mapOf(peers[0]); p != nil {
			src = p
			host, port = strOf(p["address"]), intOf(p["port"])
			if host == "" {
				host, port = strOf(p["server"]), intOf(p["server_port"])
			}
		}
	}
	wg = &WireGuard{
		PrivateKey:    strOf(ob["private_key"]),
		PeerPublicKey: pickStr(src, "peer_public_key", "public_key"),
		PreSharedKey:  pickStr(src, "pre_shared_key"),
		MTU:           intOf(ob["mtu"]),
		Address:       strListOf(pickAny(ob, "local_address", "address")),
		AllowedIPs:    strListOf(src["allowed_ips"]),
	}
	switch r := src["reserved"].(type) {
	case []any:
		for _, e := range r {
			wg.Reserved = append(wg.Reserved, intOf(e))
		}
	case string:
		if data, err := base64.StdEncoding.DecodeString(r); err == nil {
			for _, b := range data {
				wg.Reserved = append(wg.Reserved, int(b))
			}
		}
	}
	awg := map[string]any{}
	collect := func(m map[string]any) {
		for k, v := range m {
			if name, ok := awgKeys[strings.ToLower(k)]; ok {
				awg[name] = v
			}
		}
	}
	collect(ob)
	collect(mapOf(ob["amnezia_wg"]))
	collect(mapOf(ob["awg"]))
	if len(awg) > 0 {
		wg.AWG = awg
		wg.AWGVersion = awgVersionOf(awg)
	}
	return wg, host, port
}

// pickAny — первое непустое значение из перечисленных ключей карты.
func pickAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
