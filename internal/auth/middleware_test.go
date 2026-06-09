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
	})

	t.Run("TLS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "https://localhost/foo", nil)
		req.TLS = &tls.ConnectionState{} // Simulate TLS connection
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		hsts := rr.Header().Get("Strict-Transport-Security")
		if hsts != "max-age=31536000" {
			t.Errorf("expected Strict-Transport-Security header to be %q, got %q", "max-age=31536000", hsts)
		}
	})
}
