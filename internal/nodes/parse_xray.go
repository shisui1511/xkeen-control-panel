package nodes

import (
	"net"
	"strconv"
	"strings"
)

// ParseXray разбирает Xray-JSON: одиночный outbound, объект с outbounds.
// Узел хранит оригинал в Raw (числа — json.Number): Xray получает его без
// изменений, Mihomo — результат эмиттера.
func ParseXray(data []byte) (ParseResult, error) {
	v, serr := decodeJSON(data)
	if serr != nil {
		return ParseResult{Format: "xray_json"}, serr
	}
	return parseXrayValue(v)
}

func parseXrayValue(v any) (ParseResult, error) {
	res := ParseResult{Format: "xray_json"}
	root := mapOf(v)
	if root == nil {
		return res, NewError(CodeUnrecognized)
	}
	if _, ok := root["outbounds"]; ok {
		list := listOf(root["outbounds"])
		if len(list) > maxNodes {
			return res, NewError(CodeTooLarge)
		}
		xrayCollect(&res, list)
		return res, nil
	}
	if _, ok := root["protocol"]; ok {
		xrayCollect(&res, []any{root})
		return res, nil
	}
	return res, NewError(CodeUnrecognized)
}

// xrayCollect добавляет в результат узлы и пропуски списка outbound.
func xrayCollect(res *ParseResult, list []any) {
	for i, e := range list {
		ob := mapOf(e)
		if ob == nil {
			res.Skipped = append(res.Skipped, Skipped{Code: CodeUnrecognized,
				Params: map[string]string{"index": strconv.Itoa(i + 1)}})
			continue
		}
		n, ignore, err := xrayOutboundNode(ob)
		if ignore {
			continue
		}
		if err != nil {
			res.Skipped = append(res.Skipped, skippedFrom(err, 0,
				pickStr(ob, "remarks", "name", "tag"), strOf(ob["protocol"])))
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
}

// xrayServiceProtocol — служебные outbound: не узлы, в Skipped не попадают.
func xrayServiceProtocol(p string) bool {
	switch p {
	case "freedom", "blackhole", "dns", "loopback":
		return true
	}
	return false
}

// xrayOutboundNode строит узел из outbound Xray. IR заполняется из Raw, в том
// числе из полей, запрещённых в Xray 26.x (h2, allowInsecure, httpSettings):
// такой узел остаётся виден с причиной, решение «не для Xray» принимают
// XrayOutbound и матрица возможностей, а не разборщик.
func xrayOutboundNode(ob map[string]any) (n Node, ignore bool, err *SourceError) {
	proto := strings.ToLower(strings.TrimSpace(strOf(ob["protocol"])))
	if xrayServiceProtocol(proto) {
		return Node{}, true, nil
	}
	if proto == "" {
		return Node{}, false, NewError(CodeUnrecognized)
	}
	settings := mapOf(ob["settings"])
	n = Node{
		Name:   pickStr(ob, "remarks", "name", "tag"),
		Origin: OriginXrayJSON,
		Raw:    ob,
		Format: "json",
	}
	var host string
	var port int
	switch proto {
	case "vless", "vmess":
		srv, user := xrayVnext(settings)
		host, port = strOf(srv["address"]), intOf(srv["port"])
		n.Protocol = proto
		n.UUID = strOf(user["id"])
		if proto == "vless" {
			n.Flow = strOf(user["flow"])
			n.Encryption = strOf(user["encryption"])
		} else {
			n.AlterID = intOf(user["alterId"])
			n.Security = strOf(user["security"])
		}
	case "trojan", "shadowsocks", "socks", "http":
		srv := xrayServer(settings)
		host, port = strOf(srv["address"]), intOf(srv["port"])
		n.Protocol = proto
		switch proto {
		case "trojan":
			n.Password = strOf(srv["password"])
		case "shadowsocks":
			n.Method = strOf(srv["method"])
			n.Password = strOf(srv["password"])
		default:
			user := srv
			if users := listOf(srv["users"]); len(users) > 0 {
				if u := mapOf(users[0]); u != nil {
					user = u
				}
			}
			n.UUID = pickStr(user, "user", "username")
			n.Password = pickStr(user, "pass", "password")
		}
	case "wireguard":
		n.Protocol = proto
		n.WG, host, port = xrayWireGuardIR(settings)
	case "hysteria":
		if intOf(settings["version"]) != 2 {
			return Node{}, false, NewError(CodeUnsupportedType, "type", "hysteria")
		}
		n.Protocol = "hysteria2"
		host, port = strOf(settings["address"]), intOf(settings["port"])
		if hs := mapOf(mapOf(ob["streamSettings"])["hysteriaSettings"]); hs != nil {
			n.Password = pickStr(hs, "auth", "password")
		}
	default:
		return Node{}, false, NewError(CodeUnsupportedType, "type", fragmentOf(proto))
	}
	if host == "" || port < 1 || port > 65535 {
		return Node{}, false, NewError(CodeUnrecognized, "type", fragmentOf(proto))
	}
	n.Host, n.Port = host, port
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	if proto != "wireguard" {
		if ss := mapOf(ob["streamSettings"]); ss != nil {
			xrayStreamIR(&n, ss)
		}
	}
	n.Text = marshalIndent(ob)
	return n, false, nil
}

// xrayVnext — сервер и пользователь vless/vmess: форма vnext или плоская.
func xrayVnext(settings map[string]any) (srv, user map[string]any) {
	srv = settings
	if vn := listOf(settings["vnext"]); len(vn) > 0 {
		if m := mapOf(vn[0]); m != nil {
			srv = m
		}
	}
	if srv == nil {
		srv = map[string]any{}
	}
	user = srv
	if us := listOf(srv["users"]); len(us) > 0 {
		if m := mapOf(us[0]); m != nil {
			user = m
		}
	}
	return srv, user
}

// xrayServer — сервер trojan/shadowsocks/socks/http: форма servers или плоская.
func xrayServer(settings map[string]any) map[string]any {
	if sv := listOf(settings["servers"]); len(sv) > 0 {
		if m := mapOf(sv[0]); m != nil {
			return m
		}
	}
	if settings == nil {
		return map[string]any{}
	}
	return settings
}

func xrayWireGuardIR(settings map[string]any) (wg *WireGuard, host string, port int) {
	wg = &WireGuard{
		PrivateKey: strOf(settings["secretKey"]),
		MTU:        intOf(settings["mtu"]),
		Address:    strListOf(settings["address"]),
	}
	for _, r := range listOf(settings["reserved"]) {
		wg.Reserved = append(wg.Reserved, intOf(r))
	}
	if peers := listOf(settings["peers"]); len(peers) > 0 {
		if p := mapOf(peers[0]); p != nil {
			wg.PeerPublicKey = strOf(p["publicKey"])
			wg.PreSharedKey = pickStr(p, "preSharedKey", "presharedKey")
			wg.AllowedIPs = strListOf(p["allowedIPs"])
			if h, ps, err := net.SplitHostPort(strOf(p["endpoint"])); err == nil {
				host, port = h, intOf(ps)
			}
		}
	}
	return wg, host, port
}

// stringHeaders приводит карту заголовков к map[string]string целиком, с Host.
func stringHeaders(v any) map[string]string {
	m := mapOf(v)
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, e := range m {
		out[k] = firstStr(e)
	}
	return out
}

// xrayStreamIR переносит streamSettings в IR: транспорт, TLS, Reality.
func xrayStreamIR(n *Node, ss map[string]any) {
	t := &n.Transport
	network := normNetwork(strOf(ss["network"]))
	if m := strings.ToLower(strings.TrimSpace(strOf(ss["method"]))); isRemovedTransport(m) {
		network = m
	}
	if _, ok := ss["httpSettings"]; ok && network == "tcp" {
		network = "http"
	}
	if _, ok := ss["quicSettings"]; ok && network == "tcp" {
		network = "quic"
	}
	t.Network = network
	switch network {
	case "tcp":
		if h := mapOf(mapOf(ss["tcpSettings"])["header"]); h != nil {
			t.HeaderType = strOf(h["type"])
			if t.HeaderType == "http" {
				if req := mapOf(h["request"]); req != nil {
					t.Path = firstStr(req["path"])
					t.Host, t.Headers = headersOf(req["headers"])
				}
			}
		}
	case "ws":
		if ws := mapOf(ss["wsSettings"]); ws != nil {
			t.Path = strOf(ws["path"])
			t.Host = strOf(ws["host"])
			host, rest := headersOf(ws["headers"])
			if t.Host == "" {
				t.Host = host
			}
			t.Headers = rest
		}
	case "grpc":
		if g := mapOf(ss["grpcSettings"]); g != nil {
			t.ServiceName = strOf(g["serviceName"])
			if b, _ := boolOf(g["multiMode"]); b {
				t.Mode = "multi"
			}
		}
	case "httpupgrade":
		if h := mapOf(ss["httpupgradeSettings"]); h != nil {
			t.Path = strOf(h["path"])
			t.Host = strOf(h["host"])
		}
	case "xhttp":
		xs := mapOf(ss["xhttpSettings"])
		if xs == nil {
			xs = mapOf(ss["splithttpSettings"])
		}
		if xs != nil {
			xrayXHTTPIR(t, xs)
		}
	case "kcp":
		if k := mapOf(ss["kcpSettings"]); k != nil {
			t.Path = strOf(k["seed"])
			t.HeaderType = strOf(mapOf(k["header"])["type"])
		}
	case "h2", "http":
		if h := mapOf(ss["httpSettings"]); h != nil {
			t.Path = strOf(h["path"])
			t.Host = firstStr(h["host"])
		}
	}

	security := strings.ToLower(strOf(ss["security"]))
	switch {
	case security == "reality":
		rs := mapOf(ss["realitySettings"])
		n.TLS = &TLS{
			Enabled:     true,
			SNI:         strOf(rs["serverName"]),
			Fingerprint: strOf(rs["fingerprint"]),
		}
		n.Reality = &Reality{
			PublicKey: strOf(rs["publicKey"]),
			ShortID:   strOf(rs["shortId"]),
			SpiderX:   strOf(rs["spiderX"]),
		}
	case mapOf(ss["tlsSettings"]) != nil:
		ts := mapOf(ss["tlsSettings"])
		n.TLS = &TLS{
			Enabled:     true,
			SNI:         strOf(ts["serverName"]),
			ALPN:        strListOf(ts["alpn"]),
			Fingerprint: strOf(ts["fingerprint"]),
			Insecure:    rawTruthy(ts["allowInsecure"]),
			PinSHA256:   strListOf(ts["pinnedPeerCertSha256"]),
		}
	case security == "tls" || security == "xtls":
		n.TLS = &TLS{Enabled: true}
	}
}

// xrayXHTTPIR переносит xhttpSettings: path, host, mode, headers (вместе с
// Host) — в поля транспорта, остальное, включая вложенный extra, — в XHTTP.
func xrayXHTTPIR(t *Transport, xs map[string]any) {
	set := func(k string, v any) {
		switch k {
		case "path":
			if t.Path == "" {
				t.Path = strOf(v)
			}
		case "host":
			if t.Host == "" {
				t.Host = strOf(v)
			}
		case "mode":
			if t.Mode == "" {
				t.Mode = strOf(v)
			}
		case "headers":
			for hk, hv := range stringHeaders(v) {
				if t.Headers == nil {
					t.Headers = map[string]string{}
				}
				if _, dup := t.Headers[hk]; !dup {
					t.Headers[hk] = hv
				}
			}
		default:
			if t.XHTTP == nil {
				t.XHTTP = map[string]any{}
			}
			if _, dup := t.XHTTP[k]; !dup {
				t.XHTTP[k] = deepCopy(v)
			}
		}
	}
	for k, v := range xs {
		if k != "extra" {
			set(k, v)
		}
	}
	if extra := mapOf(xs["extra"]); extra != nil {
		for k, v := range extra {
			set(k, v)
		}
	}
}
