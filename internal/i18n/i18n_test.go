package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_LoadsLocales(t *testing.T) {
	instance := New("en")
	if instance == nil {
		t.Fatal("expected non-nil I18n instance")
	}

	if !instance.hasLang("ru") {
		t.Error("expected 'ru' locale to be loaded")
	}
	if !instance.hasLang("en") {
		t.Error("expected 'en' locale to be loaded")
	}
	if instance.hasLang("fr") {
		t.Error("expected 'fr' locale not to be loaded")
	}
}

func TestT_BasicAndFallback(t *testing.T) {
	instance := New("en")

	// 1. Existing key in target locale
	ruText := instance.T("ru", "auth.invalid_credentials")
	if ruText != "Неверные учетные данные" {
		t.Errorf("expected Russian translation, got %q", ruText)
	}

	enText := instance.T("en", "auth.invalid_credentials")
	if enText != "Invalid credentials" {
		t.Errorf("expected English translation, got %q", enText)
	}

	// 2. Fallback to defaultLang when key missing in requested locale
	// Inject a custom dict to test fallback
	instance.translations["custom"] = map[string]string{
		"only_in_custom": "Custom text",
	}
	// Key exists in defaultLang (en) but not in custom
	fallbackText := instance.T("custom", "auth.invalid_credentials")
	if fallbackText != "Invalid credentials" {
		t.Errorf("expected fallback to default language, got %q", fallbackText)
	}

	// 3. Unknown lang fallback to defaultLang
	unknownLangText := instance.T("de", "auth.invalid_credentials")
	if unknownLangText != "Invalid credentials" {
		t.Errorf("expected fallback to default language for unknown lang, got %q", unknownLangText)
	}

	// 4. Key missing everywhere -> returns raw key
	rawKey := instance.T("ru", "nonexistent.key.xyz")
	if rawKey != "nonexistent.key.xyz" {
		t.Errorf("expected raw key fallback, got %q", rawKey)
	}
}

func TestT_ParameterSubstitution(t *testing.T) {
	instance := New("en")
	instance.translations["ru"]["test.param"] = "Привет, %s! У вас %d уведомлений."
	instance.translations["en"]["test.param"] = "Hello, %s! You have %d notifications."
	instance.translations["en"]["test.only_en_param"] = "Status: %s (code: %d)"

	// 1. Formatting in target language
	resRu := instance.T("ru", "test.param", "Алексей", 5)
	if resRu != "Привет, Алексей! У вас 5 уведомлений." {
		t.Errorf("unexpected formatted output: %q", resRu)
	}

	resEn := instance.T("en", "test.param", "Alice", 3)
	if resEn != "Hello, Alice! You have 3 notifications." {
		t.Errorf("unexpected formatted output: %q", resEn)
	}

	// 2. Formatting in fallback language
	resFallback := instance.T("ru", "test.only_en_param", "active", 200)
	if resFallback != "Status: active (code: 200)" {
		t.Errorf("unexpected fallback formatting output: %q", resFallback)
	}

	// 3. Formatting with raw key
	resRaw := instance.T("ru", "File %s uploaded (%d bytes)", "report.pdf", 1024)
	if resRaw != "File report.pdf uploaded (1024 bytes)" {
		t.Errorf("unexpected raw key formatting output: %q", resRaw)
	}

	// 4. Resilient to mismatched args (no panic)
	resMismatch := instance.T("ru", "test.param", "Только имя")
	if !strings.Contains(resMismatch, "Только имя") || !strings.Contains(resMismatch, "EXTRA") && !strings.Contains(resMismatch, "MISSING") {
		t.Errorf("expected graceful handling of missing args: %q", resMismatch)
	}

	// 5. Global helper T
	defaultI18n.translations["en"]["test.global_param"] = "Hello, %s! You have %d notifications."
	defer delete(defaultI18n.translations["en"], "test.global_param")

	globalRes := T("en", "test.global_param", "Bob", 1)
	if globalRes != "Hello, Bob! You have 1 notifications." {
		t.Errorf("unexpected global T output: %q", globalRes)
	}

	globalWithoutArgs := T("en", "auth.invalid_credentials")
	if globalWithoutArgs != "Invalid credentials" {
		t.Errorf("unexpected global T without args: %q", globalWithoutArgs)
	}
}

func TestGetLang(t *testing.T) {
	instance := New("en")

	tests := []struct {
		name         string
		url          string
		cookieVal    string
		acceptHeader string
		expectedLang string
	}{
		{
			name:         "Query parameter ru",
			url:          "/api/data?lang=ru",
			expectedLang: "ru",
		},
		{
			name:         "Query parameter en",
			url:          "/api/data?lang=en",
			expectedLang: "en",
		},
		{
			name:         "Query parameter unsupported -> fallback to default",
			url:          "/api/data?lang=de",
			expectedLang: "en",
		},
		{
			name:         "Cookie takes precedence if query absent",
			url:          "/api/data",
			cookieVal:    "ru",
			expectedLang: "ru",
		},
		{
			name:         "Cookie unsupported -> falls back to next priority",
			url:          "/api/data",
			cookieVal:    "it",
			expectedLang: "en",
		},
		{
			name:         "Query overrides cookie",
			url:          "/api/data?lang=en",
			cookieVal:    "ru",
			expectedLang: "en",
		},
		{
			name:         "Accept-Language header with ru-RU",
			url:          "/api/data",
			acceptHeader: "ru-RU,ru;q=0.9,en-US;q=0.8",
			expectedLang: "ru",
		},
		{
			name:         "Accept-Language header with en-US",
			url:          "/api/data",
			acceptHeader: "en-US,en;q=0.9",
			expectedLang: "en",
		},
		{
			name:         "Accept-Language unsupported language falls through",
			url:          "/api/data",
			acceptHeader: "fr-FR,fr;q=0.9,zh-CN;q=0.8",
			expectedLang: "en",
		},
		{
			name:         "Default language when no hints given",
			url:          "/api/data",
			expectedLang: "en",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if tc.cookieVal != "" {
				req.AddCookie(&http.Cookie{Name: "lang", Value: tc.cookieVal})
			}
			if tc.acceptHeader != "" {
				req.Header.Set("Accept-Language", tc.acceptHeader)
			}

			lang := instance.GetLang(req)
			if lang != tc.expectedLang {
				t.Errorf("GetLang() = %q, want %q", lang, tc.expectedLang)
			}

			// Also test global GetLang
			globalLang := GetLang(req)
			if globalLang != tc.expectedLang {
				t.Errorf("Global GetLang() = %q, want %q", globalLang, tc.expectedLang)
			}
		})
	}
}

func TestMiddleware_And_LangFromContext(t *testing.T) {
	var capturedLang string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLang = LangFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(handler)

	// 1. Request with ?lang=ru
	req := httptest.NewRequest(http.MethodGet, "/test?lang=ru", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if capturedLang != "ru" {
		t.Errorf("expected capturedLang to be 'ru', got %q", capturedLang)
	}

	// 2. Request without lang -> fallback to en
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr2 := httptest.NewRecorder()
	mw.ServeHTTP(rr2, req2)

	if capturedLang != "en" {
		t.Errorf("expected capturedLang to be 'en', got %q", capturedLang)
	}

	// 3. LangFromContext on raw background context returns default
	emptyContextLang := LangFromContext(context.Background())
	if emptyContextLang != "en" {
		t.Errorf("expected empty context to return 'en', got %q", emptyContextLang)
	}
}
