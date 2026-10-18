package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
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
// Мутации применённого состояния, манифеста, журнала, живых данных и уведомлений
// идут по схеме «копия → запись на диск с fsync → подмена в памяти», поэтому
// ошибка записи не оставляет в памяти изменений, которых нет на диске. Правки
// черновика (EditDraft, EditDraftBatch, ResetDraft) меняют состояние в памяти
// сразу, а файл пишется в фоне без fsync (flushLoop): ответ на правку не ждёт
// диск. На флешке роутера под чужой записью запись файла ждёт её — fsync до
// фиксации журнала ext4, подмена переименованием до записи прежней версии файла,
// стоящей в очереди устройства за чужими данными, — это десятки секунд. Запись
// без fsync держится на том, что ext4 с auto_da_alloc (по умолчанию) сбрасывает
// данные файла, подменяющего другой переименованием, раньше самого
// переименования; на другой ФС или с noauto_da_alloc после обрыва питания возможен
// пустой файл. Поэтому, когда незаписанных правок не осталось, файл
// досинхронизируется fsync в отдельной горутине (scheduleSync), не задерживая
// правки: окно риска — только от правки до конца этого fsync. При обрыве питания в
// этом окне теряются последние правки черновика; следующая синхронная запись
// включает и их.
//
// Файл читается (или создаётся) лениво, при первом обращении включённого слоя
// (Load): выключенный слой ничего не читает и не пишет (FND-01). Пока состояние
// не загружено, мутации отказывают ошибкой загрузки, а чтение отдаёт пустое
// состояние, поэтому незагруженное состояние не затирает файл.
type Store struct {
	mu     sync.RWMutex
	path   string
	st     State
	loaded bool
	broker *Broker
	// now — источник времени для имён копий состояния; в тестах подменяется.
	now func() time.Time

	// ver — версия состояния в памяти: растёт с каждой подменой st (под mu).
	ver uint64
	// flushing — идёт фоновая запись правок черновика (под mu); flushDone
	// сигналит её конец (L — &mu).
	flushing  bool
	flushDone *sync.Cond
	// wmu упорядочивает записи файла; written — версия, лежащая в файле
	// (меняется под wmu): запись версии старше записанной пропускается.
	wmu     sync.Mutex
	written atomic.Uint64
	// syncing — идёт досинхронизация файла (scheduleSync); syncWanted — после
	// неё файл снова записывался без fsync и нужен ещё один проход.
	syncing    atomic.Bool
	syncWanted atomic.Bool
}

// NewStore создаёт хранилище, не обращаясь к диску: файл состояния читается или
// создаётся при первом Load (или первом чтении и мутации). broker может быть nil.
func NewStore(dataDir string, broker *Broker) *Store {
	s := &Store{
		path:   filepath.Join(dataDir, StateFileName),
		broker: broker,
		now:    time.Now,
	}
	s.flushDone = sync.NewCond(&s.mu)
	return s
}

// Load загружает состояние с диска, если оно ещё не загружено (читает файл или
// создаёт пустой). Повторный вызов после успеха ничего не делает.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// loadLocked — Load под замком записи.
func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}
	st, err := s.readFile()
	if err != nil {
		return err
	}
	s.commitLocked(st)
	s.loaded = true
	s.markWrittenLocked()
	return nil
}

// Invalidate сбрасывает состояние в памяти, не трогая диск: следующее обращение
// включённого слоя прочитает файл заново. Нужен выключенному слою после
// восстановления снимка панели; незаписанные правки черновика отбрасываются.
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.markWrittenLocked()
	s.st, s.loaded = State{}, false
	s.mu.Unlock()
}

// Flush дожидается фоновой записи правок черновика.
func (s *Store) Flush() {
	s.mu.Lock()
	for s.flushing {
		s.flushDone.Wait()
	}
	s.mu.Unlock()
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

// persist записывает next — следующую версию состояния — на диск (права 0600) с
// fsync. Вызывается под mu до подмены; после успеха вызывающий делает
// commitLocked(next). next включает и незаписанные фоном правки черновика.
func (s *Store) persist(next State) error {
	return s.writeVersion(next, s.ver+1, utils.AtomicWriteFile)
}

// commitLocked подменяет состояние в памяти и поднимает версию (под mu).
func (s *Store) commitLocked(next State) {
	s.st = next
	s.ver++
}

// markWrittenLocked отмечает, что файл соответствует версии в памяти (под mu):
// дожидается текущей фоновой записи, а незаписанные версии пропускаются.
func (s *Store) markWrittenLocked() {
	s.wmu.Lock()
	s.written.Store(s.ver)
	s.wmu.Unlock()
}

// scheduleFlushLocked запускает фоновую запись правок черновика, если она ещё
// не идёт (под mu).
func (s *Store) scheduleFlushLocked() {
	if s.flushing {
		return
	}
	s.flushing = true
	go s.flushLoop()
}

// flushLoop пишет в файл последнюю версию состояния без fsync
// (utils.AtomicWriteFileNoSync), пока в памяти есть незаписанные правки. Ошибка
// записи логируется: правки остаются в памяти, их запишет повтор по более новой
// версии, следующая правка черновика или синхронная запись (FlushSync при
// остановке).
func (s *Store) flushLoop() {
	wrote := false
	for {
		s.mu.Lock()
		ver := s.ver
		if !s.loaded || s.written.Load() >= ver {
			s.flushing = false
			s.flushDone.Broadcast()
			s.mu.Unlock()
			if wrote {
				s.scheduleSync()
			}
			return
		}
		st := s.st.clone()
		s.mu.Unlock()

		if err := s.writeVersion(st, ver, utils.AtomicWriteFileNoSync); err != nil {
			log.Printf("[configlayer] правка черновика не записана на диск: %v", err)
			s.mu.Lock()
			// Правка, пришедшая во время неудачной записи, не запускала новую
			// горутину (flushing был true): без повтора её никто не записал бы.
			// Повтор идёт только по новой версии, поэтому постоянный сбой диска
			// не крутит цикл вхолостую.
			if s.ver != ver {
				s.mu.Unlock()
				continue
			}
			s.flushing = false
			s.flushDone.Broadcast()
			s.mu.Unlock()
			if wrote {
				s.scheduleSync()
			}
			return
		}
		wrote = true
	}
}

// scheduleSync досинхронизирует файл состояния (fsync) в отдельной горутине:
// запись без fsync (flushLoop) не защищает от пустого файла после обрыва питания
// вне ext4 с auto_da_alloc, а ожидание fsync внутри flushLoop держало бы
// следующую фоновую запись. Ни mu, ни wmu не берётся: fsync по пути относится к
// файлу, лежащему там сейчас, а более новая подмена сама попросит ещё проход.
func (s *Store) scheduleSync() {
	s.syncWanted.Store(true)
	if !s.syncing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		for s.syncWanted.Swap(false) {
			f, err := os.Open(s.path)
			if err != nil {
				continue
			}
			if err := f.Sync(); err != nil {
				log.Printf("[configlayer] fsync файла состояния: %v", err)
			}
			_ = f.Close()
		}
		s.syncing.Store(false)
		// Запрос, пришедший между последней проверкой и сбросом флага.
		if s.syncWanted.Load() {
			s.scheduleSync()
		}
	}()
}

// FlushSync дожидается фоновой записи и, если в файле осталась версия старше
// памяти (запись не удалась или не успела), пишет текущее состояние синхронно, с
// fsync. Вызывается при остановке слоя и перед самообновлением панели: процесс
// завершается без отложенных вызовов, и правка черновика, лежавшая только в
// памяти, иначе пропала бы.
func (s *Store) FlushSync() error {
	s.Flush()
	s.mu.Lock()
	if !s.loaded || s.written.Load() >= s.ver {
		s.mu.Unlock()
		return nil
	}
	st, ver := s.st.clone(), s.ver
	s.mu.Unlock()
	return s.writeVersion(st, ver, utils.AtomicWriteFile)
}

// writeVersion пишет st версии ver, если в файле нет версии новее.
func (s *Store) writeVersion(st State, ver uint64, writeFile func(path string, data []byte, perm os.FileMode) error) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.written.Load() >= ver {
		return nil
	}
	if err := s.write(st, writeFile); err != nil {
		return err
	}
	s.written.Store(ver)
	return nil
}

func (s *Store) write(st State, writeFile func(path string, data []byte, perm os.FileMode) error) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("configlayer: encode state: %w", err)
	}
	if err := writeFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("configlayer: write state: %w", err)
	}
	return nil
}

// Snapshot возвращает глубокую копию текущего состояния.
func (s *Store) Snapshot() State {
	var out State
	s.read(func(st State) { out = st.clone() })
	return out
}

// DraftRevision возвращает текущую ревизию черновика.
func (s *Store) DraftRevision() int64 {
	var rev int64
	s.read(func(st State) { rev = st.DraftRevision })
	return rev
}

// DraftChanges возвращает число неприменённых изменений черновика.
func (s *Store) DraftChanges() int {
	var n int
	s.read(func(st State) { n = draftChanges(st) })
	return n
}

// read выполняет fn над состоянием под замком, загрузив его при необходимости.
// Если загрузить не удалось, fn получает пустое состояние (мутации в этом случае
// отказывают ошибкой загрузки и файл не затирают).
func (s *Store) read(fn func(st State)) {
	s.mu.RLock()
	if s.loaded {
		fn(s.st)
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		fn(emptyState())
		return
	}
	fn(s.st)
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
	if err := s.loadLocked(); err != nil {
		s.mu.Unlock()
		return DraftEvent{}, err
	}
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

	s.commitLocked(next)
	s.scheduleFlushLocked()
	ev := draftEventOf(next)
	s.mu.Unlock()

	s.broker.Publish(Event{Type: EventDraft, Data: ev})
	return ev, nil
}

// EditDraftBatch атомарно меняет несколько секций черновика и, при live != nil,
// живые данные слоя под одной проверкой ревизии: один файл, одна запись,
// ревизия +1, одно событие draft. Значение nil или JSON null удаляет секцию.
// Конфликт ревизии, недопустимое имя секции, невалидный JSON или ошибка
// live-функции — ничего не записано, состояние и файл прежние, события нет.
// live получает глубокую копию Live и меняет её на месте.
func (s *Store) EditDraftBatch(baseRev int64, sections map[string]json.RawMessage, live func(live Sections) error) (DraftEvent, error) {
	for name := range sections {
		if !sectionNameRE.MatchString(name) {
			return DraftEvent{}, fmt.Errorf("%w: %q", ErrInvalidSection, name)
		}
	}

	s.mu.Lock()
	if err := s.loadLocked(); err != nil {
		s.mu.Unlock()
		return DraftEvent{}, err
	}
	if baseRev != s.st.DraftRevision {
		s.mu.Unlock()
		return DraftEvent{}, ErrDraftConflict
	}

	next := s.st.clone()
	for name, value := range sections {
		if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			delete(next.Draft, name)
			continue
		}
		compact, err := compactRaw(value)
		if err != nil {
			s.mu.Unlock()
			return DraftEvent{}, fmt.Errorf("configlayer: section %q value is not valid JSON: %w", name, err)
		}
		next.Draft[name] = compact
	}
	if live != nil {
		if next.Live == nil {
			next.Live = Sections{}
		}
		if err := live(next.Live); err != nil {
			s.mu.Unlock()
			return DraftEvent{}, err
		}
	}
	next.DraftRevision++

	s.commitLocked(next)
	s.scheduleFlushLocked()
	ev := draftEventOf(next)
	s.mu.Unlock()

	s.broker.Publish(Event{Type: EventDraft, Data: ev})
	return ev, nil
}

// ResetDraft сбрасывает черновик к применённому состоянию, если baseRev
// совпадает с текущей ревизией. Ревизия растёт, публикуется событие draft.
func (s *Store) ResetDraft(baseRev int64) (DraftEvent, error) {
	s.mu.Lock()
	if err := s.loadLocked(); err != nil {
		s.mu.Unlock()
		return DraftEvent{}, err
	}
	if baseRev != s.st.DraftRevision {
		s.mu.Unlock()
		return DraftEvent{}, ErrDraftConflict
	}

	next := s.st.clone()
	next.Draft = next.Applied.Clone()
	next.DraftRevision++

	s.commitLocked(next)
	s.scheduleFlushLocked()
	ev := draftEventOf(next)
	s.mu.Unlock()

	s.broker.Publish(Event{Type: EventDraft, Data: ev})
	return ev, nil
}

// Update применяет fn к глубокой копии состояния и сохраняет копию. Если fn
// вернула ошибку или запись на диск не удалась, состояние в памяти и файл не
// меняются. Событий Update не публикует: их рассылает вызывающий конвейер.
func (s *Store) Update(fn func(st *State) error) error {
	// Синхронная запись берёт wmu под mu, а фоновая держит wmu всю запись (на
	// флешке десятки секунд): дождаться её без mu, иначе правки и чтения встанут
	// за этой записью. Окно остаётся только для правки, пришедшей после ожидания.
	s.Flush()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}

	next := s.st.clone()
	if err := fn(&next); err != nil {
		return err
	}
	next.normalize()
	if err := s.persist(next); err != nil {
		return err
	}
	s.commitLocked(next)
	return nil
}

// DismissNotice закрывает уведомление и сохраняет состояние. Допустимые id:
// "schema_reset", "recovered_from_journal" и "journal_recovery_failed".
func (s *Store) DismissNotice(id string) error {
	s.Flush() // без mu: см. Update
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}

	next := s.st.clone()
	switch id {
	case "schema_reset":
		next.Notices.SchemaReset = false
		next.Notices.SchemaResetBackup = ""
	case "recovered_from_journal":
		next.Notices.RecoveredFromJournal = false
	case "journal_recovery_failed":
		next.Notices.JournalRecoveryFailed = false
	default:
		return fmt.Errorf("%w: %q", ErrUnknownNotice, id)
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.commitLocked(next)
	return nil
}

// ReplaceExternally выполняет fn, которая подменяет файл состояния снаружи
// (восстановление снимка панели), удерживая замок хранилища, и затем перечитывает
// файл. Правки черновика, закрытие уведомлений и записи конвейера в это время
// ждут, поэтому не успевают переписать восстановленный файл состоянием из памяти
// (WR-10). Перечитывание идёт и после ошибки fn: файл мог измениться частично.
// Возвращает ошибку fn и ошибку перечитывания по отдельности. fn не должна
// обращаться к самому хранилищу (замок не реентерабелен).
func (s *Store) ReplaceExternally(fn func() error) (fnErr, reloadErr error) {
	s.Flush() // без mu: см. Update
	s.mu.Lock()
	defer s.mu.Unlock()
	// Фоновая запись черновика не должна лечь поверх подменённого файла: wmu
	// держится, пока идёт fn, а незаписанные версии отбрасываются.
	s.wmu.Lock()
	fnErr = fn()
	s.written.Store(s.ver)
	s.wmu.Unlock()
	st, err := s.readFile()
	if err != nil {
		// Загруженное состояние больше не соответствует файлу: сбрасываем кэш,
		// следующее обращение прочитает файл заново.
		s.st, s.loaded = State{}, false
		return fnErr, err
	}
	s.commitLocked(st)
	s.loaded = true
	s.markWrittenLocked()
	return fnErr, nil
}
