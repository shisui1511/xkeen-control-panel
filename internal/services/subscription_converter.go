package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
		if sub != nil && sub.TagPrefix != "" {
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

		// Preserve DialerProxy if already set in sub.Nodes or existing StreamSettings
		if sub != nil {
			for _, prev := range sub.Nodes {
				if prev.Tag == node.Tag {
					node.DialerProxy = prev.DialerProxy
					break
				}
			}
		}
		if node.DialerProxy == "" && outbounds[i].StreamSettings != nil {
			if sopt, ok := outbounds[i].StreamSettings["sockopt"].(map[string]interface{}); ok {
				if dp, _ := sopt["dialerProxy"].(string); dp != "" {
					node.DialerProxy = dp
				}
			}
		}

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
		case "wireguard":
			if outbounds[i].Settings != nil {
				if secKey, _ := outbounds[i].Settings["secretKey"].(string); secKey != "" {
					node.SecretKey = secKey
				}
				if mtu, _ := outbounds[i].Settings["mtu"].(float64); mtu > 0 {
					node.MTU = int(mtu)
				} else if mtuInt, _ := outbounds[i].Settings["mtu"].(int); mtuInt > 0 {
					node.MTU = mtuInt
				}
				if res := decodeWireguardReserved(outbounds[i].Settings["reserved"]); len(res) == 3 {
					node.Reserved = res
				}
				if addrs, ok := outbounds[i].Settings["address"].([]interface{}); ok {
					var localAddrs []string
					for _, a := range addrs {
						if s, ok := a.(string); ok && s != "" {
							localAddrs = append(localAddrs, s)
						}
					}
					node.LocalAddresses = localAddrs
				} else if addrs, ok := outbounds[i].Settings["address"].([]string); ok {
					node.LocalAddresses = addrs
				}
				if peers, ok := outbounds[i].Settings["peers"].([]interface{}); ok && len(peers) > 0 {
					if peer, ok := peers[0].(map[string]interface{}); ok {
						if pubKey, _ := peer["publicKey"].(string); pubKey != "" {
							node.PublicKey = pubKey
						}
						if psk, _ := peer["preSharedKey"].(string); psk != "" {
							node.PreSharedKey = psk
						}
						if ep, _ := peer["endpoint"].(string); ep != "" {
							node.Server = ep
						}
						if ka, _ := peer["keepAlive"].(int); ka > 0 {
							node.KeepAlive = ka
						} else if ka, _ := peer["keepAlive"].(float64); ka > 0 {
							node.KeepAlive = int(ka)
						}
						if aips, ok := peer["allowedIPs"].([]string); ok {
							node.AllowedIPs = aips
						} else if aips, ok := peer["allowedIPs"].([]interface{}); ok {
							var allowed []string
							for _, a := range aips {
								if s, ok := a.(string); ok && s != "" {
									allowed = append(allowed, s)
								}
							}
							node.AllowedIPs = allowed
						}
					}
				}
				if dnsList, ok := outbounds[i].Settings["dns"].([]string); ok {
					node.DNS = dnsList
				} else if dnsRaw, ok := outbounds[i].Settings["dns"].([]interface{}); ok {
					var dnsList []string
					for _, d := range dnsRaw {
						if s, ok := d.(string); ok && s != "" {
							dnsList = append(dnsList, s)
						}
					}
					node.DNS = dnsList
				}
				if awgOpt, ok := outbounds[i].Settings["awg"].(*AWGOptions); ok && awgOpt != nil {
					node.AWG = awgOpt.Clone()
				} else if awgMap, ok := outbounds[i].Settings["awg"].(map[string]interface{}); ok && awgMap != nil {
					node.AWG = parseAWGOptionsFromMap(awgMap)
				}
				if node.AWG != nil && node.AWG.IsEmpty() {
					node.AWG = nil
				}
				if node.Protocol == "wireguard" {
					node.Dialect = string(DetectWireGuardDialect(&node))
					InferAWGVersion(&node)
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

		// Для Shadowsocks, VMess, VLESS, Trojan, Hysteria 2, WireGuard
		if pType != "vless" && pType != "vmess" && pType != "trojan" && pType != "shadowsocks" && pType != "hysteria2" && pType != "hysteria" && pType != "wireguard" {
			continue // Неподдерживаемый протокол для Mihomo YAML конвертера
		}

		if pType == "wireguard" && (strings.TrimSpace(n.SecretKey) == "" || strings.TrimSpace(n.PublicKey) == "") {
			continue // пропускаем невалидные узлы без обязательных ключей
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
				sb.WriteString("    tls: true\n")
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

		case "wireguard":
			if n.SecretKey != "" {
				sb.WriteString(fmt.Sprintf("    private-key: %s\n", yamlSafeScalar(n.SecretKey)))
			}
			if n.PublicKey != "" {
				sb.WriteString(fmt.Sprintf("    public-key: %s\n", yamlSafeScalar(n.PublicKey)))
			}
			if n.PreSharedKey != "" {
				sb.WriteString(fmt.Sprintf("    pre-shared-key: %s\n", yamlSafeScalar(n.PreSharedKey)))
			}

			// Local IP(s)
			var ipv4, ipv6 string
			for _, addr := range n.LocalAddresses {
				clean := strings.TrimSpace(addr)
				ipStr := clean
				if idx := strings.Index(ipStr, "/"); idx != -1 {
					ipStr = ipStr[:idx]
				}
				parsed := net.ParseIP(ipStr)
				if parsed != nil {
					if parsed.To4() != nil && ipv4 == "" {
						ipv4 = clean
					} else if parsed.To4() == nil && ipv6 == "" {
						ipv6 = clean
					}
				} else if ipv4 == "" {
					ipv4 = clean
				}
			}
			if ipv4 != "" {
				sb.WriteString(fmt.Sprintf("    ip: %s\n", yamlSafeScalar(ipv4)))
			}
			if ipv6 != "" {
				sb.WriteString(fmt.Sprintf("    ipv6: %s\n", yamlSafeScalar(ipv6)))
			}

			if len(n.DNS) > 0 {
				sb.WriteString(fmt.Sprintf("    dns: [%s]\n", strings.Join(n.DNS, ", ")))
			}

			if n.MTU > 0 {
				sb.WriteString(fmt.Sprintf("    mtu: %d\n", n.MTU))
			}
			if len(n.Reserved) == 3 {
				sb.WriteString(fmt.Sprintf("    reserved: [%d, %d, %d]\n", n.Reserved[0], n.Reserved[1], n.Reserved[2]))
			}
			sb.WriteString("    udp: true\n")

			// AmneziaWG obfuscation options (emit-when-set under amnezia-wg-option)
			if n.AWG != nil && !n.AWG.IsEmpty() {
				sb.WriteString("    amnezia-wg-option:\n")
				awg := n.AWG
				if awg.Jc != nil {
					sb.WriteString(fmt.Sprintf("      jc: %d\n", *awg.Jc))
				}
				if awg.Jmin != nil {
					sb.WriteString(fmt.Sprintf("      jmin: %d\n", *awg.Jmin))
				}
				if awg.Jmax != nil {
					sb.WriteString(fmt.Sprintf("      jmax: %d\n", *awg.Jmax))
				}
				if awg.S1 != nil {
					sb.WriteString(fmt.Sprintf("      s1: %d\n", *awg.S1))
				}
				if awg.S2 != nil {
					sb.WriteString(fmt.Sprintf("      s2: %d\n", *awg.S2))
				}
				if awg.S3 != nil {
					sb.WriteString(fmt.Sprintf("      s3: %d\n", *awg.S3))
				}
				if awg.S4 != nil {
					sb.WriteString(fmt.Sprintf("      s4: %d\n", *awg.S4))
				}
				if awg.J1 != nil {
					sb.WriteString(fmt.Sprintf("      j1: %d\n", *awg.J1))
				}
				if awg.J2 != nil {
					sb.WriteString(fmt.Sprintf("      j2: %d\n", *awg.J2))
				}
				if awg.J3 != nil {
					sb.WriteString(fmt.Sprintf("      j3: %d\n", *awg.J3))
				}
				if awg.Itime != nil {
					sb.WriteString(fmt.Sprintf("      itime: %d\n", *awg.Itime))
				}
				formatH := func(val string) string {
					if strings.Contains(val, "-") {
						return fmt.Sprintf("%q", val)
					}
					return val
				}
				if awg.H1 != "" {
					sb.WriteString(fmt.Sprintf("      h1: %s\n", formatH(awg.H1)))
				}
				if awg.H2 != "" {
					sb.WriteString(fmt.Sprintf("      h2: %s\n", formatH(awg.H2)))
				}
				if awg.H3 != "" {
					sb.WriteString(fmt.Sprintf("      h3: %s\n", formatH(awg.H3)))
				}
				if awg.H4 != "" {
					sb.WriteString(fmt.Sprintf("      h4: %s\n", formatH(awg.H4)))
				}
				if awg.I1 != "" {
					sb.WriteString(fmt.Sprintf("      i1: %s\n", yamlSafeScalar(normalizeAWGInitPacket(awg.I1))))
				}
				if awg.I2 != "" {
					sb.WriteString(fmt.Sprintf("      i2: %s\n", yamlSafeScalar(normalizeAWGInitPacket(awg.I2))))
				}
				if awg.I3 != "" {
					sb.WriteString(fmt.Sprintf("      i3: %s\n", yamlSafeScalar(normalizeAWGInitPacket(awg.I3))))
				}
				if awg.I4 != "" {
					sb.WriteString(fmt.Sprintf("      i4: %s\n", yamlSafeScalar(normalizeAWGInitPacket(awg.I4))))
				}
				if awg.I5 != "" {
					sb.WriteString(fmt.Sprintf("      i5: %s\n", yamlSafeScalar(normalizeAWGInitPacket(awg.I5))))
				}
				effectiveVer := awg.Version
				if effectiveVer == "" && n.Dialect == "3.1" {
					effectiveVer = "3.1"
				}
				if effectiveVer != "" {
					sb.WriteString(fmt.Sprintf("      version: %s\n", yamlSafeScalar(effectiveVer)))
				}
				if awg.HeaderProtectionKey != "" {
					sb.WriteString(fmt.Sprintf("      header-protection-key: %s\n", yamlSafeScalar(awg.HeaderProtectionKey)))
				}
				if awg.ContentPaddingAddition != nil {
					sb.WriteString(fmt.Sprintf("      content-padding-addition: %d\n", *awg.ContentPaddingAddition))
				}
				if awg.RandomTrailers != nil {
					sb.WriteString(fmt.Sprintf("      random-trailers: %t\n", *awg.RandomTrailers))
				}
				if awg.DisableCookies != nil {
					sb.WriteString(fmt.Sprintf("      disable-cookies: %t\n", *awg.DisableCookies))
				}
				if awg.RekeyAfterTime != nil {
					sb.WriteString(fmt.Sprintf("      rekey-after-time: %d\n", *awg.RekeyAfterTime))
				}
				if awg.RekeyTimeout != nil {
					sb.WriteString(fmt.Sprintf("      rekey-timeout: %d\n", *awg.RekeyTimeout))
				}
				if awg.RejectAfterTime != nil {
					sb.WriteString(fmt.Sprintf("      reject-after-time: %d\n", *awg.RejectAfterTime))
				}
				if awg.KeepaliveTimeout != nil {
					sb.WriteString(fmt.Sprintf("      keepalive-timeout: %d\n", *awg.KeepaliveTimeout))
				}
				if awg.MaxHandshakeAttempts != nil {
					sb.WriteString(fmt.Sprintf("      max-handshake-attempts: %d\n", *awg.MaxHandshakeAttempts))
				}
				if len(awg.RawOptions) > 0 {
					var rawKeys []string
					for rk := range awg.RawOptions {
						rawKeys = append(rawKeys, rk)
					}
					sort.Strings(rawKeys)
					for _, rk := range rawKeys {
						sb.WriteString(fmt.Sprintf("      %s: %v\n", rk, awg.RawOptions[rk]))
					}
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
	case "xhttp":
		sb.WriteString("    xhttp-opts:\n")
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

// providerGenericWords — служебные слова, отбрасываемые при выводе бренда
// провайдера из profile-title подписки.
var providerGenericWords = map[string]bool{
	"подписка":     true,
	"профиль":      true,
	"subscription": true,
	"profile":      true,
}

// extractProviderBrand выводит человекочитаемый бренд провайдера из
// profile-title подписки: убирает эмодзи и прочие символы, отбрасывает служебные слова.
func extractProviderBrand(title string) string {
	var b strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" -_.+&", r) {
			b.WriteRune(r)
		}
	}
	words := strings.Fields(b.String())
	kept := words[:0]
	for _, w := range words {
		if !providerGenericWords[strings.ToLower(w)] {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// GetMihomoProviderName возвращает имя провайдера для Mihomo: имя, заданное
// пользователем → бренд из profile-title → fallback (ID подписки). Сегмент
// URL не используется: у большинства провайдеров он содержит секретный токен
// подписки, которому не место в config.yaml, именах файлов и UI.
func GetMihomoProviderName(profileTitle, name, fallback string) string {
	providerName := name
	if providerName == "" {
		providerName = extractProviderBrand(profileTitle)
	}
	if providerName == "" {
		providerName = fallback
	}

	// Transliterate Cyrillic before sanitizing, matching frontend slugifyProviderName.
	if sanitized := sanitizeProviderName(providerName); sanitized != "" {
		return sanitized
	}
	if sanitized := sanitizeProviderName(fallback); sanitized != "" {
		return sanitized
	}
	return "provider"
}

// sanitizeProviderName приводит имя к безопасному для YAML-ключа и имени
// файла виду: [A-Za-z0-9-], регистр сохраняется.
func sanitizeProviderName(s string) string {
	s = transliterateCyrillic(s)
	s = nonAlphanumericDashRe.ReplaceAllString(s, "-")
	s = multiDashRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func (s *SubscriptionService) writeFragment(path string, outbounds []Outbound, sub *Subscription) ([]SubscriptionNode, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Backup existing data for rollback if validation fails
	var oldData []byte
	existed := false
	if d, readErr := os.ReadFile(path); readErr == nil {
		oldData = d
		existed = true
	}

	nodes := s.outboundsToNodes(outbounds, sub)

	allowedOutbounds := make([]Outbound, 0, len(outbounds))
	allowedNodes := make([]SubscriptionNode, 0, len(outbounds))
	for i, node := range nodes {
		if allowedXrayProtocols[node.Protocol] {
			allowedOutbounds = append(allowedOutbounds, outbounds[i])
			allowedNodes = append(allowedNodes, node)
			if node.Protocol == "wireguard" && node.AWG != nil && !node.AWG.IsEmpty() {
				log.Printf("[Subscriptions] WARN: AmneziaWG obfuscation options will not be applied by Xray core for node %q", node.Tag)
			}
		} else {
			log.Printf("[Subscriptions] Skipping outbound %q for Xray configuration: unsupported protocol %q", outbounds[i].Tag, node.Protocol)
		}
	}

	if len(allowedNodes) != len(allowedOutbounds) {
		return nil, fmt.Errorf("mismatch between allowed nodes (%d) and outbounds (%d)", len(allowedNodes), len(allowedOutbounds))
	}

	// Merge sockopt and dialerProxy into allowed outbounds
	activeTags := s.collectActiveXrayTags(sub, allowedOutbounds)
	for i := range allowedOutbounds {
		mergeSockopt(&allowedOutbounds[i], sub, &allowedNodes[i], activeTags)
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

	if s.configDir != "" {
		if ok, out := ValidateXrayConfigDir(s.configDir); !ok {
			// Rollback
			if existed {
				_ = utils.AtomicWriteFile(path, oldData, 0600)
			} else {
				_ = os.Remove(path)
			}
			return nil, fmt.Errorf("Xray fragment validation failed, rolled back: %s", out)
		}
	}

	return nodes, nil
}

func (s *SubscriptionService) collectActiveXrayTags(currentSub *Subscription, currentOutbounds []Outbound) map[string]bool {
	tags := make(map[string]bool)
	if s != nil {
		for i := range s.subscriptions {
			sub := &s.subscriptions[i]
			if !sub.Enabled || !sub.EnableXray {
				continue
			}
			if currentSub != nil && sub.ID == currentSub.ID {
				continue
			}
			for _, node := range sub.Nodes {
				if allowedXrayProtocols[node.Protocol] && node.Tag != "" {
					tags[node.Tag] = true
				}
			}
		}
	}
	for _, ob := range currentOutbounds {
		if allowedXrayProtocols[ob.Protocol] && ob.Tag != "" {
			tags[ob.Tag] = true
		}
	}
	return tags
}

func mergeSockopt(ob *Outbound, sub *Subscription, node *SubscriptionNode, activeTags map[string]bool) {
	sockopt := make(map[string]interface{})
	if sub != nil {
		if sub.SockoptMark > 0 {
			sockopt["mark"] = sub.SockoptMark
		}
		if sub.SockoptFastOpen {
			sockopt["tcpFastOpen"] = true
		}
		if sub.SockoptMptcp {
			sockopt["tcpMptcp"] = true
		}
	}

	if node != nil && node.DialerProxy != "" {
		hasProxySettings := false
		if ob.ProxySettings != nil {
			hasProxySettings = true
		}
		if ob.StreamSettings != nil {
			if ps, ok := ob.StreamSettings["proxySettings"]; ok && ps != nil {
				hasProxySettings = true
			}
		}
		if hasProxySettings {
			log.Printf("[Subscriptions] Outbound %q already has proxySettings configured; skipping dialerProxy cascade to %q", ob.Tag, node.DialerProxy)
		} else if activeTags != nil && !activeTags[node.DialerProxy] {
			log.Printf("[Subscriptions] Target node %q for dialerProxy cascade of %q is not found in active Xray subscriptions; skipping", node.DialerProxy, ob.Tag)
		} else {
			sockopt["dialerProxy"] = node.DialerProxy
		}
	}

	if len(sockopt) > 0 {
		if ob.StreamSettings == nil {
			ob.StreamSettings = make(map[string]interface{})
		}
		if existingSockopt, ok := ob.StreamSettings["sockopt"].(map[string]interface{}); ok {
			for k, v := range sockopt {
				existingSockopt[k] = v
			}
		} else {
			ob.StreamSettings["sockopt"] = sockopt
		}
	}
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
	// Для wireguard
	if peersRaw, ok := ob.Settings["peers"]; ok {
		var firstPeer map[string]interface{}
		switch v := peersRaw.(type) {
		case []interface{}:
			if len(v) > 0 {
				firstPeer, _ = v[0].(map[string]interface{})
			}
		case []map[string]interface{}:
			if len(v) > 0 {
				firstPeer = v[0]
			}
		}
		if firstPeer != nil {
			if ep, ok := firstPeer["endpoint"].(string); ok && ep != "" {
				return ep
			}
		}
	}
	return ""
}
