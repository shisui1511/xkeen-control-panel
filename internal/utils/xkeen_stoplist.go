package utils

import "strings"

// XKeenStoplistWords — слова стоп-списка XKeen в порядке проверки скриптом
// запуска: bak, old, copy, копия, orig, save, temp, tmp.
var XKeenStoplistWords = []string{"bak", "old", "copy", "копия", "orig", "save", "temp", "tmp"}

// XKeenParenWord — псевдослово для шаблона `*(*).json`, у которого нет
// буквенного слова.
const XKeenParenWord = "()"

const xkeenJSONSuffix = ".json"

// foldXKeen сворачивает регистр явной таблицей: ASCII A–Z, кириллица А–Я и Ё.
// Остальные символы остаются как есть. Явная таблица вместо unicode.ToLower
// даёт одинаковое поведение в Go и TypeScript; в слове «копия» составных
// символов нет, поэтому Unicode-нормализация не нужна.
func foldXKeen(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			r += 'a' - 'A'
		case r >= 0x0410 && r <= 0x042F:
			r += 0x20
		case r == 0x0401:
			r = 0x0451
		}
		b.WriteRune(r)
	}
	return b.String()
}

// XKeenStoplistMatch повторяет check_xray_backups() из S05xkeen: XKeen
// отменяет запуск Xray, если в корне каталога конфигураций есть файл, подходящий
// под `-iname "*<слово>*.json"` для слов стоп-списка или под `-name "*(*).json"`.
// Флаг -iname не различает регистр и в суффиксе, поэтому «.JSON» тоже считается
// расширением, а -name для скобок регистр учитывает: «a(1).JSON» не совпадает.
//
// Возвращает первое совпавшее слово в порядке XKeen, затем XKeenParenWord.
func XKeenStoplistMatch(name string) (word string, ok bool) {
	folded := foldXKeen(name)
	if stem, found := strings.CutSuffix(folded, xkeenJSONSuffix); found {
		for _, w := range XKeenStoplistWords {
			if strings.Contains(stem, w) {
				return w, true
			}
		}
	}
	if stem, found := strings.CutSuffix(name, ")"+xkeenJSONSuffix); found && strings.Contains(stem, "(") {
		return XKeenParenWord, true
	}
	return "", false
}
