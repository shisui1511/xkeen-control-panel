package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const (
	updateCheckInterval = 6 * time.Hour
	updateTickInterval  = 15 * time.Minute
	updateFirstCheck    = 2 * time.Minute
	autoUpdateFile      = "update-auto.json"
)

// AutoInstallRecord — попытка автоустановки: From → Version.
type AutoInstallRecord struct {
	Version   string `json:"version"`
	From      string `json:"from"`
	StartedAt int64  `json:"started_at"`
	OK        bool   `json:"ok"`
	DoneAt    int64  `json:"done_at,omitempty"`
}

// autoUpdateJournal переживает перезапуск панели: после автоустановки новый
// процесс узнаёт по Pending, удалась ли она, а неудачная версия больше не ставится.
type autoUpdateJournal struct {
	Pending       *AutoInstallRecord `json:"pending,omitempty"`
	Last          *AutoInstallRecord `json:"last,omitempty"`
	FailedVersion string             `json:"failed_version,omitempty"`
}

// UpdateCheckState — результат последней проверки и настройки автообновления
// (GET /api/update/state): по нему фронт показывает уведомления.
type UpdateCheckState struct {
	CheckedAt       int64              `json:"checked_at,omitempty"`
	Channel         string             `json:"channel"`
	CurrentVersion  string             `json:"current_version"`
	LatestVersion   string             `json:"latest_version,omitempty"`
	HasUpdate       bool               `json:"has_update"`
	Prerelease      bool               `json:"prerelease"`
	Error           string             `json:"error,omitempty"`
	AutoCheck       bool               `json:"auto_check"`
	AutoInstall     bool               `json:"auto_install"`
	InstallWindow   string             `json:"install_window"`
	FailedVersion   string             `json:"auto_failed_version,omitempty"`
	LastAutoInstall *AutoInstallRecord `json:"last_auto_install,omitempty"`
}

// UpdateScheduler — фоновая проверка обновлений и автоустановка в окно времени.
// Создаётся в main.go, останавливается через Stop().
type UpdateScheduler struct {
	api     *API
	now     func() time.Time
	version func() string
	check   func(channel, current string) (*UpdateInfo, error)
	install func(channel string) bool

	mu      sync.Mutex
	state   UpdateCheckState
	journal autoUpdateJournal

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func NewUpdateScheduler(api *API) *UpdateScheduler {
	return &UpdateScheduler{
		api: api,
		now: time.Now,
		version: func() string {
			if api.srv == nil {
				return ""
			}
			return strings.TrimPrefix(api.srv.GetVersion(), "v")
		},
		check:   checkLatestRelease,
		install: api.startUpdate,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func checkLatestRelease(channel, current string) (*UpdateInfo, error) {
	info, err := fetchLatestRelease(channel)
	if err != nil {
		return nil, err
	}
	info.HasUpdate = updateAvailable(info.LatestVersion, current)
	return info, nil
}

func (s *UpdateScheduler) journalPath() string {
	return filepath.Join(s.api.cfg.DataDir, autoUpdateFile)
}

// Start подводит итог автоустановки, прерванной перезапуском, и запускает цикл.
func (s *UpdateScheduler) Start() {
	s.mu.Lock()
	s.loadJournal()
	s.resolvePending(s.version())
	s.mu.Unlock()

	go s.loop()
}

func (s *UpdateScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
}

func (s *UpdateScheduler) loop() {
	defer close(s.done)
	first := time.NewTimer(updateFirstCheck)
	defer first.Stop()
	ticker := time.NewTicker(updateTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-first.C:
			s.tick()
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick: проверка раз в updateCheckInterval (или при смене канала), затем решение
// об автоустановке.
func (s *UpdateScheduler) tick() {
	cfg := s.api.cfg
	now := s.now()
	current := s.version()

	s.mu.Lock()
	// Автоустановка упала до перезапуска (скачивание, контрольная сумма)
	if s.journal.Pending != nil && getUpdateState().Status == "failed" {
		s.finishPending(false, now)
	}
	stale := s.state.CheckedAt == 0 ||
		s.state.Channel != cfg.UpdateChannel ||
		now.Sub(time.Unix(s.state.CheckedAt, 0)) >= updateCheckInterval
	s.mu.Unlock()

	if (cfg.UpdateAutoCheck || cfg.UpdateAutoInstall) && stale {
		info, err := s.check(cfg.UpdateChannel, current)
		s.Record(cfg.UpdateChannel, current, info, err)
	}

	if !cfg.UpdateAutoInstall || !inInstallWindow(now, cfg.UpdateInstallWindow) {
		return
	}
	s.mu.Lock()
	latest := s.state.LatestVersion
	ready := s.state.HasUpdate && s.state.Channel == cfg.UpdateChannel &&
		s.journal.Pending == nil && latest != s.journal.FailedVersion
	s.mu.Unlock()
	if !ready {
		return
	}

	s.mu.Lock()
	s.journal.Pending = &AutoInstallRecord{Version: latest, From: current, StartedAt: now.Unix()}
	s.saveJournal()
	s.mu.Unlock()

	if !s.install(cfg.UpdateChannel) {
		// Идёт ручное обновление или откат — попробуем в следующий тик
		s.mu.Lock()
		s.journal.Pending = nil
		s.saveJournal()
		s.mu.Unlock()
		return
	}
	log.Printf("Update: auto-install %s → %s started (window %s)", current, latest, cfg.UpdateInstallWindow)
}

// Record сохраняет результат проверки — фоновой или ручной (/api/update/check).
func (s *UpdateScheduler) Record(channel, current string, info *UpdateInfo, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.CheckedAt = s.now().Unix()
	s.state.Channel = channel
	s.state.CurrentVersion = current
	if err != nil {
		s.state.Error = err.Error()
		return
	}
	s.state.Error = ""
	s.state.LatestVersion = info.LatestVersion
	s.state.HasUpdate = info.HasUpdate
	s.state.Prerelease = info.Prerelease
}

// State — копия состояния с актуальными настройками из конфига.
func (s *UpdateScheduler) State() UpdateCheckState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	cfg := s.api.cfg
	if st.Channel == "" {
		st.Channel = cfg.UpdateChannel
	}
	if st.CurrentVersion == "" {
		st.CurrentVersion = s.version()
	}
	// Результат другого канала не показываем как доступное обновление
	if st.Channel != cfg.UpdateChannel {
		st.HasUpdate = false
	}
	st.AutoCheck = cfg.UpdateAutoCheck
	st.AutoInstall = cfg.UpdateAutoInstall
	st.InstallWindow = cfg.UpdateInstallWindow
	st.FailedVersion = s.journal.FailedVersion
	if s.journal.Last != nil {
		last := *s.journal.Last
		st.LastAutoInstall = &last
	}
	return st
}

// resolvePending: процесс поднялся после автоустановки. Всё ещё старая версия —
// сработал откат, версия помечается неудачной.
func (s *UpdateScheduler) resolvePending(current string) {
	p := s.journal.Pending
	if p == nil {
		return
	}
	ok := current != "" && !versionMatches(p.From, current)
	s.finishPending(ok, s.now())
	if ok {
		log.Printf("Update: auto-install %s → %s succeeded", p.From, current)
	} else {
		log.Printf("Update: auto-install %s → %s failed, still on %s", p.From, p.Version, current)
	}
}

func (s *UpdateScheduler) finishPending(ok bool, now time.Time) {
	rec := *s.journal.Pending
	rec.OK = ok
	rec.DoneAt = now.Unix()
	s.journal.Pending = nil
	s.journal.Last = &rec
	if ok {
		s.journal.FailedVersion = ""
	} else {
		s.journal.FailedVersion = rec.Version
	}
	s.saveJournal()
}

func (s *UpdateScheduler) loadJournal() {
	data, err := os.ReadFile(s.journalPath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.journal); err != nil {
		log.Printf("Update: broken %s: %v", autoUpdateFile, err)
		s.journal = autoUpdateJournal{}
	}
}

func (s *UpdateScheduler) saveJournal() {
	data, err := json.Marshal(s.journal)
	if err != nil {
		return
	}
	if err := utils.AtomicWriteFile(s.journalPath(), data, 0o600); err != nil {
		log.Printf("Update: save %s: %v", autoUpdateFile, err)
	}
}

// parseInstallWindow разбирает "HH:MM-HH:MM" в минуты от полуночи.
func parseInstallWindow(window string) (start, end int, err error) {
	from, to, ok := strings.Cut(window, "-")
	if !ok {
		return 0, 0, fmt.Errorf("window must be HH:MM-HH:MM")
	}
	if start, err = parseClock(from); err != nil {
		return 0, 0, err
	}
	if end, err = parseClock(to); err != nil {
		return 0, 0, err
	}
	if start == end {
		return 0, 0, fmt.Errorf("window start and end must differ")
	}
	return start, end, nil
}

func parseClock(v string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, fmt.Errorf("invalid time %q", v)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("invalid time %q", v)
	}
	return hh*60 + mm, nil
}

// inInstallWindow — попадает ли now в окно; окно может переходить через полночь.
func inInstallWindow(now time.Time, window string) bool {
	start, end, err := parseInstallWindow(window)
	if err != nil {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

func (a *API) SetUpdateScheduler(s *UpdateScheduler) {
	a.updateScheduler = s
}

// UpdateStateHandler — GET /api/update/state: результат последней проверки
// для уведомлений и настройки автообновления.
func (a *API) UpdateStateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	JSONSuccess(w, a.updateCheckState())
}

func (a *API) updateCheckState() UpdateCheckState {
	if a.updateScheduler == nil {
		return UpdateCheckState{
			Channel:       a.cfg.UpdateChannel,
			AutoCheck:     a.cfg.UpdateAutoCheck,
			AutoInstall:   a.cfg.UpdateAutoInstall,
			InstallWindow: a.cfg.UpdateInstallWindow,
		}
	}
	return a.updateScheduler.State()
}

// UpdateSettingsHandler — POST /api/update/settings: включение фоновой проверки,
// автоустановки и окна времени. Поля необязательны.
func (a *API) UpdateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AutoCheck     *bool   `json:"auto_check"`
		AutoInstall   *bool   `json:"auto_install"`
		InstallWindow *string `json:"install_window"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.InstallWindow != nil {
		if _, _, err := parseInstallWindow(*body.InstallWindow); err != nil {
			JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.cfg.UpdateInstallWindow = *body.InstallWindow
	}
	if body.AutoCheck != nil {
		a.cfg.UpdateAutoCheck = *body.AutoCheck
	}
	if body.AutoInstall != nil {
		a.cfg.UpdateAutoInstall = *body.AutoInstall
	}
	if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	JSONSuccess(w, a.updateCheckState())
}
