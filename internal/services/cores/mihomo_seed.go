package cores

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Перенос профиля Mihomo по секциям панели (D-06, D-26, MIHO-01) и сборка частей
// профиля панели из секций. Профиль панели собирает routing/mihomo_gen.go: части
// из MihomoProfileParts дописываются в корень, затем идёт маршрутизация.

// mihomoPartOrder — порядок частей профиля панели.
var mihomoPartOrder = []string{mihomoMain, mihomoSniffer, mihomoDNS, mihomoInbounds, mihomoOther}

// mihomoIsNull сообщает, что значение JSON — null.
func mihomoIsNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// mihomoIsObject сообщает, что значение JSON — объект.
func mihomoIsObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{'
}

// mihomoHasTransparentListener сообщает, есть ли среди listeners входящий типа
// redir или tproxy: порт прозрачного прокси XKeen берёт из него (143 Q7), а
// порт из базы привязался бы второй раз.
func mihomoHasTransparentListener(listeners json.RawMessage) bool {
	var list []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(listeners, &list) != nil {
		return false
	}
	for _, l := range list {
		if l.Type == "redir" || l.Type == "tproxy" {
			return true
		}
	}
	return false
}

// SeedMihomo раскладывает ключи верхнего уровня профиля по секциям панели.
// Маршрутизация (proxies, proxy-groups, rules, providers) и закреплённые ключи
// (routing-mark, external-controller-unix, profile.store-selected) не
// переносятся; подключи profile, кроме store-selected, уходят в «Основное»;
// всё, что не относится к другим секциям (hosts, ntp, sub-rules, secret и др.),
// попадает в «Прочие ключи» в прежнем порядке. Встроенная база дополняет только
// отсутствующие ключи (D-06). Пустой профиль или nil — все секции из пресетов.
// Результат: id секции → значение; секций всегда пять.
func SeedMihomo(profile []byte, ports configlayer.MihomoPorts) (map[string]json.RawMessage, error) {
	top := configlayer.NewOrderedObject()
	root, _, err := parseYAMLRoot(profile)
	if err != nil {
		return nil, fmt.Errorf("профиль Mihomo: %w", err)
	}
	if root != nil {
		if root.Kind != yaml.MappingNode {
			return nil, errors.New("профиль Mihomo: корень документа должен быть отображением")
		}
		raw, err := YAMLToOrderedJSON(root)
		if err != nil {
			return nil, fmt.Errorf("профиль Mihomo: %w", err)
		}
		if top, err = configlayer.ParseOrderedObject(raw); err != nil {
			return nil, fmt.Errorf("профиль Mihomo: %w", err)
		}
	}

	mainObj := configlayer.NewOrderedObject()
	profileRest := configlayer.NewOrderedObject()
	inbounds := configlayer.NewOrderedObject()
	other := configlayer.NewOrderedObject()
	var dnsVal, snifferVal json.RawMessage

	for _, key := range top.Keys() {
		val, _ := top.Get(key)
		switch mihomoSectionOfKey(key) {
		case "":
			// Маршрутизация и закреплённое ведёт панель.
		case mihomoMain:
			if key != "profile" {
				mainObj.Set(key, val)
				continue
			}
			// Подключи profile, кроме закреплённого store-selected, остаются в
			// «Основном»; profile не объект (или пуст) — переносить нечего.
			if mihomoIsObject(val) {
				if po, err := configlayer.ParseOrderedObject(val); err == nil {
					for _, pk := range po.Keys() {
						if pk == "store-selected" {
							continue
						}
						pv, _ := po.Get(pk)
						profileRest.Set(pk, pv)
					}
				}
			}
		case mihomoDNS:
			// dns: без значения (или не объект) — остаётся пресет.
			if mihomoIsObject(val) {
				dnsVal = val
			}
		case mihomoSniffer:
			if mihomoIsObject(val) {
				snifferVal = val
			}
		case mihomoInbounds:
			if !mihomoIsNull(val) {
				inbounds.Set(key, val)
			}
		default:
			other.Set(key, val)
		}
	}

	// База дополняет только отсутствующие ключи (D-06).
	listeners, _ := inbounds.Get("listeners")
	transparent := mihomoHasTransparentListener(listeners)
	if preset, err := configlayer.ParseOrderedObject(MihomoPreset(mihomoMain, ports)); err == nil {
		for _, k := range preset.Keys() {
			if _, has := mainObj.Get(k); has {
				continue
			}
			if transparent && (k == "redir-port" || k == "tproxy-port") {
				continue
			}
			v, _ := preset.Get(k)
			mainObj.Set(k, v)
		}
	}
	if profileRest.Len() > 0 {
		mainObj.Set("profile", mihomoObjectRaw(profileRest))
	}
	if dnsVal == nil {
		dnsVal = MihomoPreset(mihomoDNS, ports)
	}
	if snifferVal == nil {
		snifferVal = MihomoPreset(mihomoSniffer, ports)
	}

	return map[string]json.RawMessage{
		mihomoMain:     mihomoObjectRaw(mainObj),
		mihomoDNS:      dnsVal,
		mihomoInbounds: mihomoObjectRaw(inbounds),
		mihomoSniffer:  snifferVal,
		mihomoOther:    mihomoObjectRaw(other),
	}, nil
}

// mihomoSectionValue возвращает значение секции для профиля: взятое под
// управление или, если секции нет, пресет.
func mihomoSectionValue(cores map[string]configlayer.CoreSection, id string, ports configlayer.MihomoPorts) json.RawMessage {
	cs := cores[configlayer.CoreSectionName(configlayer.KernelMihomo, id)]
	if cs.Managed && len(bytes.TrimSpace(cs.Value)) > 0 {
		return cs.Value
	}
	return MihomoPreset(id, ports)
}

// mihomoObjectOf разбирает значение секции как объект.
func mihomoObjectOf(id string, value json.RawMessage) (*configlayer.OrderedObject, error) {
	o, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, fmt.Errorf("секция mihomo_%s: ожидался объект", id)
	}
	return o, nil
}

// mihomoAppendKeys дописывает ключи объекта в отображение, пропуская skip.
func mihomoAppendKeys(part *yaml.Node, o *configlayer.OrderedObject, skip func(string) bool) error {
	for _, k := range o.Keys() {
		if skip != nil && skip(k) {
			continue
		}
		v, _ := o.Get(k)
		vn, err := OrderedJSONToYAML(v)
		if err != nil {
			return fmt.Errorf("ключ %s: %w", k, err)
		}
		part.Content = append(part.Content, yamlStrNode(k), vn)
	}
	return nil
}

// MihomoProfileParts возвращает пять частей профиля панели в порядке main,
// sniffer, dns, inbounds, other. Каждая часть — отображение, чьё содержимое
// дописывается в корень профиля. Секция, которой нет в состоянии или которая
// не под управлением, берётся из пресета (с портами ports). В main закреплённые
// значения (routing-mark, external-controller-unix панели, profile.store-selected)
// дописываются после ключей секции; закреплённые ключи из значений не берутся.
func MihomoProfileParts(s configlayer.Sections, ports configlayer.MihomoPorts) (parts []*yaml.Node, err error) {
	cores := configlayer.DesiredCores(s)
	for _, id := range mihomoPartOrder {
		value := mihomoSectionValue(cores, id, ports)
		part := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		switch id {
		case mihomoMain:
			obj, err := mihomoObjectOf(id, value)
			if err != nil {
				return nil, err
			}
			skip := func(k string) bool { return k == "profile" || mihomoPinnedTop[k] }
			if err := mihomoAppendKeys(part, obj, skip); err != nil {
				return nil, fmt.Errorf("секция mihomo_%s: %w", id, err)
			}
			part.Content = append(part.Content,
				yamlStrNode("routing-mark"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(mihomoPanelRoutingMark)},
				yamlStrNode("external-controller-unix"), yamlStrNode(services.DefaultMihomoSocketPath),
			)
			prof := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if pv, ok := obj.Get("profile"); ok && mihomoIsObject(pv) {
				po, perr := mihomoObjectOf(id, pv)
				if perr != nil {
					return nil, perr
				}
				if err := mihomoAppendKeys(prof, po, func(k string) bool { return k == "store-selected" }); err != nil {
					return nil, fmt.Errorf("секция mihomo_%s: profile: %w", id, err)
				}
			}
			prof.Content = append(prof.Content,
				yamlStrNode("store-selected"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			part.Content = append(part.Content, yamlStrNode("profile"), prof)
		case mihomoSniffer, mihomoDNS:
			if !mihomoIsObject(value) {
				return nil, fmt.Errorf("секция mihomo_%s: ожидался объект", id)
			}
			vn, err := OrderedJSONToYAML(value)
			if err != nil {
				return nil, fmt.Errorf("секция mihomo_%s: %w", id, err)
			}
			part.Content = append(part.Content, yamlStrNode(id), vn)
		default:
			obj, err := mihomoObjectOf(id, value)
			if err != nil {
				return nil, err
			}
			// Маршрутизацию и закреплённое ведёт генератор: проверка до записи
			// такие ключи отклоняет, здесь они не должны задвоиться.
			skip := func(k string) bool { return mihomoRoutingKeys[k] || mihomoPinnedTop[k] }
			if err := mihomoAppendKeys(part, obj, skip); err != nil {
				return nil, fmt.Errorf("секция mihomo_%s: %w", id, err)
			}
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// MihomoListenerNames возвращает имена listeners секции «Входящие» (если она под
// управлением) в порядке значения; по ним генератор проверяет ссылки правил.
func MihomoListenerNames(s configlayer.Sections) []string {
	cs := configlayer.DesiredCores(s)[configlayer.SectionMihomoInbounds]
	if !cs.Managed {
		return nil
	}
	obj, err := configlayer.ParseOrderedObject(cs.Value)
	if err != nil {
		return nil
	}
	raw, ok := obj.Get("listeners")
	if !ok {
		return nil
	}
	var list []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []string
	for _, l := range list {
		if l.Name != "" {
			out = append(out, l.Name)
		}
	}
	return out
}
