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

// MaskText маскирует секреты в тексте диагностики: сначала перечисленные
// секреты узлов (от шести символов) заменяются на MaskSecret, затем адреса
// http(s) — на MaskURL, затем применяется общая очистка
// services.RedactSensitiveText (пары password=/token=, ключи, LAN-адреса).
func MaskText(text string, secrets []string) string {
	uniq := make(map[string]bool, len(secrets))
	list := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if utf8.RuneCountInString(s) >= minMaskedSecret && !uniq[s] {
			uniq[s] = true
			list = append(list, s)
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
	return services.RedactSensitiveText(text)
}
