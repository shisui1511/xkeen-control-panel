//go:build router

// router-area: dns

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Состояние перехвата DNS и «Проверить резолв» на настоящей панели устройства:
// GET /api/configlayer/dns и POST /api/configlayer/dns/check. Вердикт — ответы
// панели и факты устройства (init-скрипт XKeen, часы роутера). Перехват в этих
// сценариях не включается: порт 53 держит прошивка. Адреса из ответов DNS в
// журнал теста не попадают.
//
// Метка ядра в каждом сценарии: поля active_kernel и владелец порта зависят от
// ядра стенда.

const (
	rtDNSBase  = "/api/configlayer/dns"
	rtDNSCheck = rtDNSBase + "/check"
	// rtDNSInit — init-скрипт XKeen с переменной proxy_dns.
	rtDNSInit = "/opt/etc/init.d/S05xkeen"
)

// rtDNSProxyRe — строка proxy_dns=… init-скрипта (значение в кавычках или без).
var rtDNSProxyRe = regexp.MustCompile(`(?m)^\s*proxy_dns\s*=\s*["']?([A-Za-z]+)`)

// rtDNSProxyOn читает proxy_dns из init-скрипта XKeen на устройстве.
func rtDNSProxyOn(t *testing.T) bool {
	t.Helper()
	data, err := os.ReadFile(rtDNSInit)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatalf("init-скрипт XKeen: %v", err)
	}
	m := rtDNSProxyRe.FindSubmatch(data)
	return m != nil && strings.EqualFold(string(m[1]), "on")
}

// rtDNSView читает состояние перехвата; ответ не 200 — провал.
func (e *rtSrc) rtDNSView() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtDNSBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtDNSBase, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtDNSBlockOrder — порядок блокировок UI-SPEC.
var rtDNSBlockOrder = []string{"conflict", "busy", "window", "draft", "stopped", "section", "routing", "group"}

// rtDNSBlocks — блокировки из состояния перехвата; порядок нарушен — ошибка.
func (e *rtSrc) rtDNSBlocks() map[string]bool {
	e.t.Helper()
	list, _ := e.rtDNSView()["blocks"].([]any)
	out := map[string]bool{}
	pos := 0
	for _, it := range list {
		code, _ := it.(string)
		out[code] = true
		for pos < len(rtDNSBlockOrder) && rtDNSBlockOrder[pos] != code {
			pos++
		}
		if pos == len(rtDNSBlockOrder) {
			e.t.Errorf("блокировка %q вне порядка или неизвестна: %v", code, list)
			return out
		}
	}
	return out
}

// rtDNSDraft пишет секцию dns_takeover в черновик.
func (e *rtSrc) rtDNSDraft(value any) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/api/configlayer/draft", map[string]any{
		"revision": e.revision(), "section": "dns_takeover", "value": value,
	})
	if r.Status != http.StatusOK {
		e.t.Fatalf("правка черновика dns_takeover: %d %s", r.Status, r.Code)
	}
}

// rtDNSCoresOwner — владелец DNS-секции ядра по API секций ядер.
func (e *rtSrc) rtDNSCoresOwner(kernel string) string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/cores?kernel="+kernel, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/cores?kernel=%s: %d %s", kernel, r.Status, r.Code)
	}
	var view struct {
		Sections []struct {
			ID    string `json:"id"`
			Owner string `json:"owner"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(r.Data, &view); err != nil {
		e.t.Fatalf("секции ядра %s: %v", kernel, err)
	}
	for _, sec := range view.Sections {
		if sec.ID == "dns" {
			return sec.Owner
		}
	}
	e.t.Fatalf("у ядра %s нет секции dns", kernel)
	return ""
}

func TestRouterDNSStateView(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)

	// Слой выключен: маршрут отвечает 404, как остальные /api/configlayer/*.
	e.setLayer(false)
	if r := e.call(http.MethodGet, rtDNSBase, nil); r.Status != http.StatusNotFound {
		t.Errorf("слой выключен: GET %s — %d %s, ожидалось 404", rtDNSBase, r.Status, r.Code)
	}

	e.enableLayer()
	v := e.rtDNSView()
	if got, _ := v["state"].(string); got != "off" {
		t.Errorf("state = %q, ожидалось off", got)
	}
	owner, _ := v["owner"].(map[string]any)
	if kind, _ := owner["kind"].(string); kind != "firmware" {
		t.Errorf("owner.kind = %q, ожидалось firmware (перехват выключен)", kind)
	}
	if name, _ := v["default_name"].(string); name == "" {
		t.Error("default_name пуст")
	}
	now, _ := v["server_now_ms"].(float64)
	if skew := time.Since(time.UnixMilli(int64(now))); skew < -30*time.Second || skew > 30*time.Second {
		t.Errorf("server_now_ms расходится с часами роутера на %v", skew.Round(time.Second))
	}
	legacy, _ := v["legacy"].(map[string]any)
	if got, want := legacy["proxy_dns"], rtDNSProxyOn(t); got != want {
		t.Errorf("legacy.proxy_dns = %v, в init-скрипте XKeen %v", got, want)
	}
	for _, key := range []string{"blocks", "steps", "zones", "zones_builtin", "upstreams"} {
		if _, ok := v[key].([]any); !ok {
			t.Errorf("%s: ожидался массив", key)
		}
	}

	t.Run("blocks", func(t *testing.T) {
		if e.rtDNSBlocks()["draft"] {
			t.Error("черновик чистый, а блокировка draft есть")
		}
		// Открытый черновик блокирует включение (D-01); сброс снимает блокировку.
		e.rtDNSDraft(map[string]any{"group": "", "zones": []string{"xcp-rt.test"}})
		if !e.rtDNSBlocks()["draft"] {
			t.Error("черновик открыт, а блокировки draft нет")
		}
		e.resetDraft()
		if e.rtDNSBlocks()["draft"] {
			t.Error("черновик сброшен, а блокировка draft осталась")
		}
		// DNS-секцию ядра ведёт не панель — блокировка section; ведёт панель — нет.
		view := e.rtDNSView()
		sectionOwner, _ := view["section_owner"].(string)
		blocks := e.rtDNSBlocks()
		if (sectionOwner == "panel") == blocks["section"] {
			t.Errorf("section_owner = %q, блокировка section = %v", sectionOwner, blocks["section"])
		}
		if owner := e.rtDNSCoresOwner(tg.Core); owner != "panel" && !blocks["section"] {
			t.Errorf("DNS-секцию %s ведёт %q, а блокировки section нет", tg.Core, owner)
		}
	})
}

func TestRouterDNSCheck(t *testing.T) {
	routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()

	// Имя по умолчанию: порт 53 у прошивки, путь — через прошивку.
	r := e.call(http.MethodPost, rtDNSCheck, map[string]string{})
	if r.Status != http.StatusOK {
		t.Fatalf("POST %s: %d %s", rtDNSCheck, r.Status, r.Code)
	}
	res := r.object(t)
	if ok, _ := res["ok"].(bool); !ok {
		t.Error("проверка имени по умолчанию не прошла")
	}
	if src, _ := res["source"].(string); src != "manual" {
		t.Errorf("source = %q, ожидалось manual", src)
	}
	groups, _ := res["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("групп в результате %d, ожидалась одна", len(groups))
	}
	g, _ := groups[0].(map[string]any)
	if path, _ := g["path"].(string); path != "firmware" {
		t.Errorf("path = %q, ожидалось firmware", path)
	}
	if answers, _ := g["answers"].([]any); len(answers) == 0 {
		t.Error("ответов нет")
	}
	if lat, _ := g["latency_ms"].(float64); lat <= 0 {
		t.Errorf("latency_ms = %v, ожидалось > 0", lat)
	}
	if name, _ := g["name"].(string); name == "" {
		t.Error("имя проверки пусто")
	}

	// Не доменное имя — 400 name_invalid, последняя проверка не меняется.
	bad := e.call(http.MethodPost, rtDNSCheck, map[string]string{"name": "not a domain/"})
	if bad.Status != http.StatusBadRequest || bad.Code != "name_invalid" {
		t.Errorf("не доменное имя: %d %q, ожидалось 400 name_invalid", bad.Status, bad.Code)
	}

	// Последняя проверка в состоянии совпадает с ответом проверки.
	v := e.rtDNSView()
	var want any
	if err := json.Unmarshal(r.Data, &want); err != nil {
		t.Fatalf("ответ проверки: %v", err)
	}
	if !reflect.DeepEqual(v["last_check"], want) {
		t.Error("last_check в состоянии не совпадает с ответом проверки")
	}
}

// Своя зона «bad zone» в черновике: «Применить» отказывает с проблемой
// dns_zone_invalid проверки слоя. Черновик сбрасывается в t.Cleanup.
func TestRouterDNSParamsCheck(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtDNSDraft(map[string]any{"group": "", "zones": []string{"bad zone"}})
	t.Cleanup(func() { e.resetDraft() })

	r := e.call(http.MethodPost, "/api/configlayer/apply", map[string]any{})
	if r.Status != http.StatusConflict || r.Code != "apply_blocked" {
		t.Fatalf("«Применить» с неверной зоной: %d %q, ожидалось 409 apply_blocked", r.Status, r.Code)
	}
	var data struct {
		Issues []struct {
			Key    string            `json:"key"`
			Code   string            `json:"code"`
			Params map[string]string `json:"params"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		t.Fatalf("проблемы проверки: %v", err)
	}
	found := false
	for _, is := range data.Issues {
		if is.Code == "dns_zone_invalid" && is.Key == "dns_takeover" && is.Params["zone"] == "bad zone" {
			found = true
		}
	}
	if !found {
		t.Errorf("нет проблемы dns_zone_invalid для зоны «bad zone» среди %d проблем", len(data.Issues))
	}
}
