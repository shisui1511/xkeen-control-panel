package auth

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
)

// TestSetupCode_GeneratedOnStartWithoutPassword проверяет D-24: при старте
// без пароля data_dir/setup_code существует, имеет права 0600 и содержит
// ровно 8 символов алфавита setupCodeAlphabet.
func TestSetupCode_GeneratedOnStartWithoutPassword(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})
	defer svc.Stop()

	path := filepath.Join(dir, setupCodeFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat setup_code: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected setup_code permissions 0600, got %04o", perm)
	}

	code, err := ReadSetupCode(dir)
	if err != nil {
		t.Fatalf("ReadSetupCode: %v", err)
	}
	if !isValidSetupCode(code) {
		t.Errorf("expected an 8-char code from setupCodeAlphabet, got %q", code)
	}
}

// TestSetupCode_ReusedAcrossRestart проверяет, что уже существующий валидный
// код переживает рестарт AuthService на том же data_dir (D-24).
func TestSetupCode_ReusedAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	svc1 := NewAuthService(Options{DataDir: dir})
	code1 := svc1.currentSetupCode()
	svc1.Stop()

	svc2 := NewAuthService(Options{DataDir: dir})
	defer svc2.Stop()
	code2 := svc2.currentSetupCode()

	if code1 == "" || code2 == "" {
		t.Fatal("expected non-empty setup codes")
	}
	if code1 != code2 {
		t.Errorf("expected the setup code to survive a restart on the same data_dir, got %q then %q", code1, code2)
	}
}

// TestSetupCode_NotGeneratedWhenPasswordSet проверяет, что при непустом
// PasswordHash файл кода настройки не создаётся.
func TestSetupCode_NotGeneratedWhenPasswordSet(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir, PasswordHash: "some-bcrypt-hash"})
	defer svc.Stop()

	if _, err := os.Stat(filepath.Join(dir, setupCodeFileName)); !os.IsNotExist(err) {
		t.Errorf("expected no setup_code file when a password is already configured, stat err=%v", err)
	}
}

// TestHandleSetup_RequiresCode проверяет T-134-26: без кода и с неверным
// кодом HandleSetup отдаёт 400 setup_code_invalid; верный код — в нижнем
// регистре и с дефисами-разделителями — принимается, файл кода после этого
// удаляется.
func TestHandleSetup_RequiresCode(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir, MaxLoginAttempts: 100, LockoutDuration: time.Minute})
	defer svc.Stop()
	code := svc.currentSetupCode()

	post := func(payload map[string]string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		rr := httptest.NewRecorder()
		svc.HandleSetup(rr, req)
		return rr
	}

	// Без кода.
	rrMissing := post(map[string]string{"password": "validpassword123"})
	if rrMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without a setup code, got %d", rrMissing.Code)
	}
	var missingResp struct {
		Code string `json:"code"`
	}
	json.NewDecoder(rrMissing.Body).Decode(&missingResp)
	if missingResp.Code != "setup_code_invalid" {
		t.Errorf("expected code=setup_code_invalid, got %q", missingResp.Code)
	}

	// С неверным кодом.
	rrWrong := post(map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
	if rrWrong.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with a wrong setup code, got %d", rrWrong.Code)
	}

	// Верный код, введённый в нижнем регистре и с дефисами-разделителями.
	lowered := strings.ToLower(code)
	spaced := lowered[:2] + "-" + lowered[2:4] + "-" + lowered[4:6] + "-" + lowered[6:]
	rrGood := post(map[string]string{"password": "validpassword123", "setup_code": spaced})
	if rrGood.Code != http.StatusOK {
		t.Fatalf("expected 200 with the correct (lowercase, hyphenated) setup code, got %d: %s", rrGood.Code, rrGood.Body.String())
	}

	if _, err := os.Stat(filepath.Join(dir, setupCodeFileName)); !os.IsNotExist(err) {
		t.Errorf("expected setup_code file to be removed after a successful setup, stat err=%v", err)
	}
}

// TestHandleSetup_CodeAttemptsRateLimited проверяет, что попытки ввода кода
// ограничены тем же rate-limit, что и вход: после исчерпания лимита — 429 с
// retry_after (D-24).
func TestHandleSetup_CodeAttemptsRateLimited(t *testing.T) {
	svc := NewAuthService(Options{MaxLoginAttempts: 5, LockoutDuration: time.Minute})
	defer svc.Stop()

	var last *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.9:12345"
		rr := httptest.NewRecorder()
		svc.HandleSetup(rr, req)
		last = rr
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhausting setup-code attempts, got %d", last.Code)
	}
	var resp struct {
		RetryAfter int `json:"retry_after"`
	}
	if err := json.NewDecoder(last.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RetryAfter <= 0 {
		t.Errorf("expected retry_after > 0, got %d", resp.RetryAfter)
	}
}

// TestSetupCode_NeverLogged проверяет T-134-27: код не появляется ни в одной
// строке лога — ни при генерации на старте, ни при отклонённой попытке.
func TestSetupCode_NeverLogged(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	svc := NewAuthService(Options{DataDir: dir, MaxLoginAttempts: 100, LockoutDuration: time.Minute})
	code := svc.currentSetupCode()

	body, _ := json.Marshal(map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	svc.HandleSetup(rr, req)
	svc.Stop()

	if code == "" {
		t.Fatal("expected a non-empty setup code to check against the log")
	}
	if strings.Contains(buf.String(), code) {
		t.Errorf("setup code must never appear in the log, got:\n%s", buf.String())
	}
}

// TestReloadPasswordHash_ManagesSetupCode проверяет, что ReloadPasswordHash
// удаляет код настройки при установке непустого хеша и создаёт его заново
// при сбросе хеша в пустой (D-22, D-24).
func TestReloadPasswordHash_ManagesSetupCode(t *testing.T) {
	dir := t.TempDir()
	svc := NewAuthService(Options{DataDir: dir})
	defer svc.Stop()

	if _, err := os.Stat(filepath.Join(dir, setupCodeFileName)); err != nil {
		t.Fatalf("expected setup_code to exist before any password is set: %v", err)
	}

	hash, err := svc.HashPassword("somepassword123")
	if err != nil {
		t.Fatal(err)
	}
	svc.ReloadPasswordHash(hash, nil)

	if _, err := os.Stat(filepath.Join(dir, setupCodeFileName)); !os.IsNotExist(err) {
		t.Errorf("expected setup_code to be removed once a password hash is applied, stat err=%v", err)
	}

	svc.ReloadPasswordHash("", nil)

	if _, err := os.Stat(filepath.Join(dir, setupCodeFileName)); err != nil {
		t.Errorf("expected setup_code to be recreated when the hash is reset to empty: %v", err)
	}
}

// TestNormalizeSetupCode_and_Matches проверяет нормализацию (регистр,
// пробелы, дефисы) и сравнение за постоянное время, включая пустой expected.
func TestNormalizeSetupCode_and_Matches(t *testing.T) {
	if got := NormalizeSetupCode(" ab-CD-ef-GH "); got != "ABCDEFGH" {
		t.Errorf("NormalizeSetupCode(%q) = %q, want ABCDEFGH", " ab-CD-ef-GH ", got)
	}
	if setupCodeMatches("", "ABCDEFGH") {
		t.Error("empty expected must never match any input")
	}
	if !setupCodeMatches("ABCDEFGH", "ab-cd-ef-gh") {
		t.Error("expected case/hyphen-insensitive match")
	}
	if setupCodeMatches("ABCDEFGH", "ABCDEFGI") {
		t.Error("expected mismatch to fail")
	}
	if err := ValidateSetupCode("ABCDEFGH", "wrong"); err != ErrSetupCodeInvalid {
		t.Errorf("expected ErrSetupCodeInvalid, got %v", err)
	}
}

// TestHandleSetup_LocalizedErrors проверяет 134-REVIEW WR-01: HandleSetup
// отдаёт текст ошибки ("error") на языке запроса (ru/en через контекст
// i18n.Middleware), тем же способом, что и ChangePassword — а не
// нелокализованный английский текст. Машинный код ("code") при этом не
// меняется в зависимости от языка.
func TestHandleSetup_LocalizedErrors(t *testing.T) {
	newRequest := func(lang string, payload map[string]string) *http.Request {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Accept-Language", lang)
		// Пропускаем через реальный i18n.Middleware — тот же путь, что и в
		// проде, а не подделанный контекст с приватным ключом пакета i18n.
		var captured *http.Request
		i18n.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = r
		})).ServeHTTP(httptest.NewRecorder(), req)
		return captured
	}

	post := func(svc *AuthService, lang string, payload map[string]string) (int, struct {
		Error      string `json:"error"`
		Code       string `json:"code"`
		RetryAfter int    `json:"retry_after"`
	}) {
		req := newRequest(lang, payload)
		rr := httptest.NewRecorder()
		svc.HandleSetup(rr, req)
		var resp struct {
			Error      string `json:"error"`
			Code       string `json:"code"`
			RetryAfter int    `json:"retry_after"`
		}
		json.NewDecoder(rr.Body).Decode(&resp)
		return rr.Code, resp
	}

	t.Run("wrong setup code", func(t *testing.T) {
		svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: time.Minute})
		defer svc.Stop()

		statusRu, respRu := post(svc, "ru", map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
		if statusRu != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusRu)
		}
		wantRu := i18n.T("ru", "auth.setup_code_invalid")
		if respRu.Error != wantRu {
			t.Errorf("ru: expected error %q, got %q", wantRu, respRu.Error)
		}
		if respRu.Code != "setup_code_invalid" {
			t.Errorf("ru: expected code=setup_code_invalid, got %q", respRu.Code)
		}

		statusEn, respEn := post(svc, "en", map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
		if statusEn != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusEn)
		}
		wantEn := i18n.T("en", "auth.setup_code_invalid")
		if respEn.Error != wantEn {
			t.Errorf("en: expected error %q, got %q", wantEn, respEn.Error)
		}
		if respEn.Code != "setup_code_invalid" {
			t.Errorf("en: expected code=setup_code_invalid, got %q", respEn.Code)
		}

		if respRu.Error == respEn.Error {
			t.Fatalf("expected ru and en error text to differ, both were %q", respRu.Error)
		}
		if wantRu != "Неверный код настройки" || wantEn != "Invalid setup code" {
			t.Fatalf("locale fixtures drifted: ru=%q en=%q", wantRu, wantEn)
		}
	})

	t.Run("password policy violation", func(t *testing.T) {
		svc := NewAuthService(Options{MaxLoginAttempts: 100, LockoutDuration: time.Minute})
		defer svc.Stop()
		code := svc.currentSetupCode()

		statusRu, respRu := post(svc, "ru", map[string]string{"password": "aaaaaaaaaaaa", "setup_code": code})
		if statusRu != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: error=%q code=%q", statusRu, respRu.Error, respRu.Code)
		}
		if respRu.Code != "password_repeated_char" {
			t.Fatalf("expected code=password_repeated_char, got %q (error=%q)", respRu.Code, respRu.Error)
		}
		wantRu := i18n.T("ru", "auth.password_repeated_char")
		if respRu.Error != wantRu {
			t.Errorf("ru: expected localized policy error %q, got %q", wantRu, respRu.Error)
		}

		statusEn, respEn := post(svc, "en", map[string]string{"password": "aaaaaaaaaaaa", "setup_code": code})
		if statusEn != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusEn)
		}
		wantEn := i18n.T("en", "auth.password_repeated_char")
		if respEn.Error != wantEn {
			t.Errorf("en: expected localized policy error %q, got %q", wantEn, respEn.Error)
		}
		if respRu.Error == respEn.Error {
			t.Fatalf("expected ru and en policy error text to differ, both were %q", respRu.Error)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		svc := NewAuthService(Options{MaxLoginAttempts: 3, LockoutDuration: time.Minute})
		defer svc.Stop()

		var lastRu struct {
			Error      string `json:"error"`
			Code       string `json:"code"`
			RetryAfter int    `json:"retry_after"`
		}
		var lastStatus int
		for i := 0; i < 3; i++ {
			lastStatus, lastRu = post(svc, "ru", map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
		}
		if lastStatus != http.StatusTooManyRequests {
			t.Fatalf("expected 429 after exhausting attempts, got %d", lastStatus)
		}
		wantRu := i18n.T("ru", "auth.rate_limited")
		if lastRu.Error != wantRu {
			t.Errorf("ru: expected localized rate-limit error %q, got %q", wantRu, lastRu.Error)
		}

		statusEn, respEn := post(svc, "en", map[string]string{"password": "validpassword123", "setup_code": "WRONGCOD"})
		if statusEn != http.StatusTooManyRequests {
			t.Fatalf("expected 429, got %d", statusEn)
		}
		wantEn := i18n.T("en", "auth.rate_limited")
		if respEn.Error != wantEn {
			t.Errorf("en: expected localized rate-limit error %q, got %q", wantEn, respEn.Error)
		}
		if lastRu.Error == respEn.Error {
			t.Fatalf("expected ru and en rate-limit error text to differ, both were %q", lastRu.Error)
		}
	})
}
