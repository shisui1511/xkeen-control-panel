package nodes

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
	PinSHA256   []string
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

// Identity — протокол|адрес|порт|Hash8(учётные данные) без источника.
func (n Node) Identity() string { return "" }

// Key — src + "|" + Identity().
func (n Node) Key(src string) string { return "" }

// Hash8 — первые 8 hex sha256.
func Hash8(s string) string { return "" }

// XrayTag — тег Xray узла подписки.
func XrayTag(src, key string) string { return "" }

// Ref — ссылка на узел для групп.
func Ref(src, id8 string) string { return "" }

// Uniq — уникальные ключи узлов источника.
func Uniq(src string, list []Node) (out []Keyed, collapsed int) { return nil, 0 }
