// Package cores — секции ядер Xray и Mihomo: пресеты, закреплённые значения,
// файлы панели, проверка до записи и сервис API (Phase 147).
package cores

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// XraySpec — описание одной секции Xray под управлением панели.
type XraySpec struct {
	// ID — идентификатор секции внутри ядра: log, dns, inbounds, policy, other.
	ID string
	// Section — имя секции в черновике слоя (xray_log, …).
	Section string
	// Key — ключ верхнего уровня в файле Xray; у «Прочих ключей» пуст.
	Key string
	// File — файл панели секции в каталоге Xray.
	File string
}

// XraySpecs — секции Xray в порядке показа.
var XraySpecs = buildXraySpecs()

func buildXraySpecs() []XraySpec {
	out := make([]XraySpec, 0, len(configlayer.XraySectionIDs))
	for _, id := range configlayer.XraySectionIDs {
		spec := XraySpec{
			ID:      id,
			Section: configlayer.CoreSectionName(configlayer.KernelXray, id),
			Key:     id,
			File:    configlayer.PanelXraySectionFile(id),
		}
		if id == "other" {
			spec.Key = ""
		}
		out = append(out, spec)
	}
	return out
}

// XrayFormKeys — ключи, которые знает форма секции; всё остальное в значении
// секции форма не трогает и сохраняет как есть. Совпадает с FORM_KEYS
// фронтенда.
var XrayFormKeys = map[string][]string{
	"log":      {"loglevel", "access", "error", "dnsLog"},
	"dns":      {"servers", "queryStrategy", "disableCache"},
	"inbounds": {"[].tag", "[].protocol", "[].listen", "[].port"},
	"policy":   {"levels.0.handshake", "levels.0.connIdle", "levels.0.uplinkOnly", "levels.0.downlinkOnly"},
	"other":    {},
}

// xraySpecByID возвращает описание секции по идентификатору.
func xraySpecByID(id string) (XraySpec, bool) {
	for _, s := range XraySpecs {
		if s.ID == id {
			return s, true
		}
	}
	return XraySpec{}, false
}

// xrayStatsCounters — счётчики policy.system, которые закрепляет панель (D-10):
// без них не работает «Трафик».
var xrayStatsCounters = []string{
	"statsInboundUplink",
	"statsInboundDownlink",
	"statsOutboundUplink",
	"statsOutboundDownlink",
}

// forceProxyPrefix — префикс тегов входящих force-proxy (D-12).
const forceProxyPrefix = "force-proxy"

// xrayReservedOther — ключи верхнего уровня, которые в «Прочие ключи» не входят:
// их ведут другие части панели, а ключ transport запрещён в корне каталога.
// Ключи собственных секций (log, dns, inbounds, policy) определяет
// configlayer.XraySectionOfKey.
var xrayReservedOther = []string{"routing", "api", "observatory", "burstObservatory", "outbounds", "transport"}

// xrayInbounds разбирает значение секции «Входящие» как массив объектов; ok=false —
// значение не массив или в нём не только объекты.
func xrayInbounds(value json.RawMessage) (items []json.RawMessage, ok bool) {
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, false
	}
	for _, it := range items {
		t := bytes.TrimSpace(it)
		if len(t) == 0 || t[0] != '{' {
			return nil, false
		}
	}
	return items, true
}

// xrayInboundTag возвращает тег входящего.
func xrayInboundTag(raw json.RawMessage) string {
	var in struct {
		Tag string `json:"tag"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	return in.Tag
}

// XrayKeepTags возвращает теги входящих, которые читает XKeen: прозрачные
// (redirect, tproxy; вид определяет configlayer.IsTransparentInbound) и
// force-proxy* (143 Q7). Порядок — как в значении, без повторов.
func XrayKeepTags(value json.RawMessage) []string {
	items, ok := xrayInbounds(value)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range items {
		tag := xrayInboundTag(it)
		if tag == "" || slices.Contains(out, tag) {
			continue
		}
		if _, transparent := configlayer.IsTransparentInbound(it); transparent || strings.HasPrefix(tag, forceProxyPrefix) {
			out = append(out, tag)
		}
	}
	return out
}

// pinnedInboundPaths — поля входящего, которые читает XKeen и которых панель
// не меняет: правится только порт.
var pinnedInboundPaths = [][]string{
	{"protocol"},
	{"settings", "followRedirect"},
	{"settings", "network"},
	{"streamSettings", "sockopt", "tproxy"},
}

// XrayPinned возвращает закреплённые значения секции id (D-10): для policy —
// четыре счётчика policy.system, для inbounds — protocol, settings.followRedirect,
// settings.network и streamSettings.sockopt.tproxy каждого входящего из
// XrayKeepTags (если поле есть). Остальные секции ничего не закрепляют.
func XrayPinned(id string, value json.RawMessage) []configlayer.PinnedValue {
	switch id {
	case "policy":
		out := make([]configlayer.PinnedValue, 0, len(xrayStatsCounters))
		for _, c := range xrayStatsCounters {
			out = append(out, configlayer.PinnedValue{Key: "system." + c, Value: "true"})
		}
		return out
	case "inbounds":
		items, ok := xrayInbounds(value)
		if !ok {
			return nil
		}
		var out []configlayer.PinnedValue
		for _, tag := range XrayKeepTags(value) {
			for _, it := range items {
				if xrayInboundTag(it) != tag {
					continue
				}
				out = append(out, pinnedInboundValues(tag, it)...)
				break
			}
		}
		return out
	}
	return nil
}

// pinnedInboundValues достаёт закреплённые поля одного входящего.
func pinnedInboundValues(tag string, raw json.RawMessage) []configlayer.PinnedValue {
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []configlayer.PinnedValue
	for _, path := range pinnedInboundPaths {
		var cur any = doc
		for _, k := range path {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			if cur, ok = m[k]; !ok {
				cur = nil
				break
			}
		}
		if cur == nil {
			continue
		}
		out = append(out, configlayer.PinnedValue{Key: tag + "." + strings.Join(path, "."), Value: pinnedText(cur)})
	}
	return out
}

// pinnedText записывает значение закреплённого поля строкой.
func pinnedText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
