package nodes

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlStringKeys — ключи Clash, значения которых всегда строки. Скаляр без
// кавычек, похожий на число (short-id: 12345678, пароль 0123), читается как
// текст в том виде, в каком записан: yaml.v3 превратил бы его в int, а «0123» —
// в восьмеричное число (RESEARCH, Pitfall 11).
var yamlStringKeys = map[string]bool{
	"name": true, "server": true, "uuid": true, "password": true, "username": true,
	"short-id": true, "public-key": true, "private-key": true, "pre-shared-key": true,
	"servername": true, "sni": true, "flow": true, "cipher": true, "encryption": true,
	"packet-encoding": true, "path": true, "host": true, "network": true, "type": true,
	"client-fingerprint": true, "fingerprint": true, "grpc-service-name": true,
	"obfs": true, "obfs-password": true, "plugin": true, "up": true, "down": true,
	"ports": true, "ip": true, "ipv6": true, "auth": true, "auth-str": true, "token": true,
	"mode": true, "method": true, "service-name": true,
}

// maxYAMLDepth — предел вложенности при разворачивании дерева YAML.
const maxYAMLDepth = 64

var yamlLineRE = regexp.MustCompile(`line (\d+)`)

// yamlSourceError переводит ошибку yaml.v3 в yaml_error с номером строки.
func yamlSourceError(err error) *SourceError {
	if m := yamlLineRE.FindStringSubmatch(err.Error()); m != nil {
		e := NewError(CodeYAMLError, "line", m[1])
		e.Line, _ = strconv.Atoi(m[1])
		return e
	}
	return NewError(CodeYAMLError)
}

// yamlToAny разворачивает узел YAML в карты, списки и скаляры: якоря и
// merge-ключи раскрываются, вложенность любой глубины сохраняется.
func yamlToAny(n *yaml.Node, key string, depth int) (any, error) {
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("слишком глубокая вложенность")
	}
	switch n.Kind {
	case yaml.AliasNode:
		return yamlToAny(n.Alias, key, depth+1)
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return yamlToAny(n.Content[0], key, depth+1)
	case yaml.ScalarNode:
		return yamlScalarToAny(n, key)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlToAny(c, key, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		var merges []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			for k.Kind == yaml.AliasNode {
				k = k.Alias
			}
			if k.Kind != yaml.ScalarNode {
				continue
			}
			if k.ShortTag() == "!!merge" {
				merges = append(merges, v)
				continue
			}
			val, err := yamlToAny(v, k.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m[k.Value] = val
		}
		// Merge-ключи: явные ключи сильнее, ранний источник сильнее позднего.
		for _, mv := range merges {
			for mv.Kind == yaml.AliasNode {
				mv = mv.Alias
			}
			srcs := []*yaml.Node{mv}
			if mv.Kind == yaml.SequenceNode {
				srcs = mv.Content
			}
			for _, s := range srcs {
				sub, err := yamlToAny(s, key, depth+1)
				if err != nil {
					return nil, err
				}
				sm, ok := sub.(map[string]any)
				if !ok {
					continue
				}
				for k, v := range sm {
					if _, exists := m[k]; !exists {
						m[k] = v
					}
				}
			}
		}
		return m, nil
	}
	return nil, nil
}

func yamlScalarToAny(n *yaml.Node, key string) (any, error) {
	tag := n.ShortTag()
	if tag == "!!null" {
		if yamlStringKeys[key] {
			return "", nil
		}
		return nil, nil
	}
	switch tag {
	case "!!int", "!!float":
		if yamlStringKeys[key] {
			return n.Value, nil
		}
	case "!!str", "!!timestamp", "!!binary":
		return n.Value, nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// clashItems находит список прокси: ключ proxies верхнего уровня либо сам
// документ-список.
func clashItems(doc *yaml.Node) ([]*yaml.Node, *SourceError) {
	root := doc
	for root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	for root.Kind == yaml.AliasNode {
		root = root.Alias
	}
	switch root.Kind {
	case yaml.SequenceNode:
		return root.Content, nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value != "proxies" {
				continue
			}
			v := root.Content[i+1]
			for v.Kind == yaml.AliasNode {
				v = v.Alias
			}
			if v.Kind == yaml.SequenceNode {
				return v.Content, nil
			}
			return nil, nil
		}
	}
	return nil, NewError(CodeUnrecognized)
}

// ParseClash разбирает Clash YAML (документ с ключом proxies или список
// прокси). Узел хранит оригинал в Raw: Mihomo получает его без изменений.
func ParseClash(data []byte) (ParseResult, error) {
	res := ParseResult{Format: "clash"}
	if len(bytes.TrimSpace(data)) == 0 {
		return res, NewError(CodeEmptyResponse)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return res, yamlSourceError(err)
	}
	// Полное декодирование отсекает алиас-бомбы и повторные ключи (T-145-04):
	// развёртка алиасов ниже безопасна только после этой проверки.
	var probe any
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return res, yamlSourceError(err)
	}
	items, serr := clashItems(&doc)
	if serr != nil {
		return res, serr
	}
	if len(items) > maxNodes {
		return res, NewError(CodeTooLarge)
	}
	for i, it := range items {
		for it.Kind == yaml.AliasNode {
			it = it.Alias
		}
		if it.Kind != yaml.MappingNode {
			res.Skipped = append(res.Skipped, Skipped{Line: it.Line, Code: CodeUnrecognized,
				Params: map[string]string{"index": strconv.Itoa(i + 1)}})
			continue
		}
		v, err := yamlToAny(it, "", 0)
		if err != nil {
			return res, NewError(CodeYAMLError, "line", strconv.Itoa(it.Line))
		}
		raw, _ := v.(map[string]any)
		n, ignore, nerr := clashProxyNode(raw)
		if ignore {
			continue
		}
		if nerr != nil {
			res.Skipped = append(res.Skipped, skippedFrom(nerr, it.Line, strOf(raw["name"]), strOf(raw["type"])))
			continue
		}
		res.Nodes = append(res.Nodes, n)
	}
	return res, nil
}

// clashProxyText — YAML одного прокси (ключи в порядке name, type, server, port,
// затем остальные по алфавиту): исходный текст узла для правки (D-13).
func clashProxyText(raw map[string]any) string {
	node, err := yamlMap(raw, clashHead)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return ""
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n")
}

// clashPassThrough — типы «прокси», которые не узлы (встроенные действия).
func clashPassThrough(typ string) bool {
	switch typ {
	case "direct", "reject", "reject-drop", "dns", "pass", "compatible":
		return true
	}
	return false
}

// clashProxyNode переводит прокси Clash в узел. ignore == true — запись не
// является узлом (встроенное действие), в Skipped её не заносят.
func clashProxyNode(raw map[string]any) (n Node, ignore bool, err *SourceError) {
	typ := strings.ToLower(strings.TrimSpace(strOf(raw["type"])))
	if typ == "" {
		return Node{}, false, NewError(CodeUnrecognized)
	}
	if clashPassThrough(typ) {
		return Node{}, true, nil
	}
	host, port := strOf(raw["server"]), intOf(raw["port"])
	if peers := listOf(raw["peers"]); typ == "wireguard" && (host == "" || port == 0) && len(peers) > 0 {
		if p := mapOf(peers[0]); p != nil {
			host, port = strOf(p["server"]), intOf(p["port"])
		}
	}
	if host == "" || port < 1 || port > 65535 {
		return Node{}, false, NewError(CodeUnrecognized, "type", fragmentOf(typ))
	}
	n = Node{
		Name:   strOf(raw["name"]),
		Host:   host,
		Port:   port,
		Origin: OriginClash,
		Raw:    raw,
		Format: "yaml",
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	udp := func() {
		if p := boolPtrOf(raw["udp"]); p != nil {
			n.UDP = p
			return
		}
		// Mihomo без udp не ретранслирует UDP: отсутствие ключа — false.
		f := false
		n.UDP = &f
	}
	switch typ {
	case "vless":
		n.Protocol = "vless"
		n.UUID = strOf(raw["uuid"])
		n.Flow = strOf(raw["flow"])
		n.Encryption = strOf(raw["encryption"])
		n.PacketEncoding = strOf(raw["packet-encoding"])
		udp()
		clashStreamIR(&n, raw, false)
	case "vmess":
		n.Protocol = "vmess"
		n.UUID = strOf(raw["uuid"])
		n.AlterID = intOf(raw["alterId"])
		if n.AlterID == 0 {
			n.AlterID = intOf(raw["alter-id"])
		}
		n.Security = strOf(raw["cipher"])
		n.PacketEncoding = strOf(raw["packet-encoding"])
		udp()
		clashStreamIR(&n, raw, false)
	case "trojan":
		n.Protocol = "trojan"
		n.Password = strOf(raw["password"])
		udp()
		clashStreamIR(&n, raw, true)
	case "ss":
		n.Protocol = "shadowsocks"
		n.Method = strOf(raw["cipher"])
		n.Password = strOf(raw["password"])
		n.Plugin = strOf(raw["plugin"])
		if opts := mapOf(raw["plugin-opts"]); len(opts) > 0 {
			n.PluginOpts = deepCopyMap(opts)
		}
		udp()
	case "socks5", "http":
		n.Protocol = normProtocol(typ)
		n.UUID = strOf(raw["username"])
		n.Password = strOf(raw["password"])
		if typ == "socks5" {
			udp()
		}
		if on, _ := boolOf(raw["tls"]); on {
			n.TLS = &TLS{
				Enabled:  true,
				SNI:      pickStr(raw, "sni", "servername"),
				Insecure: truthyOf(raw["skip-cert-verify"]),
			}
		}
	case "hysteria2":
		n.Protocol = "hysteria2"
		n.Password = pickStr(raw, "password", "auth")
		n.Hy2 = &Hysteria2{
			Obfs:         strOf(raw["obfs"]),
			ObfsPassword: strOf(raw["obfs-password"]),
			Up:           strOf(raw["up"]),
			Down:         strOf(raw["down"]),
			Ports:        strOf(raw["ports"]),
		}
		n.TLS = clashPlainTLS2IR(raw)
	case "tuic":
		n.Protocol = "tuic"
		n.UUID = strOf(raw["uuid"])
		n.Password = strOf(raw["password"])
		n.TUIC = &TUIC{
			CongestionControl: strOf(raw["congestion-controller"]),
			UDPRelayMode:      strOf(raw["udp-relay-mode"]),
			ReduceRTT:         truthyOf(raw["reduce-rtt"]),
		}
		n.TLS = clashPlainTLS2IR(raw)
	case "wireguard":
		n.Protocol = "wireguard"
		n.WG = clashWireGuardIR(raw)
		udp()
	default:
		// Тип, которого модель не знает (anytls, ssh, mieru, snell, hysteria…),
		// проходит насквозь: протокол — сам type, адрес и порт из Raw.
		n.Protocol = typ
	}
	n.Text = clashProxyText(raw)
	return n, false, nil
}

func truthyOf(v any) bool {
	b, _ := boolOf(v)
	return b
}

// clashPlainTLS2IR — TLS протоколов, у которых он всегда включён (hysteria2, tuic).
func clashPlainTLS2IR(raw map[string]any) *TLS {
	t := &TLS{
		Enabled:  true,
		SNI:      pickStr(raw, "sni", "servername"),
		ALPN:     strListOf(raw["alpn"]),
		Insecure: truthyOf(raw["skip-cert-verify"]),
	}
	if fp := strOf(raw["fingerprint"]); fp != "" {
		t.PinSHA256 = []string{fp}
	}
	return t
}

func clashWireGuardIR(raw map[string]any) *WireGuard {
	src := raw
	if peers := listOf(raw["peers"]); len(peers) > 0 {
		if p := mapOf(peers[0]); p != nil {
			src = p
		}
	}
	wg := &WireGuard{
		PrivateKey:    strOf(raw["private-key"]),
		PeerPublicKey: pickStr(src, "public-key"),
		PreSharedKey:  pickStr(src, "pre-shared-key"),
		MTU:           intOf(raw["mtu"]),
		AllowedIPs:    strListOf(src["allowed-ips"]),
	}
	if wg.PeerPublicKey == "" {
		wg.PeerPublicKey = strOf(raw["public-key"])
	}
	for _, k := range []string{"ip", "ipv6"} {
		if a := strOf(raw[k]); a != "" {
			wg.Address = append(wg.Address, a)
		}
	}
	for _, v := range listOf(src["reserved"]) {
		wg.Reserved = append(wg.Reserved, intOf(v))
	}
	if opt := mapOf(raw["amnezia-wg-option"]); len(opt) > 0 {
		wg.AWG = deepCopyMap(opt)
		wg.AWGVersion = awgVersionOf(wg.AWG)
	}
	return wg
}

// awgVersionOf определяет версию AmneziaWG по ключам карты опций (та же
// логика, что и у диалектов старого слоя: явная версия, затем признаки).
func awgVersionOf(opt map[string]any) string {
	if v := strOf(opt["version"]); v != "" {
		return v
	}
	has := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := opt[k]; ok {
				return true
			}
		}
		return false
	}
	switch {
	case has("header-protection-key", "i1", "i2", "i3", "i4", "i5", "content-padding-addition",
		"random-trailers", "disable-cookies", "rekey-after-time", "rekey-timeout",
		"reject-after-time", "keepalive-timeout", "max-handshake-attempts"):
		return "3.1"
	case has("s3", "s4"):
		return "2.0"
	case has("j1", "j2", "j3", "itime"):
		return "1.5"
	case has("jc", "jmin", "jmax", "s1", "s2", "h1", "h2", "h3", "h4"):
		return "1.0"
	}
	return ""
}

// clashStreamIR переносит транспорт, TLS и Reality прокси vless/vmess/trojan в узел.
func clashStreamIR(n *Node, raw map[string]any, trojan bool) {
	t := &n.Transport
	network := strings.ToLower(strOf(raw["network"]))
	switch network {
	case "", "tcp":
		t.Network = "tcp"
	case "http":
		// network: http у Clash — TCP с заголовком HTTP (http-opts).
		t.Network = "tcp"
		t.HeaderType = "http"
		if o := mapOf(raw["http-opts"]); o != nil {
			t.Path = firstStr(o["path"])
			host, rest := headersOf(o["headers"])
			t.Host, t.Headers = host, rest
		}
	case "ws":
		t.Network = "ws"
		if o := mapOf(raw["ws-opts"]); o != nil {
			t.Path = strOf(o["path"])
			host, rest := headersOf(o["headers"])
			t.Host, t.Headers = host, rest
			if truthyOf(o["v2ray-http-upgrade"]) {
				t.Network = "httpupgrade"
			}
		}
	case "grpc":
		t.Network = "grpc"
		if o := mapOf(raw["grpc-opts"]); o != nil {
			t.ServiceName = strOf(o["grpc-service-name"])
		}
	case "h2":
		t.Network = "h2"
		if o := mapOf(raw["h2-opts"]); o != nil {
			t.Path = strOf(o["path"])
			t.Host = firstStr(o["host"])
		}
	case "xhttp":
		t.Network = "xhttp"
		if o := mapOf(raw["xhttp-opts"]); o != nil {
			clashXHTTPIR(t, o)
		}
	default:
		t.Network = network
	}

	reality := mapOf(raw["reality-opts"])
	tlsOn, tlsSet := boolOf(raw["tls"])
	if trojan && !tlsSet {
		tlsOn = true
	}
	if !tlsOn && reality == nil {
		return
	}
	tls := &TLS{
		Enabled:     true,
		SNI:         pickStr(raw, "servername", "sni"),
		ALPN:        strListOf(raw["alpn"]),
		Fingerprint: strOf(raw["client-fingerprint"]),
		Insecure:    truthyOf(raw["skip-cert-verify"]),
	}
	if fp := strOf(raw["fingerprint"]); fp != "" {
		tls.PinSHA256 = []string{fp}
	}
	n.TLS = tls
	if reality != nil {
		n.Reality = &Reality{PublicKey: strOf(reality["public-key"]), ShortID: strOf(reality["short-id"])}
	}
}

// clashXHTTPIR переносит xhttp-opts в транспорт: известные поля — в Path, Host,
// Mode, Headers; остальные (в том числе вложенные reuse-settings) — в XHTTP.
func clashXHTTPIR(t *Transport, opts map[string]any) {
	for k, v := range opts {
		switch k {
		case "path":
			t.Path = strOf(v)
		case "host":
			t.Host = strOf(v)
		case "mode":
			t.Mode = strOf(v)
		case "headers":
			host, rest := headersOf(v)
			if t.Host == "" {
				t.Host = host
			}
			t.Headers = rest
		default:
			if t.XHTTP == nil {
				t.XHTTP = map[string]any{}
			}
			t.XHTTP[k] = deepCopy(v)
		}
	}
}
