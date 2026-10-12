package nodes

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Маскирование для API, журналов и диагностики (D-33, D-34). Функции не
// логируют и не возвращают исходных значений в ошибках.

const maskEllipsis = "…"

// minMaskedSecret — секреты короче этого значения в тексте не заменяются:
// короткая строка дала бы ложные совпадения внутри обычных слов.
const minMaskedSecret = 6

// maskedPrefix — сколько первых символов остаётся от секрета и сегмента пути.
const maskedPrefix = 4

var textURLRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

// encodedLinkRe — ссылки, у которых учётные данные лежат в base64: весь блок
// vmess (JSON) и ssr, userinfo ss в SIP002 или ss целиком в старом формате.
// Буквальной замены секретов они избегают, поэтому маскируются по форме.
var encodedLinkRe = regexp.MustCompile(`(?i)\b(vmess|ssr|ss)://([A-Za-z0-9+/=_-]+)`)

// maskEncodedLinks заменяет закодированную часть ссылок vmess, ssr и ss на
// MaskSecret. У ss в открытом виде «метод:пароль@…» маскировать по форме нечего:
// пароль заменяется буквально как секрет узла.
func maskEncodedLinks(text string) string {
	idx := encodedLinkRe.FindAllStringSubmatchIndex(text, -1)
	if len(idx) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, m := range idx {
		start, end := m[4], m[5]
		if strings.EqualFold(text[m[2]:m[3]], "ss") && end < len(text) && text[end] == ':' {
			continue
		}
		b.WriteString(text[last:start])
		b.WriteString(MaskSecret(text[start:end]))
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

// linkUserinfoRe — userinfo любой ссылки «схема://секрет@хост». Строка, которая не
// стала узлом (неизвестная схема, ошибка разбора), своих секретов в список не
// даёт, поэтому userinfo маскируется по форме ссылки.
var linkUserinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)([^\s@/?#"'<>]+)@`)

// maskLinkUserinfo заменяет userinfo ссылок на MaskSecret. У ss в открытом виде
// «метод:пароль» остаётся метод: секрет — только пароль. У остальных схем
// userinfo скрывается целиком: двоеточие может быть частью пароля.
func maskLinkUserinfo(text string) string {
	idx := linkUserinfoRe.FindAllStringSubmatchIndex(text, -1)
	if len(idx) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, m := range idx {
		start, end := m[4], m[5]
		info := text[start:end]
		b.WriteString(text[last:start])
		method, secret, ok := strings.Cut(info, ":")
		if ok && method != "" && secret != "" && strings.EqualFold(text[m[2]:m[3]], "ss://") {
			b.WriteString(method + ":" + MaskSecret(secret))
		} else {
			b.WriteString(MaskSecret(info))
		}
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

// escapedForms — формы секрета, в которых он встречается внутри ссылок
// (percent-кодирование userinfo и параметров запроса), кроме самого секрета.
func escapedForms(s string) []string {
	var out []string
	userinfo := strings.TrimPrefix(url.UserPassword("u", s).String(), "u:")
	for _, e := range []string{url.QueryEscape(s), url.PathEscape(s), userinfo} {
		if e != s {
			out = append(out, e)
		}
	}
	return out
}

// MaskURL оставляет схему, домен и маску пути вида «домен/…/ab12…»: последний
// непустой сегмент пути сокращается до четырёх символов. Запрос, фрагмент,
// userinfo и путь целиком не выводятся. Неразбираемый адрес даёт «…».
func MaskURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return maskEllipsis
	}
	base := u.Scheme + "://" + u.Host
	segments := strings.Split(u.Path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i] != "" {
			return base + "/" + maskEllipsis + "/" + MaskSecret(segments[i])
		}
	}
	return base + "/" + maskEllipsis
}

// MaskSecret оставляет первые четыре символа и «…»; значения из четырёх
// символов и короче скрываются целиком, пустое остаётся пустым.
func MaskSecret(s string) string {
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maskedPrefix {
		return maskEllipsis
	}
	return string([]rune(s)[:maskedPrefix]) + maskEllipsis
}

// Secrets — секреты узла, которые нельзя выводить: UUID, пароль, ключи
// WireGuard, пароль обфускации hysteria2, shortId Reality и значения password
// и psk параметров плагина. Пустые значения не перечисляются.
func Secrets(n Node) []string {
	var out []string
	add := func(s string) {
		if s != "" {
			out = append(out, s)
		}
	}
	add(n.UUID)
	add(n.Password)
	if n.WG != nil {
		add(n.WG.PrivateKey)
		add(n.WG.PreSharedKey)
	}
	if n.Hy2 != nil {
		add(n.Hy2.ObfsPassword)
	}
	if n.Reality != nil {
		add(n.Reality.ShortID)
	}
	keys := make([]string, 0, len(n.PluginOpts))
	for k := range n.PluginOpts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := n.PluginOpts[k].(string); ok && (strings.EqualFold(k, "password") || strings.EqualFold(k, "psk")) {
			add(s)
		}
	}
	return out
}

// MaskText маскирует секреты в тексте диагностики: сначала закодированная часть
// ссылок vmess, ssr и ss (учётные данные в base64), затем перечисленные секреты
// узлов (от шести символов) — сами и в percent-кодировании — заменяются на
// MaskSecret, затем адреса http(s) — на MaskURL, затем userinfo любой ссылки
// «схема://секрет@хост» (узел мог не разобраться, и его секретов нет в списке),
// затем применяется общая очистка services.RedactSensitiveText (пары
// password=/token=, ключи, LAN-адреса).
func MaskText(text string, secrets []string) string {
	text = maskEncodedLinks(text)
	uniq := make(map[string]bool, len(secrets))
	list := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if utf8.RuneCountInString(s) < minMaskedSecret {
			continue
		}
		for _, v := range append([]string{s}, escapedForms(s)...) {
			if !uniq[v] {
				uniq[v] = true
				list = append(list, v)
			}
		}
	}
	// Длинные секреты первыми: короткий не должен разрезать длинный.
	sort.Slice(list, func(i, j int) bool {
		if len(list[i]) != len(list[j]) {
			return len(list[i]) > len(list[j])
		}
		return list[i] < list[j]
	})
	for _, s := range list {
		text = strings.ReplaceAll(text, s, MaskSecret(s))
	}
	text = textURLRe.ReplaceAllStringFunc(text, func(m string) string {
		// Знаки препинания в конце не часть адреса.
		trimmed := strings.TrimRight(m, ".,;:)]}")
		return MaskURL(trimmed) + m[len(trimmed):]
	})
	// После адресов http(s): их userinfo MaskURL уже убрал, а маска внутри
	// userinfo сломала бы разбор адреса.
	text = maskLinkUserinfo(text)
	return services.RedactSensitiveText(text)
}
