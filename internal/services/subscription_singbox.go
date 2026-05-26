package services

import (
	"encoding/json"
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
