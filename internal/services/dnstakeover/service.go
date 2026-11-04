package dnstakeover

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

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
	}, nil
}

// Start запускает фоновую работу службы: прогрев сведений о прошивке (ndmc
// отвечает до 3 с, поэтому не на пути запроса и не на пути старта панели).
func (s *Service) Start() {
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
	v.ServerNowMs = s.now().UnixMilli()
	return v
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
