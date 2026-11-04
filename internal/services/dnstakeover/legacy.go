package dnstakeover

import (
	"context"
	"fmt"
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
