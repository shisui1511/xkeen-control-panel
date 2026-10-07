package nodes

import (
	"bytes"
	"encoding/json"
	"sort"

	"gopkg.in/yaml.v3"
)

// EmitClash строит прокси Clash из узла (без имени).
func EmitClash(n Node) (map[string]any, error) {
	proto := normProtocol(n.Protocol)
	m := map[string]any{"type": proto, "server": n.Host, "port": n.Port}
	switch proto {
	case "vless":
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
	default:
		return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
	}
	network := normNetwork(n.Transport.Network)
	if network != "tcp" {
		return nil, NewError(CodeUnsupportedType, "type", network)
	}
	m["network"] = network
	if n.TLS != nil && n.TLS.Enabled || n.Reality != nil {
		m["tls"] = true
		if n.TLS != nil {
			if n.TLS.SNI != "" {
				m["servername"] = n.TLS.SNI
			}
			if len(n.TLS.ALPN) > 0 {
				m["alpn"] = n.TLS.ALPN
			}
			if n.TLS.Insecure {
				m["skip-cert-verify"] = true
			}
		}
		fp := ""
		if n.TLS != nil {
			fp = n.TLS.Fingerprint
		}
		m["client-fingerprint"], _ = MihomoFingerprint(fp, n.Reality != nil)
		if n.Reality != nil {
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
