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
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
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
// probe, window (окно подтверждения, window.go); выключение — apply, release, verify.
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

// Операции службы; действие истории XKeen (GET /api/service/restart-log) —
// actionPrefix и вид записи: dns_takeover_enable, dns_takeover_keep и т. д.
const (
	opEnable     = "enable"
	opDisable    = "disable"
	actionPrefix = "dns_takeover_"
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
var enableSteps = []string{stepValidate, stepSnapshot, stepWrite, stepRestart, stepProbe, stepWindow}

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
// applying; открыто окно подтверждения — window; перехват включён — on; перехват
// желаем, а DNS у прошивки — waiting с причиной; иначе остаётся off или unavailable.
func (s *Service) fillOp(v *View, on bool, waiting string) {
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
	case v.Window != nil:
		v.State = StateWindow
	case on:
		v.State = StateOn
	case waiting != "":
		v.State = StateWaiting
		v.WaitingReason = waiting
	}
}

// update меняет состояние службы под замком и записывает его на диск. fn не
// вызывает update и не берёт saveMu: запись идёт под saveMu до конца.
func (s *Service) update(fn func(p *persisted)) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	fn(&s.st)
	if len(s.st.History) > historyLimit {
		// История в памяти не растёт сверх предела файла (saveState урезает лишь копию).
		s.st.History = append([]HistoryEntry(nil), s.st.History[len(s.st.History)-historyLimit:]...)
	}
	snapshot := s.st
	s.mu.Unlock()
	return saveState(s.statePath(), snapshot)
}

// recordHistory пишет одну запись истории на транзакцию или действие окна: в
// историю действий XKeen (RecordAction, GET /api/service/restart-log) с действием
// dns_takeover_<op> и в History файла состояния.
func (s *Service) recordHistory(op, kernel string, fail *Failure) {
	action := actionPrefix + op
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
	s.clearOutcome()
	ctx = context.WithoutCancel(ctx)
	if !s.spawn(func() {
		defer s.endOp()
		s.runEnable(ctx, pl)
	}) {
		s.endOp()
		return errStopping()
	}
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
	if s.windowOpen() {
		return enablePlan{}, errCode(CodeWindowActive)
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
		s.finishEnable(ctx, pl, &Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}, rollbackPlan{})
		return
	}
	sections, merge, err := s.enableSections(st, pl)
	if err != nil {
		s.finishEnable(ctx, pl, &Failure{Code: CodeGeneric, RolledBack: true, Detail: truncateDetail(err.Error())}, rollbackPlan{})
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
		s.finishEnable(ctx, pl, fail, rollbackPlan{restarted: pipelineRestarted(view)})
		return
	}

	s.setStep(stepProbe, statusRunning)
	res, fail, stopped := s.probeUntilStop(ctx)
	if stopped {
		return
	}
	s.storeCheck(res)
	if fail != nil {
		s.finishEnable(ctx, pl, fail, rollbackPlan{restarted: true, restore: true})
		return
	}
	s.setStep(stepProbe, statusDone)
	s.finishEnable(ctx, pl, nil, rollbackPlan{})
}

// pipelineRestarted — проход дошёл до шага перезапуска: правила XKeen с тех пор
// могли перестроиться по строке proxy_dns.
func pipelineRestarted(view configlayer.ApplyView) bool {
	for _, sv := range view.Steps {
		if sv.ID == configlayer.StepRestart && sv.State != configlayer.StepPending && sv.State != configlayer.StepSkipped {
			return true
		}
	}
	return false
}

// rollbackPlan — что нужно отката сбою включения: restore — применение уже
// зафиксировано (сбой пробы), откат идёт RunRestore к снимку; restarted — ядро
// перезапускалось после смены строки proxy_dns.
type rollbackPlan struct {
	restore   bool
	restarted bool
}

// finishEnable фиксирует итог включения: успех — desired и ядро, затем окно
// подтверждения 60 с со снимком включения (DNSF-05: состояние window до
// «Оставить»); сбой — шаг в работе failed, откат к снимку и failure. Одна запись
// истории на транзакцию по её итогу.
func (s *Service) finishEnable(ctx context.Context, pl enablePlan, fail *Failure, rb rollbackPlan) {
	if fail == nil {
		var snap *txnSnapshot
		if err := s.update(func(p *persisted) {
			p.Desired = true
			p.Kernel = pl.kernel
			p.KernelWasRunning = true
			p.DisableFailed = false
			snap = p.Snapshot
			if snap == nil {
				// Без снимка окно не открывается и OverrideBefore с ProxyDNSBefore сам
				// openWindow не запишет: выключение и сверка берут их из состояния.
				p.OverrideBefore = pl.overrideBefore
				p.ProxyDNSBefore = pl.proxyDNSBefore
			}
		}); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
		s.recordHistory(opEnable, pl.kernel, nil)
		if snap == nil {
			// Без снимка откатывать не к чему: перехват остаётся включённым без окна.
			log.Printf("dnstakeover: снимка включения нет, окно подтверждения не открыто")
			s.setStep(stepWindow, statusSkipped)
			return
		}
		win := *snap
		win.InFlight = false
		if err := s.openWindow(kindEnable, win); err != nil {
			log.Printf("dnstakeover: окно подтверждения не открыто: %v", err)
			s.settleSnapshot()
		}
		return
	}
	s.failRunning()
	s.setFailure(fail)
	s.rollbackEnable(ctx, pl, fail, rb)
	s.setFailure(fail)
	s.recordHistory(opEnable, pl.kernel, fail)
}

// rollbackEnable возвращает DNS роутера, строку proxy_dns и конфиг ядра к снимку
// после сбоя включения (DNSF-03).
//
// Отказ проверки ядрами (check_failed): прошивка, файлы и строка не тронуты —
// остаётся сверка. Сбой BeforeWrite (override_failed): файлы не записаны, ядро не
// перезапускалось — сверка выключает override и возвращает строку. Сбой записи и
// перезапуска: override и строку вернул BeforeRollback до повторного подъёма ядра
// (runTakeover). Сбой пробы (restore): первым — DNS прошивке (Pitfall 2), вторым —
// строка proxy_dns (до перезапуска отката), затем RunRestore к снимку: его
// перезапуск запущенного ядра строит прежний перехват XKeen с :53.
//
// В конце при любом сбое — идемпотентная сверка: override и строка proxy_dns
// равны снимку; если строку пришлось вернуть здесь, после перезапуска, а ядро
// запущено, оно перезапускается KernelApplier.ApplyLocked под замком жизненного
// цикла (остановленное не запускается, FND-05), чтобы XKeen перестроил перехват.
// Откат удался — failure.rolled_back=true и снимок снимается; не удался —
// rolled_back=false и код rollback_failed с причиной (прежний код — params.cause).
func (s *Service) rollbackEnable(ctx context.Context, pl enablePlan, fail *Failure, rb rollbackPlan) {
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*hookTimeout)
	defer cancel()
	var errs []string
	note := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	restarted := rb.restarted
	if rb.restore {
		if !pl.overrideBefore {
			note(s.fw.SetOverride(hctx, false))
		}
		if pl.proxyDNSBefore {
			note(s.fw.SetProxyDNS(hctx, true))
		}
		note(s.restoreSnapshot(hctx, pl))
		restarted = true
	}
	proxyLate, err := s.reconcileFirmware(hctx, pl)
	note(err)
	if proxyLate && restarted {
		note(s.rebuildXKeenRules(pl.kernel))
	}

	switch {
	case len(errs) > 0:
		if fail.Code != CodeRollbackFailed {
			if fail.Params == nil {
				fail.Params = map[string]string{}
			}
			fail.Params["cause"] = fail.Code
		}
		fail.Code = CodeRollbackFailed
		fail.RolledBack = false
		fail.Detail = truncateDetail(strings.TrimSpace(fail.Detail + "\n" + strings.Join(errs, "\n")))
		log.Printf("dnstakeover: откат включения не удался: %s", strings.Join(errs, "; "))
	case rb.restore:
		fail.RolledBack = true
	}
	if err := s.update(func(p *persisted) {
		if fail.RolledBack {
			p.Snapshot = nil
		} else if p.Snapshot != nil && s.ctx.Err() == nil {
			// Откат не удался (не из-за остановки службы — тогда старт панели
			// откатит снимок заново): снимок остаётся записью override и proxy_dns до
			// включения (сверка на старте, выключение флага слоя), но проход
			// завершён — откатывать по нему на старте панели нельзя (CR-04).
			p.Snapshot.InFlight = false
		}
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
}

// settleSnapshot помечает снимок завершённого прохода: он остаётся записью override и
// proxy_dns до включения, но старт панели по нему ничего не откатывает (CR-04).
func (s *Service) settleSnapshot() {
	if err := s.update(func(p *persisted) {
		if p.Snapshot != nil {
			p.Snapshot.InFlight = false
		}
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
}

// reconcileFirmware приводит override и строку proxy_dns к значениям до
// включения; proxyLate — строку пришлось вернуть здесь.
func (s *Service) reconcileFirmware(ctx context.Context, pl enablePlan) (proxyLate bool, err error) {
	var errs []string
	if !pl.overrideBefore {
		on, rerr := s.fw.OverrideEnabled(ctx)
		switch {
		case rerr != nil:
			errs = append(errs, rerr.Error())
		case on:
			if serr := s.fw.SetOverride(ctx, false); serr != nil {
				errs = append(errs, serr.Error())
			}
		}
	}
	cur, rerr := ProxyDNS()
	switch {
	case rerr != nil:
		errs = append(errs, "proxy_dns: "+rerr.Error())
	case cur != pl.proxyDNSBefore:
		if serr := s.fw.SetProxyDNS(ctx, pl.proxyDNSBefore); serr != nil {
			errs = append(errs, serr.Error())
		} else {
			proxyLate = true
		}
	}
	if len(errs) > 0 {
		return proxyLate, errors.New(strings.Join(errs, "; "))
	}
	return proxyLate, nil
}

// restoreSnapshot возвращает применённое состояние и Live перехвата к снимку
// включения (RunRestore с TriggerDNSRestore): секции DNS ядра и dns_takeover в
// черновике возвращаются вместе с ним, если пользователь их не правил.
func (s *Service) restoreSnapshot(ctx context.Context, pl enablePlan) error {
	s.mu.Lock()
	snap := s.st.Snapshot
	s.mu.Unlock()
	if snap == nil {
		return errors.New("откат к снимку: снимка нет")
	}
	_, err := s.restoreWindowSnapshot(ctx, pl.kernel, *snap, true)
	return err
}

// enableSections — подменяемые секции включения и их слияние в черновик: секция
// dns_takeover без группы получает группу по умолчанию (D-09), DNS-секция ядра —
// недостающие умолчания перехвата (D-19, cores.DNSTakeoverDefaults). Черновик
// закрыт (D-01), поэтому слияние пишет в него те же значения, что в Applied:
// дописанное видно в форме секции.
func (s *Service) enableSections(st configlayer.StateView, pl enablePlan) (configlayer.Sections, func(string, json.RawMessage) (json.RawMessage, error), error) {
	p := appliedParams(st)
	raw, err := takeoverParams(p, pl.groupID)
	if err != nil {
		return nil, nil, err
	}
	sections := configlayer.Sections{configlayer.SectionDNSTakeover: raw}
	dnsSection := configlayer.CoreSectionName(pl.kernel, "dns")
	cs := configlayer.DesiredCores(st.Applied)[dnsSection]
	value, filled, err := cores.DNSTakeoverDefaults(pl.kernel, cs)
	if err != nil {
		return nil, nil, err
	}
	if len(filled) > 0 {
		cs.Value = value
		enc, err := configlayer.EncodeCoreSection(cs)
		if err != nil {
			return nil, nil, err
		}
		sections[dnsSection] = enc
	}
	merge := func(name string, draft json.RawMessage) (json.RawMessage, error) {
		switch name {
		case configlayer.SectionDNSTakeover:
			dp, err := configlayer.ParseDNSTakeoverParams(draft)
			if err != nil {
				return draft, nil
			}
			return takeoverParams(dp, pl.groupID)
		case dnsSection:
			dcs, err := configlayer.ParseCoreSection(draft)
			if err != nil || !dcs.Managed {
				return draft, nil
			}
			v, _, err := cores.DNSTakeoverDefaults(pl.kernel, dcs)
			if err != nil {
				return draft, nil
			}
			dcs.Value = v
			return configlayer.EncodeCoreSection(dcs)
		}
		return draft, nil
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
		InFlight:       true,
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

// --- выключение -------------------------------------------------------------

// disableSteps — шаги выключения в фактическом порядке.
var disableSteps = []string{stepApply, stepRelease, stepVerify}

// disablePlan — итог предпроверок выключения.
type disablePlan struct {
	kernel         string
	live           bool
	overrideBefore bool
	// lineOnly — перехвата нет, возвращать нужно только строку proxy_dns: шаги
	// выключения (runDisable) пропускаются.
	lineOnly bool
}

// Disable — выключение перехвата без окна подтверждения (D-08): предпроверки
// синхронно, затем в горутине службы — apply (ядро отпускает :53: запись без
// слушателя и перезапуск запущенного ядра, остановленное не запускается), release
// (override к значению до включения из снимка), verify (прошивка отвечает через
// 127.0.0.1:53). Сбой apply не мешает вернуть DNS прошивке. Сбой любого шага
// оставляет перехват желаемым с причиной ожидания disable_failed: выключение
// повторяется той же кнопкой. proxy_dns при выключении не меняется: его
// возвращают откат (D-33) и выключение флага слоя (D-36).
func (s *Service) Disable(ctx context.Context) error {
	pl, err := s.disablePreflight(ctx)
	if err != nil {
		return err
	}
	if !s.beginOp(opDisable, disableSteps) {
		return errCode(CodeBusy)
	}
	s.clearOutcome()
	ctx = context.WithoutCancel(ctx)
	if !s.spawn(func() {
		defer s.endOp()
		s.runDisable(ctx, pl)
	}) {
		s.endOp()
		return errStopping()
	}
	return nil
}

// disablePreflight — предпроверки выключения: перехват включён или желаем
// (not_enabled), прошивка доступна, нет конфликта ядер, не идёт применение или
// операция службы.
func (s *Service) disablePreflight(ctx context.Context) (disablePlan, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return disablePlan{}, err
	}
	live, on := configlayer.ParseDNSTakeoverLive(st.Live)
	s.mu.Lock()
	desired, kernel, snap, overrideBefore := s.st.Desired, s.st.Kernel, s.st.Snapshot, s.st.OverrideBefore
	window := s.st.Window != nil
	s.mu.Unlock()
	if !on && !desired {
		return disablePlan{}, errCode(CodeNotEnabled)
	}
	// Окно подтверждения закрывают «Оставить» или «Откатить сейчас».
	if window {
		return disablePlan{}, errCode(CodeWindowActive)
	}
	if ok, reason := s.fw.Available(ctx); !ok {
		return disablePlan{}, &Error{Code: CodeUnavailable, Detail: reason}
	}
	if s.d.Kernel.ActiveState().Conflict {
		return disablePlan{}, errCode(CodeKernelConflict)
	}
	layer := s.d.Layer.Snapshot()
	if layer.Apply.Running || layer.BusyHolder != "" || s.checking.Load() || s.opActive() {
		return disablePlan{}, errCode(CodeBusy)
	}
	pl := disablePlan{kernel: kernel, live: on, overrideBefore: overrideBefore}
	if on {
		pl.kernel = live.Kernel
	}
	if snap != nil {
		pl.overrideBefore = snap.OverrideBefore
	}
	return pl, nil
}

// waitingDisableFailed — причина ожидания: выключение перехвата не удалось, его
// можно повторить (persisted.DisableFailed).
const waitingDisableFailed = "disable_failed"

// runDisable — транзакция выключения в горутине службы.
func (s *Service) runDisable(ctx context.Context, pl disablePlan) {
	var fail *Failure
	if pl.live {
		s.setStep(stepApply, statusRunning)
		view, err := s.runWithoutListener(ctx, pl.kernel)
		if fail = takeoverFailure(view, err, nil); fail != nil {
			s.setStep(stepApply, statusFailed)
		} else {
			s.setStep(stepApply, statusDone)
		}
	} else {
		s.setStep(stepApply, statusSkipped)
	}

	// DNS роутера прошивке — и при сбое apply.
	s.setStep(stepRelease, statusRunning)
	hctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	var relErr error
	if !pl.overrideBefore {
		if on, err := s.fw.OverrideEnabled(hctx); err != nil || on {
			relErr = s.fw.SetOverride(hctx, false)
		}
	}
	if relErr != nil {
		s.setStep(stepRelease, statusFailed)
		if fail == nil {
			fail = &Failure{Code: CodeOverrideFailed, Detail: truncateDetail(relErr.Error())}
		}
	} else {
		s.setStep(stepRelease, statusDone)
	}

	s.setStep(stepVerify, statusRunning)
	if err := s.verifyFirmwareDNS(hctx); err != nil {
		s.setStep(stepVerify, statusFailed)
		if fail == nil {
			fail = &Failure{Code: CodeOverrideFailed, Detail: truncateDetail(err.Error())}
		}
	} else {
		s.setStep(stepVerify, statusDone)
	}

	if err := s.update(func(p *persisted) {
		if fail != nil {
			// Выключение не удалось: перехват остаётся желаемым, иначе повторить его
			// нельзя (not_enabled), выключение флага слоя сочтёт возврат ненужным, а
			// override, включённый панелью, не снимет уже никто. override и proxy_dns
			// до включения сохраняются для повтора.
			p.Desired = true
			if pl.kernel != "" {
				p.Kernel = pl.kernel
			}
			p.DisableFailed = true
			return
		}
		p.Desired = false
		p.Kernel = ""
		p.KernelWasRunning = false
		p.WaitingReason = ""
		p.DisableFailed = false
		p.Snapshot = nil
		p.OverrideBefore = false
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	if fail != nil {
		s.setFailure(fail)
	}
	s.recordHistory(opDisable, pl.kernel, fail)
}

// verifyFirmwareDNS ждёт до overrideSettle, пока прошивка ответит через
// 127.0.0.1:53 (служебный домен; домен неизвестен — резолвер прошивки держит :53).
func (s *Service) verifyFirmwareDNS(ctx context.Context) error {
	deadline := s.now().Add(overrideSettle)
	for !s.fw.port53Settled(ctx, false) {
		if !s.now().Before(deadline) {
			return fmt.Errorf("прошивка за %v не ответила через %s", overrideSettle, LoopbackDNS)
		}
		if err := sleepCtx(ctx, overridePoll); err != nil {
			return err
		}
	}
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
		// Старое перенаправление XKeen (proxy_dns) снимается этим же запуском: его
		// правила с :53 пересобираются только полным перезапуском ядра.
		ForceFullRestart: tr.ProxyDNSBefore,
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
	return strings.Contains(low, "address already in use") || strings.Contains(low, "bind: address")
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

// probeUntilStop — проба включения, возврата или переноса перехвата (probeTakeover),
// которую прерывает остановка службы. stopped — службу остановили посреди пробы:
// итог не фиксируется, снимок прохода остаётся в файле состояния, и старт панели
// откатывает проход (rollbackPendingTxn). Остановка панели поэтому не ждёт пробу
// (до 40 с) дольше своего срока.
func (s *Service) probeUntilStop(ctx context.Context) (CheckResult, *Failure, bool) {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	res, fail := s.probeTakeover(pctx)
	if s.ctx.Err() != nil {
		log.Printf("dnstakeover: проба прервана остановкой службы — откат при старте панели")
		return res, fail, true
	}
	return res, fail, false
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
