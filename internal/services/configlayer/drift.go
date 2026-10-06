package configlayer

import (
	"crypto/md5" //nolint:gosec // контроль целостности по D-09, не защита от злоумышленника
	"encoding/hex"
	"os"
)

// FileState — результат сверки записи манифеста с диском.
type FileState string

// Состояния файла.
const (
	StateOK            FileState = "ok"
	StatePending       FileState = "pending"
	StateDriftModified FileState = "drift_modified"
	StateDriftMissing  FileState = "drift_missing"
	StateDriftRenamed  FileState = "drift_renamed"
	StateDriftEmpty    FileState = "drift_empty"
	StateReleased      FileState = "released"
)

// FileCheck — итог сверки одной записи манифеста.
type FileCheck struct {
	Key          string
	Kernel       string
	RelPath      string
	AbsPath      string
	Kind         FileKind
	State        FileState
	ObsoleteName string
	ActualHash   string
}

// IsDrift — состояние означает расхождение с тем, что записала панель.
func (s FileState) IsDrift() bool {
	switch s {
	case StateDriftModified, StateDriftMissing, StateDriftRenamed, StateDriftEmpty:
		return true
	}
	return false
}

// HashContent возвращает md5 содержимого в hex. Это контроль целостности по
// D-09 (файл изменили снаружи или нет), а не мера безопасности. Если CodeQL
// заменит алгоритм на sha256, схема не меняется: поле называется hash.
func HashContent(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // см. комментарий выше
	return hex.EncodeToString(sum[:])
}

// CheckEntry сверяет запись манифеста с диском. Отпущенная запись диск не
// читает.
func CheckEntry(roots Roots, e ManifestEntry) FileCheck {
	fc := FileCheck{
		Key:     ManifestKey(e.Kernel, e.RelPath),
		Kernel:  e.Kernel,
		RelPath: e.RelPath,
		Kind:    e.Kind,
	}
	if e.Status == StatusReleased {
		fc.State = StateReleased
		return fc
	}
	abs, err := roots.Abs(e.Kernel, e.RelPath)
	if err != nil {
		fc.State = StateDriftMissing
		return fc
	}
	fc.AbsPath = abs
	data, err := os.ReadFile(abs)
	if err != nil {
		fc.State = StateDriftModified
		return fc
	}
	fc.ActualHash = HashContent(data)
	if fc.ActualHash == e.Hash {
		fc.State = StateOK
		return fc
	}
	fc.State = StateDriftModified
	return fc
}
