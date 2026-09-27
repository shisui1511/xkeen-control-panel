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
	"sort"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

	// Причины 401 (D-19): различают клиенту, почему сессия недействительна —
	// завершена с другого устройства/из-за смены пароля, или истекла сама
	// собой (по умолчанию, если ни одна из первых двух причин не применима).
	ReasonSessionExpired      = "session_expired"
	ReasonPasswordChanged     = "password_changed"
	ReasonTerminatedElsewhere = "terminated_elsewhere"

	// tombstoneTTL — как долго после структурного завершения сессии (не по
	// истечении TTL, а явным terminate/password change) её токен ещё даёт
	// клиенту точную причину 401 вместо дефолтного session_expired. Разумный
	// запас, чтобы клиент на другом устройстве узнал причину при следующем же
	// запросе, но не бесконечно растущая карта в памяти.
	tombstoneTTL = 10 * time.Minute
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

// tombstone — причина, по которой сессия с данным хешем токена была
// структурно завершена (не сама истекла по TTL), и до какого момента эта
// причина ещё актуальна для ответа RequireAuth.
type tombstone struct {
	reason string
	until  time.Time
}

type AuthService struct {
	passwordHash string
	sessions     map[string]*Session // ключ — hashToken(сырой токен)
	// tombstones — причины 401 для недавно завершённых сессий (D-19), ключ —
	// тот же hashToken(токен), что и в sessions. Не персистируется на диск:
	// не переживает рестарт процесса — после рестарта завершённая сессия
	// получает дефолтный ReasonSessionExpired (RESEARCH считает это приемлемым).
	tombstones  map[string]tombstone
	rateLimiter *RateLimiter
	mu          sync.RWMutex
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
	// lastSeenDirty — есть ли накопленная активность (LastSeen), не
	// записанная на диск с последнего flush; сбрасывается flushIfDirty.
	lastSeenDirty bool
	// dataDir — DataDir из Options; хранится отдельно от store (который
	// может быть nil в «только память»-режиме тестов), чтобы код настройки
	// (setupcode.go) знал, писать ли его на диск.
	dataDir string
	// memSetupCode — одноразовый код настройки (D-24) в «только память»-режиме
	// (DataDir==""); при DataDir!="" код живёт на диске (data_dir/setup_code),
	// это поле не используется.
	memSetupCode string
}

// MaxSessions — лимит одновременных сессий (D-03). При создании 21-й
// удаляется сессия с самой давней последней активностью.
const MaxSessions = 20

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
	// save — синхронный колбэк персистентности (D-04); nil в
	// «только память»-режиме (тесты конструируют RateLimiter напрямую без
	// него). Вызывается уже со снимком (копией по значению), не с
	// указателями — тот же инвариант, что и snapshotSessionsLocked.
	save func(map[string]LoginAttempts)
	// dirty — есть ли рост счётчика, не записанный на диск с последнего
	// flush; для установки/снятия блокировки и полного сброса пишем
	// синхронно сразу (persistLocked), не дожидаясь тикера — блокировка
	// обязана пережить рестарт (D-04), рост счётчика без блокировки — нет
	// (RESEARCH считает потерю незаблокированного счётчика при аварийном
	// падении приемлемой).
	dirty bool
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
		tombstones:       make(map[string]tombstone),
		rateLimiter:      &RateLimiter{attempts: make(map[string]*LoginAttempts)},
		onPasswordSet:    opts.OnPasswordSet,
		maxLoginAttempts: opts.MaxLoginAttempts,
		lockoutDuration:  opts.LockoutDuration,
		idleTTL:          opts.IdleTTL,
		absoluteTTL:      opts.AbsoluteTTL,
		store:            newFileStore(opts.DataDir),
		now:              time.Now,
		stopCh:           make(chan struct{}),
		dataDir:          opts.DataDir,
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

	// Rate-limiter пишет на тот же store, что и сессии (D-04): блокировка
	// IP переживает рестарт процесса на том же data_dir.
	svc.rateLimiter.save = func(snapshot map[string]LoginAttempts) {
		if err := svc.store.saveRateLimit(snapshot); err != nil {
			log.Printf("[auth] failed to persist rate limiter state: %v", err)
		}
	}
	if loaded := svc.store.loadRateLimit(); loaded != nil {
		svc.rateLimiter.mu.Lock()
		for ip, la := range loaded {
			cp := la
			svc.rateLimiter.attempts[ip] = &cp
		}
		svc.rateLimiter.mu.Unlock()
	}

	// Пока пароль не задан, панель требует одноразовый код настройки (D-24):
	// подготовить его сразу при старте, а не при первом запросе к HandleSetup
	// — иначе setup.sh (134-10) не смог бы прочитать код сразу после запуска.
	if opts.PasswordHash == "" {
		svc.ensureSetupCodeReady()
		log.Printf("[auth] setup code ready: run 'xcp --setup-code' on the router")
	}

	svc.startCleanup()
	return svc
}

// ensureSetupCodeReady готовит одноразовый код настройки (на диске при
// DataDir!="", иначе в memSetupCode) — вызывается при старте без пароля и из
// ReloadPasswordHash, когда хеш сбрасывается в пустой (D-24).
func (a *AuthService) ensureSetupCodeReady() {
	if a.dataDir != "" {
		if _, err := EnsureSetupCode(a.dataDir); err != nil {
			log.Printf("[auth] failed to prepare setup code: %v", err)
		}
		return
	}
	code, err := generateSetupCode()
	if err != nil {
		log.Printf("[auth] failed to generate setup code: %v", err)
		return
	}
	a.mu.Lock()
	a.memSetupCode = code
	a.mu.Unlock()
}

// clearSetupCode удаляет одноразовый код настройки (файл и/или память) —
// вызывается после успешного HandleSetup и из ReloadPasswordHash, когда хеш
// становится непустым. Код не должен оставаться действительным после того,
// как пароль задан (T-134-27).
func (a *AuthService) clearSetupCode() {
	if err := RemoveSetupCode(a.dataDir); err != nil {
		log.Printf("[auth] failed to remove setup code file: %v", err)
	}
	a.mu.Lock()
	a.memSetupCode = ""
	a.mu.Unlock()
}

// currentSetupCode возвращает действующий одноразовый код настройки: при
// DataDir!="" читает его с диска, регенерируя при отсутствии файла (например,
// после переиздания через `xcp --setup-code`, 134-10, — HandleSetup всегда
// сверяется с диском, а не с застывшим значением в памяти); иначе отдаёт
// memSetupCode.
func (a *AuthService) currentSetupCode() string {
	if a.dataDir != "" {
		if code, err := ReadSetupCode(a.dataDir); err == nil {
			return code
		}
		code, err := EnsureSetupCode(a.dataDir)
		if err != nil {
			log.Printf("[auth] failed to ensure setup code: %v", err)
			return ""
		}
		return code
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.memSetupCode
}

// Stop останавливает фоновые горутины очистки и делает финальный Flush
// сессий на диск. Идемпотентен — повторный вызов безопасен (stopOnce).
func (a *AuthService) Stop() {
	a.stopOnce.Do(func() {
		if err := a.Flush(); err != nil {
			log.Printf("[auth] final flush on Stop failed: %v", err)
		}
		a.rateLimiter.flushIfDirty()
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

// ChangePassword меняет пароль администратора и перевыпускает текущую
// сессию (SESS-02, D-09): вместо того чтобы оставить сессию, стоящую за
// keepToken, как есть, она тоже завершается вместе со всеми остальными —
// клиент получает новый токен, новый CSRF и новую cookie тем же контекстом
// входа (IP/UA/RememberMe), что был у старой. Это защищает от session
// fixation через уже скомпрометированный токен: старый токен текущего
// браузера после смены пароля больше не принимается ни при каких условиях.
//
//   - попытки ввода текущего пароля ограничены тем же лимитом, что и вход
//     (ip), — украденная сессия не даёт подбирать пароль без ограничений;
//   - новый хеш сначала сохраняется на диск и только потом применяется:
//     при ошибке записи действующим остаётся старый пароль;
//   - все сессии удаляются с tombstone password_changed (включая старый
//     токен keepToken) одним снимком в памяти, затем создаётся новая — итог
//     один синхронный диск-write с новым password_fingerprint (через
//     CreateSessionWithMeta), а не два отдельных.
func (a *AuthService) ChangePassword(ip, keepToken, currentPassword, newPassword string) (*IssuedSession, error) {
	if err := a.rateLimiter.CheckLimit(ip, a.maxLoginAttempts, a.lockoutDuration); err != nil {
		return nil, ErrTooManyAttempts
	}
	a.pwMu.Lock()
	defer a.pwMu.Unlock()
	if err := a.VerifyPassword(currentPassword); err != nil {
		return nil, err
	}
	a.rateLimiter.ResetAttempts(ip)

	newHash, err := a.HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	if a.onPasswordSet != nil {
		if err := a.onPasswordSet(newHash); err != nil {
			return nil, err
		}
	}
	a.SetPasswordHash(newHash)

	meta, terminated := a.terminateAllForPasswordChange(keepToken)
	issued, err := a.CreateSessionWithMeta(meta)
	if err != nil {
		return nil, err
	}
	auditf("password changed", ip, fmt.Sprintf(" session=%s terminated=%d", shortSessionID(issued.ID), terminated))
	return issued, nil
}

// terminateAllForPasswordChange удаляет из памяти все сессии (включая
// keepToken) и ставит им tombstone password_changed — без немедленной записи
// на диск: единственный write делает последующий CreateSessionWithMeta.
// Возвращает контекст входа (IP/UA/RememberMe) сессии keepToken, если она
// была жива, — чтобы перевыпущенная сессия выглядела для клиента так же, как
// прежняя, кроме токена/CSRF, — и число завершённых сессий (для аудит-лога).
func (a *AuthService) terminateAllForPasswordChange(keepToken string) (SessionMeta, int) {
	keepHash := hashToken(keepToken)
	var meta SessionMeta

	a.mu.Lock()
	if s, ok := a.sessions[keepHash]; ok {
		meta = SessionMeta{IP: s.IP, UserAgent: s.UserAgent, RememberMe: s.RememberMe}
	}
	now := a.now()
	terminated := len(a.sessions)
	for hash := range a.sessions {
		delete(a.sessions, hash)
		a.tombstones[hash] = tombstone{reason: ReasonPasswordChanged, until: now.Add(tombstoneTTL)}
	}
	a.mu.Unlock()

	return meta, terminated
}

// ReloadPasswordHash применяет новый хеш пароля без рестарта процесса
// (D-22) — используется CLI-сбросом пароля (134-10), который меняет
// password_hash в config.json, пока панель уже запущена, и должен
// синхронизировать работающий процесс, а не полагаться на то, что
// администратор его перезапустит. Под pwMu (та же сериализация, что и
// ChangePassword): SetPasswordHash, затем apply(hash), если задан (даёт
// вызывающему коду точку для собственной синхронизации, например
// пересохранения своей копии конфига), затем все сессии получают tombstone
// password_changed и удаляются одним снимком, и наконец rate-limiter
// полностью сбрасывается (в памяти и на диске) — заблокированный IP сразу
// снова может пробовать войти с новым паролем. Возвращает число завершённых
// сессий.
func (a *AuthService) ReloadPasswordHash(hash string, apply func(string)) int {
	a.pwMu.Lock()
	defer a.pwMu.Unlock()

	a.SetPasswordHash(hash)
	if apply != nil {
		apply(hash)
	}
	if hash != "" {
		a.clearSetupCode()
	} else {
		a.ensureSetupCodeReady()
	}

	now := a.now()
	a.mu.Lock()
	count := len(a.sessions)
	for h := range a.sessions {
		delete(a.sessions, h)
		a.tombstones[h] = tombstone{reason: ReasonPasswordChanged, until: now.Add(tombstoneTTL)}
	}
	fingerprint, snapshot := a.snapshotSessionsLocked()
	a.mu.Unlock()
	a.persistSnapshot(fingerprint, snapshot)

	a.rateLimiter.Reset()
	auditf("password reloaded", "local", fmt.Sprintf(" terminated=%d", count))

	return count
}

func (a *AuthService) startCleanup() {
	go a.cleanupSessions()
	go a.cleanupRateLimiter()
	go a.flushLoop()
}

// flushIfDirty сохраняет снимок сессий, только если с последнего flush была
// активность (lastSeenDirty) — иначе no-op. Структурные изменения
// (создание/удаление сессии) пишутся синхронно сами по себе и не зависят от
// этого флага; он покрывает только LastSeen, обновляемый на каждый
// authenticated-запрос (T-134-05: без троттлинга это был бы один write на
// каждый запрос — неприемлемый износ флеша роутера).
func (a *AuthService) flushIfDirty() {
	a.mu.Lock()
	if !a.lastSeenDirty {
		a.mu.Unlock()
		return
	}
	a.lastSeenDirty = false
	fingerprint, snapshot := a.snapshotSessionsLocked()
	a.mu.Unlock()
	a.persistSnapshot(fingerprint, snapshot)
}

// flushLoop сбрасывает накопленную активность (last_seen) не чаще раза в
// lastSeenFlushInterval; финальный сброс делает Stop() через Flush()
// (безусловно, не только при lastSeenDirty).
func (a *AuthService) flushLoop() {
	ticker := time.NewTicker(lastSeenFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.flushIfDirty()
			a.rateLimiter.flushIfDirty()
		case <-a.stopCh:
			return
		}
	}
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
			for hash, t := range a.tombstones {
				if now.After(t.until) {
					delete(a.tombstones, hash)
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

	// D-03: не больше MaxSessions одновременно. При превышении лимита
	// удаляется сессия с самой давней последней активностью — единственный
	// линейный проход по ≤MaxSessions записям, отдельный индекс не нужен.
	if len(a.sessions) >= MaxSessions {
		var oldestHash string
		var oldestSeen time.Time
		first := true
		for hash, s := range a.sessions {
			if first || s.LastSeen.Before(oldestSeen) {
				oldestHash = hash
				oldestSeen = s.LastSeen
				first = false
			}
		}
		if oldestHash != "" {
			delete(a.sessions, oldestHash)
		}
	}

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
// продлевает LastSeen только в памяти и помечает lastSeenDirty — на диск это
// пишется не на каждый запрос, а throttled через flushLoop (T-134-05).
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
	a.lastSeenDirty = true
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

// ErrSessionNotFound — сессия с таким непрозрачным id не найдена (уже
// завершена или никогда не существовала).
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionIsCurrent — попытка завершить текущую (свою же) сессию через
// TerminateSession; для этого есть отдельный поток — Logout.
var ErrSessionIsCurrent = errors.New("cannot terminate the current session")

// SessionInfo — представление сессии для GET /api/auth/sessions. Ни токен,
// ни его хеш сюда никогда не попадают (T-134-09) — только непрозрачный ID,
// разобранные из User-Agent браузер/ОС, IP и время.
type SessionInfo struct {
	ID        string    `json:"id"`
	Browser   string    `json:"browser"`
	OS        string    `json:"os"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	Current   bool      `json:"current"`
}

// ListSessions возвращает список активных сессий: текущая (currentToken)
// первой, остальные — по убыванию LastSeen.
func (a *AuthService) ListSessions(currentToken string) []SessionInfo {
	currentHash := hashToken(currentToken)

	a.mu.RLock()
	list := make([]SessionInfo, 0, len(a.sessions))
	for hash, s := range a.sessions {
		browser, os := ParseUserAgent(s.UserAgent)
		list = append(list, SessionInfo{
			ID:        s.ID,
			Browser:   browser,
			OS:        os,
			IP:        s.IP,
			CreatedAt: s.CreatedAt,
			LastSeen:  s.LastSeen,
			Current:   hash == currentHash,
		})
	}
	a.mu.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		if list[i].Current != list[j].Current {
			return list[i].Current
		}
		return list[i].LastSeen.After(list[j].LastSeen)
	})
	return list
}

// TerminateSession завершает чужую сессию по непрозрачному id — в памяти и
// синхронно на диске. Завершение текущей сессии (currentToken) запрещено
// (ErrSessionIsCurrent) — для выхода из своего же браузера есть Logout.
// Завершённое устройство получает 401 с reason=terminated_elsewhere при
// следующем запросе (D-19).
func (a *AuthService) TerminateSession(id, currentToken string) error {
	currentHash := hashToken(currentToken)

	a.mu.Lock()
	var targetHash string
	for hash, s := range a.sessions {
		if s.ID == id {
			targetHash = hash
			break
		}
	}
	if targetHash == "" {
		a.mu.Unlock()
		return ErrSessionNotFound
	}
	if targetHash == currentHash {
		a.mu.Unlock()
		return ErrSessionIsCurrent
	}
	// ip/byID для аудита: ip — устройства, чья сессия завершается (её
	// собственный, а не запросивший завершение — TerminateSession не получает
	// ip актёра как параметр), byID — id текущей сессии запросившего.
	targetIP := a.sessions[targetHash].IP
	var byID string
	if cs, ok := a.sessions[currentHash]; ok {
		byID = cs.ID
	}
	delete(a.sessions, targetHash)
	a.tombstones[targetHash] = tombstone{reason: ReasonTerminatedElsewhere, until: a.now().Add(tombstoneTTL)}
	fingerprint, snapshot := a.snapshotSessionsLocked()
	a.mu.Unlock()

	a.persistSnapshot(fingerprint, snapshot)
	auditf("session terminated", targetIP, fmt.Sprintf(" session=%s by=%s", shortSessionID(id), shortSessionID(byID)))
	return nil
}

// TerminateOtherSessions завершает все сессии, кроме currentToken, — в
// памяти и синхронно на диске. Возвращает число завершённых сессий (0, если
// кроме текущей ничего не было — идемпотентно при повторном вызове).
func (a *AuthService) TerminateOtherSessions(currentToken string) int {
	currentHash := hashToken(currentToken)

	a.mu.Lock()
	var actorIP string
	if cs, ok := a.sessions[currentHash]; ok {
		actorIP = cs.IP
	}
	count := 0
	for hash := range a.sessions {
		if hash == currentHash {
			continue
		}
		delete(a.sessions, hash)
		a.tombstones[hash] = tombstone{reason: ReasonTerminatedElsewhere, until: a.now().Add(tombstoneTTL)}
		count++
	}
	var fingerprint string
	var snapshot []*Session
	if count > 0 {
		fingerprint, snapshot = a.snapshotSessionsLocked()
	}
	a.mu.Unlock()

	if count > 0 {
		a.persistSnapshot(fingerprint, snapshot)
	}
	auditf("sessions terminated others", actorIP, fmt.Sprintf(" count=%d", count))
	return count
}

// sessionReason возвращает причину недействительности сессии по её сырому
// токену — из tombstone, если он ещё не истёк, иначе дефолтный
// ReasonSessionExpired (D-19: «по умолчанию и по TTL»).
func (a *AuthService) sessionReason(token string) string {
	hash := hashToken(token)
	a.mu.RLock()
	t, ok := a.tombstones[hash]
	now := a.now()
	a.mu.RUnlock()
	if ok && now.Before(t.until) {
		return t.reason
	}
	return ReasonSessionExpired
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

// WriteSessionCookie выставляет клиенту cookie сессии, выпущенной
// ChangePassword — с текущим absoluteTTL и remember_me перевыпущенной
// сессии (унаследованным от прежней, см. terminateAllForPasswordChange).
func (a *AuthService) WriteSessionCookie(w http.ResponseWriter, s *IssuedSession) {
	_, absoluteTTL := a.TTL()
	setSessionCookie(w, s.Token, s.RememberMe, absoluteTTL)
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
		rl.dirty = true
		return nil
	}

	if time.Now().Before(attempts.LockedUntil) {
		return errors.New("too many login attempts, account locked")
	}

	if time.Since(attempts.LastAttempt) > 15*time.Minute {
		attempts.Count = 1
		attempts.LastAttempt = time.Now()
		rl.dirty = true
		return nil
	}

	attempts.Count++
	attempts.LastAttempt = time.Now()

	if attempts.Count >= maxAttempts {
		attempts.LockedUntil = time.Now().Add(lockoutDuration)
		// Блокировка обязана пережить рестарт (D-04) — пишем синхронно, не
		// дожидаясь троттлинга.
		rl.persistLocked()
		return errors.New("too many login attempts, account locked")
	}

	rl.dirty = true
	return nil
}

func (rl *RateLimiter) ResetAttempts(ip string) {
	rl.mu.Lock()
	delete(rl.attempts, ip)
	rl.persistLocked()
	rl.mu.Unlock()
}

// Reset снимает все блокировки и счётчики попыток — в памяти и синхронно на
// диске (используется ReloadPasswordHash, D-22: горячая смена пароля не
// должна оставлять старые блокировки действующими).
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	rl.attempts = make(map[string]*LoginAttempts)
	rl.persistLocked()
	rl.mu.Unlock()
}

// persistLocked пишет текущий снимок attempts на диск через save (если он
// задан) и снимает dirty. Вызывающий код уже держит rl.mu.
func (rl *RateLimiter) persistLocked() {
	rl.dirty = false
	if rl.save == nil {
		return
	}
	snapshot := make(map[string]LoginAttempts, len(rl.attempts))
	for k, v := range rl.attempts {
		snapshot[k] = *v
	}
	rl.save(snapshot)
}

// flushIfDirty сохраняет накопленный (не заблокированный) рост счётчиков,
// только если с последнего flush была активность — throttled-аналог
// AuthService.flushIfDirty, вызывается тем же flushLoop/Stop.
func (rl *RateLimiter) flushIfDirty() {
	rl.mu.Lock()
	if rl.dirty {
		rl.persistLocked()
	}
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

// auditf пишет строку аудита «[auth] event ip=<ip><extra>» в xcp.log (D-11).
// ip и extra экранируются через utils.SanitizeLogInput — перевод строки или
// возврат каретки из данных запроса (RemoteAddr, User-Agent и т.п.) не может
// разбить лог на поддельные строки (T-134-11). Сюда никогда не передаются
// пароли, токены, CSRF-токены или их хеши — только непрозрачные session ID
// (не секрет: тот же id уже отдаётся клиенту в GET /api/auth/sessions) и
// агрегированные числа.
func auditf(event, ip, extra string) {
	log.Printf("[auth] %s ip=%s%s", event, utils.SanitizeLogInput(ip), utils.SanitizeLogInput(extra))
}

// shortSessionID возвращает первые 8 символов непрозрачного id сессии —
// достаточно, чтобы связать несколько строк лога об одной и той же сессии,
// не раздувая при этом каждую запись полным 32-символьным id.
func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// jsonErrorFields — как jsonError, но со свободным набором дополнительных
// полей в JSON-теле (например reason у 401 ответов RequireAuth, D-19).
func jsonErrorFields(w http.ResponseWriter, code int, fields map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(fields)
}

// jsonErrorCode — как jsonError, но добавляет машиночитаемый code (setup_code_invalid,
// коды политики пароля из PolicyErrorCode) — клиент переводит его в текст
// сам, не парсит error.
func jsonErrorCode(w http.ResponseWriter, status int, code, msg string) {
	jsonErrorFields(w, status, map[string]interface{}{
		"success": false,
		"error":   msg,
		"code":    code,
	})
}

// Middleware
func (a *AuthService) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			jsonErrorFields(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   "Unauthorized",
				"reason":  ReasonSessionExpired,
			})
			return
		}

		session, err := a.ValidateSession(cookie.Value)
		if err != nil {
			jsonErrorFields(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   "Unauthorized",
				"reason":  a.sessionReason(cookie.Value),
			})
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
		until := a.now().Add(time.Duration(seconds) * time.Second)
		auditf("lockout", ip, fmt.Sprintf(" until=%s", until.Format(time.RFC3339)))
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
		auditf("login failed", ip, "")
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
	auditf("login ok", ip, fmt.Sprintf(" session=%s", shortSessionID(issued.ID)))

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

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		var sessionID string
		a.mu.RLock()
		if s, ok := a.sessions[hashToken(cookie.Value)]; ok {
			sessionID = s.ID
		}
		a.mu.RUnlock()
		a.DeleteSession(cookie.Value)
		auditf("logout", ip, fmt.Sprintf(" session=%s", shortSessionID(sessionID)))
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
		Password  string `json:"password"`
		SetupCode string `json:"setup_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	// Код первичной настройки (D-24): без него или с неверным — 400
	// setup_code_invalid, попытка уже учтена CheckLimit выше (T-134-26).
	if err := ValidateSetupCode(a.currentSetupCode(), req.SetupCode); err != nil {
		auditf("setup code rejected", ip, "")
		jsonErrorCode(w, http.StatusBadRequest, "setup_code_invalid", "Invalid setup code")
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
	a.clearSetupCode()
	auditf("setup completed", ip, "")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
