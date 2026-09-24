package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
)

func createTestMapFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!DOCTYPE html><html><body>SPA Root</body></html>"),
		},
		"assets/test.12345678.js": &fstest.MapFile{
			Data: []byte(`console.log("asset");`),
		},
		"favicon.ico": &fstest.MapFile{
			Data: []byte("icon-bytes"),
		},
	}
}

func TestServer_StaticAndSPAFallback(t *testing.T) {
	mapFS := createTestMapFS()
	cfg := &Config{
		Port:         0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
	}

	srv, err := New(cfg, "v1.2.3", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	tests := []struct {
		name                 string
		path                 string
		expectedStatus       int
		expectedBodyContains string
		expectedCacheControl string
	}{
		{
			name:                 "Unknown API route is a JSON 404, not the SPA page",
			path:                 "/api/xkeen/dns-redirect/enable",
			expectedStatus:       http.StatusNotFound,
			expectedBodyContains: "unknown API endpoint",
		},
		{
			name:                 "Root path serves index.html with no-cache",
			path:                 "/",
			expectedStatus:       http.StatusOK,
			expectedBodyContains: "SPA Root",
			expectedCacheControl: "no-cache, no-store, must-revalidate",
		},
		{
			name:                 "Explicit index.html redirects to / with no-cache",
			path:                 "/index.html",
			expectedStatus:       http.StatusMovedPermanently,
			expectedCacheControl: "no-cache, no-store, must-revalidate",
		},
		{
			name:                 "SPA client-side route falls back to index.html with no-cache",
			path:                 "/routes/settings",
			expectedStatus:       http.StatusOK,
			expectedBodyContains: "SPA Root",
			expectedCacheControl: "no-cache, no-store, must-revalidate",
		},
		{
			name:                 "Asset file served with long-term immutable cache",
			path:                 "/assets/test.12345678.js",
			expectedStatus:       http.StatusOK,
			expectedBodyContains: `console.log("asset");`,
			expectedCacheControl: "public, max-age=31536000, immutable",
		},
		{
			name:                 "Favicon file served",
			path:                 "/favicon.ico",
			expectedStatus:       http.StatusOK,
			expectedBodyContains: "icon-bytes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			srv.mux.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, rec.Code)
			}

			body := rec.Body.String()
			if !strings.Contains(body, tc.expectedBodyContains) {
				t.Errorf("expected body to contain %q, got %q", tc.expectedBodyContains, body)
			}

			if tc.expectedCacheControl != "" {
				cc := rec.Header().Get("Cache-Control")
				if !strings.Contains(cc, tc.expectedCacheControl) {
					t.Errorf("expected Cache-Control to contain %q, got %q", tc.expectedCacheControl, cc)
				}
			}
		})
	}
}

func TestServer_Handle_And_HandleProtected(t *testing.T) {
	mapFS := createTestMapFS()
	cfg := &Config{
		Port:         0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
	}

	srv, err := New(cfg, "v1.2.3", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.GetAuthService().Stop()

	// 1. Verify GetVersion and GetAuthService
	if srv.GetVersion() != "v1.2.3" {
		t.Errorf("expected version v1.2.3, got %q", srv.GetVersion())
	}
	if srv.GetAuthService() == nil {
		t.Fatal("expected non-nil auth service")
	}

	// 2. Register public and protected handlers
	srv.Handle("/api/public", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("public data"))
	})

	srv.HandleProtected("/api/protected", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secret data"))
	})

	// 3. Test public route -> 200 OK
	reqPub := httptest.NewRequest(http.MethodGet, "/api/public", nil)
	recPub := httptest.NewRecorder()
	srv.mux.ServeHTTP(recPub, reqPub)
	if recPub.Code != http.StatusOK {
		t.Errorf("public endpoint: expected 200, got %d", recPub.Code)
	}

	// 4. Test protected route without session -> 401 Unauthorized
	reqProtNoAuth := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	recProtNoAuth := httptest.NewRecorder()
	srv.mux.ServeHTTP(recProtNoAuth, reqProtNoAuth)
	if recProtNoAuth.Code != http.StatusUnauthorized {
		t.Errorf("protected endpoint without auth: expected 401, got %d", recProtNoAuth.Code)
	}

	// 5. Test protected route with valid session -> 200 OK
	session, err := srv.GetAuthService().CreateSession()
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}

	reqProtAuth := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	reqProtAuth.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: session.Token,
	})
	recProtAuth := httptest.NewRecorder()
	srv.mux.ServeHTTP(recProtAuth, reqProtAuth)
	if recProtAuth.Code != http.StatusOK {
		t.Errorf("protected endpoint with auth: expected 200, got %d", recProtAuth.Code)
	}
	if recProtAuth.Body.String() != "secret data" {
		t.Errorf("expected 'secret data', got %q", recProtAuth.Body.String())
	}
}

func TestServer_Start_And_Shutdown_HTTP(t *testing.T) {
	mapFS := createTestMapFS()
	cfg := &Config{
		Port:         0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
		HTTPS: HTTPSConfig{
			Enabled: false,
		},
	}

	srv, err := New(cfg, "v1.0.0", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.GetAuthService().Stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for server to start
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	startErr := <-errCh
	if startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", startErr)
	}
}

func TestServer_Start_And_Shutdown_HTTPS(t *testing.T) {
	mapFS := createTestMapFS()
	cfg := &Config{
		Port:         0,
		LoopbackPort: 0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
		HTTPS: HTTPSConfig{
			Enabled: true,
		},
	}

	srv, err := New(cfg, "v1.0.0", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.GetAuthService().Stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for server and certificate generation to complete
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	startErr := <-errCh
	if startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
		t.Fatalf("expected ErrServerClosed, got %v", startErr)
	}
}

func TestServer_MiddlewareChain(t *testing.T) {
	mapFS := createTestMapFS()
	cfg := &Config{
		Port:         0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
	}

	srv, err := New(cfg, "v1.0.0", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.GetAuthService().Stop()

	srv.Handle("/api/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic for recovery middleware")
	})

	srv.Handle("/api/echo-lang", func(w http.ResponseWriter, r *http.Request) {
		lang := i18n.LangFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(lang))
	})

	srv.Handle("/api/echo-body", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})

	handler := srv.BuildHandler()

	// 1. Test Recovery middleware on panic
	reqPanic := httptest.NewRequest(http.MethodGet, "/api/panic", nil)
	recPanic := httptest.NewRecorder()
	handler.ServeHTTP(recPanic, reqPanic)
	if recPanic.Code != http.StatusInternalServerError {
		t.Errorf("recovery: expected status 500 on panic, got %d", recPanic.Code)
	}

	// 2. Test SecurityHeaders middleware
	secHeader := recPanic.Header().Get("X-Content-Type-Options")
	if secHeader != "nosniff" {
		t.Errorf("security headers: expected nosniff, got %q", secHeader)
	}

	// 3. Test i18n middleware in chain
	reqLang := httptest.NewRequest(http.MethodGet, "/api/echo-lang?lang=ru", nil)
	recLang := httptest.NewRecorder()
	handler.ServeHTTP(recLang, reqLang)
	if recLang.Code != http.StatusOK {
		t.Errorf("i18n: expected status 200, got %d", recLang.Code)
	}
	if recLang.Body.String() != "ru" {
		t.Errorf("i18n: expected lang 'ru', got %q", recLang.Body.String())
	}
}
