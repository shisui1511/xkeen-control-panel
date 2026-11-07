//go:build router

// router-area: dns

package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Включение и выключение перехвата DNS роутера на настоящей панели устройства
// (DNSF-02, DNSF-03): POST /api/configlayer/dns/enable|disable, ход транзакции в
// GET /api/configlayer/dns. Вердикт — факты устройства: кто держит порт 53
// (/proc), ответ DNS через 127.0.0.1:53, слушатель панели в файлах ядра,
// override в рабочей конфигурации прошивки, строка proxy_dns init-скрипта XKeen.
// Подготовка: слой включён, подписка стенда в источниках, маршрутизация и
// DNS-секция активного ядра у панели, группа «Прокси» с узлами подписки. Стенд
// возвращается в t.Cleanup при любом исходе: перехват выключен, override как до
// теста, взятое отпущено, подписка удалена, черновик сброшен. Адреса и имена узлов
// в журнал теста не попадают.

const (
	rtDNSEnable   = rtDNSBase + "/enable"
	rtDNSDisable  = rtDNSBase + "/disable"
	rtDNSKeepPath = rtDNSBase + "/keep"
	// rtDNSStateWait — сколько ждать конца транзакции перехвата.
	rtDNSStateWait = 180 * time.Second
	// rtDNSXrayListenerFile — файл слушателя :53 панели в каталоге Xray (148-11).
	rtDNSXrayListenerFile = "03_inbounds.xcp-dns.json"
	// rtDNSOverrideLine — строка override в рабочей конфигурации прошивки.
	rtDNSOverrideLine = "opkg dns-override"
	// rtDNSProxyGroup — группа-цель перехвата по умолчанию (D-09).
	rtDNSProxyGroup = "Прокси"
)

// rtDNSNdmc выполняет константную команду прошивки без оболочки.
func rtDNSNdmc(t *testing.T, cmd string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "ndmc", "-c", cmd)
	if code != 0 {
		t.Fatalf("ndmc -c %q: код %d: %s", cmd, code, strings.TrimSpace(stderr))
	}
	return stdout
}

// rtDNSOverrideOn читает override из рабочей конфигурации прошивки (строка
// плоская или внутри раздела opkg).
func rtDNSOverrideOn(t *testing.T) bool {
	t.Helper()
	for _, line := range strings.Split(rtDNSNdmc(t, "show running-config"), "\n") {
		switch strings.TrimSpace(line) {
		case rtDNSOverrideLine, "dns-override":
			return true
		}
	}
	return false
}

// rtDNSKernelHasListener — в файлах ядра есть слушатель :53 панели: у Xray —
// файл входящего перехвата в каталоге конфигурации, у Mihomo — dns.listen на :53
// в профиле панели.
func rtDNSKernelHasListener(kernel string) bool {
	switch kernel {
	case configlayer.KernelXray:
		_, err := os.Stat(filepath.Join(rtRtXrayDir, rtDNSXrayListenerFile))
		return err == nil
	case configlayer.KernelMihomo:
		return rtDNSMihomoFileListens(filepath.Join(rtRtMihomoDir, rtRtPanelProfile))
	}
	return false
}

// rtDNSResolves — имя разрешается через 127.0.0.1:53 (с повторами в окне пробы).
func rtDNSResolves(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := dnstakeover.Resolve(ctx, name, dnsmessage.TypeA)
	return err == nil
}

// rtDNSStand — подготовленный стенд перехвата и то, что надо вернуть.
type rtDNSStand struct {
	e              *rtSrc
	kernel         string
	overrideBefore bool
	subID          string
	routingTaken   bool
	xrayDNSTaken   bool
}

// rtDNSPrepare готовит стенд перехвата для ядра kernel: слой включён, подписка
// стенда в источниках, маршрутизация взята, DNS-секция ядра у панели, группа
// «Прокси» есть. Без подписки стенда — пропуск: узла для группы-цели нет.
func rtDNSPrepare(t *testing.T, e *rtSrc, kernel string) *rtDNSStand {
	t.Helper()
	subURL := os.Getenv("XCP_RT_SUBSCRIPTION_URL")
	if subURL == "" {
		t.Skip("нет XCP_RT_SUBSCRIPTION_URL: группе-цели нужен узел подписки стенда")
	}
	e.enableLayer()
	e.rtRstIdle()
	// Перехват, оставшийся включённым от прерванного прогона (тест остановлен посреди
	// сценария, t.Cleanup не выполнялся), блокирует очистку: применение без маршрутизации
	// при включённом перехвате отвергается (dns_routing_required).
	e.rtDNSEnsureOff()
	e.rtRtPurge()
	// Подписка, оставшаяся от прерванного прогона, не даёт группам по умолчанию
	// появиться вместе с подпиской стенда: они создаются только с первым источником.
	for i := 0; i < 5; i++ {
		id := e.rtDNSSubscriptionID()
		if id == "" {
			break
		}
		t.Logf("подготовка перехвата: удаляется подписка, оставшаяся от прежнего прогона")
		if r := e.call(http.MethodPost, rtSourcesBase+"/delete", map[string]any{"revision": e.revision(), "id": id}); r.Status != http.StatusOK {
			t.Fatalf("удаление оставшейся подписки: %d %s", r.Status, r.Code)
		}
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtRtApplyOK("подготовка перехвата: оставшаяся подписка")
	}
	s := &rtDNSStand{e: e, kernel: kernel, overrideBefore: rtDNSOverrideOn(t)}
	t.Cleanup(func() { s.cleanup(t) })

	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": subURL})
	if pv.Status != http.StatusOK {
		t.Fatalf("sources/preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); add.Status != http.StatusOK {
		t.Fatalf("sources/add: %d %s", add.Status, add.Code)
	}
	s.subID = e.rtDNSSubscriptionID()
	if s.subID == "" {
		t.Fatal("подписки стенда нет в источниках после добавления")
	}
	take := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()})
	if take.Status != http.StatusOK {
		t.Fatalf("routing/take: %d %s", take.Status, take.Code)
	}
	s.routingTaken = true
	if kernel == configlayer.KernelXray {
		r := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}, "ack_comments": true})
		if r.Status != http.StatusOK {
			t.Fatalf("take dns (xray): %d %s", r.Status, r.Code)
		}
		s.xrayDNSTaken = true
	}
	e.rtRtApplyOK("подготовка перехвата DNS")
	if owner := e.rtDNSCoresOwner(kernel); owner != "panel" {
		t.Fatalf("DNS-секция %s после подготовки у %q, ожидалась panel", kernel, owner)
	}
	if rtRtFindGroup(e.rtRtView(), rtDNSProxyGroup) == nil {
		t.Fatalf("после подготовки нет группы %q", rtDNSProxyGroup)
	}
	return s
}

// rtDNSSubscriptionID — id первой подписки (не «Моих узлов») в источниках.
func (e *rtSrc) rtDNSSubscriptionID() string {
	e.t.Helper()
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	if view.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtSourcesBase, view.Status, view.Code)
	}
	srcs, _ := view.object(e.t)["sources"].([]any)
	for _, it := range srcs {
		src, _ := it.(map[string]any)
		if src["kind"] != "my" {
			id, _ := src["id"].(string)
			return id
		}
	}
	return ""
}

// cleanup возвращает стенд: перехват выключен, override как до подготовки,
// DNS-секция Xray и маршрутизация отпущены, подписка удалена, секция dns_takeover
// убрана, следы маршрутизации вычищены.
func (s *rtDNSStand) cleanup(t *testing.T) {
	c := s.e.with(t)
	c.rtRstIdle()
	c.rtDNSEnsureOff()
	if rtDNSOverrideOn(t) && !s.overrideBefore {
		rtDNSNdmc(t, "no "+rtDNSOverrideLine)
	}
	if s.xrayDNSTaken {
		if r := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "dns"}); r.Status != http.StatusOK {
			t.Errorf("очистка: release dns (xray): %d %s", r.Status, r.Code)
		}
	}
	if s.routingTaken {
		if r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()}); r.Status != http.StatusOK {
			t.Errorf("очистка: routing/release: %d %s", r.Status, r.Code)
		}
	}
	if s.subID != "" {
		if r := c.call(http.MethodPost, rtSourcesBase+"/delete", map[string]any{"revision": c.revision(), "id": s.subID}); r.Status != http.StatusOK {
			t.Errorf("очистка: удаление подписки: %d %s", r.Status, r.Code)
		}
	}
	if r := c.call(http.MethodPost, "/api/configlayer/draft", map[string]any{"revision": c.revision(), "section": configlayer.SectionDNSTakeover, "value": nil}); r.Status != http.StatusOK {
		t.Errorf("очистка: секция dns_takeover: %d %s", r.Status, r.Code)
	}
	if c.rtRtDraftChanges() > 0 {
		c.rtRtApplyOK("очистка перехвата DNS")
	}
	c.rtRtPurge()
}

// rtDNSPost — действие службы перехвата с телом {}.
func (e *rtSrc) rtDNSPost(path string) rtSrcResp {
	e.t.Helper()
	return e.call(http.MethodPost, path, map[string]any{})
}

// rtDNSStart запускает транзакцию (enable или disable) и требует {started:true}.
func (e *rtSrc) rtDNSStart(path string) {
	e.t.Helper()
	r := e.rtDNSPost(path)
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST %s: %d %s %s", path, r.Status, r.Code, r.Detail)
	}
	if started, _ := r.object(e.t)["started"].(bool); !started {
		e.t.Fatalf("POST %s: started=false", path)
	}
}

// rtDNSWaitDone ждёт конца транзакции (state не applying) и возвращает вид.
func (e *rtSrc) rtDNSWaitDone() map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(rtDNSStateWait)
	for {
		v := e.rtDNSView()
		if st, _ := v["state"].(string); st != "applying" {
			return v
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("транзакция перехвата не закончилась за %v: шаги %v", rtDNSStateWait, v["steps"])
		}
		time.Sleep(time.Second)
	}
}

// rtDNSFailure — код и деталь сбоя из вида ("" — сбоя нет).
func rtDNSFailure(v map[string]any) (code, detail string) {
	f, _ := v["failure"].(map[string]any)
	code, _ = f["code"].(string)
	detail, _ = f["detail"].(string)
	return code, detail
}

// rtDNSEnableWindow включает перехват и ждёт окна подтверждения (DNSF-05):
// state window, window.kind=enable; сбой — провал с кодом.
func (e *rtSrc) rtDNSEnableWindow() map[string]any {
	e.t.Helper()
	e.rtDNSStart(rtDNSEnable)
	v := e.rtDNSWaitDone()
	if st, _ := v["state"].(string); st != "window" {
		code, detail := rtDNSFailure(v)
		e.t.Fatalf("после включения state %q, ожидалось window: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
	}
	if kind, _ := rtDNSWindow(v)["kind"].(string); kind != "enable" {
		e.t.Fatalf("после включения window.kind %q, ожидалось enable: %v", kind, v["window"])
	}
	return v
}

// rtDNSWindow — окно подтверждения из вида (nil — окна нет).
func rtDNSWindow(v map[string]any) map[string]any {
	w, _ := v["window"].(map[string]any)
	return w
}

// rtDNSKeep — «Оставить»: 200 и state on; возвращает вид из ответа.
func (e *rtSrc) rtDNSKeep() map[string]any {
	e.t.Helper()
	r := e.rtDNSPost(rtDNSKeepPath)
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST %s: %d %s %s", rtDNSKeepPath, r.Status, r.Code, r.Detail)
	}
	v := r.object(e.t)
	if st, _ := v["state"].(string); st != "on" {
		e.t.Fatalf("после «Оставить» state %q, ожидалось on", st)
	}
	if w := rtDNSWindow(v); w != nil {
		e.t.Errorf("после «Оставить» окно осталось: %v", w)
	}
	return v
}

// rtDNSEnableOK включает перехват, ждёт окна подтверждения и подтверждает его
// («Оставить»): итог — state on; сбой — провал с кодом.
func (e *rtSrc) rtDNSEnableOK() map[string]any {
	e.t.Helper()
	e.rtDNSEnableWindow()
	return e.rtDNSKeep()
}

// rtDNSEnsureOff выключает перехват, если он включён, и ждёт off. Открытое окно
// подтверждения сначала подтверждается: выключение в окне — 409 window_active.
func (e *rtSrc) rtDNSEnsureOff() {
	e.t.Helper()
	v := e.rtDNSWaitDone()
	if st, _ := v["state"].(string); st == "window" {
		if r := e.rtDNSPost(rtDNSKeepPath); r.Status != http.StatusOK && r.Code != "no_window" {
			e.t.Errorf("подтверждение окна перед выключением: %d %s %s", r.Status, r.Code, r.Detail)
		}
		v = e.rtDNSWaitDone()
	}
	if desired, _ := v["desired"].(bool); !desired {
		return
	}
	r := e.rtDNSPost(rtDNSDisable)
	if r.Status != http.StatusOK {
		e.t.Errorf("выключение перехвата: %d %s %s", r.Status, r.Code, r.Detail)
		return
	}
	if st, _ := e.rtDNSWaitDone()["state"].(string); st != "off" {
		e.t.Errorf("после выключения state %q, ожидалось off", st)
	}
}

// rtDNSExpectOn проверяет действующий перехват: порт 53 держит ядро kernel,
// внешнее имя разрешается через 127.0.0.1:53, проба включения прошла по двум
// группам, в файлах ядра слушатель панели.
func (e *rtSrc) rtDNSExpectOn(v map[string]any, kernel string) {
	e.t.Helper()
	owner, _ := v["owner"].(map[string]any)
	if kind, _ := owner["kind"].(string); kind != "kernel" || owner["kernel"] != kernel {
		e.t.Errorf("владелец :53 %v, ожидалось ядро %s", owner, kernel)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerKernel || got.Kernel != kernel {
		e.t.Errorf("по /proc порт 53 держит %q/%q, ожидалось ядро %s", got.Kind, got.Kernel, kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		e.t.Error("внешнее имя не разрешается через 127.0.0.1:53 под перехватом")
	}
	lc, _ := v["last_check"].(map[string]any)
	if src, _ := lc["source"].(string); src != "auto" {
		e.t.Errorf("last_check.source %q, ожидалось auto", src)
	}
	groups, _ := lc["groups"].([]any)
	if len(groups) != 2 {
		e.t.Errorf("групп пробы %d, ожидалось две (external, local)", len(groups))
	}
	for _, it := range groups {
		g, _ := it.(map[string]any)
		if ok, _ := g["ok"].(bool); !ok {
			e.t.Errorf("проба %v не прошла: %v", g["label"], g["reason"])
		}
	}
	if !rtDNSKernelHasListener(kernel) {
		e.t.Errorf("в файлах %s нет слушателя :53 панели", kernel)
	}
}

// rtDNSUserServer — сервер «значения пользователя» в подтесте умолчаний: DoH по
// IP, не совпадающий с умолчаниями D-17.
const rtDNSUserServer = "https://9.9.9.9/dns-query"

// rtDNSDoH и rtDNSBootstrap — умолчания перехвата D-17/D-18 (cores.DNSTakeoverDefaults).
var (
	rtDNSDoH       = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	rtDNSBootstrap = []string{"1.1.1.1", "8.8.8.8"}
)

// rtDNSServersKey — ключ списка серверов DNS-секции ядра.
func rtDNSServersKey(kernel string) string {
	if kernel == configlayer.KernelMihomo {
		return "nameserver"
	}
	return "servers"
}

// rtDNSSectionForm — форма DNS-секции ядра из вида секций.
func (e *rtSrc) rtDNSSectionForm(kernel string) (form map[string]any, advanced string) {
	e.t.Helper()
	sec := rtCoresSection(e.rtCoresView(kernel), "dns")
	if sec == nil {
		e.t.Fatalf("в виде %s нет секции dns", kernel)
	}
	form, _ = sec["form"].(map[string]any)
	if form == nil {
		form = map[string]any{}
	}
	advanced, _ = sec["advanced"].(string)
	return form, advanced
}

// rtDNSSetSection готовит DNS-секцию ядра к подтесту умолчаний и применяет:
// preset — значения пресета (серверы — локальные адреса пресета), user — свой
// сервер в списке серверов, остальные поля DNS через прокси убраны.
func (e *rtSrc) rtDNSSetSection(kernel, run string) {
	e.t.Helper()
	switch run {
	case "preset":
		r := e.rtCoresPost("/preset", map[string]any{"kernel": kernel, "section": "dns"})
		if r.Status != http.StatusOK {
			e.t.Fatalf("preset dns (%s): %d %s", kernel, r.Status, r.Code)
		}
	case "user":
		form, advanced := e.rtDNSSectionForm(kernel)
		form[rtDNSServersKey(kernel)] = []string{rtDNSUserServer}
		delete(form, "proxy-server-nameserver")
		delete(form, "default-nameserver")
		r := e.rtCoresPost("/save", map[string]any{"kernel": kernel, "section": "dns", "form": form, "advanced": advanced})
		if r.Status != http.StatusOK {
			e.t.Fatalf("save dns (%s): %d %s", kernel, r.Status, r.Code)
		}
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtRtApplyOK("DNS-секция перед включением (" + run + ")")
	}
}

// rtDNSExpectDefaults сверяет форму DNS-секции после включения: preset —
// серверы D-17 (у Mihomo ещё proxy-server-nameserver, default-nameserver и
// enhanced-mode redir-host); user — свой сервер не изменён, а пустой
// proxy-server-nameserver Mihomo дополнен. Черновик закрыт: дописанное слито в него.
func (e *rtSrc) rtDNSExpectDefaults(kernel, run string) {
	e.t.Helper()
	form, _ := e.rtDNSSectionForm(kernel)
	servers := rtDNSMihomoStrings(form[rtDNSServersKey(kernel)])
	switch run {
	case "preset":
		if !slices.Equal(servers, rtDNSDoH) {
			e.t.Errorf("пресет: серверы после включения %v, ожидались умолчания %v", servers, rtDNSDoH)
		}
		if kernel == configlayer.KernelMihomo {
			if got := rtDNSMihomoStrings(form["default-nameserver"]); !slices.Equal(got, rtDNSBootstrap) {
				e.t.Errorf("пресет: default-nameserver %v, ожидалось %v", got, rtDNSBootstrap)
			}
			if got, _ := form["enhanced-mode"].(string); got != "redir-host" {
				e.t.Errorf("пресет: enhanced-mode %q, ожидалось redir-host", got)
			}
		}
	case "user":
		if !slices.Equal(servers, []string{rtDNSUserServer}) {
			e.t.Errorf("значение пользователя изменено включением: серверы %v", servers)
		}
	}
	if kernel == configlayer.KernelMihomo {
		if got := rtDNSMihomoStrings(form["proxy-server-nameserver"]); !slices.Equal(got, rtDNSDoH) {
			e.t.Errorf("%s: proxy-server-nameserver %v, ожидалось %v", run, got, rtDNSDoH)
		}
	}
	if n := e.rtRtDraftChanges(); n != 0 {
		e.t.Errorf("%s: после включения в черновике %d изменений, ожидался закрытый черновик", run, n)
	}
}

// Трасса: подготовка → умолчания секции DNS (пресет и значение пользователя:
// включение → форма → выключение) → POST enable {started} → state on → :53 у
// активного ядра, внешнее имя отвечает через 127.0.0.1:53, проба auto по двум
// группам, слушатель в файлах ядра → выключение: :53 у прошивки, слушателя нет,
// desired=false. Метка ядра: перехват ведёт активное ядро.
func TestRouterDNSEnableDisable(t *testing.T) {
	routertest.Slow(t, "включение и выключение перехвата DNS с перезапуском ядра на каждом шаге")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	t.Run("defaults", func(t *testing.T) {
		for _, run := range []string{"preset", "user"} {
			t.Run(run, func(t *testing.T) {
				c := e.with(t)
				c.rtDNSSetSection(kernel, run)
				c.rtDNSEnableOK()
				t.Cleanup(func() { c.rtDNSEnsureOff() })
				c.rtDNSExpectDefaults(kernel, run)
			})
		}
	})

	start := time.Now().Unix()
	v := e.rtDNSEnableOK()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	e.rtDNSExpectOn(v, kernel)

	// Одна запись истории на транзакцию, и последний запуск конвейера — завершающий
	// проход включения, а не отменённый.
	t.Run("one_record", func(t *testing.T) {
		c := e.with(t)
		ok, failed := c.rtDNSHistorySince(start)
		if ok != 1 {
			t.Errorf("успешных записей %s после включения %d, ожидалась одна", "dns_takeover_enable", ok)
		}
		if failed != 0 {
			t.Errorf("неудачных записей dns_takeover* после включения %d, ожидалось 0", failed)
		}
		a := c.rtRstApplyView()
		if trig, _ := a["trigger"].(string); trig != "dns_takeover" {
			t.Errorf("apply.trigger %q, ожидалось dns_takeover", trig)
		}
		if res, _ := rtRstResult(a); !res {
			t.Errorf("apply.result.ok=false: %s", rtRstDescribe(a))
		}
	})

	t.Run("disable", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSStart(rtDNSDisable)
		off := c.rtDNSWaitDone()
		if st, _ := off["state"].(string); st != "off" {
			code, detail := rtDNSFailure(off)
			t.Fatalf("после выключения state %q, ожидалось off: сбой %q %.300s", st, code, detail)
		}
		if desired, _ := off["desired"].(bool); desired {
			t.Error("после выключения desired=true")
		}
		if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
			t.Errorf("после выключения порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
		}
		if rtDNSKernelHasListener(kernel) {
			t.Errorf("после выключения в файлах %s остался слушатель :53", kernel)
		}
		if !rtDNSResolves(dnstakeover.DefaultCheckName) {
			t.Error("после выключения прошивка не отвечает через 127.0.0.1:53")
		}
	})

	// D-33: включение из proxy_dns=on снимает второй механизм на :53 — правил XKeen
	// с портом 53 нет, строка proxy_dns выключена.
	t.Run("legacy_off", func(t *testing.T) {
		c := e.with(t)
		rtDNSLegacyOn(t, c)
		c.rtDNSRequireClosedDraft()
		c.rtDNSEnableOK()
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		rtDNSWaitXKeenRules53(t, nil)
		if rtDNSProxyOn(t) {
			t.Error("после включения перехвата proxy_dns остался on")
		}
	})
}

// rtDNSHistorySince считает записи истории действий XKeen с меткой времени не
// раньше since: ok — успешные dns_takeover_enable, failed — неудачные dns_takeover*.
func (e *rtSrc) rtDNSHistorySince(since int64) (ok, failed int) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/service/restart-log", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/service/restart-log: %d", r.Status)
	}
	var entries []struct {
		Timestamp int64  `json:"timestamp"`
		Action    string `json:"action"`
		Success   bool   `json:"success"`
	}
	if err := json.Unmarshal(r.Raw, &entries); err != nil {
		e.t.Fatalf("история действий не разбирается: %v", err)
	}
	for _, en := range entries {
		if en.Timestamp < since || !strings.HasPrefix(en.Action, "dns_takeover") {
			continue
		}
		switch {
		case !en.Success:
			failed++
		case en.Action == "dns_takeover_enable":
			ok++
		}
	}
	return ok, failed
}

// --- перехват XKeen с :53 и гашение процессов --------------------------------

// rtDNSDport53Re — порт 53 в правиле: --dport 53 (и с !) или 53 в списке --dports.
var rtDNSDport53Re = regexp.MustCompile(`--dports?\s+(?:\S*[,:])?53(?:[,:]\S*)?(?:\s|$)`)

// rtDNSXKeenRules53 — правила XKeen с портом 53 (iptables -t nat и -t mangle,
// строки с «xkeen»), с таблицей впереди, отсортированные.
func rtDNSXKeenRules53(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, table := range []string{"nat", "mangle"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		stdout, stderr, code := routertest.Exec(ctx, "iptables", "-w", "-t", table, "-S")
		cancel()
		if code != 0 {
			t.Fatalf("iptables -t %s: код %d %s", table, code, strings.TrimSpace(stderr))
		}
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "xkeen") && rtDNSDport53Re.MatchString(line) {
				out = append(out, table+": "+strings.TrimSpace(line))
			}
		}
	}
	sort.Strings(out)
	return out
}

// rtDNSWaitXKeenRules53 ждёт, пока набор правил XKeen с портом 53 совпадёт с want
// (XKeen ставит правила уже после возврата запуска); не совпал — провал с обоими
// наборами.
func rtDNSWaitXKeenRules53(t *testing.T, want []string) {
	t.Helper()
	deadline := time.Now().Add(rtCoresLiveRulesWait)
	for {
		got := rtDNSXKeenRules53(t)
		if slices.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("правила XKeen с :53 за %v не совпали с ожидаемыми:\nесть %q\nожидалось %q", rtCoresLiveRulesWait, got, want)
		}
		time.Sleep(2 * time.Second)
	}
}

// rtDNSXKeen выполняет XKeen с аргументами-константами.
func rtDNSXKeen(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, stderr, code := routertest.Exec(ctx, config.Default().XKeenBinary, args...)
	if code != 0 {
		t.Fatalf("xkeen %s: код %d %s", strings.Join(args, " "), code, strings.TrimSpace(stderr))
	}
}

// rtDNSLegacyOn включает старое перенаправление DNS XKeen (proxy_dns=on) и
// перезапускает запущенное ядро через API панели: XKeen строит перехват с :53 при
// старте ядра. baseline — правила XKeen с :53; пусто — находка стенда. t.Cleanup
// возвращает строку и снова перезапускает ядро.
func rtDNSLegacyOn(t *testing.T, e *rtSrc) (baseline []string) {
	t.Helper()
	before := rtDNSProxyOn(t)
	rtDNSXKeen(t, "-dns", "on")
	t.Cleanup(func() {
		if before {
			return
		}
		rtDNSXKeen(t, "-dns", "off")
		e.with(t).rtDNSServiceControl("restart")
		rtCoresLiveWaitStatus(t)
	})
	e.rtDNSServiceControl("restart")
	rtCoresLiveWaitStatus(t)
	deadline := time.Now().Add(rtCoresLiveRulesWait)
	for {
		baseline = rtDNSXKeenRules53(t)
		if len(baseline) > 0 {
			return baseline
		}
		if time.Now().After(deadline) {
			t.Fatalf("no_xkeen_dns_rules: XKeen за %v не построил перехват :53 при proxy_dns=on", rtCoresLiveRulesWait)
		}
		time.Sleep(2 * time.Second)
	}
}

// rtDNSKillWhile — горутина сценария: раз в 10 мс вызывает match и гасит
// найденные PID сигналом SIGKILL (настоящий сбой на роутере: так же процесс
// гасит нехватка памяти). killed — сколько процессов погашено. Горутина
// останавливается в t.Cleanup раньше возврата стенда.
func rtDNSKillWhile(t *testing.T, match func() []int) (killed func() int) {
	t.Helper()
	var n atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			for _, pid := range match() {
				if syscall.Kill(pid, syscall.SIGKILL) == nil {
					n.Add(1)
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	return func() int { return int(n.Load()) }
}

// rtDNSProcArgs — argv процесса; процесса нет — nil.
func rtDNSProcArgs(pid string) []string {
	raw, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil || len(raw) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
}

// rtDNSArgAfter — значение аргумента после флага flag; нет — "".
func rtDNSArgAfter(args []string, flag string) string {
	for i := 1; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// rtDNSValidatePIDs — процессы проверки конфигурации ядром kernel (Xray —
// «run -test -confdir», Mihomo — «-t -f»), в проверяемой копии которых есть
// слушатель :53 панели: такой конфиг собирается только при включении перехвата.
func rtDNSValidatePIDs(kernel string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, ent := range entries {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || !ent.IsDir() {
			continue
		}
		args := rtDNSProcArgs(ent.Name())
		if len(args) < 2 || filepath.Base(args[0]) != kernel {
			continue
		}
		switch kernel {
		case configlayer.KernelXray:
			dir := rtDNSArgAfter(args, "-confdir")
			if !slices.Contains(args, "-test") || dir == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, rtDNSXrayListenerFile)); err != nil {
				continue
			}
		case configlayer.KernelMihomo:
			file := rtDNSArgAfter(args, "-f")
			if !slices.Contains(args, "-t") || file == "" || !rtDNSMihomoFileListens(file) {
				continue
			}
		default:
			continue
		}
		out = append(out, pid)
	}
	return out
}

// rtDNSMihomoFileListens — в профиле Mihomo dns.listen на :53.
func rtDNSMihomoFileListens(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var top struct {
		DNS struct {
			Listen string `yaml:"listen"`
		} `yaml:"dns"`
	}
	if yaml.Unmarshal(data, &top) != nil {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(top.DNS.Listen), ":53")
}

// rtDNSKernelFiles — суммы файлов ядра: у Xray — файлы каталога конфигурации, у
// Mihomo — профиль панели.
func rtDNSKernelFiles(t *testing.T, kernel string) map[string]string {
	t.Helper()
	if kernel == configlayer.KernelXray {
		return rtCoresDirSums(t)
	}
	data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, rtRtPanelProfile))
	if err != nil {
		t.Fatalf("профиль панели Mihomo не читается: %v", err)
	}
	return map[string]string{"profile": rtRtSum(data)}
}

// rtDNSStepStatus — статус шага транзакции из вида ("" — шага нет).
func rtDNSStepStatus(v map[string]any, id string) string {
	steps, _ := v["steps"].([]any)
	for _, it := range steps {
		s, _ := it.(map[string]any)
		if s["id"] == id {
			st, _ := s["status"].(string)
			return st
		}
	}
	return ""
}

// rtDNSRequireClosedDraft — черновик закрыт (включение требует его, D-01).
func (e *rtSrc) rtDNSRequireClosedDraft() {
	e.t.Helper()
	if n := e.rtRtDraftChanges(); n != 0 {
		e.t.Fatalf("перед включением в черновике %d изменений, ожидался закрытый черновик", n)
	}
}

// rtDNSEnableFails включает перехват и ждёт сбоя с кодом want (откат выполнен).
func (e *rtSrc) rtDNSEnableFails(want string) map[string]any {
	e.t.Helper()
	e.rtDNSStart(rtDNSEnable)
	v := e.rtDNSWaitDone()
	code, detail := rtDNSFailure(v)
	if st, _ := v["state"].(string); st != "off" || code != want {
		e.t.Fatalf("включение со сбоем: state %q, код %q (%.300s), ожидалось off и %s; шаги %v", st, code, detail, want, v["steps"])
	}
	f, _ := v["failure"].(map[string]any)
	if rb, _ := f["rolled_back"].(bool); !rb {
		e.t.Errorf("сбой %s: rolled_back=false", want)
	}
	return v
}

// rtDNSExpectRestored — после отката: override как до включения, строка
// proxy_dns on и прошивка отвечает. Набор правил XKeen с :53 сверяет сам подтест.
func rtDNSExpectRestored(t *testing.T, overrideBefore bool) {
	t.Helper()
	if got := rtDNSOverrideOn(t); got != overrideBefore {
		t.Errorf("после отката override=%v, до включения был %v", got, overrideBefore)
	}
	if !rtDNSProxyOn(t) {
		t.Error("после отката proxy_dns не вернулся в on")
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после отката DNS роутера не отвечает через 127.0.0.1:53")
	}
}

// rtDNSDeadGroup — группа-цель с единственным мёртвым узлом (адрес loopback без
// слушателя), применённая обычным «Применить» как группа перехвата. t.Cleanup
// убирает группу, узел и параметры перехвата тем же «Применить».
func (e *rtSrc) rtDNSDeadGroup(t *testing.T) {
	t.Helper()
	link := rtLinkText(t, "vless_ws")
	re := regexp.MustCompile(`@[^:/?#]+:\d+`)
	if !re.MatchString(link) {
		t.Fatal("в ссылке фикстуры нет адреса с портом")
	}
	e.rtRstAddMyNode(re.ReplaceAllString(link, "@127.0.0.1:"+rtRpClosedPort(t)))
	myID := ""
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	srcs, _ := view.object(t)["sources"].([]any)
	for _, it := range srcs {
		src, _ := it.(map[string]any)
		if src["kind"] == "my" {
			myID, _ = src["id"].(string)
		}
	}
	if myID == "" {
		t.Fatal("источника «Мои узлы» нет после добавления узла")
	}
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "DNS-мёртвая", "mode": "manual",
			"sources": map[string]any{"all": false, "ids": []string{myID}},
			"members": []map[string]string{},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	gid, _ := save.object(t)["group"].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		c := e.with(t)
		c.rtRstIdle()
		c.rtDNSDraft(map[string]any{"group": "", "zones": []string{}})
		if r := c.call(http.MethodPost, rtRoutingBase+"/groups/delete", map[string]any{"revision": c.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": ""}}); r.Status != http.StatusOK {
			t.Errorf("очистка: groups/delete: %d %s", r.Status, r.Code)
		}
		for id := c.rtRstMyEntry(); id != ""; id = c.rtRstMyEntry() {
			c.rtRstDeleteMyNode(id)
		}
		c.rtRtApplyOK("очистка: мёртвая группа перехвата")
	})
	e.rtDNSDraft(map[string]any{"group": gid, "zones": []string{}})
	e.rtRtApplyOK("мёртвая группа перехвата")
}

// Трасса: подготовка → proxy_dns=on и действующий перехват XKeen с :53 → три
// сбоя включения при закрытом черновике, каждый достижим только под перехватом:
// validate — гашение процесса проверки конфигурации со слушателем панели
// (check_failed, прошивка не тронута); restart — гашение ядра, пока в его файлах
// слушатель (restart_failed, ядро снова на прежних файлах); probe — группа-цель с
// мёртвым узлом (probe_external, Applied — снимок). После каждого: override как
// до включения, proxy_dns on, прежний перехват XKeen с :53. Метка ядра: сбой
// вызывается на процессах активного ядра.
func TestRouterDNSTxnRollback(t *testing.T) {
	routertest.Tier(t, routertest.Full, "внесение сбоя: гашение процессов и мёртвый узел обрывают включение перехвата DNS")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	baseline := rtDNSLegacyOn(t, e)

	t.Run("validate", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSRequireClosedDraft()
		overrideBefore := rtDNSOverrideOn(t)
		files := rtDNSKernelFiles(t, kernel)
		killed := rtDNSKillWhile(t, func() []int { return rtDNSValidatePIDs(kernel) })
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		v := c.rtDNSEnableFails("check_failed")
		if killed() < 1 {
			t.Error("процесс проверки конфигурации со слушателем панели не погашен")
		}
		for _, id := range []string{"write", "restart"} {
			if st := rtDNSStepStatus(v, id); st != "pending" && st != "skipped" {
				t.Errorf("шаг %s после отказа проверки: %q, ожидалось — не начинался", id, st)
			}
		}
		if !rtCoresSumsEqual(files, rtDNSKernelFiles(t, kernel)) {
			t.Error("файлы ядра изменились при отказе проверки")
		}
		rtDNSExpectRestored(t, overrideBefore)
		rtDNSWaitXKeenRules53(t, baseline)
	})

	t.Run("restart", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSRequireClosedDraft()
		overrideBefore := rtDNSOverrideOn(t)
		// Гасится только процесс, поднятый перезапуском: прежний, убитый до шага
		// перезапуска, конвейер считает остановленным ядром (FND-05) и не поднимает.
		old, _ := rtRstKernelPIDs(kernel)
		killed := rtDNSKillWhile(t, func() []int {
			if !rtDNSKernelHasListener(kernel) {
				return nil
			}
			pids, _ := rtRstKernelPIDs(kernel)
			if !rtDNSKernelHasListener(kernel) {
				return nil
			}
			return slices.DeleteFunc(pids, func(pid int) bool { return slices.Contains(old, pid) })
		})
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		c.rtDNSEnableFails("restart_failed")
		if killed() < 1 {
			t.Error("ядро со слушателем панели не погашено")
		}
		if rtDNSKernelHasListener(kernel) {
			t.Errorf("после отката в файлах %s остался слушатель :53", kernel)
		}
		rtRstHoldPID(t, kernel, rtRstKernelPID(t, kernel))
		rtDNSExpectRestored(t, overrideBefore)
		rtDNSWaitXKeenRules53(t, baseline)
	})

	t.Run("probe", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSDeadGroup(t)
		c.rtDNSRequireClosedDraft()
		overrideBefore := rtDNSOverrideOn(t)
		files := rtDNSKernelFiles(t, kernel)
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		v := c.rtDNSEnableFails("probe_external")
		if desired, _ := v["desired"].(bool); desired {
			t.Error("после отката по пробе desired=true")
		}
		if n := c.rtRtDraftChanges(); n != 0 {
			t.Errorf("после отката по пробе в черновике %d изменений", n)
		}
		if !rtCoresSumsEqual(files, rtDNSKernelFiles(t, kernel)) {
			t.Error("файлы ядра после отката по пробе не равны снимку")
		}
		if got := dnstakeover.PortOwner(); got.Kind == dnstakeover.OwnerKernel {
			t.Errorf("после отката по пробе порт 53 держит ядро %q", got.Kernel)
		}
		rtDNSExpectRestored(t, overrideBefore)
		rtDNSWaitXKeenRules53(t, baseline)
	})
}

// rtDNSRouterLANAddr — первый не loopback IPv4-адрес интерфейсов роутера (LAN). Адрес
// стенда в сообщения тестов не выводится: отчёты и PR его не содержат.
func rtDNSRouterLANAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("адреса интерфейсов роутера не прочитаны: %v", err)
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			return ipn.IP.String()
		}
	}
	t.Skip("у роутера нет не loopback адреса IPv4")
	return ""
}

// rtDNSServiceControl — POST /api/service/control?action=<действие>; ответ не 200 — провал.
func (e *rtSrc) rtDNSServiceControl(action string) {
	e.t.Helper()
	r := e.callSlow(http.MethodPost, "/api/service/control?action="+action, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("service/control %s: %d %s", action, r.Status, r.Code)
	}
}

// Трасса: открытый черновик → 409 draft_open; ядро остановлено через API службы
// → 409 kernel_stopped, и ядро после отказа не запущено (D-04); маршрутизация
// Xray отпущена и применена → 409 routing_not_managed, в блокировках карточки
// routing, override не включался (слушатель :53 выпускает генератор маршрутизации,
// без него включение оставило бы DNS LAN без слушателя); у Mihomo сервер DNS-секции
// с LAN-адресом самого роутера → 409 self_server, override не включался (IN-08).
// Ядро возвращается
// запущенным, маршрутизация — взятой в t.Cleanup. Метка ядра: ядро останавливается.
func TestRouterDNSEnableRefused(t *testing.T) {
	routertest.Slow(t, "остановка и запуск ядра через API служб, отпускание и возврат маршрутизации")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	t.Run("draft_open", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSDraft(map[string]any{"group": "", "zones": []string{"xcp-rt.test"}})
		t.Cleanup(func() { c.resetDraft() })
		r := c.rtDNSPost(rtDNSEnable)
		if r.Status != http.StatusConflict || r.Code != "draft_open" {
			t.Errorf("включение при открытом черновике: %d %q, ожидалось 409 draft_open", r.Status, r.Code)
		}
	})

	t.Run("kernel_stopped", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSServiceControl("stop")
		t.Cleanup(func() {
			c.rtDNSServiceControl("start")
			rtCoresLiveWaitStatus(t)
		})
		r := c.rtDNSPost(rtDNSEnable)
		if r.Status != http.StatusConflict || r.Code != "kernel_stopped" {
			t.Errorf("включение при остановленном ядре: %d %q, ожидалось 409 kernel_stopped", r.Status, r.Code)
		}
		time.Sleep(5 * time.Second)
		if pids, _ := rtRstKernelPIDs(kernel); len(pids) != 0 {
			t.Errorf("после отказа kernel_stopped процессов %s: %d, ядро не должно запускаться", kernel, len(pids))
		}
		if v := c.rtDNSView(); v["desired"] == true {
			t.Error("после отказа kernel_stopped desired=true")
		}
	})

	t.Run("self_server", func(t *testing.T) {
		if kernel != configlayer.KernelMihomo {
			t.Skip("сверка серверов с адресами интерфейсов роутера — у DNS-секции Mihomo")
		}
		lan := rtDNSRouterLANAddr(t)
		c := e.with(t)
		form, advanced := c.rtDNSSectionForm(kernel)
		form[rtDNSServersKey(kernel)] = []string{lan}
		if r := c.rtCoresPost("/save", map[string]any{"kernel": kernel, "section": "dns", "form": form, "advanced": advanced}); r.Status != http.StatusOK {
			t.Fatalf("save dns (%s): %d %s", kernel, r.Status, r.Code)
		}
		t.Cleanup(func() { c.rtDNSSetSection(kernel, "preset") })
		c.rtRtApplyOK("DNS-секция Mihomo с адресом самого роутера")

		overrideBefore := rtDNSOverrideOn(t)
		r := c.rtDNSPost(rtDNSEnable)
		if r.Status != http.StatusConflict || r.Code != "self_server" {
			t.Errorf("включение с LAN-адресом роутера в DNS-секции: %d %q, ожидалось 409 self_server", r.Status, r.Code)
		}
		if now := rtDNSOverrideOn(t); now != overrideBefore {
			t.Errorf("отказ self_server тронул override: было %v, стало %v", overrideBefore, now)
		}
		if v := c.rtDNSView(); v["desired"] == true {
			t.Error("после отказа self_server desired=true")
		}
	})

	t.Run("routing_not_managed", func(t *testing.T) {
		if kernel != configlayer.KernelXray {
			t.Skip("маршрутизацию Mihomo отпускает вместе с DNS-секцией профиля: до проверки маршрутизации включение отказывает section_foreign")
		}
		c := e.with(t)
		overrideBefore := rtDNSOverrideOn(t)
		rel := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
		if rel.Status != http.StatusOK {
			t.Fatalf("routing/release: %d %s", rel.Status, rel.Code)
		}
		t.Cleanup(func() {
			if take := c.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": c.rtRtRevision()}); take.Status != http.StatusOK {
				t.Errorf("очистка: routing/take: %d %s", take.Status, take.Code)
			}
			if c.rtRtDraftChanges() > 0 {
				c.rtRtApplyOK("очистка: маршрутизация возвращена панели")
			}
		})
		c.rtRtApplyOK("маршрутизация отпущена до включения перехвата")

		if !c.rtDNSBlocks()["routing"] {
			t.Errorf("при отпущенной маршрутизации блокировки включения %v без routing", c.rtDNSView()["blocks"])
		}
		r := c.rtDNSPost(rtDNSEnable)
		if r.Status != http.StatusConflict || r.Code != "routing_not_managed" {
			t.Errorf("включение при отпущенной маршрутизации: %d %q, ожидалось 409 routing_not_managed", r.Status, r.Code)
		}
		if now := rtDNSOverrideOn(t); now != overrideBefore {
			t.Errorf("отказ routing_not_managed тронул override: было %v, стало %v", overrideBefore, now)
		}
		if v := c.rtDNSView(); v["desired"] == true {
			t.Error("после отказа routing_not_managed desired=true")
		}
	})
}
