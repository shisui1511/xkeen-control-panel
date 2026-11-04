//go:build router

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

func TestRouterDNSStateView(t *testing.T) {
	routertest.CoreSpecific(t)
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
