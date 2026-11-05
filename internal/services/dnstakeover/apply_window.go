package dnstakeover

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
)

// Проба и окно подтверждения после пользовательского «Применить» при включённом
// перехвате (D-08, D-26, D-27, D-29). Снимок до применения берёт хук слоя
// BeforeApply под замком применения — только копия и сравнение, без ввода-вывода
// (T-148-43). После успешного применения хук AfterApply пробует оба пути D-29;
// провал — откат только этого применения к снимку без сброса черновика (правки
// пользователя остаются в нём), затем повторная проба, её провал возвращает DNS
// прошивке. Успешная проба и правка DNS-секции ядра-перехватчика, секции
// dns_takeover или группы-цели — окно подтверждения вида apply; прочие правки
// (правила, direct-домены D-21 и т. п.) окна не открывают.

const (
	// opApplyCheck — операция службы: проба после «Применить» и откат по ней.
	opApplyCheck = "apply_check"
	// historyApplyRollback — запись истории: «Применить» откачено по пробе.
	historyApplyRollback = "apply_rollback"
	// reasonApply — причина исхода: проба после «Применить» не прошла.
	reasonApply = "apply"
)

// pendingApply — снимок до пользовательского «Применить» при включённом
// перехвате: к нему возвращает откат по пробе и окно вида apply.
type pendingApply struct {
	// kernel — ядро-перехватчик на момент применения.
	kernel string
	// prevApplied — применённое состояние до применения.
	prevApplied configlayer.Sections
	// prevLive — Live[dns_takeover] до применения (перехват включён: ключ есть).
	prevLive json.RawMessage
	// dnsChanged — применение меняет DNS-секцию ядра-перехватчика, секцию
	// dns_takeover или определение группы-цели: после пробы нужно окно.
	dnsChanged bool
}

// BeforeApply — хук слоя (Hooks.BeforeApply): вызывается синхронно под замком
// применения в StartApply и ApplyEdit с копией состояния (черновик, который
// сейчас применится, прежнее Applied, Live). Перехват включён — запоминается
// снимок; выключен — прежний снимок снимается. Неуспешное применение снимок не
// использует (AfterApply не вызывается), следующий вызов его заменяет. Открытое
// окно этот хук не трогает: при повторном «Применить» в окне openWindow оставляет
// снимок первого неподтверждённого применения.
func (s *Service) BeforeApply(st configlayer.StateView) {
	var p *pendingApply
	if live, on := configlayer.ParseDNSTakeoverLive(st.Live); on {
		// Snapshot слоя отдаёт глубокие копии: секции не копируются второй раз.
		p = &pendingApply{
			kernel:      live.Kernel,
			prevApplied: st.Applied,
			prevLive:    append(json.RawMessage(nil), st.Live[configlayer.LiveKeyDNSTakeover]...),
			dnsChanged:  applyTouchesDNS(st, live.Kernel),
		}
	}
	s.mu.Lock()
	s.pendingApply = p
	s.mu.Unlock()
}

// applyTouchesDNS — черновик отличается от применённого в DNS-секции ядра
// kernel, в секции dns_takeover или в элементе группы-цели секции groups.
func applyTouchesDNS(st configlayer.StateView, kernel string) bool {
	for _, name := range []string{configlayer.CoreSectionName(kernel, "dns"), configlayer.SectionDNSTakeover} {
		if name != "" && !configlayer.SectionEqual(st.Draft[name], st.Applied[name]) {
			return true
		}
	}
	draftParams, err := configlayer.ParseDNSTakeoverParams(st.Draft[configlayer.SectionDNSTakeover])
	if err != nil {
		draftParams = configlayer.DNSTakeoverParams{}
	}
	draftID, _, _ := targetGroup(st.Draft, draftParams)
	appliedID, _, _ := targetGroup(st.Applied, appliedParams(st))
	return !configlayer.SectionEqual(groupItem(st.Draft, draftID), groupItem(st.Applied, appliedID))
}

// groupItem — элемент секции groups с данным id как есть (минимальный разбор по
// id); нет секции, группы или id пуст — nil.
func groupItem(sections configlayer.Sections, id string) json.RawMessage {
	raw, ok := sections[routing.GroupsSection]
	if id == "" || !ok || len(raw) == 0 {
		return nil
	}
	var cfg struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	for _, it := range cfg.Items {
		var g struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(it, &g) == nil && g.ID == id {
			return it
		}
	}
	return nil
}

// AfterApply — хук слоя (Hooks.AfterApply) после успешного применения: только
// пользовательское «Применить» (TriggerUser) со снимком BeforeApply. Применённое
// состояние после применения читается сразу: проба и откат идут, только если
// до начала операции службы его никто не сменил (откат окна, перенос ядра,
// следующее применение). Работа — в горутине службы.
func (s *Service) AfterApply(_ context.Context, info configlayer.AfterApplyInfo) {
	if info.Trigger != configlayer.TriggerUser {
		return
	}
	s.mu.Lock()
	p := s.pendingApply
	s.pendingApply = nil
	s.mu.Unlock()
	if p == nil || s.ctx.Err() != nil {
		return
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		log.Printf("dnstakeover: проба после «Применить»: %v", err)
		return
	}
	post := st.Applied
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.checkApply(*p, post)
	}()
}

// checkApply — проба D-29 после «Применить» операцией службы apply_check:
// успех — last_check (source auto) и, если применение меняло DNS, окно вида apply
// (при уже открытом окне — продление, снимок прежний); провал — откат применения
// (rollbackApply). Пока идёт другая операция службы, проба ждёт её конца.
func (s *Service) checkApply(p pendingApply, post configlayer.Sections) {
	steps := []string{stepProbe}
	if p.dnsChanged {
		steps = append(steps, stepWindow)
	}
	for !s.beginOp(opApplyCheck, steps) {
		if err := sleepCtx(s.ctx, time.Second); err != nil {
			return
		}
	}
	defer s.endOp()

	st, err := s.d.Layer.ReadState()
	if err != nil {
		log.Printf("dnstakeover: проба после «Применить»: %v", err)
		return
	}
	live, on := configlayer.ParseDNSTakeoverLive(st.Live)
	if !on || live.Kernel != p.kernel || !sectionsEqual(st.Applied, post) {
		log.Printf("dnstakeover: проба после «Применить» пропущена: перехват или применённое состояние уже изменились")
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, 4*hookTimeout)
	defer cancel()
	s.setStep(stepProbe, statusRunning)
	res, fail := s.probeTakeover(ctx)
	if s.ctx.Err() != nil {
		// Остановка службы посреди пробы: итог не показателен, откат не нужен.
		return
	}
	s.storeCheck(res)
	if fail != nil {
		s.setStep(stepProbe, statusFailed)
		s.rollbackApply(ctx, p, fail)
		return
	}
	s.setStep(stepProbe, statusDone)
	if !p.dnsChanged {
		return
	}
	proxy, err := ProxyDNS()
	if err != nil {
		log.Printf("dnstakeover: proxy_dns не прочитан: %v", err)
	}
	snap := txnSnapshot{
		AtMs:           s.now().UnixMilli(),
		Kernel:         p.kernel,
		Applied:        p.prevApplied,
		Live:           p.prevLive,
		OverrideBefore: true,
		ProxyDNSBefore: proxy,
	}
	if err := s.openWindow(kindApply, snap); err != nil {
		log.Printf("dnstakeover: окно подтверждения «Применить» не открыто: %v", err)
		s.setStep(stepWindow, statusSkipped)
	}
}

// rollbackApply откатывает «Применить», после которого проба не прошла (D-26):
// шаги операции становятся шагами отката apply, применённое состояние и Live
// перехвата возвращаются к снимку без сброса черновика (D-27: правки остаются в
// нём, перехват остаётся включённым), затем повторная проба; её провал — DNS
// прошивке, состояние waiting с причиной probe, исход probe_failed; открытое
// окно «Применить» не закрывается: его откат вернёт первое неподтверждённое
// применение. Итог — сбой
// пробы с rolled_back=true (или rollback_failed с причиной) и запись истории.
func (s *Service) rollbackApply(ctx context.Context, p pendingApply, probeFail *Failure) {
	s.replaceSteps(rollbackSteps(kindApply))
	s.mu.Lock()
	overrideBefore := s.st.OverrideBefore
	s.mu.Unlock()
	snap := txnSnapshot{Kernel: p.kernel, Applied: p.prevApplied, Live: p.prevLive}
	againFail, errs := s.revertApply(ctx, p.kernel, snap, overrideBefore)
	if s.ctx.Err() != nil {
		// Остановка службы посреди отката: итог не фиксируется.
		log.Printf("dnstakeover: откат «Применить» прерван остановкой службы")
		return
	}

	fail := probeFail
	if againFail != nil {
		fail = againFail
	}
	if len(errs) > 0 {
		fail = &Failure{
			Code:       CodeRollbackFailed,
			RolledBack: false,
			Params:     map[string]string{"cause": fail.Code},
			Detail:     truncateDetail(strings.Join(errs, "\n")),
		}
		log.Printf("dnstakeover: откат «Применить» не удался: %s", strings.Join(errs, "; "))
	} else {
		fail.RolledBack = true
	}
	if againFail != nil {
		// DNS у прошивки: перехват ждёт возврата пробой. Открытое окно «Применить»
		// остаётся: его снимок хранит Applied до первого неподтверждённого
		// применения, и откат по таймеру (rollbackWindow, ветка released при
		// причине ожидания) вернёт его без слушателя :53 и без пробы.
		if err := s.update(func(st *persisted) { st.WaitingReason = waitingProbe }); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
	}
	s.setFailure(fail)
	s.recordHistory(historyApplyRollback, p.kernel, fail)
	if againFail != nil {
		s.recordOutcome(Outcome{Kind: OutcomeProbeFailed, Kernel: p.kernel, Reason: reasonApply}, fail)
	}
}

// revertApply — общий откат «Применить» (окно вида apply и проба после
// применения): RunRestore к снимку без сброса черновика, проба D-29, при её
// провале — DNS роутера прошивке (override, затем файлы ядра без слушателя).
// Шаги apply, probe и release должны быть в текущей операции. Возвращает сбой
// повторной пробы (nil — DNS через ядро работает) и ошибки шагов.
func (s *Service) revertApply(ctx context.Context, kernel string, snap txnSnapshot, overrideBefore bool) (*Failure, []string) {
	var errs []string
	s.setStep(stepApply, statusRunning)
	if _, err := s.restoreWindowSnapshot(ctx, kernel, snap, false); err != nil {
		errs = append(errs, err.Error())
		s.setStep(stepApply, statusFailed)
	} else {
		s.setStep(stepApply, statusDone)
	}
	s.setStep(stepProbe, statusRunning)
	res, fail := s.probeTakeover(ctx)
	s.storeCheck(res)
	s.stepResult(stepProbe, fail == nil)
	if fail == nil {
		return nil, errs
	}
	s.setStep(stepRelease, statusRunning)
	rerr := s.releaseToFirmwareDNS(ctx, kernel, overrideBefore)
	if rerr != nil {
		errs = append(errs, rerr.Error())
	}
	s.stepResult(stepRelease, rerr == nil)
	return fail, errs
}

// replaceSteps заменяет шаги идущей операции (все pending): проба после
// «Применить» переходит в откат с его шагами.
func (s *Service) replaceSteps(ids []string) {
	s.opMu.Lock()
	s.steps = make([]Step, len(ids))
	for i, id := range ids {
		s.steps[i] = Step{ID: id, Status: statusPending}
	}
	s.opMu.Unlock()
	s.d.Layer.PublishDNS()
}

// sectionsEqual — два набора секций равны посекционно (SectionEqual).
func sectionsEqual(a, b configlayer.Sections) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !configlayer.SectionEqual(av, bv) {
			return false
		}
	}
	return true
}
