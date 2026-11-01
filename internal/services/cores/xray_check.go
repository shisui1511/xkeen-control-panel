package cores

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Проверка значений секций Xray до записи (D-08, D-10, SEC-02). Быстрая,
// диск не читает и терпит любой вход.

// XrayValueIssues проверяет значение секции id; keep — теги входящих, которые
// читает XKeen (CoreSection.Keep). Пустое значение допустимо: вместо него
// берётся пресет. Значение не того типа — core_section_invalid с пустым ключом.
func XrayValueIssues(id string, value json.RawMessage, keep []string) []configlayer.KeyIssue {
	if isEmptyValue(value) {
		return nil
	}
	if id == "inbounds" {
		items, ok := xrayInbounds(value)
		if !ok {
			return []configlayer.KeyIssue{{Code: configlayer.IssueCoreSectionInvalid}}
		}
		return inboundsIssues(items, keep)
	}
	obj, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return []configlayer.KeyIssue{{Code: configlayer.IssueCoreSectionInvalid}}
	}
	switch id {
	case "other":
		return otherIssues(obj)
	case "policy":
		return policyIssues(obj)
	}
	return nil
}

// otherIssues отклоняет в «Прочих ключах» ключи других частей панели и ключи
// собственных секций (D-08).
func otherIssues(obj *configlayer.OrderedObject) []configlayer.KeyIssue {
	var out []configlayer.KeyIssue
	for _, k := range obj.Keys() {
		if configlayer.XraySectionOfKey(k) != "other" || slices.Contains(xrayReservedOther, k) {
			out = append(out, configlayer.KeyIssue{Key: k, Code: configlayer.IssueRawReserved})
		}
	}
	return out
}

// policyIssues отклоняет выключение закреплённых счётчиков policy.system (D-10).
func policyIssues(policy *configlayer.OrderedObject) []configlayer.KeyIssue {
	raw, ok := policy.Get("system")
	if !ok || isEmptyValue(raw) {
		return nil
	}
	system, err := configlayer.ParseOrderedObject(raw)
	if err != nil {
		return []configlayer.KeyIssue{{Key: "system", Code: configlayer.IssueCoreSectionInvalid}}
	}
	var out []configlayer.KeyIssue
	for _, c := range xrayStatsCounters {
		if v, ok := system.Get(c); ok && bytes.Equal(bytes.TrimSpace(v), []byte("false")) {
			out = append(out, configlayer.KeyIssue{Key: "system." + c, Code: configlayer.IssueRawPinned})
		}
	}
	return out
}

// xrayInbound — поля входящего, нужные проверке.
type xrayInbound struct {
	tag      string
	listen   string
	port     json.RawMessage
	single   int
	networks []string
	raw      json.RawMessage
}

// inboundID возвращает имя входящего для ключа проблемы: тег или «#индекс».
func inboundID(tag string, idx int) string {
	if tag != "" {
		return tag
	}
	return "#" + strconv.Itoa(idx)
}

// parseXrayInbound достаёт поля входящего.
func parseXrayInbound(raw json.RawMessage) xrayInbound {
	var in struct {
		Tag      string          `json:"tag"`
		Listen   string          `json:"listen"`
		Port     json.RawMessage `json:"port"`
		Settings struct {
			Network string `json:"network"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(raw, &in)
	out := xrayInbound{tag: in.Tag, listen: in.Listen, port: in.Port, raw: raw}
	if out.listen == "" {
		out.listen = "0.0.0.0"
	}
	out.single, _ = parsePort(in.Port)
	for _, n := range strings.Split(in.Settings.Network, ",") {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			out.networks = append(out.networks, n)
		}
	}
	if len(out.networks) == 0 {
		out.networks = []string{"tcp"}
	}
	return out
}

// inboundsIssues проверяет порты, повторы и сохранность входящих XKeen.
func inboundsIssues(items []json.RawMessage, keep []string) []configlayer.KeyIssue {
	var out []configlayer.KeyIssue
	parsed := make([]xrayInbound, len(items))
	seenTag := map[string]bool{}
	for i, it := range items {
		in := parseXrayInbound(it)
		parsed[i] = in
		id := inboundID(in.tag, i)
		if _, valid := parsePort(in.port); !valid {
			out = append(out, configlayer.KeyIssue{Key: id + ".port", Code: configlayer.IssueInboundPort})
		}
		if in.tag != "" {
			if seenTag[in.tag] {
				out = append(out, configlayer.KeyIssue{Key: id + ".tag", Code: configlayer.IssueInboundDuplicate})
			}
			seenTag[in.tag] = true
		}
		for _, prev := range parsed[:i] {
			if in.single > 0 && prev.single == in.single && prev.listen == in.listen && sharesNetwork(prev.networks, in.networks) {
				out = append(out, configlayer.KeyIssue{Key: id + ".port", Code: configlayer.IssueInboundDuplicate})
				break
			}
		}
	}
	return append(out, keepIssues(parsed, keep)...)
}

// sharesNetwork — у двух входящих есть общий транспорт (tcp, udp).
func sharesNetwork(a, b []string) bool {
	for _, n := range a {
		if slices.Contains(b, n) {
			return true
		}
	}
	return false
}

// keepIssues отклоняет пропажу и смену вида входящих, которые читает XKeen, и
// новые прозрачные входящие: их задаёт XKeen, второй файл с ними панель не
// создаёт (143 Q7).
func keepIssues(items []xrayInbound, keep []string) []configlayer.KeyIssue {
	var out []configlayer.KeyIssue
	byTag := map[string]xrayInbound{}
	for _, in := range items {
		if _, dup := byTag[in.tag]; !dup && in.tag != "" {
			byTag[in.tag] = in
		}
	}
	for _, tag := range keep {
		in, ok := byTag[tag]
		if !ok {
			out = append(out, configlayer.KeyIssue{Key: tag, Code: configlayer.IssueRawPinned})
			continue
		}
		if configlayer.IsForceProxyTag(tag) {
			continue
		}
		mode, transparent := configlayer.IsTransparentInbound(in.raw)
		if !transparent || ((tag == "redirect" || tag == "tproxy") && mode != tag) {
			out = append(out, configlayer.KeyIssue{Key: tag, Code: configlayer.IssueRawPinned})
		}
	}
	for i, in := range items {
		if slices.Contains(keep, in.tag) {
			continue
		}
		if _, transparent := configlayer.IsTransparentInbound(in.raw); transparent {
			out = append(out, configlayer.KeyIssue{Key: inboundID(in.tag, i), Code: configlayer.IssueRawPinned})
		}
	}
	return out
}

// parsePort разбирает порт входящего: число 1–65535 или строка диапазонов вида
// "1000-2000,3000". single — порт, если он один (число или строка из одного
// числа), иначе 0.
func parsePort(raw json.RawMessage) (single int, ok bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return 0, false
	}
	var text string
	if t[0] == '"' {
		if json.Unmarshal(t, &text) != nil {
			return 0, false
		}
	} else {
		text = string(t)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	parts := strings.Split(text, ",")
	for _, p := range parts {
		lo, hi, isRange := strings.Cut(strings.TrimSpace(p), "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || a < 1 || a > 65535 {
			return 0, false
		}
		if isRange {
			b, err := strconv.Atoi(strings.TrimSpace(hi))
			if err != nil || b < a || b > 65535 {
				return 0, false
			}
		} else if len(parts) == 1 {
			single = a
		}
	}
	return single, true
}

// xrayChecker — проверка секций Xray в реестре слоя.
type xrayChecker struct{}

// XrayChecker возвращает проверку секций Xray для реестра слоя.
func XrayChecker() configlayer.Checker { return xrayChecker{} }

// ID — идентификатор проверки в реестре.
func (xrayChecker) ID() string { return xrayGenID }

// Check — CheckXray.
func (xrayChecker) Check(in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.BuildIssue {
	return CheckXray(in, installed)
}

// CheckXray проверяет секции Xray черновика до записи. Xray не установлен —
// проблем нет. Секция черновика, которая не разбирается, — core_section_invalid;
// для секции с managed=true проверяется значение (XrayValueIssues). Ключ
// проблемы — имя секции черновика, ключ поля — в Params["key"].
func CheckXray(in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.BuildIssue {
	if !installed.Xray {
		return nil
	}
	var out []configlayer.BuildIssue
	for _, spec := range XraySpecs {
		cs, err := configlayer.ParseCoreSection(in.Sections[spec.Section])
		if err != nil {
			out = append(out, configlayer.BuildIssue{Key: spec.Section, Code: configlayer.IssueCoreSectionInvalid, Detail: err.Error()})
			continue
		}
		if !cs.Managed {
			continue
		}
		for _, is := range XrayValueIssues(spec.ID, cs.Value, cs.Keep) {
			out = append(out, configlayer.BuildIssue{
				Key:    spec.Section,
				Code:   is.Code,
				Detail: is.Key,
				Params: map[string]string{"key": is.Key},
			})
		}
	}
	return out
}
