package cores

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"golang.org/x/net/idna"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Модель секций Mihomo (MIHO-01): какие ключи верхнего уровня профиля к какой
// секции панели относятся, пресеты из встроенной базы и закреплённые значения.

// Идентификаторы секций Mihomo внутри ядра.
const (
	mihomoMain     = "main"
	mihomoDNS      = "dns"
	mihomoInbounds = "inbounds"
	mihomoSniffer  = "sniffer"
	mihomoOther    = "other"
)

// Значения встроенной базы Mihomo: порты redir и tproxy и метка сокетов панели.
// Порты повторяют неэкспортируемые константы configlayer (takeover.go): при смене
// значений менять в обоих местах.
const (
	mihomoDefaultRedirPort  = 5000
	mihomoDefaultTProxyPort = 5001
	mihomoPanelRoutingMark  = 255
)

// MihomoSpec описывает секцию Mihomo: идентификатор, имя секции черновика и
// ключи верхнего уровня профиля, которые в неё входят. Single — значение
// секции равно значению одного ключа (dns, sniffer), а не объекту ключей.
type MihomoSpec struct {
	ID      string
	Section string
	Keys    []string
	Single  bool
}

// MihomoFormKeys — ключи, которые показывает форма секции; остальное в секции
// доступно через «сырой» блок. other формы не имеет.
var MihomoFormKeys = map[string][]string{
	mihomoMain: {"mixed-port", "port", "socks-port", "redir-port", "tproxy-port", "allow-lan", "bind-address", "mode", "log-level", "ipv6"},
	mihomoDNS: {
		"enable", "enhanced-mode", "ipv6", "listen", "nameserver", "default-nameserver",
		"proxy-server-nameserver", "fake-ip-range", "fake-ip-filter",
	},
	mihomoInbounds: {
		"listeners[].name", "listeners[].type", "listeners[].listen", "listeners[].port",
		"tun.enable", "tun.stack", "tun.auto-route",
	},
	mihomoSniffer: {"enable", "force-dns-mapping", "parse-pure-ip", "override-destination"},
	mihomoOther:   {},
}

// MihomoSpecs — секции Mihomo в порядке показа. Ключи секции «Прочие ключи» не
// перечисляются: в неё идёт всё, что не относится к остальным секциям и к
// маршрутизации.
var MihomoSpecs = []MihomoSpec{
	{ID: mihomoMain, Section: configlayer.SectionMihomoMain, Keys: append(append([]string{}, MihomoFormKeys[mihomoMain]...), "profile")},
	{ID: mihomoDNS, Section: configlayer.SectionMihomoDNS, Keys: []string{"dns"}, Single: true},
	{ID: mihomoInbounds, Section: configlayer.SectionMihomoInbounds, Keys: []string{"listeners", "tun"}},
	{ID: mihomoSniffer, Section: configlayer.SectionMihomoSniffer, Keys: []string{"sniffer"}, Single: true},
	{ID: mihomoOther, Section: configlayer.SectionMihomoOther},
}

// mihomoRoutingKeys — маршрутизация: её ведёт генератор профиля, в секции
// ядра эти ключи не входят.
var mihomoRoutingKeys = map[string]bool{
	"proxies":         true,
	"proxy-groups":    true,
	"rules":           true,
	"proxy-providers": true,
	"rule-providers":  true,
}

// mihomoPinnedTop — закреплённые ключи верхнего уровня: панель пишет их сама.
var mihomoPinnedTop = map[string]bool{
	"routing-mark":             true,
	"external-controller-unix": true,
}

// mihomoMainKeys — ключи основного списка секции «Основное».
var mihomoMainKeys = func() map[string]bool {
	out := map[string]bool{}
	for _, k := range MihomoFormKeys[mihomoMain] {
		out[k] = true
	}
	return out
}()

// mihomoOwnKeys — ключи верхнего уровня, принадлежащие собственным секциям
// (кроме «Прочих ключей»): в «Прочие ключи» они не входят.
var mihomoOwnKeys = func() map[string]bool {
	out := map[string]bool{"profile": true}
	for k := range mihomoMainKeys {
		out[k] = true
	}
	for _, sp := range MihomoSpecs {
		if sp.ID == mihomoOther {
			continue
		}
		for _, k := range sp.Keys {
			out[k] = true
		}
	}
	return out
}()

// mihomoSectionOfKey возвращает секцию для ключа верхнего уровня профиля: пустая
// строка — ключ не переносится (маршрутизация и закреплённое).
func mihomoSectionOfKey(key string) string {
	if mihomoRoutingKeys[key] || mihomoPinnedTop[key] {
		return ""
	}
	switch {
	case key == "profile" || mihomoMainKeys[key]:
		return mihomoMain
	case key == "dns":
		return mihomoDNS
	case key == "sniffer":
		return mihomoSniffer
	case key == "listeners" || key == "tun":
		return mihomoInbounds
	}
	return mihomoOther
}

// mihomoJSON кодирует значение в JSON без HTML-экранирования.
func mihomoJSON(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
}

// mihomoObject собирает упорядоченный объект из пар «ключ, значение».
func mihomoObject(pairs ...any) *configlayer.OrderedObject {
	o := configlayer.NewOrderedObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), mihomoJSON(pairs[i+1]))
	}
	return o
}

// mihomoObjectRaw — объект как json.RawMessage.
func mihomoObjectRaw(o *configlayer.OrderedObject) json.RawMessage {
	b, err := o.MarshalJSON()
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// MihomoPreset возвращает значение секции из встроенной базы профиля. Порты
// прозрачного прокси и доступ из LAN берутся из ports (≤ 0 — умолчания базы).
func MihomoPreset(id string, ports configlayer.MihomoPorts) json.RawMessage {
	switch id {
	case mihomoMain:
		redir, tproxy := ports.Redir, ports.TProxy
		if redir <= 0 {
			redir = mihomoDefaultRedirPort
		}
		if tproxy <= 0 {
			tproxy = mihomoDefaultTProxyPort
		}
		allowLAN := true
		if ports.AllowLAN != nil {
			allowLAN = *ports.AllowLAN
		}
		bind := ports.BindAddress
		if bind == "" {
			bind = "*"
		}
		return mihomoObjectRaw(mihomoObject(
			"redir-port", redir,
			"tproxy-port", tproxy,
			"allow-lan", allowLAN,
			"bind-address", bind,
			"mode", "rule",
		))
	case mihomoSniffer:
		sniffPorts := func(p ...string) json.RawMessage { return mihomoObjectRaw(mihomoObject("ports", p)) }
		sniff := configlayer.NewOrderedObject()
		sniff.Set("HTTP", sniffPorts("80", "8080-8880"))
		sniff.Set("TLS", sniffPorts("443", "8443"))
		sniff.Set("QUIC", sniffPorts("443", "8443"))
		o := mihomoObject(
			"enable", true,
			"force-dns-mapping", true,
			"parse-pure-ip", true,
			"override-destination", false,
		)
		o.Set("sniff", mihomoObjectRaw(sniff))
		return mihomoObjectRaw(o)
	case mihomoDNS:
		return mihomoObjectRaw(mihomoObject(
			"enable", true,
			"ipv6", false,
			"nameserver", []string{"system"},
		))
	}
	return json.RawMessage("{}")
}

// MihomoPinned возвращает закреплённые значения секции: панель дописывает их
// поверх значения секции, править их нельзя (D-10).
func MihomoPinned(id string) []configlayer.PinnedValue {
	if id != mihomoMain {
		return nil
	}
	return []configlayer.PinnedValue{
		{Key: "routing-mark", Value: strconv.Itoa(mihomoPanelRoutingMark)},
		{Key: "external-controller-unix", Value: services.DefaultMihomoSocketPath},
		{Key: "profile.store-selected", Value: "true"},
	}
}

// Значения части dns профиля, которые генератор маршрутизации ставит под
// перехватом DNS на Mihomo (routing/dns_mihomo.go; пакет routing импортирует
// cores, обратный импорт невозможен): менять только вместе.
const (
	mihomoDNSListen4     = "0.0.0.0:53"
	mihomoDNSListen6     = "[::]:53"
	mihomoEnhancedFakeIP = "fake-ip"
)

// mihomoDNSZoneName приводит имя зоны к виду ключа Mihomo так же, как
// генератор (dnsZoneName): нижний регистр, без точки в конце, в ASCII; пусто —
// имя не годится для ключа.
func mihomoDNSZoneName(name string) string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if n == "" || strings.ContainsAny(n, " \t\r\n,:#/") {
		return ""
	}
	if ascii, err := idna.Punycode.ToASCII(n); err == nil {
		n = ascii
	}
	return n
}

// mihomoDNSZones — локальные зоны перехвата в том же составе и порядке, что у
// генератора (dnsTakeoverZones): встроенные со служебным доменом (D-11), затем
// свои зоны пользователя (D-22); недопустимые свои пропускаются, без повторов.
func mihomoDNSZones(serviceDomain string, own []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(z string) {
		if name := mihomoDNSZoneName(z); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, z := range configlayer.BuiltinLocalZones(serviceDomain) {
		add(z)
	}
	for _, z := range own {
		if configlayer.ValidZone(z) == nil {
			add(z)
		}
	}
	return out
}

// MihomoDNSTakeoverPinned возвращает закреплённые значения секции DNS Mihomo
// при включённом перехвате DNS на Mihomo (D-19, DNSF-01): listen на :53;
// nameserver-policy — JSON {"zones":N,"direct":M} с числом локальных зон и
// direct-правил, ключи которых панель пишет в политику (только при известном
// адресе резолвера прошивки: без него генератор политику не пишет); при
// enhanced-mode fake-ip — зоны, которые генератор дописывает в fake-ip-filter.
// Значения считаются из тех же живых данных и параметров, что у генератора.
func MihomoDNSTakeoverPinned(live configlayer.DNSTakeoverLive, params configlayer.DNSTakeoverParams, enhancedMode string, directSets int) []configlayer.PinnedValue {
	listen := mihomoDNSListen4
	if live.IPv6 {
		listen = mihomoDNSListen6
	}
	out := []configlayer.PinnedValue{{Key: "listen", Value: listen}}
	zones := mihomoDNSZones(live.ServiceDomain, params.Zones)
	if strings.TrimSpace(live.FirmwareResolver) != "" {
		policy := mihomoJSON(struct {
			Zones  int `json:"zones"`
			Direct int `json:"direct"`
		}{Zones: len(zones), Direct: max(directSets, 0)})
		out = append(out, configlayer.PinnedValue{Key: "nameserver-policy", Value: string(policy)})
	}
	if strings.TrimSpace(enhancedMode) == mihomoEnhancedFakeIP && len(zones) > 0 {
		filters := make([]string, 0, len(zones))
		for _, z := range zones {
			filters = append(filters, "+."+z)
		}
		out = append(out, configlayer.PinnedValue{Key: "fake-ip-filter", Value: strings.Join(filters, ", ")})
	}
	return out
}
