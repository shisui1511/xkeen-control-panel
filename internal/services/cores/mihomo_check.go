package cores

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Проверка значений секций Mihomo до записи (D-08, D-11). Значения секретов не
// читаются и не выводятся: в проблемах только имя ключа.

// mihomoPortKeys — ключи основных портов секции «Основное».
var mihomoPortKeys = []string{"mixed-port", "port", "socks-port", "redir-port", "tproxy-port"}

// mihomoPortInvalid сообщает, что значение порта не целое число в диапазоне
// min–max. Строка из цифр принимается как число; диапазон портов строкой
// (в listeners) не проверяется.
func mihomoPortInvalid(raw json.RawMessage, lo, hi int) bool {
	t := bytes.TrimSpace(raw)
	if len(t) > 0 && t[0] == '"' {
		var s string
		if json.Unmarshal(t, &s) != nil {
			return true
		}
		if strings.ContainsAny(s, "-,") {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		return err != nil || n < lo || n > hi
	}
	n, err := strconv.Atoi(string(t))
	return err != nil || n < lo || n > hi
}

// mihomoControllerKeys — ключи, открывающие API ядра по TCP (D-11).
var mihomoControllerKeys = []string{"external-controller", "external-controller-tls"}

// mihomoTopIDs — секции, ключи которых ложатся в корень профиля
// (MihomoProfileParts). dns и sniffer — значение одного ключа, верхнеуровневых
// ключей не несут.
var mihomoTopIDs = []string{mihomoMain, mihomoInbounds, mihomoOther}

// mihomoControllerSet сообщает, задан ли адрес контроллера: значение не null;
// строка — непуста после TrimSpace. Любое другое значение JSON считается
// заданным, так строже.
func mihomoControllerSet(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s) != ""
	}
	return !mihomoIsNull(raw)
}

// mihomoSecretOK сообщает, что в собранном корне профиля secret есть и каждое его
// вхождение непусто: строка непуста после TrimSpace, не строка — не null. Пустое
// вхождение даёт false: корень получил бы пустой или задвоенный secret.
func mihomoSecretOK(tops []*configlayer.OrderedObject) bool {
	found := false
	for _, o := range tops {
		if o == nil {
			continue
		}
		raw, ok := o.Get("secret")
		if !ok {
			continue
		}
		found = true
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if strings.TrimSpace(s) == "" {
				return false
			}
			continue
		}
		if mihomoIsNull(raw) {
			return false
		}
	}
	return found
}

// mihomoSiblingTops возвращает значения секций mihomoTopIDs, кроме except, как их
// увидит профиль: только управляемые и непустые. Неуправляемая секция уходит в
// профиль пресетом, а в пресете secret нет. Ошибку разбора пропускает: её отдаёт
// проверка самой секции.
func mihomoSiblingTops(s configlayer.Sections, except string) []*configlayer.OrderedObject {
	var out []*configlayer.OrderedObject
	for _, id := range mihomoTopIDs {
		if id == except {
			continue
		}
		cs, err := configlayer.ParseCoreSection(s[configlayer.CoreSectionName(configlayer.KernelMihomo, id)])
		if err != nil || !cs.Managed || len(bytes.TrimSpace(cs.Value)) == 0 {
			continue
		}
		if o, perr := configlayer.ParseOrderedObject(cs.Value); perr == nil {
			out = append(out, o)
		}
	}
	return out
}

// mihomoControllerIssues отклоняет TCP-контроллер без непустого secret в собранном
// корне профиля (объект секции плюс соседние секции). В проблемах только имя
// ключа, значения не выводятся.
//
// Ключ «<<» в значении секции отклоняется как зарезервированный: глубокая
// защита (T-147-19). Настоящие слияния YAML уже раскрыты при разборе, поэтому
// «<<» здесь бывает только буквальным ключом в кавычках, а в профиле он без
// кавычек стал бы слиянием и подмешал бы вложенный external-controller в корень
// без secret. Проверка здесь, потому что функцию зовут все пути корневых ключей
// main, inbounds и other: mihomoValueIssues (save через mergeForm, raw/parse
// main и other через addValueIssues, CheckMihomo после переноса) и raw.go
// (raw/parse «Входящих» мимо mihomoValueIssues).
func mihomoControllerIssues(obj *configlayer.OrderedObject, siblings []*configlayer.OrderedObject) []configlayer.KeyIssue {
	var out []configlayer.KeyIssue
	if _, ok := obj.Get(yamlMergeKey); ok {
		out = append(out, configlayer.KeyIssue{Key: yamlMergeKey, Code: configlayer.IssueRawReserved})
	}
	tops := append([]*configlayer.OrderedObject{obj}, siblings...)
	for _, k := range mihomoControllerKeys {
		raw, ok := obj.Get(k)
		if !ok || !mihomoControllerSet(raw) {
			continue
		}
		if !mihomoSecretOK(tops) {
			out = append(out, configlayer.KeyIssue{Key: k, Code: configlayer.IssueControllerNoSecret})
		}
	}
	return out
}

// MihomoValueIssues проверяет значение секции Mihomo до записи и возвращает
// проблемы по ключам. Значение, не являющееся объектом, — core_section_invalid.
// secret контроллера ищется только в самой секции; по всем секциям профиля
// проверяют CheckMihomo, Service.Save и Service.ParseRaw.
func MihomoValueIssues(id string, value json.RawMessage) []configlayer.KeyIssue {
	return mihomoValueIssues(id, value, nil)
}

// mihomoValueIssues — MihomoValueIssues с соседними секциями, из которых берётся
// secret контроллера.
func mihomoValueIssues(id string, value json.RawMessage, siblings []*configlayer.OrderedObject) []configlayer.KeyIssue {
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return []configlayer.KeyIssue{{Code: configlayer.IssueCoreSectionInvalid}}
	}
	var out []configlayer.KeyIssue
	add := func(key, code string) { out = append(out, configlayer.KeyIssue{Key: key, Code: code}) }

	switch id {
	case mihomoOther:
		for _, k := range obj.Keys() {
			if mihomoRoutingKeys[k] || mihomoPinnedTop[k] || mihomoOwnKeys[k] {
				add(k, configlayer.IssueRawReserved)
			}
		}
	case mihomoMain:
		for _, k := range obj.Keys() {
			switch {
			case mihomoPinnedTop[k]:
				add(k, configlayer.IssueRawPinned)
			case mihomoRoutingKeys[k] || k == "dns" || k == "sniffer" || k == "listeners" || k == "tun":
				add(k, configlayer.IssueRawReserved)
			}
		}
		if raw, ok := obj.Get("profile"); ok && mihomoIsObject(raw) {
			if po, perr := configlayer.ParseOrderedObject(raw); perr == nil {
				if sel, has := po.Get("store-selected"); has && string(bytes.TrimSpace(sel)) == "false" {
					add("profile.store-selected", configlayer.IssueRawPinned)
				}
			}
		}
		for _, k := range mihomoPortKeys {
			if raw, ok := obj.Get(k); ok && mihomoPortInvalid(raw, 0, 65535) {
				add(k, configlayer.IssueInboundPort)
			}
		}
	case mihomoInbounds:
		for _, k := range obj.Keys() {
			if mihomoRoutingKeys[k] || mihomoPinnedTop[k] {
				add(k, configlayer.IssueRawReserved)
			}
		}
		if raw, ok := obj.Get("listeners"); ok && !mihomoIsNull(raw) {
			var list []map[string]json.RawMessage
			if json.Unmarshal(raw, &list) != nil {
				add("listeners", configlayer.IssueCoreSectionInvalid)
				break
			}
			seen := map[string]bool{}
			for _, l := range list {
				var name string
				if nr, ok := l["name"]; ok {
					_ = json.Unmarshal(nr, &name)
				}
				if name != "" {
					if seen[name] {
						add("listeners[].name", configlayer.IssueInboundDuplicate)
					}
					seen[name] = true
				}
				if pr, ok := l["port"]; ok && mihomoPortInvalid(pr, 1, 65535) {
					add("listeners[].port", configlayer.IssueInboundPort)
				}
			}
		}
		if raw, ok := obj.Get("tun"); ok && !mihomoIsNull(raw) && !mihomoIsObject(raw) {
			add("tun", configlayer.IssueCoreSectionInvalid)
		}
	}
	// API Mihomo по TCP открыт в LAN: без непустого secret в собранном корне
	// профиля «Применить» блокируется (D-11). Панель сама ходит через
	// unix-сокет. Проверка после switch: break в ветке «Входящих» не должен её
	// пропускать.
	if slices.Contains(mihomoTopIDs, id) {
		out = append(out, mihomoControllerIssues(obj, siblings)...)
	}
	return out
}

// CheckMihomo проверяет секции Mihomo черновика до записи. Mihomo не
// установлен — проблем нет. Диск не читается. Проверка идёт до записи при любом
// триггере и источнике, поэтому профиль с открытым контроллером не пишется;
// secret контроллера разрешается по всем управляемым секциям main, inbounds, other.
func CheckMihomo(in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.BuildIssue {
	if !installed.Mihomo {
		return nil
	}
	var out []configlayer.BuildIssue
	for _, id := range configlayer.MihomoSectionIDs {
		name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
		cs, err := configlayer.ParseCoreSection(in.Sections[name])
		if err != nil {
			out = append(out, configlayer.BuildIssue{Key: name, Code: configlayer.IssueCoreSectionInvalid})
			continue
		}
		if !cs.Managed || len(bytes.TrimSpace(cs.Value)) == 0 {
			continue
		}
		for _, is := range mihomoValueIssues(id, cs.Value, mihomoSiblingTops(in.Sections, id)) {
			bi := configlayer.BuildIssue{Key: name, Code: is.Code}
			if is.Key != "" {
				bi.Params = map[string]string{"key": is.Key}
			}
			out = append(out, bi)
		}
	}
	return out
}
