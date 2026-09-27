package auth

import "strings"

// ParseUserAgent разбирает строку User-Agent браузера в (browser, os) без
// внешних зависимостей. Используется только для отображения в списке сессий
// (GET /api/auth/sessions, T-134-09) — не участвует ни в каких
// security-решениях (валидация сессии/CSRF не зависит от результата).
// Нераспознанное значение по каждой из осей даёт пустую строку, а не панику.
func ParseUserAgent(ua string) (browser, os string) {
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "Chromium/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}

	switch {
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	// Android UA-строки почти всегда содержат "Linux" тоже — проверка
	// должна идти раньше него.
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "CrOS"):
		os = "ChromeOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}

	return browser, os
}
