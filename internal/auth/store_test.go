package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// --- Task 3: лимит 20 сессий и отложенная запись last_seen ---

// TestCreateSession_EvictsOldestByLastSeen проверяет D-03: при создании
// 21-й сессии удаляется сессия с самой давней последней активностью,
// остаётся ровно MaxSessions.
func TestCreateSession_EvictsOldestByLastSeen(t *testing.T) {
	svc := NewAuthService(Options{})
	defer svc.Stop()

	base := time.Now()
	tokens := make([]string, 0, MaxSessions)
	for i := 0; i < MaxSessions; i++ {
		svc.now = func(i int) func() time.Time {
			return func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		}(i)
		issued, err := svc.CreateSession()
		if err != nil {
			t.Fatalf("CreateSession #%d failed: %v", i, err)
		}
		tokens = append(tokens, issued.Token)
	}

	// Сессия №5 (индекс 4) искусственно делается самой давно неактивной.
	oldestIdx := 4
	svc.mu.Lock()
	if s, ok := svc.sessions[hashToken(tokens[oldestIdx])]; ok {
		s.LastSeen = base.Add(-time.Hour)
	} else {
		svc.mu.Unlock()
		t.Fatalf("session #%d not found in map", oldestIdx)
	}
	svc.mu.Unlock()

	svc.now = func() time.Time { return base.Add(time.Duration(MaxSessions) * time.Minute) }
	if _, err := svc.CreateSession(); err != nil {
		t.Fatalf("21st CreateSession failed: %v", err)
	}

	svc.mu.RLock()
	count := len(svc.sessions)
	_, oldestStillPresent := svc.sessions[hashToken(tokens[oldestIdx])]
	svc.mu.RUnlock()

	if count != MaxSessions {
		t.Errorf("expected exactly %d sessions after eviction, got %d", MaxSessions, count)
	}
	if oldestStillPresent {
		t.Error("expected the session with the oldest LastSeen to be evicted")
	}
}

// TestValidateSession_DoesNotWritePerRequest проверяет T-134-05: 100
// последовательных ValidateSession не дают ни одной дополнительной записи
// на диск (только throttled flushLoop/Stop пишут накопленный last_seen).
func TestValidateSession_DoesNotWritePerRequest(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})
	defer svc.Stop()

	issued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	before := svc.store.writes.Load()
	for i := 0; i < 100; i++ {
		if _, err := svc.ValidateSession(issued.Token); err != nil {
			t.Fatalf("ValidateSession #%d failed: %v", i, err)
		}
	}
	after := svc.store.writes.Load()

	if after != before {
		t.Errorf("expected no additional writes from 100 ValidateSession calls, before=%d after=%d", before, after)
	}
}

// TestLastSeen_FlushedAndRestored проверяет, что явный flush (эмулирующий
// срабатывание тикера flushLoop) переживает рестарт: LastSeen
// восстановленной сессии не меньше значения до рестарта.
func TestLastSeen_FlushedAndRestored(t *testing.T) {
	dir := t.TempDir()
	svc1 := NewAuthService(Options{DataDir: dir})

	issued, err := svc1.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	fixed := time.Now().Add(2 * time.Hour)
	svc1.now = func() time.Time { return fixed }
	if _, err := svc1.ValidateSession(issued.Token); err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	svc1.flushIfDirty()
	svc1.Stop()

	svc2 := NewAuthService(Options{DataDir: dir})
	defer svc2.Stop()

	svc2.mu.RLock()
	var restoredLastSeen time.Time
	for _, s := range svc2.sessions {
		restoredLastSeen = s.LastSeen
	}
	svc2.mu.RUnlock()

	if restoredLastSeen.Before(fixed.Add(-time.Second)) {
		t.Errorf("expected restored LastSeen >= %v, got %v", fixed, restoredLastSeen)
	}
}

// TestSessions_ConcurrentAccess проверяет, что параллельные
// CreateSession/ValidateSession/Flush/Stop не дают гонок под -race и что
// Stop() остаётся идемпотентным при конкурентных вызовах.
func TestSessions_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})

	var tokensMu sync.Mutex
	var tokens []string

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				switch i % 4 {
				case 0:
					issued, err := svc.CreateSession()
					if err == nil {
						tokensMu.Lock()
						tokens = append(tokens, issued.Token)
						tokensMu.Unlock()
					}
				case 1:
					tokensMu.Lock()
					var tok string
					if n := len(tokens); n > 0 {
						tok = tokens[i%n]
					}
					tokensMu.Unlock()
					if tok == "" {
						tok = "nonexistent"
					}
					_, _ = svc.ValidateSession(tok)
				case 2:
					_ = svc.Flush()
				case 3:
					svc.Stop()
				}
			}
		}()
	}
	wg.Wait()

	svc.Stop() // финальный вызов должен остаться безопасным
}

// --- 134-03 Task 2: rate-limiter на диске, сброс для CLI ---

// TestRateLimiterStore_PersistsAcrossRestart проверяет D-04: блокировка IP
// переживает Stop()/NewAuthService на том же data_dir — обходить блокировку
// рестартом процесса нельзя.
func TestRateLimiterStore_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	svc1 := NewAuthService(Options{DataDir: dir, MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})

	for i := 0; i < 5; i++ {
		_ = svc1.rateLimiter.CheckLimit("203.0.113.5", 5, 5*time.Minute)
	}
	if svc1.rateLimiter.GetLockoutRemaining("203.0.113.5") <= 0 {
		t.Fatal("expected 203.0.113.5 to be locked before Stop()")
	}
	svc1.Stop()

	svc2 := NewAuthService(Options{DataDir: dir, MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	defer svc2.Stop()

	body, _ := json.Marshal(map[string]string{"password": "whatever"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.5:12345"
	rec := httptest.NewRecorder()
	svc2.HandleLogin(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the lockout to survive a restart on the same data_dir, got %d", rec.Code)
	}
	var resp struct {
		RetryAfter int `json:"retry_after"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RetryAfter <= 0 {
		t.Errorf("expected retry_after > 0, got %d", resp.RetryAfter)
	}
}

// TestResetPersistedAuthState_ClearsFiles проверяет ResetPersistedAuthState
// (CLI-сброс пароля, 134-10): sessions.json и ratelimit.json существуют,
// пусты и 0600 после вызова; пустой dataDir — no-op.
func TestResetPersistedAuthState_ClearsFiles(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})
	if _, err := svc.CreateSession(); err != nil {
		t.Fatal(err)
	}
	_ = svc.rateLimiter.CheckLimit("1.2.3.4", 3, time.Minute)
	svc.Stop()

	if err := ResetPersistedAuthState(dir); err != nil {
		t.Fatalf("ResetPersistedAuthState failed: %v", err)
	}

	for _, name := range []string{sessionsFileName, rateLimitFileName} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("expected %s permissions 0600, got %04o", name, perm)
		}
	}

	fresh := NewAuthService(Options{DataDir: dir})
	defer fresh.Stop()
	fresh.mu.RLock()
	sessionCount := len(fresh.sessions)
	fresh.mu.RUnlock()
	if sessionCount != 0 {
		t.Errorf("expected 0 sessions after reset, got %d", sessionCount)
	}
	if rem := fresh.rateLimiter.GetLockoutRemaining("1.2.3.4"); rem != 0 {
		t.Errorf("expected rate limiter cleared after reset, got remaining=%v", rem)
	}

	if err := ResetPersistedAuthState(""); err != nil {
		t.Errorf("expected nil for empty dataDir, got %v", err)
	}
}
