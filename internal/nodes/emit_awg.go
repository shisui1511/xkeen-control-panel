package nodes

import "strconv"

// clashAWGOptions копирует опции AmneziaWG для прокси Mihomo. Версию диалекта
// («1.5», «2.0», «3.1») Mihomo читает как число: строка в кавычках отвергается
// проверкой ядра («cannot parse 'amnezia-wg-option.version' as int»), число без
// кавычек принимается. Старый слой подписок пишет её так же, числом.
func clashAWGOptions(opt map[string]any) map[string]any {
	out := deepCopyMap(opt)
	if s, ok := out["version"].(string); ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			out["version"] = f
		}
	}
	return out
}
