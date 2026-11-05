package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Перехват DNS роутера (фаза 148): GET /api/configlayer/dns — состояние для
// карточки, POST /api/configlayer/dns/<действие> — действия службы. Маршруты
// под сессией (HandleProtected), изменяющие — с X-CSRF-Token; выключенный слой и
// неподключённая служба отвечают 404, как остальные /api/configlayer/*.

// dnsActionPrefix — префикс маршрута действий службы перехвата.
const dnsActionPrefix = "/api/configlayer/dns/"

// dnsErrorStatus — код ответа по коду ошибки службы (коды совпадают с разделом
// codes в frontend/src/lib/dnsErrorCodes.json). Код вне таблицы — 500.
var dnsErrorStatus = map[string]int{
	// Неверный ввод.
	"name_invalid": http.StatusBadRequest,
	// Состояние не позволяет действие.
	"busy":                http.StatusConflict,
	"draft_open":          http.StatusConflict,
	"group_missing":       http.StatusConflict,
	"group_name_invalid":  http.StatusConflict,
	"kernel_conflict":     http.StatusConflict,
	"kernel_stopped":      http.StatusConflict,
	"legacy_blocked":      http.StatusConflict,
	"no_window":           http.StatusConflict,
	"not_enabled":         http.StatusConflict,
	"port_busy":           http.StatusConflict,
	"routing_not_managed": http.StatusConflict,
	"section_foreign":     http.StatusConflict,
	"self_server":         http.StatusConflict,
	"window_active":       http.StatusConflict,
	// Прошивка недоступна или резолвер прошивки не найден.
	"unavailable": http.StatusServiceUnavailable,
	// Транзакция не прошла: конфиг ядра, прошивка, проба.
	"check_failed":      http.StatusUnprocessableEntity,
	"probe_external":    http.StatusUnprocessableEntity,
	"probe_local":       http.StatusUnprocessableEntity,
	"restart_failed":    http.StatusUnprocessableEntity,
	"override_failed":   http.StatusBadGateway,
	"legacy_off_failed": http.StatusBadGateway,
	"rollback_failed":   http.StatusInternalServerError,
	"generic":           http.StatusInternalServerError,
}

// requireDNS — requireLayer плюс подключённая служба перехвата: без службы
// маршруты отвечают 404 not_found.
func (a *API) requireDNS(w http.ResponseWriter, r *http.Request) bool {
	if !a.requireLayer(w, r) {
		return false
	}
	if a.dns == nil {
		JSONErrorCode(w, http.StatusNotFound, "not_found", a.t(r, "error.not_found"))
		return false
	}
	return true
}

// writeDNSError переводит ошибку службы перехвата в HTTP-ответ со стабильным
// кодом и текстом configlayer.dns.<код>. Параметры — имена зон и групп, без
// адресов и значений секций. Ошибки слоя идут по его таблице.
func (a *API) writeDNSError(w http.ResponseWriter, r *http.Request, err error) {
	var de *dnstakeover.Error
	if !errors.As(err, &de) {
		a.writeLayerError(w, r, err)
		return
	}
	status, ok := dnsErrorStatus[de.Code]
	if !ok {
		status = http.StatusInternalServerError
	}
	// Деталь — технический текст (вывод команды прошивки или ядра) без
	// ANSI-последовательностей и адресов со схемой.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error:   a.t(r, "configlayer.dns."+de.Code),
		Code:    de.Code,
		Detail:  maskURLsInText(utils.StripANSI(de.Detail)),
		Params:  de.Params,
	})
}

// ConfigDNS — GET /api/configlayer/dns: состояние перехвата (DNSView).
func (a *API) ConfigDNS(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodGet) || !a.requireDNS(w, r) {
		return
	}
	JSONSuccess(w, a.dns.View())
}

// ConfigDNSAction — POST /api/configlayer/dns/<действие>: check, enable, disable,
// keep («Оставить»: вид; повтор при подтверждённом перехвате — тот же вид, без окна
// — 409 no_window), rollback («Откатить сейчас»: {started:true}; без окна — 409
// no_window), outcome-seen (тост об исходе без пользователя показан: вид),
// legacy-off («Выключить старое перенаправление»: `xkeen -dns off` и перезапуск
// запущенного ядра; вид; сбой — 502 legacy_off_failed). Неизвестное действие —
// 404 not_found.
func (a *API) ConfigDNSAction(w http.ResponseWriter, r *http.Request) {
	if !a.requireDNS(w, r) {
		return
	}
	action := strings.TrimPrefix(r.URL.Path, dnsActionPrefix)
	switch action {
	case "check":
		a.configDNSCheck(w, r)
	case "enable":
		a.configDNSTxn(w, r, a.dns.Enable)
	case "disable":
		a.configDNSTxn(w, r, a.dns.Disable)
	case "keep":
		a.configDNSView(w, r, a.dns.Keep)
	case "rollback":
		a.configDNSTxn(w, r, a.dns.Rollback)
	case "outcome-seen":
		a.configDNSView(w, r, a.dns.OutcomeSeen)
	case "legacy-off":
		a.configDNSView(w, r, a.dns.LegacyOff)
	default:
		JSONErrorCode(w, http.StatusNotFound, "not_found", a.t(r, "error.not_found"))
	}
}

// configDNSTxn — POST /api/configlayer/dns/<enable|disable> с телом {}:
// предпроверки службы синхронно (отказ — 409 для блокировок, 503 для
// unavailable), транзакция — в горутине службы и не обрывается закрытием
// вкладки; ответ {started: true}, ход и итог — в GET /api/configlayer/dns и
// событиях dns.
func (a *API) configDNSTxn(w http.ResponseWriter, r *http.Request, start func(context.Context) error) {
	if !a.requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct{}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	if err := start(context.WithoutCancel(r.Context())); err != nil {
		a.writeDNSError(w, r, err)
		return
	}
	JSONSuccess(w, map[string]bool{"started": true})
}

// configDNSView — POST /api/configlayer/dns/<keep|outcome-seen|legacy-off> с телом {}: действие службы,
// которое отвечает новым видом (DNSView). Действие доводится до конца и при
// закрытой вкладке.
func (a *API) configDNSView(w http.ResponseWriter, r *http.Request, act func(context.Context) (dnstakeover.View, error)) {
	if !a.requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct{}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	v, err := act(context.WithoutCancel(r.Context()))
	if err != nil {
		a.writeDNSError(w, r, err)
		return
	}
	JSONSuccess(w, v)
}

// --- действия с ядром из обработчиков служб (D-14, D-15) ---------------------------
//
// Помощники живут здесь, а не в service.go: обработчики служб только вызывают их.
// Служба перехвата не подключена или слой выключен — команда идёт как прежде.

// dnsLive — служба перехвата подключена и слой включён.
func (a *API) dnsLive() bool {
	return a.dns != nil && a.configLayer != nil && a.configLayer.Enabled()
}

// dnsAroundStop выполняет остановку ядра kernel обёрткой службы перехвата:
// ядро-перехватчик отпускает :53, DNS роутера возвращается прошивке, перехват
// помнится желаемым. Вызывается под замком жизненного цикла обработчика. Ошибка
// службы (*dnstakeover.Error, например busy) — остановка не выполнялась; иначе —
// ошибка самой остановки.
func (a *API) dnsAroundStop(kernel string, stop func() error) error {
	if !a.dnsLive() {
		return stop()
	}
	return a.dns.AroundKernelStop(context.Background(), kernel, stop)
}

// dnsAfterStart — ядро запущено из панели: желаемый перехват возвращается пробой
// без окна подтверждения (в горутине службы, после снятия замка обработчика).
func (a *API) dnsAfterStart() {
	if a.dnsLive() {
		a.dns.AfterKernelStart(context.Background())
	}
}

// dnsAfterSwitch — ядро сменено из панели (переключение подтверждено по
// процессам): желаемый перехват переносится на новое ядро с пробой и окном
// kernel_switch (в горутине службы, после снятия замка обработчика).
func (a *API) dnsAfterSwitch(from, to string) {
	if a.dnsLive() {
		a.dns.TransferToKernel(context.Background(), from, to)
	}
}

// dnsRefused — служба перехвата отказала до действия с ядром: ответ с кодом
// службы записан, true. Иначе (nil или ошибка самой команды) — false.
func (a *API) dnsRefused(w http.ResponseWriter, r *http.Request, err error) bool {
	var de *dnstakeover.Error
	if !errors.As(err, &de) {
		return false
	}
	a.writeDNSError(w, r, err)
	return true
}

// configDNSCheck — POST /api/configlayer/dns/check {name?}: проверка резолва
// через 127.0.0.1:53 (DNSCheckResult). Пустое имя — имя по умолчанию; не
// доменное имя — 400 name_invalid; идёт другая проверка — 409 busy. Проверка
// доводится до конца и при закрытой вкладке: итог станет последней проверкой.
func (a *API) configDNSCheck(w http.ResponseWriter, r *http.Request) {
	if !a.requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !a.decodeLayerBody(w, r, &req) {
		return
	}
	res, err := a.dns.Check(context.WithoutCancel(r.Context()), req.Name)
	if err != nil {
		a.writeDNSError(w, r, err)
		return
	}
	JSONSuccess(w, res)
}
