package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// sessionsFileName — имя файла сессий внутри DataDir панели.
// storeSchemaVersion — версия JSON-схемы sessions.json; файл с версией выше
// той, что понимает бинарник, отбрасывается целиком (см. loadSessions),
// без паники и без блокировки старта.
// lastSeenFlushInterval — период фонового сброса накопленной активности
// (last_seen) на диск (Task 3); используется тем же тикер-паттерном, что
// cleanupSessions/cleanupRateLimiter.
const (
	sessionsFileName      = "sessions.json"
	rateLimitFileName     = "ratelimit.json"
	storeSchemaVersion    = 1
	lastSeenFlushInterval = 5 * time.Minute
)

// sessionFile — корневая структура data_dir/sessions.json.
type sessionFile struct {
	SchemaVersion       int                `json:"schema_version"`
	PasswordFingerprint string             `json:"password_fingerprint"`
	Sessions            []persistedSession `json:"sessions"`
}

// persistedSession — сериализуемое представление Session. Сырые Token/CSRF
// здесь никогда не появляются (D-01/D-02) — только их SHA-256.
type persistedSession struct {
	ID         string    `json:"id"`
	TokenHash  string    `json:"token_hash"`
	CSRFHash   string    `json:"csrf_hash"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeen   time.Time `json:"last_seen"`
	RememberMe bool      `json:"remember_me"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
}

// rateLimitFile — корневая структура data_dir/ratelimit.json (D-04):
// счётчики неудачных попыток входа и активные блокировки, чтобы
// заблокированный IP оставался заблокированным после рестарта процесса.
type rateLimitFile struct {
	SchemaVersion int                `json:"schema_version"`
	Attempts      []persistedAttempt `json:"attempts"`
}

// persistedAttempt — сериализуемое представление LoginAttempts для одного IP.
type persistedAttempt struct {
	IP          string    `json:"ip"`
	Count       int       `json:"count"`
	LastAttempt time.Time `json:"last_attempt"`
	LockedUntil time.Time `json:"locked_until"`
}

// fileStore персистирует сессии в {DataDir}/sessions.json.
//
// Путь собирается из фиксированного имени файла (sessionsFileName) внутри
// DataDir, который приходит из config.json панели, а не из пользовательского
// ввода запроса — поэтому utils.PathValidator здесь не требуется, тот же
// паттерн, что уже применяют kernel.go/snapshots.go для собственных путей
// панели внутри DataDir.
type fileStore struct {
	dir    string
	mu     sync.Mutex
	closed bool
	writes atomic.Int64
}

// newFileStore возвращает nil при пустом dir — это режим «только память»,
// используемый юнит-тестами AuthService, которым персистентность не нужна.
// Все методы fileStore безопасны для вызова на nil-получателе.
func newFileStore(dir string) *fileStore {
	if dir == "" {
		return nil
	}
	return &fileStore{dir: dir}
}

func (s *fileStore) path() string {
	return filepath.Join(s.dir, sessionsFileName)
}

func (s *fileStore) rateLimitPath() string {
	return filepath.Join(s.dir, rateLimitFileName)
}

// loadRateLimit читает ratelimit.json и возвращает восстановленные счётчики
// попыток входа. Отсутствующий файл, битый JSON или неподдерживаемый
// schema_version — пустой набор без паники и без блокировки старта (тот же
// инвариант, что и loadSessions).
func (s *fileStore) loadRateLimit() map[string]LoginAttempts {
	if s == nil {
		return nil
	}

	data, err := os.ReadFile(s.rateLimitPath())
	if err != nil {
		return nil
	}

	var rf rateLimitFile
	if err := json.Unmarshal(data, &rf); err != nil {
		log.Printf("[auth] ratelimit.json ignored: invalid JSON: %v", err)
		return nil
	}
	if rf.SchemaVersion < 1 || rf.SchemaVersion > storeSchemaVersion {
		log.Printf("[auth] ratelimit.json ignored: unsupported schema_version %d", rf.SchemaVersion)
		return nil
	}

	out := make(map[string]LoginAttempts, len(rf.Attempts))
	for _, p := range rf.Attempts {
		out[p.IP] = LoginAttempts{Count: p.Count, LastAttempt: p.LastAttempt, LockedUntil: p.LockedUntil}
	}
	return out
}

// saveRateLimit пишет снимок счётчиков попыток входа атомарно с правами
// 0600. No-op после close() — не пишет на диск после Stop(). nil/пустой
// attempts даёт пустой файл (используется ResetPersistedAuthState и
// ReloadPasswordHash).
func (s *fileStore) saveRateLimit(attempts map[string]LoginAttempts) error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	rf := rateLimitFile{
		SchemaVersion: storeSchemaVersion,
		Attempts:      make([]persistedAttempt, 0, len(attempts)),
	}
	for ip, la := range attempts {
		rf.Attempts = append(rf.Attempts, persistedAttempt{
			IP:          ip,
			Count:       la.Count,
			LastAttempt: la.LastAttempt,
			LockedUntil: la.LockedUntil,
		})
	}

	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	if err := utils.AtomicWriteFile(s.rateLimitPath(), data, 0600); err != nil {
		return err
	}
	s.writes.Add(1)
	return nil
}

// loadSessions читает sessions.json и возвращает восстановленные сессии.
// Отсутствующий файл, битый JSON, неподдерживаемый schema_version или
// несовпадение fingerprint пароля (сменили пароль, пока панель стояла) —
// все эти случаи дают пустой набор без паники и без блокировки старта.
func (s *fileStore) loadSessions(fingerprint string) []*Session {
	if s == nil {
		return nil
	}

	data, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}

	var sf sessionFile
	if err := json.Unmarshal(data, &sf); err != nil {
		log.Printf("[auth] sessions.json ignored: invalid JSON: %v", err)
		return nil
	}
	if sf.SchemaVersion < 1 || sf.SchemaVersion > storeSchemaVersion {
		log.Printf("[auth] sessions.json ignored: unsupported schema_version %d", sf.SchemaVersion)
		return nil
	}
	if sf.PasswordFingerprint != fingerprint {
		log.Printf("[auth] sessions discarded: password changed while panel was stopped")
		return nil
	}

	sessions := make([]*Session, 0, len(sf.Sessions))
	for _, p := range sf.Sessions {
		sessions = append(sessions, &Session{
			ID:         p.ID,
			TokenHash:  p.TokenHash,
			CSRFHash:   p.CSRFHash,
			CreatedAt:  p.CreatedAt,
			LastSeen:   p.LastSeen,
			RememberMe: p.RememberMe,
			UserAgent:  p.UserAgent,
			IP:         p.IP,
		})
	}
	return sessions
}

// saveSessions пишет снимок сессий атомарно (tmp+rename) с правами 0600.
// No-op после close() — не пишет на диск после Stop().
func (s *fileStore) saveSessions(fingerprint string, sessions []*Session) error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	sf := sessionFile{
		SchemaVersion:       storeSchemaVersion,
		PasswordFingerprint: fingerprint,
		Sessions:            make([]persistedSession, 0, len(sessions)),
	}
	for _, sess := range sessions {
		sf.Sessions = append(sf.Sessions, persistedSession{
			ID:         sess.ID,
			TokenHash:  sess.TokenHash,
			CSRFHash:   sess.CSRFHash,
			CreatedAt:  sess.CreatedAt,
			LastSeen:   sess.LastSeen,
			RememberMe: sess.RememberMe,
			UserAgent:  sess.UserAgent,
			IP:         sess.IP,
		})
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	if err := utils.AtomicWriteFile(s.path(), data, 0600); err != nil {
		return err
	}
	s.writes.Add(1)
	return nil
}

// close помечает store закрытым: последующие saveSessions становятся no-op.
func (s *fileStore) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// hashToken — hex(sha256(s)); используется и для session-токена, и для
// CSRF-токена (единый инвариант D-01/D-02: на диске и в structs — только хеш).
func hashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// passwordFingerprint — короткий (16 hex-символов) отпечаток bcrypt-хеша
// пароля. Пустой хеш (пароль ещё не задан) даёт пустой fingerprint.
// Используется для отбрасывания sessions.json, записанного при другом
// пароле (T-134-02).
func passwordFingerprint(hash string) string {
	if hash == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:])[:16]
}

// ResetPersistedAuthState перезаписывает sessions.json и ratelimit.json в
// dataDir пустыми файлами (schema_version валиден, содержимое пусто) — для
// CLI-сброса пароля (134-10), запускаемого не тем же процессом, что панель:
// побочные сессии и блокировки rate-limiter'а не должны пережить сброс
// пароля мимо запущенной панели. Пустой dataDir — no-op (используется, когда
// DataDir не сконфигурирован).
func ResetPersistedAuthState(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	s := newFileStore(dataDir)
	if err := s.saveSessions("", nil); err != nil {
		return err
	}
	return s.saveRateLimit(nil)
}
