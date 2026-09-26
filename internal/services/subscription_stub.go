package services

import (
	"net"
	"strconv"
	"strings"
)

// Признаки узла-заглушки провайдера. Провайдер, отклонивший устройство
// (например, из-за лимита HWID), вместо рабочих узлов отдаёт фиктивные:
// адрес 0.0.0.0, порт 1 и нулевой UUID. Имя узла не анализируется.
const (
	zeroUUID = "00000000-0000-0000-0000-000000000000"

	stubReasonAddress = "address"
	stubReasonPort    = "port"
	stubReasonUUID    = "uuid"
)

// isStubHost сообщает, что адрес заведомо нерабочий: пустой, unspecified или loopback IPv4.
func isStubHost(host string) bool {
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "127.0.0.1":
		return true
	}
	return false
}

// firstMapItem возвращает первый элемент списка объектов из settings[key].
func firstMapItem(settings map[string]interface{}, key string) map[string]interface{} {
	if settings == nil {
		return nil
	}
	raw, ok := settings[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		if len(v) > 0 {
			m, _ := v[0].(map[string]interface{})
			return m
		}
	case []map[string]interface{}:
		if len(v) > 0 {
			return v[0]
		}
	}
	return nil
}

// portFromValue разбирает порт из float64, int или строки.
func portFromValue(raw interface{}) (int, bool) {
	switch p := raw.(type) {
	case float64:
		return int(p), true
	case int:
		return p, true
	case int64:
		return int(p), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// endpointFromMap читает address/port из объекта настроек.
func endpointFromMap(m map[string]interface{}) (host string, port int, hasHost bool, hasPort bool) {
	if m == nil {
		return "", 0, false, false
	}
	if raw, ok := m["address"]; ok {
		if s, isStr := raw.(string); isStr {
			host, hasHost = s, true
		}
	}
	if raw, ok := m["port"]; ok {
		if p, okPort := portFromValue(raw); okPort {
			port, hasPort = p, true
		}
	}
	return host, port, hasHost, hasPort
}

// outboundEndpoint читает адрес и порт из известных мест настроек outbound:
// vnext[0], servers[0], плоские settings.address/port и peers[0].endpoint.
// hasHost/hasPort равны true, только если ключ реально присутствует.
func outboundEndpoint(ob *Outbound) (host string, port int, hasHost bool, hasPort bool) {
	if ob == nil || ob.Settings == nil {
		return "", 0, false, false
	}
	for _, key := range []string{"vnext", "servers"} {
		if m := firstMapItem(ob.Settings, key); m != nil {
			if h, p, hh, hp := endpointFromMap(m); hh || hp {
				return h, p, hh, hp
			}
		}
	}
	if h, p, hh, hp := endpointFromMap(ob.Settings); hh || hp {
		return h, p, hh, hp
	}
	if peer := firstMapItem(ob.Settings, "peers"); peer != nil {
		if ep, ok := peer["endpoint"].(string); ok && ep != "" {
			if h, pStr, err := net.SplitHostPort(ep); err == nil {
				p, okPort := portFromValue(pStr)
				return h, p, true, okPort
			}
			return strings.Trim(ep, "[]"), 0, true, false
		}
	}
	return "", 0, false, false
}

// outboundCredentials собирает идентификаторы доступа outbound, в которых
// провайдер мог оставить нулевой UUID.
func outboundCredentials(ob *Outbound) []string {
	if ob == nil || ob.Settings == nil {
		return nil
	}
	var creds []string
	add := func(v interface{}) {
		if s, ok := v.(string); ok && s != "" {
			creds = append(creds, s)
		}
	}
	if vn := firstMapItem(ob.Settings, "vnext"); vn != nil {
		if user := firstMapItem(vn, "users"); user != nil {
			add(user["id"])
		}
	}
	add(ob.Settings["id"])
	if srv := firstMapItem(ob.Settings, "servers"); srv != nil {
		add(srv["uuid"])
		add(srv["password"])
	}
	return creds
}

// outboundStubReason возвращает причину, по которой outbound является
// заглушкой провайдера, либо "" для рабочего узла. Outbound без
// распознаваемого адреса заглушкой не считается: ложных срабатываний нет.
func outboundStubReason(ob *Outbound) string {
	if ob == nil {
		return ""
	}
	host, port, hasHost, hasPort := outboundEndpoint(ob)
	if hasHost && isStubHost(host) {
		return stubReasonAddress
	}
	if hasPort && port <= 1 {
		return stubReasonPort
	}
	for _, cred := range outboundCredentials(ob) {
		if strings.EqualFold(strings.TrimSpace(cred), zeroUUID) {
			return stubReasonUUID
		}
	}
	return ""
}

// countXrayWorkingAndStubs считает рабочие узлы (поддерживаемый Xray протокол,
// не заглушка) и заглушки. Outbounds не мутируются.
func countXrayWorkingAndStubs(outbounds []Outbound) (working int, stubs int) {
	for i := range outbounds {
		if outboundStubReason(&outbounds[i]) != "" {
			stubs++
			continue
		}
		if allowedXrayProtocols[outbounds[i].Protocol] {
			working++
		}
	}
	return working, stubs
}
