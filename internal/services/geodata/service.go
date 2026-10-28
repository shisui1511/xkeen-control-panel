package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Значения путей по умолчанию (Entware на роутере).
const (
	defaultXKeenBin    = "/opt/sbin/xkeen"
	defaultCronFile    = "/opt/var/spool/cron/crontabs/root"
	defaultXrayConfDir = "/opt/etc/xray/configs"
)

// Задания менеджера (RunView.job и OutputChunk.job).
const (
	jobUG     = "ug"
	jobJQ     = "jq"
	jobRemove = "remove"
)

// Итог запуска (LastRun.Outcome).
const (
	outcomeOK      = "ok"
	outcomePartial = "partial"
	outcomeFailed  = "failed"
	outcomeUnknown = "unknown"
)

// Итог по файлу, который панель выводит сама (XKeen всегда качает файл заново).
const (
	// ResultUpdated — файл обновлён: хэш SHA-256 до и после различается.
	ResultUpdated = "updated"
	// ResultUnchanged — файл скачан заново, содержимое прежнее.
	ResultUnchanged = "unchanged"
)

// Причины сбоя всего запуска, которые ставит панель (разбор вывода даёт run_network
// и no_sources).
const (
	runReasonTimeout = "timeout"
	runReasonExit    = "exit"
)

const (
	// tickInterval — период проверки изменений файлов вне панели.
	tickInterval = 30 * time.Second
	// outsideQuiet — сколько после конца запуска панели правки файлов ещё считаются её
	// собственными.
	outsideQuiet = 60 * time.Second
	// publishInterval — не чаще одного события geodata за этот срок.
	publishInterval = 500 * time.Millisecond
	// rateLimitTimeout — срок запроса остатка лимита GitHub API.
	rateLimitTimeout = 10 * time.Second
	// rateLimitURL — единственный адрес, который панель сама запрашивает у GitHub.
	rateLimitURL = "https://api.github.com/rate_limit"
)

// datNameRe — допустимое имя dat-файла (как в индексе списков).
var datNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dat$`)

// ErrStopped — сервис остановлен (Stop).
var ErrStopped = errors.New("geodata: сервис остановлен")

// Deps — зависимости сервиса. Пустые пути получают значения по умолчанию.
type Deps struct {
	// DataDir — каталог данных панели (geodata_state.json, geodata/last-ug.log).
	DataDir string
	// Layer — слой «Конфигурация»: исключительный замок, уведомления, черновик.
	Layer *configlayer.Layer
	// Store — запись в xkeen.json.
	Store *lists.GeodataStore
	// Index — индекс dat-файлов каталога XKeen.
	Index *lists.Index
	// Routing — сервис маршрутизации (ссылки на файлы в правилах, каскад).
	Routing *routing.Service
	// XKeenBin, XKeenDir — исполняемый файл XKeen и каталог его скриптов.
	XKeenBin, XKeenDir string
	// CronFile — crontab пользователя root.
	CronFile string
	// MihomoDir — каталог данных Mihomo; XrayConfDir — каталог конфигов Xray.
	MihomoDir, XrayConfDir string
	// Now — источник времени (nil — time.Now).
	Now func() time.Time
	// HTTP — клиент запроса остатка лимита GitHub API (nil — клиент с таймаутом 10 с).
	HTTP *http.Client
}

// Service — менеджер geodata. Создаётся один раз в cmd/xcp/main.go; Stop вызывается
// через defer в main().
type Service struct {
	d      Deps
	dir    string // DataDir/geodata
	logRun string // DataDir/geodata/last-ug.log
	state  string // DataDir/geodata_state.json
	vars   XKeenVars
	runner Runner

	// mu защищает st, seq и поля текущего задания.
	mu           sync.Mutex
	st           State
	seq          uint64
	running      bool
	waitingApply bool
	job          string
	started      time.Time

	// omu защищает указатели на буферы вывода.
	omu   sync.Mutex
	ugOut *Output
	jqOut *Output

	// nmu сериализует сборку уведомлений.
	nmu sync.Mutex

	// ops — состояние операций записи (кэш проверок, уборка, jq).
	ops opsState

	// sched — рабочее состояние расписания и cron (замок crontab, кэш чтения, догон).
	sched schedState

	// smu упорядочивает запись файла состояния: на медленной флешке копия, снятая раньше,
	// не должна лечь в файл после более новой (savedSeq — счётчик последней записанной).
	smu      sync.Mutex
	savedSeq uint64

	// pmu защищает ограничение частоты событий.
	pmu        sync.Mutex
	lastPub    time.Time
	pubTimer   *time.Timer
	pubStopped bool

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	lmu       sync.Mutex
	stopped   bool
	startOnce sync.Once
	stopOnce  sync.Once
}

// New создаёт сервис. Слой, хранилище записей и индекс обязательны. Конструктор
// читает файл состояния и журнал последнего запуска, остального на диске не трогает.
func New(d Deps) (*Service, error) {
	if d.Layer == nil {
		return nil, errors.New("geodata: не задан слой конфигурации")
	}
	if d.Store == nil {
		return nil, errors.New("geodata: не задано хранилище записей xkeen.json")
	}
	if d.Index == nil {
		return nil, errors.New("geodata: не задан индекс dat-файлов")
	}
	if d.DataDir == "" {
		return nil, errors.New("geodata: не задан каталог данных")
	}
	if d.XKeenBin == "" {
		d.XKeenBin = defaultXKeenBin
	}
	if d.XKeenDir == "" {
		d.XKeenDir = DefaultXKeenDir
	}
	if d.CronFile == "" {
		d.CronFile = defaultCronFile
	}
	if d.MihomoDir == "" {
		d.MihomoDir = lists.DefaultMihomoDir
	}
	if d.XrayConfDir == "" {
		d.XrayConfDir = defaultXrayConfDir
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: rateLimitTimeout}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		d:      d,
		dir:    filepath.Join(d.DataDir, "geodata"),
		state:  filepath.Join(d.DataDir, stateFileName),
		vars:   LoadXKeenVars(d.XKeenDir),
		runner: Runner{Bin: d.XKeenBin},
		ugOut:  NewOutput(0),
		jqOut:  NewOutput(0),
		ctx:    ctx,
		cancel: cancel,
	}
	s.logRun = filepath.Join(s.dir, "last-ug.log")
	s.st = loadState(s.state)
	s.loadLastOutput()
	return s, nil
}

// loadLastOutput кладёт журнал последнего запуска в буфер ug: полный вывод
// открывается по кнопке и после перезапуска панели (D-15).
func (s *Service) loadLastOutput() {
	data, err := os.ReadFile(s.logRun)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("[geodata] не удалось прочитать last-ug.log: %v", err)
		}
		return
	}
	text := string(data)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			s.ugOut.Append(text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		s.ugOut.Append(text[start:])
	}
}

// spawn запускает фоновую горутину сервиса под wg; после Stop не запускает.
func (s *Service) spawn(fn func()) bool {
	s.lmu.Lock()
	if s.stopped {
		s.lmu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.lmu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// Go запускает fn в фоне под учётом Stop. false — сервис остановлен, fn не запущена.
func (s *Service) Go(fn func()) bool { return s.spawn(fn) }

// Start запускает фоновый цикл: проверка правок файлов вне панели каждые 30 секунд.
// Первая проверка выполняется до возврата: она сверяет файлы с сигнатурами из
// состояния (правки, сделанные, пока панель не работала). Повторный вызов безопасен.
func (s *Service) Start() {
	s.startOnce.Do(func() {
		s.checkOutside()
		s.syncCron(true)
		s.rebuildNotices()
		s.sched.begin = time.Now()
		s.spawn(s.loop)
		s.spawn(s.schedLoop)
	})
}

// Stop останавливает сервис: отменяет контекст (идущий запуск получает SIGTERM),
// дожидается фоновых горутин. Безопасен при повторном вызове.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		s.lmu.Lock()
		s.stopped = true
		s.lmu.Unlock()
		s.cancel()
		s.pmu.Lock()
		s.pubStopped = true
		if s.pubTimer != nil {
			s.pubTimer.Stop()
			s.pubTimer = nil
		}
		s.pmu.Unlock()
	})
	s.wg.Wait()
}

// loop — цикл тиков.
func (s *Service) loop() {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick — один такт цикла: правки файлов вне панели, возврат строки cron XKeen
// (CronTwo, D-17) и уборка временных записей стандартных наборов после запусков по
// cron или SSH. Запуски по расписанию ведёт отдельный планировщик (schedLoop). С выключенным флагом слоя xkeen.json в фоне не правится
// (D-04 из 144): ожидания остаются в состоянии и доводятся первым тиком после
// включения флага.
func (s *Service) tick() {
	s.checkOutside()
	s.syncCron(false)
	if s.hasPending() && s.d.Layer.BusyHolder() != configlayer.HolderGeodataUpdate &&
		s.d.Layer.Enabled() {
		s.cleanupPending(s.ctx)
	}
}

// RunNow запускает `xkeen -ug`: исключительный замок слоя берётся синхронно (занят —
// ошибка сразу, ErrGeodataBusy), сам запуск идёт в фоне, вывод — в буфере ug.
func (s *Service) RunNow(user bool) error {
	release, err := s.d.Layer.BeginExclusive(s.ctx, user, configlayer.HolderGeodataUpdate)
	if err != nil {
		return err
	}
	trigger := "button"
	if !user {
		trigger = "auto"
	}
	s.setRun(true, jobUG)
	if !s.Go(func() {
		defer release()
		if err := s.runLocked(s.ctx, trigger); err != nil {
			log.Printf("[geodata] запуск xkeen -ug: %v", err)
		}
	}) {
		release()
		s.setRun(false, "")
		return ErrStopped
	}
	return nil
}

// run — блокирующий запуск под исключительным замком. user=false: ждёт конца идущего
// применения (RunView.waiting_apply на время ожидания), user=true: занятый замок —
// ошибка сразу. Ошибка — запуск не состоялся или не завершился (сбой процесса,
// таймаут, отмена); итоги по файлам при этом уже в состоянии.
func (s *Service) run(ctx context.Context, user bool, trigger string) error {
	if !user {
		s.setWaiting(true)
	}
	release, err := s.d.Layer.BeginExclusive(ctx, user, configlayer.HolderGeodataUpdate)
	if !user {
		s.setWaiting(false)
	}
	if err != nil {
		return err
	}
	defer release()
	return s.runLocked(ctx, trigger)
}

// runResult — итог одного запуска для постшага afterRun.
type runResult struct {
	Trigger  string
	Code     int
	Err      error
	Parse    RunParse
	Results  map[string]FileResult
	LastRun  LastRun
	Finished time.Time
}

// runLocked выполняет запуск под уже взятым замком слоя.
func (s *Service) runLocked(ctx context.Context, trigger string) error {
	s.setRun(true, jobUG)
	res := s.execute(ctx, trigger)
	s.afterRun(ctx, res)
	s.setRun(false, "")
	s.changed()
	return res.Err
}

// afterRun — постшаг запуска под тем же замком слоя. Итог запуска сохраняется до
// постшагов: сбой постшага не должен терять итог. Затем убираются временные записи
// наборов, файлы которых только что скачаны (D-01); остальные постшаги добавляют
// следующие планы фазы.
func (s *Service) afterRun(ctx context.Context, res runResult) {
	s.mu.Lock()
	if res.LastRun.Outcome != outcomeFailed {
		// Базы обновлены (любым запуском): повтор после сбоя сети больше не нужен.
		s.st.RetryAt = time.Time{}
		s.st.RetryFailed = false
	}
	st := s.st.clone()
	s.mu.Unlock()
	if err := saveState(s.state, st); err != nil {
		log.Printf("[geodata] не удалось сохранить %s: %v", stateFileName, err)
	}
	s.cleanupPending(ctx)
}

// execute запускает `xkeen -ug` и записывает итоги в состояние: хэши файлов
// источников до и после, разбор вывода по файлам, итог всего запуска.
func (s *Service) execute(ctx context.Context, trigger string) runResult {
	started := s.d.Now()
	names := s.sourceNames()
	before := s.hashAll(names)

	out := NewOutput(0)
	s.setOutput(jobUG, out)
	code, runErr := s.runner.Run(ctx, []string{"-ug"}, out)
	parsed := ParseUGOutput(out.Lines(), s.displays())
	finished := s.d.Now()

	after := s.hashAll(unionNames(names, parsed.Files))

	results := map[string]FileResult{}
	notUpdated := 0
	anyUnverified := false
	for name, fo := range parsed.Files {
		r := FileResult{Status: fo.Status, Reason: fo.Reason, At: finished}
		switch fo.Status {
		case StatusUpdated, StatusInstalled:
			r.Status = ResultUpdated
			r.Reason = ""
			if before[name] != "" && before[name] == after[name] {
				r.Status = ResultUnchanged
			} else if fo.Unverified {
				r.Unverified = true
				anyUnverified = true
			}
		case StatusKept, StatusFailed:
			notUpdated++
		}
		results[name] = r
	}
	var geoipset *FileResult
	if fo := parsed.GeoIPSet; fo != nil {
		r := FileResult{Status: fo.Status, Reason: fo.Reason, At: finished}
		switch fo.Status {
		case StatusUpdated, StatusInstalled:
			r.Status = ResultUpdated
			r.Reason = ""
			if fo.Unverified {
				r.Unverified = true
				anyUnverified = true
			}
		case StatusKept, StatusFailed:
			notUpdated++
		}
		geoipset = &r
	}
	if anyUnverified && s.githubLimited(ctx) {
		for name, r := range results {
			if r.Unverified {
				r.Limit = true
				results[name] = r
			}
		}
		if geoipset != nil && geoipset.Unverified {
			geoipset.Limit = true
		}
	}

	last := LastRun{At: started, Finished: finished, Parsed: parsed.Parsed, ExitCode: code, Trigger: trigger}
	switch {
	case errors.Is(runErr, ErrRunTimeout):
		last.Outcome, last.Reason = outcomeFailed, runReasonTimeout
	case runErr != nil:
		last.Outcome, last.Reason = outcomeFailed, runReasonExit
	case parsed.RunError != "":
		last.Outcome, last.Reason = outcomeFailed, parsed.RunError
	case !parsed.Parsed && code != 0:
		last.Outcome, last.Reason = outcomeFailed, runReasonExit
	case !parsed.Parsed:
		last.Outcome = outcomeUnknown
	case notUpdated > 0:
		last.Outcome = outcomePartial
	default:
		last.Outcome = outcomeOK
	}
	last.NotUpdated = notUpdated

	sigs, _ := s.sigsNow()
	s.mu.Lock()
	for name, r := range results {
		s.st.Results[name] = r
		if r.Status == ResultUpdated || r.Status == ResultUnchanged {
			delete(s.st.Outside, name)
		}
	}
	if geoipset != nil {
		s.st.GeoIPSet = geoipset
	}
	for name, h := range after {
		if h != "" {
			s.st.Hashes[name] = h
		}
	}
	for name, sg := range sigs {
		s.st.Sigs[name] = sg.sig
	}
	lr := last
	s.st.LastRun = &lr
	s.mu.Unlock()

	if err := out.Save(s.logRun); err != nil {
		log.Printf("[geodata] не удалось сохранить last-ug.log: %v", err)
	}
	return runResult{Trigger: trigger, Code: code, Err: runErr, Parse: parsed, Results: results, LastRun: last, Finished: finished}
}

// unionNames — имена файлов запуска: заранее известные источники и всё, что
// назвал XKeen в выводе.
func unionNames(names []string, parsed map[string]FileOutcome) []string {
	seen := make(map[string]bool, len(names)+len(parsed))
	out := make([]string, 0, len(names)+len(parsed))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for n := range parsed {
		if !seen[n] && datNameRe.MatchString(n) {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// displays — название блока в выводе `xkeen -ug` → имя файла: стандартные наборы и
// записи geodata (имя файла → имя файла).
func (s *Service) displays() map[string]string {
	m := make(map[string]string, len(StandardSets)+4)
	for _, set := range StandardSets {
		m[set.Display] = set.File
	}
	if st, err := s.d.Store.Status(); err == nil {
		for _, e := range st.Entries {
			if datNameRe.MatchString(e.File) {
				m[e.File] = e.File
			}
		}
	}
	return m
}

// sourceNames — файлы источников для хэшей до и после запуска: стандартные наборы,
// лежащие на диске, и файлы записей geodata.
func (s *Service) sourceNames() []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if datNameRe.MatchString(n) && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, set := range StandardSets {
		if s.datPath(set.File) != "" {
			add(set.File)
		}
	}
	if st, err := s.d.Store.Status(); err == nil {
		for _, e := range st.Entries {
			add(e.File)
		}
	}
	return names
}

// datPath — путь существующего файла (или симлинка) каталога dat: само имя или, для
// стандартного набора, его второе имя. Нет файла или имя недопустимо — пусто.
func (s *Service) datPath(name string) string {
	if !datNameRe.MatchString(name) {
		return ""
	}
	cands := []string{name}
	for _, set := range StandardSets {
		if set.File == name && set.Alias != "" {
			cands = append(cands, set.Alias)
		}
	}
	for _, c := range cands {
		p := filepath.Join(s.d.Index.Dir(), c)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// hashAll считает SHA-256 файлов потоком; нет файла или ошибка чтения — пустая строка.
func (s *Service) hashAll(names []string) map[string]string {
	out := make(map[string]string, len(names))
	for _, n := range names {
		out[n] = hashFile(s.datPath(n))
	}
	return out
}

// hashFile — SHA-256 файла (читается потоком); пустой путь или ошибка — "".
func hashFile(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// githubLimited — исчерпан ли лимит запросов GitHub API (resources.core.remaining == 0).
// Запрос не удался или ответ не разобран — false: причина «не сверена» остаётся «API
// недоступен».
func (s *Service) githubLimited(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, rateLimitTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rateLimitURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.d.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Resources struct {
			Core struct {
				Remaining *int `json:"remaining"`
			} `json:"core"`
		} `json:"resources"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return false
	}
	return body.Resources.Core.Remaining != nil && *body.Resources.Core.Remaining == 0
}

// setOutput подменяет буфер вывода задания (новый запуск — чистый буфер).
func (s *Service) setOutput(job string, out *Output) {
	s.omu.Lock()
	defer s.omu.Unlock()
	switch job {
	case jobJQ:
		s.jqOut = out
	default:
		s.ugOut = out
	}
}

// Output отдаёт вывод задания (ug или jq) начиная со смещения from. Построчный вывод
// по шине событий слоя не идёт: клиент опрашивает этот метод, пока running истинно.
func (s *Service) Output(job string, from int64) OutputChunk {
	if from < 0 {
		from = 0
	}
	s.omu.Lock()
	var out *Output
	switch job {
	case jobJQ:
		out = s.jqOut
	default:
		job = jobUG
		out = s.ugOut
	}
	s.omu.Unlock()
	text, next, truncated := out.Read(from)
	s.mu.Lock()
	running := s.running && s.job == job
	s.mu.Unlock()
	return OutputChunk{Job: job, From: from, Next: next, Text: text, Running: running, Truncated: truncated}
}

// setRun отмечает начало и конец задания и публикует событие.
func (s *Service) setRun(running bool, job string) {
	s.mu.Lock()
	if s.running == running && s.job == job {
		s.mu.Unlock()
		return
	}
	s.running = running
	s.job = job
	if running {
		s.started = s.d.Now()
	} else {
		s.started = time.Time{}
	}
	s.mu.Unlock()
	s.touch()
}

// setWaiting отмечает ожидание конца «Применить» перед фоновым запуском.
func (s *Service) setWaiting(v bool) {
	s.mu.Lock()
	if s.waitingApply == v {
		s.mu.Unlock()
		return
	}
	s.waitingApply = v
	s.mu.Unlock()
	s.touch()
}

// touch увеличивает счётчик состояния и публикует событие geodata.
func (s *Service) touch() {
	s.mu.Lock()
	s.seq++
	s.mu.Unlock()
	s.publish()
}

// changed вызывается после каждой правки состояния: пересобирает уведомления раздела,
// публикует событие со счётчиком и сохраняет файл состояния. Клиенты читают состояние из
// памяти, поэтому запись на флешку (на роутере она бывает медленной, до десятков секунд)
// идёт последней и не задерживает ни уведомления, ни событие.
func (s *Service) changed() {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	st := s.st.clone()
	s.mu.Unlock()
	s.rebuildNotices()
	s.publish()
	// Записи идут по очереди, и в файл попадает самое свежее из снятых состояний: опоздавшая
	// старая копия пропускается (иначе после перезапуска панели пропали бы, например,
	// CronTaken и расписание после «Перенять»).
	s.smu.Lock()
	defer s.smu.Unlock()
	if seq <= s.savedSeq {
		return
	}
	if err := saveState(s.state, st); err != nil {
		log.Printf("[geodata] не удалось сохранить %s: %v", stateFileName, err)
		return
	}
	s.savedSeq = seq
}

// publish отправляет слою событие geodata не чаще одного за publishInterval: события
// внутри окна сливаются в одно отложенное, которое несёт последний счётчик.
func (s *Service) publish() {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	if s.pubStopped || s.pubTimer != nil {
		return
	}
	if wait := publishInterval - time.Since(s.lastPub); wait > 0 {
		s.pubTimer = time.AfterFunc(wait, s.flushPublish)
		return
	}
	s.lastPub = time.Now()
	s.d.Layer.PublishGeodata(s.currentSeq())
}

// flushPublish — отложенная публикация последнего счётчика.
func (s *Service) flushPublish() {
	s.pmu.Lock()
	if s.pubStopped {
		s.pmu.Unlock()
		return
	}
	s.pubTimer = nil
	s.lastPub = time.Now()
	s.pmu.Unlock()
	s.d.Layer.PublishGeodata(s.currentSeq())
}

func (s *Service) currentSeq() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// fileSig — сигнатура dat-файла и время его изменения.
type fileSig struct {
	sig   string
	mtime time.Time
}

// sigsNow — сигнатуры (размер:mtime) всех dat-файлов каталога по реальным именам.
// false — каталог не прочитан.
func (s *Service) sigsNow() (map[string]fileSig, bool) {
	files, err := s.d.Index.Files()
	if err != nil {
		return nil, false
	}
	out := make(map[string]fileSig, len(files))
	for _, f := range files {
		out[f.Name] = fileSig{sig: fmt.Sprintf("%d:%d", f.Size, f.ModTime.UnixNano()), mtime: f.ModTime}
	}
	return out, true
}

// checkOutside сверяет файлы с сигнатурами из состояния. Файл изменился, а панель
// `-ug` не запускает (замок не у менеджера, прошлый запуск закончился больше минуты
// назад) — отметка «обновлено вне панели» с временем изменения файла (D-17). Новый
// файл только запоминается, без отметки. Пересборку провайдеров Mihomo делает
// наблюдатель dat слоя маршрутизации.
func (s *Service) checkOutside() {
	cur, ok := s.sigsNow()
	if !ok {
		return
	}
	holder := s.d.Layer.BusyHolder()
	now := s.d.Now()
	s.mu.Lock()
	quiet := holder != configlayer.HolderGeodataUpdate && !s.running
	if lr := s.st.LastRun; lr != nil && now.Sub(lr.Finished) <= outsideQuiet {
		quiet = false
	}
	dirty := false
	for name, cs := range cur {
		old, known := s.st.Sigs[name]
		if known && old == cs.sig {
			continue
		}
		if known && !quiet {
			// Идёт запуск панели или он только что закончился: итог запишет сам запуск.
			continue
		}
		if known {
			s.st.Outside[name] = cs.mtime
		}
		s.st.Sigs[name] = cs.sig
		dirty = true
	}
	if quiet {
		for name := range s.st.Sigs {
			if _, ok := cur[name]; !ok {
				delete(s.st.Sigs, name)
				delete(s.st.Outside, name)
				dirty = true
			}
		}
	}
	s.mu.Unlock()
	if dirty {
		s.changed()
	}
}
