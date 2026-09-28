package auth

import (
	_ "embed"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// passwordMinBytes/passwordMaxBytes — границы длины нового пароля в байтах
// (D-17). 8 — прежний минимум, действовавший до этого плана. 72 — жёсткий
// предел bcrypt (GenerateFromPassword возвращает bcrypt.ErrPasswordTooLong
// выше этого значения, RESEARCH Pitfall 3); проверяется здесь заранее, чтобы
// дать понятный 400 password_too_long вместо необработанного 500. Длина
// считается в байтах — тот же порядок, что len() в Go и utf8ByteLength на
// фронтенде (134-04): кириллица (2 байта/символ) достигает границы вдвое
// быстрее ASCII.
const (
	passwordMinBytes = 8
	passwordMaxBytes = 72
)

//go:embed password_blacklist.txt
var passwordBlacklistRaw string

// passwordBlacklist — набор запрещённых паролей в нижнем регистре, собранный
// один раз при инициализации пакета из password_blacklist.txt. Тот же набор,
// что frontend/src/lib/passwordBlacklist.json (134-04) — паритет проверяет
// TestPasswordBlacklist_MatchesFrontend.
var passwordBlacklist = parsePasswordBlacklist(passwordBlacklistRaw)

func parsePasswordBlacklist(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		set[strings.ToLower(line)] = struct{}{}
	}
	return set
}

// Ошибки-сентинелы политики нового пароля (D-17), по образцу
// ErrTooManyAttempts (auth.go). PolicyErrorCode переводит их в машиночитаемый
// код HTTP-ответа; сам текст ошибки — только для случаев без доступа к
// переводчику i18n (пакет auth не знает про internal/i18n).
var (
	ErrPasswordTooShort      = errors.New("password must be at least 8 bytes")
	ErrPasswordTooLong       = errors.New("password must not exceed 72 bytes")
	ErrPasswordRepeatedChar  = errors.New("password must not be a single repeated character")
	ErrPasswordBlacklisted   = errors.New("password is too common")
	ErrPasswordSameAsCurrent = errors.New("new password must differ from the current password")
)

// ValidateNewPassword проверяет новый пароль по единой политике (D-17),
// общей для setup (HandleSetup), смены пароля (ChangePassword) и будущего
// CLI-сброса (134-10): длина 8-72 байта, запрет пароля из одного
// повторяющегося символа, запрет встроенного чёрного списка без учёта
// регистра, запрет совпадения с текущим паролем (по bcrypt-хешу).
// currentHash пустой — проверка совпадения с текущим пропускается (сценарий
// setup, где текущего пароля ещё нет). Порядок проверок фиксирован (совпадает
// с passwordPolicy.ts на фронтенде, 134-04): too_short -> too_long ->
// repeated_char -> blacklisted -> same_as_current.
func ValidateNewPassword(newPassword, currentHash string) error {
	if len(newPassword) < passwordMinBytes {
		return ErrPasswordTooShort
	}
	if len(newPassword) > passwordMaxBytes {
		return ErrPasswordTooLong
	}
	if isSingleRepeatedChar(newPassword) {
		return ErrPasswordRepeatedChar
	}
	if _, blacklisted := passwordBlacklist[strings.ToLower(newPassword)]; blacklisted {
		return ErrPasswordBlacklisted
	}
	if currentHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(newPassword)); err == nil {
			return ErrPasswordSameAsCurrent
		}
	}
	return nil
}

// isSingleRepeatedChar сообщает, состоит ли пароль из одной и той же руны,
// повторённой во всю длину ("aaaaaaaa", "ЯЯЯЯЯЯЯЯ").
func isSingleRepeatedChar(password string) bool {
	runes := []rune(password)
	if len(runes) == 0 {
		return false
	}
	first := runes[0]
	for _, r := range runes[1:] {
		if r != first {
			return false
		}
	}
	return true
}

// PolicyErrorCode переводит ошибку ValidateNewPassword в машиночитаемый код
// для JSON-ответа (поле code) — клиент/фронтенд переводит его в текст сам
// (policyErrorKey, 134-04), не парсит error. Любая другая ошибка (включая
// nil) даёт пустую строку — вызывающий код (handlers.ChangePassword) отличает
// «это не ошибка политики» от «это ошибка политики без кода».
func PolicyErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrPasswordTooShort):
		return "password_too_short"
	case errors.Is(err, ErrPasswordTooLong):
		return "password_too_long"
	case errors.Is(err, ErrPasswordRepeatedChar):
		return "password_repeated_char"
	case errors.Is(err, ErrPasswordBlacklisted):
		return "password_blacklisted"
	case errors.Is(err, ErrPasswordSameAsCurrent):
		return "password_same_as_current"
	default:
		return ""
	}
}

// GeneratePasswordHash хеширует пароль bcrypt-ом с DefaultCost — единая точка
// хеширования для веба (AuthService.HashPassword делегирует сюда) и будущего
// CLI-сброса пароля (134-10), чтобы оба пути использовали один и тот же cost.
func GeneratePasswordHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
