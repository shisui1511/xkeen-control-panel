package cores

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Умолчания DNS-секции ядра при включении перехвата DNS роутера (D-19): пустые
// поля дополняются, значения пользователя не трогаются. Адрес резолвера прошивки
// сюда не входит: его как закреплённое значение панели дописывает генератор из
// живых данных перехвата (D-18), иначе адрес застыл бы в значении пользователя.

var (
	// dnsTakeoverDoH — серверы через группу-цель по умолчанию (D-17) и
	// proxy-server-nameserver Mihomo (D-18): DoH по IP, без имени сервера.
	dnsTakeoverDoH = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	// dnsTakeoverBootstrap — default-nameserver Mihomo: резолверы по IP.
	dnsTakeoverBootstrap = []string{"1.1.1.1", "8.8.8.8"}
)

// dnsTakeoverEnhancedMode — enhanced-mode Mihomo под перехватом (D-12).
const dnsTakeoverEnhancedMode = "redir-host"

// DNSTakeoverDefaults дополняет значение DNS-секции ядра умолчаниями перехвата и
// возвращает новое значение и список дописанных ключей; ключи и их порядок у
// пользователя сохраняются. Mihomo: enable (не задан — true), enhanced-mode
// (пусто — redir-host), nameserver (пусто или пресет из локальных адресов —
// DoH D-17), default-nameserver (пусто — резолверы по IP), proxy-server-nameserver
// (пусто — DoH по IP). Xray: servers (пусто или пресет из локальных адресов —
// DoH D-17). Пресет — секция взята из пресета (cs.Preset) или её список равен
// пресету «Сбросить»: локальный адрес под перехватом вёл бы запрос обратно в
// слушатель ядра. Неизвестное ядро — ошибка.
func DNSTakeoverDefaults(kernel string, cs configlayer.CoreSection) (json.RawMessage, []string, error) {
	obj := configlayer.NewOrderedObject()
	if len(bytes.TrimSpace(cs.Value)) > 0 && !bytes.Equal(bytes.TrimSpace(cs.Value), []byte("null")) {
		parsed, err := configlayer.ParseOrderedObject(cs.Value)
		if err != nil {
			return nil, nil, fmt.Errorf("секция dns %s: %w", kernel, err)
		}
		obj = parsed
	}
	var filled []string
	set := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj.Set(key, raw)
		filled = append(filled, key)
		return nil
	}
	var err error
	switch kernel {
	case configlayer.KernelMihomo:
		preset := mihomoPresetServers()
		if _, ok := obj.Get("enable"); !ok {
			err = set("enable", true)
		}
		if err == nil && emptyValue(obj, "enhanced-mode") {
			err = set("enhanced-mode", dnsTakeoverEnhancedMode)
		}
		if err == nil && serversNeedDefaults(obj, "nameserver", cs.Preset, preset) {
			err = set("nameserver", dnsTakeoverDoH)
		}
		if err == nil && emptyValue(obj, "default-nameserver") {
			err = set("default-nameserver", dnsTakeoverBootstrap)
		}
		if err == nil && emptyValue(obj, "proxy-server-nameserver") {
			err = set("proxy-server-nameserver", dnsTakeoverDoH)
		}
	case configlayer.KernelXray:
		if serversNeedDefaults(obj, "servers", cs.Preset, xrayPresetServers()) {
			err = set("servers", dnsTakeoverDoH)
		}
	default:
		return nil, nil, fmt.Errorf("секция dns: неизвестное ядро %q", kernel)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("секция dns %s: %w", kernel, err)
	}
	out, err := obj.MarshalJSON()
	if err != nil {
		return nil, nil, fmt.Errorf("секция dns %s: %w", kernel, err)
	}
	return out, filled, nil
}

// emptyValue — ключа нет, он null, пустая строка, пустой список или объект.
func emptyValue(obj *configlayer.OrderedObject, key string) bool {
	raw, ok := obj.Get(key)
	if !ok {
		return true
	}
	switch strings.TrimSpace(string(raw)) {
	case "", "null", `""`, "[]", "{}":
		return true
	}
	return false
}

// serversNeedDefaults — список серверов пуст или это пресет из одних локальных
// адресов (секция из пресета или список равен пресету).
func serversNeedDefaults(obj *configlayer.OrderedObject, key string, fromPreset bool, preset []string) bool {
	if emptyValue(obj, key) {
		return true
	}
	raw, _ := obj.Get(key)
	servers, ok := serverAddresses(raw)
	if !ok || len(servers) == 0 {
		return false
	}
	for _, s := range servers {
		if !localServer(s) {
			return false
		}
	}
	return fromPreset || slices.Equal(servers, preset)
}

// serverAddresses — адреса списка серверов: строки или объекты с address.
// ok=false — не список.
func serverAddresses(raw json.RawMessage) ([]string, bool) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, s)
			continue
		}
		var o struct {
			Address string `json:"address"`
		}
		if json.Unmarshal(it, &o) != nil {
			return nil, false
		}
		out = append(out, o.Address)
	}
	return out, true
}

// localServer — сервер на самом роутере: system, localhost, 127.0.0.0/8, ::1.
func localServer(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{"udp://", "tcp://"} {
		s = strings.TrimPrefix(s, p)
	}
	switch {
	case s == "system", s == "localhost", s == "::1", s == "[::1]":
		return true
	case strings.HasPrefix(s, "127."), strings.HasPrefix(s, "localhost:"), strings.HasPrefix(s, "[::1]:"):
		return true
	}
	return false
}

// mihomoPresetServers — nameserver пресета DNS-секции Mihomo.
func mihomoPresetServers() []string {
	var p struct {
		Nameserver []string `json:"nameserver"`
	}
	_ = json.Unmarshal(MihomoPreset(mihomoDNS, configlayer.MihomoPorts{}), &p)
	return p.Nameserver
}

// xrayPresetServers — servers пресета DNS-секции Xray.
func xrayPresetServers() []string {
	var p struct {
		Servers []string `json:"servers"`
	}
	_ = json.Unmarshal(XrayPreset("dns"), &p)
	return p.Servers
}
