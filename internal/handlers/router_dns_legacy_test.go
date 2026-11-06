//go:build router

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Стык перехвата DNS роутера со старым перенаправлением XKeen (D-33, D-35, D-36)
// на настоящей панели устройства. Вердикт — факты устройства: строка proxy_dns
// init-скрипта XKeen, действующий перехват XKeen с :53 (правила iptables, набор
// сверяется с набором до включения), кто держит порт 53 (/proc), override в рабочей
// конфигурации прошивки. Стенд готовит и возвращает rtDNSPrepare
// (router_dns_enable_test.go); строка proxy_dns возвращается к значению до теста.

// rtDNSLegacyOffPath — «Выключить старое перенаправление».
const rtDNSLegacyOffPath = rtDNSBase + "/legacy-off"

// rtDNSLegacyFlag — legacy.proxy_dns из вида.
func rtDNSLegacyFlag(v map[string]any) bool {
	l, _ := v["legacy"].(map[string]any)
	on, _ := l["proxy_dns"].(bool)
	return on
}

// rtDNSRestoreProxyLine — t.Cleanup: перехват выключен, строка proxy_dns равна
// before; если её пришлось менять, запущенное ядро перезапускается, чтобы XKeen
// перестроил правила по строке.
func rtDNSRestoreProxyLine(t *testing.T, e *rtSrc, before bool) {
	t.Helper()
	t.Cleanup(func() {
		c := e.with(t)
		c.rtDNSEnsureOff()
		if rtDNSProxyOn(t) == before {
			return
		}
		arg := "off"
		if before {
			arg = "on"
		}
		rtDNSXKeen(t, "-dns", arg)
		c.rtDNSServiceControl("restart")
		rtCoresLiveWaitStatus(t)
	})
}

// Трасса: подготовка → txn_off: proxy_dns=on и действующий перехват XKeen с :53,
// вид legacy.proxy_dns=true → включение и «Оставить» → строка off, правил XKeen с
// :53 нет → выключение перехвата → строка остаётся off (D-33: её возвращают откат
// и выключение флага слоя); rollback_restores: proxy_dns=on → включение → «Откатить
// сейчас» → строка снова on и набор правил XKeen с :53 равен набору до включения;
// drift: перехват включён, `xkeen -dns on` мимо панели с перезапуском ядра → вид
// legacy.proxy_dns=true, правила XKeen с :53 есть → legacy-off → строка off,
// перехват включён, :53 у ядра, правил XKeen с :53 нет. Метка ядра: перехват
// ведёт активное ядро, правила XKeen строятся при его старте.
func TestRouterDNSLegacyProxyDNS(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	rtDNSRestoreProxyLine(t, e, rtDNSProxyOn(t))

	t.Run("txn_off", func(t *testing.T) {
		c := e.with(t)
		rtDNSLegacyOn(t, c)
		if v := c.rtDNSView(); !rtDNSLegacyFlag(v) {
			t.Errorf("при proxy_dns=on вид legacy.proxy_dns=false: %v", v["legacy"])
		}
		c.rtDNSRequireClosedDraft()
		on := c.rtDNSEnableOK()
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		if rtDNSProxyOn(t) {
			t.Error("после включения перехвата строка proxy_dns осталась on")
		}
		if rtDNSLegacyFlag(on) {
			t.Error("после включения перехвата вид legacy.proxy_dns=true")
		}
		rtDNSWaitXKeenRules53(t, nil)

		c.rtDNSStart(rtDNSDisable)
		off := c.rtDNSWaitDone()
		if st, _ := off["state"].(string); st != "off" {
			code, detail := rtDNSFailure(off)
			t.Fatalf("после выключения state %q, ожидалось off: сбой %q %.300s", st, code, detail)
		}
		if rtDNSProxyOn(t) {
			t.Error("выключение перехвата вернуло строку proxy_dns в on: её возвращают только откат и выключение флага слоя")
		}
	})

	t.Run("rollback_restores", func(t *testing.T) {
		c := e.with(t)
		baseline := rtDNSLegacyOn(t, c)
		c.rtDNSRequireClosedDraft()
		overrideBefore := rtDNSOverrideOn(t)
		c.rtDNSEnableWindow()
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		if rtDNSProxyOn(t) {
			t.Error("в окне подтверждения строка proxy_dns осталась on")
		}
		c.rtDNSStart(rtDNSRollbackPath)
		off := c.rtDNSWaitState("off", rtDNSStateWait)
		if code, detail := rtDNSFailure(off); code != "" {
			t.Errorf("после «Откатить сейчас» сбой %q: %.300s", code, detail)
		}
		c.rtDNSExpectRolledBack(off, kernel, overrideBefore)
		if !rtDNSProxyOn(t) {
			t.Error("после отката строка proxy_dns не вернулась в on")
		}
		if !rtDNSLegacyFlag(off) {
			t.Error("после отката вид legacy.proxy_dns=false")
		}
		// Прежнее перенаправление действует, а не только строка.
		rtDNSWaitXKeenRules53(t, baseline)
	})

	t.Run("drift", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSRequireClosedDraft()
		c.rtDNSEnableOK()
		t.Cleanup(func() { c.rtDNSEnsureOff() })
		// proxy_dns включили мимо панели; перезапуск ядра делает перенаправление
		// действующим — второй механизм на :53.
		if baseline := rtDNSLegacyOn(t, c); len(baseline) == 0 {
			t.Fatal("после xkeen -dns on и перезапуска ядра правил XKeen с :53 нет")
		}
		drift := c.rtDNSView()
		if !rtDNSLegacyFlag(drift) {
			t.Errorf("дрейф proxy_dns=on: вид legacy.proxy_dns=false: %v", drift["legacy"])
		}
		if st, _ := drift["state"].(string); st != "on" {
			t.Errorf("при дрейфе state %q: панель сама перехват не меняет, ожидалось on", st)
		}

		r := c.rtDNSPost(rtDNSLegacyOffPath)
		if r.Status != http.StatusOK {
			t.Fatalf("POST %s: %d %s %s", rtDNSLegacyOffPath, r.Status, r.Code, r.Detail)
		}
		v := r.object(t)
		if rtDNSLegacyFlag(v) {
			t.Error("после legacy-off вид legacy.proxy_dns=true")
		}
		if rtDNSProxyOn(t) {
			t.Error("после legacy-off строка proxy_dns осталась on")
		}
		if st, _ := v["state"].(string); st != "on" {
			code, detail := rtDNSFailure(v)
			t.Errorf("после legacy-off state %q, ожидалось on: сбой %q %.300s", st, code, detail)
		}
		if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerKernel || got.Kernel != kernel {
			t.Errorf("после legacy-off порт 53 держит %q/%q, ожидалось ядро %s", got.Kind, got.Kernel, kernel)
		}
		if !rtDNSResolves(dnstakeover.DefaultCheckName) {
			t.Error("после legacy-off внешнее имя не разрешается через 127.0.0.1:53")
		}
		rtDNSWaitXKeenRules53(t, nil)

		// Повтор при выключенной строке — тот же вид без перезапуска.
		pid := rtRstKernelPID(t, kernel)
		switch again := c.rtDNSPost(rtDNSLegacyOffPath); {
		case again.Status != http.StatusOK:
			t.Errorf("повторный legacy-off: %d %s %s", again.Status, again.Code, again.Detail)
		case rtDNSLegacyFlag(again.object(t)):
			t.Error("повторный legacy-off: вид legacy.proxy_dns=true")
		}
		if got := rtRstKernelPID(t, kernel); got != pid {
			t.Errorf("повторный legacy-off при строке off перезапустил %s: PID %d -> %d", kernel, pid, got)
		}
	})
}

// rtDNSPanelFiles — пути файлов панели из снимка слоя (owner panel).
func (e *rtSrc) rtDNSPanelFiles() []string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	files, _ := r.object(e.t)["files"].([]any)
	var out []string
	for _, it := range files {
		f, _ := it.(map[string]any)
		if p, _ := f["path"].(string); p != "" && f["owner"] == "panel" {
			out = append(out, p)
		}
	}
	return out
}

// rtDNSStateFile — файл состояния службы перехвата в каталоге данных панели:
// читается и при выключенном слое, когда GET /api/configlayer/dns отвечает 404.
func rtDNSStateFile(t *testing.T) (desired bool, window bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.Default().DataDir, "dns_takeover.json"))
	if err != nil {
		t.Fatalf("файл состояния перехвата не читается: %v", err)
	}
	var st struct {
		Desired bool            `json:"desired"`
		Window  json.RawMessage `json:"window"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("файл состояния перехвата не разбирается: %v", err)
	}
	return st.Desired, len(st.Window) > 0 && string(st.Window) != "null"
}

// Трасса: подготовка → proxy_dns=on и действующий перехват XKeen с :53 (набор до
// включения) → включение и «Оставить» → строка off, правил XKeen с :53 нет →
// POST /api/settings/config-layer {enabled:false} → 200, флаг снят; порт 53 у
// прошивки и она отвечает через 127.0.0.1:53, override как до включения, строка
// proxy_dns on (из снимка до включения), набор правил XKeen с :53 равен набору до
// включения, если dns.servers остался в файлах ядра, иначе правил с :53 нет
// (старое перенаправление действует), слушателя панели в файлах ядра
// нет, файлы панели перенесены, в файле состояния перехвата desired=false. Слой
// включается обратно до возврата стенда; флаг и строка — прежние. Метка ядра:
// возврат перезапускает активное ядро, правила XKeen строятся при его старте.
func TestRouterDNSFlagOff(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	rtDNSRestoreProxyLine(t, e, rtDNSProxyOn(t))
	// Возврат стенда rtDNSPrepare идёт через API слоя: слой включается раньше него.
	t.Cleanup(func() {
		c := e.with(t)
		c.setLayer(true)
		c.rtRstIdle()
	})
	baseline := rtDNSLegacyOn(t, e)
	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSRequireClosedDraft()
	e.rtDNSEnableOK()
	if rtDNSProxyOn(t) {
		t.Error("после включения перехвата строка proxy_dns осталась on")
	}
	rtDNSWaitXKeenRules53(t, nil)
	panelFiles := e.rtDNSPanelFiles()
	if len(panelFiles) == 0 {
		t.Fatal("в снимке слоя нет файлов панели: переносить при выключении нечего")
	}

	r := e.callSlow(http.MethodPost, "/api/settings/config-layer", map[string]bool{"enabled": false})
	if r.Status != http.StatusOK {
		t.Fatalf("выключение флага слоя при включённом перехвате: %d %s %.300s", r.Status, r.Code, r.Detail)
	}
	if e.layerFlag() {
		t.Fatal("после выключения флаг слоя остался включён")
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после выключения флага порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после выключения флага прошивка не отвечает через 127.0.0.1:53")
	}
	if got := rtDNSOverrideOn(t); got != overrideBefore {
		t.Errorf("после выключения флага override=%v, до включения был %v", got, overrideBefore)
	}
	if !rtDNSProxyOn(t) {
		t.Error("после выключения флага строка proxy_dns не вернулась в on")
	}
	// Старое перенаправление действует, а не только строка. XKeen строит правила с
	// :53 только при наличии dns.servers в файлах Xray: если секция dns была
	// файлом панели и ушла вместе с ним при выключении флага, правил с :53 нет, и
	// это верный исход — перенаправлять DNS некуда.
	wantRules := baseline
	if kernel == configlayer.KernelXray && !rtDNSXrayHasDNSServers(t) {
		wantRules = nil
	}
	rtDNSWaitXKeenRules53(t, wantRules)
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после выключения флага в файлах %s остался слушатель :53", kernel)
	}
	for _, p := range panelFiles {
		if rtRstExists(p) {
			t.Errorf("после выключения флага файл панели %s остался на месте", filepath.Base(p))
		}
	}
	if desired, window := rtDNSStateFile(t); desired || window {
		t.Errorf("после выключения флага в состоянии перехвата desired=%v, окно=%v", desired, window)
	}
}

// rtDNSXrayHasDNSServers — в каталоге файлов Xray есть секция dns с servers: по
// ней XKeen решает, строить ли правила перехвата с :53.
func rtDNSXrayHasDNSServers(t *testing.T) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(rtRtXrayDir, "*.json"))
	if err != nil {
		t.Fatalf("файлы Xray не перечислены: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc struct {
			DNS struct {
				Servers json.RawMessage `json:"servers"`
			} `json:"dns"`
		}
		if json.Unmarshal(data, &doc) == nil {
			if n := len(doc.DNS.Servers); n > 0 && string(doc.DNS.Servers) != "null" {
				return true
			}
			continue
		}
		// Файл с комментариями не разбирается как JSON: грубая проверка по тексту.
		if bytes.Contains(data, []byte(`"dns"`)) && bytes.Contains(data, []byte(`"servers"`)) {
			return true
		}
	}
	return false
}
