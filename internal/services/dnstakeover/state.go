package dnstakeover

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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
	// Desired — пользователь хочет перехват (включение прошло, выключения не было);
	// Kernel — ядро-перехватчик; KernelWasRunning — ядро работало при включении.
	// Их читает возврат перехвата после перезагрузки и остановки ядра (148-22).
	Desired          bool   `json:"desired"`
	Kernel           string `json:"kernel,omitempty"`
	KernelWasRunning bool   `json:"kernel_was_running,omitempty"`
	// Snapshot — снимок до включения: пишется до первой команды прошивки; после
	// успешной пробы переходит в окно подтверждения, снимается откатом и выключением.
	Snapshot *txnSnapshot `json:"snapshot,omitempty"`
	// OverrideBefore — override прошивки до включения перехвата: окно и снимок
	// закрыты, а выключение, откат по виду kernel_switch и возврат DNS прошивке
	// возвращают его к этому значению.
	OverrideBefore bool `json:"override_before,omitempty"`
	// WaitingReason — перехват желаем, но DNS у прошивки (probe, kernel_stopped,
	// watchdog): состояние waiting; пусто — не ждёт.
	WaitingReason string `json:"waiting_reason,omitempty"`
	// Window — окно подтверждения (D-05, D-07): вид, дедлайн и снимок, к которому
	// возвращает откат. Переживает перезапуск панели только до её старта: Start
	// откатывает неподтверждённый перехват сразу.
	Window *windowState `json:"window,omitempty"`
}

// windowState — окно подтверждения на диске.
type windowState struct {
	// Kind — вид окна: enable, apply, kernel_switch.
	Kind string `json:"kind"`
	// DeadlineUnixMs — дедлайн окна, unix-время в миллисекундах.
	DeadlineUnixMs int64 `json:"deadline_unix_ms"`
	// Snapshot — состояние до действия, открывшего окно: Applied, Live перехвата,
	// override и proxy_dns до него.
	Snapshot txnSnapshot `json:"snapshot"`
}

// txnSnapshot — состояние до включения перехвата: к нему возвращает откат.
type txnSnapshot struct {
	AtMs   int64  `json:"at_ms"`
	Kernel string `json:"kernel"`
	// Applied — применённое состояние слоя целиком до включения.
	Applied configlayer.Sections `json:"applied"`
	// Live — прежнее значение Live[dns_takeover]; пусто — ключа не было.
	Live json.RawMessage `json:"live,omitempty"`
	// OverrideBefore — override прошивки до включения.
	OverrideBefore bool `json:"override_before"`
	// ProxyDNSBefore — строка proxy_dns XKeen до включения (D-33).
	ProxyDNSBefore bool `json:"proxy_dns_before"`
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
