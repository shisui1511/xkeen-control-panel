package routing

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// xrayTarget переводит цель правила в пару «поле, тег»: ручная группа —
// outboundTag, доступная «авто» или «резерв» — balancerTag, DIRECT — xcp-direct,
// REJECT — xcp-block. Группа, которой нет, недоступная на версии ядра или пустая
// для Xray (D-13, D-42), ведёт в xcp-block: трафик не уходит мимо VPN.
func (b *xrayBuild) xrayTarget(t Target) (field, tag string) {
	switch t.Kind {
	case KindDirect:
		return "outboundTag", TagDirect
	case KindReject:
		return "outboundTag", TagBlock
	case KindGroup:
		g := b.byID[t.ID]
		if g == nil {
			return "outboundTag", TagBlock
		}
		if g.Mode == ModeManual {
			if b.selection(*g) == "" {
				return "outboundTag", TagBlock
			}
			return "outboundTag", GroupTag(g.ID)
		}
		if bal := b.balancers[g.ID]; bal != nil {
			return "balancerTag", bal.tag
		}
	}
	return "outboundTag", TagBlock
}

// xrayDomain приводит значение условия «домен» к синтаксису Xray: без префикса
// домен ловится с поддоменами (D-18), full:, keyword: и regexp: остаются как есть.
func xrayDomain(v string) string {
	for _, p := range []string{"full:", "keyword:", "regexp:"} {
		if strings.HasPrefix(v, p) {
			return v
		}
	}
	return "domain:" + v
}

// xrayListRefs превращает списки dat в значения ext:<файл>:<тег>.
func xrayListRefs(c Cond) []string {
	out := make([]string, 0, len(c.Lists))
	for _, l := range c.Lists {
		out = append(out, "ext:"+l.File+":"+l.Tag)
	}
	return out
}

// xrayRule переводит правило модели в правило routing Xray. ok=false — правило
// пропускается: оно выключено или написано для Mihomo. Условия правила — поля
// одного объекта (И), значения поля — список (ИЛИ). Два условия, которые ложатся
// в одно поле, не выражаются: ошибка xray_cond_conflict (расширение правила
// могло бы направить трафик иначе, чем задумано).
func (b *xrayBuild) xrayRule(r Rule) (rule map[string]any, ok bool, err error) {
	if !r.Enabled {
		return nil, false, nil
	}
	field, tag := b.xrayTarget(r.Target)
	if r.Raw != nil {
		if r.Raw.Kernel != configlayer.KernelXray {
			return nil, false, nil
		}
		dec := json.NewDecoder(bytes.NewReader([]byte(r.Raw.Text)))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil || obj == nil {
			return nil, false, genErr(genErrRawInvalid, "")
		}
		// Цель назначает панель, цель из текста правила отбрасывается.
		delete(obj, "outboundTag")
		delete(obj, "balancerTag")
		obj[field] = tag
		return obj, true, nil
	}

	rule = map[string]any{}
	set := func(name string, value any) error {
		if _, taken := rule[name]; taken {
			return genErr(genErrCondConflict, name)
		}
		rule[name] = value
		return nil
	}
	for _, c := range r.Conds {
		var e error
		switch c.Type {
		case CondDomain:
			vals := make([]string, 0, len(c.Values))
			for _, v := range c.Values {
				vals = append(vals, xrayDomain(v))
			}
			e = set("domain", vals)
		case CondGeosite:
			e = set("domain", xrayListRefs(c))
		case CondIP:
			e = set("ip", append([]string(nil), c.Values...))
		case CondGeoip:
			e = set("ip", xrayListRefs(c))
		case CondPort:
			e = set("port", strings.Join(c.Values, ","))
		case CondNetwork:
			e = set("network", strings.Join(c.Values, ","))
		case CondSrcIP:
			e = set("source", append([]string(nil), c.Values...))
		case CondBittorrent:
			e = set("protocol", []string{"bittorrent"})
		}
		if e != nil {
			return nil, false, e
		}
	}
	if len(rule) == 0 {
		// Правило без условий поймало бы весь трафик; проверка модели такого не
		// допускает.
		return nil, false, genErr(genErrRawInvalid, "")
	}
	rule[field] = tag
	return rule, true, nil
}
