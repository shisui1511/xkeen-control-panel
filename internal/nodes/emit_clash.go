package nodes

import (
	"bytes"
	"encoding/json"
	"net"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// EmitClash строит прокси Clash из узла (без имени). Всё, что Mihomo не
// выражает, возвращается как *SourceError с кодом причины.
func EmitClash(n Node) (map[string]any, error) {
	proto := normProtocol(n.Protocol)
	m := map[string]any{"server": n.Host, "port": n.Port}
	switch proto {
	case "vless":
		m["type"] = "vless"
		m["uuid"] = n.UUID
		if n.Encryption != "" {
			m["encryption"] = n.Encryption
		}
		if n.Flow != "" {
			m["flow"] = n.Flow
		}
		if n.PacketEncoding != "" {
			m["packet-encoding"] = n.PacketEncoding
		}
		m["udp"] = udpValue(n)
		return clashStream(n, m, "servername")
	case "vmess":
		m["type"] = "vmess"
		m["uuid"] = n.UUID
		m["alterId"] = n.AlterID
		m["cipher"] = orDefault(n.Security, "auto")
		if n.PacketEncoding != "" {
			m["packet-encoding"] = n.PacketEncoding
		}
		m["udp"] = udpValue(n)
		return clashStream(n, m, "servername")
	case "trojan":
		m["type"] = "trojan"
		m["password"] = n.Password
		m["udp"] = udpValue(n)
		return clashStream(n, m, "sni")
	case "shadowsocks":
		m["type"] = "ss"
		m["cipher"] = n.Method
		m["password"] = n.Password
		m["udp"] = udpValue(n)
		if n.Plugin != "" {
			plugin, opts := clashPlugin(n.Plugin, n.PluginOpts)
			m["plugin"] = plugin
			if len(opts) > 0 {
				m["plugin-opts"] = opts
			}
		}
	case "socks":
		m["type"] = "socks5"
		if n.UUID != "" {
			m["username"] = n.UUID
		}
		if n.Password != "" {
			m["password"] = n.Password
		}
		m["udp"] = udpValue(n)
	case "http":
		m["type"] = "http"
		if n.UUID != "" {
			m["username"] = n.UUID
		}
		if n.Password != "" {
			m["password"] = n.Password
		}
		if n.TLS != nil && n.TLS.Enabled {
			m["tls"] = true
			if n.TLS.SNI != "" {
				m["sni"] = n.TLS.SNI
			}
			if n.TLS.Insecure {
				m["skip-cert-verify"] = true
			}
		}
	case "hysteria2":
		m["type"] = "hysteria2"
		m["password"] = n.Password
		if h := n.Hy2; h != nil {
			if h.Obfs != "" {
				m["obfs"] = h.Obfs
			}
			if h.ObfsPassword != "" {
				m["obfs-password"] = h.ObfsPassword
			}
			if h.Up != "" {
				m["up"] = h.Up
			}
			if h.Down != "" {
				m["down"] = h.Down
			}
			if h.Ports != "" {
				m["ports"] = h.Ports
			}
		}
		clashPlainTLS(n, m)
	case "tuic":
		m["type"] = "tuic"
		m["uuid"] = n.UUID
		m["password"] = n.Password
		if t := n.TUIC; t != nil {
			if t.CongestionControl != "" {
				m["congestion-controller"] = t.CongestionControl
			}
			if t.UDPRelayMode != "" {
				m["udp-relay-mode"] = t.UDPRelayMode
			}
			if t.ReduceRTT {
				m["reduce-rtt"] = true
			}
		}
		clashPlainTLS(n, m)
	case "wireguard":
		if n.WG == nil {
			return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
		}
		clashWireGuard(n, m)
	default:
		return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
	}
	return m, nil
}

// clashPlainTLS пишет TLS-поля протоколов, у которых TLS всегда включён
// (hysteria2, tuic): sni, alpn, skip-cert-verify, закрепление сертификата.
func clashPlainTLS(n Node, m map[string]any) {
	t := n.TLS
	if t == nil {
		return
	}
	if t.SNI != "" {
		m["sni"] = t.SNI
	}
	if len(t.ALPN) > 0 {
		m["alpn"] = stringsToAny(t.ALPN)
	}
	if t.Insecure {
		m["skip-cert-verify"] = true
	}
	if pins := normPins(t.PinSHA256); len(pins) > 0 {
		m["fingerprint"] = pins[0]
	}
}

func clashWireGuard(n Node, m map[string]any) {
	wg := n.WG
	m["type"] = "wireguard"
	m["private-key"] = wg.PrivateKey
	m["public-key"] = wg.PeerPublicKey
	if wg.PreSharedKey != "" {
		m["pre-shared-key"] = wg.PreSharedKey
	}
	var v4, v6 string
	for _, a := range wg.Address {
		a = strings.TrimSpace(a)
		ip := a
		if i := strings.Index(ip, "/"); i >= 0 {
			ip = ip[:i]
		}
		parsed := net.ParseIP(ip)
		switch {
		case parsed == nil:
			if v4 == "" {
				v4 = a
			}
		case parsed.To4() != nil:
			if v4 == "" {
				v4 = a
			}
		default:
			if v6 == "" {
				v6 = a
			}
		}
	}
	if v4 != "" {
		m["ip"] = v4
	}
	if v6 != "" {
		m["ipv6"] = v6
	}
	if len(wg.AllowedIPs) > 0 {
		m["allowed-ips"] = stringsToAny(wg.AllowedIPs)
	}
	if wg.MTU > 0 {
		m["mtu"] = wg.MTU
	}
	if len(wg.Reserved) > 0 {
		r := make([]any, len(wg.Reserved))
		for i, v := range wg.Reserved {
			r[i] = v
		}
		m["reserved"] = r
	}
	if len(wg.AWG) > 0 {
		m["amnezia-wg-option"] = clashAWGOptions(wg.AWG)
	}
	m["udp"] = udpValue(n)
}

// clashPlugin переводит плагин SIP003 из ссылки в имя и параметры Mihomo.
func clashPlugin(plugin string, in map[string]any) (string, map[string]any) {
	opts := map[string]any{}
	get := func(keys ...string) (any, bool) {
		for _, k := range keys {
			if v, ok := in[k]; ok {
				return v, true
			}
		}
		return nil, false
	}
	switch strings.ToLower(plugin) {
	case "obfs-local", "simple-obfs", "obfs":
		if v, ok := get("obfs", "mode"); ok {
			opts["mode"] = v
		}
		if v, ok := get("obfs-host", "host"); ok {
			opts["host"] = v
		}
		return "obfs", opts
	case "v2ray-plugin":
		opts["mode"] = "websocket"
		if v, ok := get("mode"); ok {
			opts["mode"] = v
		}
		if v, ok := get("tls"); ok {
			opts["tls"] = pluginBool(v)
		}
		for _, k := range []string{"host", "path"} {
			if v, ok := get(k); ok {
				opts[k] = v
			}
		}
		if v, ok := get("mux"); ok {
			opts["mux"] = pluginBool(v)
		}
		return "v2ray-plugin", opts
	}
	for k, v := range in {
		opts[k] = v
	}
	return plugin, opts
}

func pluginBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "" || truthy(x)
	}
	return false
}

// clashStream дописывает транспорт, TLS и Reality прокси vless/vmess/trojan.
// sniKey — имя ключа SNI: servername у vless/vmess, sni у trojan.
func clashStream(n Node, m map[string]any, sniKey string) (map[string]any, error) {
	t := n.Transport
	network := normNetwork(t.Network)
	switch network {
	case "tcp":
		if t.HeaderType == "http" {
			opts := map[string]any{"method": "GET", "path": []any{orDefault(t.Path, "/")}}
			if t.Host != "" {
				opts["headers"] = map[string]any{"Host": []any{t.Host}}
			}
			m["network"] = "http"
			m["http-opts"] = opts
		} else {
			m["network"] = "tcp"
		}
	case "ws", "httpupgrade":
		opts := map[string]any{}
		if t.Path != "" {
			opts["path"] = t.Path
		}
		headers := map[string]any{}
		for k, v := range t.Headers {
			headers[k] = v
		}
		if t.Host != "" {
			headers["Host"] = t.Host
		}
		if len(headers) > 0 {
			opts["headers"] = headers
		}
		if network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		m["network"] = "ws"
		m["ws-opts"] = opts
	case "grpc":
		opts := map[string]any{}
		if t.ServiceName != "" {
			opts["grpc-service-name"] = t.ServiceName
		}
		m["network"] = "grpc"
		m["grpc-opts"] = opts
	case "h2":
		opts := map[string]any{}
		if t.Path != "" {
			opts["path"] = t.Path
		}
		if t.Host != "" {
			opts["host"] = []any{t.Host}
		}
		m["network"] = "h2"
		m["h2-opts"] = opts
	case "http":
		opts := map[string]any{"method": "GET", "path": []any{orDefault(t.Path, "/")}}
		if t.Host != "" {
			opts["headers"] = map[string]any{"Host": []any{t.Host}}
		}
		m["network"] = "http"
		m["http-opts"] = opts
	case "xhttp":
		opts, err := xhttpSettings(t, true)
		if err != nil {
			return nil, err
		}
		m["network"] = "xhttp"
		m["xhttp-opts"] = opts
	case "kcp":
		return nil, NewError(CodeKCPMihomo)
	default:
		return nil, NewError(CodeUnsupportedType, "type", network)
	}

	reality := n.Reality != nil
	if n.TLS != nil && n.TLS.Enabled || reality {
		if normProtocol(n.Protocol) != "trojan" {
			m["tls"] = true
		}
		fp := ""
		if n.TLS != nil {
			if n.TLS.SNI != "" {
				m[sniKey] = n.TLS.SNI
			}
			if len(n.TLS.ALPN) > 0 {
				m["alpn"] = stringsToAny(n.TLS.ALPN)
			}
			if n.TLS.Insecure {
				m["skip-cert-verify"] = true
			}
			if pins := normPins(n.TLS.PinSHA256); len(pins) > 0 {
				m["fingerprint"] = pins[0]
			}
			fp = n.TLS.Fingerprint
		}
		m["client-fingerprint"], _ = MihomoFingerprint(fp, reality)
		if reality {
			ro := map[string]any{"public-key": n.Reality.PublicKey}
			if n.Reality.ShortID != "" {
				ro["short-id"] = n.Reality.ShortID
			}
			m["reality-opts"] = ro
		}
	}
	return m, nil
}

// udpValue — значение udp: по умолчанию true (Xray ретранслирует UDP всегда).
func udpValue(n Node) bool {
	if n.UDP == nil {
		return true
	}
	return *n.UDP
}

// ClashProxy — прокси с именем. Родной узел Clash (Origin clash с Raw)
// отдаётся копией оригинала.
func ClashProxy(n Node, name string) (map[string]any, error) {
	var m map[string]any
	if n.Origin == OriginClash && n.Raw != nil {
		m = deepCopyMap(n.Raw)
	} else {
		var err error
		if m, err = EmitClash(n); err != nil {
			return nil, err
		}
	}
	m["name"] = name
	return m, nil
}

// clashHead — ключи прокси, которые идут первыми и в этом порядке.
var clashHead = []string{"name", "type", "server", "port"}

// ClashFile сериализует прокси в документ с ключом proxies. Порядок ключей
// прокси: name, type, server, port, затем остальные по алфавиту.
func ClashFile(proxies []map[string]any) ([]byte, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, p := range proxies {
		node, err := yamlMap(p, clashHead)
		if err != nil {
			return nil, err
		}
		seq.Content = append(seq.Content, node)
	}
	if len(seq.Content) == 0 {
		seq.Style = yaml.FlowStyle
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "proxies"},
			seq,
		},
	}}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlMap строит узел-отображение: ключи head первыми, остальные по алфавиту.
func yamlMap(m map[string]any, head []string) (*yaml.Node, error) {
	out := &yaml.Node{Kind: yaml.MappingNode}
	used := map[string]bool{}
	add := func(k string) error {
		v, err := yamlValue(m[k])
		if err != nil {
			return err
		}
		out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, v)
		return nil
	}
	for _, k := range head {
		if _, ok := m[k]; ok {
			used[k] = true
			if err := add(k); err != nil {
				return nil, err
			}
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !used[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if err := add(k); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func yamlValue(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case map[string]any:
		return yamlMap(x, nil)
	case []any:
		return yamlSeq(len(x), func(i int) any { return x[i] })
	case []string:
		return yamlSeq(len(x), func(i int) any { return x[i] })
	case []int:
		return yamlSeq(len(x), func(i int) any { return x[i] })
	case []map[string]any:
		return yamlSeq(len(x), func(i int) any { return x[i] })
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return yamlScalar(i)
		}
		f, err := x.Float64()
		if err != nil {
			return yamlScalar(x.String())
		}
		return yamlScalar(f)
	}
	return yamlScalar(v)
}

func yamlSeq(n int, at func(int) any) (*yaml.Node, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	scalars := true
	for i := 0; i < n; i++ {
		item, err := yamlValue(at(i))
		if err != nil {
			return nil, err
		}
		if item.Kind != yaml.ScalarNode {
			scalars = false
		}
		seq.Content = append(seq.Content, item)
	}
	if scalars {
		seq.Style = yaml.FlowStyle
	}
	return seq, nil
}

// yamlScalar кодирует скаляр стандартным способом yaml.v3: строки, похожие на
// числа, получают кавычки.
func yamlScalar(v any) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(v); err != nil {
		return nil, err
	}
	return &node, nil
}
