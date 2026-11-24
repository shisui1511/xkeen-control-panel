package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Типы удержания лимитов трафика — интерфейс между службой лимитов (заполняет) и арбитром
// маршрутизации (читает: квота > перехват > расписание). Удерживают выбор только включённые
// лимиты с действием block или direct; лимит с действием notify выбор группы не меняет.

// LimitHold — исчерпанный лимит, который держит выбор: группа переведена на блокировку
// (block) или на прямое соединение (direct) до сброса счётчика.
type LimitHold struct {
	// LimitID — стабильный ID лимита.
	LimitID string `json:"limit_id"`
	// Name — название лимита для строки состояния и подсказки плитки.
	Name string `json:"name"`
	// Action — действие при исчерпании: "block" или "direct".
	Action string `json:"action"`
	// ResetAt — момент сброса счётчика (Unix, секунды).
	ResetAt int64 `json:"reset_at"`
}

// LimitHolds — снимок удержаний лимитов на момент запроса.
type LimitHolds struct {
	// Groups — исчерпанные включённые лимиты групп с действием block/direct
	// по ID ручной группы. Группы «авто» и «резерв» сюда не попадают: узел выбирает ядро.
	Groups map[string]LimitHold
	// All — исчерпанный включённый общий лимит («весь трафик») с block/direct; nil — нет такого.
	All *LimitHold
	// Tail — ID ручных групп, на которые действует включённый лимит block/direct,
	// исчерпан он или нет.
	Tail map[string]bool
	// TailAll — есть включённый общий лимит block/direct, исчерпан он или нет.
	//
	// Tail и TailAll читает сверка служебного хвоста арбитра: при расхождении со снимком
	// hidden_tail в применённом состоянии она пересобирает профиль Mihomo из применённого
	// состояния — без «Применить» и без черновика. Группе только из провайдера с узлом
	// по умолчанию из провайдера служебный хвост REJECT/DIRECT пишется, только пока на неё
	// действует такой лимит (Tail[id] или TailAll); остальным ручным группам хвост
	// пишется всегда.
	TailAll bool
}

// Служба лимитов трафика нейтральна к ядру (D-03): счётчик один на лимит, хранится в
// собственном файле в корне DataDir (каталоги traffic/ и smartproxy/ уходят в архив
// старых данных, D-16) и продолжается с того же значения при смене активного ядра.
// Учёт (поток Mihomo, счётчики Xray) прибавляет байты через AddUsage.

const (
	// limitsFileName — файл лимитов в корне DataDir.
	limitsFileName = "traffic-limits.json"
	// maxLimits — предел числа лимитов.
	maxLimits = 32
	// maxLimitNameRunes — предел длины названия лимита в символах.
	maxLimitNameRunes = 64
	// limitsSaveThrottle — не чаще одной записи счётчиков на флешку за период.
	limitsSaveThrottle = time.Minute
	// maxLimitAlerts — предел перечня оповещений в памяти.
	maxLimitAlerts = 50
	// maxLimitBytes — верхняя граница лимита (1 ПиБ).
	maxLimitBytes = int64(1) << 50
	// defaultLimitThreshold — порог предупреждения по умолчанию, проценты.
	defaultLimitThreshold = 80
	// limitsLoopInterval — период цикла сбросов и записи счётчиков.
	limitsLoopInterval = time.Minute
	// limitsMinClockYear — сбросы периодов не применяются, пока часы роутера
	// не синхронизированы (год меньше этого).
	limitsMinClockYear = 2025
)

// Действия лимита при исчерпании (D-15).
const (
	LimitActionNotify = "notify"
	LimitActionBlock  = "block"
	LimitActionDirect = "direct"
)

// Цели лимита (D-12): группа панели по стабильному ID или весь трафик.
const (
	LimitTargetGroup = "group"
	LimitTargetAll   = "all"
)

// Периоды лимита.
const (
	limitPeriodDaily   = "daily"
	limitPeriodWeekly  = "weekly"
	limitPeriodMonthly = "monthly"
)

// Режимы групп, как в routing.GroupMode (пакет routing импортирует services, обратная
// зависимость невозможна).
const (
	limitGroupModeManual   = "manual"
	limitGroupModeAuto     = "auto"
	limitGroupModeFallback = "fallback"
)

// Состояния лимита в списке.
const (
	LimitStateOK           = "ok"
	LimitStateThreshold    = "threshold"
	LimitStateExhausted    = "exhausted"
	LimitStateDisabled     = "disabled"
	LimitStateGroupDeleted = "group_deleted"
)

// Состояния данных учёта.
const (
	LimitDataOK         = "ok"
	LimitDataNoSocket   = "no_socket"
	LimitDataNoCounters = "no_counters"
	LimitDataStopped    = "stopped"
)

// Виды оповещений лимита.
const (
	limitAlertThreshold = "threshold"
	limitAlertExceeded  = "exceeded"
)

// limitIDRe — стабильный ID лимита и ID группы панели: 8 шестнадцатеричных символов.
var limitIDRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ErrLimitNotFound — лимита с таким ID нет.
var ErrLimitNotFound = errors.New("limit not found")

// LimitInputError — отказ валидации поля лимита: Field — имя поля в JSON, Reason —
// машиночитаемая причина (required, too_long, invalid, out_of_range, action_invalid,
// too_many).
type LimitInputError struct {
	Field  string
	Reason string
}

func (e *LimitInputError) Error() string {
	return "limit " + e.Field + ": " + e.Reason
}

// TrafficLimit — лимит трафика группы панели или всего трафика.
type TrafficLimit struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	LimitBytes int64  `json:"limit_bytes"`
	// Period — "daily" | "weekly" | "monthly".
	Period string `json:"period"`
	// Threshold — порог предупреждения, 1–99 %.
	Threshold int `json:"threshold"`
	// Action — "notify" | "block" | "direct".
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
	// UsedBytes — расход за текущий период (один счётчик на лимит при любом ядре).
	UsedBytes int64 `json:"used_bytes"`
	// LastReset — момент последнего сброса счётчика (Unix, секунды).
	LastReset        int64 `json:"last_reset"`
	AlertedThreshold bool  `json:"alerted_threshold"`
	AlertedExceeded  bool  `json:"alerted_exceeded"`
}

// LimitAlert — оповещение о достижении порога или исчерпании лимита.
type LimitAlert struct {
	LimitID   string `json:"limit_id"`
	LimitName string `json:"limit_name"`
	// Kind — "threshold" | "exceeded".
	Kind       string  `json:"kind"`
	UsedBytes  int64   `json:"used_bytes"`
	LimitBytes int64   `json:"limit_bytes"`
	Percent    float64 `json:"percent"`
	Timestamp  int64   `json:"timestamp"`
}

// LimitView — лимит в списке с вычисленными полями.
type LimitView struct {
	TrafficLimit
	// ResetAt — начало следующего периода по часам роутера (Unix, секунды).
	ResetAt int64 `json:"reset_at"`
	// State — ok | threshold | exhausted | disabled | group_deleted.
	State string `json:"state"`
	// TargetName / TargetMode — имя и режим группы-цели; пусто для общего лимита.
	TargetName string `json:"target_name"`
	TargetMode string `json:"target_mode"`
	// Approx — расход приблизительный (Xray: узел входит в несколько групп).
	Approx bool `json:"approx"`
	// Holding — ID групп, которые лимит сейчас держит.
	Holding []string `json:"holding"`
}

// LimitGroup — группа панели, доступная как цель лимита (поле «Цель» формы).
type LimitGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Mode — "manual" | "auto" | "fallback".
	Mode string `json:"mode"`
}

// LimitsView — ответ списка лимитов.
type LimitsView struct {
	Limits []LimitView `json:"limits"`
	// Groups — группы применённого состояния маршрутизации по имени (те же, по которым
	// Save проверяет цель); пусто, пока цели учёта неизвестны.
	Groups []LimitGroup `json:"groups"`
	// Data — состояние учёта: ok | no_socket | no_counters | stopped.
	Data string `json:"data"`
	// Kernel — активное ядро ("xray" | "mihomo" | "" — ни одного или конфликт).
	Kernel string `json:"kernel"`
	Now    int64  `json:"now"`
}

// GroupTarget — группа панели как цель учёта.
type GroupTarget struct {
	Name string
	// Mode — "manual" | "auto" | "fallback".
	Mode string
	// MihomoNames — имена групп в профиле Mihomo, чей трафик считается группе.
	MihomoNames []string
	// XrayTags — теги исходящих Xray, чьи счётчики считаются группе.
	XrayTags []string
	// Approx — расход приблизительный (узел входит в несколько групп).
	Approx bool
	// Direct — группа сейчас на DIRECT/REJECT: учёт её не копит.
	Direct bool
}

// TrafficTargets — цели учёта из применённого состояния маршрутизации.
type TrafficTargets struct {
	// Groups — группы по стабильному ID.
	Groups         map[string]GroupTarget
	XrayManaged    bool
	MihomoManaged  bool
	XrayGlobalTags []string
}

// limitsStore — содержимое traffic-limits.json.
type limitsStore struct {
	Limits []TrafficLimit `json:"limits"`
}

// LimitsService — хранение лимитов, CRUD, сбросы периодов, оповещения и удержания
// для арбитра маршрутизации. Создаётся только в cmd/xcp/main.go.
type LimitsService struct {
	mu        sync.RWMutex
	path      string
	limits    []TrafficLimit
	alerts    []LimitAlert
	dataState string
	dirty     bool
	lastSave  time.Time

	targets       func() (TrafficTargets, error)
	onHolds       func()
	activeKernelF func() string
	// mihomoStateF — состояние потока /connections Mihomo (SetMihomoState).
	mihomoStateF func() string
	// obsTargets, obsTargetsAt — цели учёта для частого приёма дельт Mihomo:
	// снимки идут раз в секунду, цели читаются не чаще limitsTargetsTTL.
	obsTargets   TrafficTargets
	obsTargetsAt time.Time
	// xraySrc — источник клиента gRPC Xray для опроса счётчиков (SetXrayCounters).
	xraySrc XrayCounterSource

	stopCh chan struct{}
	done   chan struct{}
	// xrayDone закрывает цикл опроса счётчиков Xray.
	xrayDone chan struct{}
	started  bool
	stopOnce sync.Once
}

// NewLimitsService загружает лимиты из <dataDir>/traffic-limits.json. Отсутствующий
// файл — пустой список; повреждённый — пустой список и строка журнала без содержимого.
func NewLimitsService(dataDir string) *LimitsService {
	s := &LimitsService{
		dataState: LimitDataOK,
		stopCh:    make(chan struct{}),
		done:      make(chan struct{}),
		xrayDone:  make(chan struct{}),
	}
	path := filepath.Join(dataDir, limitsFileName)
	clean, err := utils.NewPathValidator([]string{dataDir}).ValidateUnder(path, dataDir)
	if err != nil {
		log.Printf("limits: путь файла лимитов не прошёл проверку, лимиты не сохраняются")
	} else {
		s.path = clean
	}
	s.load()
	return s
}

// load читает файл лимитов. Записи с недопустимыми полями пропускаются.
func (s *LimitsService) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("limits: файл лимитов не прочитан, список пуст")
		}
		return
	}
	var store limitsStore
	if err := json.Unmarshal(data, &store); err != nil {
		log.Printf("limits: файл лимитов повреждён, список пуст")
		return
	}
	seen := make(map[string]bool, len(store.Limits))
	skipped := 0
	for _, l := range store.Limits {
		if len(s.limits) >= maxLimits || !limitIDRe.MatchString(l.ID) || seen[l.ID] {
			skipped++
			continue
		}
		if err := normalizeLimit(&l); err != nil {
			skipped++
			continue
		}
		if l.UsedBytes < 0 {
			l.UsedBytes = 0
		}
		seen[l.ID] = true
		s.limits = append(s.limits, l)
	}
	if skipped > 0 {
		log.Printf("limits: пропущено недопустимых записей: %d", skipped)
	}
}

// Start запускает цикл раз в минуту (сбросы периодов и запись счётчиков с
// троттлингом) и цикл опроса счётчиков Xray (xrayPollInterval, только при активном
// Xray под управлением панели).
func (s *LimitsService) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop()
	go s.xrayLoop()
}

func (s *LimitsService) loop() {
	defer close(s.done)
	t := time.NewTicker(limitsLoopInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if s.checkResets(time.Now()) {
				s.notifyHoldsChanged()
			}
			s.flush(false)
		}
	}
}

// Stop останавливает цикл и записывает несохранённые счётчики.
func (s *LimitsService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.mu.RLock()
		started := s.started
		s.mu.RUnlock()
		if started {
			<-s.done
			<-s.xrayDone
		}
		s.flush(true)
	})
}

// SetTargets задаёт источник целей учёта (применённое состояние маршрутизации).
func (s *LimitsService) SetTargets(fn func() (TrafficTargets, error)) {
	s.mu.Lock()
	s.targets = fn
	s.mu.Unlock()
}

// SetOnHoldsChanged задаёт колбэк смены удержаний (Kick арбитра). Колбэк вызывается
// вне мьютекса службы и не должен блокировать.
func (s *LimitsService) SetOnHoldsChanged(fn func()) {
	s.mu.Lock()
	s.onHolds = fn
	s.mu.Unlock()
}

// SetActiveKernel задаёт источник активного ядра ("" — ни одного или конфликт).
func (s *LimitsService) SetActiveKernel(fn func() string) {
	s.mu.Lock()
	s.activeKernelF = fn
	s.mu.Unlock()
}

// setDataState запоминает состояние учёта (ok, no_socket, no_counters, stopped).
func (s *LimitsService) setDataState(state string) {
	s.mu.Lock()
	s.dataState = state
	s.mu.Unlock()
}

func (s *LimitsService) activeKernel() string {
	s.mu.RLock()
	fn := s.activeKernelF
	s.mu.RUnlock()
	if fn == nil {
		return ""
	}
	return fn()
}

// readTargets читает цели учёта вне мьютекса службы. ok=false — цели неизвестны
// (источник не задан или вернул ошибку).
func (s *LimitsService) readTargets() (TrafficTargets, bool) {
	s.mu.RLock()
	fn := s.targets
	s.mu.RUnlock()
	if fn == nil {
		return TrafficTargets{}, false
	}
	t, err := fn()
	if err != nil {
		return TrafficTargets{}, false
	}
	return t, true
}

// notifyHoldsChanged будит арбитр после изменения, которое может поменять Holds()
// или Tail/TailAll. Вызывается только после снятия мьютекса.
func (s *LimitsService) notifyHoldsChanged() {
	s.mu.RLock()
	fn := s.onHolds
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// normalizeLimit проверяет поля лимита без учёта целей и подставляет значения по
// умолчанию (порог 80 %). Действия throttle, log_only и любые другие, кроме notify,
// block и direct, отклоняются (D-15).
func normalizeLimit(l *TrafficLimit) error {
	l.Name = strings.TrimSpace(l.Name)
	switch {
	case l.Name == "":
		return &LimitInputError{Field: "name", Reason: "required"}
	case !utf8.ValidString(l.Name) || strings.ContainsFunc(l.Name, unicode.IsControl):
		return &LimitInputError{Field: "name", Reason: "invalid"}
	case utf8.RuneCountInString(l.Name) > maxLimitNameRunes:
		return &LimitInputError{Field: "name", Reason: "too_long"}
	}
	if l.LimitBytes <= 0 || l.LimitBytes > maxLimitBytes {
		return &LimitInputError{Field: "limit_bytes", Reason: "out_of_range"}
	}
	switch l.Period {
	case limitPeriodDaily, limitPeriodWeekly, limitPeriodMonthly:
	default:
		return &LimitInputError{Field: "period", Reason: "invalid"}
	}
	if l.Threshold == 0 {
		l.Threshold = defaultLimitThreshold
	}
	if l.Threshold < 1 || l.Threshold > 99 {
		return &LimitInputError{Field: "threshold", Reason: "out_of_range"}
	}
	switch l.Action {
	case LimitActionNotify, LimitActionBlock, LimitActionDirect:
	default:
		return &LimitInputError{Field: "action", Reason: "action_invalid"}
	}
	switch l.TargetType {
	case LimitTargetGroup:
		if !limitIDRe.MatchString(l.TargetID) {
			return &LimitInputError{Field: "target_id", Reason: "invalid"}
		}
	case LimitTargetAll:
		l.TargetID = ""
	default:
		return &LimitInputError{Field: "target_type", Reason: "invalid"}
	}
	return nil
}

// holdingAction — действие лимита держит выбор группы (block или direct).
func holdingAction(action string) bool {
	return action == LimitActionBlock || action == LimitActionDirect
}

// newLimitID — случайный ID из 8 шестнадцатеричных символов, не занятый в list.
func newLimitID(list []TrafficLimit) (string, error) {
	for range 16 {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b[:])
		if !slices.ContainsFunc(list, func(l TrafficLimit) bool { return l.ID == id }) {
			return id, nil
		}
	}
	return "", errors.New("limit id: no free id")
}

func (s *LimitsService) indexLocked(id string) int {
	return slices.IndexFunc(s.limits, func(l TrafficLimit) bool { return l.ID == id })
}

// reached — расход достиг доли pct процентов лимита.
func reached(used, limit int64, pct int) bool {
	if limit <= 0 {
		return false
	}
	if pct >= 100 {
		return used >= limit
	}
	// В float64: used*100 в int64 переполнится раньше, чем счётчик упрётся в предел.
	return float64(used)*100 >= float64(limit)*float64(pct)
}

// Save создаёт лимит (пустой ID) или правит существующий. Правка сохраняет счётчик,
// кроме смены цели или периода — тогда счётчик обнуляется.
func (s *LimitsService) Save(l TrafficLimit) (TrafficLimit, error) {
	if l.ID != "" && !limitIDRe.MatchString(l.ID) {
		return TrafficLimit{}, ErrLimitNotFound
	}
	if err := normalizeLimit(&l); err != nil {
		return TrafficLimit{}, err
	}
	// D-13: у групп «авто» и «резерв» узел выбирает ядро — допустимо только «уведомить».
	if l.TargetType == LimitTargetGroup && l.Action != LimitActionNotify {
		if t, ok := s.readTargets(); ok {
			if g, found := t.Groups[l.TargetID]; found && (g.Mode == limitGroupModeAuto || g.Mode == limitGroupModeFallback) {
				return TrafficLimit{}, &LimitInputError{Field: "action", Reason: "action_invalid"}
			}
		}
	}

	now := time.Now().Unix()
	s.mu.Lock()
	prev := slices.Clone(s.limits)
	if l.ID == "" {
		if len(s.limits) >= maxLimits {
			s.mu.Unlock()
			return TrafficLimit{}, &LimitInputError{Field: "limits", Reason: "too_many"}
		}
		id, err := newLimitID(s.limits)
		if err != nil {
			s.mu.Unlock()
			return TrafficLimit{}, err
		}
		l.ID = id
		l.UsedBytes = 0
		l.LastReset = now
		l.AlertedThreshold = false
		l.AlertedExceeded = false
		s.limits = append(s.limits, l)
	} else {
		i := s.indexLocked(l.ID)
		if i < 0 {
			s.mu.Unlock()
			return TrafficLimit{}, ErrLimitNotFound
		}
		old := s.limits[i]
		if old.TargetType != l.TargetType || old.TargetID != l.TargetID || old.Period != l.Period {
			l.UsedBytes = 0
			l.LastReset = now
			l.AlertedThreshold = false
			l.AlertedExceeded = false
		} else {
			l.UsedBytes = old.UsedBytes
			l.LastReset = old.LastReset
			l.AlertedExceeded = old.AlertedExceeded && reached(l.UsedBytes, l.LimitBytes, 100)
			l.AlertedThreshold = old.AlertedThreshold && reached(l.UsedBytes, l.LimitBytes, l.Threshold)
		}
		s.limits[i] = l
	}
	if err := s.writeLocked(utils.AtomicWriteFile); err != nil {
		s.limits = prev
		s.mu.Unlock()
		log.Printf("limits: запись лимита %s не удалась", l.ID)
		return TrafficLimit{}, err
	}
	s.mu.Unlock()
	s.notifyHoldsChanged()
	return l, nil
}

// mutate применяет fn к лимиту id и записывает файл; при ошибке записи откатывает.
func (s *LimitsService) mutate(id string, fn func(i int)) error {
	if !limitIDRe.MatchString(id) {
		return ErrLimitNotFound
	}
	s.mu.Lock()
	i := s.indexLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return ErrLimitNotFound
	}
	prev := slices.Clone(s.limits)
	fn(i)
	if err := s.writeLocked(utils.AtomicWriteFile); err != nil {
		s.limits = prev
		s.mu.Unlock()
		log.Printf("limits: запись лимита %s не удалась", id)
		return err
	}
	s.mu.Unlock()
	return nil
}

// Delete удаляет лимит.
func (s *LimitsService) Delete(id string) error {
	err := s.mutate(id, func(i int) {
		s.limits = slices.Delete(s.limits, i, i+1)
	})
	if err != nil {
		return err
	}
	s.notifyHoldsChanged()
	return nil
}

// SetEnabled включает или выключает лимит.
func (s *LimitsService) SetEnabled(id string, enabled bool) error {
	err := s.mutate(id, func(i int) {
		s.limits[i].Enabled = enabled
	})
	if err != nil {
		return err
	}
	s.notifyHoldsChanged()
	return nil
}

// Reset обнуляет счётчик лимита («Сбросить счётчик»): арбитр сразу снимает удержание.
func (s *LimitsService) Reset(id string) error {
	now := time.Now().Unix()
	err := s.mutate(id, func(i int) {
		l := &s.limits[i]
		l.UsedBytes = 0
		l.LastReset = now
		l.AlertedThreshold = false
		l.AlertedExceeded = false
	})
	if err != nil {
		return err
	}
	s.notifyHoldsChanged()
	return nil
}

// Alerts возвращает оповещения, новые в конце.
func (s *LimitsService) Alerts() []LimitAlert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LimitAlert, len(s.alerts))
	copy(out, s.alerts)
	return out
}

// ClearAlerts очищает перечень оповещений.
func (s *LimitsService) ClearAlerts() {
	s.mu.Lock()
	s.alerts = nil
	s.mu.Unlock()
}

// nextReset — начало следующего периода по часам роутера.
func nextReset(period string, now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case limitPeriodDaily:
		return day.AddDate(0, 0, 1)
	case limitPeriodWeekly:
		offset := (int(now.Weekday()) - int(time.Monday) + 7) % 7
		return day.AddDate(0, 0, 7-offset)
	default:
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	}
}

// periodStart — начало периода, в который попадает t (неделя — с понедельника).
func periodStart(period string, t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	switch period {
	case limitPeriodDaily:
		return day
	case limitPeriodWeekly:
		offset := (int(t.Weekday()) - int(time.Monday) + 7) % 7
		return day.AddDate(0, 0, -offset)
	default:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	}
}

// holdsLocked считает удержания. known=false — цели неизвестны: группа-цель считается
// ручной (арбитр всё равно читает удержания только ручных групп). Caller holds s.mu.
func (s *LimitsService) holdsLocked(t TrafficTargets, known bool, now time.Time) LimitHolds {
	h := LimitHolds{Groups: map[string]LimitHold{}, Tail: map[string]bool{}}
	var all []TrafficLimit
	for _, l := range s.limits {
		if !l.Enabled || !holdingAction(l.Action) {
			continue
		}
		if l.TargetType == LimitTargetAll {
			h.TailAll = true
			if reached(l.UsedBytes, l.LimitBytes, 100) {
				all = append(all, l)
			}
			continue
		}
		if known {
			g, ok := t.Groups[l.TargetID]
			if !ok || g.Mode != limitGroupModeManual {
				continue
			}
		}
		h.Tail[l.TargetID] = true
		if !reached(l.UsedBytes, l.LimitBytes, 100) {
			continue
		}
		hold := LimitHold{LimitID: l.ID, Name: l.Name, Action: l.Action, ResetAt: nextReset(l.Period, now).Unix()}
		if cur, ok := h.Groups[l.TargetID]; !ok || limitLess(hold.Name, hold.LimitID, cur.Name, cur.LimitID) {
			h.Groups[l.TargetID] = hold
		}
	}
	if len(all) > 0 {
		sort.Slice(all, func(i, j int) bool { return limitLess(all[i].Name, all[i].ID, all[j].Name, all[j].ID) })
		first := all[0]
		h.All = &LimitHold{LimitID: first.ID, Name: first.Name, Action: first.Action, ResetAt: nextReset(first.Period, now).Unix()}
	}
	return h
}

// limitLess — порядок лимитов по имени без учёта регистра, затем по ID.
func limitLess(nameA, idA, nameB, idB string) bool {
	a, b := strings.ToLower(nameA), strings.ToLower(nameB)
	if a != b {
		return a < b
	}
	return idA < idB
}

// Holds возвращает удержания для арбитра: исчерпанные включённые лимиты с block/direct
// (Groups — по ID ручной группы, All — общий, первый по имени) и служебный хвост
// (Tail/TailAll — включённые лимиты block/direct независимо от исчерпания).
func (s *LimitsService) Holds() LimitHolds {
	t, known := s.readTargets()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.holdsLocked(t, known, time.Now())
}

// holdKeyLocked — отпечаток множества удержаний (исчерпанные включённые лимиты
// block/direct). Caller holds s.mu.
func (s *LimitsService) holdKeyLocked() string {
	var ids []string
	for _, l := range s.limits {
		if l.Enabled && holdingAction(l.Action) && reached(l.UsedBytes, l.LimitBytes, 100) {
			ids = append(ids, l.ID)
		}
	}
	return strings.Join(ids, ",")
}

// List возвращает лимиты с состояниями: исчерпанные, достигшие порога, затем по имени.
func (s *LimitsService) List() LimitsView {
	t, known := s.readTargets()
	kernel := s.activeKernel()
	now := time.Now()

	s.mu.RLock()
	holds := s.holdsLocked(t, known, now)
	data := s.dataState
	views := make([]LimitView, 0, len(s.limits))
	for _, l := range s.limits {
		v := LimitView{TrafficLimit: l, ResetAt: nextReset(l.Period, now).Unix(), Holding: []string{}}
		var g GroupTarget
		groupKnown := false
		if l.TargetType == LimitTargetGroup && known {
			g, groupKnown = t.Groups[l.TargetID]
			if groupKnown {
				v.TargetName = g.Name
				v.TargetMode = g.Mode
				v.Approx = g.Approx && kernel == "xray"
			}
		}
		switch {
		case !l.Enabled:
			v.State = LimitStateDisabled
		case l.TargetType == LimitTargetGroup && known && !groupKnown:
			v.State = LimitStateGroupDeleted
		case reached(l.UsedBytes, l.LimitBytes, 100):
			v.State = LimitStateExhausted
		case reached(l.UsedBytes, l.LimitBytes, l.Threshold):
			v.State = LimitStateThreshold
		default:
			v.State = LimitStateOK
		}
		if l.TargetType == LimitTargetGroup {
			if h, ok := holds.Groups[l.TargetID]; ok && h.LimitID == l.ID {
				v.Holding = append(v.Holding, l.TargetID)
			}
		} else if holds.All != nil && holds.All.LimitID == l.ID && known {
			for id, gt := range t.Groups {
				if _, own := holds.Groups[id]; !own && gt.Mode == limitGroupModeManual {
					v.Holding = append(v.Holding, id)
				}
			}
			sort.Strings(v.Holding)
		}
		views = append(views, v)
	}
	s.mu.RUnlock()

	rank := func(state string) int {
		switch state {
		case LimitStateExhausted:
			return 0
		case LimitStateThreshold:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(views, func(i, j int) bool {
		ri, rj := rank(views[i].State), rank(views[j].State)
		if ri != rj {
			return ri < rj
		}
		return limitLess(views[i].Name, views[i].ID, views[j].Name, views[j].ID)
	})
	// Состояние данных по активному ядру: Mihomo — поток /connections, Xray —
	// последний опрос счётчиков (dataState), ни одного — stopped.
	switch kernel {
	case "mihomo":
		data = s.mihomoDataState()
	case "xray":
	default:
		data = LimitDataStopped
	}
	groups := make([]LimitGroup, 0, len(t.Groups))
	if known {
		for id, g := range t.Groups {
			groups = append(groups, LimitGroup{ID: id, Name: g.Name, Mode: g.Mode})
		}
		sort.Slice(groups, func(i, j int) bool {
			return limitLess(groups[i].Name, groups[i].ID, groups[j].Name, groups[j].ID)
		})
	}
	return LimitsView{Limits: views, Groups: groups, Data: data, Kernel: kernel, Now: now.Unix()}
}

// AddUsage прибавляет байты включённым лимитам: общему — allBytes, лимиту группы —
// groupBytes[TargetID]. Порог и исчерпание оповещают один раз за период. Смена
// множества удержаний будит арбитр.
func (s *LimitsService) AddUsage(groupBytes map[string]int64, allBytes int64) {
	s.setDataState(LimitDataOK)
	now := time.Now()
	s.mu.Lock()
	before := s.holdKeyLocked()
	for i := range s.limits {
		l := &s.limits[i]
		if !l.Enabled {
			continue
		}
		var add int64
		if l.TargetType == LimitTargetAll {
			add = allBytes
		} else {
			add = groupBytes[l.TargetID]
		}
		if add <= 0 {
			continue
		}
		l.UsedBytes += add
		s.dirty = true
		switch {
		case !l.AlertedExceeded && reached(l.UsedBytes, l.LimitBytes, 100):
			l.AlertedExceeded = true
			l.AlertedThreshold = true
			s.addAlertLocked(l, limitAlertExceeded, now)
		case !l.AlertedThreshold && !l.AlertedExceeded && reached(l.UsedBytes, l.LimitBytes, l.Threshold):
			l.AlertedThreshold = true
			s.addAlertLocked(l, limitAlertThreshold, now)
		}
	}
	changed := s.holdKeyLocked() != before
	s.mu.Unlock()
	if changed {
		s.notifyHoldsChanged()
	}
}

// addAlertLocked добавляет оповещение; перечень не длиннее maxLimitAlerts. Caller holds s.mu.
func (s *LimitsService) addAlertLocked(l *TrafficLimit, kind string, now time.Time) {
	if len(s.alerts) >= maxLimitAlerts {
		s.alerts = slices.Delete(s.alerts, 0, len(s.alerts)-maxLimitAlerts+1)
	}
	pct := 0.0
	if l.LimitBytes > 0 {
		pct = float64(l.UsedBytes) * 100 / float64(l.LimitBytes)
	}
	s.alerts = append(s.alerts, LimitAlert{
		LimitID:    l.ID,
		LimitName:  l.Name,
		Kind:       kind,
		UsedBytes:  l.UsedBytes,
		LimitBytes: l.LimitBytes,
		Percent:    pct,
		Timestamp:  now.Unix(),
	})
	log.Printf("limits: лимит %s: %s, %d из %d байт", l.ID, kind, l.UsedBytes, l.LimitBytes)
}

// checkResets обнуляет счётчики лимитов, чей период сменился (неделя — с понедельника).
// Пока часы роутера не синхронизированы (год < 2025), сбросы не применяются. Возвращает
// true, если какой-то счётчик сброшен.
func (s *LimitsService) checkResets(now time.Time) bool {
	if now.Year() < limitsMinClockYear {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.limits {
		l := &s.limits[i]
		if periodStart(l.Period, time.Unix(l.LastReset, 0)).Equal(periodStart(l.Period, now)) {
			continue
		}
		l.UsedBytes = 0
		l.LastReset = now.Unix()
		l.AlertedThreshold = false
		l.AlertedExceeded = false
		changed = true
	}
	if changed {
		s.dirty = true
	}
	return changed
}

// flush записывает счётчики, если они менялись: периодически — не чаще
// limitsSaveThrottle, при остановке (force) — сразу. Без fsync: под идущей записью на
// флешку fsync ждёт десятки секунд; подмена файла остаётся атомарной, потеря последнего
// снимка счётчиков при обрыве питания допустима.
func (s *LimitsService) flush(force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	if !force && !s.lastSave.IsZero() && time.Since(s.lastSave) < limitsSaveThrottle {
		return
	}
	if err := s.writeLocked(utils.AtomicWriteFileNoSync); err != nil {
		log.Printf("limits: запись счётчиков не удалась")
	}
}

// writeLocked сериализует лимиты и пишет файл функцией write. Caller holds s.mu.
func (s *LimitsService) writeLocked(write func(path string, data []byte, perm os.FileMode) error) error {
	if s.path == "" {
		s.dirty = false
		return nil
	}
	list := s.limits
	if list == nil {
		list = []TrafficLimit{}
	}
	data, err := json.MarshalIndent(limitsStore{Limits: list}, "", "  ")
	if err != nil {
		return err
	}
	if err := write(s.path, data, 0600); err != nil {
		return err
	}
	s.dirty = false
	s.lastSave = time.Now()
	return nil
}
