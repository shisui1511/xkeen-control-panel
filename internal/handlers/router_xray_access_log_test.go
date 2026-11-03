//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
		var diff []string
		for name, sum := range got {
			if want[name] != sum {
				diff = append(diff, "+"+name)
			}
		}
		for name := range want {
			if _, ok := got[name]; !ok {
				diff = append(diff, "-"+name)
			}
		}
		slices.Sort(diff)
		t.Errorf("после возврата секции log файлы каталога конфигов Xray отличаются от исходных (файлов %d, ожидалось %d): %v", len(got), len(want), diff)
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

// rtAccFixtures пишет файлы-фикстуры чужой секции log и убирает их при выходе.
func rtAccFixtures(t *testing.T) {
	t.Helper()
	files := map[string]string{
		rtAccPure:   "// комментарий фикстуры\n{\n  \"log\": {\"loglevel\": \"warning\", \"access\": \"none\"}\n}\n",
		rtAccMixed:  "{\"log\": {\"loglevel\": \"warning\"}, \"stats\": {}}\n",
		rtAccShadow: "{\"log\": {\"access\": \"" + rtAccSideLog + "\"}, \"stats\": {}}\n",
	}
	for name, body := range files {
		path := filepath.Join(rtRtXrayDir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("запись фикстуры (длина имени %d): %v", len(name), err)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
	}
}

// Трасса: секция log за чужими файлами; поверх них смешанный файл после файла
// панели с другим access → статус показывает значение последнего файла, владелец
// xkeen или user; при открытом черновике POST access-log отвечает 409 draft_open,
// файлы те же; без черновика смешанный файл после файла панели — 409
// section_shadowed; после его удаления без ack_comments — 409
// comments_not_acknowledged, файлы те же; с ack_comments секция берётся и
// применяется одним шагом: «чистый» файл ушёл в копии, смешанный остался, файл
// панели хранит access; «Отпустить» возвращает файлы побайтно. Метка ядра: исход
// перезапуска зависит от активного ядра.
func TestRouterAccessLogForeignTake(t *testing.T) {
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

	// Очистка идёт в обратном порядке: сначала возврат секции, затем фикстуры, затем
	// сверка каталога с исходным.
	orig := rtCoresDirSums(t)
	t.Cleanup(func() {
		if got := rtCoresDirSums(t); !rtCoresSumsEqual(got, orig) {
			t.Errorf("после сценария каталог конфигов Xray отличается от исходного (файлов %d, ожидалось %d)", len(got), len(orig))
		}
	})
	rtAccFixtures(t)
	withFixtures := rtCoresDirSums(t)
	t.Cleanup(func() { rtAccReleaseLog(t, e, withFixtures) })

	// D-25: действующее значение — у последнего файла с log.
	st := e.rtAccStatus()
	if st["access_path"] != rtAccSideLog || st["enabled"] != true {
		t.Errorf("статус при смешанном файле: access_path %v, enabled %v, ожидалось значение последнего файла", st["access_path"], st["enabled"])
	}
	if owner, _ := st["owner"].(string); owner != "xkeen" && owner != "user" {
		t.Errorf("статус при чужой секции: владелец %q, ожидался xkeen или user", owner)
	}

	// Открытый черновик: взятие чужой секции отклоняется, файлы не меняются.
	rtAccDraftEdit(t, e)
	r := e.rtAccPost(true, true)
	e.rtRtExpect("access-лог при открытом черновике", r, http.StatusConflict, "draft_open")
	if got := rtCoresDirSums(t); !rtCoresSumsEqual(got, withFixtures) {
		t.Error("отказ draft_open изменил файлы каталога конфигов Xray")
	}
	e.resetDraft()

	// Смешанный файл после файла панели перекрыл бы его.
	r = e.rtAccPost(true, true)
	e.rtRtExpect("access-лог при файле после файла панели", r, http.StatusConflict, "section_shadowed")
	if r.Params["file"] != rtAccShadow {
		t.Errorf("section_shadowed: файл в параметрах не совпадает с фикстурой (длина %d)", len(r.Params["file"]))
	}
	if err := os.Remove(filepath.Join(rtRtXrayDir, rtAccShadow)); err != nil {
		t.Fatalf("удаление файла-перекрытия: %v", err)
	}
	withoutShadow := rtCoresDirSums(t)
	// Файла-перекрытия больше нет: очистка сверяет каталог уже без него.
	withFixtures = withoutShadow
	// Путь записи при включении: действующий путь или последний путь панели.
	wantAccess, _ := e.rtAccStatus()["will_write"].(string)
	if wantAccess == "" {
		t.Fatal("статус без will_write: путь записи при включении неизвестен")
	}

	// Комментарии JSONC: без согласия — отказ, файлы те же.
	r = e.rtAccPost(true, false)
	e.rtRtExpect("access-лог без согласия на потерю комментариев", r, http.StatusConflict, "comments_not_acknowledged")
	if got := rtCoresDirSums(t); !rtCoresSumsEqual(got, withoutShadow) {
		t.Error("отказ comments_not_acknowledged изменил файлы каталога конфигов Xray")
	}

	// С согласием: секция берётся и применяется одним шагом.
	var pid int
	if core == "xray" {
		pid = rtRstKernelPID(t, "xray")
	}
	view := rtAccResult(t, "взятие чужой секции log с включением", e.rtAccPost(true, true))
	rtAccRestarted(t, "взятие чужой секции log с включением", core, view, pid)
	if _, err := os.Stat(filepath.Join(rtRtXrayDir, rtAccPure)); err == nil {
		t.Error("«чистый» файл секции log остался в configs/ после взятия")
	}
	if _, err := os.Stat(filepath.Join(rtRtXrayDir, rtAccMixed)); err != nil {
		t.Errorf("смешанный файл секции log пропал из configs/: %v", err)
	}
	if got := rtAccFileAccess(t); got != wantAccess {
		t.Errorf("access в %s %q, ожидалось %q", configlayer.PanelXrayLogFile, got, wantAccess)
	}
	state := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if state.Status != http.StatusOK {
		t.Fatalf("GET /api/configlayer/state: %d %s", state.Status, state.Code)
	}
	cores, _ := state.object(t)["cores"].(map[string]any)
	xrayCores, _ := cores["xray"].(map[string]any)
	logSnap, _ := xrayCores["log"].(map[string]any)
	if taken, _ := logSnap["taken"].(bool); !taken {
		t.Error("в снимке слоя секция log не взята после взятия")
	}
	if !slices.Contains(rtCoresStrings(logSnap["moved"]), rtAccPure) {
		t.Error("«чистый» файл секции log не в списке перенесённых в копии")
	}
	if !slices.Contains(rtCoresStrings(logSnap["left"]), rtAccMixed) {
		t.Error("смешанный файл секции log не в списке оставленных на месте")
	}
	if st := e.rtAccStatus(); st["owner"] != "panel" || st["access_path"] != wantAccess {
		t.Errorf("статус после взятия: owner %v, access_path %v", st["owner"], st["access_path"])
	}
}

// --- «выключено» всегда none, свой путь помнится (D-15, D-16) ---------------------

const (
	// rtAccEmptyDir — каталог журналов сценария внутри /opt/var/log; убирается при выходе.
	rtAccEmptyDir = "/opt/var/log/xcp-rt"
	// rtAccEmptyPathA, rtAccEmptyPathB — свои пути журнала сценария.
	rtAccEmptyPathA = rtAccEmptyDir + "/a.log"
	rtAccEmptyPathB = rtAccEmptyDir + "/b.log"
	// rtAccDeniedPath — путь вне /opt/var/log: панель его не читает и не создаёт.
	rtAccDeniedPath = "/opt/etc/xray/rt.log"
)

// rtAccSaveForm сохраняет секцию log с заданным access: nil снимает ключ в форме
// (поле не прислано), иначе значение ставится как есть, в том числе пустое.
func (e *rtSrc) rtAccSaveForm(label string, access *string) {
	e.t.Helper()
	sec := rtCoresSection(e.rtCoresView("xray"), "log")
	if sec == nil {
		e.t.Fatalf("%s: в виде Xray нет секции log", label)
	}
	form, _ := sec["form"].(map[string]any)
	if form == nil {
		form = map[string]any{}
	}
	if access == nil {
		delete(form, "access")
	} else {
		form["access"] = *access
	}
	advanced, _ := sec["advanced"].(string)
	sv := e.rtCoresPost("/save", map[string]any{"kernel": "xray", "section": "log", "form": form, "advanced": advanced})
	if sv.Status != http.StatusOK {
		e.t.Fatalf("%s: save log: %d %s", label, sv.Status, sv.Code)
	}
}

// rtAccXrayTest проверяет каталог конфигов Xray самим ядром (xray run -test).
func rtAccXrayTest(t *testing.T, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, stderr, code := routertest.Exec(ctx, rtCoresXrayBin, "run", "-test", "-confdir", rtRtXrayDir)
	if code != 0 {
		t.Fatalf("%s: xray run -test вернул %d: %s", label, code, stderr)
	}
}

// rtAccSize — размер файла или -1, если файла нет.
func rtAccSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
}

// rtAccExpectDisabled проверяет, что в файле панели access равен none, ядро
// принимает каталог, а статус описывает то же состояние: журнал выключен.
func (e *rtSrc) rtAccExpectDisabled(label string) {
	e.t.Helper()
	if got := rtAccFileAccess(e.t); got != "none" {
		e.t.Fatalf("%s: access в %s %q, ожидалось none", label, configlayer.PanelXrayLogFile, got)
	}
	rtAccXrayTest(e.t, label)
	if st := e.rtAccStatus(); st["enabled"] != false {
		e.t.Errorf("%s: статус enabled %v, ожидалось false", label, st["enabled"])
	}
}

// Трасса (D-15, D-16): секция log у панели. Форма без access и с access:"" после
// «Применить» даёт в файле access none, xray run -test принимает каталог, статус
// «выключен» (ветка xray: размеры журналов XKeen до и после перезапуска пишутся в
// журнал теста без вердикта). Свой путь переживает выключение и «Сбросить»: путь
// из формы → выключение → сброс черновика → включение возвращает его. Очистка поля
// формы не теряет путь. Путь вне /opt/var/log: статус path_denied, записей нет,
// панель каталог и файл не создаёт. Метка ядра: исход перезапуска и поведение
// консольного журнала зависят от активного ядра.
func TestRouterAccessLogEmptyAccess(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	core := tg.Core
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	if core == "xray" {
		rtRstKernelPID(t, "xray")
	}
	rtAccLogGuard(t, e)
	orig := rtCoresDirSums(t)

	if err := os.MkdirAll(rtAccEmptyDir, 0o755); err != nil {
		t.Fatalf("каталог журналов сценария: %v", err)
	}
	// Убирается последним: сначала возврат секции log прежнему владельцу.
	t.Cleanup(func() {
		_ = os.RemoveAll(rtAccEmptyDir)
		if err := os.Remove(rtAccDeniedPath); err == nil && core == "xray" {
			t.Log("Xray сам создал журнал по пути вне /opt/var/log, убран при выходе")
		}
	})
	t.Cleanup(func() { rtAccReleaseLog(t, e, orig) })

	take := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"log"}, "ack_comments": true})
	if take.Status != http.StatusOK {
		t.Fatalf("take log: %d %s", take.Status, take.Code)
	}
	e.rtRtApplyOK("взятие секции log Xray")

	pathB, pathA, denied, empty := rtAccEmptyPathB, rtAccEmptyPathA, rtAccDeniedPath, ""

	t.Run("without_access", func(t *testing.T) {
		c := e.with(t)
		c.rtAccSaveForm("путь в форме", &pathB)
		c.rtRtApplyOK("access с путём")
		if got := rtAccFileAccess(t); got != pathB {
			t.Fatalf("access в %s %q, ожидалось %q", configlayer.PanelXrayLogFile, got, pathB)
		}
		var xkeenBefore, detachedBefore int64
		if core == "xray" {
			xkeenBefore = rtAccSize("/opt/var/log/xkeen.log")
			detachedBefore = rtAccSize("/opt/var/log/xkeen-detached.log")
		}
		c.rtAccSaveForm("форма без access", nil)
		c.rtRtApplyOK("форма без access")
		c.rtAccExpectDisabled("форма без access")
		if core == "xray" {
			t.Logf("журналы XKeen до и после перезапуска (байт, -1 — файла нет): xkeen.log %d -> %d, xkeen-detached.log %d -> %d",
				xkeenBefore, rtAccSize("/opt/var/log/xkeen.log"), detachedBefore, rtAccSize("/opt/var/log/xkeen-detached.log"))
		}
	})

	t.Run("empty_access", func(t *testing.T) {
		c := e.with(t)
		c.rtAccSaveForm("путь в форме", &pathB)
		c.rtRtApplyOK("access с путём")
		c.rtAccSaveForm("форма с пустым access", &empty)
		c.rtRtApplyOK("форма с пустым access")
		c.rtAccExpectDisabled("форма с пустым access")
	})

	t.Run("remembers_path", func(t *testing.T) {
		c := e.with(t)
		c.rtAccSaveForm("путь в форме", &pathA)
		c.rtRtApplyOK("свой путь access")
		rtAccResult(t, "выключение access-лога", c.rtAccPost(false, false))
		c.rtAccExpectDisabled("выключение access-лога")
		c.resetDraft()
		rtAccResult(t, "включение access-лога после сброса", c.rtAccPost(true, false))
		if got := rtAccFileAccess(t); got != pathA {
			t.Fatalf("после выключения и сброса access в %s %q, ожидался свой путь %q", configlayer.PanelXrayLogFile, got, pathA)
		}
	})

	t.Run("form_clear", func(t *testing.T) {
		c := e.with(t)
		// Состояние после remembers_path: журнал включён на своём пути.
		if got := rtAccFileAccess(t); got != pathA {
			c.rtAccSaveForm("путь в форме", &pathA)
			c.rtRtApplyOK("свой путь access")
		}
		c.rtAccSaveForm("очистка поля access", &empty)
		c.rtRtApplyOK("очистка поля access")
		c.rtAccExpectDisabled("очистка поля access")
		rtAccResult(t, "включение после очистки поля", c.rtAccPost(true, false))
		if got := rtAccFileAccess(t); got != pathA {
			t.Fatalf("после очистки поля и включения access %q, ожидался прежний путь %q", got, pathA)
		}
	})

	t.Run("outside_root", func(t *testing.T) {
		c := e.with(t)
		c.rtAccSaveForm("путь вне каталога журналов", &denied)
		c.rtRtApplyOK("путь вне каталога журналов")
		st := c.rtAccStatus()
		if st["path_denied"] != true {
			t.Errorf("статус для пути вне /opt/var/log: path_denied %v, ожидалось true", st["path_denied"])
		}
		r := c.call(http.MethodGet, rtAccStatusPath, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", rtAccStatusPath, r.Status, r.Code)
		}
		report, _ := r.object(t)["report"].(map[string]any)
		if entries, _ := report["entries"].([]any); len(entries) != 0 {
			t.Errorf("для запрещённого пути в отчёте %d записей, ожидалось 0", len(entries))
		}
		if core != "xray" {
			if _, err := os.Stat(denied); err == nil {
				t.Error("панель создала журнал по пути вне /opt/var/log")
			}
		}
	})
}
