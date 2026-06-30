package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func (s *SubscriptionService) applyFilters(outbounds []Outbound, sub *Subscription) []Outbound {
	if sub.FilterName == "" && sub.FilterType == "" && sub.FilterTransport == "" {
		return outbounds
	}

	// Компилируем regex для FilterName (с case-insensitive флагом).
	// Если pattern невалидный — трактуем как пустой (не фильтруем по имени).
	var nameRe *regexp.Regexp
	if sub.FilterName != "" {
		if r, err := regexp.Compile("(?i)" + sub.FilterName); err == nil {
			nameRe = r
		}
	}

	var filtered []Outbound
	for _, ob := range outbounds {
		if nameRe != nil && !nameRe.MatchString(ob.Tag) {
			continue
		}
		if sub.FilterType != "" && !strings.EqualFold(ob.Protocol, sub.FilterType) {
			continue
		}
		if sub.FilterTransport != "" {
			transport := ""
			if ob.StreamSettings != nil {
				if net, ok := ob.StreamSettings["network"].(string); ok {
					transport = net
				}
			}
			if !strings.EqualFold(transport, sub.FilterTransport) {
				continue
			}
		}
		filtered = append(filtered, ob)
	}

	return filtered
}

func (s *SubscriptionService) outboundsToNodes(outbounds []Outbound, sub *Subscription) []SubscriptionNode {
	nodes := make([]SubscriptionNode, 0, len(outbounds))
	seen := make(map[string]int)
	for i := range outbounds {
		origTag := outbounds[i].Tag

		// Add tag prefix and deduplicate tags
		if sub.TagPrefix != "" {
			outbounds[i].Tag = fmt.Sprintf("%s-%s", sub.TagPrefix, outbounds[i].Tag)
		}
		tag := outbounds[i].Tag
		if count, exists := seen[tag]; exists {
			outbounds[i].Tag = fmt.Sprintf("%s-%d", tag, count)
			seen[tag]++
		} else {
			seen[tag] = 1
		}

		// Парсим оригинальный тег (remark) для метаданных
		node := parseRemark(origTag)
		node.Tag = outbounds[i].Tag
		node.Protocol = outbounds[i].Protocol
		node.Server = extractServer(&outbounds[i])

		// Извлекаем transport и security
		node.Transport = "tcp"
		node.Security = "none"

		// Извлекаем детальные настройки протокола
		switch node.Protocol {
		case "vless":
			node.UUID = getVNextUserField(&outbounds[i], "id")
			node.Flow = getVNextUserField(&outbounds[i], "flow")
		case "vmess":
			node.UUID = getVNextUserField(&outbounds[i], "id")
			node.AlterID = getVNextUserInt(&outbounds[i], "alterId")
		case "trojan":
			node.Password = getServerField(&outbounds[i], "password")
		case "tuic":
			node.UUID = getServerField(&outbounds[i], "uuid")
			node.Password = getServerField(&outbounds[i], "password")
			node.Congestion = getServerField(&outbounds[i], "congestionControl")
		case "shadowsocks":
			node.Cipher = getServerField(&outbounds[i], "method")
			node.Password = getServerField(&outbounds[i], "password")
		case "hysteria2":
			node.Password = getServerField(&outbounds[i], "password")
			if hy2Settings, ok := outbounds[i].Settings["hysteria2Settings"].(map[string]interface{}); ok {
				if obfsMap, ok := hy2Settings["obfs"].(map[string]interface{}); ok {
					if ot, _ := obfsMap["type"].(string); ot != "" {
						node.ObfsType = ot
					}
					if op, _ := obfsMap["password"].(string); op != "" {
						node.ObfsPassword = op
					}
				}
			}
		}

		if outbounds[i].StreamSettings != nil {
			if net, ok := outbounds[i].StreamSettings["network"].(string); ok && net != "" {
				node.Transport = net
			}
			if sec, ok := outbounds[i].StreamSettings["security"].(string); ok && sec != "" {
				node.Security = sec
			}

			// tlsSettings / realitySettings
			if node.Security == "reality" {
				if rsRaw, ok := outbounds[i].StreamSettings["realitySettings"]; ok {
					if rsMap, ok := rsRaw.(map[string]interface{}); ok {
						if pbk, _ := rsMap["publicKey"].(string); pbk != "" {
							node.PublicKey = pbk
						}
						if sid, _ := rsMap["shortId"].(string); sid != "" {
							node.ShortID = sid
						}
						if sn, _ := rsMap["serverName"].(string); sn != "" {
							node.ServerName = sn
							node.SNI = sn
						}
						if fp, _ := rsMap["fingerprint"].(string); fp != "" {
							node.Fingerprint = fp
						}
					}
				}
			} else if node.Security == "tls" {
				if tsRaw, ok := outbounds[i].StreamSettings["tlsSettings"]; ok {
					if tsMap, ok := tsRaw.(map[string]interface{}); ok {
						if sn, _ := tsMap["serverName"].(string); sn != "" {
							node.ServerName = sn
							node.SNI = sn
						}
						if fp, _ := tsMap["fingerprint"].(string); fp != "" {
							node.Fingerprint = fp
						}
						if insecure, _ := tsMap["allowInsecure"].(bool); insecure {
							node.Insecure = true
						}
					}
				}
			}

			// wsSettings / httpupgradeSettings / xhttpSettings
			if node.Transport == "ws" {
				if wsRaw, ok := outbounds[i].StreamSettings["wsSettings"]; ok {
					if wsMap, ok := wsRaw.(map[string]interface{}); ok {
						if path, _ := wsMap["path"].(string); path != "" {
							node.WSPath = path
						}
					}
				}
			} else if node.Transport == "httpupgrade" {
				if huRaw, ok := outbounds[i].StreamSettings["httpupgradeSettings"]; ok {
					if huMap, ok := huRaw.(map[string]interface{}); ok {
						if path, _ := huMap["path"].(string); path != "" {
							node.WSPath = path
						}
					}
				}
			} else if node.Transport == "xhttp" {
				if xhttpRaw, ok := outbounds[i].StreamSettings["xhttpSettings"]; ok {
					if xhttpMap, ok := xhttpRaw.(map[string]interface{}); ok {
						if path, _ := xhttpMap["path"].(string); path != "" {
							node.WSPath = path
						}
					}
				}
			}
		}

		nodes = append(nodes, node)
	}
	return nodes
}

func (s *SubscriptionService) convertSubscriptionNodesToClashYAML(nodes []SubscriptionNode) (string, []string) {
	var sb strings.Builder
	sb.WriteString("proxies:\n")
	var names []string

	for _, n := range nodes {
		// Извлекаем хост и порт
		host := ""
		port := 0
		if n.Server != "" {
			if lastColon := strings.LastIndex(n.Server, ":"); lastColon >= 0 {
				portStr := n.Server[lastColon+1:]
				if p, err := strconv.Atoi(portStr); err == nil {
					port = p
					host = n.Server[:lastColon]
					// Strip square brackets around IPv6 addresses if present
					if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
						host = host[1 : len(host)-1]
					}
				} else {
					host = n.Server
				}
			} else {
				host = n.Server
			}
		}

		if host == "" {
			continue
		}

		// Выбираем тип протокола
		pType := strings.ToLower(n.Protocol)
		if pType == "ss" {
			pType = "shadowsocks"
		}

		// Для Shadowsocks, VMess, VLESS, Trojan, Hysteria 2
		if pType != "vless" && pType != "vmess" && pType != "trojan" && pType != "shadowsocks" && pType != "hysteria2" && pType != "hysteria" {
			continue // Неподдерживаемый протокол для Mihomo YAML конвертера
		}

		if pType == "hysteria" {
			pType = "hysteria2"
		}

		names = append(names, n.Tag)

		sb.WriteString(fmt.Sprintf("  - name: %s\n", yamlSafeScalar(n.Tag)))
		sb.WriteString(fmt.Sprintf("    type: %s\n", pType))
		sb.WriteString(fmt.Sprintf("    server: %s\n", yamlSafeScalar(host)))
		if port > 0 {
			sb.WriteString(fmt.Sprintf("    port: %d\n", port))
		}

		switch pType {
		case "vless":
			sb.WriteString(fmt.Sprintf("    uuid: %s\n", yamlSafeScalar(n.UUID)))
			cipher := n.Cipher
			if cipher == "" {
				cipher = "auto"
			}
			sb.WriteString(fmt.Sprintf("    cipher: %s\n", yamlSafeScalar(cipher)))
			if n.Flow != "" {
				sb.WriteString(fmt.Sprintf("    flow: %s\n", yamlSafeScalar(n.Flow)))
			}

			// reality / tls
			if n.Security == "reality" {
				sb.WriteString("    reality-opts:\n")
				sb.WriteString(fmt.Sprintf("      public-key: %s\n", yamlSafeScalar(n.PublicKey)))
				if n.ShortID != "" {
					sb.WriteString(fmt.Sprintf("      short-id: %s\n", yamlSafeScalar(n.ShortID)))
				}
				// В VLESS/Reality sni передается в servername/sni
				if n.ServerName != "" {
					sb.WriteString(fmt.Sprintf("    servername: %s\n", yamlSafeScalar(n.ServerName)))
				}
			} else if n.Security == "tls" {
				sb.WriteString("    tls: true\n")
				if n.ServerName != "" {
					sb.WriteString(fmt.Sprintf("    servername: %s\n", yamlSafeScalar(n.ServerName)))
				}
				if n.Insecure {
					sb.WriteString("    skip-cert-verify: true\n")
				}
			}

			if n.Fingerprint != "" {
				sb.WriteString(fmt.Sprintf("    client-fingerprint: %s\n", yamlSafeScalar(n.Fingerprint)))
			}

			// network transport
			writeTransportOpts(&sb, n)

		case "vmess":
			sb.WriteString(fmt.Sprintf("    uuid: %s\n", yamlSafeScalar(n.UUID)))
			sb.WriteString(fmt.Sprintf("    alter-id: %d\n", n.AlterID))
			cipher := n.Cipher
			if cipher == "" {
				cipher = "auto"
			}
			sb.WriteString(fmt.Sprintf("    cipher: %s\n", yamlSafeScalar(cipher)))

			if n.Security == "tls" {
				sb.WriteString("    tls: true\n")
				if n.ServerName != "" {
					sb.WriteString(fmt.Sprintf("    servername: %s\n", yamlSafeScalar(n.ServerName)))
				}
				if n.Insecure {
					sb.WriteString("    skip-cert-verify: true\n")
				}
			}

			if n.Fingerprint != "" {
				sb.WriteString(fmt.Sprintf("    client-fingerprint: %s\n", yamlSafeScalar(n.Fingerprint)))
			}

			// network transport
			writeTransportOpts(&sb, n)

		case "trojan":
			sb.WriteString(fmt.Sprintf("    password: %s\n", yamlSafeScalar(n.Password)))
			sb.WriteString("    tls: true\n")
			if n.ServerName != "" {
				sb.WriteString(fmt.Sprintf("    sni: %s\n", yamlSafeScalar(n.ServerName)))
			}
			if n.Insecure {
				sb.WriteString("    skip-cert-verify: true\n")
			}
			if n.Fingerprint != "" {
				sb.WriteString(fmt.Sprintf("    client-fingerprint: %s\n", yamlSafeScalar(n.Fingerprint)))
			}

		case "shadowsocks":
			cipher := n.Cipher
			if cipher == "" {
				cipher = "aes-256-gcm"
			}
			sb.WriteString(fmt.Sprintf("    cipher: %s\n", yamlSafeScalar(cipher)))
			sb.WriteString(fmt.Sprintf("    password: %s\n", yamlSafeScalar(n.Password)))

		case "hysteria2":
			sb.WriteString(fmt.Sprintf("    password: %s\n", yamlSafeScalar(n.Password)))
			if n.ServerName != "" {
				sb.WriteString(fmt.Sprintf("    sni: %s\n", yamlSafeScalar(n.ServerName)))
			}
			if n.Insecure {
				sb.WriteString("    skip-cert-verify: true\n")
			}
			if n.ObfsType != "" {
				sb.WriteString("    obfs:\n")
				sb.WriteString(fmt.Sprintf("      type: %s\n", yamlSafeScalar(n.ObfsType)))
				if n.ObfsPassword != "" {
					sb.WriteString(fmt.Sprintf("      password: %s\n", yamlSafeScalar(n.ObfsPassword)))
				}
			}
		}
	}

	return sb.String(), names
}

func writeTransportOpts(sb *strings.Builder, n SubscriptionNode) {
	trans := strings.ToLower(n.Transport)
	if trans == "" {
		return
	}
	sb.WriteString(fmt.Sprintf("    network: %s\n", yamlSafeScalar(trans)))
	switch trans {
	case "ws":
		sb.WriteString("    ws-opts:\n")
		path := n.WSPath
		if path == "" {
			path = "/"
		}
		sb.WriteString(fmt.Sprintf("      path: %s\n", yamlSafeScalar(path)))
		if n.ServerName != "" {
			sb.WriteString("      headers:\n")
			sb.WriteString(fmt.Sprintf("        Host: %s\n", yamlSafeScalar(n.ServerName)))
		}
	case "grpc":
		sb.WriteString("    grpc-opts:\n")
		serviceName := n.WSPath
		if serviceName == "" {
			serviceName = "TunVPN"
		}
		sb.WriteString(fmt.Sprintf("      grpc-service-name: %s\n", yamlSafeScalar(serviceName)))
	case "httpupgrade":
		sb.WriteString("    httpupgrade-opts:\n")
		path := n.WSPath
		if path == "" {
			path = "/"
		}
		sb.WriteString(fmt.Sprintf("      path: %s\n", yamlSafeScalar(path)))
		if n.ServerName != "" {
			sb.WriteString("      headers:\n")
			sb.WriteString(fmt.Sprintf("        Host: %s\n", yamlSafeScalar(n.ServerName)))
		}
	}
}

func (s *SubscriptionService) applyClashFilters(blocks []string, names []string, sub *Subscription) ([]string, []string) {
	if sub.FilterName == "" && sub.FilterType == "" && sub.FilterTransport == "" {
		return blocks, names
	}

	var nameRe *regexp.Regexp
	if sub.FilterName != "" {
		if r, err := regexp.Compile("(?i)" + sub.FilterName); err == nil {
			nameRe = r
		}
	}

	var filteredBlocks []string
	var filteredNames []string

	for idx, block := range blocks {
		node := ParseClashProxyNode(block)
		if node.Tag == "" {
			continue
		}

		if nameRe != nil && !nameRe.MatchString(node.Tag) {
			continue
		}
		if sub.FilterType != "" && !strings.EqualFold(node.Protocol, sub.FilterType) {
			continue
		}
		if sub.FilterTransport != "" && !strings.EqualFold(node.Transport, sub.FilterTransport) {
			continue
		}

		filteredBlocks = append(filteredBlocks, block)
		filteredNames = append(filteredNames, names[idx])
	}

	return filteredBlocks, filteredNames
}

func getVNextUserField(ob *Outbound, field string) string {
	if ob.Settings == nil {
		return ""
	}
	vnextRaw, ok := ob.Settings["vnext"]
	if !ok {
		return ""
	}

	var firstVN map[string]interface{}
	switch v := vnextRaw.(type) {
	case []interface{}:
		if len(v) > 0 {
			firstVN, _ = v[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(v) > 0 {
			firstVN = v[0]
		}
	}
	if firstVN == nil {
		return ""
	}

	usersRaw, ok := firstVN["users"]
	if !ok {
		return ""
	}

	var firstUser map[string]interface{}
	switch u := usersRaw.(type) {
	case []interface{}:
		if len(u) > 0 {
			firstUser, _ = u[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(u) > 0 {
			firstUser = u[0]
		}
	}
	if firstUser == nil {
		return ""
	}

	val, _ := firstUser[field].(string)
	return val
}

func getVNextUserInt(ob *Outbound, field string) int {
	if ob.Settings == nil {
		return 0
	}
	vnextRaw, ok := ob.Settings["vnext"]
	if !ok {
		return 0
	}

	var firstVN map[string]interface{}
	switch v := vnextRaw.(type) {
	case []interface{}:
		if len(v) > 0 {
			firstVN, _ = v[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(v) > 0 {
			firstVN = v[0]
		}
	}
	if firstVN == nil {
		return 0
	}

	usersRaw, ok := firstVN["users"]
	if !ok {
		return 0
	}

	var firstUser map[string]interface{}
	switch u := usersRaw.(type) {
	case []interface{}:
		if len(u) > 0 {
			firstUser, _ = u[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(u) > 0 {
			firstUser = u[0]
		}
	}
	if firstUser == nil {
		return 0
	}

	switch v := firstUser[field].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

func getServerField(ob *Outbound, field string) string {
	if ob.Settings == nil {
		return ""
	}
	serversRaw, ok := ob.Settings["servers"]
	if !ok {
		return ""
	}

	var firstSrv map[string]interface{}
	switch s := serversRaw.(type) {
	case []interface{}:
		if len(s) > 0 {
			firstSrv, _ = s[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(s) > 0 {
			firstSrv = s[0]
		}
	}
	if firstSrv == nil {
		return ""
	}

	val, _ := firstSrv[field].(string)
	return val
}

// cyrillicMap maps Cyrillic runes to their Latin transliterations,
// matching the CYRILLIC_MAP used in the frontend's slugifyProviderName.
var cyrillicMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "j", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
	'ы': "y", 'э': "e", 'ю': "yu", 'я': "ya", 'ь': "", 'ъ': "",
}

// transliterateCyrillic replaces each Cyrillic rune with its Latin equivalent.
func transliterateCyrillic(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		lower := r
		if r >= 'А' && r <= 'Я' {
			lower = r - 'А' + 'а' // uppercase → lowercase Cyrillic
		} else if r == 'Ё' {
			lower = 'ё'
		}
		if lat, ok := cyrillicMap[lower]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func GetMihomoProviderName(profileTitle, name, urlStr, fallback string) string {
	providerName := profileTitle
	if providerName == "" {
		providerName = name
	}
	if providerName == "" {
		if parsed, err := url.Parse(urlStr); err == nil && parsed.Path != "" {
			providerName = path.Base(parsed.Path)
		}
	}
	if providerName == "" || providerName == "." || providerName == "/" {
		providerName = fallback
	}

	// Transliterate Cyrillic before sanitizing, matching frontend slugifyProviderName.
	providerName = transliterateCyrillic(providerName)
	providerName = strings.ToLower(providerName)
	providerName = nonAlphanumericDashRe.ReplaceAllString(providerName, "-")
	providerName = multiDashRe.ReplaceAllString(providerName, "-")
	providerName = strings.Trim(providerName, "-")

	if providerName == "" {
		providerName = fallback
	}
	if matched, _ := regexp.MatchString(`^[a-z0-9\-]+$`, providerName); !matched {
		providerName = "safe-provider-" + fallback
		providerName = nonAlphanumericDashRe.ReplaceAllString(providerName, "-")
		providerName = strings.ToLower(providerName)
	}
	return providerName
}

func (s *SubscriptionService) writeFragment(path string, outbounds []Outbound, sub *Subscription) ([]SubscriptionNode, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	nodes := s.outboundsToNodes(outbounds, sub)

	allowedOutbounds := make([]Outbound, 0, len(outbounds))
	for i, node := range nodes {
		if allowedXrayProtocols[node.Protocol] {
			allowedOutbounds = append(allowedOutbounds, outbounds[i])
		} else {
			log.Printf("[Subscriptions] Skipping outbound %q for Xray configuration: unsupported protocol %q", outbounds[i].Tag, node.Protocol)
		}
	}

	wrapper := struct {
		Outbounds []Outbound `json:"outbounds"`
	}{
		Outbounds: allowedOutbounds,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := utils.AtomicWriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	return nodes, nil
}

func extractServer(ob *Outbound) string {
	if ob.Settings == nil {
		return ""
	}
	// Для vmess / vless
	if vnextRaw, ok := ob.Settings["vnext"]; ok {
		var firstVN map[string]interface{}
		switch v := vnextRaw.(type) {
		case []interface{}:
			if len(v) > 0 {
				firstVN, _ = v[0].(map[string]interface{})
			}
		case []map[string]interface{}:
			if len(v) > 0 {
				firstVN = v[0]
			}
		}
		if firstVN != nil {
			address, _ := firstVN["address"].(string)
			var port float64
			if p, ok := firstVN["port"].(float64); ok {
				port = p
			} else if p, ok := firstVN["port"].(int); ok {
				port = float64(p)
			}
			if address != "" && port > 0 {
				return fmt.Sprintf("%s:%d", address, int(port))
			}
		}
	}
	// Для trojan / shadowsocks / hysteria2 / socks / http
	if serversRaw, ok := ob.Settings["servers"]; ok {
		var firstS map[string]interface{}
		switch v := serversRaw.(type) {
		case []interface{}:
			if len(v) > 0 {
				firstS, _ = v[0].(map[string]interface{})
			}
		case []map[string]interface{}:
			if len(v) > 0 {
				firstS = v[0]
			}
		}
		if firstS != nil {
			address, _ := firstS["address"].(string)
			var port float64
			if p, ok := firstS["port"].(float64); ok {
				port = p
			} else if p, ok := firstS["port"].(int); ok {
				port = float64(p)
			}
			if address != "" && port > 0 {
				return fmt.Sprintf("%s:%d", address, int(port))
			}
		}
	}
	return ""
}

