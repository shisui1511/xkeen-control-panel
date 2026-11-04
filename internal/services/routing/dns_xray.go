package routing

import (
	"net"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/idna"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
)

// Перехват DNS роутера на Xray (DNSF-04): слушатель :53 (dokodemo-door без флага
// прозрачного перенаправления — XKeen такой входящий не берёт), исходящий dns,
// правила DNS в начале routing и объект dns с тегом запросов и серверами, которые
// идут мимо группы-цели. Внешние имена резолвит группа-цель (fail-closed, D-10):
// прямой выход есть только у серверов с domains и тегом TagDNSDirect, и все они
// с skipFallback — запасом для чужих имён они не становятся.

// Коды ошибок генератора перехвата DNS (начало текста ошибки).
const (
	xdnsErrGroupMissing    = "dns_group_missing"
	xdnsErrSectionRequired = "dns_section_required"
	xdnsErrResolver        = "dns_firmware_resolver_invalid"
)

// xdnsPort — порт слушателя перехвата.
const xdnsPort = 53

// xrayDNSParts — что перехват добавляет в файлы Xray.
type xrayDNSParts struct {
	// inbounds — входящие слушателя (03_inbounds.xcp-dns.json).
	inbounds []map[string]any
	// outbound — исходящий dns (в конец 04_outbounds.xcp-groups.tail.json).
	outbound map[string]any
	// rules — правила DNS для начала routing (99_xcp-routing.json).
	rules []any
	// dnsFile — содержимое 99_xcp-dns.json.
	dnsFile []byte
}

// dnsTakeover считает части перехвата DNS; nil — перехват выключен или его ведёт
// не Xray. Группа-цель — параметр перехвата или группа «Прокси» (D-09): нет
// группы — ошибка dns_group_missing. DNS-секция Xray не у панели —
// dns_section_required (проверка перехвата до записи этого не пропускает, здесь
// защита).
func (b *xrayBuild) dnsTakeover() (*xrayDNSParts, error) {
	if !b.dnsOn {
		return nil, nil
	}
	live := b.dnsLive
	params, err := configlayer.ParseDNSTakeoverParams(b.sections[configlayer.SectionDNSTakeover])
	if err != nil {
		params = configlayer.DNSTakeoverParams{}
	}
	id := params.Group
	if id == "" {
		gid, ok := configlayer.DNSGroupIDByName(b.sections, configlayer.DefaultDNSGroupName)
		if !ok {
			return nil, genErr(xdnsErrGroupMissing, configlayer.DefaultDNSGroupName)
		}
		id = gid
	}
	if b.byID[id] == nil {
		return nil, genErr(xdnsErrGroupMissing, id)
	}
	cs := configlayer.DesiredCores(b.sections)[configlayer.SectionXrayDNS]
	if !cs.Managed {
		return nil, genErr(xdnsErrSectionRequired, configlayer.SectionXrayDNS)
	}
	fwHost, fwPort, err := xdnsResolverAddr(live.FirmwareResolver)
	if err != nil {
		return nil, err
	}

	inbounds, inTags := xdnsInbounds(live)
	field, tag := b.xrayTarget(Target{Kind: KindGroup, ID: id})
	rules := []any{
		map[string]any{"inboundTag": inTags, "outboundTag": TagDNSOut},
		map[string]any{"inboundTag": []string{TagDNSDirect}, "outboundTag": TagDirect},
		map[string]any{"inboundTag": []string{TagDNSQuery}, field: tag},
	}

	front := b.dnsFront(live, params, fwHost, fwPort)
	dnsFile, err := cores.XrayDNSFileWithTakeover(cs.Value, cores.XrayDNSOverlay{Tag: TagDNSQuery, Front: front})
	if err != nil {
		return nil, err
	}
	return &xrayDNSParts{
		inbounds: inbounds,
		outbound: map[string]any{"tag": TagDNSOut, "protocol": "dns"},
		rules:    rules,
		dnsFile:  dnsFile,
	}, nil
}

// xdnsResolverAddr разбирает адрес резолвера прошивки из живых данных
// перехвата (хост:порт). Без адреса локальные зоны и direct-домены некуда
// направить — ошибка, а не умолчание: 127.0.0.1:53 под перехватом — само ядро.
func xdnsResolverAddr(addr string) (string, int, error) {
	host, p, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" {
		return "", 0, genErr(xdnsErrResolver, "")
	}
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, genErr(xdnsErrResolver, "")
	}
	return host, port, nil
}

// xdnsInbounds — входящие слушателя :53 и их теги. Адреса — Live.Listen, если
// заданы, иначе по D-31: при IPv6 у роутера один входящий на «::» (факт
// bindv6only=0 из 148-SPIKE.md, решение владельца 2026-11-04: сокет [::]:53
// принимает и IPv4), без IPv6 — «0.0.0.0». Первый входящий — TagDNSIn, второй —
// TagDNSIn6, следующие — TagDNSIn с номером.
func xdnsInbounds(live configlayer.DNSTakeoverLive) ([]map[string]any, []string) {
	var addrs []string
	for _, a := range live.Listen {
		a = strings.TrimSpace(a)
		if h, _, err := net.SplitHostPort(a); err == nil {
			a = h
		}
		a = strings.Trim(a, "[]")
		if a != "" && !slices.Contains(addrs, a) {
			addrs = append(addrs, a)
		}
	}
	if len(addrs) == 0 {
		if live.IPv6 {
			addrs = []string{"::"}
		} else {
			addrs = []string{"0.0.0.0"}
		}
	}
	inbounds := make([]map[string]any, 0, len(addrs))
	tags := make([]string, 0, len(addrs))
	for i, a := range addrs {
		tag := TagDNSIn
		switch {
		case i == 1:
			tag = TagDNSIn6
		case i > 1:
			tag = TagDNSIn + "-" + strconv.Itoa(i+1)
		}
		tags = append(tags, tag)
		// address и port у dokodemo-door для перехвата не используются: запрос
		// забирает исходящий dns. Флага прозрачного перенаправления нет (143 Q7).
		inbounds = append(inbounds, map[string]any{
			"tag":      tag,
			"listen":   a,
			"port":     xdnsPort,
			"protocol": "dokodemo-door",
			"settings": map[string]any{"address": "1.1.1.1", "port": xdnsPort, "network": "tcp,udp"},
		})
	}
	return inbounds, tags
}

// dnsFront — серверы, которые встают в начало объекта dns: локальные зоны
// (встроенные D-11 и свои) — резолвер прошивки с finalQuery, без запаса через
// группу (D-22).
func (b *xrayBuild) dnsFront(live configlayer.DNSTakeoverLive, params configlayer.DNSTakeoverParams, fwHost string, fwPort int) []map[string]any {
	var front []map[string]any
	if zones := xdnsZoneDomains(live.ServiceDomain, params.Zones); len(zones) > 0 {
		front = append(front, xdnsFirmwareServer(fwHost, fwPort, zones, true))
	}
	return front
}

// xdnsFirmwareServer — сервер резолвера прошивки для списка доменов: тег
// прямого выхода, skipFallback (запасом для чужих имён он не становится);
// final — finalQuery, без запаса через группу.
func xdnsFirmwareServer(host string, port int, domains []string, final bool) map[string]any {
	s := map[string]any{
		"address":      host,
		"port":         port,
		"domains":      domains,
		"tag":          TagDNSDirect,
		"skipFallback": true,
	}
	if final {
		s["finalQuery"] = true
	}
	return s
}

// xdnsZoneDomains — локальные зоны в записи domains Xray («domain:<зона>»):
// встроенный набор со служебным доменом и свои зоны; имена в ASCII (idna),
// без повторов, недопустимые пропускаются (их блокирует проверка перехвата).
func xdnsZoneDomains(serviceDomain string, own []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, z := range append(configlayer.BuiltinLocalZones(serviceDomain), own...) {
		name, ok := xdnsASCII(z)
		if !ok || configlayer.ValidZone(name) != nil || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, "domain:"+name)
	}
	return out
}

// xdnsASCII приводит имя к нижнему регистру ASCII без точки в конце.
func xdnsASCII(name string) (string, bool) {
	n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if n == "" {
		return "", false
	}
	a, err := idna.Punycode.ToASCII(n)
	if err != nil || a == "" {
		return "", false
	}
	return a, true
}
