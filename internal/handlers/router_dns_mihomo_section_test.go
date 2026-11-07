//go:build router

// router-area: dns

package handlers

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// DNSF-01: поля DNS через прокси формы секции DNS Mihomo (listen,
// proxy-server-nameserver, default-nameserver, fake-ip-range, fake-ip-filter)
// попадают из формы в профиль панели. Путь тот, что у формы: маршрутизация Mihomo
// взята (секция dns у панели) → POST /api/configlayer/cores/save с формой →
// «Применить» (конвейер с проверкой ядром) → ключи dns.* в profiles/xcp-main.yaml
// на диске устройства. Вердикт — файл профиля и, если Mihomo — активное ядро,
// слушатель DNS ядра на выбранном порту. Стенд возвращается в t.Cleanup при любом
// исходе: значение секции — прежнее, маршрутизация отпущена, флаг слоя прежний.

// rtDNSMihomoProxyNS, rtDNSMihomoDefaultNS — серверы формы: публичный DoH по IP и
// резолвер по IP (адреса конфигурации, не узлы подписки).
var (
	rtDNSMihomoProxyNS   = []string{"https://1.1.1.1/dns-query"}
	rtDNSMihomoDefaultNS = []string{"1.1.1.1"}
	rtDNSMihomoFilter    = []string{"+.lan"}
)

// rtDNSMihomoRange — диапазон fake-ip формы (умолчание ядра, записанное явно).
const rtDNSMihomoRange = "198.18.0.1/16"

// rtDNSMihomoListenWait — сколько ждать слушателя DNS ядра после «Применить».
const rtDNSMihomoListenWait = 30 * time.Second

// rtDNSMihomoStrings приводит значение YAML-списка к срезу строк; не список — nil.
func rtDNSMihomoStrings(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, fmt.Sprint(it))
	}
	return out
}

// rtDNSMihomoListening сообщает, что на addr (host:port) есть слушатель UDP.
func rtDNSMihomoListening(addr string) bool {
	for _, cmd := range [][]string{{"netstat", "-lnu"}, {"ss", "-lnu"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stdout, _, code := routertest.Exec(ctx, cmd[0], cmd[1:]...)
		cancel()
		if code == 0 {
			return strings.Contains(stdout, addr)
		}
	}
	return false
}

// Трасса: слой включён → маршрутизация Mihomo взята и применена → секция dns у
// панели → save формы с полями DNSF-01 → «Применить» OK → в xcp-main.yaml dns.listen,
// dns.proxy-server-nameserver, dns.default-nameserver, dns.fake-ip-range,
// dns.fake-ip-filter равны сохранённым → вид секции показывает их в форме, а не в
// «Дополнительно» → при активном Mihomo слушатель DNS поднят на выбранном порту.
// Метка ядра: исход перезапуска после «Применить» зависит от активного ядра.
func TestRouterDNSMihomoSection(t *testing.T) {
	routertest.Slow(t, "«Применить» секции DNS Mihomo с перезапуском ядра")
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	rtCoresLiveMihomoTake(t, e, tg)

	sec := rtCoresSection(e.rtCoresView("mihomo"), "dns")
	if sec == nil {
		t.Fatal("в виде Mihomo нет секции dns")
	}
	if owner, _ := sec["owner"].(string); owner != "panel" {
		t.Fatalf("секция dns после взятия маршрутизации: владелец %q, ожидался panel", owner)
	}
	origForm, _ := sec["form"].(map[string]any)
	origAdvanced, _ := sec["advanced"].(string)
	// Очистка до отпускания маршрутизации (t.Cleanup — в обратном порядке): прежние
	// форма и «Дополнительно» секции возвращаются тем же маршрутом, каким менялись, и
	// уходят в черновик: «Применить» отпускания их применяет.
	t.Cleanup(func() {
		c := e.with(t)
		c.rtRstIdle()
		r := c.rtCoresPost("/save", map[string]any{
			"kernel": "mihomo", "section": "dns", "form": origForm, "advanced": origAdvanced,
		})
		if r.Status != http.StatusOK {
			t.Errorf("очистка: возврат секции dns Mihomo: %d %s", r.Status, r.Code)
		}
	})

	listen := fmt.Sprintf("127.0.0.1:%d", rtCoresLiveFreePort(t))
	form := map[string]any{}
	if orig, _ := sec["form"].(map[string]any); orig != nil {
		form = maps.Clone(orig)
	}
	form["enable"] = true
	if len(rtDNSMihomoStrings(form["nameserver"])) == 0 {
		form["nameserver"] = []string{"system"}
	}
	form["listen"] = listen
	form["proxy-server-nameserver"] = rtDNSMihomoProxyNS
	form["default-nameserver"] = rtDNSMihomoDefaultNS
	form["fake-ip-range"] = rtDNSMihomoRange
	form["fake-ip-filter"] = rtDNSMihomoFilter
	advanced, _ := sec["advanced"].(string)

	r := e.rtCoresPost("/save", map[string]any{
		"kernel": "mihomo", "section": "dns", "form": form, "advanced": advanced,
	})
	if r.Status != http.StatusOK {
		t.Fatalf("save dns (mihomo): %d %s", r.Status, r.Code)
	}
	e.rtRtApplyOK("поля DNS через прокси формы Mihomo")

	top := rtCoresRetakeProfileTop(t)
	dns, _ := top["dns"].(map[string]any)
	if dns == nil {
		t.Fatal("в xcp-main.yaml нет секции dns")
	}
	if got, _ := dns["listen"].(string); got != listen {
		t.Errorf("xcp-main.yaml: dns.listen %q, ожидался %q", got, listen)
	}
	if got, _ := dns["fake-ip-range"].(string); got != rtDNSMihomoRange {
		t.Errorf("xcp-main.yaml: dns.fake-ip-range %q, ожидался %q", got, rtDNSMihomoRange)
	}
	for key, want := range map[string][]string{
		"proxy-server-nameserver": rtDNSMihomoProxyNS,
		"default-nameserver":      rtDNSMihomoDefaultNS,
		"fake-ip-filter":          rtDNSMihomoFilter,
	} {
		if got := rtDNSMihomoStrings(dns[key]); !slices.Equal(got, want) {
			t.Errorf("xcp-main.yaml: dns.%s %v, ожидалось %v", key, got, want)
		}
	}

	// Вид секции после «Применить»: поля — в форме, в «Дополнительно» их нет.
	after := rtCoresSection(e.rtCoresView("mihomo"), "dns")
	afterForm, _ := after["form"].(map[string]any)
	for _, key := range []string{"listen", "proxy-server-nameserver", "default-nameserver", "fake-ip-range", "fake-ip-filter"} {
		if _, ok := afterForm[key]; !ok {
			t.Errorf("вид секции dns: в форме нет %s", key)
		}
		if adv, _ := after["advanced"].(string); strings.Contains(adv, key+":") {
			t.Errorf("вид секции dns: %s остался в «Дополнительно»", key)
		}
	}

	if tg.Core != "mihomo" {
		return
	}
	deadline := time.Now().Add(rtDNSMihomoListenWait)
	for !rtDNSMihomoListening(listen) {
		if time.Now().After(deadline) {
			t.Fatalf("активный Mihomo не слушает DNS на адресе формы за %v", rtDNSMihomoListenWait)
		}
		time.Sleep(time.Second)
	}
}
