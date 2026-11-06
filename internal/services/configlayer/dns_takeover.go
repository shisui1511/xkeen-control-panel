package configlayer

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// Общие типы перехвата DNS (фаза 148): их читают служба перехвата и генераторы
// маршрутизации и секций ядер, не импортируя друг друга.

const (
	// SectionDNSTakeover — секция черновика с параметрами перехвата (группа-цель,
	// свои локальные зоны): значения правятся в черновике и применяются через
	// «Применить», как любые другие секции.
	SectionDNSTakeover = "dns_takeover"
	// LiveKeyDNSTakeover — ключ живых данных перехвата (включён, ядро, адрес
	// резолвера прошивки, служебный домен, IPv6). Фиксируется только транзакциями
	// перехвата (RunSectionLive с Live, RunRestore) одной записью с манифестом.
	LiveKeyDNSTakeover = "dns_takeover"
	// DefaultDNSGroupName — группа-цель перехвата по умолчанию (D-09): то же имя,
	// что у группы по умолчанию маршрутизации (defaultProxyGroupName в service.go
	// пакета routing); менять только вместе.
	DefaultDNSGroupName = "Прокси"
	// groupsSectionName — секция групп маршрутизации (routing.GroupsSection).
	groupsSectionName = "groups"
	// maxZoneLen — предел длины доменного имени в ASCII.
	maxZoneLen = 253
	// maxZoneLabelLen — предел длины одной метки имени.
	maxZoneLabelLen = 63
)

// DNSTakeoverParams — значение секции черновика dns_takeover.
type DNSTakeoverParams struct {
	// Group — id группы маршрутизации, через которую идут DNS-запросы; пусто —
	// группа по умолчанию (DefaultDNSGroupName).
	Group string `json:"group"`
	// Zones — свои локальные зоны пользователя сверх встроенных (D-11).
	Zones []string `json:"zones"`
}

// DNSTakeoverLive — живые данные перехвата (State.Live[LiveKeyDNSTakeover]).
type DNSTakeoverLive struct {
	// Enabled — перехват включён: генераторы добавляют слушатель :53 и маршруты.
	Enabled bool `json:"enabled"`
	// Kernel — ядро, которое держит перехват (xray или mihomo).
	Kernel string `json:"kernel"`
	// FirmwareResolver — адрес резолвера прошивки (хост:порт) для локальных зон.
	FirmwareResolver string `json:"firmware_resolver"`
	// ServiceDomain — служебный домен роутера по бренду.
	ServiceDomain string `json:"service_domain"`
	// IPv6 — у роутера есть IPv6: ядро слушает и [::]:53 (D-31).
	IPv6 bool `json:"ipv6"`
	// Listen — адреса слушателя по факту; пусто — умолчания генератора.
	Listen []string `json:"listen,omitempty"`
}

// ParseDNSTakeoverParams разбирает секцию dns_takeover; пустое значение или JSON
// null — нулевые параметры.
func ParseDNSTakeoverParams(raw json.RawMessage) (DNSTakeoverParams, error) {
	if isNullRaw(raw) {
		return DNSTakeoverParams{}, nil
	}
	var p DNSTakeoverParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return DNSTakeoverParams{}, fmt.Errorf("configlayer: секция %s: %w", SectionDNSTakeover, err)
	}
	return p, nil
}

// ParseDNSTakeoverLive читает живые данные перехвата; ok=true только если ключ
// есть, разбирается и перехват включён.
func ParseDNSTakeoverLive(live Sections) (DNSTakeoverLive, bool) {
	raw, has := live[LiveKeyDNSTakeover]
	if !has || isNullRaw(raw) {
		return DNSTakeoverLive{}, false
	}
	var v DNSTakeoverLive
	if err := json.Unmarshal(raw, &v); err != nil {
		return DNSTakeoverLive{}, false
	}
	return v, v.Enabled
}

// builtinLocalZones — встроенные локальные зоны (D-11) без служебного домена:
// домены локальной сети, обратные зоны частных сетей RFC 1918, link-local IPv4 и
// уникальных локальных адресов IPv6.
var builtinLocalZones = func() []string {
	out := []string{"lan", "local", "home.arpa", "10.in-addr.arpa"}
	for i := 16; i <= 31; i++ {
		out = append(out, strconv.Itoa(i)+".172.in-addr.arpa")
	}
	return append(out, "168.192.in-addr.arpa", "254.169.in-addr.arpa", "d.f.ip6.arpa")
}()

// BuiltinLocalZones возвращает встроенный набор локальных зон (D-11) и служебный
// домен роутера, если он задан и ещё не входит в набор. Результат — новая копия.
func BuiltinLocalZones(serviceDomain string) []string {
	out := append([]string(nil), builtinLocalZones...)
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(serviceDomain)), ".")
	if d == "" {
		return out
	}
	for _, z := range out {
		if z == d {
			return out
		}
	}
	return append(out, d)
}

// zoneLabelRE — метка доменного имени в ASCII (допускает подчёркивание, как в
// служебных именах).
var zoneLabelRE = regexp.MustCompile(`^[a-z0-9_](?:[a-z0-9_-]*[a-z0-9_])?$`)

// ValidZone проверяет имя локальной зоны: доменное имя без схемы, пути, порта и
// пробелов; после перевода в ASCII (idna) не длиннее 253 символов, метки не
// длиннее 63.
func ValidZone(name string) error {
	if name == "" {
		return errors.New("зона: пустое имя")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("зона %q: пробелы и управляющие символы недопустимы", name)
		}
	}
	if strings.ContainsAny(name, "/:@?#[]\\") {
		return fmt.Errorf("зона %q: нужно доменное имя без схемы, пути и порта", name)
	}
	ascii, err := idna.Punycode.ToASCII(strings.ToLower(name))
	if err != nil {
		return fmt.Errorf("зона %q: %w", name, err)
	}
	if len(ascii) > maxZoneLen {
		return fmt.Errorf("зона %q: длиннее %d символов", name, maxZoneLen)
	}
	for _, label := range strings.Split(ascii, ".") {
		if len(label) > maxZoneLabelLen || !zoneLabelRE.MatchString(label) {
			return fmt.Errorf("зона %q: недопустимая метка %q", name, label)
		}
	}
	return nil
}

// dnsGroupItem — минимальный разбор группы маршрутизации: id и имя.
type dnsGroupItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// dnsGroups разбирает секцию groups до id и имён групп; ошибка или пустая
// секция — нет групп.
func dnsGroups(sections Sections) []dnsGroupItem {
	raw, ok := sections[groupsSectionName]
	if !ok || isNullRaw(raw) {
		return nil
	}
	var cfg struct {
		Items []dnsGroupItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	return cfg.Items
}

// DNSTargetGroupName возвращает имя группы маршрутизации по её id из секции
// groups; ok=false — группы нет.
func DNSTargetGroupName(sections Sections, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for _, g := range dnsGroups(sections) {
		if g.ID == id {
			return g.Name, true
		}
	}
	return "", false
}

// DNSGroupIDByName возвращает id первой группы маршрутизации с именем name из
// секции groups; ok=false — такой группы нет.
func DNSGroupIDByName(sections Sections, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	for _, g := range dnsGroups(sections) {
		if g.Name == name {
			return g.ID, true
		}
	}
	return "", false
}
