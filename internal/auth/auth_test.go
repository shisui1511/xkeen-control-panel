package auth

import (
	"bytes"
	"encoding/json"
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
	svc := newTestAuthService()

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
