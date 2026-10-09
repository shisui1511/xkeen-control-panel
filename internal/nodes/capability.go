package nodes

import (
	"errors"
	"strings"
)

// Матрица возможностей ядер (SRC-03, SRC-06): отвечает на вопрос «в какое ядро
// уходит узел и почему нет». Пакет не знает версий установленных ядер: пороги
// приходят параметром Caps, которые считает вызывающий код.

// Имена ядер в Support. Строковые литералы, без зависимости от configlayer.
const (
	kernelXray   = "xray"
	kernelMihomo = "mihomo"
)

// Gate — доступность функции в установленном ядре: OK и минимальная версия
// ядра для сообщения пользователю (параметр kernel_min).
type Gate struct {
	OK  bool
	Min string
}

// Caps — версионные ворота функций обоих ядер. Нулевое значение — «ничего не
// доступно»; неустановленное ядро оценивается по CapsLatest.
type Caps struct {
	XrayXHTTP        Gate
	XraySessionID    Gate
	MihomoXHTTP      Gate
	MihomoXHTTPReuse Gate
	MihomoAWG        Gate
	MihomoAWG15      Gate
	MihomoAWG20      Gate
	MihomoAWG3       Gate
}

// CapsLatest — «последняя известная версия» обоих ядер: все функции доступны,
// пороги заполнены значениями по умолчанию. Метки «только <ядро>» для
// неустановленного ядра от этого стабильны.
func CapsLatest() Caps {
	return Caps{
		XrayXHTTP:        Gate{OK: true, Min: "24.11.21"},
		XraySessionID:    Gate{OK: true, Min: "26.6.22"},
		MihomoXHTTP:      Gate{OK: true, Min: "1.19.22"},
		MihomoXHTTPReuse: Gate{OK: true, Min: "1.19.23"},
		MihomoAWG:        Gate{OK: true, Min: "1.18.9"},
		MihomoAWG15:      Gate{OK: true, Min: "1.19.13"},
		MihomoAWG20:      Gate{OK: true, Min: "1.19.14"},
		MihomoAWG3:       Gate{OK: true, Min: "1.19.30"},
	}
}

// Verdict — ответ матрицы для одного узла и одного ядра. Уходит клиенту в
// предпросмотре и представлении, поэтому ключи JSON заданы явно.
type Verdict struct {
	OK     bool              `json:"ok"`
	Code   string            `json:"code,omitempty"`
	Params map[string]string `json:"params,omitempty"`
	Notes  []string          `json:"notes,omitempty"`
}

// Support отвечает, выражается ли узел в ядре kernel ("xray" или "mihomo").
// Вердикт OK выдаётся только после успешной пробной эмиссии теми же
// эмиттерами, которые пишут файл ядра: узел, который нельзя выразить, никогда
// не помечен поддерживаемым. Версионные ворота идут до эмиссии, ошибка эмиттера
// становится вердиктом с тем же кодом и параметрами.
func Support(n Node, kernel string, c Caps) Verdict {
	switch kernel {
	case kernelXray:
		return supportXray(n, c)
	case kernelMihomo:
		return supportMihomo(n, c)
	}
	return Verdict{Code: CodeUnsupportedType, Params: map[string]string{"kernel": kernel}}
}

func supportXray(n Node, c Caps) Verdict {
	// Запреты Xray 26.x не зависят от происхождения узла: у неродного узла их
	// отсекает проверка по IR, у родного (оригинал уходит ядру как есть) — пробная
	// эмиссия через checkXrayRaw: allowInsecure даже с закреплением, h2/http/quic,
	// httpSettings, Host в заголовках xhttp, отпечаток вне таблиц Xray. Вердикт OK
	// для оригинала невозможен без успешной проверки оригинала.
	if !nativeXray(n) && reachesXrayStream(n) {
		if network := normNetwork(n.Transport.Network); isRemovedTransport(network) {
			return fromError(NewError(CodeXrayNoHTTPTransport, "network", network))
		}
		if xrayInsecureForbidden(n) {
			return fromError(NewError(CodeXrayNoInsecure))
		}
	}
	if normNetwork(n.Transport.Network) == "xhttp" {
		if !c.XrayXHTTP.OK {
			return needs(CodeNeedsXray, c.XrayXHTTP)
		}
		if hasXHTTPField(n.Transport.XHTTP, isSessionIDField) && !c.XraySessionID.OK {
			return needs(CodeNeedsXray, c.XraySessionID)
		}
	}
	if _, err := XrayOutbound(n, "xcp-probe"); err != nil {
		return fromError(err)
	}
	return Verdict{OK: true, Notes: levelBNotes(n, kernelXray)}
}

func supportMihomo(n Node, c Caps) Verdict {
	if normNetwork(n.Transport.Network) == "xhttp" {
		if !c.MihomoXHTTP.OK {
			return needs(CodeNeedsMihomo, c.MihomoXHTTP)
		}
		if hasXHTTPField(n.Transport.XHTTP, isReuseField) && !c.MihomoXHTTPReuse.OK {
			return needs(CodeNeedsMihomo, c.MihomoXHTTPReuse)
		}
	}
	if n.WG != nil && (len(n.WG.AWG) > 0 || n.WG.AWGVersion != "") {
		if g := awgGate(n.WG.AWGVersion, c); !g.OK {
			return needs(CodeNeedsMihomo, g)
		}
	}
	name := n.Name
	if name == "" {
		name = "probe"
	}
	if _, err := ClashProxy(n, name); err != nil {
		return fromError(err)
	}
	return Verdict{OK: true, Notes: levelBNotes(n, kernelMihomo)}
}

// nativeXray — узел из Xray-JSON с оригиналом: ядро получает копию Raw.
func nativeXray(n Node) bool { return n.Origin == OriginXrayJSON && n.Raw != nil }

// reachesXrayStream — узел идёт через streamSettings эмиттера Xray (протоколы
// с транспортом и TLS), а не отклоняется раньше по типу: IR-проверки транспорта
// и TLS применимы только к нему, иначе они подменили бы код отказа эмиттера.
func reachesXrayStream(n Node) bool {
	switch normProtocol(n.Protocol) {
	case "vless", "vmess", "trojan", "socks", "http":
		return true
	case "shadowsocks":
		return n.Plugin == ""
	}
	return false
}

// awgGate выбирает ворота Mihomo по версии диалекта AmneziaWG: 1.0 (классика и
// версия не указана), 1.5, 2.0, 3.x.
func awgGate(version string, c Caps) Gate {
	v := strings.TrimSpace(version)
	switch {
	case strings.HasPrefix(v, "3"):
		return c.MihomoAWG3
	case strings.HasPrefix(v, "2"):
		return c.MihomoAWG20
	case v == "1.5":
		return c.MihomoAWG15
	}
	return c.MihomoAWG
}

// levelBNotes — примечания уровня B (A13, D-20): узел работает, но при переводе
// в это ядро теряется функция. Вердикт не меняют, считаются при каждой
// генерации и в состоянии не хранятся. Родное ядро узла получает оригинал
// без потерь, поэтому для него примечаний нет.
func levelBNotes(n Node, kernel string) []string {
	if kernel == kernelXray && nativeXray(n) || kernel == kernelMihomo && n.Origin == OriginClash && n.Raw != nil {
		return nil
	}
	var notes []string
	if kernel == kernelXray {
		if n.UDP != nil && !*n.UDP {
			notes = append(notes, CodeNoteUDPDropped)
		}
		if n.PacketEncoding != "" {
			notes = append(notes, CodeNotePacketEncDropped)
		}
	}
	if kernel == kernelMihomo && n.Reality != nil && n.Reality.SpiderX != "" {
		notes = append(notes, CodeNoteSpiderXDropped)
	}
	if n.TLS != nil && fingerprintEmitted(n, kernel) {
		reality := n.Reality != nil
		var replaced bool
		if kernel == kernelXray {
			_, replaced = XrayFingerprint(n.TLS.Fingerprint, reality)
		} else {
			_, replaced = MihomoFingerprint(n.TLS.Fingerprint, reality)
		}
		if replaced {
			notes = append(notes, CodeNoteFingerprintDropped)
		}
	}
	return notes
}

// fingerprintEmitted — эмиттер ядра пишет отпечаток uTLS этого узла: TLS или
// Reality у протоколов с транспортом (у hysteria2, tuic и wireguard отпечатка нет).
func fingerprintEmitted(n Node, kernel string) bool {
	if !n.TLS.Enabled && n.Reality == nil {
		return false
	}
	switch normProtocol(n.Protocol) {
	case "vless", "vmess", "trojan":
		return true
	case "shadowsocks", "socks", "http":
		return kernel == kernelXray
	}
	return false
}

// needs — вердикт «нужна более новая версия ядра» с порогом из ворот.
func needs(code string, g Gate) Verdict {
	v := Verdict{Code: code}
	if g.Min != "" {
		v.Params = map[string]string{"kernel_min": g.Min}
	}
	return v
}

// fromError превращает отказ эмиттера в вердикт с тем же кодом и параметрами.
// Не *SourceError — «неизвестный тип»: узел всё равно не материализуется.
func fromError(err error) Verdict {
	var se *SourceError
	if !errors.As(err, &se) || se == nil {
		return Verdict{Code: CodeUnsupportedType}
	}
	v := Verdict{Code: se.Code}
	if len(se.Params) > 0 {
		v.Params = make(map[string]string, len(se.Params))
		for k, val := range se.Params {
			v.Params[k] = val
		}
	}
	return v
}

// hasXHTTPField — есть ли в полях xhttp ключ, для которого pred вернул true.
// Имя ключа приводится к виду Xray или Mihomo через таблицу соответствия.
func hasXHTTPField(fields map[string]any, pred func(k string) bool) bool {
	for k := range fields {
		if pred(k) {
			return true
		}
	}
	return false
}

// isSessionIDField — поля sessionID* (Xray ≥ 26.6.22) и session-* (Mihomo).
func isSessionIDField(k string) bool {
	f, ok := xhttpByName[normKey(k)]
	return ok && strings.HasPrefix(f.xray, "sessionID")
}

// isReuseField — вложенные настройки мультиплексирования (xmux / reuse-settings).
func isReuseField(k string) bool {
	switch normKey(k) {
	case "xmux", "reusesettings":
		return true
	}
	return false
}
