package dnstakeover

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Стык со старым перенаправлением DNS XKeen (D-33…D-36; RESEARCH «proxy_dns —
// поправка к D-33/D-36»). Строка proxy_dns живёт в init-скрипте S05xkeen, и правила
// перехвата XKeen с :53 строятся по ней при старте ядра: смена строки без
// перезапуска запущенного ядра не действует. Поэтому каждая смена строки здесь
// заканчивается KernelApplier.ApplyLocked под замком жизненного цикла;
// остановленное ядро не запускается (FND-05). Своего переключателя строки у
// панели нет (DNSF-06): строку меняют только транзакция перехвата и кнопка
// LegacyOff.

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
		if err := s.restartConfirmed(ctx, kernel); err != nil {
			msg := fmt.Sprintf("перезапуск %s после xkeen -dns off: %v", kernel, err)
			return &Failure{Code: CodeLegacyOffFailed, Params: map[string]string{"reason": legacyReason(msg)}, Detail: truncateDetail(msg)}
		}
		return nil
	}()
	if fail != nil {
		s.setFailure(fail)
	}
	if fail == nil {
		// Пользователь сам выключил строку: возвращать её к значению до включения
		// перехвата (откат, сторож, сверка) больше не нужно.
		if err := s.update(func(p *persisted) {
			p.ProxyDNSBefore = false
			p.ProxyRestartPending = false
		}); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
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

// returnProxyLine — общий помощник возврата строки proxy_dns: строка получает
// значение want, и если она менялась (или прежний перезапуск не довели,
// ProxyRestartPending), запущенное активное ядро перезапускается KernelApplier.
// ApplyLocked: XKeen строит прежний перехват с :53 только при старте ядра,
// остановленное ядро не запускается. Строка возвращена, перезапуск ещё впереди —
// сбой между ними не теряется (ProxyRestartPending в файле). ProxyDNSBefore и
// ProxyRestartPending снимаются только после успеха обоих шагов. Сбой — *Error:
// generic (строка не прочитана или не записана) или restart_failed. Его зовут все
// пути, делающие перехват нежелаемым: откат переноса и окна переноса, возврат DNS
// прошивке сторожем, сверка на старте.
func (s *Service) returnProxyLine(ctx context.Context, want bool) *Error {
	changed, err := s.restoreProxyLine(ctx, want)
	if err != nil {
		return &Error{Code: CodeGeneric, Detail: truncateDetail(err.Error())}
	}
	if changed {
		// Строка возвращена, перезапуск ещё впереди: сбой между ними не должен
		// потеряться при следующем возврате.
		if err := s.update(func(p *persisted) { p.ProxyRestartPending = true }); err != nil {
			log.Printf("dnstakeover: %v", err)
		}
	}
	s.mu.Lock()
	restart := changed || s.st.ProxyRestartPending
	s.mu.Unlock()
	if kernel := s.d.Kernel.ActiveState().Kernel; restart && (kernel == configlayer.KernelXray || kernel == configlayer.KernelMihomo) {
		if s.d.Applier == nil {
			return &Error{Code: CodeRestartFailed, Params: kernelParam(kernel), Detail: "перезапуск ядра: KernelApplier не задан"}
		}
		if err := s.restartConfirmed(ctx, kernel); err != nil {
			return &Error{Code: CodeRestartFailed, Params: kernelParam(kernel), Detail: truncateDetail(fmt.Sprintf("перезапуск %s после возврата proxy_dns: %v", kernel, err))}
		}
	}
	if err := s.update(func(p *persisted) {
		p.ProxyDNSBefore = false
		p.ProxyRestartPending = false
	}); err != nil {
		log.Printf("dnstakeover: %v", err)
	}
	return nil
}

// releaseProxyLine возвращает строку proxy_dns к значению до включения перехвата
// (ProxyDNSBefore из состояния службы), когда перехват становится нежелаемым.
// Строка до включения не была включена — ничего. Вызывается после того, как DNS
// у прошивки и файлы ядра без слушателя: перезапуск ядра строит прежние правила
// XKeen с :53 только на таких файлах.
func (s *Service) releaseProxyLine(ctx context.Context) error {
	s.mu.Lock()
	before := s.st.ProxyDNSBefore
	s.mu.Unlock()
	if !before {
		return nil
	}
	if e := s.returnProxyLine(ctx, true); e != nil {
		return errors.New(e.Detail)
	}
	return nil
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
