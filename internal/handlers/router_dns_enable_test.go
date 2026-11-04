//go:build router

package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"

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
	rtDNSEnable  = rtDNSBase + "/enable"
	rtDNSDisable = rtDNSBase + "/disable"
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
		data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, rtRtPanelProfile))
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
	e.rtRtPurge()
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

// rtDNSEnableOK включает перехват и ждёт состояния on; сбой — провал с кодом.
func (e *rtSrc) rtDNSEnableOK() map[string]any {
	e.t.Helper()
	e.rtDNSStart(rtDNSEnable)
	v := e.rtDNSWaitDone()
	if st, _ := v["state"].(string); st != "on" {
		code, detail := rtDNSFailure(v)
		e.t.Fatalf("после включения state %q, ожидалось on: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
	}
	return v
}

// rtDNSEnsureOff выключает перехват, если он включён, и ждёт off.
func (e *rtSrc) rtDNSEnsureOff() {
	e.t.Helper()
	v := e.rtDNSWaitDone()
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

// Трасса: подготовка → POST enable {started} → state on → :53 у активного ядра,
// внешнее имя отвечает через 127.0.0.1:53, проба auto по двум группам, слушатель
// в файлах ядра. Метка ядра: перехват ведёт активное ядро.
func TestRouterDNSEnableDisable(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	v := e.rtDNSEnableOK()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	e.rtDNSExpectOn(v, kernel)
}
