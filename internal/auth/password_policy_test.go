package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestPasswordPolicy — табличный тест ValidateNewPassword по поведенческой
// спецификации плана (D-17): границы длины в байтах (ASCII и кириллица),
// повторяющийся символ, чёрный список без учёта регистра, совпадение с
// текущим паролем.
func TestPasswordPolicy(t *testing.T) {
	hashOf := func(password string) string {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		return string(hash)
	}

	cases := []struct {
		name        string
		newPassword string
		currentHash string
		wantErr     error
	}{
		{"too short", "1234567", "", ErrPasswordTooShort},
		{"exactly 8 bytes ASCII ok", "abcdefgh", "", nil},
		{"exactly 72 bytes ASCII ok", strings.Repeat("a1", 36), "", nil},
		{"73 bytes ASCII too long", strings.Repeat("a1", 36) + "x", "", ErrPasswordTooLong},
		{"36 distinct cyrillic chars (72 bytes) ok", cyrillicRun(36), "", nil},
		{"37 distinct cyrillic chars (74 bytes) too long", cyrillicRun(37), "", ErrPasswordTooLong},
		{"repeated ascii char", "aaaaaaaa", "", ErrPasswordRepeatedChar},
		{"repeated cyrillic char", "ЯЯЯЯЯЯЯЯ", "", ErrPasswordRepeatedChar},
		{"blacklisted lowercase", "password123", "", ErrPasswordBlacklisted},
		{"blacklisted mixed case", "Password", "", ErrPasswordBlacklisted},
		{"blacklisted keenetic uppercase", "KEENETIC", "", ErrPasswordBlacklisted},
		{"blacklisted cyrillic", "йцукенгш", "", ErrPasswordBlacklisted},
		{"same as current", "old-pass-123", hashOf("old-pass-123"), ErrPasswordSameAsCurrent},
		{"same as current skipped when hash empty", "old-pass-123", "", nil},
		{"different from current", "brandnewpass1", hashOf("old-pass-123"), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewPassword(tc.newPassword, tc.currentHash)
			if !errors.Is(err, tc.wantErr) {
				if tc.wantErr == nil {
					t.Errorf("ValidateNewPassword(%q) = %v, want nil", tc.newPassword, err)
				} else {
					t.Errorf("ValidateNewPassword(%q) = %v, want %v", tc.newPassword, err, tc.wantErr)
				}
			}
		})
	}
}

// cyrillicRun возвращает n различных кириллических символов (2 байта каждый
// в UTF-8) без повторов, чтобы не попасть под ErrPasswordRepeatedChar.
func cyrillicRun(n int) string {
	// А-Я — 32 буквы (ёЁ отдельно за пределами диапазона), достаточно для n<=37
	// с повторным проходом по алфавиту для n>32, без опасности случайного
	// совпадения с чёрным списком.
	const alphabet = "АБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ"
	runes := []rune(alphabet)
	out := make([]rune, n)
	for i := 0; i < n; i++ {
		out[i] = runes[i%len(runes)]
	}
	return string(out)
}

// TestPolicyErrorCode проверяет перевод ошибок политики в машиночитаемые
// коды и пустую строку для любой другой ошибки (в т.ч. nil).
func TestPolicyErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrPasswordTooShort, "password_too_short"},
		{ErrPasswordTooLong, "password_too_long"},
		{ErrPasswordRepeatedChar, "password_repeated_char"},
		{ErrPasswordBlacklisted, "password_blacklisted"},
		{ErrPasswordSameAsCurrent, "password_same_as_current"},
		{errors.New("some other error"), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := PolicyErrorCode(tc.err); got != tc.want {
			t.Errorf("PolicyErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// TestPasswordBlacklist_MatchesFrontend проверяет T-134-30: набор строк
// internal/auth/password_blacklist.txt совпадает с
// frontend/src/lib/passwordBlacklist.json (без учёта регистра/порядка).
func TestPasswordBlacklist_MatchesFrontend(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "passwordBlacklist.json"))
	if err != nil {
		t.Fatalf("read frontend blacklist: %v", err)
	}
	var frontendList []string
	if err := json.Unmarshal(data, &frontendList); err != nil {
		t.Fatalf("parse frontend blacklist: %v", err)
	}

	frontendSet := make(map[string]struct{}, len(frontendList))
	for _, w := range frontendList {
		frontendSet[strings.ToLower(w)] = struct{}{}
	}

	if len(frontendSet) < 47 {
		t.Fatalf("expected at least 47 entries in the frontend blacklist, got %d", len(frontendSet))
	}
	if len(passwordBlacklist) != len(frontendSet) {
		t.Errorf("blacklist size mismatch: backend=%d frontend=%d", len(passwordBlacklist), len(frontendSet))
	}
	for w := range frontendSet {
		if _, ok := passwordBlacklist[w]; !ok {
			t.Errorf("word %q present in frontend blacklist but missing from backend", w)
		}
	}
	for w := range passwordBlacklist {
		if _, ok := frontendSet[w]; !ok {
			t.Errorf("word %q present in backend blacklist but missing from frontend", w)
		}
	}
}

// TestHandleSetup_PolicyErrors проверяет, что HandleSetup применяет
// ValidateNewPassword и отдаёт понятный 400 (не 500) на пароле длиннее 72
// байт, с соответствующим кодом в ответе.
func TestHandleSetup_PolicyErrors(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: 0})
	defer svc.Stop()
	setupCode := svc.currentSetupCode()

	post := func(password string) (int, string) {
		body, _ := json.Marshal(map[string]string{"password": password, "setup_code": setupCode})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		rr := httptest.NewRecorder()
		svc.HandleSetup(rr, req)
		var resp struct {
			Code string `json:"code"`
		}
		json.NewDecoder(rr.Body).Decode(&resp)
		return rr.Code, resp.Code
	}

	longPassword := strings.Repeat("a", 73)
	status, code := post(longPassword)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for a 73-byte password, got %d", status)
	}
	if code != "password_too_long" {
		t.Errorf("expected code=password_too_long, got %q", code)
	}

	status, code = post("password123")
	if status != http.StatusBadRequest || code != "password_blacklisted" {
		t.Errorf("expected 400 password_blacklisted, got %d %q", status, code)
	}
}

// TestLogin_DoesNotApplyPolicy проверяет, что действующий пароль,
// зафиксированный до введения политики (например "qwerty12" — 8 байт, не в
// чёрном списке, но было бы отклонено политикой при смене), продолжает
// работать при входе — HandleLogin не перепроверяет политику.
func TestLogin_DoesNotApplyPolicy(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: 0})
	defer svc.Stop()

	hash, err := bcrypt.GenerateFromPassword([]byte("qwerty12"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetPasswordHash(string(hash))

	body, _ := json.Marshal(map[string]string{"password": "qwerty12"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	svc.HandleLogin(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected a pre-policy password to still log in, got %d: %s", rr.Code, rr.Body.String())
	}
}
