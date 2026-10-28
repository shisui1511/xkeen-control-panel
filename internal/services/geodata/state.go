package geodata

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// stateFileName — файл состояния менеджера в каталоге данных панели. Он живёт
// отдельно от состояния слоя «Конфигурация» и не зависит от флага слоя: возврат
// cron XKeen при выключении флага и догон пропущенного запуска опираются на него.
const stateFileName = "geodata_state.json"

// stateVersion — версия формата файла состояния.
const stateVersion = 1

// ScheduleState — расписание обновления баз панелью. Day: -1 — ежедневно,
// 0..6 — день недели (0 — воскресенье).
type ScheduleState struct {
	Enabled bool `json:"enabled"`
	Day     int  `json:"day"`
	Hour    int  `json:"hour"`
	Minute  int  `json:"minute"`
}

// CronTaken — строка cron XKeen, которую панель убрала при перенятии расписания
// (дословно, для возврата при выключении флага слоя).
type CronTaken struct {
	Line string    `json:"line"`
	At   time.Time `json:"at"`
}

// LastRun — итог последнего запуска `xkeen -ug`.
type LastRun struct {
	At         time.Time `json:"at"`
	Finished   time.Time `json:"finished"`
	Outcome    string    `json:"outcome"`
	NotUpdated int       `json:"not_updated"`
	Parsed     bool      `json:"parsed"`
	Reason     string    `json:"reason,omitempty"`
	ExitCode   int       `json:"exit_code"`
	Trigger    string    `json:"trigger,omitempty"`
}

// FileResult — итог последнего запуска по одному файлу.
type FileResult struct {
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	Unverified bool      `json:"unverified,omitempty"`
	Limit      bool      `json:"limit,omitempty"`
	At         time.Time `json:"at"`
}

// PendingEnable — стандартный набор, включённый временной записью geodata и
// ожидающий скачивания файла (D-01).
type PendingEnable struct {
	File  string    `json:"file"`
	URL   string    `json:"url"`
	Since time.Time `json:"since"`
}

// PendingDelete — незавершённое удаление источника: запись или файл не убраны,
// Reason — почему; Entry — запись geodata остаётся в xkeen.json.
type PendingDelete struct {
	File   string    `json:"file"`
	Reason string    `json:"reason"`
	Entry  bool      `json:"entry"`
	At     time.Time `json:"at"`
}

// State — состояние менеджера geodata (DataDir/geodata_state.json).
type State struct {
	V        int           `json:"v"`
	Schedule ScheduleState `json:"schedule"`
	// Suspended — расписание панели приостановлено: базы обновлял cron XKeen (D-36).
	Suspended bool `json:"suspended"`
	// CronTaken — строка cron XKeen, убранная при перенятии; nil — панель ничего не убирала.
	CronTaken *CronTaken `json:"cron_taken,omitempty"`
	// CronKept — строка cron XKeen, которую владелец оставил («Не сейчас»).
	CronKept     string `json:"cron_kept,omitempty"`
	CronTwo      bool   `json:"cron_two"`
	CronSchedule string `json:"cron_schedule,omitempty"`
	// LastSlotDone — слот расписания, который уже отработан (RFC3339).
	LastSlotDone string `json:"last_slot_done,omitempty"`
	// RetryAt — время повтора после сбоя сети; RetryFailed — повтор тоже не удался.
	RetryAt     time.Time `json:"retry_at"`
	RetryFailed bool      `json:"retry_failed"`
	LastRun     *LastRun  `json:"last_run,omitempty"`
	// Results — итог последнего запуска по файлам.
	Results  map[string]FileResult `json:"results"`
	GeoIPSet *FileResult           `json:"geoipset,omitempty"`
	// Hashes — SHA-256 файлов источников после последнего запуска.
	Hashes map[string]string `json:"hashes"`
	// Sigs — сигнатуры (размер:mtime) dat-файлов, по которым находят правку вне панели.
	Sigs map[string]string `json:"sigs"`
	// Outside — файлы, изменённые вне панели, и время изменения (D-17).
	Outside        map[string]time.Time `json:"outside"`
	PendingEnables []PendingEnable      `json:"pending_enables"`
	// IncludeStuck — запись не убрана после скачивания: файл → причина.
	IncludeStuck map[string]string `json:"include_stuck"`
	// Cleaned — когда панель убрала временную запись набора.
	Cleaned        map[string]time.Time `json:"cleaned"`
	PendingDeletes []PendingDelete      `json:"pending_deletes"`
	JQError        string               `json:"jq_error,omitempty"`
}

// newState — пустое состояние: расписание выключено, предложенное время — ночь (D-12).
func newState() State {
	st := State{V: stateVersion, Schedule: ScheduleState{Day: -1, Hour: 4, Minute: 15}}
	st.normalize()
	return st
}

// normalize заводит пустые словари и списки: после разбора JSON они бывают nil.
func (st *State) normalize() {
	if st.V == 0 {
		st.V = stateVersion
	}
	if st.Results == nil {
		st.Results = map[string]FileResult{}
	}
	if st.Hashes == nil {
		st.Hashes = map[string]string{}
	}
	if st.Sigs == nil {
		st.Sigs = map[string]string{}
	}
	if st.Outside == nil {
		st.Outside = map[string]time.Time{}
	}
	if st.IncludeStuck == nil {
		st.IncludeStuck = map[string]string{}
	}
	if st.Cleaned == nil {
		st.Cleaned = map[string]time.Time{}
	}
	if st.PendingEnables == nil {
		st.PendingEnables = []PendingEnable{}
	}
	if st.PendingDeletes == nil {
		st.PendingDeletes = []PendingDelete{}
	}
}

// clone — глубокая копия: снимок и уведомления читают её без замка.
func (st State) clone() State {
	data, err := json.Marshal(st)
	if err != nil {
		return newState()
	}
	var out State
	if err := json.Unmarshal(data, &out); err != nil {
		return newState()
	}
	out.normalize()
	return out
}

// loadState читает состояние. Нет файла — пустое состояние; битый JSON — пустое
// состояние и запись в журнал панели.
func loadState(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("[geodata] не удалось прочитать %s: %v", stateFileName, err)
		}
		return newState()
	}
	st := newState()
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("[geodata] %s повреждён, начато с пустого состояния: %v", stateFileName, err)
		return newState()
	}
	st.normalize()
	return st
}

// saveState записывает состояние атомарно (0600).
func saveState(path string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0o600)
}
