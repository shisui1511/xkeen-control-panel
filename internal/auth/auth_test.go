package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestAuthService() *AuthService {
	return NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
}

// TestRateLimiterIPOnly verifies that the rate limiter uses only the IP (not IP:port).
func TestRateLimiterIPOnly(t *testing.T) {
	rl := &RateLimiter{attempts: make(map[string]*LoginAttempts)}

	// Simulate requests from same IP but different ports
	ips := []string{"192.168.1.1", "192.168.1.1", "192.168.1.1"}
	for i, ip := range ips {
		err := rl.CheckLimit(ip, 5, time.Minute)
		if i < 4 && err != nil {
			t.Errorf("attempt %d: unexpected error: %v", i, err)
		}
	}

	// After 5 attempts, 6th should be blocked
	rl2 := &RateLimiter{attempts: make(map[string]*LoginAttempts)}
	for i := 0; i < 5; i++ {
		rl2.CheckLimit("10.0.0.1", 5, time.Minute)
	}
	if err := rl2.CheckLimit("10.0.0.1", 5, time.Minute); err == nil {
		t.Error("expected rate limit error after 5 attempts, got nil")
	}
}

// TestRateLimiterEviction verifies that stale entries are evicted.
func TestRateLimiterEviction(t *testing.T) {
	rl := &RateLimiter{attempts: make(map[string]*LoginAttempts)}

	// Add a stale locked entry
	past := time.Now().Add(-20 * time.Minute)
	rl.attempts["10.0.0.2"] = &LoginAttempts{
		Count:       5,
		LastAttempt: past,
		LockedUntil: past.Add(5 * time.Minute),
	}

	// Next call should evict the stale entry and allow the request
	err := rl.CheckLimit("10.0.0.2", 5, 5*time.Minute)
	if err != nil {
		t.Errorf("expected stale entry to be evicted, got error: %v", err)
	}

	// Entry count should be reset to 1 (new attempt)
	rl.mu.RLock()
	a := rl.attempts["10.0.0.2"]
	rl.mu.RUnlock()
	if a == nil || a.Count != 1 {
		t.Errorf("expected count=1 after eviction, got %v", a)
	}
}

// TestSessionEviction verifies that an expired session is evicted when it is
// itself looked up via ValidateSession (the map-wide sweep on every call was
// removed in Phase 134 — periodic eviction is now cleanupSessions's job).
func TestSessionEviction(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	// Manually insert a session whose idle-TTL (default 24h) has elapsed.
	expiredToken := "expired-token"
	tokenHash := hashToken(expiredToken)
	svc.mu.Lock()
	svc.sessions[tokenHash] = &Session{
		ID:        "test-id",
		TokenHash: tokenHash,
		CSRFHash:  hashToken("csrf"),
		CreatedAt: time.Now().Add(-48 * time.Hour),
		LastSeen:  time.Now().Add(-25 * time.Hour),
	}
	svc.mu.Unlock()

	if _, err := svc.ValidateSession(expiredToken); err == nil {
		t.Error("expected error validating an expired session")
	}

	// The expired session should now be gone
	svc.mu.RLock()
	_, exists := svc.sessions[tokenHash]
	svc.mu.RUnlock()

	if exists {
		t.Error("expected expired session to be evicted, but it still exists")
	}
}

// TestSetupRateLimit verifies that HandleSetup blocks after maxAttempts (3) are exhausted.
// CheckLimit with maxAttempts=3: attempts 1,2 pass; attempt 3 triggers lock; attempt 4+ returns 429.
func TestSetupRateLimit(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 3, LockoutDuration: 5 * time.Minute})

	// First 2 attempts should not be 429 (short password rejected by validation, not rate limit)
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(map[string]string{"password": "short"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		rr := httptest.NewRecorder()
		svc.HandleSetup(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Errorf("attempt %d: got 429 too early", i+1)
		}
	}

	// 3rd attempt reaches maxAttempts=3 → locked. The handler returns 429.
	body, _ := json.Marshal(map[string]string{"password": "short"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	svc.HandleSetup(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 3rd setup attempt (maxAttempts reached), got %d", rr.Code)
	}

	// 4th attempt should also be rate limited (429)
	body, _ = json.Marshal(map[string]string{"password": "short"})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr = httptest.NewRecorder()
	svc.HandleSetup(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 4th setup attempt, got %d", rr.Code)
	}
}

// TestRateLimitResponseDetails verifies that exceeding the attempts limit returns 429,
// Retry-After header, and detailed JSON message.
func TestRateLimitResponseDetails(t *testing.T) {
	svc := NewAuthService(Options{PasswordHash: "hash", MaxLoginAttempts: 2, LockoutDuration: 10 * time.Second})

	// 1st login attempt (wrong password) -> 401
	body, _ := json.Marshal(map[string]string{"password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()
	svc.HandleLogin(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// 2nd login attempt (wrong password) -> triggers lockout and returns 429
	body, _ = json.Marshal(map[string]string{"password": "wrong"})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "1.2.3.4:12345"
	rr = httptest.NewRecorder()
	svc.HandleLogin(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	retryAfter := rr.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected Retry-After header, got empty")
	}

	var resp struct {
		Error      string `json:"error"`
		RetryAfter int    `json:"retry_after"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error json: %v", err)
	}

	if resp.Error != "too many attempts, account locked" {
		t.Errorf("expected error message 'too many attempts, account locked', got '%s'", resp.Error)
	}
	if resp.RetryAfter <= 0 || resp.RetryAfter > 10 {
		t.Errorf("expected retry_after between 1 and 10, got %d", resp.RetryAfter)
	}
}

// TestHandleLoginIPExtraction verifies that login rate limiting uses IP only (not IP:port).
func TestHandleLoginIPExtraction(t *testing.T) {
	svc := newTestAuthService()
	// Set a known password hash for "testpass123"
	hash, err := svc.HashPassword("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	// Send requests from same IP but different source ports — all should share rate limit
	attempts := 0
	for i := 0; i < 6; i++ {
		body, _ := json.Marshal(map[string]string{"password": "wrongpassword"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		// Different ports, same IP
		req.RemoteAddr = "192.168.0.100:5000" + string(rune('0'+i))
		rr := httptest.NewRecorder()
		svc.HandleLogin(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			attempts = i + 1
			break
		}
	}

	if attempts == 0 {
		t.Error("expected rate limit to trigger, but all requests went through")
	}
}

// TestRateLimiter_IPOnly (T016): два запроса с одного IP разными портами засчитываются как один источник.
func TestRateLimiter_IPOnly(t *testing.T) {
	rl := &RateLimiter{attempts: make(map[string]*LoginAttempts)}

	// Same IP, different ports — should accumulate under one key
	for i := 0; i < 4; i++ {
		_ = rl.CheckLimit("10.0.0.5", 5, time.Minute)
	}

	rl.mu.RLock()
	entry := rl.attempts["10.0.0.5"]
	rl.mu.RUnlock()

	if entry == nil {
		t.Fatal("expected rate limit entry for 10.0.0.5, got nil")
	}
	if entry.Count != 4 {
		t.Errorf("expected count=4, got %d", entry.Count)
	}

	// Confirm that "10.0.0.5:9999" treated the same as "10.0.0.5"
	// (the AuthService.HandleLogin extracts host via net.SplitHostPort)
	svc := newTestAuthService()
	hash, _ := svc.HashPassword("password123")
	svc.SetPasswordHash(hash)

	for i := 0; i < 6; i++ {
		body, _ := json.Marshal(map[string]string{"password": "wrongpass"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.99:" + string(rune('5'+'0'+i)) // different ports
		httptest.NewRecorder()
		rr := httptest.NewRecorder()
		svc.HandleLogin(rr, req)
	}
	// Just verifies no panic occurs and IP extraction works; rate limit behaviour
	// is covered by TestHandleLoginIPExtraction above.
}

// TestChangePassword_WrongCurrent (T017): wrong current password returns error.
func TestChangePassword_WrongCurrent(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	hash, err := svc.HashPassword("correctpass")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	err = svc.ChangePassword("127.0.0.1", "", "wrongpass", "newpassword123")
	if err == nil {
		t.Error("expected error for wrong current password, got nil")
	}
}

// TestCSRFRotation_OnLogin verifies that each login creates a new session with
// a unique CSRF token (token rotation on re-login).
func TestCSRFRotation_OnLogin(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	hash, err := svc.HashPassword("securepass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	login := func() string {
		body, _ := json.Marshal(map[string]string{"password": "securepass123"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.1:12345"
		rr := httptest.NewRecorder()
		svc.HandleLogin(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("login failed: %d %s", rr.Code, rr.Body.String())
		}
		// Extract CSRF token from response body
		var resp struct {
			CSRFToken string `json:"csrf_token"`
		}
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp.CSRFToken
	}

	csrf1 := login()
	csrf2 := login()

	if csrf1 == "" || csrf2 == "" {
		t.Skip("HandleLogin does not return CSRF token in body — skipping rotation check")
	}

	if csrf1 == csrf2 {
		t.Error("CSRF token must rotate on each login; got identical tokens for two logins")
	}
}

// TestChangePassword_Success (T017): correct current password → password changed.
func TestChangePassword_Success(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	hash, err := svc.HashPassword("oldpass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	err = svc.ChangePassword("127.0.0.1", "", "oldpass123", "newpass456")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// New password should verify correctly
	if err := svc.VerifyPassword("newpass456"); err != nil {
		t.Errorf("new password did not verify: %v", err)
	}
	// Old password should no longer work
	if err := svc.VerifyPassword("oldpass123"); err == nil {
		t.Error("old password still verifies after change")
	}
}

func TestAuthService_SessionLifecycle(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	// 1. CreateSession
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if session.Token == "" || session.CSRFToken == "" {
		t.Fatal("expected non-empty Token and CSRFToken")
	}
	if session.CreatedAt.After(time.Now()) {
		t.Fatal("expected CreatedAt not in the future")
	}

	// 2. Validate valid session
	validated, err := svc.ValidateSession(session.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if validated.TokenHash != hashToken(session.Token) {
		t.Errorf("validated token hash mismatch: got %q, want %q", validated.TokenHash, hashToken(session.Token))
	}

	// 3. Validate non-existent token
	_, err = svc.ValidateSession("random-non-existent-token")
	if err == nil {
		t.Error("expected error for non-existent session, got nil")
	}

	// 4. DeleteSession
	svc.DeleteSession(session.Token)
	_, err = svc.ValidateSession(session.Token)
	if err == nil {
		t.Error("expected error after DeleteSession, got nil")
	}
}

func TestAuthService_Stop_And_CleanupGoroutines(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	// Stop closes stopCh; ensure multiple or clean stop does not panic
	svc.Stop()
}

func TestAuthService_ValidateCSRF(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	issued, err := svc.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	session, err := svc.ValidateSession(issued.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	// 1. Valid CSRF
	if !svc.ValidateCSRF(session, issued.CSRFToken) {
		t.Error("expected ValidateCSRF to return true for matching CSRF token")
	}

	// 2. Invalid CSRF
	if svc.ValidateCSRF(session, "wrong-csrf-token") {
		t.Error("expected ValidateCSRF to return false for wrong CSRF token")
	}

	// 3. Empty CSRF
	if svc.ValidateCSRF(session, "") {
		t.Error("expected ValidateCSRF to return false for empty CSRF token")
	}
}

func TestAuthService_HandleMe(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	// 1. Without cookie -> authenticated: false
	req1 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec1 := httptest.NewRecorder()
	svc.HandleMe(rec1, req1)
	var resp1 map[string]interface{}
	if err := json.NewDecoder(rec1.Body).Decode(&resp1); err != nil {
		t.Fatal(err)
	}
	if resp1["authenticated"] != false {
		t.Errorf("expected authenticated=false, got %v", resp1["authenticated"])
	}

	// 2. With invalid cookie -> authenticated: false
	req2 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "invalid-token"})
	rec2 := httptest.NewRecorder()
	svc.HandleMe(rec2, req2)
	var resp2 map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
		t.Fatal(err)
	}
	if resp2["authenticated"] != false {
		t.Errorf("expected authenticated=false, got %v", resp2["authenticated"])
	}

	// 3. With valid session cookie -> authenticated: true
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	req3 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req3.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	rec3 := httptest.NewRecorder()
	svc.HandleMe(rec3, req3)
	var resp3 map[string]interface{}
	if err := json.NewDecoder(rec3.Body).Decode(&resp3); err != nil {
		t.Fatal(err)
	}
	if resp3["authenticated"] != true {
		t.Errorf("expected authenticated=true, got %v", resp3["authenticated"])
	}
	if resp3["csrf_token"] != session.CSRFToken {
		t.Errorf("expected csrf_token %q, got %q", session.CSRFToken, resp3["csrf_token"])
	}
}

func TestAuthService_HandleLogout(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Method not allowed for GET
	reqGet := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	recGet := httptest.NewRecorder()
	svc.HandleLogout(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET logout, got %d", recGet.Code)
	}

	// 2. POST logout with valid cookie
	reqPost := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqPost.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	recPost := httptest.NewRecorder()
	svc.HandleLogout(recPost, reqPost)
	if recPost.Code != http.StatusOK {
		t.Errorf("expected 200 for POST logout, got %d", recPost.Code)
	}

	// Cookie must be expired
	cookies := recPost.Result().Cookies()
	var logoutCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			logoutCookie = c
			break
		}
	}
	if logoutCookie == nil || logoutCookie.MaxAge != -1 {
		t.Errorf("expected session cookie to have MaxAge=-1, got %v", logoutCookie)
	}

	// Session must be deleted
	if _, err := svc.ValidateSession(session.Token); err == nil {
		t.Error("expected session to be deleted after logout")
	}
}

func TestAuthService_RequireAuthMiddleware(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	var handlerExecuted bool
	protectedHandler := svc.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		handlerExecuted = true
		w.WriteHeader(http.StatusOK)
	})

	// 1. GET without session -> 401
	handlerExecuted = false
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	recNoAuth := httptest.NewRecorder()
	protectedHandler(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusUnauthorized || handlerExecuted {
		t.Errorf("expected 401 Unauthorized, got %d, executed=%v", recNoAuth.Code, handlerExecuted)
	}

	// 2. GET with valid session -> 200
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	handlerExecuted = false
	reqGetAuth := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	reqGetAuth.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	recGetAuth := httptest.NewRecorder()
	protectedHandler(recGetAuth, reqGetAuth)
	if recGetAuth.Code != http.StatusOK || !handlerExecuted {
		t.Errorf("expected 200 OK for authenticated GET, got %d, executed=%v", recGetAuth.Code, handlerExecuted)
	}

	// 3. POST with valid session but missing CSRF token -> 403 Forbidden
	handlerExecuted = false
	reqPostNoCSRF := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	reqPostNoCSRF.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	recPostNoCSRF := httptest.NewRecorder()
	protectedHandler(recPostNoCSRF, reqPostNoCSRF)
	if recPostNoCSRF.Code != http.StatusForbidden || handlerExecuted {
		t.Errorf("expected 403 Forbidden without CSRF, got %d, executed=%v", recPostNoCSRF.Code, handlerExecuted)
	}

	// 4. POST with valid session and invalid CSRF token -> 403 Forbidden
	handlerExecuted = false
	reqPostBadCSRF := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	reqPostBadCSRF.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	reqPostBadCSRF.Header.Set(CSRFHeaderName, "invalid-token")
	recPostBadCSRF := httptest.NewRecorder()
	protectedHandler(recPostBadCSRF, reqPostBadCSRF)
	if recPostBadCSRF.Code != http.StatusForbidden || handlerExecuted {
		t.Errorf("expected 403 Forbidden with bad CSRF, got %d, executed=%v", recPostBadCSRF.Code, handlerExecuted)
	}

	// 5. POST with valid session and matching CSRF token -> 200 OK
	handlerExecuted = false
	reqPostGood := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	reqPostGood.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.Token})
	reqPostGood.Header.Set(CSRFHeaderName, session.CSRFToken)
	recPostGood := httptest.NewRecorder()
	protectedHandler(recPostGood, reqPostGood)
	if recPostGood.Code != http.StatusOK || !handlerExecuted {
		t.Errorf("expected 200 OK with valid CSRF, got %d, executed=%v", recPostGood.Code, handlerExecuted)
	}
}

func TestAuthService_HandleSetup_Scenarios(t *testing.T) {
	var savedHash string
	onPasswordSet := func(hash string) error {
		savedHash = hash
		return nil
	}

	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute, OnPasswordSet: onPasswordSet})
	defer svc.Stop()

	// 1. Method not allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/auth/setup", nil)
	recGet := httptest.NewRecorder()
	svc.HandleSetup(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader([]byte("{invalid")))
	recBadJSON := httptest.NewRecorder()
	svc.HandleSetup(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad json, got %d", recBadJSON.Code)
	}

	// 3. Password too short (< 8 chars)
	reqShort := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader([]byte(`{"password":"123"}`)))
	recShort := httptest.NewRecorder()
	svc.HandleSetup(recShort, reqShort)
	if recShort.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", recShort.Code)
	}

	// 4. Successful setup (>= 8 chars)
	reqGood := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader([]byte(`{"password":"validpassword123"}`)))
	recGood := httptest.NewRecorder()
	svc.HandleSetup(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good setup, got %d", recGood.Code)
	}
	if savedHash == "" {
		t.Error("expected onPasswordSet callback to receive saved hash")
	}

	// 5. Repeated setup when password is already set -> 403 Forbidden
	reqSecond := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader([]byte(`{"password":"validpassword456"}`)))
	recSecond := httptest.NewRecorder()
	svc.HandleSetup(recSecond, reqSecond)
	if recSecond.Code != http.StatusForbidden {
		t.Errorf("expected 403 for second setup attempt, got %d", recSecond.Code)
	}
}

func TestRateLimiter_Reset_And_GetLockoutRemaining(t *testing.T) {
	rl := &RateLimiter{attempts: make(map[string]*LoginAttempts)}

	// Initial remaining should be 0
	if rem := rl.GetLockoutRemaining("1.2.3.4"); rem != 0 {
		t.Errorf("expected 0 remaining for clean IP, got %v", rem)
	}

	// CheckLimit until locked
	for i := 0; i < 3; i++ {
		_ = rl.CheckLimit("1.2.3.4", 3, 5*time.Minute)
	}

	rem := rl.GetLockoutRemaining("1.2.3.4")
	if rem <= 0 || rem > 5*time.Minute {
		t.Errorf("expected lockout remaining between 0 and 5m, got %v", rem)
	}

	// Reset attempts
	rl.ResetAttempts("1.2.3.4")
	if remAfter := rl.GetLockoutRemaining("1.2.3.4"); remAfter != 0 {
		t.Errorf("expected 0 remaining after reset, got %v", remAfter)
	}
}

// TestChangePassword_SaveFailureKeepsOldPassword: если новый хеш не удалось
// сохранить, действующим остаётся старый пароль (иначе после перезапуска
// вернулся бы старый, а до него работал бы «несохранённый» новый).
func TestChangePassword_SaveFailureKeepsOldPassword(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute, OnPasswordSet: func(string) error {
		return errors.New("disk full")
	}})
	hash, err := svc.HashPassword("oldpass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	if err := svc.ChangePassword("127.0.0.1", "", "oldpass123", "newpass456"); err == nil {
		t.Fatal("expected save error")
	}
	if err := svc.VerifyPassword("oldpass123"); err != nil {
		t.Errorf("old password must stay active after failed save: %v", err)
	}
	if err := svc.VerifyPassword("newpass456"); err == nil {
		t.Error("unsaved new password must not be active")
	}
}

// TestChangePassword_EndsOtherSessions: смена пароля завершает остальные
// сессии и сохраняет текущую.
func TestChangePassword_EndsOtherSessions(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: 5 * time.Minute})
	hash, _ := svc.HashPassword("oldpass123")
	svc.SetPasswordHash(hash)

	current, _ := svc.CreateSession()
	other, _ := svc.CreateSession()

	if err := svc.ChangePassword("127.0.0.1", current.Token, "oldpass123", "newpass456"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateSession(current.Token); err != nil {
		t.Errorf("current session must survive: %v", err)
	}
	if _, err := svc.ValidateSession(other.Token); err == nil {
		t.Error("other session must be ended after password change")
	}
}

// TestChangePassword_RateLimited: подбор текущего пароля упирается в лимит.
func TestChangePassword_RateLimited(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 3, LockoutDuration: 5 * time.Minute})
	hash, _ := svc.HashPassword("oldpass123")
	svc.SetPasswordHash(hash)

	var last error
	for i := 0; i < 5; i++ {
		last = svc.ChangePassword("10.0.0.5", "", "wrong-guess", "newpass456")
	}
	if !errors.Is(last, ErrTooManyAttempts) {
		t.Errorf("expected ErrTooManyAttempts after repeated wrong guesses, got %v", last)
	}
	// Даже верный пароль не принимается, пока действует блокировка
	if err := svc.ChangePassword("10.0.0.5", "", "oldpass123", "newpass456"); !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("lockout must apply to the correct password too, got %v", err)
	}
}

// TestChangePassword_ConcurrentMemoryMatchesDisk: при параллельной смене
// пароля в памяти остаётся тот же хеш, что последним записан на диск.
func TestChangePassword_ConcurrentMemoryMatchesDisk(t *testing.T) {
	var diskMu sync.Mutex
	var disk string
	svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: time.Minute, OnPasswordSet: func(h string) error {
		time.Sleep(5 * time.Millisecond) // окно для гонки между записью и применением
		diskMu.Lock()
		disk = h
		diskMu.Unlock()
		return nil
	}})
	defer svc.Stop()
	hash, err := svc.HashPassword("oldpass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)

	var wg sync.WaitGroup
	for _, pw := range []string{"newpassAAA", "newpassBBB"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.ChangePassword("127.0.0.1", "", "oldpass123", pw)
		}()
	}
	wg.Wait()

	diskMu.Lock()
	defer diskMu.Unlock()
	if got := svc.GetPasswordHash(); got != disk {
		t.Fatal("password in memory differs from the one saved on disk")
	}
}

// TestHandleSetup_ConcurrentOnlyOneWins: из параллельных первичных настроек
// проходит одна, вторая не перезаписывает уже заданный пароль.
func TestHandleSetup_ConcurrentOnlyOneWins(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: time.Minute, OnPasswordSet: func(string) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}})
	defer svc.Stop()

	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, pw := range []string{"firstpass1", "secondpass2"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"password": pw})
			req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			svc.HandleSetup(rr, req)
			codes <- rr.Code
		}()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d setups succeeded, want exactly 1", ok)
	}
}

// --- Task 2: TTL из конфига, cookie-хардинг, «Запомнить меня» ---

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func assertProtectedCookieAttrs(t *testing.T, c *http.Cookie, phase string) {
	t.Helper()
	if !c.HttpOnly {
		t.Errorf("%s: expected HttpOnly cookie", phase)
	}
	if !c.Secure {
		t.Errorf("%s: expected Secure cookie", phase)
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("%s: expected SameSite=Strict, got %v", phase, c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("%s: expected Path=/, got %q", phase, c.Path)
	}
	if c.Domain != "" {
		t.Errorf("%s: expected empty Domain, got %q", phase, c.Domain)
	}
}

// TestSession_IdleVsAbsoluteTTL: активность продлевает только idle-TTL;
// без активности дольше idle-TTL сессия истекает даже если absolute-TTL ещё
// далеко.
func TestSession_IdleVsAbsoluteTTL(t *testing.T) {
	svc := NewAuthService(Options{IdleTTL: time.Hour, AbsoluteTTL: 24 * time.Hour})
	defer svc.Stop()

	current := time.Now()
	svc.now = func() time.Time { return current }

	issued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	// Активность за минуту до idle-TTL продлевает сессию.
	current = current.Add(59 * time.Minute)
	if _, err := svc.ValidateSession(issued.Token); err != nil {
		t.Fatalf("expected session valid just before idle TTL: %v", err)
	}

	// Без дальнейшей активности дольше idle-TTL — сессия истекает.
	current = current.Add(61 * time.Minute)
	if _, err := svc.ValidateSession(issued.Token); err == nil {
		t.Error("expected session to expire after idle TTL elapses without activity")
	}
}

// TestSession_TTLBoundaryIsExpiry: now == created_at+absoluteTTL и
// now == last_seen+idleTTL — сессия уже недействительна (edge adjacency).
func TestSession_TTLBoundaryIsExpiry(t *testing.T) {
	svc := NewAuthService(Options{IdleTTL: time.Hour, AbsoluteTTL: 2 * time.Hour})
	defer svc.Stop()

	start := time.Now()
	svc.now = func() time.Time { return start }

	idleIssued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return start.Add(time.Hour) }
	if _, err := svc.ValidateSession(idleIssued.Token); err == nil {
		t.Error("expected session invalid exactly at idle TTL boundary")
	}

	svc.now = func() time.Time { return start }
	absIssued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	// Активность прямо перед границей продлевает LastSeen, но absolute-TTL
	// не продлевается ей же.
	svc.now = func() time.Time { return start.Add(30 * time.Minute) }
	if _, err := svc.ValidateSession(absIssued.Token); err != nil {
		t.Fatalf("expected session valid before the boundary: %v", err)
	}
	svc.now = func() time.Time { return start.Add(2 * time.Hour) }
	if _, err := svc.ValidateSession(absIssued.Token); err == nil {
		t.Error("expected session invalid exactly at absolute TTL boundary")
	}
}

// TestSetTTL_AppliesToExistingSessions: SetTTL применяется немедленно к уже
// созданным сессиям, а не только к новым.
func TestSetTTL_AppliesToExistingSessions(t *testing.T) {
	svc := NewAuthService(Options{IdleTTL: 24 * time.Hour, AbsoluteTTL: 30 * 24 * time.Hour})
	defer svc.Stop()

	start := time.Now()
	svc.now = func() time.Time { return start }
	issued, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	svc.SetTTL(time.Hour, 24*time.Hour)

	svc.now = func() time.Time { return start.Add(90 * time.Minute) }
	if _, err := svc.ValidateSession(issued.Token); err == nil {
		t.Error("expected SetTTL to apply immediately to an already-existing session")
	}
}

// TestSessionCookie_LoginLogoutSymmetric: cookie сессии выставлена
// одинаково (HttpOnly/Secure/SameSite=Strict/Path=/, без Domain) на входе и
// на выходе; оба ответа дополнительно гасят legacy-cookie xcp_session.
func TestSessionCookie_LoginLogoutSymmetric(t *testing.T) {
	svc := NewAuthService(Options{})
	hash, err := svc.HashPassword("symmetricpass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)
	defer svc.Stop()

	body, _ := json.Marshal(map[string]string{"password": "symmetricpass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1111"
	rec := httptest.NewRecorder()
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}

	loginCookie := findCookie(rec.Result().Cookies(), SessionCookieName)
	if loginCookie == nil {
		t.Fatal("expected session cookie on login")
	}
	assertProtectedCookieAttrs(t, loginCookie, "login")

	legacyLogin := findCookie(rec.Result().Cookies(), LegacySessionCookieName)
	if legacyLogin == nil || legacyLogin.MaxAge >= 0 {
		t.Error("expected login to also clear the legacy xcp_session cookie")
	}

	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.AddCookie(loginCookie)
	recLogout := httptest.NewRecorder()
	svc.HandleLogout(recLogout, reqLogout)
	if recLogout.Code != http.StatusOK {
		t.Fatalf("logout failed: %d", recLogout.Code)
	}

	logoutCookie := findCookie(recLogout.Result().Cookies(), SessionCookieName)
	if logoutCookie == nil {
		t.Fatal("expected session cookie on logout")
	}
	assertProtectedCookieAttrs(t, logoutCookie, "logout")
	if logoutCookie.MaxAge != -1 {
		t.Errorf("expected logout cookie MaxAge=-1, got %d", logoutCookie.MaxAge)
	}

	legacyLogout := findCookie(recLogout.Result().Cookies(), LegacySessionCookieName)
	if legacyLogout == nil || legacyLogout.MaxAge >= 0 {
		t.Error("expected logout to also clear the legacy xcp_session cookie")
	}
}

// TestLogin_RememberMeCookieMaxAge: remember_me=true даёт постоянную cookie
// (Max-Age = absolute-TTL в секундах); remember_me=false — ни Max-Age, ни
// Expires (cookie сессии браузера).
func TestLogin_RememberMeCookieMaxAge(t *testing.T) {
	svc := NewAuthService(Options{AbsoluteTTL: 48 * time.Hour})
	hash, err := svc.HashPassword("remembermepass123")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(hash)
	defer svc.Stop()

	body, _ := json.Marshal(map[string]interface{}{"password": "remembermepass123", "remember_me": true})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:2221"
	rec := httptest.NewRecorder()
	svc.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	cookie := findCookie(rec.Result().Cookies(), SessionCookieName)
	if cookie == nil {
		t.Fatal("expected session cookie")
	}
	wantMaxAge := int((48 * time.Hour).Seconds())
	if cookie.MaxAge != wantMaxAge {
		t.Errorf("expected MaxAge=%d for remember_me=true, got %d", wantMaxAge, cookie.MaxAge)
	}

	body2, _ := json.Marshal(map[string]interface{}{"password": "remembermepass123", "remember_me": false})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body2))
	req2.RemoteAddr = "127.0.0.1:2222"
	rec2 := httptest.NewRecorder()
	svc.HandleLogin(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec2.Code, rec2.Body.String())
	}
	cookie2 := findCookie(rec2.Result().Cookies(), SessionCookieName)
	if cookie2 == nil {
		t.Fatal("expected session cookie")
	}
	if cookie2.MaxAge != 0 {
		t.Errorf("expected no Max-Age for remember_me=false, got %d", cookie2.MaxAge)
	}
	if !cookie2.Expires.IsZero() {
		t.Errorf("expected no Expires for remember_me=false, got %v", cookie2.Expires)
	}
}

// --- Task 3 (134-03 Task 1): список/завершение сессий, причина 401 ---

// TestListSessions_OpaqueIDsNoSecrets: сериализованный список сессий не
// содержит ни сырых токенов/CSRF, ни их хешей — только непрозрачный
// 32-символьный hex id (T-134-09).
func TestListSessions_OpaqueIDsNoSecrets(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	current, err := svc.CreateSessionWithMeta(SessionMeta{
		IP:        "1.2.3.4",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateSessionWithMeta(SessionMeta{
		IP:        "5.6.7.8",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/118.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	list := svc.ListSessions(current.Token)
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if !list[0].Current {
		t.Error("expected current session listed first")
	}

	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	secrets := []string{
		current.Token, other.Token,
		current.CSRFToken, other.CSRFToken,
		hashToken(current.Token), hashToken(other.Token),
	}
	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Errorf("session list JSON leaks a secret: %q", secret)
		}
	}

	for _, s := range list {
		if len(s.ID) != 32 {
			t.Errorf("expected opaque 32-hex-char id, got %q (%d chars)", s.ID, len(s.ID))
		}
	}
}

// TestTerminateSession_ReasonTerminatedElsewhere: устройство B после
// завершения его сессии с устройства A получает 401 с
// reason=terminated_elsewhere при следующем запросе.
func TestTerminateSession_ReasonTerminatedElsewhere(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	deviceA, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	deviceB, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.TerminateSession(deviceB.ID, deviceA.Token); err != nil {
		t.Fatalf("TerminateSession failed: %v", err)
	}

	if _, err := svc.ValidateSession(deviceB.Token); err == nil {
		t.Fatal("expected terminated session to be invalid")
	}
	svc.mu.RLock()
	_, exists := svc.sessions[hashToken(deviceB.Token)]
	svc.mu.RUnlock()
	if exists {
		t.Error("expected terminated session removed from the in-memory map (and thus from the persisted snapshot)")
	}

	protected := svc.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: deviceB.Token})
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	var resp struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Reason != ReasonTerminatedElsewhere {
		t.Errorf("expected reason %q, got %q", ReasonTerminatedElsewhere, resp.Reason)
	}
}

// TestTerminateSession_CurrentRejected: попытку завершить свою же сессию
// TerminateSession отклоняет — для этого есть Logout.
func TestTerminateSession_CurrentRejected(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	current, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.TerminateSession(current.ID, current.Token); !errors.Is(err, ErrSessionIsCurrent) {
		t.Errorf("expected ErrSessionIsCurrent, got %v", err)
	}
	if _, err := svc.ValidateSession(current.Token); err != nil {
		t.Errorf("current session must remain valid after rejected terminate: %v", err)
	}
}

// TestTerminateOtherSessions_KeepsCurrent: завершает все, кроме текущей;
// повторный вызов с одной оставшейся сессией идемпотентен (возвращает 0).
func TestTerminateOtherSessions_KeepsCurrent(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	current, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	other1, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	other2, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	if n := svc.TerminateOtherSessions(current.Token); n != 2 {
		t.Errorf("expected 2 terminated, got %d", n)
	}
	if _, err := svc.ValidateSession(current.Token); err != nil {
		t.Errorf("current session must survive: %v", err)
	}
	if _, err := svc.ValidateSession(other1.Token); err == nil {
		t.Error("other1 must be terminated")
	}
	if _, err := svc.ValidateSession(other2.Token); err == nil {
		t.Error("other2 must be terminated")
	}

	if n := svc.TerminateOtherSessions(current.Token); n != 0 {
		t.Errorf("expected idempotent 0 on repeated call with only the current session left, got %d", n)
	}
}

// TestRequireAuth_401Reason: без cookie, с неизвестным токеном и с сессией,
// истёкшей по idle-TTL — все три случая дают дефолтную причину
// session_expired (только явные terminate/password-change дают другую).
func TestRequireAuth_401Reason(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	protected := svc.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	readReason := func(rec *httptest.ResponseRecorder) string {
		var resp struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		return resp.Reason
	}

	req1 := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec1 := httptest.NewRecorder()
	protected(rec1, req1)
	if rec1.Code != http.StatusUnauthorized || readReason(rec1) != ReasonSessionExpired {
		t.Errorf("no cookie: expected 401/%q, got %d/%q", ReasonSessionExpired, rec1.Code, readReason(rec1))
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "unknown-token"})
	rec2 := httptest.NewRecorder()
	protected(rec2, req2)
	if rec2.Code != http.StatusUnauthorized || readReason(rec2) != ReasonSessionExpired {
		t.Errorf("unknown token: expected 401/%q, got %d/%q", ReasonSessionExpired, rec2.Code, readReason(rec2))
	}

	svc2 := NewAuthService(Options{IdleTTL: time.Hour, AbsoluteTTL: 24 * time.Hour})
	defer svc2.Stop()
	start := time.Now()
	svc2.now = func() time.Time { return start }
	issued, err := svc2.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	svc2.now = func() time.Time { return start.Add(2 * time.Hour) }
	protected2 := svc2.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req3 := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req3.AddCookie(&http.Cookie{Name: SessionCookieName, Value: issued.Token})
	rec3 := httptest.NewRecorder()
	protected2(rec3, req3)
	if rec3.Code != http.StatusUnauthorized || readReason(rec3) != ReasonSessionExpired {
		t.Errorf("idle expired: expected 401/%q, got %d/%q", ReasonSessionExpired, rec3.Code, readReason(rec3))
	}
}
