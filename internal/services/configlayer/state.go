package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// ErrDraftConflict — правка черновика сделана на устаревшей ревизии.
var ErrDraftConflict = errors.New("configlayer: draft revision conflict")

// ErrInvalidSection — недопустимое имя секции черновика.
var ErrInvalidSection = errors.New("configlayer: invalid section name")

// ErrUnknownNotice — неизвестный идентификатор уведомления.
var ErrUnknownNotice = errors.New("configlayer: unknown notice id")

// sectionNameRE — допустимое имя секции черновика.
var sectionNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Store — хранилище состояния слоя: структура в памяти под мьютексом и
// JSON-файл в каталоге данных панели.
//
// Все мутации идут по схеме «копия → запись на диск → подмена в памяти», поэтому
// ошибка записи не оставляет в памяти изменений, которых нет на диске.
type Store struct {
	mu     sync.RWMutex
	path   string
	st     State
	broker *Broker
	// now — источник времени для имён копий состояния; в тестах подменяется.
	now func() time.Time
}

// OpenStore открывает (или создаёт) файл состояния в dataDir. broker может
// быть nil — тогда события не публикуются.
func OpenStore(dataDir string, broker *Broker) (*Store, error) {
	s := &Store{
		path:   filepath.Join(dataDir, StateFileName),
		broker: broker,
		now:    time.Now,
	}
	st, err := s.readFile()
	if err != nil {
		return nil, err
	}
	s.st = st
	return s, nil
}

// readFile читает файл состояния; если файла нет — создаёт пустое состояние.
//
// Файл с чужим schema_version или нечитаемым JSON не разбирается: его байты
// копируются в backup/config-layer/state, после чего создаётся пустое
// состояние с уведомлением schema_reset (D-08). Миграций нет.
func (s *Store) readFile() (State, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		st := emptyState()
		if err := s.persist(st); err != nil {
			return State{}, err
		}
		return st, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("configlayer: read state: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil || st.SchemaVersion != SchemaVersion {
		return s.resetState(data)
	}
	st.normalize()
	return st, nil
}

// resetState сохраняет исходные байты файла состояния в каталоге копий и
// записывает пустое состояние. Если копию сделать не удалось, возвращается
// ошибка и прежний файл остаётся нетронутым: байты старого состояния не
// теряются.
func (s *Store) resetState(original []byte) (State, error) {
	backupPath, err := s.backupOriginal(original)
	if err != nil {
		return State{}, err
	}
	st := emptyState()
	st.Notices.SchemaReset = true
	st.Notices.SchemaResetBackup = backupPath
	if err := s.persist(st); err != nil {
		return State{}, err
	}
	return st, nil
}

// backupOriginal пишет копию state.<unix>.json (0600) в каталог 0700. Если файл
// с таким именем уже есть (два сброса в одну секунду), добавляется суффикс.
func (s *Store) backupOriginal(original []byte) (string, error) {
	dir := filepath.Join(filepath.Dir(s.path), "backup", "config-layer", "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("configlayer: create state backup dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("configlayer: chmod state backup dir: %w", err)
	}

	unix := s.now().Unix()
	path := filepath.Join(dir, fmt.Sprintf("state.%d.json", unix))
	for n := 1; ; n++ {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return "", fmt.Errorf("configlayer: check state backup: %w", err)
		}
		path = filepath.Join(dir, fmt.Sprintf("state.%d.%d.json", unix, n))
	}
	if err := utils.AtomicWriteFile(path, original, 0o600); err != nil {
		return "", fmt.Errorf("configlayer: write state backup: %w", err)
	}
	return path, nil
}

// persist сохраняет состояние на диск (права 0600).
func (s *Store) persist(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("configlayer: encode state: %w", err)
	}
	if err := utils.AtomicWriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("configlayer: write state: %w", err)
	}
	return nil
}

// Snapshot возвращает глубокую копию текущего состояния.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st.clone()
}

// DraftRevision возвращает текущую ревизию черновика.
func (s *Store) DraftRevision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.st.DraftRevision
}

// DraftChanges возвращает число неприменённых изменений черновика.
func (s *Store) DraftChanges() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return draftChanges(s.st)
}

// draftChanges считает секции, чей черновик отличается от применённого
// состояния: ключ есть только с одной стороны или значения не равны.
func draftChanges(st State) int {
	n := 0
	for k, d := range st.Draft {
		a, ok := st.Applied[k]
		if !ok || !SectionEqual(d, a) {
			n++
		}
	}
	for k := range st.Applied {
		if _, ok := st.Draft[k]; !ok {
			n++
		}
	}
	return n
}

func draftEventOf(st State) DraftEvent {
	return DraftEvent{DraftRevision: st.DraftRevision, DraftChanges: draftChanges(st)}
}

// EditDraft меняет секцию черновика, если baseRev совпадает с текущей
// ревизией (сравнить и записать). Значение nil или JSON null удаляет секцию.
// При конфликте возвращается ErrDraftConflict, состояние и файл не меняются,
// событие не публикуется.
func (s *Store) EditDraft(baseRev int64, section string, value json.RawMessage) (DraftEvent, error) {
	if !sectionNameRE.MatchString(section) {
		return DraftEvent{}, fmt.Errorf("%w: %q", ErrInvalidSection, section)
	}

	s.mu.Lock()
	if baseRev != s.st.DraftRevision {
		s.mu.Unlock()
		return DraftEvent{}, ErrDraftConflict
	}

	next := s.st.clone()
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		delete(next.Draft, section)
	} else {
		compact, err := compactRaw(value)
		if err != nil {
			s.mu.Unlock()
			return DraftEvent{}, fmt.Errorf("configlayer: section value is not valid JSON: %w", err)
		}
		next.Draft[section] = compact
	}
	next.DraftRevision++

	if err := s.persist(next); err != nil {
		s.mu.Unlock()
		return DraftEvent{}, err
	}
	s.st = next
	ev := draftEventOf(next)
	s.mu.Unlock()

	s.broker.Publish(Event{Type: EventDraft, Data: ev})
	return ev, nil
}

// ResetDraft сбрасывает черновик к применённому состоянию, если baseRev
// совпадает с текущей ревизией. Ревизия растёт, публикуется событие draft.
func (s *Store) ResetDraft(baseRev int64) (DraftEvent, error) {
	s.mu.Lock()
	if baseRev != s.st.DraftRevision {
		s.mu.Unlock()
		return DraftEvent{}, ErrDraftConflict
	}

	next := s.st.clone()
	next.Draft = next.Applied.Clone()
	next.DraftRevision++

	if err := s.persist(next); err != nil {
		s.mu.Unlock()
		return DraftEvent{}, err
	}
	s.st = next
	ev := draftEventOf(next)
	s.mu.Unlock()

	s.broker.Publish(Event{Type: EventDraft, Data: ev})
	return ev, nil
}

// Update применяет fn к глубокой копии состояния и сохраняет копию. Если fn
// вернула ошибку или запись на диск не удалась, состояние в памяти и файл не
// меняются. Событий Update не публикует: их рассылает вызывающий конвейер.
func (s *Store) Update(fn func(st *State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.st.clone()
	if err := fn(&next); err != nil {
		return err
	}
	next.normalize()
	if err := s.persist(next); err != nil {
		return err
	}
	s.st = next
	return nil
}

// DismissNotice закрывает уведомление и сохраняет состояние. Допустимые id:
// "schema_reset" и "recovered_from_journal".
func (s *Store) DismissNotice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.st.clone()
	switch id {
	case "schema_reset":
		next.Notices.SchemaReset = false
		next.Notices.SchemaResetBackup = ""
	case "recovered_from_journal":
		next.Notices.RecoveredFromJournal = false
	default:
		return fmt.Errorf("%w: %q", ErrUnknownNotice, id)
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.st = next
	return nil
}

// Reload перечитывает файл состояния по тем же правилам, что и OpenStore.
//
// Порядок замков: Reload берёт только замок Store и никогда не берёт замки
// конвейера применения. Его вызывает обработчик восстановления снимка, который
// уже держит замок жизненного цикла ядер, поэтому обратный порядок невозможен.
// Если чтение не удалось, состояние в памяти не меняется.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.readFile()
	if err != nil {
		return err
	}
	s.st = st
	return nil
}
