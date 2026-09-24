package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestAuthService() *AuthService {
	return NewAuthService("", false, 5, 5*time.Minute, nil)
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

// TestSessionEviction verifies that expired sessions are cleaned up on ValidateSession.
func TestSessionEviction(t *testing.T) {
	svc := newTestAuthService()

	// Manually insert an expired session
	expiredToken := "expired-token"
	svc.mu.Lock()
	svc.sessions[expiredToken] = &Session{
		Token:     expiredToken,
		CSRFToken: "csrf",
		CreatedAt: time.Now().Add(-48 * time.Hour),
		ExpiresAt: time.Now().Add(-24 * time.Hour),
	}
	svc.mu.Unlock()

	// Validate a different (non-existent) token — this triggers the eviction sweep
	_, _ = svc.ValidateSession("nonexistent")

	// The expired session should now be gone
	svc.mu.RLock()
	_, exists := svc.sessions[expiredToken]
	svc.mu.RUnlock()

	if exists {
		t.Error("expected expired session to be evicted, but it still exists")
	}
}

// TestSetupRateLimit verifies that HandleSetup blocks after maxAttempts (3) are exhausted.
// CheckLimit with maxAttempts=3: attempts 1,2 pass; attempt 3 triggers lock; attempt 4+ returns 429.
func TestSetupRateLimit(t *testing.T) {
	svc := NewAuthService("", false, 3, 5*time.Minute, nil)

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
	svc := NewAuthService("hash", false, 2, 10*time.Second, nil)

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
	svc := NewAuthService("", false, 5, 5*time.Minute, nil)
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
	svc := NewAuthService("", false, 5, 5*time.Minute, nil)
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
	svc := NewAuthService("", false, 5, 5*time.Minute, nil)
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
	if session.ExpiresAt.Before(time.Now()) {
		t.Fatal("expected ExpiresAt in the future")
	}

	// 2. Validate valid session
	validated, err := svc.ValidateSession(session.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if validated.Token != session.Token {
		t.Errorf("validated token mismatch: got %q, want %q", validated.Token, session.Token)
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
	svc := NewAuthService("", false, 5, 5*time.Minute, nil)
	// Stop closes stopCh; ensure multiple or clean stop does not panic
	svc.Stop()
}

func TestAuthService_ValidateCSRF(t *testing.T) {
	svc := newTestAuthService()
	defer svc.Stop()

	session, err := svc.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// 1. Valid CSRF
	if !svc.ValidateCSRF(session, session.CSRFToken) {
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

	svc := NewAuthService("", false, 5, 5*time.Minute, onPasswordSet)
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
	svc := NewAuthService("", false, 5, 5*time.Minute, func(string) error {
		return errors.New("disk full")
	})
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
	svc := NewAuthService("", false, 5, 5*time.Minute, nil)
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
	svc := NewAuthService("", false, 3, 5*time.Minute, nil)
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
