package nodes

import (
	"bytes"
	"encoding/json"
)

// xrayMark — метка исходящих пакетов, обязательная для XKeen (D-09).
const xrayMark = 255

// EmitXray строит outbound Xray из узла (без тега и метки).
func EmitXray(n Node) (map[string]any, error) {
	proto := normProtocol(n.Protocol)
	var settings map[string]any
	switch proto {
	case "vless":
		user := map[string]any{"id": n.UUID, "encryption": orDefault(n.Encryption, "none")}
		if n.Flow != "" {
			user["flow"] = n.Flow
		}
		settings = map[string]any{"vnext": []any{map[string]any{
			"address": n.Host,
			"port":    n.Port,
			"users":   []any{user},
		}}}
	default:
		return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
	}
	ob := map[string]any{"protocol": proto, "settings": settings}
	ss, err := xrayStream(n)
	if err != nil {
		return nil, err
	}
	if ss != nil {
		ob["streamSettings"] = ss
	}
	return ob, nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// xrayStream строит streamSettings: транспорт, TLS и Reality.
func xrayStream(n Node) (map[string]any, error) {
	network := normNetwork(n.Transport.Network)
	if network != "tcp" {
		return nil, NewError(CodeUnsupportedType, "type", network)
	}
	ss := map[string]any{"network": network}
	switch {
	case n.Reality != nil:
		rs := map[string]any{"publicKey": n.Reality.PublicKey}
		if n.Reality.ShortID != "" {
			rs["shortId"] = n.Reality.ShortID
		}
		if n.Reality.SpiderX != "" {
			rs["spiderX"] = n.Reality.SpiderX
		}
		if n.TLS != nil {
			if n.TLS.SNI != "" {
				rs["serverName"] = n.TLS.SNI
			}
			if fp, _ := XrayFingerprint(n.TLS.Fingerprint, true); fp != "" {
				rs["fingerprint"] = fp
			}
		}
		ss["security"] = "reality"
		ss["realitySettings"] = rs
	case n.TLS != nil && n.TLS.Enabled:
		ts := map[string]any{}
		if n.TLS.SNI != "" {
			ts["serverName"] = n.TLS.SNI
		}
		if len(n.TLS.ALPN) > 0 {
			ts["alpn"] = n.TLS.ALPN
		}
		if fp, _ := XrayFingerprint(n.TLS.Fingerprint, false); fp != "" {
			ts["fingerprint"] = fp
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = ts
	}
	return ss, nil
}

// checkXrayRaw проверяет родной outbound Xray на ключи, которые Xray 26.x
// отвергает (заполняется в задаче 3 плана).
func checkXrayRaw(raw map[string]any) *SourceError { return nil }

// XrayOutbound — outbound с тегом и меткой sockopt.mark. Родной узел Xray-JSON
// (Origin xray_json с Raw) идёт копией оригинала после проверки запретов,
// остальные — через EmitXray.
func XrayOutbound(n Node, tag string) (map[string]any, error) {
	var ob map[string]any
	if n.Origin == OriginXrayJSON && n.Raw != nil {
		if e := checkXrayRaw(n.Raw); e != nil {
			return nil, e
		}
		ob = deepCopyMap(n.Raw)
	} else {
		var err error
		if ob, err = EmitXray(n); err != nil {
			return nil, err
		}
	}
	ob["tag"] = tag
	if proto, _ := ob["protocol"].(string); proto != "blackhole" && proto != "loopback" {
		setSockoptMark(ob)
	}
	return ob, nil
}

// setSockoptMark ставит streamSettings.sockopt.mark = 255.
func setSockoptMark(ob map[string]any) {
	ss, ok := ob["streamSettings"].(map[string]any)
	if !ok {
		ss = map[string]any{}
		ob["streamSettings"] = ss
	}
	so, ok := ss["sockopt"].(map[string]any)
	if !ok {
		so = map[string]any{}
		ss["sockopt"] = so
	}
	so["mark"] = xrayMark
}

// XrayFile сериализует outbounds в файл конфигурации Xray. Ключи карт
// сортируются, числа json.Number сохраняются, в конце перевод строки.
func XrayFile(outbounds []map[string]any) ([]byte, error) {
	if outbounds == nil {
		outbounds = []map[string]any{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"outbounds": outbounds}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// deepCopyMap копирует вложенные карты и списки: исходный Raw не меняется.
func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopy(v)
	}
	return out
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return deepCopyMap(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = deepCopy(x[i])
		}
		return out
	case []string:
		return append([]string(nil), x...)
	case []map[string]any:
		out := make([]map[string]any, len(x))
		for i := range x {
			out[i] = deepCopyMap(x[i])
		}
		return out
	}
	return v
}
