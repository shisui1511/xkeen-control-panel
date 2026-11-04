package routing

import (
	"errors"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/net/idna"
	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Часть dns профиля Mihomo под перехватом DNS роутера (фаза 148, DNSF-01,
// DNSF-04). Включён ли перехват, какое ядро его держит и адрес резолвера
// прошивки — только из живых данных Live[dns_takeover] (их обновляет каждое
// включение, возврат и перенос перехвата); группа-цель и свои зоны — из секции
// dns_takeover. Без перехвата часть dns остаётся такой, какой её собрали
// секции Mihomo (cores.MihomoProfileParts).

// Коды ошибок генератора Mihomo под перехватом.
const (
	// genErrDNSGroupMissing — группы-цели нет в профиле (удалена или не
	// выпущена): суффиксу `#<имя>` не на что ссылаться.
	genErrDNSGroupMissing = "dns_group_missing"
	// genErrDNSGroupNameInvalid — имя группы-цели нельзя поставить в суффикс
	// `#` адреса DoH (Pitfall 5, правило DNSGroupNameURLSafe).
	genErrDNSGroupNameInvalid = "dns_group_name_invalid"
)

// Значения части dns, которые ставит перехват.
const (
	// mihomoDNSListen4 и mihomoDNSListen6 — слушатель DNS роутера. Решение
	// владельца D-31 (fact-closed, 148-SPIKE.md от 2026-11-04): на стенде
	// bindv6only=0, поэтому один сокет `[::]:53` принимает и IPv4, и IPv6; без
	// IPv6 у роутера — `0.0.0.0:53`. Переключателя во время работы нет: смена
	// решения — новая правка генератора.
	mihomoDNSListen4 = "0.0.0.0:53"
	mihomoDNSListen6 = "[::]:53"
	// mihomoDNSScheme — схема адреса резолвера прошивки в записях Mihomo.
	mihomoDNSScheme = "udp://"
	// mihomoSystemDNS — запись «системный резолвер»: под перехватом это сам
	// слушатель ядра на :53 (петля, Pitfall 3).
	mihomoSystemDNS = "system"
	// mihomoDHCPDNS — схема «DNS из DHCP»: на роутере это тоже он сам.
	mihomoDHCPDNS = "dhcp://"
)

// dnsDefaultDoH — серверы по умолчанию (D-17): DoH по IP, загрузочный резолвер
// не нужен. Они же — умолчание proxy-server-nameserver (D-18).
var dnsDefaultDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

// dnsDefaultBootstrap — default-nameserver по умолчанию: только IP.
var dnsDefaultBootstrap = []string{"1.1.1.1", "8.8.8.8"}

// takeoverDNS правит часть dns профиля под перехват, если Live[dns_takeover]
// включён и перехват держит Mihomo; иначе ничего не меняет. Узлы YAML правятся
// на месте, значения пользователя (порядок ключей, свои записи политики)
// сохраняются, служебное панель ставит всегда.
func (b *mihomoBuild) takeoverDNS(parts []*yaml.Node) error {
	live, on := configlayer.ParseDNSTakeoverLive(b.live)
	if !on || live.Kernel != configlayer.KernelMihomo {
		return nil
	}
	params, err := configlayer.ParseDNSTakeoverParams(b.sections[configlayer.SectionDNSTakeover])
	if err != nil {
		return err
	}
	group, err := b.dnsGroupName(params)
	if err != nil {
		return err
	}
	dns := mihomoDNSNode(parts)
	if dns == nil {
		return errors.New("routing: в профиле Mihomo нет части dns")
	}
	firmware := ""
	if fw := strings.TrimSpace(live.FirmwareResolver); fw != "" {
		firmware = mihomoDNSScheme + fw
	}

	yMapSet(dns, "enable", yBool(true))
	listen := mihomoDNSListen4
	if live.IPv6 {
		listen = mihomoDNSListen6
	}
	yMapSet(dns, "listen", yStr(listen))

	// D-20: все серверы «Серверы» идут в группу-цель, вписанные пользователем
	// тоже; записи, ведущие на сам роутер, убираются (петля, Pitfall 3). Пусто
	// после очистки — умолчания D-17. fallback Mihomo опрашивает параллельно с
	// nameserver: без суффикса он ушёл бы мимо туннеля, поэтому и он идёт в группу.
	servers := dnsLoopFree(yStrings(yMapGet(dns, "nameserver")), true)
	if len(servers) == 0 {
		servers = dnsDefaultDoH
	}
	yMapSet(dns, "nameserver", ySeq(dnsViaGroup(servers, group), false))
	if fb := yMapGet(dns, "fallback"); fb != nil {
		if list := dnsLoopFree(yStrings(fb), true); len(list) > 0 {
			yMapSet(dns, "fallback", ySeq(dnsViaGroup(list, group), false))
		} else {
			yMapDel(dns, "fallback")
		}
	}

	if len(yStrings(yMapGet(dns, "default-nameserver"))) == 0 {
		yMapSet(dns, "default-nameserver", ySeq(dnsDefaultBootstrap, false))
	}

	// D-18: имена узлов резолвятся до туннеля — серверы секции как есть (без
	// суффикса: прямое соединение с меткой 255), пусто — пара DoH по IP. Решение
	// владельца D-18 (race, 148-SPIKE.md от 2026-11-04): в конец дописывается
	// закреплённое значение панели — резолвер прошивки из Live. При
	// mihomo_list_mode=parallel список опрашивается гонкой, кто ответит первым,
	// тот и выигрывает; так решил владелец. В значение секции и в черновик
	// адрес прошивки не пишется: новое включение или возврат даёт новый адрес.
	psn := dnsLoopFree(yStrings(yMapGet(dns, "proxy-server-nameserver")), false)
	if len(psn) == 0 {
		psn = append([]string(nil), dnsDefaultDoH...)
	}
	if firmware != "" && !dnsHasServer(psn, firmware) {
		psn = append(psn, firmware)
	}
	yMapSet(dns, "proxy-server-nameserver", ySeq(psn, false))

	zones := dnsTakeoverZones(live.ServiceDomain, params.Zones)
	if firmware != "" {
		keys := make([]string, 0, len(zones))
		for _, z := range zones {
			keys = append(keys, "+."+z)
		}
		mihomoPolicyAdd(dns, keys, firmware)
	}
	return nil
}

// dnsGroupName возвращает имя выпущенной группы-цели перехвата: группа секции
// dns_takeover или группа по умолчанию (D-09). Группы нет в профиле —
// dns_group_missing; имя нельзя поставить в суффикс `#` — dns_group_name_invalid
// (то же правило, что у проверки слоя и предпроверки включения).
func (b *mihomoBuild) dnsGroupName(p configlayer.DNSTakeoverParams) (string, error) {
	id, want := p.Group, p.Group
	if id == "" {
		want = configlayer.DefaultDNSGroupName
		id, _ = configlayer.DNSGroupIDByName(b.sections, configlayer.DefaultDNSGroupName)
	}
	mg := b.shapeByID[id]
	if id == "" || mg == nil {
		return "", genErr(genErrDNSGroupMissing, want)
	}
	name := mg.g.Name
	if !DNSGroupNameURLSafe(name) {
		return "", genErr(genErrDNSGroupNameInvalid, name)
	}
	return name, nil
}

// mihomoPolicyAdd дописывает в nameserver-policy ключи панели с одним
// значением — резолвером прошивки. Решение владельца R5 (вариант a, 148-SPIKE.md
// от 2026-11-04): запаса через группу в записи нет — mihomo_list_mode=parallel
// сделал бы его гонкой, и ответ на локальное или direct-имя мог бы прийти из
// туннеля; при отказе прошивки клиент получает SERVFAIL и повторяет запрос
// (отклонение от D-23 только для Mihomo). Записи пользователя (D-24) остаются
// первыми, совпадающий ключ остаётся пользовательским (D-19); ключи панели
// идут после них без повторов.
func mihomoPolicyAdd(dns *yaml.Node, keys []string, firmware string) {
	policy := yMapGet(dns, "nameserver-policy")
	if policy == nil || policy.Kind != yaml.MappingNode {
		policy = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(policy.Content); i += 2 {
		seen[strings.ToLower(policy.Content[i].Value)] = true
	}
	for _, k := range keys {
		low := strings.ToLower(k)
		if seen[low] {
			continue
		}
		seen[low] = true
		policy.Content = append(policy.Content, yKey(k), yStr(firmware))
	}
	if len(policy.Content) > 0 {
		yMapSet(dns, "nameserver-policy", policy)
	}
}

// dnsTakeoverZones — встроенные локальные зоны и служебный домен (D-11) и свои
// зоны пользователя (D-22): нижний регистр, без точки в конце, в ASCII, без
// повторов; недопустимые свои зоны пропускаются (их отвергает проверка слоя, в
// ключи YAML они не попадают).
func dnsTakeoverZones(serviceDomain string, own []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(z string) {
		if name := dnsZoneName(z); name != "" && !seen[name] {
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

// dnsZoneName приводит доменное имя к виду ключа Mihomo: нижний регистр, без
// точки в конце, в ASCII (idna). Пусто — имя не годится для ключа.
func dnsZoneName(name string) string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if n == "" || strings.ContainsAny(n, " \t\r\n,:#/") {
		return ""
	}
	if ascii, err := idna.Punycode.ToASCII(n); err == nil {
		n = ascii
	}
	return n
}

// dnsViaGroup ставит каждому серверу суффикс `#<группа>`: прежний фрагмент
// после `#` (своя группа, параметры `&…`) заменяется. Повторы убираются.
func dnsViaGroup(servers []string, group string) []string {
	out := make([]string, 0, len(servers))
	seen := map[string]bool{}
	for _, s := range servers {
		base, _, _ := strings.Cut(s, "#")
		v := base + "#" + group
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// dnsLoopFree убирает записи, ведущие на сам роутер: `system`, `dhcp://…` и
// адреса loopback, `localhost` и «любой адрес» — на порту 53 (порт не указан —
// тоже 53, кроме DoH и DoT). anyPort — loopback убирается на любом порту: такие
// серверы в nameserver пошли бы через группу и на узле указывали бы на сам узел.
// Пустые строки пропускаются.
func dnsLoopFree(servers []string, anyPort bool) []string {
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		s = strings.TrimSpace(s)
		if s == "" || dnsSelfServer(s, anyPort) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// dnsSelfServer сообщает, что запись сервера Mihomo ведёт на сам роутер.
func dnsSelfServer(s string, anyPort bool) bool {
	low := strings.ToLower(s)
	if low == mihomoSystemDNS || strings.HasPrefix(low, mihomoDHCPDNS) {
		return true
	}
	base, _, _ := strings.Cut(low, "#")
	scheme, rest, ok := strings.Cut(base, "://")
	if !ok {
		scheme, rest = "udp", base
	}
	host, _, _ := strings.Cut(rest, "/")
	port := ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		host, port = h, p
	}
	host = strings.Trim(host, "[]")
	if !dnsSelfHost(host) {
		return false
	}
	if anyPort {
		return true
	}
	if port == "" {
		switch scheme {
		case "https", "tls", "quic", "h3":
			return false
		}
		port = "53"
	}
	return port == "53"
}

// dnsSelfHost — адрес самого роутера: loopback, «любой адрес» или localhost.
func dnsSelfHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.IsLoopback() || ip.IsUnspecified()
}

// dnsHasServer сообщает, что адрес (схема udp:// или без схемы) уже в списке.
func dnsHasServer(list []string, addr string) bool {
	bare := strings.TrimPrefix(addr, mihomoDNSScheme)
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == addr || s == bare {
			return true
		}
	}
	return false
}

// mihomoDNSNode возвращает отображение dns из частей профиля; nil — части нет.
func mihomoDNSNode(parts []*yaml.Node) *yaml.Node {
	for _, part := range parts {
		if v := yMapGet(part, "dns"); v != nil && v.Kind == yaml.MappingNode {
			return v
		}
	}
	return nil
}

// yMapGet — значение ключа отображения; nil — ключа нет или узел не отображение.
func yMapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// yMapSet ставит значение ключа на его месте или дописывает ключ в конец.
func yMapSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, yKey(key), v)
}

// yMapDel убирает ключ из отображения.
func yMapDel(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

// yStrings — строки списка YAML (одна строка — список из неё); прочее — пусто.
func yStrings(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" || strings.TrimSpace(n.Value) == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.Tag != "!!null" && strings.TrimSpace(c.Value) != "" {
				out = append(out, c.Value)
			}
		}
		return out
	}
	return nil
}
