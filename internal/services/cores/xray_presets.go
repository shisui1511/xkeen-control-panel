package cores

import "encoding/json"

// Пресеты секций Xray (D-04): значения, которые берутся, когда чужого файла
// секции нет. Сырые JSON-литералы: порядок ключей и запись чисел не меняются.
const (
	xrayLogPreset = `{"loglevel":"warning","access":"none"}`
	// DNS как у Xray без секции DNS.
	xrayDNSPreset = `{"servers":["localhost"]}`
	// Обязательная часть прозрачных входящих XKeen (143 Q7): redirect (tcp) и
	// tproxy (udp) на одном порту, dokodemo-door с followRedirect.
	xrayInboundsPreset = `[` +
		`{"tag":"redirect","port":1181,"protocol":"dokodemo-door","settings":{"network":"tcp","followRedirect":true},"sniffing":{"enabled":true,"destOverride":["http","tls"]}},` +
		`{"tag":"tproxy","port":1181,"protocol":"dokodemo-door","settings":{"network":"udp","followRedirect":true},"streamSettings":{"sockopt":{"tproxy":"tproxy"}},"sniffing":{"enabled":true,"destOverride":["http","tls"]}}` +
		`]`
	// Уровень 0 с умолчаниями Xray.
	xrayPolicyPreset = `{"levels":{"0":{"handshake":4,"connIdle":300,"uplinkOnly":2,"downlinkOnly":5}}}`
	xrayOtherPreset  = `{}`
)

// XrayPreset возвращает пресет секции Xray; для неизвестной секции — nil.
func XrayPreset(id string) json.RawMessage {
	switch id {
	case "log":
		return json.RawMessage(xrayLogPreset)
	case "dns":
		return json.RawMessage(xrayDNSPreset)
	case "inbounds":
		return json.RawMessage(xrayInboundsPreset)
	case "policy":
		return json.RawMessage(xrayPolicyPreset)
	case "other":
		return json.RawMessage(xrayOtherPreset)
	}
	return nil
}
