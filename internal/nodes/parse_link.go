package nodes

import (
	"bytes"
	"encoding/json"
	"net"
	"net/url"
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
