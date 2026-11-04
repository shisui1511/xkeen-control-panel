package routing

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DNSGroupNameURLSafe сообщает, что имя группы можно вставить в адрес DoH Mihomo
// после `#` (Pitfall 5): `#` начал бы новый суффикс, `&` добавил бы параметры
// сервера, `?` и `/` ломают разбор адреса, пробельные и управляющие символы
// недопустимы, как и байты не в UTF-8. Пустое имя недопустимо; кириллица и
// прочие буквы допустимы (кириллицу в суффиксе подтверждает генератор Mihomo).
// Правило одно для проверки слоя (dns_group_name_invalid), предпроверки
// включения перехвата (group_name_invalid) и генератора Mihomo: имя отвергается
// кодом, а не экранируется (T-148-56).
func DNSGroupNameURLSafe(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "#&?/") {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
