package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	SessionCookieName = "xcp_session"
	CSRFHeaderName    = "X-CSRF-Token"
	SessionDuration   = 24 * time.Hour
)

type AuthService struct {
	passwordHash  string
	sessionSecret []byte
	sessions      map[string]*Session
	rateLimiter   *RateLimiter
	mu            sync.RWMutex
}

type Session struct {
	Token     string
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type RateLimiter struct {
	attempts map[string]*LoginAttempts
	mu       sync.RWMutex
}

type LoginAttempts struct {
	Count       int
	LastAttempt time.Time
	LockedUntil time.Time
}

func NewAuthService(passwordHash string) *AuthService {
	secret := make([]byte, 32)
	rand.Read(secret)

	return &AuthService{
		passwordHash:  passwordHash,
		sessionSecret: secret,
		sessions:      make(map[string]*Session),
		rateLimiter:   &RateLimiter{attempts: make(map[string]*LoginAttempts)},
	}
}

func (a *AuthService) SetPasswordHash(hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passwordHash = hash
}

func (a *AuthService) GetPasswordHash() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.passwordHash
}

func (a *AuthService) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (a *AuthService) VerifyPassword(password string) error {
	a.mu.RLock()
	hash := a.passwordHash
	a.mu.RUnlock()

	if hash == "" {
		return errors.New("password not set")
	}

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (a *AuthService) CreateSession() (*Session, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}

	csrfToken := make([]byte, 32)
	if _, err := rand.Read(csrfToken); err != nil {
		return nil, err
	}

	session := &Session{
		Token:     base64.URLEncoding.EncodeToString(token),
		CSRFToken: base64.URLEncoding.EncodeToString(csrfToken),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(SessionDuration),
	}

	a.mu.Lock()
	a.sessions[session.Token] = session
	a.mu.Unlock()

	return session, nil
}

func (a *AuthService) ValidateSession(token string) (*Session, error) {
	a.mu.RLock()
	session, exists := a.sessions[token]
	a.mu.RUnlock()

	if !exists {
		return nil, errors.New("session not found")
	}

	if time.Now().After(session.ExpiresAt) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return nil, errors.New("session expired")
	}

	return session, nil
}

func (a *AuthService) DeleteSession(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

func (a *AuthService) ValidateCSRF(session *Session, csrfToken string) bool {
	return hmac.Equal([]byte(session.CSRFToken), []byte(csrfToken))
}

func (rl *RateLimiter) CheckLimit(ip string, maxAttempts int, lockoutDuration time.Duration) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	attempts, exists := rl.attempts[ip]
	if !exists {
		rl.attempts[ip] = &LoginAttempts{Count: 1, LastAttempt: time.Now()}
		return nil
	}

	if time.Now().Before(attempts.LockedUntil) {
		return errors.New("too many login attempts, account locked")
	}

	if time.Since(attempts.LastAttempt) > 15*time.Minute {
		attempts.Count = 1
		attempts.LastAttempt = time.Now()
		return nil
	}

	attempts.Count++
	attempts.LastAttempt = time.Now()

	if attempts.Count >= maxAttempts {
		attempts.LockedUntil = time.Now().Add(lockoutDuration)
		return errors.New("too many login attempts, account locked")
	}

	return nil
}

func (rl *RateLimiter) ResetAttempts(ip string) {
	rl.mu.Lock()
	delete(rl.attempts, ip)
	rl.mu.Unlock()
}

// Middleware
func (a *AuthService) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, err := a.ValidateSession(cookie.Value)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Validate CSRF for mutating requests
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			csrfToken := r.Header.Get(CSRFHeaderName)
			if !a.ValidateCSRF(session, csrfToken) {
				http.Error(w, "CSRF validation failed", http.StatusForbidden)
				return
			}
		}

		next(w, r)
	}
}

// Handlers
func (a *AuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ip := r.RemoteAddr
	if err := a.rateLimiter.CheckLimit(ip, 5, 5*time.Minute); err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := a.VerifyPassword(req.Password); err != nil {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	a.rateLimiter.ResetAttempts(ip)

	session, err := a.CreateSession()
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
	})

	json.NewEncoder(w).Encode(map[string]string{
		"csrf_token": session.CSRFToken,
	})
}

func (a *AuthService) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err == nil {
		a.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
}

func (a *AuthService) HandleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated":  false,
			"setup_required": a.GetPasswordHash() == "",
		})
		return
	}

	session, err := a.ValidateSession(cookie.Value)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated":  false,
			"setup_required": a.GetPasswordHash() == "",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"csrf_token":    session.CSRFToken,
	})
}

func (a *AuthService) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.GetPasswordHash() != "" {
		http.Error(w, "Setup already completed", http.StatusForbidden)
		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := a.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	a.SetPasswordHash(hash)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
