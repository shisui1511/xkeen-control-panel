package nodes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// maxLinkBytes — предел длины одной ссылки (T-145-01).
const maxLinkBytes = 8192

// ParseLink разбирает share-ссылку в узел. Любая ошибка — *SourceError; текст
// ссылки в ошибку не попадает: он содержит секреты.
func ParseLink(line string) (Node, error) {
	line = strings.TrimSpace(line)
	if len(line) > maxLinkBytes {
		return Node{}, NewError(CodeTooLarge)
	}
	scheme, _, ok := strings.Cut(line, "://")
	if !ok || scheme == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	scheme = strings.ToLower(scheme)
	if !validScheme(scheme) {
		return Node{}, NewError(CodeUnrecognized)
	}

	var (
		n   Node
		err error
	)
	switch scheme {
	case "vless":
		n, err = parseVless(line)
	case "vmess":
		n, err = parseVmess(line)
	case "trojan":
		n, err = parseTrojan(line)
	case "ss":
		n, err = parseShadowsocks(line)
	case "hysteria2", "hy2":
		n, err = parseHysteria(line, "hysteria2")
	case "hysteria":
		n, err = parseHysteria(line, "hysteria")
	case "tuic":
		n, err = parseTUIC(line)
	case "socks", "socks5":
		n, err = parseSocksHTTP(line, "socks")
	case "http", "http-proxy":
		n, err = parseSocksHTTP(line, "http")
	case "wireguard", "wg":
		n, err = parseWireGuard(line)
	default:
		return Node{}, NewError(CodeUnknownScheme, "scheme", scheme)
	}
	if err != nil {
		return Node{}, err
	}
	n.Origin = OriginLink
	n.Format = "link"
	n.Text = line
	return n, nil
}

func validScheme(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '+' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

// splitFragment отделяет имя узла (фрагмент после #) от тела ссылки. Имя
// декодируется вручную: битая процентная последовательность в имени не должна
// ронять разбор всей ссылки.
func splitFragment(line string) (body, name string) {
	body, frag, found := strings.Cut(line, "#")
	if !found {
		return line, ""
	}
	if dec, err := url.PathUnescape(frag); err == nil {
		return body, dec
	}
	return body, frag
}

// linkURL разбирает тело ссылки и возвращает адрес и порт.
func linkURL(body string) (*url.URL, string, int, error) {
	u, err := url.Parse(body)
	if err != nil {
		return nil, "", 0, NewError(CodeUnrecognized)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return nil, "", 0, NewError(CodeUnrecognized)
	}
	return u, host, port, nil
}

// lowerQuery приводит имена параметров к нижнему регистру (первое значение
// побеждает): клиенты пишут serviceName, servicename и service_name по-разному.
func lowerQuery(q url.Values) map[string]string {
	out := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) == 0 {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if _, dup := out[k]; !dup {
			out[k] = strings.TrimSpace(v[0])
		}
	}
	return out
}

// pick возвращает первое непустое значение из перечисленных параметров.
func pick(q map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := q[k]; v != "" {
			return v
		}
	}
	return ""
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normNetwork приводит имя транспорта к каноническому виду.
func normNetwork(s string) string {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case "", "raw", "tcp":
		return "tcp"
	case "splithttp":
		return "xhttp"
	case "mkcp":
		return "kcp"
	}
	return s
}

// applyStream переносит транспорт и TLS/Reality из параметров ссылки в узел.
func applyStream(n *Node, q map[string]string, defaultSecurity string) error {
	n.Transport.Network = normNetwork(q["type"])
	n.Transport.Path = q["path"]
	n.Transport.Host = q["host"]
	n.Transport.ServiceName = pick(q, "servicename", "service_name")
	n.Transport.Mode = q["mode"]
	n.Transport.HeaderType = pick(q, "headertype", "header_type")
	if n.Transport.Network == "kcp" && n.Transport.Path == "" {
		n.Transport.Path = q["seed"]
	}
	if extra := q["extra"]; extra != "" {
		dec := json.NewDecoder(bytes.NewReader([]byte(extra)))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return NewError(CodeJSONError, "field", "extra")
		}
		n.Transport.XHTTP = m
	}

	security := strings.ToLower(q["security"])
	if security == "" {
		security = defaultSecurity
	}
	tls := &TLS{
		Enabled:     true,
		SNI:         pick(q, "sni", "peer", "servername"),
		ALPN:        splitList(q["alpn"]),
		Fingerprint: q["fp"],
		Insecure:    truthy(q["allowinsecure"]) || truthy(q["insecure"]),
		PinSHA256:   splitList(pick(q, "pcs", "pinsha256")),
	}
	switch security {
	case "tls", "xtls":
		n.TLS = tls
	case "reality":
		n.TLS = tls
		n.Reality = &Reality{PublicKey: q["pbk"], ShortID: q["sid"], SpiderX: q["spx"]}
	}
	return nil
}

func defaultName(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func parseVless(line string) (Node, error) {
	body, name := splitFragment(line)
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	q := lowerQuery(u.Query())
	n := Node{
		Name:           name,
		Protocol:       "vless",
		Host:           host,
		Port:           port,
		UUID:           u.User.Username(),
		Flow:           q["flow"],
		Encryption:     q["encryption"],
		PacketEncoding: pick(q, "packetencoding", "packet_encoding"),
	}
	if n.Encryption == "" {
		n.Encryption = "none"
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	if err := applyStream(&n, q, ""); err != nil {
		return Node{}, err
	}
	return n, nil
}

// decodeBase64Any пробует четыре варианта base64: со значением и без
// заполнения, стандартный и URL-алфавит.
func decodeBase64Any(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	var lastErr error
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		data, err := enc.DecodeString(s)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// jsonString приводит значение из JSON к строке (vmess-ссылки пишут числа и
// строкой, и числом).
func jsonString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
	}
	return ""
}

func parseVmess(line string) (Node, error) {
	body, _ := splitFragment(line)
	_, payload, _ := strings.Cut(body, "://")
	data, err := decodeBase64Any(payload)
	if err != nil {
		return Node{}, NewError(CodeBadBase64)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return Node{}, NewError(CodeJSONError)
	}
	host := strings.TrimSpace(jsonString(m["add"]))
	port, perr := strconv.Atoi(jsonString(m["port"]))
	id := jsonString(m["id"])
	if host == "" || perr != nil || port < 1 || port > 65535 || id == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	aid, _ := strconv.Atoi(jsonString(m["aid"]))
	n := Node{
		Name:     jsonString(m["ps"]),
		Protocol: "vmess",
		Host:     host,
		Port:     port,
		UUID:     id,
		AlterID:  aid,
		Security: orDefault(jsonString(m["scy"]), "auto"),
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	tr := &n.Transport
	tr.Network = normNetwork(jsonString(m["net"]))
	tr.Host = jsonString(m["host"])
	typ := jsonString(m["type"])
	if typ == "none" {
		typ = ""
	}
	switch tr.Network {
	case "grpc":
		tr.ServiceName = jsonString(m["path"])
		tr.Mode = typ
	case "xhttp":
		tr.Path = jsonString(m["path"])
		tr.Mode = typ
	default:
		tr.Path = jsonString(m["path"])
		tr.HeaderType = typ
	}
	if strings.EqualFold(jsonString(m["tls"]), "tls") {
		n.TLS = &TLS{
			Enabled:     true,
			SNI:         orDefault(jsonString(m["sni"]), tr.Host),
			ALPN:        splitList(jsonString(m["alpn"])),
			Fingerprint: jsonString(m["fp"]),
			Insecure:    truthy(jsonString(m["allowInsecure"])),
		}
	}
	return n, nil
}

// userPassword возвращает userinfo ссылки целиком: пароль может содержать ":".
func userPassword(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	pw := u.User.Username()
	if p, ok := u.User.Password(); ok {
		pw += ":" + p
	}
	return pw
}

func parseTrojan(line string) (Node, error) {
	body, name := splitFragment(line)
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	pw := userPassword(u)
	if pw == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	n := Node{Name: name, Protocol: "trojan", Host: host, Port: port, Password: pw}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	if err := applyStream(&n, lowerQuery(u.Query()), "tls"); err != nil {
		return Node{}, err
	}
	return n, nil
}

func parseShadowsocks(line string) (Node, error) {
	body, name := splitFragment(line)
	_, rest, _ := strings.Cut(body, "://")
	rest, query, _ := strings.Cut(rest, "?")
	rest = strings.TrimSuffix(rest, "/")

	var creds, hostport string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		// SIP002: userinfo@host:port, userinfo — base64(method:password) или открытый текст.
		ui, err := url.PathUnescape(rest[:at])
		if err != nil {
			return Node{}, NewError(CodeUnrecognized)
		}
		creds, hostport = ui, rest[at+1:]
		if !strings.Contains(creds, ":") {
			data, err := decodeBase64Any(creds)
			if err != nil {
				return Node{}, NewError(CodeBadBase64)
			}
			creds = string(data)
		}
	} else {
		// Старый формат: base64(method:password@host:port).
		data, err := decodeBase64Any(rest)
		if err != nil {
			return Node{}, NewError(CodeBadBase64)
		}
		at := strings.LastIndex(string(data), "@")
		if at < 0 {
			return Node{}, NewError(CodeUnrecognized)
		}
		creds, hostport = string(data[:at]), string(data[at+1:])
	}
	method, password, ok := strings.Cut(creds, ":")
	if !ok || method == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	host, portStr, err := net.SplitHostPort(hostport)
	port, perr := strconv.Atoi(portStr)
	if err != nil || perr != nil || host == "" || port < 1 || port > 65535 {
		return Node{}, NewError(CodeUnrecognized)
	}
	n := Node{Name: name, Protocol: "shadowsocks", Host: host, Port: port, Method: method, Password: password}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	if q, err := url.ParseQuery(query); err == nil {
		if plugin := q.Get("plugin"); plugin != "" {
			parts := strings.Split(plugin, ";")
			n.Plugin = parts[0]
			if len(parts) > 1 {
				n.PluginOpts = make(map[string]any, len(parts)-1)
				for _, p := range parts[1:] {
					if p == "" {
						continue
					}
					if k, v, found := strings.Cut(p, "="); found {
						n.PluginOpts[k] = v
					} else {
						n.PluginOpts[k] = true
					}
				}
			}
		}
	}
	return n, nil
}

// hyPortsRE находит список портов вида host:443,10000-20000 в адресе.
var hyPortsRE = regexp.MustCompile(`^([A-Za-z0-9]+://(?:[^@/?#]*@)?)(\[[^\]]+\]|[^/?#:\[\]]+):(\d+)((?:[,-]\d+)+)`)

func parseHysteria(line, protocol string) (Node, error) {
	body, name := splitFragment(line)
	ports := ""
	if m := hyPortsRE.FindStringSubmatch(body); m != nil {
		ports = m[3] + m[4]
		body = m[1] + m[2] + ":" + m[3] + body[len(m[0]):]
	}
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	q := lowerQuery(u.Query())
	pw := userPassword(u)
	if pw == "" {
		pw = pick(q, "auth", "auth_str", "auth-str", "password")
	}
	n := Node{
		Name:     name,
		Protocol: protocol,
		Host:     host,
		Port:     port,
		Password: pw,
		Hy2: &Hysteria2{
			Obfs:         q["obfs"],
			ObfsPassword: pick(q, "obfs-password", "obfs_password", "obfs-pass", "obfsparam", "obfs_param"),
			Up:           pick(q, "up", "upmbps", "up_mbps"),
			Down:         pick(q, "down", "downmbps", "down_mbps"),
			Ports:        orDefault(pick(q, "mport", "ports"), ports),
		},
		TLS: &TLS{
			Enabled:   true,
			SNI:       pick(q, "sni", "peer"),
			ALPN:      splitList(q["alpn"]),
			Insecure:  truthy(pick(q, "insecure", "skip-cert-verify", "skip_cert_verify", "allowinsecure")),
			PinSHA256: splitList(pick(q, "pinsha256", "pin_sha256")),
		},
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	return n, nil
}

func parseTUIC(line string) (Node, error) {
	body, name := splitFragment(line)
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return Node{}, NewError(CodeUnrecognized)
	}
	pw, _ := u.User.Password()
	q := lowerQuery(u.Query())
	n := Node{
		Name:     name,
		Protocol: "tuic",
		Host:     host,
		Port:     port,
		UUID:     u.User.Username(),
		Password: pw,
		TUIC: &TUIC{
			CongestionControl: pick(q, "congestion_control", "congestion-control", "congestion", "congestion_controller"),
			UDPRelayMode:      pick(q, "udp_relay_mode", "udp-relay-mode"),
			ReduceRTT:         truthy(pick(q, "reduce_rtt", "reduce-rtt")),
		},
		TLS: &TLS{
			Enabled:  true,
			SNI:      pick(q, "sni", "peer"),
			ALPN:     splitList(q["alpn"]),
			Insecure: truthy(pick(q, "allow_insecure", "allowinsecure", "insecure")),
		},
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	return n, nil
}

func parseSocksHTTP(line, protocol string) (Node, error) {
	body, name := splitFragment(line)
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	n := Node{Name: name, Protocol: protocol, Host: host, Port: port}
	if u.User != nil {
		user := u.User.Username()
		pass, hasPass := u.User.Password()
		if !hasPass && user != "" {
			// socks://base64(user:pass)@host:port
			if data, err := decodeBase64Any(user); err == nil {
				if du, dp, ok := strings.Cut(string(data), ":"); ok {
					user, pass = du, dp
				}
			}
		}
		n.UUID, n.Password = user, pass
	}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	return n, nil
}

// validWGKey проверяет ключ WireGuard: base64 от 32 байт.
func validWGKey(s string) bool {
	data, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(data) == 32
}

func parseWireGuard(line string) (Node, error) {
	body, name := splitFragment(line)
	u, host, port, err := linkURL(body)
	if err != nil {
		return Node{}, err
	}
	// Имена параметров приводятся к виду publickey, presharedkey, allowedips.
	q := map[string]string{}
	for k, v := range u.Query() {
		if len(v) == 0 {
			continue
		}
		k = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(strings.TrimSpace(k)))
		if _, dup := q[k]; !dup {
			q[k] = strings.TrimSpace(v[0])
		}
	}
	priv := ""
	if u.User != nil {
		priv = u.User.Username()
	}
	pub := q["publickey"]
	if priv == "" || pub == "" {
		return Node{}, NewError(CodeWGMissingKey)
	}
	psk := pick(q, "presharedkey", "psk")
	if !validWGKey(priv) || !validWGKey(pub) || (psk != "" && !validWGKey(psk)) {
		return Node{}, NewError(CodeUnrecognized)
	}
	wg := &WireGuard{
		PrivateKey:    priv,
		PeerPublicKey: pub,
		PreSharedKey:  psk,
		Address:       splitList(q["address"]),
		AllowedIPs:    splitList(q["allowedips"]),
	}
	if mtu := q["mtu"]; mtu != "" {
		v, err := strconv.Atoi(mtu)
		if err != nil || v < 0 {
			return Node{}, NewError(CodeUnrecognized)
		}
		wg.MTU = v
	}
	for _, r := range splitList(q["reserved"]) {
		v, err := strconv.Atoi(r)
		if err != nil || v < 0 || v > 255 {
			return Node{}, NewError(CodeUnrecognized)
		}
		wg.Reserved = append(wg.Reserved, v)
	}
	n := Node{Name: name, Protocol: "wireguard", Host: host, Port: port, WG: wg}
	if n.Name == "" {
		n.Name = defaultName(host, port)
	}
	return n, nil
}
