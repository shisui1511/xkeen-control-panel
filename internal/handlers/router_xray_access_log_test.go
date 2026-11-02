//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Включение access-лога на секции "log" без ключа access: правка конфига Xray
// должна оставаться корректным JSON. Вердикт — json.Valid результата и значения,
// которые читает сам парсер, эталонных файлов нет.

func TestRouterAccessLogLegacyEmptySection(t *testing.T) {
	tg := routertest.Current(t)

	cases := []struct {
		name string
		body string
		keep map[string]string
	}{
		{"empty_section", `{"log": {}}`, nil},
		{"empty_with_space", "{\"log\": {\n  }\n}", nil},
		{"loglevel_kept", `{"log": {"loglevel":"warning"}}`, map[string]string{"loglevel": "warning"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(tg.WorkDir, "accesslog-")
			if err != nil {
				t.Fatalf("каталог конфигов: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			file := filepath.Join(dir, "01_log.json")
			if err := os.WriteFile(file, []byte(c.body), 0o644); err != nil {
				t.Fatalf("запись конфига: %v", err)
			}

			svc := services.NewXrayAccessLogService(dir)
			if _, err := svc.SetEnabled(true); err != nil {
				t.Fatalf("SetEnabled(true): %v", err)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("чтение конфига: %v", err)
			}
			if !json.Valid(data) {
				t.Fatalf("файл после включения не JSON: %s", data)
			}
			var root struct {
				Log map[string]string `json:"log"`
			}
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatalf("разбор файла: %v", err)
			}
			if got := root.Log["access"]; got != services.DefaultXrayAccessLog {
				t.Fatalf("access = %q, ждали %q", got, services.DefaultXrayAccessLog)
			}
			for k, v := range c.keep {
				if root.Log[k] != v {
					t.Fatalf("ключ %s = %q, ждали %q", k, root.Log[k], v)
				}
			}
		})
	}
}

// --- access-лог через секцию log (PART-01, D-13, D-14, D-25) --------------------

const (
	// rtAccBase — маршрут включения access-лога.
	rtAccBase = rtCoresBase + "/access-log"
	// rtAccStatusPath — маршрут статуса access-лога.
	rtAccStatusPath = "/api/xray/access-log"
	// rtAccSideLog — путь access-лога в чужом файле сценария (внутри каталога журналов).
	rtAccSideLog = "/opt/var/log/xray/rt-last.log"
	// rtAccPure, rtAccMixed, rtAccShadow — файлы-фикстуры сценария чужой секции: «чистый»
	// с комментарием JSONC, смешанный до файла панели и смешанный после него.
	rtAccPure   = "04_rt-accesslog-pure.json"
	rtAccMixed  = "05_rt-accesslog-mixed.json"
	rtAccShadow = "zz-rt-log.json"
)

// rtAccPost вызывает POST /api/configlayer/cores/access-log; запрос идёт вместе с
// перезапуском ядра, поэтому с увеличенным пределом ожидания.
func (e *rtSrc) rtAccPost(enabled, ack bool) rtSrcResp {
	e.t.Helper()
	return e.callSlow(http.MethodPost, rtAccBase, map[string]any{"enabled": enabled, "ack_comments": ack})
}

// rtAccStatus читает status из GET /api/xray/access-log.
func (e *rtSrc) rtAccStatus() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtAccStatusPath, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtAccStatusPath, r.Status, r.Code)
	}
	status, _ := r.object(e.t)["status"].(map[string]any)
	if status == nil {
		e.t.Fatalf("в ответе %s нет status", rtAccStatusPath)
	}
	return status
}

// rtAccLogObject читает ключ log файла name каталога конфигов Xray с порядком ключей.
func rtAccLogObject(t *testing.T, name string) *configlayer.OrderedObject {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtXrayDir, name))
	if err != nil {
		t.Fatalf("на устройстве нет %s: %v", name, err)
	}
	top, err := configlayer.ParseOrderedObject(data)
	if err != nil {
		t.Fatalf("%s не JSON-объект: %v", name, err)
	}
	raw, ok := top.Get("log")
	if !ok {
		t.Fatalf("в %s нет ключа log", name)
	}
	obj, err := configlayer.ParseOrderedObject(raw)
	if err != nil {
		t.Fatalf("log в %s не объект: %v", name, err)
	}
	return obj
}

// rtAccFileAccess возвращает log.access файла панели на диске.
func rtAccFileAccess(t *testing.T) string {
	t.Helper()
	raw, ok := rtAccLogObject(t, configlayer.PanelXrayLogFile).Get("access")
	if !ok {
		t.Fatalf("в %s у log нет access", configlayer.PanelXrayLogFile)
	}
	var access string
	if err := json.Unmarshal(raw, &access); err != nil {
		t.Fatalf("log.access в %s не строка: %v", configlayer.PanelXrayLogFile, err)
	}
	return access
}

// rtAccResult проверяет итог запроса access-лога: 200, итог применён; возвращает
// data ответа.
func rtAccResult(t *testing.T, label string, r rtSrcResp) map[string]any {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("%s: %d %s", label, r.Status, r.Code)
	}
	view := r.object(t)
	if ok, code := rtRstResult(view); !ok || code != "applied" {
		t.Fatalf("%s: ok=%v code=%q; %s", label, ok, code, rtRstDescribe(view))
	}
	return view
}

// rtAccRestarted проверяет перезапуск Xray по ветке ядра: на ветке xray — исход
// restarted и новый PID (ядро держится), на ветке mihomo Xray не запущен — исход
// untouched_inactive.
func rtAccRestarted(t *testing.T, label, core string, view map[string]any, pidBefore int) {
	t.Helper()
	outcome := rtRstRestartOutcome(view, "xray")
	if core == "mihomo" {
		if outcome != "untouched_inactive" {
			t.Fatalf("%s: исход перезапуска xray %q, ожидался untouched_inactive; %s", label, outcome, rtRstDescribe(view))
		}
		return
	}
	if outcome != "restarted" {
		t.Fatalf("%s: исход перезапуска xray %q, ожидался restarted; %s", label, outcome, rtRstDescribe(view))
	}
	pid := rtRstKernelPID(t, "xray")
	if pid == pidBefore {
		t.Fatalf("%s: PID xray не сменился (%d); %s", label, pid, rtRstDescribe(view))
	}
	rtRstHoldPID(t, "xray", pid)
}

// rtAccDraftEdit открывает черновик правкой другой секции: берёт в черновик первую
// из секций dns, policy, которую можно взять. Возвращает её идентификатор; до
// «Применить» секция остаётся у прежнего владельца, файлы не меняются.
func rtAccDraftEdit(t *testing.T, e *rtSrc) string {
	t.Helper()
	view := e.rtCoresView("xray")
	for _, id := range []string{"dns", "policy"} {
		sec := rtCoresSection(view, id)
		if sec == nil {
			continue
		}
		if blocked, _ := sec["blocked"].(string); blocked != "" {
			continue
		}
		if owner, _ := sec["owner"].(string); owner == "panel" || owner == "pending" || owner == "drift" || owner == "releasing" {
			continue
		}
		r := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{id}, "ack_comments": true})
		if r.Status != http.StatusOK {
			t.Fatalf("take %s в черновик: %d %s", id, r.Status, r.Code)
		}
		return id
	}
	t.Fatal("ни одну из секций dns, policy нельзя взять в черновик: открытый черновик не воспроизвести")
	return ""
}

// rtAccLogGuard проверяет исходное состояние секции log: Xray установлен, секцию
// можно взять, она не у панели.
func rtAccLogGuard(t *testing.T, e *rtSrc) {
	t.Helper()
	view := e.rtCoresView("xray")
	if installed, _ := view["installed"].(bool); !installed {
		t.Fatal("Xray не установлен на стенде: access-лог не проверить")
	}
	sec := rtCoresSection(view, "log")
	if sec == nil {
		t.Fatal("в виде Xray нет секции log")
	}
	if blocked, _ := sec["blocked"].(string); blocked != "" {
		t.Fatalf("секцию log Xray нельзя взять: %s", blocked)
	}
	if owner, _ := sec["owner"].(string); owner == "panel" || owner == "pending" || owner == "drift" || owner == "releasing" {
		t.Fatalf("секция log уже у панели (владелец %s): стенд не в исходном состоянии", owner)
	}
}

// rtAccReleaseLog возвращает секцию log прежнему владельцу при любом исходе:
// черновик сбрасывается (иначе «Применить» применил бы и чужие правки), секция
// отпускается и применяется, исходные файлы сверяются побайтно с want.
func rtAccReleaseLog(t *testing.T, e *rtSrc, want map[string]string) {
	c := e.with(t)
	c.rtRstIdle()
	c.resetDraft()
	owner := c.rtCoresRetakeOwner("xray", "log")
	if owner == "panel" || owner == "drift" || owner == "pending" {
		r := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "log"})
		if r.Status != http.StatusOK {
			t.Errorf("очистка: release log: %d %s", r.Status, r.Code)
			return
		}
	}
	if owner == "panel" || owner == "drift" || owner == "releasing" {
		c.rtRtApplyOK("возврат секции log Xray")
	}
	if got := rtCoresDirSums(t); !rtCoresSumsEqual(got, want) {
		t.Errorf("после возврата секции log файлы каталога конфигов Xray отличаются от исходных (файлов %d, ожидалось %d)", len(got), len(want))
	}
}

// Трасса: секция log взята и применена, значение access сведено к none; в
// черновике правка другой секции (dns или policy) ждёт «Применить» → POST
// access-log enabled:true: итог применён, Xray перезапущен (ветка xray, новый PID)
// или не тронут (ветка mihomo, Xray не запущен); в файле панели access равен пути
// по умолчанию, остальные ключи log и их порядок прежние; правка в черновике на
// месте и не применена, значение log в черновике то же → enabled:false: access
// none, путь в статусе помнится → снова enabled:true: тот же путь, каталог журнала
// создан. Метка ядра: исход перезапуска зависит от активного ядра.
func TestRouterAccessLogEnable(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	core := tg.Core
	if core != "xray" && core != "mihomo" {
		t.Fatalf("неизвестное ядро цели %q", core)
	}
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	if core == "xray" {
		rtRstKernelPID(t, "xray")
	}

	rtAccLogGuard(t, e)
	orig := rtCoresDirSums(t)
	t.Cleanup(func() { rtAccReleaseLog(t, e, orig) })

	// Секция log у панели, access выключен: исходное состояние сценария.
	take := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"log"}, "ack_comments": true})
	if take.Status != http.StatusOK {
		t.Fatalf("take log: %d %s", take.Status, take.Code)
	}
	e.rtRtApplyOK("взятие секции log Xray")
	sec := rtCoresSection(e.rtCoresView("xray"), "log")
	form, _ := sec["form"].(map[string]any)
	if form == nil {
		form = map[string]any{}
	}
	if cur, _ := form["access"].(string); cur != "none" {
		form["access"] = "none"
		advanced, _ := sec["advanced"].(string)
		sv := e.rtCoresPost("/save", map[string]any{"kernel": "xray", "section": "log", "form": form, "advanced": advanced})
		if sv.Status != http.StatusOK {
			t.Fatalf("save log: %d %s", sv.Status, sv.Code)
		}
		e.rtRtApplyOK("access none в секции log Xray")
	}
	if got := rtAccFileAccess(t); got != "none" {
		t.Fatalf("access в %s %q, ожидалось none", configlayer.PanelXrayLogFile, got)
	}

	// Открытый черновик: правка другой секции.
	other := rtAccDraftEdit(t, e)
	otherFile := configlayer.PanelXraySectionFile(other)
	if rtCoresFileSum(otherFile) != "" {
		t.Fatalf("на устройстве уже есть %s до «Применить»", otherFile)
	}
	before := rtAccLogObject(t, configlayer.PanelXrayLogFile)

	// Включение.
	var pid int
	if core == "xray" {
		pid = rtRstKernelPID(t, "xray")
	}
	view := rtAccResult(t, "включение access-лога", e.rtAccPost(true, false))
	rtAccRestarted(t, "включение access-лога", core, view, pid)

	after := rtAccLogObject(t, configlayer.PanelXrayLogFile)
	if got := rtAccFileAccess(t); got != services.DefaultXrayAccessLog {
		t.Fatalf("access в %s %q, ожидалось %q", configlayer.PanelXrayLogFile, got, services.DefaultXrayAccessLog)
	}
	if !slices.Equal(before.Keys(), after.Keys()) {
		t.Errorf("после включения набор или порядок ключей log изменился: было %v, стало %v", before.Keys(), after.Keys())
	}
	for _, k := range before.Keys() {
		if k == "access" {
			continue
		}
		b, _ := before.Get(k)
		a, _ := after.Get(k)
		if string(b) != string(a) {
			t.Errorf("после включения изменился ключ %s секции log", k)
		}
	}
	if _, err := os.Stat(filepath.Dir(services.DefaultXrayAccessLog)); err != nil {
		t.Errorf("каталог журнала не создан: %v", err)
	}
	if st := e.rtAccStatus(); st["owner"] != "panel" || st["enabled"] != true || st["access_path"] != services.DefaultXrayAccessLog {
		t.Errorf("статус после включения: owner %v, enabled %v, access_path %v", st["owner"], st["enabled"], st["access_path"])
	}

	// Правка другой секции осталась в черновике и не применена; log в черновике не
	// отличается от применённого.
	if owner := e.rtCoresRetakeOwner("xray", other); owner != "pending" {
		t.Errorf("секция %s после включения: владелец %q, ожидался pending", other, owner)
	}
	if rtCoresFileSum(otherFile) != "" {
		t.Errorf("включение access-лога применило правку секции %s: на устройстве появился %s", other, otherFile)
	}
	if n := e.rtRtDraftChanges(); n == 0 {
		t.Error("черновик после включения пуст: правка другой секции пропала")
	}
	sec = rtCoresSection(e.rtCoresView("xray"), "log")
	value, _ := sec["value"].(map[string]any)
	if value["access"] != services.DefaultXrayAccessLog {
		t.Errorf("значение log в черновике: access %v, ожидалось %q", value["access"], services.DefaultXrayAccessLog)
	}
	if dirty, _ := sec["dirty"].(bool); dirty {
		t.Error("секция log отличается в черновике и применённом состоянии после включения")
	}

	// Выключение: access none, путь помнится.
	if core == "xray" {
		pid = rtRstKernelPID(t, "xray")
	}
	view = rtAccResult(t, "выключение access-лога", e.rtAccPost(false, false))
	rtAccRestarted(t, "выключение access-лога", core, view, pid)
	if got := rtAccFileAccess(t); got != "none" {
		t.Fatalf("после выключения access в %s %q, ожидалось none", configlayer.PanelXrayLogFile, got)
	}
	if st := e.rtAccStatus(); st["enabled"] != false || st["will_write"] != services.DefaultXrayAccessLog {
		t.Errorf("статус после выключения: enabled %v, will_write %v, ожидался прежний путь", st["enabled"], st["will_write"])
	}
	if owner := e.rtCoresRetakeOwner("xray", other); owner != "pending" {
		t.Errorf("секция %s после выключения: владелец %q, ожидался pending", other, owner)
	}

	// Снова включение: тот же путь.
	if core == "xray" {
		pid = rtRstKernelPID(t, "xray")
	}
	view = rtAccResult(t, "повторное включение access-лога", e.rtAccPost(true, false))
	rtAccRestarted(t, "повторное включение access-лога", core, view, pid)
	if got := rtAccFileAccess(t); got != services.DefaultXrayAccessLog {
		t.Errorf("после повторного включения access %q, ожидалось %q", got, services.DefaultXrayAccessLog)
	}
}
