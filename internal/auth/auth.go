package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// SessionCookieName использует префикс __Host- (RFC 6265bis): браузер
	// принимает такую cookie только по HTTPS, без Domain и с Path=/ — то же
	// самое, что уже требует D-15 (Secure+HttpOnly+SameSite=Strict). Ослабляет
	// класс атак с поддоменов/самоподписанными cookie на других портах.
	SessionCookieName = "__Host-xcp_session"
	// LegacySessionCookieName — имя cookie до фазы 134; активно очищается на
	// входе и выходе, пока у клиентов остаётся старая cookie в браузере.
	LegacySessionCookieName = "xcp_session"
	CSRFHeaderName          = "X-CSRF-Token"

	// defaultIdleTTL/defaultAbsoluteTTL — дефолты D-05, применяются когда
	// Options не задаёт своих значений (например, тесты, вызывающие
	// NewAuthService с нулевым Options.IdleTTL/AbsoluteTTL). В проде реальные
	// значения приходят из config.json (session_idle_ttl_hours /
	// session_absolute_ttl_days), см. cmd/xcp/main.go.
	defaultIdleTTL     = 24 * time.Hour
	defaultAbsoluteTTL = 30 * 24 * time.Hour
)

// Options конфигурирует AuthService. Заменяет прежний набор позиционных
// аргументов NewAuthService — новые поля (IdleTTL/AbsoluteTTL/DataDir)
// добавляются без изменения сигнатуры конструктора.
type Options struct {
	PasswordHash     string
	MaxLoginAttempts int
	LockoutDuration  time.Duration
	IdleTTL          time.Duration
	AbsoluteTTL      time.Duration
	DataDir          string
	OnPasswordSet    func(string) error
}

type AuthService struct {
	passwordHash string
	sessions     map[string]*Session // ключ — hashToken(сырой токен)
	rateLimiter  *RateLimiter
	mu           sync.RWMutex
	// pwMu сериализует смену пароля целиком (проверка → запись на диск →
	// применение): иначе параллельные запросы оставят в памяти и на диске
	// разные пароли
	pwMu             sync.Mutex
	onPasswordSet    func(string) error
	maxLoginAttempts int
	lockoutDuration  time.Duration
	idleTTL          time.Duration
	absoluteTTL      time.Duration
	store            *fileStore
	now              func() time.Time
	stopCh           chan struct{}
	stopOnce         sync.Once
}

// Session — состояние сессии, как оно живёт в памяти AuthService.
// Сырые Token/CSRFToken здесь не хранятся (D-01/D-02): только их SHA-256,
// тот же инвариант — на диске (см. persistedSession в store.go).
type Session struct {
	ID         string
	TokenHash  string
	CSRFHash   string
	CreatedAt  time.Time
	LastSeen   time.Time
	RememberMe bool
	UserAgent  string
	IP         string
}

// SessionMeta — контекст входа, известный на момент создания сессии.
type SessionMeta struct {
	IP         string
	UserAgent  string
	RememberMe bool
}

// IssuedSession — то, что реально возвращается вызывающему коду при создании
// сессии. Единственное место, где сырые Token/CSRFToken существуют в памяти
// дольше одного выражения — они никогда не попадают в Session/на диск.
type IssuedSession struct {
	ID         string
	Token      string
	CSRFToken  string
	CreatedAt  time.Time
	RememberMe bool
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

func NewAuthService(opts Options) *AuthService {
	if opts.MaxLoginAttempts <= 0 {
		opts.MaxLoginAttempts = 5
	}
	if opts.LockoutDuration <= 0 {
		opts.LockoutDuration = 5 * time.Minute
	}
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = defaultIdleTTL
	}
	if opts.AbsoluteTTL <= 0 {
		opts.AbsoluteTTL = defaultAbsoluteTTL
	}

	svc := &AuthService{
		passwordHash:     opts.PasswordHash,
		sessions:         make(map[string]*Session),
		rateLimiter:      &RateLimiter{attempts: make(map[string]*LoginAttempts)},
		onPasswordSet:    opts.OnPasswordSet,
		maxLoginAttempts: opts.MaxLoginAttempts,
		lockoutDuration:  opts.LockoutDuration,
		idleTTL:          opts.IdleTTL,
		absoluteTTL:      opts.AbsoluteTTL,
		store:            newFileStore(opts.DataDir),
		now:              time.Now,
		stopCh:           make(chan struct{}),
	}

	// Восстановление сессий, переживших рестарт процесса (SESS-01, D-01).
	// Уже истёкшие (idle или absolute) при загрузке не восстанавливаются.
	now := svc.now()
	for _, s := range svc.store.loadSessions(passwordFingerprint(opts.PasswordHash)) {
		if now.Sub(s.CreatedAt) >= svc.absoluteTTL || now.Sub(s.LastSeen) >= svc.idleTTL {
			continue
		}
		svc.sessions[s.TokenHash] = s
	}

	svc.startCleanup()
	return svc
}

// Stop останавливает фоновые горутины очистки и делает финальный Flush
// сессий на диск. Идемпотентен — повторный вызов безопасен (stopOnce).
func (a *AuthService) Stop() {
	a.stopOnce.Do(func() {
		if err := a.Flush(); err != nil {
			log.Printf("[auth] final flush on Stop failed: %v", err)
		}
		if a.store != nil {
			a.store.close()
		}
		close(a.stopCh)
	})
}

// snapshotSessionsLocked копирует текущие сессии по значению (не по
// указателю), пока вызывающий код держит a.mu — это единственный способ
// затем безопасно сериализовать снимок за пределами блокировки без гонки с
// одновременной записью LastSeen другим запросом (go test -race).
func (a *AuthService) snapshotSessionsLocked() (string, []*Session) {
	sessions := make([]*Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		cp := *s
		sessions = append(sessions, &cp)
	}
	return passwordFingerprint(a.passwordHash), sessions
}

// Flush сохраняет снимок текущих сессий на диск синхронно.
func (a *AuthService) Flush() error {
	a.mu.Lock()
	fingerprint, snapshot := a.snapshotSessionsLocked()
	a.mu.Unlock()
	return a.store.saveSessions(fingerprint, snapshot)
}

// persistSessions — Flush() с логированием ошибки вместо её возврата;
// используется после структурных изменений сессий (создание/удаление),
// которые сами по себе не возвращают ошибку вызывающему HTTP-хендлеру.
func (a *AuthService) persistSessions() {
	if err := a.Flush(); err != nil {
		log.Printf("[auth] failed to persist sessions: %v", err)
	}
}

// persistSnapshot — как persistSessions, но принимает уже готовый снимок
// (снятый под a.mu в вызывающем коде, например в cleanupSessions).
func (a *AuthService) persistSnapshot(fingerprint string, sessions []*Session) {
	if err := a.store.saveSessions(fingerprint, sessions); err != nil {
		log.Printf("[auth] failed to persist sessions: %v", err)
	}
}

// ChangePassword validates the current password and replaces it with a new bcrypt hash.
// Returns bcrypt.ErrMismatchedHashAndPassword if currentPassword is wrong.
// ErrTooManyAttempts — превышен лимит попыток ввода пароля.
var ErrTooManyAttempts = errors.New("too many attempts")

// ChangePassword меняет пароль администратора.
//
//   - попытки ввода текущего пароля ограничены тем же лимитом, что и вход
//     (ip), — украденная сессия не даёт подбирать пароль без ограничений;
//   - новый хеш сначала сохраняется на диск и только потом применяется:
//     при ошибке записи действующим остаётся старый пароль;
//   - все сессии, кроме текущей (keepToken), завершаются — и в памяти, и на
//     диске (новый password_fingerprint делает старые сессии недействительными
//     и после рестарта).
func (a *AuthService) ChangePassword(ip, keepToken, currentPassword, newPassword string) error {
	if err := a.rateLimiter.CheckLimit(ip, a.maxLoginAttempts, a.lockoutDuration); err != nil {
		return ErrTooManyAttempts
	}
	a.pwMu.Lock()
	defer a.pwMu.Unlock()
	if err := a.VerifyPassword(currentPassword); err != nil {
		return err
	}
	a.rateLimiter.ResetAttempts(ip)

	newHash, err := a.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if a.onPasswordSet != nil {
		if err := a.onPasswordSet(newHash); err != nil {
			return err
		}
	}
	a.SetPasswordHash(newHash)
	a.deleteSessionsExcept(keepToken)
	return nil
}

// deleteSessionsExcept завершает все сессии, кроме keepToken (сырой токен),
// в памяти и синхронно на диске.
func (a *AuthService) deleteSessionsExcept(keepToken string) {
	keepHash := hashToken(keepToken)
	a.mu.Lock()
	for hash := range a.sessions {
		if hash != keepHash {
			delete(a.sessions, hash)
		}
	}
	a.mu.Unlock()
	a.persistSessions()
}

func (a *AuthService) startCleanup() {
	go a.cleanupSessions()
	go a.cleanupRateLimiter()
}

// cleanupSessions выселяет по тикеру сессии, истёкшие по idle- или
// absolute-TTL, и сохраняет снимок на диск, только если что-то было удалено
// (structural change) — сама по себе периодическая проверка не пишет на
// флеш, если выселять нечего.
func (a *AuthService) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.mu.Lock()
			now := a.now()
			removed := false
			for hash, s := range a.sessions {
				if now.Sub(s.CreatedAt) >= a.absoluteTTL || now.Sub(s.LastSeen) >= a.idleTTL {
					delete(a.sessions, hash)
					removed = true
				}
			}
			var fingerprint string
			var snapshot []*Session
			if removed {
				fingerprint, snapshot = a.snapshotSessionsLocked()
			}
			a.mu.Unlock()
			if removed {
				a.persistSnapshot(fingerprint, snapshot)
			}
		case <-a.stopCh:
			return
		}
	}
}

func (a *AuthService) cleanupRateLimiter() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			a.rateLimiter.mu.Lock()
			for k, v := range a.rateLimiter.attempts {
				// Удаляем запись, если блокировка истекла и с момента последней попытки прошло достаточно времени
				if now.After(v.LockedUntil) && now.Sub(v.LastAttempt) > a.lockoutDuration*2 {
					delete(a.rateLimiter.attempts, k)
				}
			}
			a.rateLimiter.mu.Unlock()
		case <-a.stopCh:
			return
		}
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

// DeriveCSRFToken выводит CSRF-токен из session-токена детерминированно
// (HMAC-SHA256, ключ — сам токен). Позволяет восстановить действующий
// CSRF-токен из cookie после рестарта процесса, не храня его сырым нигде,
// кроме памяти запроса (D-02): на диске и в Session — только hashToken(CSRF).
func DeriveCSRFToken(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("xcp-csrf-v1"))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// CreateSessionWithMeta создаёt новую сессию с контекстом входа (IP,
// User-Agent, «Запомнить меня»). Записывает синхронный снимок на диск.
func (a *AuthService) CreateSessionWithMeta(meta SessionMeta) (*IssuedSession, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)
	csrfToken := DeriveCSRFToken(token)

	a.mu.Lock()
	now := a.now()
	session := &Session{
		ID:         hex.EncodeToString(idBytes),
		TokenHash:  hashToken(token),
		CSRFHash:   hashToken(csrfToken),
		CreatedAt:  now,
		LastSeen:   now,
		RememberMe: meta.RememberMe,
		UserAgent:  meta.UserAgent,
		IP:         meta.IP,
	}
	a.sessions[session.TokenHash] = session
	a.mu.Unlock()

	a.persistSessions()

	return &IssuedSession{
		ID:         session.ID,
		Token:      token,
		CSRFToken:  csrfToken,
		CreatedAt:  session.CreatedAt,
		RememberMe: session.RememberMe,
	}, nil
}

// CreateSession — обёртка CreateSessionWithMeta без контекста входа,
// используется тестами и местами, где IP/UA/remember_me не важны.
func (a *AuthService) CreateSession() (*IssuedSession, error) {
	return a.CreateSessionWithMeta(SessionMeta{})
}

// ValidateSession ищет сессию по хешу токена и проверяет её срок:
// недействительна, если now >= CreatedAt+absoluteTTL или
// now >= LastSeen+idleTTL (граница уже считается истечением). При активности
// продлевает LastSeen только в памяти — на диск это не пишется на каждый
// запрос (T-134-05), периодическая persist-логика — Task 3 (flushLoop).
func (a *AuthService) ValidateSession(token string) (*Session, error) {
	tokenHash := hashToken(token)

	a.mu.Lock()
	now := a.now()
	session, exists := a.sessions[tokenHash]
	if !exists {
		a.mu.Unlock()
		return nil, errors.New("session not found")
	}

	if now.Sub(session.CreatedAt) >= a.absoluteTTL || now.Sub(session.LastSeen) >= a.idleTTL {
		delete(a.sessions, tokenHash)
		fingerprint, snapshot := a.snapshotSessionsLocked()
		a.mu.Unlock()
		a.persistSnapshot(fingerprint, snapshot)
		return nil, errors.New("session expired")
	}

	session.LastSeen = now
	a.mu.Unlock()

	return session, nil
}

func (a *AuthService) DeleteSession(token string) {
	a.mu.Lock()
	delete(a.sessions, hashToken(token))
	a.mu.Unlock()
	a.persistSessions()
}

// ValidateCSRF сравнивает хеш CSRF-токена из заголовка с сохранённым при
// сессии CSRFHash — не зависит от того, восстановлена сессия из файла или
// создана в этом же процессе (Pattern 3, вариант «б»).
func (a *AuthService) ValidateCSRF(session *Session, csrfToken string) bool {
	return hmac.Equal([]byte(hashToken(csrfToken)), []byte(session.CSRFHash))
}

// SetTTL меняет idle/absolute TTL сессий немедленно — уже существующие
// сессии проверяются по новым значениям при следующем ValidateSession
// (срок считается от CreatedAt/LastSeen на лету, а не кешируется при
// создании сессии), а не только новые сессии, созданные после вызова.
// Нулевое значение аргумента оставляет соответствующий TTL без изменений.
func (a *AuthService) SetTTL(idle, absolute time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if idle > 0 {
		a.idleTTL = idle
	}
	if absolute > 0 {
		a.absoluteTTL = absolute
	}
}

// TTL возвращает текущие idle/absolute TTL.
func (a *AuthService) TTL() (idle, absolute time.Duration) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.idleTTL, a.absoluteTTL
}

// setSessionCookie выставляет cookie сессии одинаково при входе и выходе
// (D-15, T-134-03): HttpOnly + Secure + SameSite=Strict + Path=/. Domain
// сознательно не выставляется — непустой Domain делает cookie с префиксом
// __Host- недействительной для браузера (RFC 6265bis, __Host- требует
// отсутствия Domain и Path=/). При rememberMe выставляется Max-Age по
// absoluteTTL (постоянная cookie); без него — cookie сессии браузера,
// живущая до его закрытия, хотя серверный TTL один и тот же в обоих
// случаях (D-07). Одновременно очищает cookie legacy-имени (миграция с
// xcp_session на __Host-xcp_session).
func setSessionCookie(w http.ResponseWriter, token string, rememberMe bool, absoluteTTL time.Duration) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	if rememberMe {
		cookie.MaxAge = int(absoluteTTL.Seconds())
	}
	http.SetCookie(w, cookie)
	clearLegacySessionCookie(w)
}

// clearLegacySessionCookie гасит cookie старого имени (xcp_session), если
// она осталась в браузере с версии до фазы 134.
func clearLegacySessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     LegacySessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// clearSessionCookies очищает текущую и legacy cookie сессии — симметрично
// setSessionCookie, используется при выходе (D-15: атрибуты одинаковы на
// входе и на выходе).
func clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	clearLegacySessionCookie(w)
}

func (rl *RateLimiter) CheckLimit(ip string, maxAttempts int, lockoutDuration time.Duration) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Evict stale entries
	now := time.Now()
	for k, v := range rl.attempts {
		if now.After(v.LockedUntil.Add(lockoutDuration)) && now.Sub(v.LastAttempt) > 2*lockoutDuration {
			delete(rl.attempts, k)
		}
	}

	attempts, exists := rl.attempts[ip]
	if !exists {
		rl.attempts[ip] = &LoginAttempts{Count: 1, LastAttempt: now}
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

// GetLockoutRemaining returns the duration remaining for the lockout of the given IP address.
func (rl *RateLimiter) GetLockoutRemaining(ip string) time.Duration {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	attempts, exists := rl.attempts[ip]
	if !exists {
		return 0
	}
	remaining := time.Until(attempts.LockedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}

// Middleware
func (a *AuthService) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		session, err := a.ValidateSession(cookie.Value)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		// Validate CSRF for mutating requests
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			csrfToken := r.Header.Get(CSRFHeaderName)
			if !a.ValidateCSRF(session, csrfToken) {
				jsonError(w, http.StatusForbidden, "CSRF validation failed")
				return
			}
		}

		next(w, r)
	}
}

// Handlers
func (a *AuthService) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if err := a.rateLimiter.CheckLimit(ip, a.maxLoginAttempts, a.lockoutDuration); err != nil {
		remaining := a.rateLimiter.GetLockoutRemaining(ip)
		seconds := int(remaining.Seconds())
		if seconds <= 0 {
			seconds = int(a.lockoutDuration.Seconds())
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       "too many attempts, account locked",
			"retry_after": seconds,
		})
		return
	}

	var req struct {
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if err := a.VerifyPassword(req.Password); err != nil {
		jsonError(w, http.StatusUnauthorized, "Invalid password")
		return
	}

	a.rateLimiter.ResetAttempts(ip)

	ua := r.UserAgent()
	if len(ua) > 256 {
		ua = ua[:256]
	}
	issued, err := a.CreateSessionWithMeta(SessionMeta{IP: ip, UserAgent: ua, RememberMe: req.RememberMe})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	_, absoluteTTL := a.TTL()
	setSessionCookie(w, issued.Token, issued.RememberMe, absoluteTTL)

	json.NewEncoder(w).Encode(map[string]string{
		"csrf_token": issued.CSRFToken,
	})
}

func (a *AuthService) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err == nil {
		a.DeleteSession(cookie.Value)
	}

	clearSessionCookies(w)

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

	_, err = a.ValidateSession(cookie.Value)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated":  false,
			"setup_required": a.GetPasswordHash() == "",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		// Выводится из токена cookie, а не из памяти сессии — тот же CSRF
		// после рестарта процесса, без хранения сырого CSRF где-либо (D-02).
		"csrf_token": DeriveCSRFToken(cookie.Value),
	})
}

func (a *AuthService) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if a.GetPasswordHash() != "" {
		jsonError(w, http.StatusForbidden, "Setup already completed")
		return
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if err := a.rateLimiter.CheckLimit(ip, a.maxLoginAttempts, a.lockoutDuration); err != nil {
		remaining := a.rateLimiter.GetLockoutRemaining(ip)
		seconds := int(remaining.Seconds())
		if seconds <= 0 {
			seconds = int(a.lockoutDuration.Seconds())
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       "too many attempts, account locked",
			"retry_after": seconds,
		})
		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	if len(req.Password) < 8 {
		jsonError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	hash, err := a.HashPassword(req.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	// Повторная проверка под pwMu: параллельный первичный запрос мог уже
	// задать пароль, и этот не должен его перезаписать
	a.pwMu.Lock()
	defer a.pwMu.Unlock()
	if a.GetPasswordHash() != "" {
		jsonError(w, http.StatusForbidden, "Setup already completed")
		return
	}
	if a.onPasswordSet != nil {
		if err := a.onPasswordSet(hash); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to save password")
			return
		}
	}
	a.SetPasswordHash(hash)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
