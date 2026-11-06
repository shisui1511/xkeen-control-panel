package cores

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Идентификатор генератора секций Xray в реестре слоя.
const xrayGenID = "cores_xray"

// xrayIndent — отступ файлов панели (как у остальных генераторов Xray).
const xrayIndent = "  "

// isEmptyValue — значения нет: пусто или null.
func isEmptyValue(v json.RawMessage) bool {
	t := bytes.TrimSpace(v)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// wrapXrayKey собирает файл {"<ключ>": значение}: значение — сырые байты,
// порядок вложенных ключей не меняется; отступ два пробела, перевод строки в
// конце.
func wrapXrayKey(key string, value json.RawMessage) ([]byte, error) {
	o := configlayer.NewOrderedObject()
	o.Set(key, value)
	return indentXray(o)
}

// indentXray сериализует объект файла панели.
func indentXray(o *configlayer.OrderedObject) ([]byte, error) {
	b, err := o.MarshalIndent(xrayIndent)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// policyWithCounters дописывает в policy.system четыре закреплённых счётчика
// (D-10): остальные ключи system и levels, а также порядок ключей сохраняются.
func policyWithCounters(value json.RawMessage) (json.RawMessage, error) {
	policy, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	system := configlayer.NewOrderedObject()
	if raw, ok := policy.Get("system"); ok && !isEmptyValue(raw) {
		if system, err = configlayer.ParseOrderedObject(raw); err != nil {
			return nil, fmt.Errorf("policy.system: %w", err)
		}
	}
	for _, c := range xrayStatsCounters {
		system.Set(c, json.RawMessage("true"))
	}
	sys, err := system.MarshalJSON()
	if err != nil {
		return nil, err
	}
	policy.Set("system", sys)
	return policy.MarshalJSON()
}

// XraySectionFile возвращает содержимое файла панели секции id из её значения.
// Пустое значение заменяется пресетом секции. Для log, dns, policy файл —
// {"<ключ>": значение}, для inbounds — {"inbounds": [...]}, для «Прочих
// ключей» — сам объект значения; ok=false — файла нет (пустые «Прочие ключи»).
// В policy дописываются закреплённые счётчики статистики. Поля, которых форма не
// знает, и порядок ключей переносятся как есть.
func XraySectionFile(id string, value json.RawMessage) (content []byte, ok bool, err error) {
	spec, known := xraySpecByID(id)
	if !known {
		return nil, false, fmt.Errorf("секция Xray %q неизвестна", id)
	}
	if isEmptyValue(value) {
		value = XrayPreset(id)
	}
	switch id {
	case "log":
		// Выключенный журнал всегда пишется как access: none (D-16).
		if value, err = withAccessNone(value); err == nil {
			content, err = wrapXrayKey(spec.Key, value)
		}
	case "dns":
		content, err = wrapXrayKey(spec.Key, value)
	case "policy":
		if value, err = policyWithCounters(value); err == nil {
			content, err = wrapXrayKey(spec.Key, value)
		}
	case "inbounds":
		if _, isList := xrayInbounds(value); !isList {
			return nil, false, fmt.Errorf("секция Xray %q: ожидался массив объектов", id)
		}
		content, err = wrapXrayKey(spec.Key, value)
	case "other":
		var obj *configlayer.OrderedObject
		if obj, err = configlayer.ParseOrderedObject(value); err != nil {
			return nil, false, fmt.Errorf("секция Xray %q: %w", id, err)
		}
		if obj.Len() == 0 {
			return nil, false, nil
		}
		content, err = indentXray(obj)
	}
	if err != nil {
		return nil, false, fmt.Errorf("секция Xray %q: %w", id, err)
	}
	return content, true, nil
}

// xrayDNSDefaultServers — апстримы перехвата по умолчанию (D-17): DoH по IP,
// загрузочный резолвер не нужен. Запросы к ним наследуют тег объекта dns и идут
// через группу-цель (D-20).
var xrayDNSDefaultServers = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

// XrayDNSOverlay — то, что перехват DNS роутера дописывает в объект dns Xray:
// тег запросов DNS-модуля (по нему правило ведёт их в группу-цель) и серверы,
// которые встают первыми (локальные зоны, имена узлов, домены direct-правил).
type XrayDNSOverlay struct {
	Tag   string
	Front []map[string]any
}

// XrayDNSTakeover сообщает, что перехват DNS роутера ведёт Xray и объект dns
// в этот запуск выпускает генератор маршрутизации Xray (он знает группы, узлы и
// правила): перехват включён в живых данных для Xray, а маршрутизация Xray под
// панелью. Генератор секций секцию dns тогда не выпускает — у файла
// 99_xcp-dns.json один производитель на запуск. Маршрутизация не под панелью —
// перехвата в файлах нет, объект dns пишет генератор секций, как без перехвата.
func XrayDNSTakeover(in configlayer.Input) (configlayer.DNSTakeoverLive, bool) {
	live, on := configlayer.ParseDNSTakeoverLive(in.Live)
	if !on || live.Kernel != configlayer.KernelXray {
		return configlayer.DNSTakeoverLive{}, false
	}
	if !configlayer.DesiredRouting(in.Sections).ManagedFor(configlayer.KernelXray) {
		return configlayer.DNSTakeoverLive{}, false
	}
	return live, true
}

// XrayDNSFileWithTakeover возвращает файл 99_xcp-dns.json при перехвате DNS:
// значение секции DNS (пустое — пресет) с сохранёнными ключами и их порядком,
// tag — тег оверлея, servers — серверы оверлея, затем серверы пользователя. У
// серверов пользователя снимаются свой tag и режим «+local» (иначе запрос обошёл
// бы группу, D-20), серверы, ведущие на сам слушатель :53 роутера (localhost,
// loopback на порт 53), отбрасываются: это петля. Не осталось ни одного —
// умолчания D-17. Значение секции в черновике не меняется.
func XrayDNSFileWithTakeover(value json.RawMessage, ov XrayDNSOverlay) ([]byte, error) {
	if isEmptyValue(value) {
		value = XrayPreset("dns")
	}
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, fmt.Errorf("секция Xray \"dns\": %w", err)
	}
	var user []json.RawMessage
	if raw, ok := obj.Get("servers"); ok && !isEmptyValue(raw) {
		if err := json.Unmarshal(raw, &user); err != nil {
			return nil, fmt.Errorf("секция Xray \"dns\": servers: ожидался массив")
		}
	}
	servers := make([]any, 0, len(user))
	for _, s := range ov.Front {
		servers = append(servers, s)
	}
	kept := 0
	for _, raw := range user {
		s, ok, err := xrayTakeoverServer(raw)
		if err != nil {
			return nil, fmt.Errorf("секция Xray \"dns\": servers: %w", err)
		}
		if ok {
			servers = append(servers, s)
			kept++
		}
	}
	if kept == 0 {
		for _, s := range xrayDNSDefaultServers {
			servers = append(servers, s)
		}
	}
	tag, err := jsonNoEscape(ov.Tag)
	if err != nil {
		return nil, err
	}
	list, err := jsonNoEscape(servers)
	if err != nil {
		return nil, err
	}
	obj.Set("tag", tag)
	obj.Set("servers", list)
	dns, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("секция Xray \"dns\": %w", err)
	}
	return wrapXrayKey("dns", dns)
}

// xrayTakeoverServer готовит сервер пользователя к перехвату: ok=false — сервер
// ведёт на слушатель :53 самого роутера (петля) и отбрасывается. Строка и объект
// чистятся от «+local» и своего tag; прочие значения переносятся как есть.
func xrayTakeoverServer(raw json.RawMessage) (json.RawMessage, bool, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return nil, false, nil
	}
	switch t[0] {
	case '"':
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return nil, false, err
		}
		if xrayLoopServer(s, 0) {
			return nil, false, nil
		}
		out, err := jsonNoEscape(xrayNoLocal(s))
		return out, err == nil, err
	case '{':
		o, err := configlayer.ParseOrderedObject(t)
		if err != nil {
			return nil, false, err
		}
		addr := ""
		if v, ok := o.Get("address"); ok {
			_ = json.Unmarshal(v, &addr)
		}
		port := 0
		if v, ok := o.Get("port"); ok {
			_ = json.Unmarshal(v, &port)
		}
		if addr != "" && xrayLoopServer(addr, port) {
			return nil, false, nil
		}
		if addr != "" {
			a, err := jsonNoEscape(xrayNoLocal(addr))
			if err != nil {
				return nil, false, err
			}
			o.Set("address", a)
		}
		o.Delete("tag")
		out, err := o.MarshalJSON()
		return out, err == nil, err
	}
	return t, true, nil
}

// xrayNoLocal убирает «+local» из схемы адреса сервера (https+local://,
// tcp+local://, quic+local://): локальный режим идёт системным сокетом мимо
// маршрутизации, то есть мимо группы-цели.
func xrayNoLocal(addr string) string {
	if i := strings.Index(addr, "+local://"); i > 0 {
		return addr[:i] + addr[i+len("+local"):]
	}
	return addr
}

// xrayLoopServer сообщает, что сервер DNS ведёт на слушатель :53 самого роутера:
// localhost (системный резолвер, а он под перехватом — само ядро) или адрес
// loopback либо «любой» на порт 53 (явный или по умолчанию схемы). port —
// отдельное поле port объекта сервера (0 — не задано).
func xrayLoopServer(addr string, port int) bool {
	a := strings.ToLower(strings.TrimSpace(addr))
	scheme := ""
	if i := strings.Index(a, "://"); i >= 0 {
		scheme, a = a[:i], a[i+len("://"):]
	}
	scheme = strings.TrimSuffix(scheme, "+local")
	if i := strings.IndexAny(a, "/?#"); i >= 0 {
		a = a[:i]
	}
	if a == "localhost" && scheme == "" {
		return true
	}
	host, p := a, ""
	if h, pp, err := net.SplitHostPort(a); err == nil {
		host, p = h, pp
	}
	host = strings.Trim(host, "[]")
	if p == "" && port > 0 {
		p = strconv.Itoa(port)
	}
	if p == "" {
		switch scheme {
		case "", "udp", "tcp":
			p = "53"
		}
	}
	if p != "53" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// jsonNoEscape кодирует значение без HTML-экранирования (адреса DoH с & и ?
// остаются как есть) и без перевода строки в конце.
func jsonNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// xrayGenerator — генератор файлов секций Xray (cores_xray).
type xrayGenerator struct{}

// XrayGenerator возвращает генератор файлов секций Xray под управлением панели.
func XrayGenerator() configlayer.Generator { return xrayGenerator{} }

// ID — идентификатор генератора в реестре.
func (xrayGenerator) ID() string { return xrayGenID }

// Generate выдаёт файл панели для каждой секции Xray с managed=true. Xray не
// установлен — файлов нет. Служебные поля секции черновика (managed, from, keep,
// …) в файл не попадают: берётся только Value. Protocol, network, followRedirect
// и теги входящих генератор не меняет — только переносит значение. При
// перехвате DNS роутера на Xray (XrayDNSTakeover) секцию dns выпускает генератор
// маршрутизации, здесь она пропускается.
func (xrayGenerator) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	if !installed.Xray {
		return nil, nil
	}
	cores := configlayer.DesiredCores(in.Sections)
	_, takeover := XrayDNSTakeover(in)
	var out []configlayer.GeneratedFile
	for _, spec := range XraySpecs {
		cs := cores[spec.Section]
		if !cs.Managed {
			continue
		}
		if takeover && spec.ID == "dns" {
			continue
		}
		content, ok, err := XraySectionFile(spec.ID, cs.Value)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, configlayer.GeneratedFile{
			Kernel:  configlayer.KernelXray,
			RelPath: spec.File,
			Kind:    configlayer.KindXrayJSON,
			Content: content,
		})
	}
	return out, nil
}
