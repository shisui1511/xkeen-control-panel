package configlayer

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"time"
)

// SchemaVersion — номер схемы файла состояния.
//
// Правило: любое несовместимое изменение формата — это SchemaVersion+1, без
// миграции и переноса. Файл с чужим номером схемы (или нечитаемый) не
// разбирается: его байты копируются в backup/config-layer/state, а панель
// стартует с пустым состоянием и флагом уведомления schema_reset (D-08).
const SchemaVersion int64 = 1

// StateFileName — имя файла состояния в каталоге данных панели.
const StateFileName = "config_layer_state.json"

// Ядра, которыми владеет слой.
const (
	KernelXray   = "xray"
	KernelMihomo = "mihomo"
)

// FileKind — вид файла, который слой пишет в каталог ядра.
type FileKind string

// Виды файлов манифеста.
const (
	KindXrayJSON            FileKind = "xray_json"
	KindMihomoProxyProvider FileKind = "mihomo_provider_proxies"
	KindMihomoRuleProvider  FileKind = "mihomo_provider_rules"
	KindMihomoProfile       FileKind = "mihomo_profile"
)

// EntryStatus — состояние записи манифеста.
type EntryStatus string

// Статусы записей манифеста.
const (
	StatusManaged  EntryStatus = "managed"
	StatusReleased EntryStatus = "released"
)

// ManifestEntry — запись о файле, которым владеет панель.
type ManifestEntry struct {
	Kernel    string      `json:"kernel"`
	RelPath   string      `json:"rel_path"`
	Kind      FileKind    `json:"kind"`
	Hash      string      `json:"hash"`
	Status    EntryStatus `json:"status"`
	WrittenAt time.Time   `json:"written_at"`
}

// ManifestKey — ключ записи манифеста: ядро и путь внутри каталога ядра.
func ManifestKey(kernel, rel string) string {
	return kernel + ":" + filepath.ToSlash(rel)
}

// Sections — секции черновика или применённого состояния: имя → JSON.
type Sections map[string]json.RawMessage

// Clone возвращает глубокую копию; результат всегда не nil.
func (s Sections) Clone() Sections {
	out := make(Sections, len(s))
	for k, v := range s {
		out[k] = cloneRaw(v)
	}
	return out
}

// SectionEqual сравнивает два JSON-значения после json.Compact: пробелы и
// переводы строк значения не имеют. Если один из операндов не компактится
// (невалидный JSON), сравниваются исходные байты.
func SectionEqual(a, b json.RawMessage) bool {
	ca, errA := compactRaw(a)
	cb, errB := compactRaw(b)
	if errA != nil || errB != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca, cb)
}

func compactRaw(v json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cloneRaw(v json.RawMessage) json.RawMessage {
	if v == nil {
		return nil
	}
	out := make(json.RawMessage, len(v))
	copy(out, v)
	return out
}

// JournalFile — файл, затронутый записью конвейера применения.
type JournalFile struct {
	Key     string `json:"key"`
	Existed bool   `json:"existed"`
	NewHash string `json:"new_hash"`
}

// Journal — журнал незавершённой записи (для восстановления после сбоя).
type Journal struct {
	BackupDir string        `json:"backup_dir"`
	Trigger   string        `json:"trigger"`
	StartedAt time.Time     `json:"started_at"`
	Files     []JournalFile `json:"files"`
}

// Notices — уведомления, которые панель показывает до явного закрытия.
type Notices struct {
	SchemaReset          bool   `json:"schema_reset,omitempty"`
	SchemaResetBackup    string `json:"schema_reset_backup,omitempty"`
	RecoveredFromJournal bool   `json:"recovered_from_journal,omitempty"`
	// JournalRecoveryFailed — журнал прерванной записи снят, но файлы по нему не
	// вернулись (набор копий не читается): часть файлов может быть записана не до конца.
	JournalRecoveryFailed bool `json:"journal_recovery_failed,omitempty"`
}

// State — содержимое файла состояния.
type State struct {
	SchemaVersion int64                    `json:"schema_version"`
	DraftRevision int64                    `json:"draft_revision"`
	Draft         Sections                 `json:"draft"`
	Applied       Sections                 `json:"applied"`
	Manifest      map[string]ManifestEntry `json:"manifest"`
	Notices       Notices                  `json:"notices"`
	Journal       *Journal                 `json:"journal,omitempty"`
}

// emptyState — чистое состояние текущей схемы.
func emptyState() State {
	return State{
		SchemaVersion: SchemaVersion,
		Draft:         Sections{},
		Applied:       Sections{},
		Manifest:      map[string]ManifestEntry{},
	}
}

// clone возвращает глубокую копию состояния (секции, манифест, журнал).
func (st State) clone() State {
	out := st
	out.Draft = st.Draft.Clone()
	out.Applied = st.Applied.Clone()
	out.Manifest = make(map[string]ManifestEntry, len(st.Manifest))
	for k, v := range st.Manifest {
		out.Manifest[k] = v
	}
	if st.Journal != nil {
		j := *st.Journal
		j.Files = append([]JournalFile(nil), st.Journal.Files...)
		out.Journal = &j
	}
	return out
}

// normalize заменяет nil-коллекции пустыми после чтения файла.
func (st *State) normalize() {
	if st.Draft == nil {
		st.Draft = Sections{}
	}
	if st.Applied == nil {
		st.Applied = Sections{}
	}
	if st.Manifest == nil {
		st.Manifest = map[string]ManifestEntry{}
	}
}
