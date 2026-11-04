package dnstakeover

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Стык со старым перенаправлением DNS XKeen (D-33…D-36; RESEARCH «proxy_dns —
// поправка к D-33/D-36»). Строка proxy_dns живёт в init-скрипте S05xkeen, и правила
// перехвата XKeen с :53 строятся по ней при старте ядра: смена строки без
// перезапуска запущенного ядра не действует. Поэтому каждая смена строки здесь
// заканчивается KernelApplier.ApplyLocked под замком жизненного цикла;
// остановленное ядро не запускается (FND-05). Код старого переключателя
// (SetDNSProxying, /api/service/dns-redirect) не трогается: его удаляет Phase 151.

// CodeLegacyOffFailed — старое перенаправление не выключилось (dnsErrorCodes.json).
const CodeLegacyOffFailed = "legacy_off_failed"

// opLegacyOff — операция службы и вид записи истории кнопки «Выключить старое
// перенаправление».
const opLegacyOff = "legacy_off"

// LegacyOff — «Выключить старое перенаправление» (D-35): proxy_dns включили мимо
// панели. Строка off — тот же вид без изменений. Иначе операцией службы
// `xkeen -dns off`, затем перезапуск запущенного активного ядра, чтобы XKeen
// перестроил правила без :53 (перехват панели при этом остаётся включённым);
// исход снимается, запись истории, событие dns. Вызывается только кнопкой
// пользователя: сама панель чужую строку не меняет. Сбой — legacy_off_failed с
// причиной в params.reason.
func (s *Service) LegacyOff(ctx context.Context) (View, error) {
	on, err := ProxyDNS()
	if err != nil {
		return View{}, legacyOffError("proxy_dns: " + err.Error())
	}
	if !on {
		return s.View(), nil
	}
	layer := s.d.Layer.Snapshot()
	if layer.Apply.Running || layer.BusyHolder != "" || s.checking.Load() {
		return View{}, errCode(CodeBusy)
	}
	if !s.beginOp(opLegacyOff, nil) {
		return View{}, errCode(CodeBusy)
	}
	s.clearOutcome()
	active := s.d.Kernel.ActiveState()
	kernel := active.Kernel

	fail := func() *Failure {
		hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookTimeout)
		defer cancel()
		if err := s.fw.SetProxyDNS(hctx, false); err != nil {
			return &Failure{Code: CodeLegacyOffFailed, Params: map[string]string{"reason": legacyReason(err.Error())}, Detail: truncateDetail(err.Error())}
		}
		if kernel != configlayer.KernelXray && kernel != configlayer.KernelMihomo {
			return nil
		}
		if s.d.Applier == nil {
			msg := "перезапуск ядра: KernelApplier не задан"
			return &Failure{Code: CodeLegacyOffFailed, Params: map[string]string{"reason": msg}, Detail: msg}
		}
		// XKeen перестраивает правила только при старте ядра.
		s.d.Lifecycle.Lock()
		res := s.d.Applier.ApplyLocked(kernel)
		s.d.Lifecycle.Unlock()
		if res.Outcome == services.ApplyRestartFailed {
			msg := fmt.Sprintf("перезапуск %s после xkeen -dns off: %s", kernel, res.Error)
			return &Failure{Code: CodeLegacyOffFailed, Params: map[string]string{"reason": legacyReason(msg)}, Detail: truncateDetail(msg)}
		}
		return nil
	}()
	if fail != nil {
		s.setFailure(fail)
	}
	s.recordHistory(opLegacyOff, kernel, fail)
	s.endOp()
	if fail != nil {
		return View{}, &Error{Code: fail.Code, Params: fail.Params, Detail: fail.Detail}
	}
	return s.View(), nil
}

// legacyOffError — сбой выключения старого перенаправления до начала операции.
func legacyOffError(reason string) *Error {
	return &Error{Code: CodeLegacyOffFailed, Params: map[string]string{"reason": legacyReason(reason)}, Detail: truncateDetail(reason)}
}

// legacyReason — короткая причина для params.reason: первая строка, не длиннее
// 200 байт (полный текст — в detail).
func legacyReason(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	const limit = 200
	for len(s) > limit {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// opFlagOff — операция службы: возврат DNS прошивке при выключении флага слоя.
const opFlagOff = "flag_off"

// ReleaseForFlagOff — возврат DNS прошивке и строки proxy_dns перед выключением
// флага слоя (D-36, D-04 из 144): обработчик настроек вызывает его до переноса
// файлов, ошибка отменяет выключение флага. Перехвата нет (не включён, не желаем,
// окна нет) — сразу nil.
//
// Синхронно, операцией службы: окно подтверждения закрывается без отката (таймер
// остановлен); шаги выключения — ядро без слушателя (запущенное перезапускается,
// остановленное не запускается), override к значению до включения, проверка ответа
// прошивки через 127.0.0.1:53; desired=false, снимок снимается. Затем строка
// proxy_dns получает значение до включения перехвата, и если строка изменилась,
// запущенное ядро перезапускается KernelApplier.ApplyLocked: XKeen строит прежний
// перехват с :53 только при старте ядра. DNS прошивке возвращается первым и тогда,
// когда шаг ядра не удался.
func (s *Service) ReleaseForFlagOff(ctx context.Context) error {
	pl, proxyBefore, needed, err := s.flagOffPlan()
	if err != nil || !needed {
		return err
	}
	if !s.beginOp(opFlagOff, disableSteps) {
		return errCode(CodeBusy)
	}
	defer s.endOp()
	if !s.claimFlagOffWindow() {
		return &Error{Code: CodeBusy, Detail: "идёт откат окна подтверждения"}
	}
	s.clearOutcome()
	s.runDisable(ctx, pl)
	s.closeFlagOffWindow()
	if f := s.opFailure(); f != nil {
		return &Error{Code: f.Code, Params: f.Params, Detail: f.Detail}
	}
	changed, err := s.restoreProxyLine(ctx, proxyBefore)
	if err != nil {
		return s.flagOffFailed(CodeGeneric, nil, err)
	}
	if kernel := s.d.Kernel.ActiveState().Kernel; changed && (kernel == configlayer.KernelXray || kernel == configlayer.KernelMihomo) {
		if s.d.Applier == nil {
			return s.flagOffFailed(CodeRestartFailed, kernelParam(kernel), errors.New("перезапуск ядра: KernelApplier не задан"))
		}
		s.d.Lifecycle.Lock()
		res := s.d.Applier.ApplyLocked(kernel)
		s.d.Lifecycle.Unlock()
		if res.Outcome == services.ApplyRestartFailed {
			return s.flagOffFailed(CodeRestartFailed, kernelParam(kernel), fmt.Errorf("перезапуск %s после возврата proxy_dns: %s", kernel, res.Error))
		}
	}
	if err := s.update(func(p *persisted) { p.ProxyDNSBefore = false }); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	return nil
}

// flagOffPlan — что возвращать при выключении флага слоя: план шагов выключения,
// proxy_dns до включения перехвата (из окна включения, снимка или состояния) и
// нужен ли возврат вообще.
func (s *Service) flagOffPlan() (pl disablePlan, proxyBefore, needed bool, err error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return disablePlan{}, false, false, err
	}
	live, on := configlayer.ParseDNSTakeoverLive(st.Live)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !on && !s.st.Desired && s.st.Window == nil {
		return disablePlan{}, false, false, nil
	}
	pl = disablePlan{kernel: s.st.Kernel, live: on, overrideBefore: s.st.OverrideBefore}
	proxyBefore = s.st.ProxyDNSBefore
	if on {
		pl.kernel = live.Kernel
	}
	switch {
	case s.st.Window != nil && s.st.Window.Kind == kindEnable:
		pl.overrideBefore = s.st.Window.Snapshot.OverrideBefore
		proxyBefore = s.st.Window.Snapshot.ProxyDNSBefore
	case s.st.Snapshot != nil:
		pl.overrideBefore = s.st.Snapshot.OverrideBefore
		proxyBefore = s.st.Snapshot.ProxyDNSBefore
	}
	return pl, proxyBefore, true, nil
}

// claimFlagOffWindow забирает открытое окно подтверждения без отката: таймер
// остановлен, флаг отката держит окно, пока идёт возврат. false — окно уже
// откатывает таймер или «Откатить сейчас». Окна нет — true.
func (s *Service) claimFlagOffWindow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Window == nil {
		return true
	}
	if s.winRolling {
		return false
	}
	s.winRolling = true
	s.stopWindowTimerLocked()
	return true
}

// closeFlagOffWindow закрывает окно, забранное claimFlagOffWindow: перехват
// выключен, откатывать окну больше нечего.
func (s *Service) closeFlagOffWindow() {
	if err := s.update(func(p *persisted) { p.Window = nil }); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	s.mu.Lock()
	s.winRolling = false
	s.mu.Unlock()
}

// opFailure — копия сбоя идущей операции; nil — сбоя нет.
func (s *Service) opFailure() *Failure {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.failure == nil {
		return nil
	}
	f := *s.failure
	return &f
}

// restoreProxyLine приводит строку proxy_dns к want; changed — строку пришлось менять.
func (s *Service) restoreProxyLine(ctx context.Context, want bool) (changed bool, err error) {
	cur, err := ProxyDNS()
	if err != nil {
		return false, fmt.Errorf("proxy_dns не прочитан: %w", err)
	}
	if cur == want {
		return false, nil
	}
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hookTimeout)
	defer cancel()
	if err := s.fw.SetProxyDNS(hctx, want); err != nil {
		return false, err
	}
	return true, nil
}

// flagOffFailed — сбой после возврата DNS прошивке (строка proxy_dns или
// перезапуск ядра): сбой в виде, запись истории, ошибка с кодом для обработчика.
func (s *Service) flagOffFailed(code string, params map[string]string, err error) error {
	f := &Failure{Code: code, Params: params, Detail: truncateDetail(err.Error())}
	s.setFailure(f)
	s.recordHistory(opFlagOff, "", f)
	return &Error{Code: f.Code, Params: f.Params, Detail: f.Detail}
}
