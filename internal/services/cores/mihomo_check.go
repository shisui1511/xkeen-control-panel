package cores

import (
	"bytes"
	"encoding/json"
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

// mihomoSecretSet сообщает, задан ли непустой secret.
func mihomoSecretSet(o *configlayer.OrderedObject) bool {
	raw, ok := o.Get("secret")
	if !ok {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s) != ""
	}
	return !mihomoIsNull(raw)
}

// MihomoValueIssues проверяет значение секции Mihomo до записи и возвращает
// проблемы по ключам. Значение, не являющееся объектом, — core_section_invalid.
func MihomoValueIssues(id string, value json.RawMessage) []configlayer.KeyIssue {
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
		// API Mihomo по TCP открыт в LAN: без непустого secret «Применить»
		// блокируется (D-11). Панель сама ходит через unix-сокет.
		if raw, ok := obj.Get("external-controller"); ok {
			var addr string
			if json.Unmarshal(raw, &addr) == nil && strings.TrimSpace(addr) != "" && !mihomoSecretSet(obj) {
				add("external-controller", configlayer.IssueControllerNoSecret)
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
	return out
}

// CheckMihomo проверяет секции Mihomo черновика до записи. Mihomo не
// установлен — проблем нет. Диск не читается.
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
		for _, is := range MihomoValueIssues(id, cs.Value) {
			bi := configlayer.BuildIssue{Key: name, Code: is.Code}
			if is.Key != "" {
				bi.Params = map[string]string{"key": is.Key}
			}
			out = append(out, bi)
		}
	}
	return out
}
