package nodes

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// Origin — откуда узел пришёл; от него зависит, отдавать ли ядру оригинал.
type Origin string

const (
	OriginLink     Origin = "link"
	OriginXrayJSON Origin = "xray_json"
	OriginClash    Origin = "clash"
	OriginSingbox  Origin = "singbox"
	OriginWGConf   Origin = "wg_conf"
)

// Transport — транспорт узла (network и его параметры).
type Transport struct {
	Network     string
	Path        string
	Host        string
	Headers     map[string]string
	ServiceName string
	Mode        string
	HeaderType  string
	XHTTP       map[string]any
}

// TLS — параметры TLS (в том числе для Reality: SNI, ALPN, отпечаток).
type TLS struct {
	Enabled     bool
	SNI         string
	ALPN        []string
	Fingerprint string
	Insecure    bool
	// PinSHA256 — SHA-256 всего сертификата (hex): его выражают оба ядра.
	PinSHA256 []string
	// PinPubKeySHA256 — SHA-256 открытого ключа сертификата (SPKI) в base64, как в
	// sing-box; ни Xray 26.x, ни Mihomo его не выражают. omitempty: канонический
	// JSON и хэш снимков узлов без этого поля не меняются.
	PinPubKeySHA256 []string `json:"PinPubKeySHA256,omitempty"`
}

// Reality — параметры Reality (ключи); TLS при этом тоже заполнен.
type Reality struct {
	PublicKey string
	ShortID   string
	SpiderX   string
}

// Hysteria2 — параметры hysteria2/hysteria.
type Hysteria2 struct {
	Obfs         string
	ObfsPassword string
	Up           string
	Down         string
	Ports        string
}

// TUIC — параметры tuic.
type TUIC struct {
	CongestionControl string
	UDPRelayMode      string
	ReduceRTT         bool
}

// WireGuard — параметры wireguard (в том числе AmneziaWG).
type WireGuard struct {
	PrivateKey    string
	PeerPublicKey string
	PreSharedKey  string
	Address       []string
	MTU           int
	Reserved      []int
	AllowedIPs    []string
	AWG           map[string]any
	AWGVersion    string
}

// Node — узел в промежуточном представлении.
//
// Для socks и http имя пользователя хранится в UUID, пароль — в Password.
type Node struct {
	Name           string
	Protocol       string
	Host           string
	Port           int
	UUID           string
	Password       string
	Method         string
	Security       string
	AlterID        int
	Flow           string
	Encryption     string
	Plugin         string
	PluginOpts     map[string]any
	Transport      Transport
	TLS            *TLS
	Reality        *Reality
	UDP            *bool
	PacketEncoding string
	Hy2            *Hysteria2
	TUIC           *TUIC
	WG             *WireGuard
	Origin         Origin
	Raw            map[string]any
	Text           string
	Format         string
	Notes          []string
}

// Keyed — узел с вычисленным ключом.
type Keyed struct {
	Key  string
	Node Node
}

// normProtocol приводит имя протокола к каноническому виду.
func normProtocol(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "hy2":
		return "hysteria2"
	case "ss":
		return "shadowsocks"
	case "socks5":
		return "socks"
	case "wg":
		return "wireguard"
	}
	return p
}

// Identity — протокол|адрес|порт|Hash8(учётные данные) без источника.
// Имя узла в идентичность не входит: переименование у провайдера её не меняет.
func (n Node) Identity() string {
	priv := ""
	if n.WG != nil {
		priv = n.WG.PrivateKey
	}
	return normProtocol(n.Protocol) + "|" + strings.ToLower(n.Host) + "|" + strconv.Itoa(n.Port) +
		"|" + Hash8(n.UUID+"|"+n.Password+"|"+priv)
}

// Key — src + "|" + Identity().
func (n Node) Key(src string) string { return src + "|" + n.Identity() }

// DupKey — идентичность узла и отпечаток транспорта: по нему узлы считаются
// дублями (как при схлопывании в Recognize). Узлы с одной идентичностью и
// разным транспортом (CDN-фронты) дублями не являются.
func (n Node) DupKey() string { return n.Identity() + "|" + transportFingerprint(n) }

// Hash8 — первые 8 hex от sha256.
func Hash8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// XrayTag — тег Xray узла подписки: xcp-<источник>-<8 hex от sha256(ключа)>.
func XrayTag(src, key string) string { return "xcp-" + src + "-" + Hash8(key) }

// Ref — ссылка на узел для групп: <источник>/<8 hex>.
func Ref(src, id8 string) string { return src + "/" + id8 }

// transportFingerprint — отпечаток транспорта и TLS: различает узлы с одной
// идентичностью (CDN-фронты с одним UUID и разными host/path).
func transportFingerprint(n Node) string {
	sni, pbk := "", ""
	if n.TLS != nil {
		sni = n.TLS.SNI
	}
	if n.Reality != nil {
		pbk = n.Reality.PublicKey
	}
	t := n.Transport
	return Hash8(t.Network + "|" + t.Path + "|" + t.Host + "|" + t.ServiceName + "|" + sni + "|" + pbk)
}

// Uniq присваивает узлам источника ключи. Полные дубли (та же идентичность и
// тот же отпечаток транспорта) схлопываются, их число возвращается в collapsed.
// Узлы с одной идентичностью, но разным транспортом/TLS не схлопываются: все,
// кроме первого по отпечатку, получают суффикс "~" + 6 hex отпечатка. Результат
// отсортирован по ключу и не зависит от порядка входа.
func Uniq(src string, list []Node) (out []Keyed, collapsed int) {
	type item struct {
		fp   string
		node Node
	}
	groups := map[string][]item{}
	for _, n := range list {
		id := n.Identity()
		groups[id] = append(groups[id], item{fp: transportFingerprint(n), node: n})
	}
	for _, g := range groups {
		sort.SliceStable(g, func(i, j int) bool {
			if g[i].fp != g[j].fp {
				return g[i].fp < g[j].fp
			}
			if g[i].node.Name != g[j].node.Name {
				return g[i].node.Name < g[j].node.Name
			}
			return g[i].node.Text < g[j].node.Text
		})
		prev := ""
		for i, it := range g {
			if i > 0 && it.fp == prev {
				collapsed++
				continue
			}
			key := it.node.Key(src)
			if i > 0 {
				key += "~" + it.fp[:6]
			}
			out = append(out, Keyed{Key: key, Node: it.node})
			prev = it.fp
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, collapsed
}
