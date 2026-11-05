package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/netip"
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
	// stopMu согласует wg.Add из обработчиков и хуков с Stop → wg.Wait: после
	// Stop новые горутины не принимаются (spawn).
	stopMu   sync.Mutex
	stopping bool

	// mu защищает st — состояние на диске (последняя проверка, история, окно) —
	// и таймер окна: winStop — канал остановки идущей горутины-таймера (nil —
	// таймера нет), winRolling — окно забрано под откат; pendingApply — снимок до
	// пользовательского «Применить» при включённом перехвате по номеру запуска
	// (BeforeApply), его забирает AfterApply того же запуска.
	mu           sync.Mutex
	st           persisted
	winStop      chan struct{}
	winRolling   bool
	pendingApply map[int64]*pendingApply
	// saveMu держится от изменения st до записи файла состояния: записи ложатся на
	// диск в порядке изменений, а маршалинг не пересекается с правкой тех же
	// указателей (окно, снимок). Порядок замков: saveMu, затем mu; любое изменение
	// st идёт под обоими (update, openWindow, Keep, Check).
	saveMu sync.Mutex
	// checkMu — одновременно идёт одна проверка резолва (T-148-17); checking —
	// проверка идёт (операция службы для блокировки busy).
	checkMu  sync.Mutex
	checking atomic.Bool
	// domain — служебный домен роутера (string), прогретый в Start.
	domain atomic.Value

	// opMu защищает op, steps и failure — ход транзакции службы (включение,
	// выключение) для View. op — идущая операция ("" — нет): она же блокировка busy.
	opMu    sync.Mutex
	op      string
	steps   []Step
	failure *Failure
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
		fw:     newFirmware(d.Device, d.XKeen),
		now:    time.Now,
		ctx:    ctx,
		cancel: cancel,
		st:     cleanState(),
	}, nil
}

// Start читает состояние с диска и запускает фоновую работу службы: прогрев
// сведений о прошивке (ndmc отвечает до 3 с, поэтому не на пути запроса и не на
// пути старта панели). Окно подтверждения в файле значит, что панель
// перезапустилась посреди окна: неподтверждённый перехват откатывается сразу, без
// ожидания дедлайна (D-07: продолжать окно после перезапуска не нужно). Затем
// сверка прошивки и состояния (reconcile.go) и сторож DNS (guard.go).
func (s *Service) Start() {
	st := loadState(s.statePath())
	s.mu.Lock()
	s.st = st
	s.mu.Unlock()
	// Уведомления слоя живут в памяти: исход без пользователя возвращается в
	// список после перезапуска панели (D-28).
	s.d.Layer.SetNotices(noticePrefix, outcomeNotices(st.Outcome))
	// Окно и прерванный проход забираются сразу, до прогрева прошивки (ndmc отвечает
	// до 3 с): обработчики уже подключены, и за это время «Оставить» подтвердило бы
	// окно, которое по D-07 должно откатиться, а новое включение — записало бы
	// снимок, который старт принял бы за прерванный.
	w, hasWindow := s.claimWindow(nil)
	var pend pendingTxn
	hasPend := false
	if !hasWindow {
		pend, hasPend = s.claimPendingTxn()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.fw.Available(s.ctx)
		s.domain.Store(s.fw.ServiceDomain())
		// Проверку черновика с известным уже служебным доменом нужно пересчитать.
		s.d.Layer.InvalidateChecks()
		if hasWindow {
			log.Printf("dnstakeover: окно подтверждения %s не закрыто до перезапуска панели — откат", w.Kind)
			s.rollbackWindowInOp(w, reasonPanelRestart)
		}
		// Проход включения, возврата или переноса, прерванный остановкой панели до
		// окна (обычно посреди пробы), не подтверждён: откат как у окна (D-07).
		if hasPend {
			s.runPendingTxn(pend)
		} else {
			s.rollbackPendingTxn()
		}
		// Сверка прошивки и состояния (D-16, R3) — после отката окна, до сторожа.
		s.reconcileOnStart(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		// Сторож DNS (D-13) — после отката окна и сверки: до них он принял бы
		// неподтверждённый перехват за действующий.
		s.wg.Add(1)
		go s.guardLoop(s.ctx)
	}()
}

// Stop отменяет контекст службы — с ним таймер окна подтверждения и сторож DNS — и
// ждёт фоновые горутины. Откатов не делает: окно остаётся в файле состояния, и
// решение о DNS принимает старт панели, а не её остановка. Повторный вызов
// безопасен.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		s.stopMu.Lock()
		s.stopping = true
		s.stopMu.Unlock()
		s.cancel()
	})
	s.wg.Wait()
}

// spawn запускает горутину службы под wg. false — служба останавливается: горутина
// не запущена, вызывающий снимает всё, что успел занять. Add и Stop сериализованы
// stopMu, поэтому Add не гоняется с Wait.
func (s *Service) spawn(fn func()) bool {
	s.stopMu.Lock()
	if s.stopping {
		s.stopMu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.stopMu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// errStopping — служба останавливается, действие не принято.
func errStopping() error {
	return &Error{Code: CodeBusy, Detail: "служба останавливается"}
}

// cachedServiceDomain — служебный домен роутера из кэша, без вызова ndmc: им
// пользуется проверка слоя, которая обязана работать быстро. До прогрева в Start
// — пусто.
func (s *Service) cachedServiceDomain() string {
	d, _ := s.domain.Load().(string)
	return d
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
		ZonesBuiltin: configlayer.BuiltinLocalZones(s.fw.ServiceDomain()),
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
	on := false
	routingFree := false
	if st, err := s.d.Layer.ReadState(); err == nil {
		var live configlayer.DNSTakeoverLive
		live, on = configlayer.ParseDNSTakeoverLive(st.Live)
		if on {
			v.Desired = true
			v.Kernel = live.Kernel
			v.IPv6 = live.IPv6
		} else {
			v.IPv6 = routerHasIPv6()
		}
		s.fillParams(&v, st)
		kernel := interceptKernel(st, active)
		v.Upstreams = upstreams(st, kernel)
		if kernel != "" {
			v.SectionOwner = s.sectionOwner(st, kernel)
			// Слушатель :53 выпускает генератор маршрутизации ядра: отпущенная
			// маршрутизация оставила бы override без слушателя.
			routingFree = !configlayer.DesiredRouting(st.Applied).ManagedFor(kernel)
		}
	}
	proxy, err := ProxyDNS()
	if err != nil {
		log.Printf("dnstakeover: proxy_dns не прочитан: %v", err)
	}
	v.Legacy.ProxyDNS = proxy
	waiting := ""
	s.mu.Lock()
	if s.st.LastCheck != nil {
		lc := cloneCheck(*s.st.LastCheck)
		v.LastCheck = &lc
	}
	if !on && s.st.Desired {
		v.Desired = true
		v.Kernel = s.st.Kernel
		waiting = s.st.WaitingReason
		if s.st.DisableFailed {
			waiting = waitingDisableFailed
		}
	}
	if w := s.st.Window; w != nil && !s.winRolling {
		v.Window = &Window{Kind: w.Kind, DeadlineMs: w.DeadlineUnixMs, TotalS: windowTotalS}
	}
	if s.st.Outcome != nil {
		o := *s.st.Outcome
		v.Outcome = &o
	}
	rolling := s.winRolling
	s.mu.Unlock()
	s.fillOp(&v, on, waiting)
	// Окно забрано под откат, а операция отката ещё не началась: уже applying.
	if rolling && v.State != StateUnavailable {
		v.State = StateApplying
	}
	v.Blocks = s.blocks(v, active, routingFree, rolling)
	v.ServerNowMs = s.now().UnixMilli()
	return v
}

// Причины блокировки включения (UI-SPEC «Порядок блокировок»).
const (
	BlockConflict = "conflict"
	BlockBusy     = "busy"
	BlockWindow   = "window"
	BlockDraft    = "draft"
	BlockStopped  = "stopped"
	BlockSection  = "section"
	BlockRouting  = "routing"
	BlockGroup    = "group"
)

// blocks — причины блокировки в порядке приоритета UI-SPEC: конфликт ядер, идёт
// применение слоя, операция службы или откат забранного окна и прерванного прохода
// (rolling), идёт окно подтверждения, открыт черновик
// (D-01), ядро остановлено (D-04), DNS-секцию ядра ведёт не панель, маршрутизацию
// ядра панель отпустила (routingFree), группы-цели нет. Порядок задаёт только этот
// список.
func (s *Service) blocks(v View, active services.ActiveKernelState, routingFree, rolling bool) []string {
	snap := s.d.Layer.Snapshot()
	ordered := []struct {
		code string
		on   bool
	}{
		{BlockConflict, active.Conflict},
		{BlockBusy, snap.Apply.Running || snap.BusyHolder != "" || s.checking.Load() || s.opActive() || rolling},
		{BlockWindow, v.Window != nil},
		{BlockDraft, snap.DraftChanges > 0},
		{BlockStopped, !v.KernelRunning},
		{BlockSection, v.SectionOwner != "" && v.SectionOwner != SectionOwnerPanel},
		{BlockRouting, routingFree},
		{BlockGroup, v.Group == nil || v.Group.Missing},
	}
	out := []string{}
	for _, b := range ordered {
		if b.on {
			out = append(out, b.code)
		}
	}
	return out
}

// SectionOwnerPanel — DNS-секцию ядра ведёт панель (применённое состояние).
const SectionOwnerPanel = "panel"

// fillParams заполняет зоны, группу-цель и параметры черновика: зоны и группа —
// из применённой секции dns_takeover (группа по умолчанию — D-09), параметры
// черновика и признак их отличия — из черновика.
func (s *Service) fillParams(v *View, st configlayer.StateView) {
	p := appliedParams(st)
	v.Zones = append([]string{}, p.Zones...)
	id, name, ok := targetGroup(st.Applied, p)
	v.Group = &GroupRef{ID: id, Name: name, Missing: !ok}

	key := configlayer.SectionDNSTakeover
	dp, err := configlayer.ParseDNSTakeoverParams(st.Draft[key])
	if err != nil {
		dp = configlayer.DNSTakeoverParams{}
	}
	v.DraftParams = &Params{Group: dp.Group, Zones: append([]string{}, dp.Zones...)}
	v.ParamsChanged = !configlayer.SectionEqual(st.Draft[key], st.Applied[key])
}

// defaultUpstreams — апстримы через группу-цель по умолчанию (D-17): DoH по IP.
var defaultUpstreams = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

// upstreams — серверы DNS-секции ядра из применённого состояния: у Xray
// servers (строки или объекты с address), у Mihomo nameserver. Секции нет или
// она пуста — умолчания D-17.
func upstreams(st configlayer.StateView, kernel string) []string {
	out := []string{}
	cs := configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")]
	if kernel != "" && cs.Managed && len(cs.Value) > 0 {
		if obj, err := configlayer.ParseOrderedObject(cs.Value); err == nil {
			key := "nameserver"
			if kernel == configlayer.KernelXray {
				key = "servers"
			}
			if raw, ok := obj.Get(key); ok {
				out = serverList(raw)
			}
		}
	}
	if len(out) == 0 {
		return append(out, defaultUpstreams...)
	}
	return out
}

// serverList разбирает список серверов: строки и объекты с полем address.
func serverList(raw json.RawMessage) []string {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return []string{}
	}
	out := []string{}
	for _, it := range items {
		var str string
		if json.Unmarshal(it, &str) == nil {
			if str != "" {
				out = append(out, str)
			}
			continue
		}
		var obj struct {
			Address string `json:"address"`
		}
		if json.Unmarshal(it, &obj) == nil && obj.Address != "" {
			out = append(out, obj.Address)
		}
	}
	return out
}

// sectionOwner — владелец DNS-секции ядра: panel, если секция под панелью в
// применённом состоянии; иначе у Xray — владелец по скану configs/ (xkeen,
// user, none), у Mihomo — по активному профилю (config.yaml — xkeen, другой —
// user, нет профиля — none).
func (s *Service) sectionOwner(st configlayer.StateView, kernel string) string {
	if configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")].Managed {
		return SectionOwnerPanel
	}
	switch kernel {
	case configlayer.KernelXray:
		scans, _, err := s.d.Layer.ScanXraySections()
		if err != nil {
			return configlayer.OwnerNone
		}
		if sc, ok := scans["dns"]; ok && sc.Owner != "" {
			return sc.Owner
		}
		return configlayer.OwnerNone
	case configlayer.KernelMihomo:
		name, _, _, err := s.d.Layer.MihomoActiveProfile()
		switch {
		case err != nil:
			return configlayer.OwnerNone
		case name == "config.yaml":
			return configlayer.OwnerXKeen
		}
		return configlayer.OwnerUser
	}
	return configlayer.OwnerNone
}

// routerHasIPv6 — у роутера есть глобальный (или уникальный локальный) адрес
// IPv6 на поднятом интерфейсе (D-31).
func routerHasIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if ok && ipn.IP.To4() == nil && ipn.IP.IsGlobalUnicast() {
				return true
			}
		}
	}
	return false
}

// routerHostCheck — функция «хост — адрес интерфейса роутера»: IPv4 и IPv6 всех
// интерфейсов (адреса читаются один раз на вызов). Loopback и «любой адрес» в неё не
// входят: записи на них чистый генератор профиля убирает сам. Адреса интерфейсов
// зависят от окружения, поэтому сверку делают проверка слоя и предпроверка
// включения, а не генератор (IN-08).
func routerHostCheck() func(host string) bool {
	set := map[netip.Addr]bool{}
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				set[ip.Unmap().WithZone("")] = true
			}
		}
	}
	return func(host string) bool {
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return false
		}
		return set[ip.Unmap().WithZone("")]
	}
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

// Источники проверки: ручная проверка и проба транзакции включения (D-29).
const (
	SourceManual = "manual"
	SourceAuto   = "auto"
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
// или своя локальная зона, порт 53 у прошивки, домен или список direct-правила
// (D-21, через прошивку), иначе группа-цель.
func (s *Service) routeOf(ctx context.Context, st configlayer.StateView, name string) route {
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
	if s.d.Routing != nil && s.d.Routing.DNSDirectMatch(ctx, name) {
		return route{path: PathFirmware}
	}
	_, group, _ := targetGroup(st.Applied, p)
	return route{path: PathGroup, group: group}
}

// interceptKernel — ядро, чья DNS-секция действует: ядро-перехватчик, иначе
// активное ядро (xray или mihomo); пусто — ядра нет или запущены оба.
func interceptKernel(st configlayer.StateView, active services.ActiveKernelState) string {
	if live, on := configlayer.ParseDNSTakeoverLive(st.Live); on {
		return live.Kernel
	}
	switch active.Kernel {
	case configlayer.KernelXray, configlayer.KernelMihomo:
		return active.Kernel
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

// checkRouteTimeout — предел поиска домена по спискам direct-правил в ручной
// проверке (как у поиска по каталогу геоданных).
const checkRouteTimeout = 20 * time.Second

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
	// Поиск по спискам dat (D-21) ограничен: обработчик отдаёт контекст без
	// дедлайна и отмены (проверка доводится до записи итога), а индекс большой.
	// Не уложились — путь считается по группе, как при недоступном индексе.
	routeCtx, cancelRoute := context.WithTimeout(ctx, checkRouteTimeout)
	rt := s.routeOf(routeCtx, st, name)
	cancelRoute()
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
	} else if sectionIPv6(st, interceptKernel(st, s.d.Kernel.ActiveState())) {
		// AAAA — дополнение: пустой ответ на AAAA проверку не валит.
		if a6, err := Query(ctx, LoopbackDNS, name, dnsmessage.TypeAAAA, probeAttempt); err == nil && a6.RCode == dnsmessage.RCodeSuccess {
			g.Answers = append(g.Answers, a6.Addrs...)
		}
	}
	res := CheckResult{OK: g.OK, Source: SourceManual, AtMs: s.now().UnixMilli(), Groups: []CheckGroup{g}}

	stored := cloneCheck(res)
	s.saveMu.Lock()
	s.mu.Lock()
	s.st.LastCheck = &stored
	snapshot := s.st
	s.mu.Unlock()
	err = saveState(s.statePath(), snapshot)
	s.saveMu.Unlock()
	if err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.d.Layer.PublishDNS()
	return res, nil
}
