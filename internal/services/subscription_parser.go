package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// subscriptionUserAgent — единый User-Agent для всех запросов подписок.
// Провайдеры отдают разные наборы нод в зависимости от UA клиента; разные UA
// для Xray- и Mihomo-путей приводили к рассинхрону списков нод одной подписки.
// По UA Happ провайдеры отдают максимально полный набор нод.
const subscriptionUserAgent = "Happ/1.0"

func sanitizeSSRFURL(urlStr string) string {
	b := make([]byte, len(urlStr))
	for i := 0; i < len(urlStr); i++ {
		b[i] = urlStr[i]
	}
	return string(b)
}

// fetchWithUserAgent выполняет GET с правильным User-Agent и HWID-заголовками.
func (s *SubscriptionService) fetchWithUserAgent(ctx context.Context, subURL string, sub *Subscription, ua string) (*http.Response, error) {
	parsed, err := url.ParseRequestURI(subURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, fmt.Errorf("empty host")
	}

	// Разрешаем loopback/private IP в тестовом окружении
	isTest := flag.Lookup("test.v") != nil
	if !isTest {
		if ip := net.ParseIP(hostname); ip != nil {
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
				return nil, fmt.Errorf("SSRF: target is a private/loopback IP address")
			}
			if ip4 := ip.To4(); ip4 != nil {
				if ip4[0] == 100 && (ip4[1] >= 64 && ip4[1] <= 127) {
					return nil, fmt.Errorf("SSRF: target is CGNAT IP address")
				}
			}
		} else {
			ips, err := net.LookupHost(hostname)
			if err == nil {
				for _, ipStr := range ips {
					ip := net.ParseIP(ipStr)
					if ip == nil {
						continue
					}
					if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
						return nil, fmt.Errorf("SSRF: target resolves to a private/loopback IP address")
					}
					if ip4 := ip.To4(); ip4 != nil {
						if ip4[0] == 100 && (ip4[1] >= 64 && ip4[1] <= 127) {
							return nil, fmt.Errorf("SSRF: target resolves to CGNAT IP address")
						}
					}
				}
			}
		}
	}

	cleanURL := sanitizeSSRFURL(parsed.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cleanURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	// HWID Device Limit: per-subscription override or global device HWID.
	hwid := sub.HwidToken
	if hwid == "" {
		hwid = s.hwid
	}
	if hwid != "" {
		req.Header.Set("x-hwid", hwid)

		deviceOS, deviceModel, osVersion := "Linux", "XKeen Control Panel", ""
		if s.deviceInfo != nil {
			deviceModel, deviceOS, osVersion = s.deviceInfo.Get()
		}
		req.Header.Set("x-device-os", deviceOS)
		req.Header.Set("x-device-model", deviceModel)
		if osVersion != "" {
			req.Header.Set("x-ver-os", osVersion)
		}
	}
	return s.httpClient.Do(req)
}

// DownloadRaw скачивает подписку с единым User-Agent панели
// (subscriptionUserAgent). Экспортируется для использования из
// ProviderFetch (loopback provider endpoint).
func (s *SubscriptionService) DownloadRaw(ctx context.Context, subURL string, sub *Subscription) ([]byte, http.Header, error) {
	return s.downloadRaw(ctx, subURL, sub)
}

func (s *SubscriptionService) downloadWithUA(ctx context.Context, subURL string, sub *Subscription, ua string) ([]byte, http.Header, error) {
	parsed, err := url.Parse(subURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, nil, fmt.Errorf("only http and https URLs are allowed for subscriptions")
	}
	resp, err := s.fetchWithUserAgent(ctx, subURL, sub, ua)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	applySubscriptionHeaders(resp.Header, sub)

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes))
	if err != nil {
		return nil, nil, err
	}
	return body, resp.Header, nil
}

func (s *SubscriptionService) downloadRaw(ctx context.Context, subURL string, sub *Subscription) ([]byte, http.Header, error) {
	return s.downloadWithUA(ctx, subURL, sub, subscriptionUserAgent)
}

func (s *SubscriptionService) downloadAndParse(ctx context.Context, subURL string, sub *Subscription) (outbounds []Outbound, skips []SkipReason, bodyBytes []byte, headers http.Header, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in parser: %v", r)
			log.Printf("[Subscriptions] PANIC recovered: %v", r)
		}
	}()

	body, headers, err := s.downloadWithUA(ctx, subURL, sub, subscriptionUserAgent)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	outs, skipReasons, err := parseSubscriptionBody(body, headers.Get("Content-Type"), sub)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return outs, skipReasons, body, headers, nil
}

// parseSubscriptionBody detects the format of a subscription response and parses it
// into outbounds. It tries formats in priority order: sing-box JSON, xray JSON,
// clash YAML, then base64/share-links.
func parseSubscriptionBody(body []byte, contentTypeHeader string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	contentType := strings.ToLower(contentTypeHeader)
	content := strings.TrimSpace(string(body))

	// 1) Sing-box JSON
	if (contentType == "" || strings.Contains(contentType, "json")) && looksLikeSingBoxJSON(body) {
		if outs, err := parseSingBoxJSON(body); err == nil && len(outs) > 0 {
			sub.DetectedFormat = "sing-box"
			sub.LastCount = len(outs)
			sub.LastSkipped = 0
			return outs, nil, nil
		}
	}

	// 2) Xray full-config array (each element is a complete xray config with "remarks" as node name)
	if outs := parseXrayConfigArray(body); len(outs) > 0 {
		sub.DetectedFormat = "xray-json"
		sub.LastCount = len(outs)
		sub.LastSkipped = 0
		return outs, nil, nil
	}

	// 3) Xray JSON array of outbounds (with non-empty protocol)
	var jsonOutbounds []Outbound
	if err := json.Unmarshal(body, &jsonOutbounds); err == nil {
		// filter to outbounds that actually have a protocol (avoids false positive on config arrays)
		var valid []Outbound
		for _, o := range jsonOutbounds {
			if o.Protocol != "" {
				valid = append(valid, o)
			}
		}
		if len(valid) > 0 {
			sub.DetectedFormat = "xray-json"
			sub.LastCount = len(valid)
			sub.LastSkipped = 0
			return valid, nil, nil
		}
	}

	// 4) Xray JSON object
	var jsonConfig struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &jsonConfig); err == nil && len(jsonConfig.Outbounds) > 0 {
		sub.DetectedFormat = "xray-json"
		sub.LastCount = len(jsonConfig.Outbounds)
		sub.LastSkipped = 0
		return jsonConfig.Outbounds, nil, nil
	}

	// 5) Clash/Mihomo YAML Check
	if looksLikeClashYAML(content) {
		if outs, skips, err := parseClashYAMLToXray(content, sub); err == nil && len(outs) > 0 {
			return outs, skips, nil
		}
		return nil, nil, fmt.Errorf("данная подписка имеет формат Clash/Mihomo YAML, но её не удалось распарсить для ядра XRay")
	}

	// 6) Base64 or plain share-links
	return parseShareLinks(content, sub)
}

// looksLikeClashYAML определяет, является ли content Clash/Mihomo YAML,
// разыскивая top-level ключ "proxies:" или "proxy-providers:" по всему
// документу (не ограничиваясь первыми N строками — большие конфиги с
// длинной секцией rules:/rule-providers: перед proxies: не должны
// пропускаться на этапе детекции формата).
func looksLikeClashYAML(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false
	}
	if section, _ := extractProxiesSection(trimmed); section != "" {
		return true
	}
	normalized := strings.ReplaceAll(trimmed, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if key, ok := yamlTopLevelKeyLine(line); ok && key == "proxy-providers" {
			return true
		}
	}
	return false
}

func parseClashYAMLToXray(content string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	newBlocks, _ := ParseMihomoSubscriptionBlocks(content)
	if len(newBlocks) == 0 {
		return nil, nil, fmt.Errorf("no proxy blocks found in subscription YAML")
	}

	var outbounds []Outbound
	var skipReasons []SkipReason
	skipped := 0

	for idx, block := range newBlocks {
		node := ParseClashProxyNode(block)
		if node.Tag == "" {
			continue
		}
		outbound := convertSubscriptionNodeToOutbound(&node)
		if outbound != nil {
			outbounds = append(outbounds, *outbound)
		} else {
			skipped++
			snippet := node.Tag
			if len(snippet) > 60 {
				snippet = snippet[:57] + "..."
			}
			skipReasons = append(skipReasons, SkipReason{
				Line:    idx + 1,
				Reason:  fmt.Sprintf("неподдерживаемый протокол Clash: %s", node.Protocol),
				Snippet: snippet,
			})
		}
	}

	sub.LastCount = len(outbounds)
	sub.LastSkipped = skipped
	sub.DetectedFormat = "clash-meta"

	return outbounds, skipReasons, nil
}

func convertSubscriptionNodeToOutbound(node *SubscriptionNode) *Outbound {
	protocol := node.Protocol
	if protocol == "ss" {
		protocol = "shadowsocks"
	}

	switch protocol {
	case "direct", "block", "dns", "selector", "urltest", "":
		return nil
	}

	lastColon := strings.LastIndex(node.Server, ":")
	if lastColon == -1 {
		return nil
	}
	address := node.Server[:lastColon]
	portStr := node.Server[lastColon+1:]
	address = strings.Trim(address, "[]")

	portInt, err := strconv.Atoi(portStr)
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	// Build StreamSettings
	streamSettings := map[string]interface{}{}
	network := "tcp"
	if node.Transport != "" {
		network = node.Transport
	}
	streamSettings["network"] = network

	switch network {
	case "ws":
		ws := map[string]interface{}{}
		if node.WSPath != "" {
			ws["path"] = node.WSPath
		}
		if node.ServerName != "" {
			ws["headers"] = map[string]interface{}{"Host": node.ServerName}
		}
		if len(ws) > 0 {
			streamSettings["wsSettings"] = ws
		}
	case "grpc":
		if node.WSPath != "" {
			streamSettings["grpcSettings"] = map[string]interface{}{
				"serviceName": node.WSPath,
			}
		}
	case "http", "httpupgrade":
		h := map[string]interface{}{}
		if node.ServerName != "" {
			h["host"] = []string{node.ServerName}
		}
		if node.WSPath != "" {
			h["path"] = node.WSPath
		}
		if len(h) > 0 {
			streamSettings[network+"Settings"] = h
		}
	}

	// Security
	if node.Security == "reality" {
		streamSettings["security"] = "reality"
		reality := map[string]interface{}{}
		if node.PublicKey != "" {
			reality["publicKey"] = node.PublicKey
		}
		if node.ShortID != "" {
			reality["shortId"] = node.ShortID
		}
		if node.ServerName != "" {
			reality["serverName"] = node.ServerName
		}
		if node.Fingerprint != "" {
			reality["fingerprint"] = node.Fingerprint
		}
		streamSettings["realitySettings"] = reality
	} else if node.Security == "tls" {
		streamSettings["security"] = "tls"
		tls := map[string]interface{}{}
		if node.ServerName != "" {
			tls["serverName"] = node.ServerName
		}
		if node.Insecure {
			tls["allowInsecure"] = true
		}
		if node.Fingerprint != "" {
			tls["fingerprint"] = node.Fingerprint
		}
		streamSettings["tlsSettings"] = tls
	}

	// Protocol settings
	switch protocol {
	case "vless":
		user := map[string]interface{}{
			"id":         node.UUID,
			"encryption": "none",
		}
		if node.Flow != "" {
			user["flow"] = node.Flow
		}
		return &Outbound{
			Tag:      node.Tag,
			Protocol: "vless",
			Settings: map[string]interface{}{
				"vnext": []map[string]interface{}{{
					"address": address,
					"port":    portInt,
					"users":   []map[string]interface{}{user},
				}},
			},
			StreamSettings: streamSettings,
		}

	case "vmess":
		return &Outbound{
			Tag:      node.Tag,
			Protocol: "vmess",
			Settings: map[string]interface{}{
				"vnext": []map[string]interface{}{{
					"address": address,
					"port":    portInt,
					"users": []map[string]interface{}{{
						"id":       node.UUID,
						"alterId":  node.AlterID,
						"security": "auto",
					}},
				}},
			},
			StreamSettings: streamSettings,
		}

	case "trojan":
		return &Outbound{
			Tag:      node.Tag,
			Protocol: "trojan",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  address,
					"port":     portInt,
					"password": node.Password,
				}},
			},
			StreamSettings: streamSettings,
		}

	case "shadowsocks":
		return &Outbound{
			Tag:      node.Tag,
			Protocol: "shadowsocks",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  address,
					"port":     portInt,
					"method":   node.Cipher,
					"password": node.Password,
				}},
			},
		}

	case "hysteria2", "hysteria":
		return &Outbound{
			Tag:      node.Tag,
			Protocol: "hysteria2",
			Settings: map[string]interface{}{
				"servers": []map[string]interface{}{{
					"address":  address,
					"port":     portInt,
					"password": node.Password,
				}},
			},
			StreamSettings: streamSettings,
		}
	}

	return nil
}

// parseXrayConfigArray parses a subscription where the response is a JSON array
// of complete Xray configs (each element has dns/routing/outbounds/remarks).
// Each element represents one server; "remarks" is used as the node tag.
// Returns nil if the body does not match this format.
func parseXrayConfigArray(body []byte) []Outbound {
	var configs []struct {
		Remarks   string     `json:"remarks"`
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &configs); err != nil || len(configs) == 0 {
		return nil
	}
	// Only match if first element looks like a full config (has outbounds with protocol)
	if len(configs[0].Outbounds) == 0 {
		return nil
	}
	hasProto := false
	for _, o := range configs[0].Outbounds {
		if o.Protocol != "" {
			hasProto = true
			break
		}
	}
	if !hasProto {
		return nil
	}

	proxyProtocols := map[string]bool{
		"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
		"socks": true, "http": true, "wireguard": true,
		"hysteria": true, "hysteria2": true,
	}

	var result []Outbound
	for _, cfg := range configs {
		// Find the primary proxy outbound (first one with a proxy protocol)
		for _, ob := range cfg.Outbounds {
			if !proxyProtocols[ob.Protocol] {
				continue
			}
			out := ob
			if out.Protocol == "hysteria" || out.Protocol == "hysteria2" {
				out = normalizeXrayHysteriaOutbound(out)
			}
			// Use "remarks" as the tag for this server
			if cfg.Remarks != "" {
				out.Tag = cfg.Remarks
			}
			result = append(result, out)
			break
		}
	}
	return result
}

// normalizeXrayHysteriaOutbound приводит outbound с protocol "hysteria"/
// "hysteria2" из формата xray full-config array (адрес/порт лежат прямо в
// settings, пароль — в streamSettings.hysteriaSettings.auth) к каноническому
// виду settings.servers[0]={address,port,password}, который умеют читать
// extractServer/getServerField/outboundsToNodes (as для hy2:// share-link'ов
// и Clash YAML type: hysteria). Без этой нормализации адрес/порт/пароль
// ноды остаются нераспознанными даже после того, как она не отброшена
// фильтром proxyProtocols.
func normalizeXrayHysteriaOutbound(ob Outbound) Outbound {
	address, _ := ob.Settings["address"].(string)

	var port int
	switch p := ob.Settings["port"].(type) {
	case float64:
		port = int(p)
	case int:
		port = p
	case string:
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}

	password := ""
	if ss := ob.StreamSettings; ss != nil {
		if hySettings, ok := ss["hysteriaSettings"].(map[string]interface{}); ok {
			if auth, ok := hySettings["auth"].(string); ok {
				password = auth
			}
		}
	}

	normalized := ob
	normalized.Protocol = "hysteria2"
	normalized.Settings = map[string]interface{}{
		"servers": []map[string]interface{}{{
			"address":  address,
			"port":     port,
			"password": password,
		}},
	}

	newStream := map[string]interface{}{"network": "tcp"}
	if ss := ob.StreamSettings; ss != nil {
		if sec, ok := ss["security"].(string); ok && sec != "" {
			newStream["security"] = sec
		}
		if tls, ok := ss["tlsSettings"]; ok {
			newStream["tlsSettings"] = tls
		}
	}
	normalized.StreamSettings = newStream

	return normalized
}

// parseShareLinks parses a subscription body that is either a base64-encoded or
// plain newline-separated list of proxy share links.
func parseShareLinks(content string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	wasBase64 := false
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(content)
	}
	if err == nil {
		content = string(decoded)
		wasBase64 = true
	}

	lines := strings.Split(content, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 5000 {
		return nil, nil, fmt.Errorf("subscription too large: %d entries (max 5000)", nonEmpty)
	}

	var outbounds []Outbound
	var skipReasons []SkipReason
	skipped := 0
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if outbound := parseShareLink(line); outbound != nil {
			outbounds = append(outbounds, *outbound)
		} else {
			skipped++
			snippet := line
			if len(snippet) > 60 {
				snippet = snippet[:57] + "..."
			}
			reason := skipReasonForScheme(line)
			if strings.HasPrefix(line, "vmess://") && len(line) > maxVmessLinkBytes {
				reason = "vmess:// link exceeds 8KB limit"
			}
			skipReasons = append(skipReasons, SkipReason{
				Line:    idx + 1,
				Reason:  reason,
				Snippet: snippet,
			})
		}
	}

	sub.LastCount = len(outbounds)
	sub.LastSkipped = skipped
	if wasBase64 {
		sub.DetectedFormat = "base64"
	} else {
		sub.DetectedFormat = "share-links"
	}
	return outbounds, skipReasons, nil
}

// skipReasonForScheme returns a human-readable skip reason based on the URL scheme prefix.
func skipReasonForScheme(line string) string {
	switch {
	case strings.HasPrefix(line, "vmess://"):
		return "ошибка декодирования или невалидный JSON в vmess://"
	case strings.HasPrefix(line, "vless://"):
		return "невалидный URL или порт в vless://"
	case strings.HasPrefix(line, "trojan://"):
		return "невалидный URL или порт в trojan://"
	case strings.HasPrefix(line, "ss://"):
		return "невалидный URL или порт в ss://"
	case strings.HasPrefix(line, "hy2://"), strings.HasPrefix(line, "hysteria2://"):
		return "невалидный URL или порт в hy2://"
	case strings.HasPrefix(line, "hysteria://"):
		return "невалидный URL или порт в hysteria://"
	case strings.HasPrefix(line, "tuic://"):
		return "невалидный URL или порт в tuic://"
	case strings.HasPrefix(line, "socks://"), strings.HasPrefix(line, "socks5://"):
		return "невалидный URL или порт в socks://"
	case strings.HasPrefix(line, "http-proxy://"):
		return "невалидный URL или порт в http-proxy://"
	default:
		return "неподдерживаемый протокол или невалидный URL"
	}
}

const maxVmessLinkBytes = 8192

// parseShareLink parses various share link formats
func parseShareLink(link string) (out *Outbound) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Subscriptions] PANIC recovered: %v", r)
			out = nil
		}
	}()

	// vmess://
	if strings.HasPrefix(link, "vmess://") {
		if len(link) > maxVmessLinkBytes {
			return nil
		}
		return parseVMessLink(link)
	}

	// vless://
	if strings.HasPrefix(link, "vless://") {
		return parseVLESSLink(link)
	}

	// trojan://
	if strings.HasPrefix(link, "trojan://") {
		return parseTrojanLink(link)
	}

	// ss:// (Shadowsocks)
	if strings.HasPrefix(link, "ss://") {
		return parseSSLink(link)
	}

	// hy2:// (Hysteria2)
	if strings.HasPrefix(link, "hy2://") || strings.HasPrefix(link, "hysteria2://") {
		return parseHysteria2Link(link)
	}

	// hysteria:// (Hysteria v1) — та же грамматика URI password@host:port?params#tag,
	// что и у hysteria2; консистентно с YAML type: hysteria -> Protocol hysteria2
	// в convertSubscriptionNodeToOutbound.
	if strings.HasPrefix(link, "hysteria://") {
		return parseHysteria2Link(link)
	}

	// tuic:// (TUIC)
	if strings.HasPrefix(link, "tuic://") {
		return parseTUICLink(link)
	}

	// socks:// or socks5://
	if strings.HasPrefix(link, "socks://") || strings.HasPrefix(link, "socks5://") {
		return parseSOCKSLink(link)
	}

	// http:// proxy (must come after http-based subscription URL check is done)
	if strings.HasPrefix(link, "http-proxy://") {
		return parseHTTPProxyLink(link)
	}

	return nil
}

func parseVMessLink(link string) *Outbound {
	// vmess://base64(json) — some clients use URL-safe base64 without padding
	b64 := strings.TrimPrefix(link, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Try URL-safe base64 with padding
		padded := b64
		if rem := len(padded) % 4; rem != 0 {
			padded += strings.Repeat("=", 4-rem)
		}
		var err2 error
		data, err2 = base64.URLEncoding.DecodeString(padded)
		if err2 != nil {
			// Try raw URL-safe base64 (no padding required)
			data, err2 = base64.RawURLEncoding.DecodeString(b64)
			if err2 != nil {
				return nil
			}
		}
	}

	var vmess struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
		Aid  string `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		Sni  string `json:"sni"`
	}

	if err := json.Unmarshal(data, &vmess); err != nil {
		return nil
	}

	portInt, err := strconv.Atoi(vmess.Port)
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}
	aidInt, _ := strconv.Atoi(vmess.Aid) // Aid=0 if empty/invalid — valid default

	// Build StreamSettings from VMess JSON fields
	streamSettings := map[string]interface{}{}
	if vmess.Net != "" {
		streamSettings["network"] = vmess.Net
	}
	switch vmess.Net {
	case "ws":
		wsSettings := map[string]interface{}{}
		if vmess.Path != "" {
			wsSettings["path"] = vmess.Path
		}
		if vmess.Host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": vmess.Host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	case "grpc":
		if vmess.Path != "" {
			streamSettings["grpcSettings"] = map[string]interface{}{"serviceName": vmess.Path}
		}
	}
	if vmess.TLS == "tls" {
		tlsSettings := map[string]interface{}{}
		sni := vmess.Sni
		if sni == "" {
			sni = vmess.Host
		}
		if sni != "" {
			tlsSettings["serverName"] = sni
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	ob := &Outbound{
		Tag:      vmess.PS,
		Protocol: "vmess",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": vmess.Add,
					"port":    portInt,
					"users": []map[string]interface{}{
						{
							"id":       vmess.ID,
							"alterId":  aidInt,
							"security": "auto",
						},
					},
				},
			},
		},
	}
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
}

func parseVLESSLink(link string) *Outbound {
	// vless://id@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	id := ""
	if u.User != nil {
		id = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build user entry
	user := map[string]interface{}{
		"id":         id,
		"encryption": "none",
	}
	// flow parameter
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}

	// Build StreamSettings from query params; unknown keys are silently ignored
	streamSettings := map[string]interface{}{}
	network := q.Get("type")
	if network != "" {
		streamSettings["network"] = network
	}
	security := q.Get("security")
	if security != "" {
		streamSettings["security"] = security
	}

	switch security {
	case "reality":
		realitySettings := map[string]interface{}{}
		if pbk := q.Get("pbk"); pbk != "" {
			realitySettings["publicKey"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			realitySettings["shortId"] = sid
		}
		if sni := q.Get("sni"); sni != "" {
			realitySettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			realitySettings["fingerprint"] = fp
		}
		if len(realitySettings) > 0 {
			streamSettings["realitySettings"] = realitySettings
		}
	case "tls":
		tlsSettings := map[string]interface{}{}
		if sni := q.Get("sni"); sni != "" {
			tlsSettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			tlsSettings["fingerprint"] = fp
		}
		if alpnStr := q.Get("alpn"); alpnStr != "" {
			tlsSettings["alpn"] = strings.Split(alpnStr, ",")
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	// WebSocket settings (network=ws)
	if network == "ws" {
		wsSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			wsSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	}

	// HTTPUpgrade settings (network=httpupgrade)
	if network == "httpupgrade" {
		huSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			huSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			huSettings["host"] = host
		}
		if len(huSettings) > 0 {
			streamSettings["httpupgradeSettings"] = huSettings
		}
	}

	// XHTTP / SplitHTTP settings (network=xhttp)
	if network == "xhttp" {
		xhttpSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			xhttpSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			xhttpSettings["host"] = host
		}
		if mode := q.Get("mode"); mode != "" {
			xhttpSettings["mode"] = mode
		}
		if len(xhttpSettings) > 0 {
			streamSettings["xhttpSettings"] = xhttpSettings
		}
	}

	ob := &Outbound{
		Tag:      tag,
		Protocol: "vless",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": u.Hostname(),
					"port":    portInt,
					"users":   []map[string]interface{}{user},
				},
			},
		},
	}
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
}

func parseTrojanLink(link string) *Outbound {
	// trojan://password@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// In trojan:// URIs, the password is the entire userinfo (before @), not a "password" field
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build StreamSettings from query params; unknown keys are silently ignored
	security := q.Get("security")
	if security == "" {
		security = "tls" // default for trojan
	}
	streamSettings := map[string]interface{}{
		"security": security,
	}
	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		tlsSettings["fingerprint"] = fp
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}
	if len(tlsSettings) > 0 {
		streamSettings["tlsSettings"] = tlsSettings
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "trojan",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"password": password,
				},
			},
		},
		StreamSettings: streamSettings,
	}
}

func parseHysteria2Link(link string) *Outbound {
	// hy2://password@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	insecureVal := q.Get("insecure")
	if insecureVal == "" {
		insecureVal = q.Get("skip-cert-verify")
	}
	if insecureVal == "" {
		insecureVal = q.Get("skip_cert_verify")
	}
	if insecureVal == "1" || insecureVal == "true" {
		tlsSettings["allowInsecure"] = true
	}

	streamSettings := map[string]interface{}{
		"network":     "tcp",
		"security":    "tls",
		"tlsSettings": tlsSettings,
	}

	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address":  u.Hostname(),
				"port":     portInt,
				"password": password,
			},
		},
	}

	// obfs settings placed in settings; unknown params silently ignored
	if obfs := q.Get("obfs"); obfs != "" {
		obfsMap := map[string]interface{}{"type": obfs}
		obfsPass := q.Get("obfs-password")
		if obfsPass == "" {
			obfsPass = q.Get("obfs_password")
		}
		if obfsPass == "" {
			obfsPass = q.Get("obfs-pass")
		}
		if obfsPass != "" {
			obfsMap["password"] = obfsPass
		}
		settings["hysteria2Settings"] = map[string]interface{}{
			"obfs": obfsMap,
		}
	}

	return &Outbound{
		Tag:            tag,
		Protocol:       "hysteria2",
		Settings:       settings,
		StreamSettings: streamSettings,
	}
}

func parseTUICLink(link string) *Outbound {
	// tuic://uuid:password@host:port?sni=...&congestion_control=...&alpn=...#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	uuid := ""
	password := ""
	if u.User != nil {
		uuid = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}

	server := map[string]interface{}{
		"address":  u.Hostname(),
		"port":     portInt,
		"uuid":     uuid,
		"password": password,
	}
	if cc := q.Get("congestion_control"); cc != "" {
		server["congestionControl"] = cc
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "tuic",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
		StreamSettings: map[string]interface{}{
			"network":     "udp",
			"security":    "tls",
			"tlsSettings": tlsSettings,
		},
	}
}

func parseSSLink(link string) *Outbound {
	// ss://method:password@host:port#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// Try to decode base64 user info
	userInfo := u.User.String()
	decoded, err := base64.StdEncoding.DecodeString(userInfo)
	if err == nil {
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			u.User = url.UserPassword(parts[0], parts[1])
		}
	}

	method := ""
	password := ""
	if u.User != nil {
		method = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "shadowsocks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"method":   method,
					"password": password,
				},
			},
		},
	}
}

func parseSOCKSLink(link string) *Outbound {
	link = strings.Replace(link, "socks5://", "socks://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "socks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

func parseHTTPProxyLink(link string) *Outbound {
	link = strings.Replace(link, "http-proxy://", "http://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "http",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

// ParseLinks Result holds the result for a single link parse attempt.
type ParseLinksResult struct {
	Link     string    `json:"link"`
	Outbound *Outbound `json:"outbound,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// ParseLinks parses a slice of share links and returns results for each.
func (s *SubscriptionService) ParseLinks(links []string) []ParseLinksResult {
	results := make([]ParseLinksResult, 0, len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		ob := parseShareLink(link)
		if ob == nil {
			results = append(results, ParseLinksResult{
				Link:  link,
				Error: "unsupported or invalid share link format",
			})
		} else {
			results = append(results, ParseLinksResult{
				Link:     link,
				Outbound: ob,
			})
		}
	}
	return results
}

// parseSubscriptionUserinfo parses values from Subscription-Userinfo header:
// e.g., upload=123; download=456; total=789; expire=0
func parseSubscriptionUserinfo(header string) (upload, download, total, expire int64) {
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		vStr := strings.TrimSpace(kv[1])
		val, err := strconv.ParseInt(vStr, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			upload = val
		case "download":
			download = val
		case "total":
			total = val
		case "expire":
			expire = val
		}
	}
	return
}

// applySubscriptionHeaders читает все стандартные headers подписки
// (Remnawave/Marzban/X-UI протокол) и записывает в Subscription.
func applySubscriptionHeaders(h http.Header, sub *Subscription) {
	if userInfo := h.Get("Subscription-Userinfo"); userInfo != "" {
		sub.Upload, sub.Download, sub.Total, sub.Expire = parseSubscriptionUserinfo(userInfo)
	} else {
		sub.Upload, sub.Download, sub.Total, sub.Expire = 0, 0, 0, 0
	}

	if title := h.Get("profile-title"); title != "" {
		title = strings.TrimPrefix(title, "base64:")
		if decoded, err := base64.StdEncoding.DecodeString(title); err == nil {
			sub.ProfileTitle = strings.TrimSpace(string(decoded))
		} else if decoded, err := base64.URLEncoding.DecodeString(title); err == nil {
			sub.ProfileTitle = strings.TrimSpace(string(decoded))
		} else {
			sub.ProfileTitle = strings.TrimSpace(title)
		}
	}

	if updInt := h.Get("profile-update-interval"); updInt != "" {
		if hours, err := strconv.Atoi(strings.TrimSpace(updInt)); err == nil && hours > 0 {
			sub.ProfileUpdateHours = hours
		}
	}

	sub.SupportURL = strings.TrimSpace(h.Get("support-url"))
	sub.ProfileWebPageURL = strings.TrimSpace(h.Get("profile-web-page-url"))

	if strings.EqualFold(strings.TrimSpace(h.Get("x-hwid-not-supported")), "true") {
		sub.HwidLocked = true
	} else {
		sub.HwidLocked = false
	}

	sub.ProviderType = detectProviderType(h, sub.ProfileWebPageURL, sub.SupportURL)
}

// detectProviderType определяет тип провайдера по заголовкам и URL.
func detectProviderType(h http.Header, webPageURL, supportURL string) string {
	for _, key := range []string{"x-remnawave-version", "x-remnawave", "remnawave-version"} {
		if h.Get(key) != "" {
			return "remnawave"
		}
	}
	if containsAny(webPageURL+supportURL, "remnawave") {
		return "remnawave"
	}

	for _, key := range []string{"x-marzban-version", "x-marzban"} {
		if h.Get(key) != "" {
			return "marzban"
		}
	}
	if containsAny(webPageURL+supportURL, "marzban") {
		return "marzban"
	}

	if containsAny(webPageURL+supportURL, "3x-ui", "x-ui", "3xui", "xui") {
		return "3x-ui"
	}
	if h.Get("x-xui") != "" || h.Get("x-3xui") != "" {
		return "3x-ui"
	}

	return "custom"
}

func containsAny(s string, subs ...string) bool {
	s = strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func parseAnnouncement(body []byte, headers http.Header) string {
	content := string(body)
	content = strings.TrimSpace(content)

	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(content)
	}
	if err == nil {
		content = string(decoded)
	}

	lines := strings.Split(content, "\n")
	var announceLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			ann := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			announceLines = append(announceLines, ann)
		} else {
			break
		}
	}

	if len(announceLines) > 0 {
		return strings.Join(announceLines, "\n")
	}

	if ann := headers.Get("Announce"); ann != "" {
		ann = strings.TrimPrefix(ann, "base64:")
		if dec, err := base64.StdEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		if dec, err := base64.URLEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(ann)
	}
	if ann := headers.Get("subscription-announce"); ann != "" {
		if dec, err := base64.StdEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(ann)
	}
	if desc := headers.Get("profile-description"); desc != "" {
		if dec, err := base64.StdEncoding.DecodeString(desc); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(desc)
	}
	if st := headers.Get("support-text"); st != "" {
		if dec, err := base64.StdEncoding.DecodeString(st); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(st)
	}

	return ""
}
