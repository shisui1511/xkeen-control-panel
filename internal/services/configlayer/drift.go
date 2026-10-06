package configlayer

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

// HashContent — хэш содержимого файла.
func HashContent(b []byte) string { return "" }

// CheckEntry сверяет запись манифеста с диском.
func CheckEntry(roots Roots, e ManifestEntry) FileCheck { return FileCheck{} }
