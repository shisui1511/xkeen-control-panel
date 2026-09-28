package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/crypto/bcrypt"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
)

// newLoopbackTestServer wires a Server the same way cmd/xcp/main.go wires
// login/setup/me and the Mihomo provider routes (134-RESEARCH.md
// interfaces), with a known bcrypt password hash so login/setup can be
// exercised end-to-end. Real MihomoProviderAdapter/MihomoProviderRedirect
// live in internal/handlers and need a full API (subscription service,
// config, ...) — this plan only needs the allowlist to pass their paths
// through to s.mux, so lightweight stubs stand in for them.
func newLoopbackTestServer(t *testing.T) (srv *Server, password string) {
	t.Helper()

	password = "correct-horse-battery-staple-1"
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	mapFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>SPA Root</body></html>")},
	}

	cfg := &Config{
		Port:         0,
		LoopbackPort: 0,
		AllowedRoots: []string{t.TempDir()},
		DataDir:      t.TempDir(),
		PasswordHash: string(hashBytes),
	}

	srv, err = New(cfg, "v-test", mapFS)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.GetAuthService().Stop() })

	authSvc := srv.GetAuthService()
	srv.Handle("/api/auth/login", authSvc.HandleLogin)
	srv.Handle("/api/auth/me", authSvc.HandleMe)
	srv.Handle("/api/auth/setup", authSvc.HandleSetup)
	srv.HandleProtected("/api/config/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("config list"))
	})

	srv.Handle("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("v-test"))
	})
	srv.Handle("/api/provider.yaml", func(w http.ResponseWriter, r *http.Request) {
		// Заглушка намеренно пытается выставить cookie — TestLoopback_StripsSetCookie
		// проверяет, что noCookieWriter её всё равно снимает на loopback.
		http.SetCookie(w, &http.Cookie{Name: "should-not-leak", Value: "1"})
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxies: []\n"))
	})
	srv.Handle("/mihomo/provider.yaml", func(w http.ResponseWriter, r *http.Request) {
		target := "/api/provider.yaml"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
	})

	return srv, password
}

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestLoopback_RejectsAuthRoutes(t *testing.T) {
	srv, password := newLoopbackTestServer(t)
	ts := httptest.NewServer(srv.BuildLoopbackHandler())
	defer ts.Close()

	client := noRedirectClient()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"login with correct password", http.MethodPost, "/api/auth/login", `{"password":"` + password + `"}`},
		{"setup", http.MethodPost, "/api/auth/setup", `{"password":"` + password + `"}`},
		{"me", http.MethodGet, "/api/auth/me", ""},
		{"protected config list", http.MethodGet, "/api/config/list", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			var err error
			if tc.body != "" {
				req, err = http.NewRequest(tc.method, ts.URL+tc.path, bytes.NewBufferString(tc.body))
				if err == nil {
					req.Header.Set("Content-Type", "application/json")
				}
			} else {
				req, err = http.NewRequest(tc.method, ts.URL+tc.path, nil)
			}
			if err != nil {
				t.Fatalf("failed to build request: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("expected 403, got %d", resp.StatusCode)
			}

			if len(resp.Cookies()) != 0 {
				t.Errorf("expected no Set-Cookie on loopback, got %d cookie(s)", len(resp.Cookies()))
			}
			if resp.Header.Get("Set-Cookie") != "" {
				t.Errorf("expected no Set-Cookie header, got %q", resp.Header.Get("Set-Cookie"))
			}

			var respBody map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			errMsg, _ := respBody["error"].(string)
			if !strings.Contains(errMsg, "not available on loopback") {
				t.Errorf("expected error to mention 'not available on loopback', got %q", errMsg)
			}
		})
	}
}

func TestLoopback_AllowsServiceRoutes(t *testing.T) {
	srv, _ := newLoopbackTestServer(t)
	ts := httptest.NewServer(srv.BuildLoopbackHandler())
	defer ts.Close()

	client := noRedirectClient()

	t.Run("version", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/version")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("provider.yaml", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/provider.yaml?url=https://example.com/sub")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("mihomo provider.yaml redirects to api/provider.yaml", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/mihomo/provider.yaml?url=https://example.com/sub")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Errorf("expected 302, got %d", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/api/provider.yaml") {
			t.Errorf("expected redirect to /api/provider.yaml, got %q", loc)
		}
	})

	t.Run("mihomo hwid provider.yaml path passes the allowlist", func(t *testing.T) {
		// /mihomo/hwid/provider.yaml — служебный путь allowlist-а (D-14), не
		// зарегистрированный обработчиком в этом тестовом сервере; главное —
		// что allowlist пропускает его дальше в s.mux (не 403 от loopback-слоя).
		resp, err := client.Get(ts.URL + "/mihomo/hwid/provider.yaml")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("expected allowlist to pass /mihomo/hwid/provider.yaml through to mux, got 403")
		}
	})
}

func TestLoopback_StripsSetCookie(t *testing.T) {
	srv, _ := newLoopbackTestServer(t)
	ts := httptest.NewServer(srv.BuildLoopbackHandler())
	defer ts.Close()

	resp, err := noRedirectClient().Get(ts.URL + "/api/provider.yaml?url=https://example.com/sub")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Errorf("expected Set-Cookie to be stripped on loopback, got %q", resp.Header.Get("Set-Cookie"))
	}
}

func TestPublicHandler_StillServesLogin(t *testing.T) {
	srv, password := newLoopbackTestServer(t)
	ts := httptest.NewServer(srv.BuildHandler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewBufferString(`{"password":"`+password+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	found := false
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookieName {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s cookie on public login response, got none", auth.SessionCookieName)
	}
}
