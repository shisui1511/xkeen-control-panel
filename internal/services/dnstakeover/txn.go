package dnstakeover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Транзакция включения перехвата DNS роутера (DNSF-02, D-01, RESEARCH Pattern 3):
// предпроверки синхронно, затем в горутине службы — проверка конфигурации обоими
// ядрами, снимок, override прошивки, запись, перезапуск запущенного ядра и проба
// обоих путей (D-29). Запись и перезапуск идут конвейером слоя (RunSectionLive с
// живыми данными перехвата), команды прошивки — в его хуках BeforeWrite и
// BeforeRollback. Адрес резолвера прошивки — по решению владельца D-01 =
// fact-closed (148-SPIKE): только чтением до первой команды прошивки, один проход.

// Коды отказа и сбоя транзакций (frontend/src/lib/dnsErrorCodes.json).
const (
	CodeCheckFailed      = "check_failed"
	CodeDraftOpen        = "draft_open"
	CodeGroupMissing     = "group_missing"
	CodeGroupNameInvalid = "group_name_invalid"
	CodeKernelConflict   = "kernel_conflict"
	CodeKernelStopped    = "kernel_stopped"
	CodeNotEnabled       = "not_enabled"
	CodeOverrideFailed   = "override_failed"
	CodePortBusy         = "port_busy"
	CodeProbeExternal    = "probe_external"
	CodeProbeLocal       = "probe_local"
	CodeRestartFailed    = "restart_failed"
	CodeRollbackFailed   = "rollback_failed"
	CodeSectionForeign   = "section_foreign"
	CodeWindowActive     = "window_active"
)

// Шаги транзакций (DNSStep.id): включение — validate, snapshot, write, restart,
// probe (окно подтверждения добавляет 148-16); выключение — apply, release, verify.
const (
	stepValidate = "validate"
	stepSnapshot = "snapshot"
	stepWrite    = "write"
	stepRestart  = "restart"
	stepProbe    = "probe"
	stepApply    = "apply"
	stepRelease  = "release"
	stepVerify   = "verify"
)

// Статусы шага (DNSStep.status).
const (
	statusPending = "pending"
	statusRunning = "running"
	statusDone    = "done"
	statusFailed  = "failed"
	statusSkipped = "skipped"
)

// Операции службы и действия истории XKeen (GET /api/service/restart-log).
const (
	opEnable      = "enable"
	opDisable     = "disable"
	actionEnable  = "dns_takeover_enable"
	actionDisable = "dns_takeover_disable"
)

const (
	// maxDetail — предел failure.detail и вывода команд (4 КБ последних байт).
	maxDetail = 4096
	// hookTimeout — предел команд прошивки и XKeen в одном хуке конвейера.
	hookTimeout = 90 * time.Second
	// probeLabelExternal и probeLabelLocal — группы строк пробы включения.
	probeLabelExternal = "external"
	probeLabelLocal    = "local"
)

// enableSteps — шаги включения в порядке показа.
var enableSteps = []string{stepValidate, stepSnapshot, stepWrite, stepRestart, stepProbe}

// truncateDetail оставляет не больше maxDetail последних байт, не разрезая
// UTF-8-символ.
func truncateDetail(s string) string {
	if len(s) <= maxDetail {
		return s
	}
	s = s[len(s)-maxDetail:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// --- ход операции -----------------------------------------------------------

// opActive — идёт транзакция службы.
func (s *Service) opActive() bool {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.op != ""
}

// beginOp начинает операцию с шагами ids (все pending) и сбрасывает прошлый сбой;
// false — уже идёт другая операция (busy, T-148-30).
func (s *Service) beginOp(op string, ids []string) bool {
	s.opMu.Lock()
	if s.op != "" {
		s.opMu.Unlock()
		return false
	}
	s.op = op
	s.steps = make([]Step, len(ids))
	for i, id := range ids {
		s.steps[i] = Step{ID: id, Status: statusPending}
	}
	s.failure = nil
	s.opMu.Unlock()
	s.d.Layer.PublishDNS()
	return true
}

// endOp закрывает операцию: шаги, которые так и не начались, — skipped.
func (s *Service) endOp() {
	s.opMu.Lock()
	s.op = ""
	for i := range s.steps {
		if s.steps[i].Status == statusPending {
			s.steps[i].Status = statusSkipped
		}
	}
	s.opMu.Unlock()
	s.d.Layer.PublishDNS()
}

// setStep меняет статус шага и публикует событие dns. Завершённый шаг (done,
// failed) назад в running не возвращается: события конвейера приходят и после
// хуков службы.
func (s *Service) setStep(id, status string) {
	s.opMu.Lock()
	changed := false
	for i := range s.steps {
		if s.steps[i].ID != id || s.steps[i].Status == status {
			continue
		}
		cur := s.steps[i].Status
		if status == statusRunning && (cur == statusDone || cur == statusFailed) {
			continue
		}
		s.steps[i].Status = status
		changed = true
	}
	s.opMu.Unlock()
	if changed {
		s.d.Layer.PublishDNS()
	}
}

// failRunning отмечает шаг в работе как failed: на нём транзакция остановилась.
func (s *Service) failRunning() {
	s.opMu.Lock()
	for i := range s.steps {
		if s.steps[i].Status == statusRunning {
			s.steps[i].Status = statusFailed
		}
	}
	s.opMu.Unlock()
}

// setFailure запоминает сбой транзакции для View.
func (s *Service) setFailure(f *Failure) {
	s.opMu.Lock()
	s.failure = f
	s.opMu.Unlock()
	s.d.Layer.PublishDNS()
}

// fillOp заполняет ход и итог транзакции и состояние перехвата: идёт операция —
// applying; перехват включён — on; иначе остаётся off или unavailable.
func (s *Service) fillOp(v *View, on bool) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	v.Steps = append([]Step{}, s.steps...)
	if s.failure != nil {
		f := *s.failure
		if f.Params != nil {
			p := make(map[string]string, len(f.Params))
			for k, val := range f.Params {
				p[k] = val
			}
			f.Params = p
		}
		v.Failure = &f
	}
	switch {
	case s.op != "":
		v.State = StateApplying
	case v.State == StateUnavailable:
	case on:
		v.State = StateOn
	}
}

// update меняет состояние службы под замком и записывает его на диск.
func (s *Service) update(fn func(p *persisted)) error {
	s.mu.Lock()
	fn(&s.st)
	snapshot := s.st
	s.mu.Unlock()
	return saveState(s.statePath(), snapshot)
}

// recordHistory пишет одну запись истории на транзакцию: в историю действий
// XKeen (RecordAction, GET /api/service/restart-log) и в History файла состояния.
func (s *Service) recordHistory(op, kernel string, fail *Failure) {
	action := actionEnable
	if op == opDisable {
		action = actionDisable
	}
	var err error
	code, detail := "", ""
	if fail != nil {
		code, detail = fail.Code, fail.Detail
		err = errors.New(fail.Code)
	}
	s.d.XKeen.RecordAction(action, detail, err)
	entry := HistoryEntry{Kind: op, AtMs: s.now().UnixMilli(), OK: fail == nil, Code: code, Kernel: kernel}
	if err := s.update(func(p *persisted) { p.History = append(p.History, entry) }); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
}

// storeCheck сохраняет итог проверки резолва как последнюю проверку.
func (s *Service) storeCheck(res CheckResult) {
	stored := cloneCheck(res)
	if err := s.update(func(p *persisted) { p.LastCheck = &stored }); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.d.Layer.PublishDNS()
}

// --- включение --------------------------------------------------------------

// enablePlan — итог предпроверок включения.
type enablePlan struct {
	kernel         string
	groupID        string
	groupName      string
	overrideBefore bool
	proxyDNSBefore bool
}

// Enable — включение перехвата (D-01): предпроверки синхронно, отказ — *Error с
// кодом сразу; затем транзакция в горутине службы под ctx без отмены (закрытая
// вкладка её не обрывает). Ход и итог — в View и событиях dns.
func (s *Service) Enable(ctx context.Context) error {
	pl, err := s.enablePreflight(ctx)
	if err != nil {
		return err
	}
	if !s.beginOp(opEnable, enableSteps) {
		return errCode(CodeBusy)
	}
	ctx = context.WithoutCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.endOp()
		s.runEnable(ctx, pl)
	}()
	return nil
}

// enablePreflight — предпроверки включения в порядке блокировок UI-SPEC:
// прошивка доступна, конфликт ядер, идёт применение или операция службы,
// открыт черновик (D-01), ядро остановлено (D-04: не запускается), DNS-секцию
// ядра ведёт не панель, группы-цели нет (D-09) или её имя нельзя поставить в
// суффикс DoH Mihomo (routing.DNSGroupNameURLSafe — то же правило, что у
// генератора и проверки слоя), резолвер прошивки не найден (fact-closed: адрес
// только чтением, прошивка не тронута). Здесь же читаются override и proxy_dns
// до включения.
func (s *Service) enablePreflight(ctx context.Context) (enablePlan, error) {
	if ok, reason := s.fw.Available(ctx); !ok {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: reason}
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return enablePlan{}, err
	}
	active := s.d.Kernel.ActiveState()
	snap := s.d.Layer.Snapshot()
	switch {
	case active.Conflict:
		return enablePlan{}, errCode(CodeKernelConflict)
	case snap.Apply.Running || snap.BusyHolder != "" || s.checking.Load() || s.opActive():
		return enablePlan{}, errCode(CodeBusy)
	}
	if _, on := configlayer.ParseDNSTakeoverLive(st.Live); on {
		return enablePlan{}, &Error{Code: CodeBusy, Detail: "перехват уже включён"}
	}
	if snap.DraftChanges > 0 {
		return enablePlan{}, errCode(CodeDraftOpen)
	}
	kernel := active.Kernel
	if (kernel != configlayer.KernelXray && kernel != configlayer.KernelMihomo) || !s.kernelRunning(active) {
		return enablePlan{}, errCode(CodeKernelStopped)
	}
	if !configlayer.DesiredCores(st.Applied)[configlayer.CoreSectionName(kernel, "dns")].Managed {
		return enablePlan{}, &Error{Code: CodeSectionForeign, Params: map[string]string{"kernel": kernel}}
	}
	p := appliedParams(st)
	id, name, ok := targetGroup(st.Applied, p)
	if !ok {
		ref := name
		if ref == "" {
			ref = p.Group
		}
		return enablePlan{}, &Error{Code: CodeGroupMissing, Params: map[string]string{"group": ref}}
	}
	if kernel == configlayer.KernelMihomo && !routing.DNSGroupNameURLSafe(name) {
		return enablePlan{}, &Error{Code: CodeGroupNameInvalid, Params: map[string]string{"name": name}}
	}
	if _, err := s.fw.ResolverAddr(ctx); err != nil {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: err.Error()}
	}
	over, err := s.fw.OverrideEnabled(ctx)
	if err != nil {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: err.Error()}
	}
	proxy, err := ProxyDNS()
	if err != nil {
		return enablePlan{}, &Error{Code: CodeUnavailable, Detail: "proxy_dns: " + err.Error()}
	}
	return enablePlan{kernel: kernel, groupID: id, groupName: name, overrideBefore: over, proxyDNSBefore: proxy}, nil
}

// runEnable — транзакция включения в горутине службы.
func (s *Service) runEnable(ctx context.Context, pl enablePlan) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		s.finishEnable(ctx, pl, &Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())})
		return
	}
	sections, merge, err := s.enableSections(st, pl)
	if err != nil {
		s.finishEnable(ctx, pl, &Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())})
		return
	}
	var (
		snapErr  error
		undoMu   sync.Mutex
		undoErrs []string
	)
	view, err := s.runTakeover(ctx, takeoverRun{
		Kernel:         pl.kernel,
		Sections:       sections,
		MergeDraft:     merge,
		OverrideBefore: pl.overrideBefore,
		ProxyDNSBefore: pl.proxyDNSBefore,
		BeforeOverride: func(context.Context) error {
			snapErr = s.takeSnapshot(pl)
			return snapErr
		},
		OnUndoError: func(err error) {
			undoMu.Lock()
			undoErrs = append(undoErrs, err.Error())
			undoMu.Unlock()
		},
	})
	if fail := takeoverFailure(view, err, snapErr); fail != nil {
		if len(undoErrs) > 0 {
			fail.Detail = truncateDetail(strings.TrimSpace(fail.Detail + "\n" + strings.Join(undoErrs, "\n")))
		}
		s.finishEnable(ctx, pl, fail)
		return
	}

	s.setStep(stepProbe, statusRunning)
	res, fail := s.probeTakeover(ctx)
	s.storeCheck(res)
	if fail != nil {
		s.finishEnable(ctx, pl, fail)
		return
	}
	s.setStep(stepProbe, statusDone)
	s.finishEnable(ctx, pl, nil)
}

// finishEnable фиксирует итог включения: успех — desired, ядро и состояние on;
// сбой — шаг в работе failed и failure. Одна запись истории на транзакцию.
func (s *Service) finishEnable(_ context.Context, pl enablePlan, fail *Failure) {
	if fail == nil {
		if err := s.update(func(p *persisted) {
			p.Desired = true
			p.Kernel = pl.kernel
			p.KernelWasRunning = true
		}); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
		s.recordHistory(opEnable, pl.kernel, nil)
		return
	}
	s.failRunning()
	s.setFailure(fail)
	s.recordHistory(opEnable, pl.kernel, fail)
}

// enableSections — подменяемые секции включения и их слияние в черновик: секция
// dns_takeover без группы получает группу по умолчанию (D-09).
func (s *Service) enableSections(st configlayer.StateView, pl enablePlan) (configlayer.Sections, func(string, json.RawMessage) (json.RawMessage, error), error) {
	p := appliedParams(st)
	raw, err := takeoverParams(p, pl.groupID)
	if err != nil {
		return nil, nil, err
	}
	sections := configlayer.Sections{configlayer.SectionDNSTakeover: raw}
	merge := func(name string, draft json.RawMessage) (json.RawMessage, error) {
		if name != configlayer.SectionDNSTakeover {
			return draft, nil
		}
		dp, err := configlayer.ParseDNSTakeoverParams(draft)
		if err != nil {
			return draft, nil
		}
		return takeoverParams(dp, pl.groupID)
	}
	return sections, merge, nil
}

// takeoverParams — значение секции dns_takeover с группой по умолчанию вместо
// пустой и списком зон вместо null.
func takeoverParams(p configlayer.DNSTakeoverParams, defaultGroup string) (json.RawMessage, error) {
	if p.Group == "" {
		p.Group = defaultGroup
	}
	if p.Zones == nil {
		p.Zones = []string{}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("секция %s: %w", configlayer.SectionDNSTakeover, err)
	}
	return raw, nil
}

// takeSnapshot записывает снимок до включения в файл состояния: прежнее Applied,
// прежнее Live перехвата, override и proxy_dns до включения. Вызывается после
// проверки ядрами и до первой команды прошивки (D-01).
func (s *Service) takeSnapshot(pl enablePlan) error {
	s.setStep(stepSnapshot, statusRunning)
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return fmt.Errorf("снимок: %w", err)
	}
	var prevLive json.RawMessage
	if raw, ok := st.Live[configlayer.LiveKeyDNSTakeover]; ok {
		prevLive = append(json.RawMessage(nil), raw...)
	}
	snap := &txnSnapshot{
		AtMs:           s.now().UnixMilli(),
		Kernel:         pl.kernel,
		Applied:        st.Applied,
		Live:           prevLive,
		OverrideBefore: pl.overrideBefore,
		ProxyDNSBefore: pl.proxyDNSBefore,
	}
	if err := s.update(func(p *persisted) { p.Snapshot = snap }); err != nil {
		return fmt.Errorf("снимок: %w", err)
	}
	s.setStep(stepSnapshot, statusDone)
	return nil
}

// --- общий путь включения ---------------------------------------------------

// takeoverRun — параметры прохода включения перехвата.
type takeoverRun struct {
	// Kernel — ядро-перехватчик.
	Kernel string
	// Sections — подменяемые секции запуска (dns_takeover, секция DNS ядра).
	Sections configlayer.Sections
	// MergeDraft — слияние подменённых секций в черновик; nil — черновик не меняется.
	MergeDraft func(name string, draft json.RawMessage) (json.RawMessage, error)
	// OverrideBefore — override прошивки до прохода: откат его не выключает.
	OverrideBefore bool
	// ProxyDNSBefore — строка proxy_dns до прохода: on — BeforeWrite её выключает
	// (D-33), откат возвращает.
	ProxyDNSBefore bool
	// BeforeOverride вызывается после проверки ядрами и до первой команды
	// прошивки (снимок включения); ошибка отменяет проход без касания файлов.
	BeforeOverride func(ctx context.Context) error
	// OnUndoError получает ошибки отката прошивки и строки proxy_dns в
	// BeforeRollback: откат конвейера при этом продолжается.
	OnUndoError func(err error)
}

// runTakeover — единственное место пакета, где собираются живые данные
// включённого перехвата и вызывается проход RunSectionLive с BeforeWrite и
// BeforeRollback прошивки. Его вызывают включение (Enable), возврат перехвата
// (D-14, D-16) и перенос на другое ядро (D-15).
//
// Live собирается заново при каждом вызове: firmware_resolver — свежий
// ResolverAddr, service_domain, ipv6 (HasIPv6); прежнее Live[dns_takeover] для
// адреса не читается (после перезагрузки или остановки ядра override снят, и
// прежний адрес не виден или устарел). Решение D-01 = fact-closed: адрес
// резолвера прошивки читается до любой команды прошивки, и проход один; при уже
// включённом override (перенос D-15) — так же: адрес после override виден, а
// SetOverride(true) в BeforeWrite идемпотентен. Override до проверки ядрами не
// включается ни в одном варианте.
func (s *Service) runTakeover(ctx context.Context, tr takeoverRun) (configlayer.ApplyView, error) {
	addr, err := s.fw.ResolverAddr(ctx)
	if err != nil {
		return configlayer.ApplyView{}, &Error{Code: CodeUnavailable, Detail: err.Error()}
	}
	live, err := json.Marshal(configlayer.DNSTakeoverLive{
		Enabled:          true,
		Kernel:           tr.Kernel,
		FirmwareResolver: addr,
		ServiceDomain:    s.fw.ServiceDomain(),
		IPv6:             s.fw.HasIPv6(),
	})
	if err != nil {
		return configlayer.ApplyView{}, err
	}
	section := configlayer.CoreSectionName(tr.Kernel, "dns")
	var touchedOverride, touchedProxy atomic.Bool
	report := func(err error) {
		log.Printf("dnstakeover: откат: %v", err)
		if tr.OnUndoError != nil {
			tr.OnUndoError(err)
		}
	}

	stop := s.watchPipelineSteps()
	view, err := s.d.Layer.RunSectionLive(ctx, configlayer.SectionLiveRun{
		Trigger:    configlayer.TriggerDNSTakeover,
		Kernel:     tr.Kernel,
		Sections:   tr.Sections,
		MergeDraft: tr.MergeDraft,
		Live:       configlayer.Sections{configlayer.LiveKeyDNSTakeover: live},
		// Пока ждали замок применения, секцию могли отпустить, а черновик — открыть.
		Guard: func(st configlayer.StateView) error {
			if !configlayer.DesiredCores(st.Applied)[section].Managed {
				return &Error{Code: CodeSectionForeign, Params: map[string]string{"kernel": tr.Kernel}}
			}
			if draftOpen(st) {
				return errCode(CodeDraftOpen)
			}
			return nil
		},
		// Порядок D-01: проверка ядрами → снимок → override → запись. Отказ проверки
		// сюда не доходит, поэтому прошивка при нём не тронута.
		BeforeWrite: func(pctx context.Context) error {
			s.setStep(stepValidate, statusDone)
			if tr.BeforeOverride != nil {
				if err := tr.BeforeOverride(pctx); err != nil {
					return err
				}
			}
			s.setStep(stepWrite, statusRunning)
			hctx, cancel := context.WithTimeout(context.WithoutCancel(pctx), hookTimeout)
			defer cancel()
			touchedOverride.Store(true)
			if err := s.fw.SetOverride(hctx, true); err != nil {
				return err
			}
			if tr.ProxyDNSBefore {
				touchedProxy.Store(true)
				if err := s.fw.SetProxyDNS(hctx, false); err != nil {
					return err
				}
			}
			return nil
		},
		// Откат конвейера (сбой записи, перезапуска, фиксации): первым — DNS
		// прошивке (Pitfall 2), вторым — строка proxy_dns, до повторного подъёма
		// ядра на прежних файлах: его перезапуск строит прежний перехват XKeen с :53.
		BeforeRollback: func() {
			hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookTimeout)
			defer cancel()
			if touchedOverride.Load() && !tr.OverrideBefore {
				if err := s.fw.SetOverride(hctx, false); err != nil {
					report(err)
				}
			}
			if touchedProxy.Load() {
				if err := s.fw.SetProxyDNS(hctx, true); err != nil {
					report(err)
				}
			}
		},
	})
	stop()
	s.mapPipelineSteps(view.Steps)
	return view, err
}

// draftOpen — в черновике есть правки относительно применённого состояния.
func draftOpen(st configlayer.StateView) bool {
	for k, d := range st.Draft {
		if a, ok := st.Applied[k]; !ok || !configlayer.SectionEqual(d, a) {
			return true
		}
	}
	for k := range st.Applied {
		if _, ok := st.Draft[k]; !ok {
			return true
		}
	}
	return false
}

// watchPipelineSteps переводит шаги конвейера запуска dns_takeover в шаги
// транзакции, пока идёт проход; stop останавливает слежение. Подписаться не
// удалось — шаги переводятся по итогу прохода.
func (s *Service) watchPipelineSteps() (stop func()) {
	ch, cancel, err := s.d.Layer.Subscribe()
	if err != nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if sv, isStep := ev.Data.(configlayer.StepView); isStep && ev.Type == configlayer.EventApplyStep && sv.Trigger == configlayer.TriggerDNSTakeover {
					s.mapPipelineStep(sv)
				}
			}
		}
	}()
	return func() {
		close(done)
		cancel()
		<-finished
	}
}

// mapPipelineSteps переводит итоговые шаги конвейера в шаги транзакции.
func (s *Service) mapPipelineSteps(steps []configlayer.StepView) {
	for _, sv := range steps {
		s.mapPipelineStep(sv)
	}
}

// mapPipelineStep — шаг конвейера в шаг транзакции: сборка и проверки обоими
// ядрами — validate, запись — write, перезапуск — restart.
func (s *Service) mapPipelineStep(sv configlayer.StepView) {
	id := ""
	switch sv.ID {
	case configlayer.StepBuild, configlayer.StepValidateXray, configlayer.StepValidateMihomo:
		id = stepValidate
		if sv.State == configlayer.StepDone || sv.State == configlayer.StepSkipped {
			return // validate завершает BeforeWrite: после последней проверки
		}
	case configlayer.StepWrite:
		id = stepWrite
	case configlayer.StepRestart:
		id = stepRestart
	default:
		return
	}
	switch sv.State {
	case configlayer.StepRunning:
		s.setStep(id, statusRunning)
	case configlayer.StepDone, configlayer.StepSkipped, configlayer.StepDeferred:
		s.setStep(id, statusDone)
	case configlayer.StepFailed:
		s.setStep(id, statusFailed)
	}
}

// takeoverFailure сопоставляет итог прохода с кодом сбоя; nil — проход успешен.
// snapErr — снимок не записался: прошивка не тронута, код generic.
func takeoverFailure(view configlayer.ApplyView, err error, snapErr error) *Failure {
	if err != nil {
		var de *Error
		switch {
		case errors.As(err, &de):
			return &Failure{Code: de.Code, RolledBack: true, Params: de.Params, Detail: truncateDetail(de.Detail)}
		case errors.Is(err, configlayer.ErrApplyBusy), errors.Is(err, configlayer.ErrKernelBusy), errors.Is(err, configlayer.ErrGeodataBusy):
			return &Failure{Code: CodeBusy, RolledBack: true}
		case errors.Is(err, configlayer.ErrDraftOpen):
			return &Failure{Code: CodeDraftOpen, RolledBack: true}
		}
		if view.Result == nil {
			return &Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}
		}
	}
	res := view.Result
	if res == nil {
		return &Failure{Code: CodeGeneric, RolledBack: true, Detail: "итог применения не получен"}
	}
	if res.OK {
		return nil
	}
	detail := truncateDetail(res.Message)
	switch {
	case res.Code == configlayer.ResultBuildFailed || strings.HasPrefix(res.Code, "validation_"):
		// Отказ сборки или проверки ядром: прошивка и файлы не тронуты.
		return &Failure{Code: CodeCheckFailed, RolledBack: true, Params: kernelParam(res.Kernel), Detail: detail}
	case res.Code == configlayer.ResultBeforeWriteFailed:
		if snapErr != nil {
			return &Failure{Code: CodeGeneric, RolledBack: true, Detail: detail}
		}
		return &Failure{Code: CodeOverrideFailed, RolledBack: true, Detail: detail}
	case res.Code == configlayer.ResultRestartFailed || res.Code == configlayer.ResultKernelNotRecovered:
		code := CodeRestartFailed
		if portBusy(res.Message) {
			code = CodePortBusy // Pitfall 1: порт 53 занят
		}
		return &Failure{Code: code, RolledBack: res.Code == configlayer.ResultRestartFailed && res.RolledBack, Params: kernelParam(res.Kernel), Detail: detail}
	case res.Code == configlayer.ResultRollbackFailed:
		return &Failure{Code: CodeRollbackFailed, RolledBack: false, Params: kernelParam(res.Kernel), Detail: detail}
	}
	return &Failure{Code: CodeGeneric, RolledBack: res.RolledBack, Detail: truncateDetail(res.Code + ": " + res.Message)}
}

// kernelParam — параметры сбоя с ядром; пустое ядро — без параметров.
func kernelParam(kernel string) map[string]string {
	if kernel == "" {
		return nil
	}
	return map[string]string{"kernel": kernel}
}

// portBusy — в выводе ядра признак занятого адреса.
func portBusy(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "address already in use") || strings.Contains(low, "bind")
}

// --- проба ------------------------------------------------------------------

// probeTakeover — проба обоих путей после включения (D-29): внешнее имя через
// группу и служебный домен роутера через прошивку, оба через 127.0.0.1:53.
// Итог — проверка с source auto и сбой первого неудачного пути.
func (s *Service) probeTakeover(ctx context.Context) (CheckResult, *Failure) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return CheckResult{Source: SourceAuto, AtMs: s.now().UnixMilli(), Groups: []CheckGroup{}},
			&Failure{Code: CodeGeneric, Detail: truncateDetail(err.Error())}
	}
	var fail *Failure
	ext := s.probeName(ctx, st, probeLabelExternal, DefaultCheckName)
	groups := []CheckGroup{ext}
	if !ext.OK {
		fail = &Failure{Code: CodeProbeExternal, Params: map[string]string{"name": ext.Name, "group": ext.Group}, Detail: ext.Reason}
	}
	if domain := s.fw.ServiceDomain(); domain != "" {
		loc := s.probeName(ctx, st, probeLabelLocal, domain)
		groups = append(groups, loc)
		if !loc.OK && fail == nil {
			fail = &Failure{Code: CodeProbeLocal, Params: map[string]string{"name": loc.Name}, Detail: loc.Reason}
		}
	}
	return CheckResult{OK: fail == nil, Source: SourceAuto, AtMs: s.now().UnixMilli(), Groups: groups}, fail
}

// probeName разрешает имя через 127.0.0.1:53 с повторами (Resolve) и описывает
// путь по применённому состоянию.
func (s *Service) probeName(ctx context.Context, st configlayer.StateView, label, name string) CheckGroup {
	rt := s.routeOf(ctx, st, name)
	ans, err := Resolve(ctx, name, dnsmessage.TypeA)
	g := CheckGroup{
		Label:     label,
		Name:      name,
		OK:        err == nil,
		LatencyMs: latencyMs(ans.Latency),
		Path:      rt.path,
		Answers:   append([]string{}, ans.Addrs...),
		Zone:      rt.zone,
		Group:     rt.group,
	}
	if err != nil {
		g.Reason = failReason(ans, err)
	}
	return g
}
