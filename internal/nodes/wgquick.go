package nodes

import (
	"bufio"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// wgQuickOnly содержит ключи интерфейса, которые не относятся к туннелю клиента.
// Их наличие не превращает обычный WireGuard в AmneziaWG.
var wgQuickOnly = map[string]bool{
	"table": true, "preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true, "listenport": true, "fwmark": true,
}

var wgQuickClashKey = regexp.MustCompile(`(?m)^(?:proxies|proxy-providers):`)

// ParseWgQuickConf разбирает один блок [Interface] с его секциями [Peer].
// Для нескольких интерфейсов и диагностики по строкам используется ParseWG.
func ParseWgQuickConf(content string) ([]Node, error) {
	peers, err := parseWgQuickPeers(content)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(peers))
	for i, peer := range peers {
		host, portText, err := net.SplitHostPort(peer.Endpoint)
		port, portErr := strconv.Atoi(portText)
		if err != nil || portErr != nil || host == "" || port < 1 || port > 65535 {
			return nil, fmt.Errorf("wg-quick conf: peer #%d has invalid Endpoint", i+1)
		}
		wg := &WireGuard{
			PrivateKey:    peer.PrivateKey,
			PeerPublicKey: peer.PublicKey,
			PreSharedKey:  peer.PreSharedKey,
			Address:       peer.Address,
			DNS:           peer.DNS,
			MTU:           peer.MTU,
			AllowedIPs:    peer.AllowedIPs,
			KeepAlive:     peer.KeepAlive,
			AWGVersion:    InferAWGVersion(peer.AWG),
		}
		if !peer.AWG.IsEmpty() {
			wg.AWG = awgOptionsMap(peer.AWG, wg.AWGVersion)
		}
		out = append(out, Node{
			Name:     orDefault(peer.Name, WgQuickPeerName(host, i, len(peers))),
			Protocol: "wireguard",
			Host:     host,
			Port:     port,
			WG:       wg,
			Origin:   OriginWGConf,
			Text:     content,
			Format:   "wg",
		})
	}
	return out, nil
}

// WgQuickPeerName строит имя для пира без комментария.
func WgQuickPeerName(host string, index, total int) string {
	name := "WireGuard"
	if host != "" {
		name += "-" + host
	}
	if total > 1 {
		name += "-" + strconv.Itoa(index+1)
	}
	return name
}

// WgQuickPeer содержит параметры пира, включая общие настройки интерфейса.
type WgQuickPeer struct {
	Name         string
	Endpoint     string
	PrivateKey   string
	PublicKey    string
	PreSharedKey string
	Address      []string
	DNS          []string
	MTU          int
	AllowedIPs   []string
	KeepAlive    int
	AWG          *AWGOptions
}

func validWgQuickKey(key string) bool {
	b, err := decodeBase64Any(key)
	return err == nil && len(b) == 32
}

// LooksLikeWgQuickConf возвращает true, если содержимое напоминает wg-quick .conf конфигурацию.
func LooksLikeWgQuickConf(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") {
		return false
	}
	if !strings.HasPrefix(trimmed, "[") && wgQuickClashKey.MatchString(trimmed) {
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

// AWGOptions содержит параметры обфускации протокола AmneziaWG (1.0/1.5/2.0/3.1).
type AWGOptions struct {
	Jc                     *int   `json:"jc,omitempty"`
	Jmin                   *int   `json:"jmin,omitempty"`
	Jmax                   *int   `json:"jmax,omitempty"`
	S1                     *int   `json:"s1,omitempty"`
	S2                     *int   `json:"s2,omitempty"`
	S3                     *int   `json:"s3,omitempty"`
	S4                     *int   `json:"s4,omitempty"`
	H1                     string `json:"h1,omitempty"` // число или min-max
	H2                     string `json:"h2,omitempty"`
	H3                     string `json:"h3,omitempty"`
	H4                     string `json:"h4,omitempty"`
	Version                string `json:"version,omitempty"`
	HeaderProtectionKey    string `json:"header_protection_key,omitempty"`
	I1                     string `json:"i1,omitempty"` // uppercase
	I2                     string `json:"i2,omitempty"`
	I3                     string `json:"i3,omitempty"`
	I4                     string `json:"i4,omitempty"`
	I5                     string `json:"i5,omitempty"`
	ContentPaddingAddition *int   `json:"content_padding_addition,omitempty"`
	RandomTrailers         *bool  `json:"random_trailers,omitempty"`
	DisableCookies         *bool  `json:"disable_cookies,omitempty"`
	RekeyAfterTime         *int   `json:"rekey_after_time,omitempty"`
	// AWG 1.5 fields
	J1    *int `json:"j1,omitempty"`
	J2    *int `json:"j2,omitempty"`
	J3    *int `json:"j3,omitempty"`
	Itime *int `json:"itime,omitempty"`
	// AWG 3.1 timing fields
	RekeyTimeout         *int                   `json:"rekey_timeout,omitempty"`
	RejectAfterTime      *int                   `json:"reject_after_time,omitempty"`
	KeepaliveTimeout     *int                   `json:"keepalive_timeout,omitempty"`
	MaxHandshakeAttempts *int                   `json:"max_handshake_attempts,omitempty"`
	RawOptions           map[string]interface{} `json:"raw_options,omitempty"`
}

// Clone возвращает глубокую копию AWGOptions.
func (o *AWGOptions) Clone() *AWGOptions {
	if o == nil {
		return nil
	}
	res := *o
	if o.Jc != nil {
		v := *o.Jc
		res.Jc = &v
	}
	if o.Jmin != nil {
		v := *o.Jmin
		res.Jmin = &v
	}
	if o.Jmax != nil {
		v := *o.Jmax
		res.Jmax = &v
	}
	if o.S1 != nil {
		v := *o.S1
		res.S1 = &v
	}
	if o.S2 != nil {
		v := *o.S2
		res.S2 = &v
	}
	if o.S3 != nil {
		v := *o.S3
		res.S3 = &v
	}
	if o.S4 != nil {
		v := *o.S4
		res.S4 = &v
	}
	if o.ContentPaddingAddition != nil {
		v := *o.ContentPaddingAddition
		res.ContentPaddingAddition = &v
	}
	if o.RandomTrailers != nil {
		v := *o.RandomTrailers
		res.RandomTrailers = &v
	}
	if o.DisableCookies != nil {
		v := *o.DisableCookies
		res.DisableCookies = &v
	}
	if o.RekeyAfterTime != nil {
		v := *o.RekeyAfterTime
		res.RekeyAfterTime = &v
	}
	if o.J1 != nil {
		v := *o.J1
		res.J1 = &v
	}
	if o.J2 != nil {
		v := *o.J2
		res.J2 = &v
	}
	if o.J3 != nil {
		v := *o.J3
		res.J3 = &v
	}
	if o.Itime != nil {
		v := *o.Itime
		res.Itime = &v
	}
	if o.RekeyTimeout != nil {
		v := *o.RekeyTimeout
		res.RekeyTimeout = &v
	}
	if o.RejectAfterTime != nil {
		v := *o.RejectAfterTime
		res.RejectAfterTime = &v
	}
	if o.KeepaliveTimeout != nil {
		v := *o.KeepaliveTimeout
		res.KeepaliveTimeout = &v
	}
	if o.MaxHandshakeAttempts != nil {
		v := *o.MaxHandshakeAttempts
		res.MaxHandshakeAttempts = &v
	}
	if o.RawOptions != nil {
		res.RawOptions = make(map[string]interface{}, len(o.RawOptions))
		for k, v := range o.RawOptions {
			res.RawOptions[k] = v
		}
	}
	return &res
}

// IsEmpty проверяет, задан ли хотя бы один параметр обфускации AWG.
func (o *AWGOptions) IsEmpty() bool {
	if o == nil {
		return true
	}
	return o.Jc == nil && o.Jmin == nil && o.Jmax == nil &&
		o.S1 == nil && o.S2 == nil && o.S3 == nil && o.S4 == nil &&
		o.H1 == "" && o.H2 == "" && o.H3 == "" && o.H4 == "" &&
		o.Version == "" && o.HeaderProtectionKey == "" &&
		o.I1 == "" && o.I2 == "" && o.I3 == "" && o.I4 == "" && o.I5 == "" &&
		o.ContentPaddingAddition == nil && o.RandomTrailers == nil &&
		o.DisableCookies == nil && o.RekeyAfterTime == nil &&
		o.J1 == nil && o.J2 == nil && o.J3 == nil && o.Itime == nil &&
		o.RekeyTimeout == nil && o.RejectAfterTime == nil &&
		o.KeepaliveTimeout == nil && o.MaxHandshakeAttempts == nil &&
		len(o.RawOptions) == 0
}

// WireGuardDialect представляет тип/диалект конфигурации WireGuard/AmneziaWG.
type WireGuardDialect string

const (
	DialectPlain   WireGuardDialect = "plain"
	DialectClassic WireGuardDialect = "classic"
	Dialect15      WireGuardDialect = "1.5"
	Dialect20      WireGuardDialect = "2.0"
	Dialect31      WireGuardDialect = "3.1"
)

// DetectWireGuardDialect определяет диалект WireGuard/AmneziaWG узла:
// - "plain": чистый WireGuard без параметров обфускации;
// - "classic": AmneziaWG 1.0 (Jc, Jmin, Jmax, S1, S2, H1-H4);
// - "1.5": AmneziaWG 1.5 (J1, J2, J3, Itime);
// - "2.0": AmneziaWG 2.0 (наличие S3 или S4);
// - "3.1": AmneziaWG 3.1 (Version, HeaderProtectionKey, I1-I5, ContentPaddingAddition, RandomTrailers, DisableCookies, RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts или RawOptions).
func DetectWireGuardDialect(awg *AWGOptions) WireGuardDialect {
	if awg.IsEmpty() {
		return DialectPlain
	}

	// Явная версия в AWGOptions (если указана)
	ver := strings.ToLower(strings.TrimSpace(awg.Version))
	if strings.HasPrefix(ver, "3") || strings.HasPrefix(ver, "v3") {
		return Dialect31
	}
	if strings.HasPrefix(ver, "2") || strings.HasPrefix(ver, "v2") {
		return Dialect20
	}
	if strings.HasPrefix(ver, "1.5") || strings.HasPrefix(ver, "v1.5") {
		return Dialect15
	}
	if strings.HasPrefix(ver, "1") || strings.HasPrefix(ver, "v1") {
		return DialectClassic
	}

	// 1. Проверка параметров диалекта 3.1 (наивысший приоритет)
	if awg.Version != "" ||
		awg.HeaderProtectionKey != "" ||
		awg.I1 != "" || awg.I2 != "" || awg.I3 != "" || awg.I4 != "" || awg.I5 != "" ||
		awg.ContentPaddingAddition != nil ||
		awg.RandomTrailers != nil ||
		awg.DisableCookies != nil ||
		awg.RekeyAfterTime != nil ||
		awg.RekeyTimeout != nil ||
		awg.RejectAfterTime != nil ||
		awg.KeepaliveTimeout != nil ||
		awg.MaxHandshakeAttempts != nil ||
		len(awg.RawOptions) > 0 {
		return Dialect31
	}

	// 2. Проверка параметров диалекта 2.0 (S3, S4)
	if awg.S3 != nil || awg.S4 != nil {
		return Dialect20
	}

	// 3. Проверка параметров диалекта 1.5 (J1, J2, J3, Itime)
	if awg.J1 != nil || awg.J2 != nil || awg.J3 != nil || awg.Itime != nil {
		return Dialect15
	}

	// 4. Проверка параметров диалекта Classic (1.0)
	if awg.Jc != nil || awg.Jmin != nil || awg.Jmax != nil ||
		awg.S1 != nil || awg.S2 != nil ||
		awg.H1 != "" || awg.H2 != "" || awg.H3 != "" || awg.H4 != "" {
		return DialectClassic
	}

	return DialectPlain
}

// InferAWGVersion возвращает заданную или определённую по опциям версию AWG.
func InferAWGVersion(awg *AWGOptions) string {
	if awg.IsEmpty() {
		return ""
	}
	if awg.Version != "" {
		return awg.Version
	}
	switch DetectWireGuardDialect(awg) {
	case Dialect31:
		return "3.1"
	case Dialect20:
		return "2.0"
	case Dialect15:
		return "1.5"
	case DialectClassic:
		return "1.0"
	default:
		return ""
	}
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

// NormalizeAWGInitPacket нормализует значение init packet (I1..I5).
// Если значение содержит CPS-теги генератора (<c>, <b 0x...>, <r N>, <t>), сохраняет исходный регистр,
// иначе приводит hex-строку к верхнему регистру.
func NormalizeAWGInitPacket(val string) string {
	trimmed := strings.TrimSpace(val)
	if strings.Contains(trimmed, "<") {
		return trimmed
	}
	return strings.ToUpper(trimmed)
}

// Set разбирает известный параметр AWG и сообщает, удалось ли его применить.
func (awg *AWGOptions) Set(key, val string) bool {
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
		awg.I1 = NormalizeAWGInitPacket(val)
		return true
	case "i2":
		awg.I2 = NormalizeAWGInitPacket(val)
		return true
	case "i3":
		awg.I3 = NormalizeAWGInitPacket(val)
		return true
	case "i4":
		awg.I4 = NormalizeAWGInitPacket(val)
		return true
	case "i5":
		awg.I5 = NormalizeAWGInitPacket(val)
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

// normalizeWgQuickAddress ensures local CIDR mask (/32 for IPv4, /128 for IPv6).
func normalizeWgQuickAddress(addr string) string {
	clean := strings.TrimSpace(addr)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "/") {
		return clean
	}
	ip := net.ParseIP(clean)
	if ip == nil {
		return clean
	}
	if ip.To4() != nil {
		return clean + "/32"
	}
	return clean + "/128"
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

// parseWgQuickPeers разбирает пиры wg-quick; ключи интерфейса из wgQuickOnly
// к туннелю клиента не относятся и пропускаются.
func parseWgQuickPeers(content string) ([]WgQuickPeer, error) {
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
		lowerKey := strings.ToLower(key)

		if currentSection == "interface" {
			if wgQuickOnly[lowerKey] {
				continue
			}
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
				if !iface.awg.Set(key, val) {
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
				if !currentPeer.awg.Set(key, val) {
					currentPeer.awg.RawOptions[key] = val
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("wg-quick conf: read error: %w", err)
	}

	if currentPeer != nil {
		peers = append(peers, *currentPeer)
	}

	if iface.privateKey == "" {
		return nil, fmt.Errorf("wg-quick conf: missing PrivateKey in [Interface]")
	}
	if !validWgQuickKey(iface.privateKey) {
		return nil, fmt.Errorf("wg-quick conf: invalid PrivateKey: expected 32-byte base64")
	}

	if len(peers) == 0 {
		return nil, fmt.Errorf("wg-quick conf: no [Peer] section found")
	}

	// Нормализация локальных адресов
	var normAddresses []string
	for _, a := range iface.addresses {
		if norm := normalizeWgQuickAddress(a); norm != "" {
			normAddresses = append(normAddresses, norm)
		}
	}

	out := make([]WgQuickPeer, 0, len(peers))
	for i, peer := range peers {
		if peer.endpoint == "" {
			return nil, fmt.Errorf("wg-quick conf: peer #%d is missing Endpoint", i+1)
		}
		if peer.publicKey == "" {
			return nil, fmt.Errorf("wg-quick conf: peer #%d is missing PublicKey", i+1)
		}
		if !validWgQuickKey(peer.publicKey) {
			return nil, fmt.Errorf("wg-quick conf: peer #%d has invalid PublicKey", i+1)
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

		out = append(out, WgQuickPeer{
			Name:         peer.comment,
			Endpoint:     peer.endpoint,
			PrivateKey:   iface.privateKey,
			PublicKey:    peer.publicKey,
			PreSharedKey: peer.presharedKey,
			Address:      normAddresses,
			DNS:          iface.dns,
			MTU:          iface.mtu,
			AllowedIPs:   peer.allowedIPs,
			KeepAlive:    peer.keepAlive,
			AWG:          nodeAWG,
		})
	}
	return out, nil
}

// awgOptionsMap переводит опции AmneziaWG в карту с ключами Mihomo
// (amnezia-wg-option): только заданные поля.
func awgOptionsMap(o *AWGOptions, version string) map[string]any {
	m := map[string]any{}
	ints := map[string]*int{
		"jc": o.Jc, "jmin": o.Jmin, "jmax": o.Jmax, "s1": o.S1, "s2": o.S2, "s3": o.S3, "s4": o.S4,
		"j1": o.J1, "j2": o.J2, "j3": o.J3, "itime": o.Itime,
		"content-padding-addition": o.ContentPaddingAddition, "rekey-after-time": o.RekeyAfterTime,
		"rekey-timeout": o.RekeyTimeout, "reject-after-time": o.RejectAfterTime,
		"keepalive-timeout": o.KeepaliveTimeout, "max-handshake-attempts": o.MaxHandshakeAttempts,
	}
	for k, p := range ints {
		if p != nil {
			m[k] = *p
		}
	}
	strs := map[string]string{
		"h1": o.H1, "h2": o.H2, "h3": o.H3, "h4": o.H4,
		"i1": o.I1, "i2": o.I2, "i3": o.I3, "i4": o.I4, "i5": o.I5,
		"version": version, "header-protection-key": o.HeaderProtectionKey,
	}
	for k, v := range strs {
		if v != "" {
			m[k] = v
		}
	}
	if o.RandomTrailers != nil {
		m["random-trailers"] = *o.RandomTrailers
	}
	if o.DisableCookies != nil {
		m["disable-cookies"] = *o.DisableCookies
	}
	for k, v := range o.RawOptions {
		m[strings.ToLower(k)] = v
	}
	return m
}
