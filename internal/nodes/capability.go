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
	return Verdict{OK: true}
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
	name := n.Name
	if name == "" {
		name = "probe"
	}
	if _, err := ClashProxy(n, name); err != nil {
		return fromError(err)
	}
	return Verdict{OK: true}
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
