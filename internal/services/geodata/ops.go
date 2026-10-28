package geodata

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Виды операций записи (WriteOp.Kind), которые идут сразу (D-18).
const (
	// WriteEnableSet — включить набор: стандартный (временная запись, D-01) или набор
	// встроенного каталога (постоянная запись, D-02, D-38).
	WriteEnableSet = "enable_set"
	// WriteAdd — добавить свой источник (после проверки, D-03, D-04).
	WriteAdd = "add"
	// WriteSetURL — сменить адрес записи (после проверки, D-07).
	WriteSetURL = "set_url"
	// WriteGhProxy — записать xkeen.gh_proxy (D-09).
	WriteGhProxy = "gh_proxy"
	// WriteEnableUpdate — включить geofile_update (D-10).
	WriteEnableUpdate = "enable_update"
	// WriteDedup — убрать дубль: запись со стандартным именем при лежащем файле (D-08).
	WriteDedup = "dedup"
	// WriteFinishInclude — довести включение: убрать временную запись, которую фоновая
	// уборка не смогла убрать сама (D-01, Pitfall 9).
	WriteFinishInclude = "finish_include"
)

const (
	// probeCacheTTL — сколько результат проверки источника годится для записи.
	probeCacheTTL = 10 * time.Minute
	// ghProxyMax — наибольшая длина значения gh_proxy.
	ghProxyMax = 512
)

// Коды ошибок операций менеджера (вместе с кодами lists: set_unknown, already_present,
// entry_not_found, comments_lost, geodata_changed, no_jq, geofile_update_off,
// update_failed). Обработчик определяет код через lists.ErrorCode, а блок проверки
// источника — через errors.As к *ProbeBlockedError.
var (
	ErrNameInvalid  = &lists.CodedError{Code: "name_invalid"}
	ErrNameTaken    = &lists.CodedError{Code: "name_taken"}
	ErrURLInvalid   = &lists.CodedError{Code: "url_invalid"}
	ErrXKeenMissing = &lists.CodedError{Code: "xkeen_missing"}
	ErrXKeenTooOld  = &lists.CodedError{Code: "xkeen_too_old"}
	ErrNotPending   = &lists.CodedError{Code: "not_pending"}
	ErrBusy         = &lists.CodedError{Code: "geodata_busy"}
	ErrApplyBusy    = &lists.CodedError{Code: "apply_busy"}
	ErrBadOp        = &lists.CodedError{Code: "bad_op"}
)

// ProbeBlockedError — проверка источника нашла блок: запись не сделана. Result несёт
// проверки для показа в интерфейсе (код probe_blocked).
type ProbeBlockedError struct{ Result ProbeResult }

func (e *ProbeBlockedError) Error() string { return "probe_blocked" }

// WriteOp — операция записи по запросу интерфейса.
type WriteOp struct {
	Kind  string `json:"kind"`
	Set   string `json:"set,omitempty"`
	File  string `json:"file,omitempty"`
	URL   string `json:"url,omitempty"`
	Value string `json:"value,omitempty"`
}

// PreviewView — предпросмотр правки xkeen.json. Base — отпечаток Before: клиент
// возвращает его при записи.
type PreviewView struct {
	Before       string `json:"before"`
	After        string `json:"after"`
	CommentsKept bool   `json:"comments_kept"`
	Base         string `json:"base"`
}

// opsState — состояние операций записи: кэш проверок источника, замки уборки и jq.
type opsState struct {
	pmu    sync.Mutex
	pcache map[string]probeCacheEntry

	// cleanMu сериализует cleanupPending: запуск из постшага и из тика не идут вместе.
	cleanMu sync.Mutex
	// jqMu занят, пока ставится jq.
	jqMu sync.Mutex
}

type probeCacheEntry struct {
	res ProbeResult
	at  time.Time
}

// opPlan — правка xkeen.json и побочные действия после успешной записи.
type opPlan struct {
	gop lists.GeodataOp
	// pending — стандартный набор, который нужно запомнить в state.pending_enables.
	pending *PendingEnable
	// finish — операция «довести включение» по этому файлу.
	finish string
}

// standardSet ищет стандартный набор по ID.
func standardSet(id string) (StandardSet, bool) {
	for _, set := range StandardSets {
		if set.ID == id {
			return set, true
		}
	}
	return StandardSet{}, false
}

// standardByName ищет стандартный набор по основному или второму имени файла.
func standardByName(name string) (StandardSet, bool) {
	for _, set := range StandardSets {
		if set.File == name || (set.Alias != "" && set.Alias == name) {
			return set, true
		}
	}
	return StandardSet{}, false
}

// takenNames — имена, которые нельзя дать своему источнику: стандартные (с алиасами)
// и файлы существующих записей.
func takenNames(entries []lists.GeodataEntry) map[string]bool {
	m := make(map[string]bool, len(StandardSets)*2+len(entries))
	for _, set := range StandardSets {
		m[set.File] = true
		if set.Alias != "" {
			m[set.Alias] = true
		}
	}
	for _, e := range entries {
		m[e.File] = true
	}
	return m
}

// hasEntryNamed — в xkeen.json есть запись geodata с таким именем файла.
func hasEntryNamed(entries []lists.GeodataEntry, file string) bool {
	for _, e := range entries {
		if e.File == file {
			return true
		}
	}
	return false
}

// staticCheck — условия, без которых запись невозможна: XKeen установлен и достаточно
// новый.
func (s *Service) staticCheck() error {
	if _, err := os.Stat(s.d.XKeenBin); err != nil {
		return ErrXKeenMissing
	}
	if s.d.Layer.FeatureAvailability(configlayer.FeatureXKeenUserGeodata) == configlayer.Unavailable {
		return ErrXKeenTooOld
	}
	return nil
}

// writeCheck — условия записи: статические плюс отсутствие `xkeen -ug` и «Применить».
func (s *Service) writeCheck() error {
	if err := s.staticCheck(); err != nil {
		return err
	}
	if s.d.Layer.BusyHolder() != "" {
		return ErrBusy
	}
	if s.d.Layer.Snapshot().Apply.Running {
		return ErrApplyBusy
	}
	return nil
}

// probeKey — ключ кэша проверки. Режим входит в ключ: результат режима add содержит
// проверку имени, результат edit — нет.
func probeKey(file, url, ghProxy, mode string) string {
	return mode + "|" + file + "|" + url + "|" + ghProxy
}

// Probe проверяет свой источник без записи и кладёт результат в кэш на 10 минут:
// запись тех же значений в этот срок не повторяет сетевую проверку. mode —
// ProbeModeAdd (по умолчанию) или ProbeModeEdit (имя не проверяется).
func (s *Service) Probe(ctx context.Context, file, url, mode string) (ProbeResult, error) {
	if mode != ProbeModeEdit {
		mode = ProbeModeAdd
	}
	st, err := s.d.Store.Status()
	if err != nil {
		return ProbeResult{}, err
	}
	in := ProbeInput{
		File:    file,
		URL:     url,
		GhProxy: st.GhProxy,
		Mode:    mode,
		Mirrors: s.vars.Mirrors,
		GeoDir:  s.d.Index.Dir(),
		Taken:   takenNames(st.Entries),
	}
	if len(s.vars.GitHubURLs) > 0 {
		in.GitHubURL = s.vars.GitHubURLs[0]
	}
	res := Probe(ctx, in)
	s.ops.pmu.Lock()
	if s.ops.pcache == nil {
		s.ops.pcache = map[string]probeCacheEntry{}
	}
	now := s.d.Now()
	for k, e := range s.ops.pcache {
		if now.Sub(e.at) >= probeCacheTTL {
			delete(s.ops.pcache, k)
		}
	}
	s.ops.pcache[probeKey(file, url, st.GhProxy, mode)] = probeCacheEntry{res: res, at: now}
	s.ops.pmu.Unlock()
	return res, nil
}

// probedOK возвращает результат проверки для записи: из кэша (не старше 10 минут) или
// после новой проверки. Блок — *ProbeBlockedError.
func (s *Service) probedOK(ctx context.Context, file, url, ghProxy, mode string) error {
	s.ops.pmu.Lock()
	e, ok := s.ops.pcache[probeKey(file, url, ghProxy, mode)]
	s.ops.pmu.Unlock()
	res := e.res
	if !ok || s.d.Now().Sub(e.at) >= probeCacheTTL {
		var err error
		if res, err = s.Probe(ctx, file, url, mode); err != nil {
			return err
		}
	}
	if res.Blocked {
		return &ProbeBlockedError{Result: res}
	}
	return nil
}

// validGhProxy — значение gh_proxy: пусто (прокси не нужен) или адрес http(s) без
// пробелов, кавычек, обратной косой и управляющих символов, не длиннее 512 байт.
func validGhProxy(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > ghProxyMax {
		return false
	}
	for _, r := range v {
		if r <= 0x20 || r == 0x7f || r == '"' || r == '\'' || r == '\\' || r == '`' {
			return false
		}
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	}
	return false
}

// planOp превращает операцию интерфейса в правку xkeen.json. probe=false — сетевую
// проверку источника не делать (предпросмотр): имя и схема проверяются всегда.
func (s *Service) planOp(ctx context.Context, op WriteOp, probe bool) (opPlan, error) {
	switch op.Kind {
	case WriteEnableSet:
		return s.planEnableSet(op)
	case WriteAdd:
		return s.planAdd(ctx, op, probe)
	case WriteSetURL:
		return s.planSetURL(ctx, op, probe)
	case WriteGhProxy:
		if !validGhProxy(op.Value) {
			return opPlan{}, ErrURLInvalid
		}
		return opPlan{gop: lists.GeodataOp{Kind: lists.OpGhProxy, Value: op.Value}}, nil
	case WriteEnableUpdate:
		return opPlan{gop: lists.GeodataOp{Kind: lists.OpEnableUpdate}}, nil
	case WriteDedup:
		return s.planDedup(op)
	case WriteFinishInclude:
		return s.planFinishInclude(op)
	}
	return opPlan{}, ErrBadOp
}

// planEnableSet — включение набора. Стандартный набор получает временную запись под
// стандартным именем и адресом XKeen, ожидание запоминается в state.pending_enables.
// Набор каталога — постоянная запись.
func (s *Service) planEnableSet(op WriteOp) (opPlan, error) {
	st, err := s.d.Store.Status()
	if err != nil {
		return opPlan{}, err
	}
	if set, ok := standardSet(op.Set); ok {
		// XKeen считает набор включённым по наличию файла (в том числе второго имени).
		if s.datPath(set.File) != "" || hasEntryNamed(st.Entries, set.File) || (set.Alias != "" && hasEntryNamed(st.Entries, set.Alias)) {
			return opPlan{}, lists.ErrAlreadyPresent
		}
		u := s.vars.URL(set)
		return opPlan{
			gop:     lists.GeodataOp{Kind: lists.OpAdd, File: set.File, URL: u},
			pending: &PendingEnable{File: set.File, URL: u, Since: s.d.Now()},
		}, nil
	}
	cat, err := lists.Resolve(s.d.Index, st.Entries)
	if err != nil {
		return opPlan{}, err
	}
	for _, e := range cat.Extended {
		if e.ID != op.Set {
			continue
		}
		if e.InXKeen {
			return opPlan{}, lists.ErrAlreadyPresent
		}
		return opPlan{gop: lists.GeodataOp{Kind: lists.OpAdd, File: e.File, URL: e.URL}}, nil
	}
	return opPlan{}, lists.ErrSetUnknown
}

// checkURL — адрес пригоден для записи: схема http или https, без пробелов и
// управляющих символов.
func checkURL(raw string) error {
	if c, _ := probeCheckScheme(raw); c.Level == ProbeLevelBlock {
		return ErrURLInvalid
	}
	return nil
}

// planAdd — свой источник: имя, схема, затем проверка источника без блоков.
func (s *Service) planAdd(ctx context.Context, op WriteOp, probe bool) (opPlan, error) {
	st, err := s.d.Store.Status()
	if err != nil {
		return opPlan{}, err
	}
	switch c := CheckName(op.File, takenNames(st.Entries)); {
	case c.Code == "name_taken":
		return opPlan{}, ErrNameTaken
	case c.Level == ProbeLevelBlock:
		return opPlan{}, ErrNameInvalid
	}
	if err := checkURL(op.URL); err != nil {
		return opPlan{}, err
	}
	if probe {
		if err := s.probedOK(ctx, op.File, op.URL, st.GhProxy, ProbeModeAdd); err != nil {
			return opPlan{}, err
		}
	}
	return opPlan{gop: lists.GeodataOp{Kind: lists.OpAdd, File: op.File, URL: op.URL}}, nil
}

// planSetURL — смена адреса записи: запись есть, проверка источника без блоков.
func (s *Service) planSetURL(ctx context.Context, op WriteOp, probe bool) (opPlan, error) {
	st, err := s.d.Store.Status()
	if err != nil {
		return opPlan{}, err
	}
	if !hasEntryNamed(st.Entries, op.File) {
		return opPlan{}, lists.ErrEntryNotFound
	}
	if err := checkURL(op.URL); err != nil {
		return opPlan{}, err
	}
	if probe {
		if err := s.probedOK(ctx, op.File, op.URL, st.GhProxy, ProbeModeEdit); err != nil {
			return opPlan{}, err
		}
	}
	return opPlan{gop: lists.GeodataOp{Kind: lists.OpSetURL, File: op.File, URL: op.URL}}, nil
}

// pendingFor — ожидание включения по имени файла.
func (s *Service) pendingFor(file string) (PendingEnable, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.PendingEnables {
		if p.File == file {
			return p, true
		}
	}
	return PendingEnable{}, false
}

// planDedup — «Убрать дубль» (D-08): запись со стандартным именем (или вторым именем)
// при лежащем файле; запись из ожидающих включений не трогается.
func (s *Service) planDedup(op WriteOp) (opPlan, error) {
	set, ok := standardByName(op.File)
	if !ok || s.datPath(set.File) == "" {
		return opPlan{}, ErrNotPending
	}
	st, err := s.d.Store.Status()
	if err != nil {
		return opPlan{}, err
	}
	if !hasEntryNamed(st.Entries, op.File) {
		return opPlan{}, ErrNotPending
	}
	if _, pend := s.pendingFor(op.File); pend {
		return opPlan{}, ErrNotPending
	}
	return opPlan{gop: lists.GeodataOp{Kind: lists.OpRemove, File: op.File}}, nil
}

// planFinishInclude — «Довести включение» (Pitfall 9): у файла есть застрявшая уборка;
// запись убирается по паре файл и адрес из ожидания.
func (s *Service) planFinishInclude(op WriteOp) (opPlan, error) {
	s.mu.Lock()
	_, stuck := s.st.IncludeStuck[op.File]
	s.mu.Unlock()
	p, pend := s.pendingFor(op.File)
	if !stuck || !pend {
		return opPlan{}, ErrNotPending
	}
	return opPlan{
		gop:    lists.GeodataOp{Kind: lists.OpRemove, File: p.File, MatchURL: p.URL},
		finish: p.File,
	}, nil
}

// Preview строит предпросмотр правки: xkeen.json не меняется, сеть не трогается.
func (s *Service) Preview(op WriteOp) (PreviewView, error) {
	if err := s.staticCheck(); err != nil {
		return PreviewView{}, err
	}
	plan, err := s.planOp(context.Background(), op, false)
	if err != nil {
		return PreviewView{}, err
	}
	p, err := s.d.Store.Preview(plan.gop)
	if err != nil {
		// Запись для уборки уже нет: нечего убирать, показывается пустая правка.
		if plan.finish != "" && lists.ErrorCode(err) == lists.ErrEntryNotFound.Code && p.Before != "" {
			return PreviewView{Before: p.Before, After: p.Before, CommentsKept: true, Base: p.Base}, nil
		}
		return PreviewView{}, err
	}
	return PreviewView{Before: p.Before, After: p.After, CommentsKept: p.CommentsKept, Base: p.Base}, nil
}

// Write выполняет операцию сразу (D-18): проверка условий, при add и set_url —
// проверка источника без блоков, запись в xkeen.json с резервной копией и сверкой по
// base. acceptCommentLoss подтверждает потерю комментариев. updateNow запускает
// `xkeen -ug` после записи: ошибка запуска при сделанной записи — update_failed.
func (s *Service) Write(ctx context.Context, op WriteOp, base string, acceptCommentLoss, updateNow bool) error {
	if err := s.writeCheck(); err != nil {
		return err
	}
	plan, err := s.planOp(ctx, op, true)
	if err != nil {
		return err
	}
	if err := s.d.Store.Apply(plan.gop, base, acceptCommentLoss); err != nil {
		// Запись для уборки пропала сама: ожидание закрывается без правки файла.
		if plan.finish == "" || lists.ErrorCode(err) != lists.ErrEntryNotFound.Code {
			return err
		}
	}
	s.mu.Lock()
	if plan.pending != nil {
		s.dropPendingLocked(plan.pending.File)
		s.st.PendingEnables = append(s.st.PendingEnables, *plan.pending)
		delete(s.st.IncludeStuck, plan.pending.File)
		delete(s.st.Cleaned, plan.pending.File)
	}
	if plan.finish != "" {
		s.dropPendingLocked(plan.finish)
		delete(s.st.IncludeStuck, plan.finish)
	}
	s.mu.Unlock()
	// Списки могли появиться или исчезнуть: проверка черновика пересчитывается.
	s.d.Layer.InvalidateChecks()
	s.changed()
	if updateNow {
		if err := s.RunNow(true); err != nil {
			return fmt.Errorf("%w: %v", lists.ErrUpdateFailed, err)
		}
	}
	return nil
}

// dropPendingLocked убирает ожидание включения по имени файла. Вызывается под s.mu.
func (s *Service) dropPendingLocked(file string) {
	kept := s.st.PendingEnables[:0:0]
	for _, p := range s.st.PendingEnables {
		if p.File != file {
			kept = append(kept, p)
		}
	}
	s.st.PendingEnables = kept
}

// hasPending — есть ли ожидания включения.
func (s *Service) hasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.PendingEnables) > 0
}
