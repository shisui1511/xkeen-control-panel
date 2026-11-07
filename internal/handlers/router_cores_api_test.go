//go:build router

// router-area: cores

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка API секций ядер на настоящей панели устройства: маршруты
// /api/configlayer/cores* вызываются по HTTPS с сессией, созданной на ПК
// (routertest.Panel). Вердикт — коды и тела ответов панели и файлы ядер на диске
// устройства. Флаг слоя «Конфигурация» на время проверки переключается и
// возвращается прежним, всё взятое возвращается «Отпустить» + «Применить»,
// черновик после сценария сбрасывается. В сообщения попадают только коды, имена
// секций и числа.

const rtCoresBase = "/api/configlayer/cores"

const (
	// rtCoresDNSPanelFile — файл панели секции dns.
	rtCoresDNSPanelFile = "99_xcp-dns.json"
	// rtCoresAPIPanelFile — файл панели с блоком api Xray.
	rtCoresAPIPanelFile = "99_xcp-api.json"
	// rtCoresXrayBin — бинарник Xray на устройстве.
	rtCoresXrayBin = "/opt/sbin/xray"
	// rtCoresFixtureDNS — файл-фикстура секции dns, если у стенда своих файлов dns нет.
	rtCoresFixtureDNS = "02_dns.json"
	// rtCoresFixtureBody — фикстура с комментарием: для проверки согласия на потерю.
	rtCoresFixtureBody = "// комментарий фикстуры\n{\n  \"dns\": {\"servers\": [\"127.0.0.1\"]}\n}\n"
)

// rtCoresXraySections — секции Xray в порядке показа.
var rtCoresXraySections = []string{"log", "dns", "inbounds", "policy", "other"}

// rtCoresOwners — допустимые владельцы секции в виде.
var rtCoresOwners = []string{"panel", "pending", "releasing", "xkeen", "user", "none", "drift"}

// rtCoresAPIKey находит в тексте файла ключ api верхнего уровня; файлы с
// комментариями не разбираются как JSON, для них — поиск по тексту.
var rtCoresAPIKey = regexp.MustCompile(`"api"\s*:`)

// rtCoresStrings приводит JSON-массив строк к срезу.
func rtCoresStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, it := range list {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// rtCoresFileSum — sha256 файла каталога конфигов Xray; пусто, если файла нет.
func rtCoresFileSum(name string) string {
	data, err := os.ReadFile(filepath.Join(rtRtXrayDir, name))
	if err != nil {
		return ""
	}
	return rtRtSum(data)
}

// rtCoresDirSums — sha256 всех .json каталога конфигов Xray.
func rtCoresDirSums(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	sums := map[string]string{}
	for _, ent := range entries {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".json") {
			sums[ent.Name()] = rtCoresFileSum(ent.Name())
		}
	}
	return sums
}

// rtCoresForeignAPI сообщает, есть ли ключ api в файлах каталога конфигов Xray, кроме
// файла панели.
func rtCoresForeignAPI(t *testing.T) (bool, string) {
	t.Helper()
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") || ent.Name() == rtCoresAPIPanelFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, ent.Name()))
		if err != nil {
			continue
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(data, &top) == nil {
			if raw, ok := top["api"]; ok {
				return true, rtCoresAPIListen(raw)
			}
			continue
		}
		if rtCoresAPIKey.Match(data) {
			return true, ""
		}
	}
	return false, ""
}

// rtCoresAPIListen достаёт api.listen из значения ключа api.
func rtCoresAPIListen(raw json.RawMessage) string {
	var api struct {
		Listen string `json:"listen"`
	}
	_ = json.Unmarshal(raw, &api)
	return api.Listen
}

// rtCoresView читает вид секций ядра.
func (e *rtSrc) rtCoresView(kernel string) map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtCoresBase+"?kernel="+kernel, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s (%s): %d %s", rtCoresBase, kernel, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtCoresSection ищет секцию в виде по id.
func rtCoresSection(view map[string]any, id string) map[string]any {
	list, _ := view["sections"].([]any)
	for _, item := range list {
		if m, _ := item.(map[string]any); m != nil && m["id"] == id {
			return m
		}
	}
	return nil
}

// rtCoresPost вызывает изменяющий маршрут секций с текущей ревизией черновика.
func (e *rtSrc) rtCoresPost(path string, body map[string]any) rtSrcResp {
	e.t.Helper()
	body["revision"] = e.revision()
	return e.call(http.MethodPost, rtCoresBase+path, body)
}

// rtCoresRelease отпускает секцию dns и применяет; для очистки при любом исходе.
func (e *rtSrc) rtCoresRelease(section string) {
	e.t.Helper()
	r := e.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": section})
	if r.Status != http.StatusOK {
		e.t.Errorf("очистка: release %s: %d %s", section, r.Status, r.Code)
		return
	}
	e.rtRtApplyOK("очистка: возврат секции " + section)
}

// Трасса: слой выключен → 404; слой включён → вид секций Xray и Mihomo; неизвестное
// ядро → 400; запросы без сессии и без CSRF отклоняются; взятие dns с согласием →
// «Применить» → файл панели, Редактор отказывает (file_managed); «Отпустить» →
// «Применить» → исходные файлы побайтно; блок api Xray при взятой маршрутизации.
// Метка ядра: сценарий читает активное ядро.
func TestRouterCoresAPIFlow(t *testing.T) {
	routertest.Slow(t, "две серии «Применить» с перезапуском ядра: взятие dns и отпускание")
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)

	t.Run("слой выключен", func(t *testing.T) {
		c := e.with(t)
		c.setLayer(false)
		r := c.call(http.MethodGet, rtCoresBase+"?kernel=xray", nil)
		c.rtRtExpect("GET cores при выключенном слое", r, http.StatusNotFound, "config_layer_disabled")
	})

	e.enableLayer()
	e.rtRstIdle()

	t.Run("вид секций", func(t *testing.T) {
		c := e.with(t)
		view := c.rtCoresView("xray")
		if view["kernel"] != "xray" {
			t.Errorf("вид Xray: kernel %v", view["kernel"])
		}
		if installed, _ := view["installed"].(bool); installed {
			list, _ := view["sections"].([]any)
			if len(list) != len(rtCoresXraySections) {
				t.Fatalf("вид Xray: секций %d, ожидалось %d", len(list), len(rtCoresXraySections))
			}
			for i, id := range rtCoresXraySections {
				m, _ := list[i].(map[string]any)
				if m == nil || m["id"] != id {
					t.Errorf("вид Xray: на месте %d не секция %s", i, id)
					continue
				}
				if owner, _ := m["owner"].(string); !slices.Contains(rtCoresOwners, owner) {
					t.Errorf("вид Xray: секция %s, владелец %q вне набора", id, owner)
				}
			}
		}

		mih := c.rtCoresView("mihomo")
		if mih["kernel"] != "mihomo" {
			t.Errorf("вид Mihomo: kernel %v", mih["kernel"])
		}
		if _, ok := mih["installed"].(bool); !ok {
			t.Error("вид Mihomo: нет признака installed")
		}

		bad := c.call(http.MethodGet, rtCoresBase+"?kernel=bad", nil)
		c.rtRtExpect("GET cores?kernel=bad", bad, http.StatusBadRequest, "invalid_input")
		unknown := c.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"nope"}, "ack_comments": true})
		c.rtRtExpect("take неизвестной секции", unknown, http.StatusBadRequest, "section_unknown")
	})

	t.Run("защита маршрутов", func(t *testing.T) {
		c := e.with(t)
		body := []byte(`{"revision":0,"kernel":"xray","sections":["dns"],"ack_comments":true}`)
		status, _, err := c.p.Request(http.MethodPost, rtCoresBase+"/take", body, routertest.AuthNoCSRF)
		if err != nil || status != http.StatusForbidden {
			t.Errorf("POST take без CSRF-токена: %d %v, ожидалось 403", status, err)
		}
		status, _, err = c.p.Request(http.MethodPost, rtCoresBase+"/take", body, routertest.AuthNone)
		if err != nil || status != http.StatusUnauthorized {
			t.Errorf("POST take без сессии: %d %v, ожидалось 401", status, err)
		}
		status, _, err = c.p.Request(http.MethodGet, rtCoresBase+"?kernel=xray", nil, routertest.AuthNone)
		if err != nil || status != http.StatusUnauthorized {
			t.Errorf("GET cores без сессии: %d %v, ожидалось 401", status, err)
		}
	})

	t.Run("dns: взятие и возврат", func(t *testing.T) {
		c := e.with(t)
		view := c.rtCoresView("xray")
		if installed, _ := view["installed"].(bool); !installed {
			t.Skip("Xray не установлен на стенде")
		}
		sec := rtCoresSection(view, "dns")
		if sec == nil {
			t.Fatal("в виде Xray нет секции dns")
		}
		if blocked, _ := sec["blocked"].(string); blocked != "" {
			// Секцию нельзя взять: API отвечает тем же кодом, что и вид.
			r := c.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}, "ack_comments": true})
			c.rtRtExpect("take заблокированной секции dns", r, http.StatusConflict, blocked)
			return
		}

		// У стенда нет файлов dns — пишется фикстура с комментарием, убирается при выходе.
		if len(rtCoresStrings(sec["files"])) == 0 {
			fixture := filepath.Join(rtRtXrayDir, rtCoresFixtureDNS)
			if err := os.WriteFile(fixture, []byte(rtCoresFixtureBody), 0o644); err != nil {
				t.Fatalf("запись фикстуры dns: %v", err)
			}
			t.Cleanup(func() { _ = os.Remove(fixture) })
			sec = rtCoresSection(c.rtCoresView("xray"), "dns")
			if sec == nil || len(rtCoresStrings(sec["files"])) == 0 {
				t.Fatal("после записи фикстуры у секции dns нет файлов")
			}
		}
		files := rtCoresStrings(sec["files"])
		left := rtCoresStrings(sec["left"])
		comments := rtCoresStrings(sec["comments"])
		before := map[string]string{}
		for _, name := range files {
			before[name] = rtCoresFileSum(name)
			if before[name] == "" {
				t.Fatalf("файл dns (длина имени %d) не читается", len(name))
			}
		}

		taken := false
		t.Cleanup(func() {
			if taken {
				e.with(t).rtCoresRelease("dns")
			}
		})

		// Предпросмотр: ровно запрошенная секция.
		pv := c.call(http.MethodGet, rtCoresBase+"/takeover?kernel=xray&sections=dns", nil)
		if pv.Status != http.StatusOK {
			t.Fatalf("GET takeover: %d %s", pv.Status, pv.Code)
		}
		if list, _ := pv.object(t)["sections"].([]any); len(list) != 1 {
			t.Errorf("предпросмотр dns: секций %d, ожидалась одна", len(list))
		}
		bad := c.call(http.MethodGet, rtCoresBase+"/takeover?kernel=xray&sections=nope", nil)
		c.rtRtExpect("takeover неизвестной секции", bad, http.StatusBadRequest, "section_unknown")

		// Комментарии не теряются без согласия.
		if len(comments) > 0 {
			r := c.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}, "ack_comments": false})
			c.rtRtExpect("take dns без согласия на потерю комментариев", r, http.StatusConflict, "comments_not_acknowledged")
			if r.Params["file"] == "" {
				t.Error("comments_not_acknowledged: в параметрах нет имени файла")
			}
		}
		r := c.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}, "ack_comments": true})
		if r.Status != http.StatusOK {
			t.Fatalf("take dns: %d %s", r.Status, r.Code)
		}
		taken = true
		if owner := rtCoresSection(c.rtCoresView("xray"), "dns")["owner"]; owner != "pending" {
			t.Errorf("dns после take: владелец %v, ожидался pending", owner)
		}
		for _, name := range files {
			if got := rtCoresFileSum(name); got != before[name] {
				t.Errorf("take изменил файл dns (длина имени %d) до «Применить»", len(name))
			}
		}

		// Секция other без взятия не правится.
		if other := rtCoresSection(c.rtCoresView("xray"), "other"); other != nil && other["owner"] != "panel" && other["owner"] != "pending" {
			sv := c.rtCoresPost("/save", map[string]any{"kernel": "xray", "section": "other", "form": map[string]any{}, "advanced": ""})
			c.rtRtExpect("save секции other без взятия", sv, http.StatusConflict, "section_not_managed")
		}

		// Разбор сырого блока: ключ api занят панелью.
		raw := c.call(http.MethodPost, rtCoresBase+"/raw/parse", map[string]any{
			"kernel": "xray", "section": "other", "text": `{"api": {"tag": "x"}}`,
		})
		if raw.Status != http.StatusOK {
			t.Fatalf("raw/parse: %d %s", raw.Status, raw.Code)
		}
		reserved := false
		rejected, _ := raw.object(t)["rejected"].([]any)
		for _, it := range rejected {
			if m, _ := it.(map[string]any); m != nil && m["key"] == "api" && m["code"] == "raw_reserved" {
				reserved = true
			}
		}
		if !reserved {
			t.Error("raw/parse: ключ api не отклонён как raw_reserved")
		}

		c.rtRtApplyOK("взятие dns")
		sec = rtCoresSection(c.rtCoresView("xray"), "dns")
		if sec["owner"] != "panel" || !slices.Equal(rtCoresStrings(sec["files"]), []string{rtCoresDNSPanelFile}) {
			t.Errorf("dns после «Применить»: владелец %v, файлы %v", sec["owner"], sec["files"])
		}
		if rtCoresFileSum(rtCoresDNSPanelFile) == "" {
			t.Fatalf("на устройстве нет %s после «Применить»", rtCoresDNSPanelFile)
		}
		for _, name := range files {
			_, statErr := os.Stat(filepath.Join(rtRtXrayDir, name))
			if slices.Contains(left, name) {
				if statErr != nil {
					t.Errorf("файл dns с другими секциями (длина имени %d) пропал из configs/", len(name))
				}
			} else if statErr == nil {
				t.Errorf("файл dns (длина имени %d) остался в configs/ после взятия", len(name))
			}
		}

		// Критерий 2: файл панели правится только через панель.
		save := c.call(http.MethodPost, "/api/config/save?path="+url.QueryEscape(filepath.Join(rtRtXrayDir, rtCoresDNSPanelFile)), map[string]any{})
		c.rtRtExpect("Редактор: запись файла панели", save, http.StatusConflict, "file_managed")

		// Отпускание ждёт «Применить»: секция ещё у панели, но правки закрыты, значение —
		// прежнее; «Управлять» (take) отменяет отпускание без изменения файлов.
		appliedDNS := rtCoresFileSum(rtCoresDNSPanelFile)
		rel := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "dns"})
		if rel.Status != http.StatusOK {
			t.Fatalf("release dns: %d %s", rel.Status, rel.Code)
		}
		if owner := rtCoresSection(c.rtCoresView("xray"), "dns")["owner"]; owner != "releasing" {
			t.Errorf("dns после release до «Применить»: владелец %v, ожидался releasing", owner)
		}
		sv := c.rtCoresPost("/save", map[string]any{"kernel": "xray", "section": "dns", "form": map[string]any{}, "advanced": ""})
		c.rtRtExpect("save секции dns в состоянии releasing", sv, http.StatusConflict, "section_not_managed")
		back := c.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}})
		if back.Status != http.StatusOK {
			t.Fatalf("take dns при отпускании: %d %s", back.Status, back.Code)
		}
		if owner := rtCoresSection(c.rtCoresView("xray"), "dns")["owner"]; owner != "panel" {
			t.Errorf("dns после отмены отпускания: владелец %v, ожидался panel", owner)
		}
		if got := rtCoresFileSum(rtCoresDNSPanelFile); got != appliedDNS {
			t.Error("отмена отпускания изменила файл панели dns")
		}

		// Возврат: исходные файлы побайтно, файла панели нет.
		c.rtCoresRelease("dns")
		taken = false
		for name, sum := range before {
			if got := rtCoresFileSum(name); got != sum {
				t.Errorf("после «Отпустить» файл dns (длина имени %d) вернулся с другим содержимым или не вернулся", len(name))
			}
		}
		if rtCoresFileSum(rtCoresDNSPanelFile) != "" {
			t.Errorf("после «Отпустить» на устройстве остался %s", rtCoresDNSPanelFile)
		}
		if owner := rtCoresSection(c.rtCoresView("xray"), "dns")["owner"]; owner == "panel" || owner == "pending" {
			t.Errorf("dns после «Отпустить»: владелец %v", owner)
		}
		c.resetDraft()
	})

	t.Run("api: блок Xray при взятой маршрутизации", func(t *testing.T) {
		c := e.with(t)
		if installed, _ := c.rtCoresView("xray")["installed"].(bool); !installed {
			t.Skip("Xray не установлен на стенде")
		}
		c.rtRtPurge()
		routingBefore := rtRtScanDisk(t)
		foreign, foreignListen := rtCoresForeignAPI(t)

		taken := false
		t.Cleanup(func() {
			cc := e.with(t)
			if taken {
				r := cc.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": cc.rtRtRevision()})
				if r.Status != http.StatusOK {
					t.Errorf("очистка: routing/release: %d %s", r.Status, r.Code)
					return
				}
				cc.rtRtApplyOK("очистка: возврат маршрутизации")
			}
			cc.rtRtPurge()
		})

		// «Мои узлы»: одна ссылка с уникальным портом; без узла маршрутизацию не взять.
		pv := c.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": rtRtUniqueLink(t)})
		if pv.Status != http.StatusOK {
			t.Fatalf("sources/preview: %d %s", pv.Status, pv.Code)
		}
		token, _ := pv.object(t)["token"].(string)
		if add := c.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": c.revision(), "token": token}); add.Status != http.StatusOK {
			t.Fatalf("sources/add: %d %s", add.Status, add.Code)
		}
		take := c.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": c.rtRtRevision()})
		if take.Status != http.StatusOK {
			t.Fatalf("routing/take: %d %s", take.Status, take.Code)
		}
		taken = true
		c.rtRtApplyOK("взятие маршрутизации")

		// Блок api: файл панели, если чужого api нет; иначе блок ведёт чужой файл.
		listen := foreignListen
		if foreign {
			if rtCoresFileSum(rtCoresAPIPanelFile) != "" {
				t.Errorf("при чужом блоке api панель записала %s", rtCoresAPIPanelFile)
			}
			if again, _ := rtCoresForeignAPI(t); !again {
				t.Error("чужой блок api пропал после «Применить»")
			}
		} else {
			data, err := os.ReadFile(filepath.Join(rtRtXrayDir, rtCoresAPIPanelFile))
			if err != nil {
				t.Fatalf("на устройстве нет %s при взятой маршрутизации Xray: %v", rtCoresAPIPanelFile, err)
			}
			var doc struct {
				API struct {
					Listen   string   `json:"listen"`
					Services []string `json:"services"`
				} `json:"api"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("%s не JSON: %v", rtCoresAPIPanelFile, err)
			}
			if !strings.HasPrefix(doc.API.Listen, "127.0.0.1:") {
				t.Error("api.listen файла панели не на 127.0.0.1")
			}
			if !slices.Contains(doc.API.Services, "HandlerService") {
				t.Error("в services файла панели нет HandlerService")
			}
			listen = doc.API.Listen
		}

		// Старый переключатель мониторинга блок панели не трогает.
		sums := rtCoresDirSums(t)
		mon := c.call(http.MethodPost, "/api/xray/grpc/monitoring", map[string]bool{"enabled": true})
		c.rtRtExpect("старое включение мониторинга", mon, http.StatusConflict, "xray_api_managed")
		if after := rtCoresDirSums(t); !rtCoresSumsEqual(sums, after) {
			t.Error("отказанное включение мониторинга изменило файлы configs/")
		}

		switch tg.Core {
		case "xray":
			// Готовность API для панели и живой ответ рабочего ядра.
			ready := false
			deadline := time.Now().Add(30 * time.Second)
			for !ready && time.Now().Before(deadline) {
				caps := c.call(http.MethodGet, "/api/capabilities", nil)
				if caps.Status == http.StatusOK {
					xr, _ := caps.object(t)["xray"].(map[string]any)
					ready, _ = xr["grpc_ready"].(bool)
				}
				if !ready {
					time.Sleep(2 * time.Second)
				}
			}
			if !ready {
				t.Error("capabilities.xray.grpc_ready не стал true за 30 с")
			}
			if listen == "" {
				t.Log("адрес api чужого блока не задан listen: живой ответ не проверялся")
				break
			}
			code := -1
			deadline = time.Now().Add(40 * time.Second)
			for code != 0 && time.Now().Before(deadline) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_, _, code = routertest.Exec(ctx, rtCoresXrayBin, "api", "statssys", "-s", listen)
				cancel()
				if code != 0 {
					time.Sleep(2 * time.Second)
				}
			}
			if code != 0 {
				t.Errorf("xray api statssys: код %d, ожидался 0", code)
			}
		case "mihomo":
			// Активно Mihomo: Xray не запускается (FND-05), файл api на диске есть.
			if pids, _ := rtRstKernelPIDs("xray"); len(pids) != 0 {
				t.Errorf("при активном Mihomo запущен Xray: процессов %d", len(pids))
			}
		}

		// Возврат: файл api уходит вместе с маршрутизацией.
		rel := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
		if rel.Status != http.StatusOK {
			t.Fatalf("routing/release: %d %s", rel.Status, rel.Code)
		}
		c.rtRtApplyOK("возврат маршрутизации")
		taken = false
		if rtCoresFileSum(rtCoresAPIPanelFile) != "" {
			t.Errorf("после «Отпустить» на устройстве остался %s", rtCoresAPIPanelFile)
		}
		c.rtRtExpectReleased(tg, routingBefore)
	})
}

// rtCoresSumsEqual сравнивает карты имя → sha256.
func rtCoresSumsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
