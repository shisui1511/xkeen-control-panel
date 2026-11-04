package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Состояния перехвата в DNSView (frontend/src/lib/dnsContract.json).
const (
	StateOff         = "off"
	StateApplying    = "applying"
	StateWindow      = "window"
	StateOn          = "on"
	StateWaiting     = "waiting"
	StateUnavailable = "unavailable"
)

// DefaultCheckName — имя «Проверить резолв» по умолчанию: нейтральное, без
// доменов провайдеров узлов (CONTEXT, Claude's Discretion).
const DefaultCheckName = "example.com"

// Коды ошибок службы (frontend/src/lib/dnsErrorCodes.json, раздел codes).
const (
	CodeBusy        = "busy"
	CodeGeneric     = "generic"
	CodeNameInvalid = "name_invalid"
	CodeUnavailable = "unavailable"
)

// Error — ошибка службы со стабильным кодом: обработчик переводит её в HTTP-ответ
// по таблице кодов и тексту configlayer.dns.<код>. Params — имена (зона, группа),
// без значений секций и адресов; Detail — технический текст для журнала и поля
// detail ответа.
type Error struct {
	Code   string
	Params map[string]string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return "dnstakeover: " + e.Code + ": " + e.Detail
	}
	return "dnstakeover: " + e.Code
}

// errCode — ошибка службы с кодом без параметров.
func errCode(code string) *Error { return &Error{Code: code} }

// Deps — зависимости службы; создаются в cmd/xcp/main.go.
type Deps struct {
	// DataDir — каталог данных панели (файл состояния dns_takeover.json).
	DataDir string
	// Layer — слой «Конфигурация»: применённые секции, черновик, живые данные.
	Layer *configlayer.Layer
	// XKeen — служба XKeen (окна перезапуска, история действий).
	XKeen *services.XKeenService
	// Kernel — служба ядер: активное ядро и процессы.
	Kernel *services.KernelService
	// Applier — единое решение «перезапускать ли ядро».
	Applier *services.KernelApplier
	// Lifecycle — общий замок жизненного цикла ядра (api.LifecycleLock()).
	Lifecycle *sync.Mutex
	// Device — модель и бренд прошивки (ndmc show version, с кэшем).
	Device *services.DeviceInfo
	// Routing — служба маршрутизации; nil, если она не подключена к слою.
	Routing *routing.Service
}

// Service — служба перехвата DNS роутера. Создаётся только в main.go; Stop
// вызывается через defer там же.
type Service struct {
	d   Deps
	fw  *Firmware
	now func() time.Time

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once

	// mu защищает st — состояние на диске (последняя проверка, история).
	mu sync.Mutex
	st persisted
	// checkMu — одновременно идёт одна проверка резолва (T-148-17); checking —
	// проверка идёт (операция службы для блокировки busy).
	checkMu  sync.Mutex
	checking atomic.Bool
}

// New проверяет обязательные зависимости и создаёт службу. Фоновой работы New
// не начинает: её запускает Start.
func New(d Deps) (*Service, error) {
	switch {
	case d.Layer == nil:
		return nil, errors.New("dnstakeover: не задан слой конфигурации")
	case d.Kernel == nil:
		return nil, errors.New("dnstakeover: не задана служба ядер")
	case d.XKeen == nil:
		return nil, errors.New("dnstakeover: не задана служба XKeen")
	case d.Lifecycle == nil:
		return nil, errors.New("dnstakeover: не задан замок жизненного цикла")
	case d.Device == nil:
		return nil, errors.New("dnstakeover: не задано описание устройства")
	case d.DataDir == "":
		return nil, errors.New("dnstakeover: не задан каталог данных")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		d:      d,
		fw:     newFirmware(d.Device),
		now:    time.Now,
		ctx:    ctx,
		cancel: cancel,
		st:     cleanState(),
	}, nil
}

// Start читает состояние с диска и запускает фоновую работу службы: прогрев сведений о прошивке (ndmc
// отвечает до 3 с, поэтому не на пути запроса и не на пути старта панели).
func (s *Service) Start() {
	st := loadState(s.statePath())
	s.mu.Lock()
	s.st = st
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.fw.Available(s.ctx)
		_ = s.fw.ServiceDomain()
	}()
}

// Stop отменяет контекст службы и ждёт фоновые горутины. Откатов не делает:
// решения о DNS принимает старт панели, а не её остановка. Повторный вызов
// безопасен.
func (s *Service) Stop() {
	s.stopOnce.Do(s.cancel)
	s.wg.Wait()
}

// Hooks — подключение службы к слою: регистрируется до configLayer.Start().
func (s *Service) Hooks() configlayer.Hooks {
	return configlayer.Hooks{}
}

// View — состояние перехвата для GET /api/configlayer/dns и карточки
// (DNSView в dnsContract.json). Обязательные поля — без omitempty.
type View struct {
	State         string       `json:"state"`
	Desired       bool         `json:"desired"`
	Kernel        string       `json:"kernel"`
	ActiveKernel  string       `json:"active_kernel"`
	KernelRunning bool         `json:"kernel_running"`
	Owner         Owner        `json:"owner"`
	Upstreams     []string     `json:"upstreams"`
	ZonesBuiltin  []string     `json:"zones_builtin"`
	Zones         []string     `json:"zones"`
	IPv6          bool         `json:"ipv6"`
	DefaultName   string       `json:"default_name"`
	Blocks        []string     `json:"blocks"`
	Steps         []Step       `json:"steps"`
	Legacy        Legacy       `json:"legacy"`
	ServerNowMs   int64        `json:"server_now_ms"`
	Group         *GroupRef    `json:"group,omitempty"`
	LastCheck     *CheckResult `json:"last_check,omitempty"`
	Window        *Window      `json:"window,omitempty"`
	Failure       *Failure     `json:"failure,omitempty"`
	Outcome       *Outcome     `json:"outcome,omitempty"`
	// UnavailableReason — техническая причина недоступности (ndmc не найден,
	// команда не выполнена): показывается строкой mono.
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	WaitingReason     string `json:"waiting_reason,omitempty"`
	// SectionOwner — владелец DNS-секции ядра-перехватчика: panel, xkeen, user, none.
	SectionOwner  string  `json:"section_owner,omitempty"`
	DraftParams   *Params `json:"draft_params,omitempty"`
	ParamsChanged bool    `json:"params_changed,omitempty"`
}

// Owner — кто держит порт 53 (DNSOwner): kind kernel|firmware|other|none.
type Owner struct {
	Kind    string `json:"kind"`
	Kernel  string `json:"kernel,omitempty"`
	Process string `json:"process,omitempty"`
	Addr    string `json:"addr,omitempty"`
}

// Владельцы порта 53.
const (
	OwnerKernel   = "kernel"
	OwnerFirmware = "firmware"
	OwnerOther    = "other"
	OwnerNone     = "none"
)

// GroupRef — группа-цель (DNSGroupRef): Missing — группы с таким id нет.
type GroupRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Missing bool   `json:"missing"`
}

// Window — окно подтверждения (DNSWindow): kind enable|apply|kernel_switch.
type Window struct {
	Kind       string `json:"kind"`
	DeadlineMs int64  `json:"deadline_ms"`
	TotalS     int    `json:"total_s"`
}

// Step — шаг транзакции (DNSStep).
type Step struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Failure — сбой транзакции (DNSFailure).
type Failure struct {
	Code       string            `json:"code"`
	RolledBack bool              `json:"rolled_back"`
	Params     map[string]string `json:"params,omitempty"`
	Detail     string            `json:"detail,omitempty"`
}

// Outcome — исход без пользователя (DNSOutcome): откат по окну, возврат
// сторожем, ожидание ядра.
type Outcome struct {
	Kind         string `json:"kind"`
	AtMs         int64  `json:"at_ms"`
	ToastPending bool   `json:"toast_pending"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	Kernel       string `json:"kernel,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Legacy — старое перенаправление DNS XKeen (DNSLegacy).
type Legacy struct {
	ProxyDNS bool `json:"proxy_dns"`
}

// Params — параметры перехвата черновика (DNSParams).
type Params struct {
	Group string   `json:"group"`
	Zones []string `json:"zones"`
}

// CheckResult — итог проверки резолва (DNSCheckResult); source manual|probe.
type CheckResult struct {
	OK     bool         `json:"ok"`
	Source string       `json:"source"`
	AtMs   int64        `json:"at_ms"`
	Groups []CheckGroup `json:"groups"`
}

// CheckGroup — одна проверенная пара «имя — путь» (DNSCheckGroup); path
// zone|firmware|group.
type CheckGroup struct {
	Label     string   `json:"label"`
	Name      string   `json:"name"`
	OK        bool     `json:"ok"`
	LatencyMs int64    `json:"latency_ms"`
	Path      string   `json:"path"`
	Answers   []string `json:"answers"`
	Zone      string   `json:"zone,omitempty"`
	Group     string   `json:"group,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

// View собирает состояние перехвата: прошивка, порт 53, ядро, старое
// перенаправление, время сервера.
func (s *Service) View() View {
	ctx, cancel := context.WithTimeout(s.ctx, firmwareTimeout)
	defer cancel()

	v := View{
		State:        StateOff,
		Upstreams:    []string{},
		ZonesBuiltin: []string{},
		Zones:        []string{},
		DefaultName:  DefaultCheckName,
		Blocks:       []string{},
		Steps:        []Step{},
		Owner:        PortOwner(),
	}
	if ok, reason := s.fw.Available(ctx); !ok {
		v.State = StateUnavailable
		v.UnavailableReason = reason
	}
	active := s.d.Kernel.ActiveState()
	v.ActiveKernel = active.Label()
	v.KernelRunning = s.kernelRunning(active)
	if st, err := s.d.Layer.ReadState(); err == nil {
		if live, on := configlayer.ParseDNSTakeoverLive(st.Live); on {
			v.Desired = true
			v.Kernel = live.Kernel
		}
	}
	proxy, err := ProxyDNS()
	if err != nil {
		log.Printf("dnstakeover: proxy_dns не прочитан: %v", err)
	}
	v.Legacy.ProxyDNS = proxy
	s.mu.Lock()
	if s.st.LastCheck != nil {
		lc := cloneCheck(*s.st.LastCheck)
		v.LastCheck = &lc
	}
	s.mu.Unlock()
	v.ServerNowMs = s.now().UnixMilli()
	return v
}

// cloneCheck — глубокая копия результата проверки.
func cloneCheck(c CheckResult) CheckResult {
	out := c
	out.Groups = make([]CheckGroup, len(c.Groups))
	for i, g := range c.Groups {
		g.Answers = append([]string{}, g.Answers...)
		out.Groups[i] = g
	}
	return out
}

// kernelRunning — активное ядро запущено по /proc (ProcessStates). При
// конфликте запущены оба.
func (s *Service) kernelRunning(active services.ActiveKernelState) bool {
	if active.Conflict {
		return true
	}
	for _, st := range s.d.Kernel.ProcessStates() {
		if st.Name == active.Kernel && st.Status == "running" {
			return true
		}
	}
	return false
}

// Пути резолва в результате проверки (D-30).
const (
	PathZone     = "zone"
	PathFirmware = "firmware"
	PathGroup    = "group"
)

// Источники проверки.
const (
	SourceManual = "manual"
	SourceProbe  = "probe"
)

// checkLabelManual — подпись единственной группы строк ручной проверки.
const checkLabelManual = "manual"

// route — куда пойдёт запрос имени: путь и, для зоны и группы, их имена.
type route struct {
	path  string
	zone  string
	group string
}

// normalizeName приводит имя к виду сравнения: без пробелов по краям, без точки
// в конце, в нижнем регистре.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// inZone сообщает, что имя совпадает с зоной или лежит под ней.
func inZone(name, zone string) bool {
	zone = normalizeName(zone)
	return zone != "" && (name == zone || strings.HasSuffix(name, "."+zone))
}

// appliedParams — параметры перехвата из применённой секции dns_takeover;
// ошибка разбора — нулевые параметры.
func appliedParams(st configlayer.StateView) configlayer.DNSTakeoverParams {
	p, err := configlayer.ParseDNSTakeoverParams(st.Applied[configlayer.SectionDNSTakeover])
	if err != nil {
		return configlayer.DNSTakeoverParams{}
	}
	return p
}

// targetGroup — группа-цель перехвата по секциям: заданная id или группа по
// умолчанию (D-09). ok=false — группы нет.
func targetGroup(sections configlayer.Sections, p configlayer.DNSTakeoverParams) (id, name string, ok bool) {
	if p.Group != "" {
		name, ok = configlayer.DNSTargetGroupName(sections, p.Group)
		return p.Group, name, ok
	}
	id, ok = configlayer.DNSGroupIDByName(sections, configlayer.DefaultDNSGroupName)
	return id, configlayer.DefaultDNSGroupName, ok
}

// routeOf определяет путь резолва имени по применённому состоянию: встроенная
// или своя локальная зона, порт 53 у прошивки, иначе группа-цель.
func (s *Service) routeOf(st configlayer.StateView, name string) route {
	p := appliedParams(st)
	zones := append(configlayer.BuiltinLocalZones(s.fw.ServiceDomain()), p.Zones...)
	for _, z := range zones {
		if inZone(name, z) {
			return route{path: PathZone, zone: normalizeName(z)}
		}
	}
	if PortOwner().Kind == OwnerFirmware {
		return route{path: PathFirmware}
	}
	_, group, _ := targetGroup(st.Applied, p)
	return route{path: PathGroup, group: group}
}

// interceptKernel — ядро, чья DNS-секция действует: ядро-перехватчик, иначе
// активное ядро (xray или mihomo); пусто — ядра нет или запущены оба.
func (s *Service) interceptKernel(st configlayer.StateView) string {
	if live, on := configlayer.ParseDNSTakeoverLive(st.Live); on {
		return live.Kernel
	}
	switch k := s.d.Kernel.ActiveState().Kernel; k {
	case configlayer.KernelXray, configlayer.KernelMihomo:
		return k
	}
	return ""
}

// sectionIPv6 — DNS-секция ядра под панелью отвечает AAAA: у Mihomo ipv6: true,
// у Xray queryStrategy UseIP или UseIPv6, заданный явно.
func sectionIPv6(st configlayer.StateView, kernel string) bool {
	if kernel == "" {
		return false
	}
	cs := configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")]
	if !cs.Managed || len(cs.Value) == 0 {
		return false
	}
	obj, err := configlayer.ParseOrderedObject(cs.Value)
	if err != nil {
		return false
	}
	switch kernel {
	case configlayer.KernelMihomo:
		raw, ok := obj.Get("ipv6")
		var on bool
		return ok && json.Unmarshal(raw, &on) == nil && on
	case configlayer.KernelXray:
		raw, ok := obj.Get("queryStrategy")
		var qs string
		return ok && json.Unmarshal(raw, &qs) == nil && (qs == "UseIP" || qs == "UseIPv6")
	}
	return false
}

// latencyMs — задержка в миллисекундах с округлением вверх: ответ из кэша за
// доли миллисекунды не показывается нулём.
func latencyMs(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Millisecond - 1) / time.Millisecond)
}

// failReason — короткая причина неудачного резолва: timeout, код RCODE или
// no_address (NOERROR без адресов).
func failReason(ans Answer, err error) string {
	switch {
	case errors.Is(err, ErrNoAnswer):
		return "timeout"
	case ans.RCode != dnsmessage.RCodeSuccess:
		return rcodeName(ans.RCode)
	}
	return "no_address"
}

// Check — ручная проверка резолва через 127.0.0.1:53 (D-03, D-30): пустое имя —
// имя по умолчанию; не доменное имя — name_invalid; одновременно идёт одна
// проверка (вторая — busy); на роутере без прошивки NDMS — unavailable. Итог
// сохраняется как последняя проверка и публикуется событием dns.
func (s *Service) Check(ctx context.Context, name string) (CheckResult, error) {
	name = normalizeName(name)
	if name == "" {
		name = DefaultCheckName
	}
	if _, err := queryName(name); err != nil {
		return CheckResult{}, errCode(CodeNameInvalid)
	}
	if ok, reason := s.fw.Available(ctx); !ok {
		return CheckResult{}, &Error{Code: CodeUnavailable, Detail: reason}
	}
	if !s.checkMu.TryLock() {
		return CheckResult{}, errCode(CodeBusy)
	}
	defer s.checkMu.Unlock()
	s.checking.Store(true)
	defer s.checking.Store(false)

	st, err := s.d.Layer.ReadState()
	if err != nil {
		return CheckResult{}, err
	}
	rt := s.routeOf(st, name)
	ans, rerr := Resolve(ctx, name, dnsmessage.TypeA)
	g := CheckGroup{
		Label:     checkLabelManual,
		Name:      name,
		OK:        rerr == nil,
		LatencyMs: latencyMs(ans.Latency),
		Path:      rt.path,
		Answers:   append([]string{}, ans.Addrs...),
		Zone:      rt.zone,
		Group:     rt.group,
	}
	if rerr != nil {
		g.Reason = failReason(ans, rerr)
	} else if sectionIPv6(st, s.interceptKernel(st)) {
		// AAAA — дополнение: пустой ответ на AAAA проверку не валит.
		if a6, err := Query(ctx, LoopbackDNS, name, dnsmessage.TypeAAAA, probeAttempt); err == nil && a6.RCode == dnsmessage.RCodeSuccess {
			g.Answers = append(g.Answers, a6.Addrs...)
		}
	}
	res := CheckResult{OK: g.OK, Source: SourceManual, AtMs: s.now().UnixMilli(), Groups: []CheckGroup{g}}

	s.mu.Lock()
	stored := cloneCheck(res)
	s.st.LastCheck = &stored
	snapshot := s.st
	s.mu.Unlock()
	if err := saveState(s.statePath(), snapshot); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.d.Layer.PublishDNS()
	return res, nil
}
