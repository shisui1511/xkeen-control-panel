package services

import (
	"regexp"
	"strings"
)

// Regex для определения признака новизны.
var isNewRe = regexp.MustCompile(`(?i:\[new\]|\(new\))|🆕|\bNEW\b`)

// Regex для определения скорости (например, 10Gb/s, 1 Gbps, 100 Mbps).
var speedRe = regexp.MustCompile(`(?i)\b(\d+)\s?(Gb|Mb|Tb)/s\b|\b(\d+)\s?(Gbps|Mbps|Tbps)\b`)

// Regex для поиска UseCase в круглых или квадратных скобках (например, (Youtube, Netflix)).
var bracesUseCaseRe = regexp.MustCompile(`\(([^)]+)\)|\[([^\]]+)\]`)

// Ключевые слова для фильтрации UseCase.
var useCaseKeywords = []string{
	"youtube", "instagram", "netflix", "chatgpt", "openai", "gaming", "tiktok",
	"facebook", "twitter", "spotify", "all", "socials", "ads", "web", "torrent",
	"steam", "zoom",
}

// Regex для поиска двухбуквенного кода страны в начале строки (например, "RU-", "DE ", "NL -").
var countryPrefixRe = regexp.MustCompile(`^(?i)\s*([a-z]{2})\s*[-_:]\s*`)

// countryToFlag динамически генерирует эмодзи-флаг из двухбуквенного ISO-кода страны.
func countryToFlag(code string) string {
	if len(code) != 2 {
		return ""
	}
	c1 := strings.ToUpper(string(code[0]))
	c2 := strings.ToUpper(string(code[1]))
	if c1[0] < 'A' || c1[0] > 'Z' || c2[0] < 'A' || c2[0] > 'Z' {
		return ""
	}
	r1 := rune(int(c1[0]-'A') + 0x1F1E6)
	r2 := rune(int(c2[0]-'A') + 0x1F1E6)
	return string([]rune{r1, r2})
}

// extractFlagEmoji ищет эмодзи-флаг в строке.
// Флаг состоит из двух рун в диапазоне Regional Indicator Symbol [0x1F1E6, 0x1F1FF].
func extractFlagEmoji(s string) (string, string) {
	runes := []rune(s)
	for i := 0; i < len(runes)-1; i++ {
		r1 := runes[i]
		r2 := runes[i+1]
		if r1 >= 0x1F1E6 && r1 <= 0x1F1FF && r2 >= 0x1F1E6 && r2 <= 0x1F1FF {
			flag := string([]rune{r1, r2})
			c1 := string(rune(int(r1) - 0x1F1E6 + int('A')))
			c2 := string(rune(int(r2) - 0x1F1E6 + int('A')))
			code := c1 + c2
			return flag, code
		}
	}
	return "", ""
}

// parseRemark разбирает строку remark и извлекает метаданные узла подписки.
func parseRemark(remark string) SubscriptionNode {
	node := SubscriptionNode{}
	workStr := remark

	// 1. Извлекаем UseCase по разделителям | или ║
	for _, sep := range []string{"|", "║"} {
		if idx := strings.Index(workStr, sep); idx >= 0 {
			left := strings.TrimSpace(workStr[:idx])
			right := strings.TrimSpace(workStr[idx+len(sep):])

			hasUC := false
			lowerRight := strings.ToLower(right)
			for _, kw := range useCaseKeywords {
				if strings.Contains(lowerRight, kw) {
					hasUC = true
					break
				}
			}

			if hasUC || len(right) > 0 {
				node.UseCase = right
				workStr = left
			}
			break
		}
	}

	// 2. Ищем UseCase в скобках, если он еще не найден
	if node.UseCase == "" {
		matches := bracesUseCaseRe.FindAllStringSubmatch(workStr, -1)
		for _, match := range matches {
			content := ""
			if match[1] != "" {
				content = match[1]
			} else {
				content = match[2]
			}

			lowerContent := strings.ToLower(content)
			isUC := false
			for _, kw := range useCaseKeywords {
				if strings.Contains(lowerContent, kw) {
					isUC = true
					break
				}
			}
			if lowerContent == "new" {
				isUC = false
			}

			if isUC {
				node.UseCase = strings.TrimSpace(content)
				fullMatch := match[0]
				workStr = strings.Replace(workStr, fullMatch, "", 1)
				break
			}
		}
	}

	// 3. Извлекаем IsNew
	if isNewRe.MatchString(workStr) {
		node.IsNew = true
		workStr = isNewRe.ReplaceAllString(workStr, "")
	}

	// 4. Извлекаем Speed
	if match := speedRe.FindString(workStr); match != "" {
		node.Speed = strings.TrimSpace(match)
		workStr = speedRe.ReplaceAllString(workStr, "")
	}

	// 5. Извлекаем эмодзи-флаг
	if flag, code := extractFlagEmoji(workStr); flag != "" {
		node.Flag = flag
		node.Country = code
		workStr = strings.Replace(workStr, flag, "", -1)
	}

	// 6. Ищем двухбуквенный префикс страны в начале строки (например, "RU-", "NL -"),
	// если флаг ещё не был найден.
	if node.Country == "" {
		if matches := countryPrefixRe.FindStringSubmatch(workStr); len(matches) > 1 {
			code := strings.ToUpper(matches[1])
			node.Country = code
			node.Flag = countryToFlag(code)
			workStr = countryPrefixRe.ReplaceAllString(workStr, "")
		}
	}

	// 7. Формируем чистое имя
	cleanName := workStr
	for {
		prevLen := len(cleanName)
		cleanName = strings.TrimSpace(cleanName)
		cleanName = strings.Trim(cleanName, "-_/,()[]{}║|• ")
		if len(cleanName) == prevLen {
			break
		}
	}

	// Если имя после очистки оказалось пустым, а страна определена,
	// используем код страны в качестве имени.
	if cleanName == "" && node.Country != "" {
		cleanName = node.Country
	}

	node.Name = cleanName
	return node
}
