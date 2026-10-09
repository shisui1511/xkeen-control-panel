package nodes

import (
	"bytes"
	"encoding/json"
	"net"
	"strconv"
	"strings"
)

// xrayMark — метка исходящих пакетов, обязательная для XKeen (D-09).
const xrayMark = 255

// EmitXray строит outbound Xray из узла (без тега и метки). Всё, что Xray не
// выражает, возвращается как *SourceError с кодом причины: узел не исчезает молча.
func EmitXray(n Node) (map[string]any, error) {
	proto := normProtocol(n.Protocol)
	var settings map[string]any
	switch proto {
	case "vless":
		user := map[string]any{"id": n.UUID, "encryption": orDefault(n.Encryption, "none")}
		if n.Flow != "" {
			user["flow"] = n.Flow
		}
		settings = vnextSettings(n, user)
	case "vmess":
		user := map[string]any{
			"id":       n.UUID,
			"alterId":  n.AlterID,
			"security": orDefault(n.Security, "auto"),
		}
		settings = vnextSettings(n, user)
	case "trojan":
		settings = serversSettings(n, map[string]any{"password": n.Password})
	case "shadowsocks":
		if n.Plugin != "" {
			return nil, NewError(CodeSSPluginXray, "plugin", n.Plugin)
		}
		settings = serversSettings(n, map[string]any{"method": n.Method, "password": n.Password})
	case "socks", "http":
		extra := map[string]any{}
		if n.UUID != "" || n.Password != "" {
			extra["users"] = []any{map[string]any{"user": n.UUID, "pass": n.Password}}
		}
		settings = serversSettings(n, extra)
	case "wireguard":
		var err error
		if settings, err = wireguardSettings(n); err != nil {
			return nil, err
		}
	case "tuic":
		return nil, NewError(CodeTUICXray)
	case "hysteria2":
		return nil, NewError(CodeHysteria2Xray)
	default:
		return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
	}
	ob := map[string]any{"protocol": proto, "settings": settings}
	if proto == "wireguard" {
		return ob, nil
	}
	ss, err := xrayStream(n)
	if err != nil {
		return nil, err
	}
	if ss != nil {
		ob["streamSettings"] = ss
	}
	return ob, nil
}

func vnextSettings(n Node, user map[string]any) map[string]any {
	return map[string]any{"vnext": []any{map[string]any{
		"address": n.Host,
		"port":    n.Port,
		"users":   []any{user},
	}}}
}

func serversSettings(n Node, server map[string]any) map[string]any {
	server["address"] = n.Host
	server["port"] = n.Port
	return map[string]any{"servers": []any{server}}
}

// wireguardSettings строит settings outbound wireguard; AWG Xray не выражает.
func wireguardSettings(n Node) (map[string]any, error) {
	wg := n.WG
	if wg == nil {
		return nil, NewError(CodeUnsupportedType, "type", n.Protocol)
	}
	if len(wg.AWG) > 0 || wg.AWGVersion != "" {
		return nil, NewError(CodeAWGXray)
	}
	peer := map[string]any{
		"publicKey": wg.PeerPublicKey,
		"endpoint":  net.JoinHostPort(n.Host, strconv.Itoa(n.Port)),
	}
	if wg.PreSharedKey != "" {
		peer["preSharedKey"] = wg.PreSharedKey
	}
	if len(wg.AllowedIPs) > 0 {
		peer["allowedIPs"] = stringsToAny(wg.AllowedIPs)
	} else {
		peer["allowedIPs"] = []any{"0.0.0.0/0", "::/0"}
	}
	// noKernelTun: userspace-стек вместо настоящего TUN — панель не создаёт
	// интерфейсов на роутере, а на mipsle проверка конфигурации не падает.
	s := map[string]any{"secretKey": wg.PrivateKey, "peers": []any{peer}, "noKernelTun": true}
	if len(wg.Address) > 0 {
		s["address"] = stringsToAny(wg.Address)
	}
	if wg.MTU > 0 {
		s["mtu"] = wg.MTU
	}
	if len(wg.Reserved) > 0 {
		r := make([]any, len(wg.Reserved))
		for i, v := range wg.Reserved {
			r[i] = v
		}
		s["reserved"] = r
	}
	return s, nil
}

func stringsToAny(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
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
	ss := map[string]any{"network": network}
	t := n.Transport
	switch network {
	case "tcp":
		if t.HeaderType == "http" {
			req := map[string]any{"path": []any{orDefault(t.Path, "/")}}
			if t.Host != "" {
				req["headers"] = map[string]any{"Host": []any{t.Host}}
			}
			ss["tcpSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	case "ws":
		ws := map[string]any{}
		if t.Path != "" {
			ws["path"] = t.Path
		}
		host := t.Host
		headers := map[string]any{}
		for k, v := range t.Headers {
			if strings.EqualFold(k, "host") {
				if host == "" {
					host = v
				}
				continue
			}
			headers[k] = v
		}
		if host != "" {
			ws["host"] = host
		}
		if len(headers) > 0 {
			ws["headers"] = headers
		}
		ss["wsSettings"] = ws
	case "grpc":
		g := map[string]any{}
		if t.ServiceName != "" {
			g["serviceName"] = t.ServiceName
		}
		if t.Mode == "multi" {
			g["multiMode"] = true
		}
		ss["grpcSettings"] = g
	case "httpupgrade":
		h := map[string]any{}
		if t.Path != "" {
			h["path"] = t.Path
		}
		if t.Host != "" {
			h["host"] = t.Host
		}
		ss["httpupgradeSettings"] = h
	case "xhttp":
		x, err := xhttpSettings(t, false)
		if err != nil {
			return nil, err
		}
		ss["xhttpSettings"] = x
	case "kcp":
		k := map[string]any{}
		if t.Path != "" {
			k["seed"] = t.Path
		}
		if t.HeaderType != "" {
			k["header"] = map[string]any{"type": t.HeaderType}
		}
		ss["kcpSettings"] = k
	case "h2", "http", "quic":
		return nil, NewError(CodeXrayNoHTTPTransport, "network", network)
	default:
		return nil, NewError(CodeUnsupportedType, "type", network)
	}

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
		if xrayInsecureForbidden(n) {
			return nil, NewError(CodeXrayNoInsecure)
		}
		ts := map[string]any{}
		if n.TLS.SNI != "" {
			ts["serverName"] = n.TLS.SNI
		}
		if len(n.TLS.ALPN) > 0 {
			ts["alpn"] = stringsToAny(n.TLS.ALPN)
		}
		if fp, _ := XrayFingerprint(n.TLS.Fingerprint, false); fp != "" {
			ts["fingerprint"] = fp
		}
		if len(n.TLS.PinSHA256) > 0 {
			ts["pinnedPeerCertSha256"] = stringsToAny(n.TLS.PinSHA256)
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = ts
	}
	return ss, nil
}

// xrayInsecureForbidden — отключение проверки сертификата, которое Xray 26.x не
// выражает: allowInsecure удалён, поэтому узел с «insecure» без закрепления
// сертификата уйти в Xray не может. С закреплением (pinnedPeerCertSha256)
// узел принимается: поле allowInsecure не пишется, сертификат сверяется по хешу.
// Reality сертификат не проверяет вовсе.
func xrayInsecureForbidden(n Node) bool {
	return n.Reality == nil && n.TLS != nil && n.TLS.Enabled && n.TLS.Insecure && len(n.TLS.PinSHA256) == 0
}

// checkXrayRaw проверяет родной outbound Xray на ключи, которые Xray 26.x
// отвергает. Raw не правится: родное ядро получает оригинал или ничего, иначе
// xray run -test откатит всё применение.
func checkXrayRaw(raw map[string]any) *SourceError {
	if _, ok := raw["transport"]; ok {
		return NewError(CodeXrayForbiddenKey, "key", "transport")
	}
	ss, _ := raw["streamSettings"].(map[string]any)
	if ss == nil {
		return nil
	}
	if _, ok := ss["transport"]; ok {
		return NewError(CodeXrayForbiddenKey, "key", "transport")
	}
	if tls, ok := ss["tlsSettings"].(map[string]any); ok {
		if rawTruthy(tls["allowInsecure"]) {
			return NewError(CodeXrayNoInsecure)
		}
		if fp, _ := tls["fingerprint"].(string); fp != "" && !validXrayFingerprint(fp, false) {
			return NewError(CodeXrayForbiddenKey, "key", "tlsSettings.fingerprint")
		}
	}
	if rs, ok := ss["realitySettings"].(map[string]any); ok {
		if fp, _ := rs["fingerprint"].(string); fp != "" && !validXrayFingerprint(fp, true) {
			return NewError(CodeXrayForbiddenKey, "key", "realitySettings.fingerprint")
		}
	}
	for _, k := range []string{"network", "method"} {
		if v, _ := ss[k].(string); isRemovedTransport(v) {
			return NewError(CodeXrayNoHTTPTransport, "network", strings.ToLower(v))
		}
	}
	for _, k := range []string{"httpSettings", "quicSettings"} {
		if _, ok := ss[k]; ok {
			return NewError(CodeXrayNoHTTPTransport, "key", k)
		}
	}
	if xs, ok := ss["xhttpSettings"].(map[string]any); ok {
		if rawHasHostHeader(xs["headers"]) {
			return NewError(CodeXrayForbiddenKey, "key", "xhttpSettings.headers.Host")
		}
		if extra, ok := xs["extra"].(map[string]any); ok && rawHasHostHeader(extra["headers"]) {
			return NewError(CodeXrayForbiddenKey, "key", "xhttpSettings.headers.Host")
		}
	}
	return nil
}

func isRemovedTransport(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "h2", "http", "quic":
		return true
	}
	return false
}

// rawTruthy — значение флага из JSON: bool или строка.
func rawTruthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return truthy(x)
	}
	return false
}

func rawHasHostHeader(v any) bool {
	h, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k := range h {
		if strings.EqualFold(k, "host") {
			return true
		}
	}
	return false
}

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
