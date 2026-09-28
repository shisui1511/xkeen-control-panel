package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	// Dummy handler to wrap with middleware
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := SecurityHeaders(nextHandler)

	t.Run("Standard request (non-TLS)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://localhost/foo", nil)
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		headers := rr.Header()

		// Verify security headers
		expectedHeaders := map[string]string{
			"X-Frame-Options":           "DENY",
			"X-Content-Type-Options":    "nosniff",
			"X-XSS-Protection":          "1; mode=block",
			"Referrer-Policy":           "strict-origin-when-cross-origin",
			"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
			"Strict-Transport-Security": "", // Should not be present on non-TLS
		}

		for key, expectedVal := range expectedHeaders {
			actualVal := headers.Get(key)
			if expectedVal == "" {
				if actualVal != "" {
					t.Errorf("expected header %q to be empty or absent on non-TLS request, got %q", key, actualVal)
				}
			} else {
				if actualVal != expectedVal {
					t.Errorf("expected header %q to be %q, got %q", key, expectedVal, actualVal)
				}
			}
		}

		// Verify CSP specifically
		csp := headers.Get("Content-Security-Policy")
		if csp == "" {
			t.Fatal("Content-Security-Policy header is missing")
		}

		// Verify CSP contains google fonts domains
		if !strings.Contains(csp, "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;") {
			t.Errorf("CSP style-src does not contain https://fonts.googleapis.com: %s", csp)
		}
		if !strings.Contains(csp, "font-src 'self' https://fonts.gstatic.com;") {
			t.Errorf("CSP font-src does not contain https://fonts.gstatic.com: %s", csp)
		}
		if !strings.Contains(csp, "connect-src 'self' https://ipinfo.io") {
			t.Errorf("CSP connect-src does not contain https://ipinfo.io: %s", csp)
		}

		// 134-REVIEW WR-01: script-src не должен содержать 'unsafe-inline' —
		// иначе CSP не защищает от inline-XSS. Разбираем директивы, а не просто
		// ищем подстроку в заголовке целиком, чтобы не спутать script-src со
		// style-src, где 'unsafe-inline' допустим.
		directives := make(map[string]string)
		for _, part := range strings.Split(csp, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			fields := strings.SplitN(part, " ", 2)
			name := fields[0]
			value := ""
			if len(fields) > 1 {
				value = fields[1]
			}
			directives[name] = value
		}

		scriptSrc, ok := directives["script-src"]
		if !ok {
			t.Fatal("CSP is missing script-src directive")
		}
		if strings.Contains(scriptSrc, "unsafe-inline") {
			t.Errorf("script-src must not contain 'unsafe-inline' (WR-01), got %q", scriptSrc)
		}
		hasSelf := false
		for _, src := range strings.Fields(scriptSrc) {
			if src == "'self'" {
				hasSelf = true
			}
		}
		if !hasSelf {
			t.Errorf("script-src must contain 'self', got %q", scriptSrc)
		}
	})

	t.Run("TLS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://localhost/foo", nil)
		req.TLS = &tls.ConnectionState{} // Simulate TLS connection
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		// D-16: HSTS не включается с положительным max-age — самоподписанный
		// сертификат заблокировал бы доступ к панели в Chrome. max-age=0
		// снимает политику, закэшированную прежними версиями (RFC 6797 §6.1.1).
		hsts := rr.Header().Get("Strict-Transport-Security")
		if hsts != "max-age=0" {
			t.Errorf("expected Strict-Transport-Security header to be %q, got %q", "max-age=0", hsts)
		}
	})
}
