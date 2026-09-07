package services

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
)

// Sing-box outbound — формат который отдают Hiddify, Karing, INCY и
// большинство sing-box-совместимых клиентов. Структура отличается от
// Xray:
//   - server_port вместо port
//   - uuid на верхнем уровне вместо settings.vnext[0].users[0].id
//   - tls.server_name вместо streamSettings.tlsSettings.serverName
//   - transport — отдельный объект с типом и опциями
//
// Этот парсер конвертирует sing-box outbounds в наш Outbound (XRay-формат).
type singBoxOutbound struct {
	Type       string          `json:"type"`
	Tag        string          `json:"tag"`
	Server     string          `json:"server"`
	ServerPort int             `json:"server_port"`
	UUID       string          `json:"uuid"`
	Password   string          `json:"password"`
	Method     string          `json:"method"` // shadowsocks
	Flow       string          `json:"flow"`
	TLS        *singBoxTLS     `json:"tls,omitempty"`
	Transport  *singBoxTrans   `json:"transport,omitempty"`
	Multiplex  json.RawMessage `json:"multiplex,omitempty"`

	// WireGuard fields
	PrivateKey    string      `json:"private_key"`
	PeerPublicKey string      `json:"peer_public_key"`
	PreSharedKey  string      `json:"pre_shared_key"`
	LocalAddress  interface{} `json:"local_address"`
	MTU           int         `json:"mtu"`
	Reserved      interface{} `json:"reserved"`

	// AmneziaWG fields
	Jc                     *int                   `json:"jc,omitempty"`
	Jmin                   *int                   `json:"jmin,omitempty"`
	Jmax                   *int                   `json:"jmax,omitempty"`
	S1                     *int                   `json:"s1,omitempty"`
	S2                     *int                   `json:"s2,omitempty"`
	S3                     *int                   `json:"s3,omitempty"`
	S4                     *int                   `json:"s4,omitempty"`
	H1                     string                 `json:"h1,omitempty"`
	H2                     string                 `json:"h2,omitempty"`
	H3                     string                 `json:"h3,omitempty"`
	H4                     string                 `json:"h4,omitempty"`
	I1                     string                 `json:"i1,omitempty"`
	I2                     string                 `json:"i2,omitempty"`
	I3                     string                 `json:"i3,omitempty"`
	I4                     string                 `json:"i4,omitempty"`
	I5                     string                 `json:"i5,omitempty"`
	Version                string                 `json:"version,omitempty"`
	HeaderProtectionKey    string                 `json:"header_protection_key,omitempty"`
	ContentPaddingAddition *int                   `json:"content_padding_addition,omitempty"`
	RandomTrailers         *bool                  `json:"random_trailers,omitempty"`
	DisableCookies         *bool                  `json:"disable_cookies,omitempty"`
	RekeyAfterTime         *int                   `json:"rekey_after_time,omitempty"`
	AmneziaWG              map[string]interface{} `json:"amnezia_wg,omitempty"`
	AWG                    map[string]interface{} `json:"awg,omitempty"`
}

type singBoxTLS struct {
	Enabled    bool            `json:"enabled"`
	ServerName string          `json:"server_name"`
	Insecure   bool            `json:"insecure"`
	ALPN       []string        `json:"alpn"`
	Reality    *singBoxReality `json:"reality,omitempty"`
	UTLS       *singBoxUTLS    `json:"utls,omitempty"`
}

type singBoxReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type singBoxUTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}

type singBoxTrans struct {
	Type        string            `json:"type"`
	Path        string            `json:"path"`
	Host        string            `json:"host"`
	Headers     map[string]string `json:"headers,omitempty"`
	ServiceName string            `json:"service_name"` // grpc
}

// looksLikeSingBoxJSON определяет является ли body sing-box-форматом
// по наличию snake_case полей (server_port, transport.type и т.п.),
// которые отсутствуют в xray-json формате.
func looksLikeSingBoxJSON(body []byte) bool {
	// Быстрая проверка — наличие server_port в JSON.
	// В xray-json соответствующее поле — port.
	return strings.Contains(string(body), `"server_port"`) ||
		strings.Contains(string(body), `"server_port" :`)
}

// parseSingBoxJSON парсит подписку в sing-box формате
// (одиночный объект с outbounds[] или просто массив outbounds).
func parseSingBoxJSON(body []byte) ([]Outbound, error) {
	var wrapper struct {
		Outbounds []singBoxOutbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Outbounds) > 0 {
		return convertSingBoxOutbounds(wrapper.Outbounds), nil
	}

	var arr []singBoxOutbound
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		return convertSingBoxOutbounds(arr), nil
	}

	return nil, nil
}

func convertSingBoxOutbounds(sb []singBoxOutbound) []Outbound {
	result := make([]Outbound, 0, len(sb))
	for i := range sb {
		ob := convertSingBoxOutbound(&sb[i])
		if ob != nil {
			result = append(result, *ob)
		}
	}
	return result
}

func convertSingBoxOutbound(sb *singBoxOutbound) *Outbound {
	// Skip non-proxy outbounds (direct, block, dns, selector, urltest)
	switch sb.Type {
	case "direct", "block", "dns", "selector", "urltest", "":
		return nil
	}

	tag := sb.Tag
	if tag == "" {
		tag = sb.Server
	}
	if sb.ServerPort < 1 || sb.ServerPort > 65535 {
		return nil
	}

	streamSettings := convertSingBoxStreamSettings(sb)

	switch sb.Type {
	case "vless":
		user := map[string]interface{}{
			"id":         sb.UUID,
			"encryption": "none",
		}
		if sb.Flow != "" {
			user["flow"] = sb.Flow
		}
		return &Outbound{
			Tag:      tag,
			Protocol: "vless",
			Settings: map[string]interface{}{
				"vnext": []map[string]interface{}{{
					"address": sb.Server,
					"port":    sb.ServerPort,
					"users":   []map[string]interface{}{user},
				}},
			},
			StreamSettings: streamSettings,
		}

	case "vmess":
		return &Outbound{
			Tag:      tag,
			Protocol: "vmess",
			Settings: map[string]interface{}{
				"vnext": []map[string]interface{}{{
					"address": sb.Server,
					"port":    sb.ServerPort,
					"users": []map[string]interface{}{{
						"id":       sb.UUID,
						"alterId":  0,
						"security": "auto",
					}},
				}},
			},
			StreamSettings: streamSettings,
		}

	case "trojan":
		return &Outbound{
			Tag:      tag,
			Protocol: "trojan",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  sb.Server,
					"port":     sb.ServerPort,
					"password": sb.Password,
				}},
			},
			StreamSettings: streamSettings,
		}

	case "shadowsocks":
		return &Outbound{
			Tag:      tag,
			Protocol: "shadowsocks",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  sb.Server,
					"port":     sb.ServerPort,
					"method":   sb.Method,
					"password": sb.Password,
				}},
			},
		}

	case "hysteria2":
		return &Outbound{
			Tag:      tag,
			Protocol: "hysteria2",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  sb.Server,
					"port":     sb.ServerPort,
					"password": sb.Password,
				}},
			},
			StreamSettings: streamSettings,
		}

	case "wireguard", "amneziawg", "amnezia-wg", "awg":
		var localAddrs []string
		if sb.LocalAddress != nil {
			switch la := sb.LocalAddress.(type) {
			case string:
				if s := strings.TrimSpace(la); s != "" {
					localAddrs = []string{s}
				}
			case []interface{}:
				for _, elem := range la {
					if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
						localAddrs = append(localAddrs, strings.TrimSpace(s))
					}
				}
			}
		}

		reserved := decodeWireguardReserved(sb.Reserved)
		pubKey := sb.PeerPublicKey
		var serverAddr string
		if sb.Server != "" && sb.ServerPort > 0 {
			serverAddr = net.JoinHostPort(sb.Server, strconv.Itoa(sb.ServerPort))
		}

		var awg *AWGOptions
		if sb.AmneziaWG != nil {
			awg = parseAWGOptionsFromMap(sb.AmneziaWG)
		} else if sb.AWG != nil {
			awg = parseAWGOptionsFromMap(sb.AWG)
		}
		if awg == nil {
			awg = &AWGOptions{RawOptions: make(map[string]interface{})}
		}
		if sb.Jc != nil {
			awg.Jc = sb.Jc
		}
		if sb.Jmin != nil {
			awg.Jmin = sb.Jmin
		}
		if sb.Jmax != nil {
			awg.Jmax = sb.Jmax
		}
		if sb.S1 != nil {
			awg.S1 = sb.S1
		}
		if sb.S2 != nil {
			awg.S2 = sb.S2
		}
		if sb.S3 != nil {
			awg.S3 = sb.S3
		}
		if sb.S4 != nil {
			awg.S4 = sb.S4
		}
		if sb.H1 != "" {
			awg.H1 = sb.H1
		}
		if sb.H2 != "" {
			awg.H2 = sb.H2
		}
		if sb.H3 != "" {
			awg.H3 = sb.H3
		}
		if sb.H4 != "" {
			awg.H4 = sb.H4
		}
		if sb.I1 != "" {
			awg.I1 = strings.ToUpper(sb.I1)
		}
		if sb.I2 != "" {
			awg.I2 = strings.ToUpper(sb.I2)
		}
		if sb.I3 != "" {
			awg.I3 = strings.ToUpper(sb.I3)
		}
		if sb.I4 != "" {
			awg.I4 = strings.ToUpper(sb.I4)
		}
		if sb.I5 != "" {
			awg.I5 = strings.ToUpper(sb.I5)
		}
		if sb.Version != "" {
			awg.Version = sb.Version
		}
		if sb.HeaderProtectionKey != "" {
			awg.HeaderProtectionKey = sb.HeaderProtectionKey
		}
		if sb.ContentPaddingAddition != nil {
			awg.ContentPaddingAddition = sb.ContentPaddingAddition
		}
		if sb.RandomTrailers != nil {
			awg.RandomTrailers = sb.RandomTrailers
		}
		if sb.DisableCookies != nil {
			awg.DisableCookies = sb.DisableCookies
		}
		if sb.RekeyAfterTime != nil {
			awg.RekeyAfterTime = sb.RekeyAfterTime
		}

		if awg.IsEmpty() {
			awg = nil
		}

		node := &SubscriptionNode{
			Tag:            tag,
			Protocol:       "wireguard",
			Server:         serverAddr,
			PublicKey:      pubKey,
			SecretKey:      sb.PrivateKey,
			PreSharedKey:   sb.PreSharedKey,
			LocalAddresses: localAddrs,
			MTU:            sb.MTU,
			Reserved:       reserved,
			AWG:            awg,
		}
		ob, _ := wireguardNodeToOutbound(node)
		return ob
	}

	return nil
}

// convertSingBoxStreamSettings формирует XRay streamSettings из sing-box
// TLS + transport блоков.
func convertSingBoxStreamSettings(sb *singBoxOutbound) map[string]interface{} {
	ss := map[string]interface{}{}

	// Transport (network).
	network := "tcp"
	if sb.Transport != nil && sb.Transport.Type != "" {
		network = sb.Transport.Type
	}
	ss["network"] = network

	if sb.Transport != nil {
		switch sb.Transport.Type {
		case "ws":
			ws := map[string]interface{}{}
			if sb.Transport.Path != "" {
				ws["path"] = sb.Transport.Path
			}
			headers := map[string]interface{}{}
			if sb.Transport.Host != "" {
				headers["Host"] = sb.Transport.Host
			}
			for k, v := range sb.Transport.Headers {
				headers[k] = v
			}
			if len(headers) > 0 {
				ws["headers"] = headers
			}
			if len(ws) > 0 {
				ss["wsSettings"] = ws
			}
		case "grpc":
			if sb.Transport.ServiceName != "" {
				ss["grpcSettings"] = map[string]interface{}{
					"serviceName": sb.Transport.ServiceName,
				}
			}
		case "http", "httpupgrade":
			h := map[string]interface{}{}
			if sb.Transport.Host != "" {
				h["host"] = []string{sb.Transport.Host}
			}
			if sb.Transport.Path != "" {
				h["path"] = sb.Transport.Path
			}
			if len(h) > 0 {
				ss["httpSettings"] = h
			}
		}
	}

	// TLS / Reality.
	if sb.TLS != nil && sb.TLS.Enabled {
		if sb.TLS.Reality != nil && sb.TLS.Reality.Enabled {
			ss["security"] = "reality"
			reality := map[string]interface{}{}
			if sb.TLS.Reality.PublicKey != "" {
				reality["publicKey"] = sb.TLS.Reality.PublicKey
			}
			if sb.TLS.Reality.ShortID != "" {
				reality["shortId"] = sb.TLS.Reality.ShortID
			}
			if sb.TLS.ServerName != "" {
				reality["serverName"] = sb.TLS.ServerName
			}
			if sb.TLS.UTLS != nil && sb.TLS.UTLS.Fingerprint != "" {
				reality["fingerprint"] = sb.TLS.UTLS.Fingerprint
			}
			ss["realitySettings"] = reality
		} else {
			ss["security"] = "tls"
			tls := map[string]interface{}{}
			if sb.TLS.ServerName != "" {
				tls["serverName"] = sb.TLS.ServerName
			}
			if len(sb.TLS.ALPN) > 0 {
				tls["alpn"] = sb.TLS.ALPN
			}
			if sb.TLS.Insecure {
				tls["allowInsecure"] = true
			}
			if sb.TLS.UTLS != nil && sb.TLS.UTLS.Fingerprint != "" {
				tls["fingerprint"] = sb.TLS.UTLS.Fingerprint
			}
			if len(tls) > 0 {
				ss["tlsSettings"] = tls
			}
		}
	}

	return ss
}
