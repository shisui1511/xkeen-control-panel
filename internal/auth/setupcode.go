package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// setupCodeFileName — имя файла одноразового кода первичной настройки внутри
// DataDir панели (D-24). setupCodeAlphabet — 32 символа без визуально
// спутываемых 0/O/1/I; setupCodeLength=8 даёт 40 бит энтропии — вместе с
// rate-limit входа (тот же CheckLimit, что и HandleLogin) этого достаточно
// против подбора из LAN за время жизни кода (T-134-26).
const (
	setupCodeFileName = "setup_code"
	setupCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	setupCodeLength   = 8
)

// ErrSetupCodeInvalid — введённый код первичной настройки не совпадает с
// текущим ожидаемым. Экспортируется для будущего CLI-предъявления кода
// (134-10), а не только для HandleSetup.
var ErrSetupCodeInvalid = errors.New("invalid setup code")

// EnsureSetupCode возвращает текущий одноразовый код настройки для dataDir:
// если на диске уже лежит валидный (8 символов из setupCodeAlphabet после
// нормализации) код — переиспользует его (код переживает рестарт, D-24);
// иначе генерирует новый через crypto/rand и атомарно записывает файл с
// правами 0600. Пустой dataDir — генерация без записи на диск («только
// память», используется вызывающим кодом для memSetupCode).
func EnsureSetupCode(dataDir string) (string, error) {
	if dataDir == "" {
		return generateSetupCode()
	}
	if code, err := ReadSetupCode(dataDir); err == nil && isValidSetupCode(code) {
		return code, nil
	}
	code, err := generateSetupCode()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, setupCodeFileName)
	if err := utils.AtomicWriteFile(path, []byte(code+"\n"), 0600); err != nil {
		return "", err
	}
	return code, nil
}

// ReadSetupCode читает и нормализует файл кода настройки из dataDir.
// Отсутствие файла пробрасывается как есть (os.ErrNotExist через errors.Is).
func ReadSetupCode(dataDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, setupCodeFileName))
	if err != nil {
		return "", err
	}
	return NormalizeSetupCode(string(data)), nil
}

// RemoveSetupCode удаляет файл кода настройки; отсутствие файла — не ошибка.
// Пустой dataDir — no-op (режим «только память»).
func RemoveSetupCode(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	if err := os.Remove(filepath.Join(dataDir, setupCodeFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// NormalizeSetupCode приводит код к верхнему регистру и убирает пробелы и
// дефисы — пользователь может ввести код с разделителями («ab-cd-ef-gh») или
// в нижнем регистре.
func NormalizeSetupCode(s string) string {
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, "-", "")
	return strings.Join(strings.Fields(s), "")
}

// setupCodeMatches сравнивает нормализованные expected/given за постоянное
// время (T-134-26). Пустой expected никогда не совпадает — отсутствие
// настроенного кода не должно трактоваться как «любой ввод принимается».
func setupCodeMatches(expected, given string) bool {
	if expected == "" {
		return false
	}
	e := []byte(NormalizeSetupCode(expected))
	g := []byte(NormalizeSetupCode(given))
	if len(e) != len(g) {
		return false
	}
	return subtle.ConstantTimeCompare(e, g) == 1
}

// ValidateSetupCode — как setupCodeMatches, но возвращает ErrSetupCodeInvalid
// вместо bool; общая точка для HandleSetup и будущего CLI (134-10).
func ValidateSetupCode(expected, given string) error {
	if !setupCodeMatches(expected, given) {
		return ErrSetupCodeInvalid
	}
	return nil
}

func isValidSetupCode(code string) bool {
	if len(code) != setupCodeLength {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(setupCodeAlphabet, c) {
			return false
		}
	}
	return true
}

// generateSetupCode возвращает криптографически случайный код длины
// setupCodeLength из setupCodeAlphabet. len(setupCodeAlphabet)==32 делит 256
// без остатка, поэтому b%32 не даёт смещения распределения.
func generateSetupCode() (string, error) {
	buf := make([]byte, setupCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, setupCodeLength)
	for i, b := range buf {
		out[i] = setupCodeAlphabet[int(b)%len(setupCodeAlphabet)]
	}
	return string(out), nil
}
