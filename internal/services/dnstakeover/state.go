package dnstakeover

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Состояние службы на диске: файл dns_takeover.json в каталоге данных панели.
// Хранится отдельно от состояния слоя: откат DNS при выключении флага слоя
// (D-36) должен найти его и тогда, когда выключенный слой ничего не читает.

const (
	// stateFile — имя файла состояния в Deps.DataDir.
	stateFile = "dns_takeover.json"
	// stateSchemaVersion — версия схемы файла. До 1.0 несовместимое изменение
	// поднимает версию без миграции: файл другой версии читается как чистое
	// состояние.
	stateSchemaVersion = 1
	// maxHistory — сколько последних записей истории хранится.
	maxHistory = 10
)

// HistoryEntry — запись истории действий перехвата.
type HistoryEntry struct {
	Kind   string `json:"kind"`
	AtMs   int64  `json:"at_ms"`
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
	Kernel string `json:"kernel,omitempty"`
}

// persisted — содержимое dns_takeover.json.
type persisted struct {
	SchemaVersion int            `json:"schema_version"`
	LastCheck     *CheckResult   `json:"last_check,omitempty"`
	History       []HistoryEntry `json:"history"`
}

// cleanState — состояние без записей.
func cleanState() persisted {
	return persisted{SchemaVersion: stateSchemaVersion, History: []HistoryEntry{}}
}

// statePath — путь к файлу состояния.
func (s *Service) statePath() string {
	return filepath.Join(s.d.DataDir, stateFile)
}

// loadState читает файл состояния. Файла нет — чистое состояние; повреждённый
// файл или другая версия схемы — запись в журнал и чистое состояние.
func loadState(path string) persisted {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cleanState()
	}
	if err != nil {
		log.Printf("dnstakeover: состояние не прочитано, начинаем с чистого: %v", err)
		return cleanState()
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("dnstakeover: состояние повреждено, начинаем с чистого: %v", err)
		return cleanState()
	}
	if p.SchemaVersion != stateSchemaVersion {
		log.Printf("dnstakeover: версия состояния %d вместо %d, начинаем с чистого", p.SchemaVersion, stateSchemaVersion)
		return cleanState()
	}
	if p.History == nil {
		p.History = []HistoryEntry{}
	}
	return p
}

// saveState записывает состояние атомарно (0600); история урезается до
// maxHistory последних записей.
func saveState(path string, p persisted) error {
	p.SchemaVersion = stateSchemaVersion
	if len(p.History) > maxHistory {
		p.History = append([]HistoryEntry(nil), p.History[len(p.History)-maxHistory:]...)
	}
	if p.History == nil {
		p.History = []HistoryEntry{}
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("dnstakeover: состояние: %w", err)
	}
	if err := utils.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("dnstakeover: запись состояния: %w", err)
	}
	return nil
}
