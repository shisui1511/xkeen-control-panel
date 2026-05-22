package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed locales/*.json
var localesFS embed.FS

// I18n provides translation and localization support.
type I18n struct {
	translations map[string]map[string]string
	defaultLang  string
}

var defaultI18n *I18n

func init() {
	defaultI18n = New("en")
}

// New creates a new I18n instance with the specified default language and loads all locale JSON files.
func New(defaultLang string) *I18n {
	i := &I18n{
		translations: make(map[string]map[string]string),
		defaultLang:  defaultLang,
	}

	// Load all locale files
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return i
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		lang := strings.TrimSuffix(entry.Name(), ".json")
		data, err := localesFS.ReadFile("locales/" + entry.Name())
		if err != nil {
			continue
		}

		var dict map[string]string
		if err := json.Unmarshal(data, &dict); err != nil {
			continue
		}

		i.translations[lang] = dict
	}

	return i
}

// T translates the key into the specified language, falling back to the default language or key itself.
func (i *I18n) T(lang, key string) string {
	if dict, ok := i.translations[lang]; ok {
		if val, ok := dict[key]; ok {
			return val
		}
	}
	// Fallback to default language
	if dict, ok := i.translations[i.defaultLang]; ok {
		if val, ok := dict[key]; ok {
			return val
		}
	}
	return key
}

// GetLang gets the language from request query parameters, cookies, Accept-Language header, or falls back to default.
func (i *I18n) GetLang(r *http.Request) string {
	// 1. Query parameter
	if lang := r.URL.Query().Get("lang"); lang != "" {
		if i.hasLang(lang) {
			return lang
		}
	}

	// 2. Cookie
	if cookie, err := r.Cookie("lang"); err == nil {
		if i.hasLang(cookie.Value) {
			return cookie.Value
		}
	}

	// 3. Accept-Language header
	if acceptLang := r.Header.Get("Accept-Language"); acceptLang != "" {
		// Parse language tag (e.g., "ru-RU,ru;q=0.9,en-US;q=0.8")
		for _, tag := range strings.Split(acceptLang, ",") {
			lang := strings.TrimSpace(strings.Split(tag, ";")[0])
			lang = strings.Split(lang, "-")[0] // Get primary language
			if i.hasLang(lang) {
				return lang
			}
		}
	}

	return i.defaultLang
}

func (i *I18n) hasLang(lang string) bool {
	_, ok := i.translations[lang]
	return ok
}

// Global functions using default instance

// T is a global helper that translates the key using the default I18n instance.
func T(lang, key string) string {
	return defaultI18n.T(lang, key)
}

// GetLang is a global helper that retrieves the language from the request using the default I18n instance.
func GetLang(r *http.Request) string {
	return defaultI18n.GetLang(r)
}

// Middleware adds lang to request context
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := GetLang(r)
		r = r.WithContext(context.WithValue(r.Context(), langKey{}, lang))
		next.ServeHTTP(w, r)
	})
}

type langKey struct{}

// LangFromContext gets the language from request context
func LangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(langKey{}).(string); ok {
		return lang
	}
	return defaultI18n.defaultLang
}
