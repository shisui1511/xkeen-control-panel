package services

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// LooksLikeWgQuickConf экспортирует проверку формата wg-quick .conf для хендлеров.
func LooksLikeWgQuickConf(content string) bool {
	return looksLikeWgQuickConf(content)
}

// looksLikeWgQuickConf возвращает true, если содержимое напоминает wg-quick .conf конфигурацию.
func looksLikeWgQuickConf(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		return false
	}
	if looksLikeClashYAML(trimmed) {
		return false
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "[interface]") {
		return false
	}
	return strings.Contains(lower, "privatekey") ||
		strings.Contains(lower, "address") ||
		strings.Contains(lower, "[peer]") ||
		strings.Contains(lower, "endpoint") ||
		strings.Contains(lower, "jc") ||
		strings.Contains(lower, "s1")
}

// parseBoolValue парсит булевы флаги из различных текстовых представлений.
func parseBoolValue(val string) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(val))
	switch v {
	case "1", "true", "yes", "on", "t":
		return true, nil
	case "0", "false", "no", "off", "f":
		return false, nil
	default:
		return strconv.ParseBool(v)
	}
}

// normalizeAWGInitPacket нормализует значение init packet (I1..I5).
// Если значение содержит CPS-теги генератора (<c>, <b 0x...>, <r N>, <t>), сохраняет исходный регистр,
// иначе приводит hex-строку к верхнему регистру.
func normalizeAWGInitPacket(val string) string {
	trimmed := strings.TrimSpace(val)
	if strings.Contains(trimmed, "<") {
		return trimmed
	}
	return strings.ToUpper(trimmed)
}

// parseAWGField парсит один ключ-значение параметра AmneziaWG в переданный AWGOptions.
// Возвращает true, если ключ был распознан как известный параметр AWG 3.1.
func parseAWGField(awg *AWGOptions, key, val string) bool {
	if awg == nil {
		return false
	}
	cleanKey := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "_", ""), "-", ""))
	val = strings.TrimSpace(val)

	switch cleanKey {
	case "jc":
		if n, err := strconv.Atoi(val); err == nil {
			awg.Jc = &n
			return true
		}
	case "jmin":
		if n, err := strconv.Atoi(val); err == nil {
			awg.Jmin = &n
			return true
		}
	case "jmax":
		if n, err := strconv.Atoi(val); err == nil {
			awg.Jmax = &n
			return true
		}
	case "s1":
		if n, err := strconv.Atoi(val); err == nil {
			awg.S1 = &n
			return true
		}
	case "s2":
		if n, err := strconv.Atoi(val); err == nil {
			awg.S2 = &n
			return true
		}
	case "s3":
		if n, err := strconv.Atoi(val); err == nil {
			awg.S3 = &n
			return true
		}
	case "s4":
		if n, err := strconv.Atoi(val); err == nil {
			awg.S4 = &n
			return true
		}
	case "h1":
		awg.H1 = val
		return true
	case "h2":
		awg.H2 = val
		return true
	case "h3":
		awg.H3 = val
		return true
	case "h4":
		awg.H4 = val
		return true
	case "i1":
		awg.I1 = normalizeAWGInitPacket(val)
		return true
	case "i2":
		awg.I2 = normalizeAWGInitPacket(val)
		return true
	case "i3":
		awg.I3 = normalizeAWGInitPacket(val)
		return true
	case "i4":
		awg.I4 = normalizeAWGInitPacket(val)
		return true
	case "i5":
		awg.I5 = normalizeAWGInitPacket(val)
		return true
	case "version":
		awg.Version = val
		return true
	case "headerprotectionkey":
		awg.HeaderProtectionKey = val
		return true
	case "contentpaddingaddition", "cpa":
		if n, err := strconv.Atoi(val); err == nil {
			awg.ContentPaddingAddition = &n
			return true
		}
	case "randomtrailers":
		if b, err := parseBoolValue(val); err == nil {
			awg.RandomTrailers = &b
			return true
		}
	case "disablecookies":
		if b, err := parseBoolValue(val); err == nil {
			awg.DisableCookies = &b
			return true
		}
	case "rekeyaftertime", "rekey":
		if n, err := strconv.Atoi(val); err == nil {
			awg.RekeyAfterTime = &n
			return true
		}
	case "j1":
		if n, err := strconv.Atoi(val); err == nil {
			awg.J1 = &n
			return true
		}
	case "j2":
		if n, err := strconv.Atoi(val); err == nil {
			awg.J2 = &n
			return true
		}
	case "j3":
		if n, err := strconv.Atoi(val); err == nil {
			awg.J3 = &n
			return true
		}
	case "itime":
		if n, err := strconv.Atoi(val); err == nil {
			awg.Itime = &n
			return true
		}
	case "rekeytimeout":
		if n, err := strconv.Atoi(val); err == nil {
			awg.RekeyTimeout = &n
			return true
		}
	case "rejectaftertime":
		if n, err := strconv.Atoi(val); err == nil {
			awg.RejectAfterTime = &n
			return true
		}
	case "keepalivetimeout":
		if n, err := strconv.Atoi(val); err == nil {
			awg.KeepaliveTimeout = &n
			return true
		}
	case "maxhandshakeattempts":
		if n, err := strconv.Atoi(val); err == nil {
			awg.MaxHandshakeAttempts = &n
			return true
		}
	}
	return false
}

// parseAWGOptionsFromMap восстанавливает AWGOptions из универсальной карты параметров.
func parseAWGOptionsFromMap(m map[string]interface{}) *AWGOptions {
	if m == nil {
		return nil
	}
	opts := &AWGOptions{
		RawOptions: make(map[string]interface{}),
	}
	for k, v := range m {
		valStr := fmt.Sprintf("%v", v)
		if !parseAWGField(opts, k, valStr) {
			opts.RawOptions[k] = v
		}
	}
	if opts.IsEmpty() {
		return nil
	}
	return opts
}

type parsedWgInterface struct {
	privateKey string
	addresses  []string
	dns        []string
	mtu        int
	awg        *AWGOptions
}

type parsedWgPeer struct {
	publicKey    string
	presharedKey string
	endpoint     string
	allowedIPs   []string
	keepAlive    int
	awg          *AWGOptions
	comment      string
}

// parseWgQuickConf выполняет построчный разбор конфигурации формата wg-quick .conf (INI).
// Поддерживает секции [Interface] и [Peer], параметры чистого WireGuard и все 13 полей AmneziaWG 3.1.
func parseWgQuickConf(content string, tagPrefix string) ([]SubscriptionNode, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	currentSection := "" // "interface", "peer"
	var iface parsedWgInterface
	iface.awg = &AWGOptions{
		RawOptions: make(map[string]interface{}),
	}
	var peers []parsedWgPeer
	var currentPeer *parsedWgPeer
	lastComment := ""

	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		// Комментарии всей строки
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			commentText := strings.TrimSpace(strings.TrimLeft(line, "#; "))
			if currentSection == "peer" && currentPeer != nil && currentPeer.comment == "" && currentPeer.publicKey == "" && currentPeer.endpoint == "" {
				currentPeer.comment = commentText
				lastComment = ""
			} else {
				lastComment = commentText
			}
			continue
		}

		// Заголовок секции [Interface] / [Peer]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			secName := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if secName == "interface" {
				currentSection = "interface"
				lastComment = ""
			} else if secName == "peer" {
				currentSection = "peer"
				if currentPeer != nil {
					peers = append(peers, *currentPeer)
				}
				currentPeer = &parsedWgPeer{
					comment: lastComment,
					awg:     &AWGOptions{RawOptions: make(map[string]interface{})},
				}
				lastComment = ""
			} else {
				currentSection = secName
				lastComment = ""
			}
			continue
		}

		// Разбор пары Ключ = Значение
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if currentSection == "interface" {
			lastComment = ""
		}

		// Очистка строчных инлайн-комментариев (только если предваряются пробелом или табом)
		if idx := strings.Index(val, " #"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		} else if idx := strings.Index(val, " ;"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		} else if idx := strings.Index(val, "\t#"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		} else if idx := strings.Index(val, "\t;"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		}

		lowerKey := strings.ToLower(key)

		switch currentSection {
		case "interface":
			switch lowerKey {
			case "privatekey", "secretkey":
				iface.privateKey = val
			case "address":
				for _, a := range strings.Split(val, ",") {
					if clean := strings.TrimSpace(a); clean != "" {
						iface.addresses = append(iface.addresses, clean)
					}
				}
			case "dns":
				for _, d := range strings.Split(val, ",") {
					if clean := strings.TrimSpace(d); clean != "" {
						iface.dns = append(iface.dns, clean)
					}
				}
			case "mtu":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					iface.mtu = n
				}
			default:
				if !parseAWGField(iface.awg, key, val) {
					// Неизвестная/пользовательская опция сохраняется в RawOptions (AWGIN-03)
					iface.awg.RawOptions[key] = val
				}
			}

		case "peer":
			if currentPeer == nil {
				currentPeer = &parsedWgPeer{
					awg: &AWGOptions{RawOptions: make(map[string]interface{})},
				}
			}
			switch lowerKey {
			case "publickey":
				currentPeer.publicKey = val
			case "presharedkey":
				currentPeer.presharedKey = val
			case "endpoint":
				currentPeer.endpoint = val
			case "allowedips":
				for _, ip := range strings.Split(val, ",") {
					if clean := strings.TrimSpace(ip); clean != "" {
						currentPeer.allowedIPs = append(currentPeer.allowedIPs, clean)
					}
				}
			case "persistentkeepalive", "keepalive":
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					currentPeer.keepAlive = n
				}
			default:
				if !parseAWGField(currentPeer.awg, key, val) {
					currentPeer.awg.RawOptions[key] = val
				}
			}
		}
	}

	if currentPeer != nil {
		peers = append(peers, *currentPeer)
	}

	if iface.privateKey == "" {
		return nil, fmt.Errorf("wg-quick conf: missing PrivateKey in [Interface]")
	}
	if !isValidWireguardKey(iface.privateKey) {
		return nil, fmt.Errorf("wg-quick conf: invalid PrivateKey: expected 32-byte base64")
	}

	if len(peers) == 0 {
		return nil, fmt.Errorf("wg-quick conf: no [Peer] section found")
	}

	// Нормализация локальных адресов
	var normAddresses []string
	for _, a := range iface.addresses {
		if norm := normalizeWireguardLocalAddress(a); norm != "" {
			normAddresses = append(normAddresses, norm)
		}
	}

	nodes := make([]SubscriptionNode, 0, len(peers))
	for i, peer := range peers {
		if peer.endpoint == "" {
			return nil, fmt.Errorf("wg-quick conf: peer #%d is missing Endpoint", i+1)
		}
		if peer.publicKey == "" {
			return nil, fmt.Errorf("wg-quick conf: peer #%d is missing PublicKey", i+1)
		}
		if !isValidWireguardKey(peer.publicKey) {
			return nil, fmt.Errorf("wg-quick conf: peer #%d has invalid PublicKey", i+1)
		}

		var tag string
		if peer.comment != "" {
			tag = peer.comment
		} else if tagPrefix != "" {
			if len(peers) == 1 {
				tag = tagPrefix
			} else {
				tag = fmt.Sprintf("%s-%d", tagPrefix, i+1)
			}
		} else {
			host, _, err := net.SplitHostPort(peer.endpoint)
			if err == nil && host != "" {
				if len(peers) == 1 {
					tag = fmt.Sprintf("WireGuard-%s", host)
				} else {
					tag = fmt.Sprintf("WireGuard-%s-%d", host, i+1)
				}
			} else {
				if len(peers) == 1 {
					tag = "WireGuard"
				} else {
					tag = fmt.Sprintf("WireGuard-%d", i+1)
				}
			}
		}

		// Слияние AWG настроек интерфейса и пира
		var nodeAWG *AWGOptions
		if iface.awg != nil && !iface.awg.IsEmpty() {
			nodeAWG = iface.awg.Clone()
		}
		if peer.awg != nil && !peer.awg.IsEmpty() {
			if nodeAWG == nil {
				nodeAWG = peer.awg.Clone()
			} else {
				// Применение переопределений пира
				peerClone := peer.awg.Clone()
				if peerClone.Jc != nil {
					nodeAWG.Jc = peerClone.Jc
				}
				if peerClone.Jmin != nil {
					nodeAWG.Jmin = peerClone.Jmin
				}
				if peerClone.Jmax != nil {
					nodeAWG.Jmax = peerClone.Jmax
				}
				if peerClone.S1 != nil {
					nodeAWG.S1 = peerClone.S1
				}
				if peerClone.S2 != nil {
					nodeAWG.S2 = peerClone.S2
				}
				if peerClone.S3 != nil {
					nodeAWG.S3 = peerClone.S3
				}
				if peerClone.S4 != nil {
					nodeAWG.S4 = peerClone.S4
				}
				if peerClone.H1 != "" {
					nodeAWG.H1 = peerClone.H1
				}
				if peerClone.H2 != "" {
					nodeAWG.H2 = peerClone.H2
				}
				if peerClone.H3 != "" {
					nodeAWG.H3 = peerClone.H3
				}
				if peerClone.H4 != "" {
					nodeAWG.H4 = peerClone.H4
				}
				if peerClone.I1 != "" {
					nodeAWG.I1 = peerClone.I1
				}
				if peerClone.I2 != "" {
					nodeAWG.I2 = peerClone.I2
				}
				if peerClone.I3 != "" {
					nodeAWG.I3 = peerClone.I3
				}
				if peerClone.I4 != "" {
					nodeAWG.I4 = peerClone.I4
				}
				if peerClone.I5 != "" {
					nodeAWG.I5 = peerClone.I5
				}
				if peerClone.HeaderProtectionKey != "" {
					nodeAWG.HeaderProtectionKey = peerClone.HeaderProtectionKey
				}
				if peerClone.Version != "" {
					nodeAWG.Version = peerClone.Version
				}
				if peerClone.ContentPaddingAddition != nil {
					nodeAWG.ContentPaddingAddition = peerClone.ContentPaddingAddition
				}
				if peerClone.RandomTrailers != nil {
					nodeAWG.RandomTrailers = peerClone.RandomTrailers
				}
				if peerClone.DisableCookies != nil {
					nodeAWG.DisableCookies = peerClone.DisableCookies
				}
				if peerClone.RekeyAfterTime != nil {
					nodeAWG.RekeyAfterTime = peerClone.RekeyAfterTime
				}
				if peerClone.J1 != nil {
					nodeAWG.J1 = peerClone.J1
				}
				if peerClone.J2 != nil {
					nodeAWG.J2 = peerClone.J2
				}
				if peerClone.J3 != nil {
					nodeAWG.J3 = peerClone.J3
				}
				if peerClone.Itime != nil {
					nodeAWG.Itime = peerClone.Itime
				}
				if peerClone.RekeyTimeout != nil {
					nodeAWG.RekeyTimeout = peerClone.RekeyTimeout
				}
				if peerClone.RejectAfterTime != nil {
					nodeAWG.RejectAfterTime = peerClone.RejectAfterTime
				}
				if peerClone.KeepaliveTimeout != nil {
					nodeAWG.KeepaliveTimeout = peerClone.KeepaliveTimeout
				}
				if peerClone.MaxHandshakeAttempts != nil {
					nodeAWG.MaxHandshakeAttempts = peerClone.MaxHandshakeAttempts
				}
				for rk, rv := range peerClone.RawOptions {
					nodeAWG.RawOptions[rk] = rv
				}
			}
		}

		if nodeAWG != nil && nodeAWG.IsEmpty() {
			nodeAWG = nil
		}

		node := SubscriptionNode{
			Tag:            tag,
			Name:           peer.comment,
			Protocol:       "wireguard",
			Server:         peer.endpoint,
			SecretKey:      iface.privateKey,
			PublicKey:      peer.publicKey,
			PreSharedKey:   peer.presharedKey,
			LocalAddresses: normAddresses,
			DNS:            iface.dns,
			MTU:            iface.mtu,
			AllowedIPs:     peer.allowedIPs,
			KeepAlive:      peer.keepAlive,
			AWG:            nodeAWG,
		}
		node.Dialect = string(DetectWireGuardDialect(&node))
		InferAWGVersion(&node)

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// parseWgQuickConfToOutbounds разбирает конфигурацию wg-quick .conf в слайс Outbound для общего пайплайна.
func parseWgQuickConfToOutbounds(content string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	tagPrefix := ""
	if sub != nil && sub.TagPrefix != "" {
		tagPrefix = sub.TagPrefix
	}
	nodes, err := parseWgQuickConf(content, tagPrefix)
	if err != nil {
		return nil, nil, err
	}
	outbounds := make([]Outbound, 0, len(nodes))
	for i := range nodes {
		ob, reason := wireguardNodeToOutbound(&nodes[i])
		if ob == nil {
			return nil, nil, fmt.Errorf("invalid wireguard peer #%d: %s", i+1, reason)
		}
		outbounds = append(outbounds, *ob)
	}
	return outbounds, nil, nil
}
