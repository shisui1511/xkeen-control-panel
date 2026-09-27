package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestSessionStore_PersistsAcrossRestart проверяет сквозной путь SESS-01:
// сессия и её CSRF, выданные одним AuthService, принимаются новым
// AuthService на том же data_dir без повторного входа.
func TestSessionStore_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	hashBytes, err := bcrypt.GenerateFromPassword([]byte("correcthorse123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	hash := string(hashBytes)

	svc1 := NewAuthService(Options{DataDir: dir, PasswordHash: hash})

	body, _ := json.Marshal(map[string]string{"password": "correcthorse123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	svc1.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected session cookie in login response")
	}

	var loginResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	svc1.Stop()

	svc2 := NewAuthService(Options{DataDir: dir, PasswordHash: hash})
	defer svc2.Stop()

	protected := svc2.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	req2.AddCookie(cookie)
	req2.Header.Set(CSRFHeaderName, loginResp.CSRFToken)
	rec2 := httptest.NewRecorder()
	protected(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 after restart with the same cookie/CSRF, got %d: %s", rec2.Code, rec2.Body.String())
	}

	reqMe := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe.AddCookie(cookie)
	recMe := httptest.NewRecorder()
	svc2.HandleMe(recMe, reqMe)
	var meResp struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(recMe.Body).Decode(&meResp); err != nil {
		t.Fatalf("decode /api/auth/me response: %v", err)
	}
	if meResp.CSRFToken != loginResp.CSRFToken {
		t.Errorf("expected same csrf_token after restart, got %q want %q", meResp.CSRFToken, loginResp.CSRFToken)
	}
}

// TestSessionStore_FileHasOnlyHashes проверяет T-134-01: sessions.json имеет
// права 0600 и не содержит ни сырой session-токен, ни сырой CSRF-токен —
// только их SHA-256.
func TestSessionStore_FileHasOnlyHashes(t *testing.T) {
	dir := t.TempDir()
	hashBytes, err := bcrypt.GenerateFromPassword([]byte("secretpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAuthService(Options{DataDir: dir, PasswordHash: string(hashBytes)})
	defer svc.Stop()

	issued, err := svc.CreateSessionWithMeta(SessionMeta{IP: "1.2.3.4", UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("CreateSessionWithMeta failed: %v", err)
	}

	path := filepath.Join(dir, sessionsFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sessions.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected sessions.json permissions 0600, got %04o", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sessions.json: %v", err)
	}
	content := string(data)

	if strings.Contains(content, issued.Token) {
		t.Error("sessions.json must not contain the raw session token")
	}
	if strings.Contains(content, issued.CSRFToken) {
		t.Error("sessions.json must not contain the raw CSRF token")
	}
	if !strings.Contains(content, hashToken(issued.Token)) {
		t.Error("sessions.json must contain token_hash = sha256(raw token)")
	}
	if !strings.Contains(content, hashToken(issued.CSRFToken)) {
		t.Error("sessions.json must contain csrf_hash = sha256(raw CSRF token)")
	}
	if !strings.Contains(content, "schema_version") {
		t.Error("sessions.json must contain schema_version")
	}
	if !strings.Contains(content, "password_fingerprint") {
		t.Error("sessions.json must contain password_fingerprint")
	}
}

// TestSessionStore_DiscardsOnPasswordChangeWhileStopped проверяет T-134-02:
// sessions.json записанный при одном пароле отвергается новым AuthService,
// запущенным с другим password hash (пароль сменили, пока панель стояла).
func TestSessionStore_DiscardsOnPasswordChangeWhileStopped(t *testing.T) {
	dir := t.TempDir()
	hash1, err := bcrypt.GenerateFromPassword([]byte("pass-one-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := bcrypt.GenerateFromPassword([]byte("pass-two-456"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	svc1 := NewAuthService(Options{DataDir: dir, PasswordHash: string(hash1)})
	issued, err := svc1.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	svc1.Stop()

	svc2 := NewAuthService(Options{DataDir: dir, PasswordHash: string(hash2)})
	defer svc2.Stop()

	if _, err := svc2.ValidateSession(issued.Token); err == nil {
		t.Error("expected session issued under the old password to be rejected after a password change")
	}
}

// TestSessionStore_CorruptOrFutureSchemaStartsEmpty проверяет T-134-04:
// битый JSON и schema_version выше поддерживаемой не паникуют и не
// блокируют старт — панель стартует с пустым набором сессий.
func TestSessionStore_CorruptOrFutureSchemaStartsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"garbage-json", "not-json{{{"},
		{"future-schema-version", `{"schema_version":99,"sessions":[]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, sessionsFileName), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			hashBytes, err := bcrypt.GenerateFromPassword([]byte("freshpass123"), bcrypt.DefaultCost)
			if err != nil {
				t.Fatal(err)
			}

			var svc *AuthService
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("NewAuthService panicked on %s: %v", tc.name, r)
					}
				}()
				svc = NewAuthService(Options{DataDir: dir, PasswordHash: string(hashBytes)})
			}()
			defer svc.Stop()

			issued, err := svc.CreateSession()
			if err != nil {
				t.Fatalf("CreateSession failed after %s: %v", tc.name, err)
			}
			if _, err := svc.ValidateSession(issued.Token); err != nil {
				t.Errorf("expected a freshly created session to validate: %v", err)
			}
		})
	}
}

// TestAuthService_StopIsIdempotent проверяет, что повторный Stop() не
// паникует и что после Stop() сохранение сессий на диск становится no-op.
func TestAuthService_StopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})

	svc.Stop()
	svc.Stop() // не должен паниковать

	before := svc.store.writes.Load()
	if _, err := svc.CreateSession(); err != nil {
		t.Fatal(err)
	}
	after := svc.store.writes.Load()
	if after != before {
		t.Errorf("expected no store writes after Stop(), before=%d after=%d", before, after)
	}
}
