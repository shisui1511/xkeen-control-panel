//go:build router

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка API маршрутизации на настоящей панели устройства: маршруты
// /api/configlayer/routing* вызываются по HTTPS с сессией, созданной на ПК
// (routertest.Panel). Вердикт — коды и тела ответов панели, файлы ядер на диске
// устройства и Clash API Mihomo через панель. Флаг слоя «Конфигурация» на время
// проверки переключается и возвращается прежним, маршрутизация возвращается
// «Отпустить» + «Применить», черновик после сценария сбрасывается. В сообщения
// попадают только коды и числа.

const rtRoutingBase = "/api/configlayer/routing"

const (
	// rtRtXrayDir — каталог конфигов Xray по умолчанию.
	rtRtXrayDir = "/opt/etc/xray/configs"
	// rtRtMihomoDir — каталог Mihomo по умолчанию.
	rtRtMihomoDir = "/opt/etc/mihomo"
	// rtRtPanelProfile — профиль панели внутри каталога Mihomo.
	rtRtPanelProfile = "profiles/xcp-main.yaml"
	// rtRtProviderWait — сколько ждать провайдера «Моих узлов» в Clash API после
	// перезапуска Mihomo.
	rtRtProviderWait = 90 * time.Second
)

// rtRtDiskState — слепок файлов маршрутизации ядер на диске устройства.
type rtRtDiskState struct {
	// xray — файлы каталога конфигов Xray с ключом routing верхнего уровня: имя → sha256.
	xray map[string]string
	// xrayPure — из них «чистые»: в файле нет ничего, кроме routing.
	xrayPure map[string]bool
	// mihomoTarget — путь, на который указывает config.yaml Mihomo ("" — файла нет).
	mihomoTarget string
	// mihomoSum — sha256 содержимого config.yaml.
	mihomoSum string
}

func rtRtSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rtRtScanDisk снимает слепок файлов routing Xray и config.yaml Mihomo.
func rtRtScanDisk(t *testing.T) rtRtDiskState {
	t.Helper()
	st := rtRtDiskState{xray: map[string]string{}, xrayPure: map[string]bool{}}
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, ent.Name()))
		if err != nil {
			continue
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(data, &top) != nil {
			continue
		}
		if _, ok := top["routing"]; !ok {
			continue
		}
		st.xray[ent.Name()] = rtRtSum(data)
		st.xrayPure[ent.Name()] = len(top) == 1
	}
	cfg := filepath.Join(rtRtMihomoDir, "config.yaml")
	if target, err := filepath.EvalSymlinks(cfg); err == nil {
		st.mihomoTarget = target
		if data, err := os.ReadFile(cfg); err == nil {
			st.mihomoSum = rtRtSum(data)
		}
	}
	return st
}

// rtRtUniqueLink берёт ссылку фикстуры и подставляет порт, которого нет у других
// источников стенда.
func rtRtUniqueLink(t *testing.T) string {
	t.Helper()
	return rtRtUniqueLinkOf(t, "vless_ws", 0)
}

// rtRtUniqueLinkOf — как rtRtUniqueLink для заданной фикстуры; salt разводит порты
// нескольких ссылок, взятых подряд.
func rtRtUniqueLinkOf(t *testing.T, fixture string, salt int) string {
	t.Helper()
	link := rtLinkText(t, fixture)
	re := regexp.MustCompile(`(@[^:/?#]+):\d+`)
	port := 20000 + int((time.Now().UnixNano()+int64(salt)*7919)%30000)
	if !re.MatchString(link) {
		t.Fatal("в ссылке фикстуры нет порта")
	}
	return re.ReplaceAllString(link, "${1}:"+strconv.Itoa(port))
}

// rtRtView читает представление маршрутизации.
func (e *rtSrc) rtRtView() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtRoutingBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtRoutingBase, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtRtRevision — ревизия черновика из представления маршрутизации.
func (e *rtSrc) rtRtRevision() int64 {
	e.t.Helper()
	rev, _ := e.rtRtView()["revision"].(float64)
	return int64(rev)
}

// rtRtFindGroup ищет группу представления по имени.
func rtRtFindGroup(view map[string]any, name string) map[string]any {
	groups, _ := view["groups"].([]any)
	for _, g := range groups {
		if m, _ := g.(map[string]any); m != nil && m["name"] == name {
			return m
		}
	}
	return nil
}

// rtRtExpect требует от ответа статус и код ошибки.
func (e *rtSrc) rtRtExpect(label string, r rtSrcResp, status int, code string) {
	e.t.Helper()
	if r.Status != status || r.Code != code {
		e.t.Errorf("%s: %d %q, ожидалось %d %q", label, r.Status, r.Code, status, code)
	}
}

// rtRtApplyOK запускает «Применить», ждёт итог и требует успех.
func (e *rtSrc) rtRtApplyOK(label string) {
	e.t.Helper()
	before := e.rtRstStartApply()
	a := e.rtRstWaitApply(before)
	if ok, code := rtRstResult(a); !ok {
		e.t.Fatalf("«Применить» (%s): код %q: %s", label, code, rtRstDescribe(a))
	}
}

// rtRtDraftChanges — число изменений черновика относительно применённого.
func (e *rtSrc) rtRtDraftChanges() int {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	n, _ := r.object(e.t)["draft_changes"].(float64)
	return int(n)
}

// rtRtPurge убирает следы маршрутизации и узлов: удаляет узлы «Моих узлов» и
// секции groups, rules, probe и routing, затем применяет, если черновик отличается
// от применённого. Состояние стенда после этого — как до первого узла: группы по
// умолчанию создадутся заново вместе с первым источником.
func (e *rtSrc) rtRtPurge() {
	e.t.Helper()
	for id := e.rtRstMyEntry(); id != ""; id = e.rtRstMyEntry() {
		e.rtRstDeleteMyNode(id)
	}
	for _, section := range []string{"groups", "rules", "probe", "routing"} {
		r := e.call(http.MethodPost, "/api/configlayer/draft", map[string]any{"revision": e.revision(), "section": section, "value": nil})
		if r.Status != http.StatusOK {
			e.t.Fatalf("удаление секции №%d черновика: %d %s", len(section), r.Status, r.Code)
		}
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtRtApplyOK("очистка")
	}
	e.rtRstIdle()
}

// rtRtOwnership читает владение маршрутизацией ядра из GET /api/configlayer/state.
func (e *rtSrc) rtRtOwnership(kernel string) (desired, taken bool) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	routing, _ := r.object(e.t)["routing"].(map[string]any)
	desired, _ = routing["desired"].(bool)
	if k, _ := routing[kernel].(map[string]any); k != nil {
		taken, _ = k["taken"].(bool)
	}
	return desired, taken
}

// rtRtProviderNodes ждёт ответа Clash API (через панель) по провайдеру xcp-my и
// возвращает число его узлов; -1 — провайдера нет за отведённое время.
func (e *rtSrc) rtRtProviderNodes() int {
	e.t.Helper()
	deadline := time.Now().Add(rtRtProviderWait)
	for {
		r := e.call(http.MethodGet, "/api/mihomo/proxy/providers/proxies/xcp-my", nil)
		if r.Status == http.StatusOK {
			var body struct {
				Proxies []json.RawMessage `json:"proxies"`
			}
			if json.Unmarshal(r.Raw, &body) == nil && body.Proxies != nil {
				return len(body.Proxies)
			}
		}
		if time.Now().After(deadline) {
			return -1
		}
		time.Sleep(2 * time.Second)
	}
}

// rtRtExpectTaken сверяет диск и Clash API с состоянием «маршрутизацией управляет
// панель» для активного ядра.
func (e *rtSrc) rtRtExpectTaken(tg routertest.Target, before rtRtDiskState, nodes int) {
	e.t.Helper()
	switch tg.Core {
	case "xray":
		if _, err := os.Stat(filepath.Join(rtRtXrayDir, "99_xcp-routing.json")); err != nil {
			e.t.Errorf("Xray: нет 99_xcp-routing.json после «Применить»: %v", err)
		}
		now := rtRtScanDisk(e.t)
		for name := range before.xray {
			if before.xrayPure[name] {
				if _, still := now.xray[name]; still {
					e.t.Errorf("Xray: «чистый» файл routing №%d остался в configs/", len(name))
				}
			}
		}
	case "mihomo":
		now := rtRtScanDisk(e.t)
		if !strings.HasSuffix(now.mihomoTarget, rtRtPanelProfile) {
			e.t.Errorf("Mihomo: config.yaml не указывает на %s", rtRtPanelProfile)
		}
		if got := e.rtRtProviderNodes(); got != nodes {
			e.t.Errorf("Mihomo: в провайдере xcp-my узлов %d, ожидалось %d", got, nodes)
		}
	}
}

// rtRtExpectReleased сверяет диск с состоянием до взятия.
func (e *rtSrc) rtRtExpectReleased(tg routertest.Target, before rtRtDiskState) {
	e.t.Helper()
	now := rtRtScanDisk(e.t)
	switch tg.Core {
	case "xray":
		for name, sum := range before.xray {
			got, ok := now.xray[name]
			if !ok {
				e.t.Errorf("Xray: файл routing (длина имени %d) не вернулся после «Отпустить»", len(name))
			} else if got != sum {
				e.t.Errorf("Xray: файл routing (длина имени %d) вернулся с другим содержимым", len(name))
			}
		}
	case "mihomo":
		if now.mihomoTarget != before.mihomoTarget {
			e.t.Error("Mihomo: config.yaml не вернулся на прежний профиль")
		}
		if now.mihomoSum != before.mihomoSum {
			e.t.Error("Mihomo: содержимое config.yaml после «Отпустить» отличается от исходного")
		}
	}
}

// Трасса: флаг → «Мои узлы» → группы по умолчанию → группа → take → «Применить» →
// маршрутизация активного ядра у панели → release → «Применить» → всё как было.
// Метка ядра: тест читает активное ядро и меняет его маршрутизацию.
func TestRouterRoutingAPIFlow(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	e.rtRtPurge()
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	before := rtRtScanDisk(t)
	taken := false

	// Маршрутизация и следы сценария убираются при любом исходе: release →
	// «Применить», затем очистка узлов и секций. Cleanup регистрируется после
	// enableLayer и потому идёт раньше сброса черновика и возврата флага.
	t.Cleanup(func() {
		c := e.with(t)
		if taken {
			r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
			if r.Status != http.StatusOK {
				t.Errorf("очистка: release: %d %s", r.Status, r.Code)
				return
			}
			c.rtRtApplyOK("очистка: возврат")
		}
		c.rtRtPurge()
	})

	// «Мои узлы»: одна ссылка с уникальным портом.
	link := rtRtUniqueLink(t)
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link})
	if pv.Status != http.StatusOK {
		t.Fatalf("sources/preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); add.Status != http.StatusOK {
		t.Fatalf("sources/add: %d %s", add.Status, add.Code)
	}

	// Представление: группы по умолчанию появились вместе с первым источником.
	view := e.rtRtView()
	if hs, _ := view["has_sources"].(bool); !hs {
		t.Error("представление: has_sources=false после добавления узла")
	}
	rtAssertContractKeys(t, contract, view, "RoutingView", "routing")
	if done, _ := view["defaults_done"].(bool); !done {
		t.Error("представление: defaults_done=false после добавления первого источника")
	}
	for _, name := range []string{"Авто", "Прокси"} {
		if rtRtFindGroup(view, name) == nil {
			t.Errorf("представление: нет группы по умолчанию №%d", len(name))
		}
	}

	// groups/save: новая ручная группа получает идентификатор из 8 hex.
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "Проверка", "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{{"kind": "direct"}},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	saved, _ := save.object(t)["group"].(map[string]any)
	if id, _ := saved["id"].(string); len(id) != 8 {
		t.Errorf("groups/save: идентификатор группы длиной %d, ожидалось 8", len(id))
	}
	if ev, _ := save.object(t)["event"].(map[string]any); ev == nil || ev["draft_revision"] == nil {
		t.Errorf("groups/save: в ответе нет event.draft_revision")
	}
	if rtRtFindGroup(e.rtRtView(), "Проверка") == nil {
		t.Error("представление: сохранённой группы нет")
	}
	gid, _ := saved["id"].(string)
	manual := func(name string) map[string]any {
		return map[string]any{
			"name": name, "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{{"kind": "direct"}},
		}
	}

	// Коды ошибок записи группы.
	e.rtRtExpect("groups/save: то же имя", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": e.rtRtRevision(), "group": manual("проверка")}), http.StatusConflict, "group_name_taken")
	bad := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{"revision": e.rtRtRevision(), "group": manual("")})
	e.rtRtExpect("groups/save: пустое имя", bad, http.StatusBadRequest, "invalid_input")
	if bad.Params["field"] != "name" || bad.Params["reason"] != "name_empty" {
		t.Errorf("groups/save: пустое имя: params %v, ожидались field=name reason=name_empty", bad.Params)
	}
	e.rtRtExpect("groups/save: старая ревизия", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": 0, "group": manual("Другая")}), http.StatusConflict, "draft_conflict")
	ghost := manual("Другая")
	ghost["id"] = "ffffffff"
	e.rtRtExpect("groups/save: неизвестная группа", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": e.rtRtRevision(), "group": ghost}), http.StatusNotFound, "group_not_found")

	// groups/preview: ответ по установленным ядрам, группа не сохраняется.
	gp := e.call(http.MethodPost, rtRoutingBase+"/groups/preview", map[string]any{"group": map[string]any{
		"name": "Предпросмотр", "mode": "auto", "sources": map[string]any{"all": true},
	}})
	if gp.Status != http.StatusOK {
		t.Errorf("groups/preview: %d %s", gp.Status, gp.Code)
	} else {
		rtAssertContractKeys(t, contract, gp.object(t), "GroupPreview", "groups/preview")
	}
	if rtRtFindGroup(e.rtRtView(), "Предпросмотр") != nil {
		t.Error("groups/preview сохранил группу")
	}

	// Правила: сохранить, выключить, включить, переставить, удалить; коды ошибок.
	ruleBody := func(domain string) map[string]any {
		return map[string]any{
			"enabled": true,
			"conds":   []map[string]any{{"type": "domain", "values": []string{domain}}},
			"target":  map[string]string{"kind": "group", "id": gid},
		}
	}
	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{"revision": e.rtRtRevision(), "rule": ruleBody("example.com")})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	savedRule, _ := rs.object(t)["rule"].(map[string]any)
	rid, _ := savedRule["id"].(string)
	if len(rid) != 8 {
		t.Fatalf("rules/save: идентификатор правила длиной %d, ожидалось 8", len(rid))
	}
	rs2 := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{"revision": e.rtRtRevision(), "rule": ruleBody("example.org")})
	if rs2.Status != http.StatusOK {
		t.Fatalf("rules/save (второе): %d %s", rs2.Status, rs2.Code)
	}
	secondRule, _ := rs2.object(t)["rule"].(map[string]any)
	rid2, _ := secondRule["id"].(string)
	for _, enabled := range []bool{false, true} {
		r := e.call(http.MethodPost, rtRoutingBase+"/rules/enable", map[string]any{"revision": e.rtRtRevision(), "id": rid, "enabled": enabled})
		if r.Status != http.StatusOK {
			t.Errorf("rules/enable enabled=%v: %d %s", enabled, r.Status, r.Code)
		}
	}
	if r := e.call(http.MethodPost, rtRoutingBase+"/rules/move", map[string]any{"revision": e.rtRtRevision(), "id": rid2, "to": 0}); r.Status != http.StatusOK {
		t.Errorf("rules/move: %d %s", r.Status, r.Code)
	}
	if rules, _ := e.rtRtView()["rules"].([]any); len(rules) < 2 || rules[0].(map[string]any)["id"] != rid2 {
		t.Error("rules/move: второе правило не стало первым")
	}
	mv := e.call(http.MethodPost, rtRoutingBase+"/rules/move", map[string]any{"revision": e.rtRtRevision(), "id": rid, "to": 99})
	e.rtRtExpect("rules/move: вне диапазона", mv, http.StatusBadRequest, "invalid_input")
	if mv.Params["reason"] != "out_of_range" {
		t.Errorf("rules/move: вне диапазона: reason %q", mv.Params["reason"])
	}
	e.rtRtExpect("rules/save: без условий", e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule":     map[string]any{"enabled": true, "conds": []any{}, "target": map[string]string{"kind": "direct"}},
	}), http.StatusBadRequest, "invalid_input")
	e.rtRtExpect("rules/delete: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/rules/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff"}), http.StatusNotFound, "rule_not_found")
	e.rtRtExpect("rules/enable: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/rules/enable",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff", "enabled": true}), http.StatusNotFound, "rule_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/rules/delete", map[string]any{"revision": e.rtRtRevision(), "id": rid2}); r.Status != http.StatusOK {
		t.Errorf("rules/delete: %d %s", r.Status, r.Code)
	}

	// probe: параметры из представления сохраняются, недопустимые отклоняются.
	probe, _ := e.rtRtView()["probe"].(map[string]any)
	if r := e.call(http.MethodPost, rtRoutingBase+"/probe", map[string]any{"revision": e.rtRtRevision(), "probe": probe}); r.Status != http.StatusOK {
		t.Errorf("probe: %d %s", r.Status, r.Code)
	}
	badProbe := map[string]any{"url": "ftp://x", "interval_s": 30, "timeout_s": 5}
	e.rtRtExpect("probe: недопустимый адрес", e.call(http.MethodPost, rtRoutingBase+"/probe",
		map[string]any{"revision": e.rtRtRevision(), "probe": badProbe}), http.StatusBadRequest, "invalid_input")

	// impact и каскад удаления группы: на группу ссылаются правило и «Остальное».
	imp := e.call(http.MethodPost, rtRoutingBase+"/impact", map[string]string{"kind": "group", "id": gid})
	if imp.Status != http.StatusOK {
		t.Errorf("impact: %d %s", imp.Status, imp.Code)
	} else {
		rtAssertContractKeys(t, contract, imp.object(t), "Impact", "impact")
		if refs, _ := imp.object(t)["rules"].([]any); len(refs) != 1 {
			t.Errorf("impact: правил с целью на группу %d, ожидалось 1", len(refs))
		}
	}
	e.rtRtExpect("impact: неизвестный вид", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "bogus", "id": gid}), http.StatusBadRequest, "invalid_input")
	e.rtRtExpect("impact: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "rule", "id": "ffffffff"}), http.StatusNotFound, "rule_not_found")
	e.rtRtExpect("impact: неизвестный источник", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "source", "id": "ffffffff"}), http.StatusNotFound, "source_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/final", map[string]any{"revision": e.rtRtRevision(), "target": map[string]string{"kind": "group", "id": gid}}); r.Status != http.StatusOK {
		t.Fatalf("final: %d %s", r.Status, r.Code)
	}
	ref := e.call(http.MethodPost, rtRoutingBase+"/groups/delete", map[string]any{"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": ""}})
	e.rtRtExpect("groups/delete: есть ссылки", ref, http.StatusConflict, "group_referenced")
	if impact, _ := ref.object(t)["impact"].(map[string]any); impact == nil {
		t.Error("groups/delete: в ответе group_referenced нет impact")
	} else {
		rtAssertContractKeys(t, contract, impact, "Impact", "groups/delete.impact")
		if refs, _ := impact["rules"].([]any); len(refs) != 1 {
			t.Errorf("groups/delete: в impact правил %d, ожидалось 1", len(refs))
		}
	}
	e.rtRtExpect("groups/delete: «Остальное» без замены", e.call(http.MethodPost, rtRoutingBase+"/groups/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": "drop"}}), http.StatusBadRequest, "final_requires_redirect")
	e.rtRtExpect("groups/delete: группа не найдена", e.call(http.MethodPost, rtRoutingBase+"/groups/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff", "cascade": map[string]any{"mode": ""}}), http.StatusNotFound, "group_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/groups/delete", map[string]any{
		"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": "redirect", "to": map[string]string{"kind": "direct"}},
	}); r.Status != http.StatusOK {
		t.Fatalf("groups/delete с заменой: %d %s", r.Status, r.Code)
	}
	view = e.rtRtView()
	if rtRtFindGroup(view, "Проверка") != nil {
		t.Error("groups/delete: группа осталась в представлении")
	}
	if fin, _ := view["final"].(map[string]any); fin["kind"] != "direct" {
		t.Errorf("groups/delete: «Остальное» после замены: %v", fin["kind"])
	}
	if rules, _ := view["rules"].([]any); len(rules) != 1 || rules[0].(map[string]any)["target"].(map[string]any)["kind"] != "direct" {
		t.Error("groups/delete: правило не направлено на DIRECT")
	}

	// Предпросмотр взятия и reclaim с неизвестным ядром.
	tp := e.call(http.MethodGet, rtRoutingBase+"/takeover", nil)
	if tp.Status != http.StatusOK {
		t.Errorf("takeover: %d %s", tp.Status, tp.Code)
	} else {
		rtAssertContractKeys(t, contract, tp.object(t), "TakeoverPreview", "takeover")
		if m, _ := tp.object(t)[tg.Core].(map[string]any); m == nil {
			t.Errorf("takeover: для активного ядра %s нет предпросмотра", tg.Core)
		}
	}
	e.rtRtExpect("reclaim: неизвестное ядро", e.call(http.MethodPost, rtRoutingBase+"/reclaim",
		map[string]string{"kernel": "bogus"}), http.StatusBadRequest, "invalid_input")

	// take: желание управлять маршрутизацией в черновике, затем «Применить».
	tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()})
	if tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	if on, _ := e.rtRtView()["desired"].(bool); !on {
		t.Error("представление: desired=false после take")
	}
	taken = true
	e.rtRtApplyOK("взятие")
	if desired, got := e.rtRtOwnership(tg.Core); !desired || !got {
		t.Errorf("снимок слоя: желаемое=%v, взято %s=%v", desired, tg.Core, got)
	}
	if st := e.call(http.MethodGet, "/api/configlayer/state", nil); st.Status == http.StatusOK {
		snap, _ := st.object(t)["routing"].(map[string]any)
		rtAssertContractKeys(t, contract, snap, "RoutingSnapshot", "state.routing")
	}
	e.rtRtExpectTaken(tg, before, 1)
	// Взято и применено: расхождений нет, возвращать нечего.
	e.rtRtExpect("reclaim: нет расхождений", e.call(http.MethodPost, rtRoutingBase+"/reclaim",
		map[string]string{"kernel": tg.Core}), http.StatusConflict, "nothing_to_reclaim")

	// release → «Применить»: файлы ядра как были.
	rl := e.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": e.rtRtRevision()})
	if rl.Status != http.StatusOK {
		t.Fatalf("release: %d %s", rl.Status, rl.Code)
	}
	e.rtRtApplyOK("возврат")
	taken = false
	if _, got := e.rtRtOwnership(tg.Core); got {
		t.Errorf("снимок слоя: %s остаётся взятым после «Отпустить»", tg.Core)
	}
	e.rtRtExpectReleased(tg, before)
}

// --- выбор участника ----------------------------------------------------------

// rtRtXrayOutbounds читает исходящие всех файлов каталога конфигов Xray: тег → объект.
func rtRtXrayOutbounds(t *testing.T) map[string]map[string]any {
	t.Helper()
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	out := map[string]map[string]any{}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, ent.Name()))
		if err != nil {
			continue
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for _, ob := range doc.Outbounds {
			if tag, _ := ob["tag"].(string); tag != "" {
				out[tag] = ob
			}
		}
	}
	return out
}

// rtRtWithoutTag — исходящий без тега: сравнение копии узла с самим узлом.
func rtRtWithoutTag(ob map[string]any) map[string]any {
	cp := make(map[string]any, len(ob))
	for k, v := range ob {
		if k != "tag" {
			cp[k] = v
		}
	}
	return cp
}

// rtRtGroupOutbound — исходящий группы xcp-g-<id> без тега и признак «найден».
func rtRtGroupOutbound(t *testing.T, gid string) (map[string]any, bool) {
	t.Helper()
	ob, ok := rtRtXrayOutbounds(t)["xcp-g-"+gid]
	if !ok {
		return nil, false
	}
	return rtRtWithoutTag(ob), true
}

// rtRtMatchesNode сообщает, что исходящий (без тега) совпадает с исходящим одного
// из узлов «Моих узлов».
func rtRtMatchesNode(t *testing.T, ob map[string]any) bool {
	t.Helper()
	for tag, cand := range rtRtXrayOutbounds(t) {
		if strings.HasPrefix(tag, "xcp-my-") && reflect.DeepEqual(rtRtWithoutTag(cand), ob) {
			return true
		}
	}
	return false
}

// rtRtMihomoNow читает выбранного участника группы Mihomo через панель (Clash API).
func (e *rtSrc) rtRtMihomoNow(group string) string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/mihomo/proxy/proxies/"+url.PathEscape(group), nil)
	if r.Status != http.StatusOK {
		return ""
	}
	var body struct {
		Now string `json:"now"`
	}
	_ = json.Unmarshal(r.Raw, &body)
	return body.Now
}

// rtRtWaitNow ждёт, пока выбранный участник группы Mihomo станет want.
func (e *rtSrc) rtRtWaitNow(group, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		now := e.rtRtMihomoNow(group)
		if now == want || time.Now().After(deadline) {
			return now
		}
		time.Sleep(time.Second)
	}
}

// rtRtNodeOptions — варианты участника группы представления, которые являются
// узлами: список member и подпись каждого.
func rtRtNodeOptions(group map[string]any) (members []string, labels map[string]string) {
	labels = map[string]string{}
	opts, _ := group["member_options"].([]any)
	for _, o := range opts {
		m, _ := o.(map[string]any)
		member, _ := m["member"].(string)
		if !strings.HasPrefix(member, "node:") {
			continue
		}
		members = append(members, member)
		labels[member], _ = m["label"].(string)
	}
	return members, labels
}

// Выбор участника ручной группы: флаг → два узла → ручная группа → take → «Применить»
// → POST select → «Сейчас» в представлении, now группы в Mihomo или исходящий группы
// в Xray (с перезапуском запущенного Xray) → правка правила и «Применить» не
// сбрасывают выбор. Метка ядра: тест читает активное ядро и ставит выбор в нём.
func TestRouterRoutingSelectLive(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	e.rtRtPurge()
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	taken := false

	// Очистка при любом исходе: release → «Применить», затем узлы и секции. Выбор в
	// Live уходит вместе с группой при «Применить».
	t.Cleanup(func() {
		c := e.with(t)
		if taken {
			r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
			if r.Status != http.StatusOK {
				t.Errorf("очистка: release: %d %s", r.Status, r.Code)
				return
			}
			c.rtRtApplyOK("очистка: возврат")
		}
		c.rtRtPurge()
	})

	e.rtRstAddMyNode(rtRtUniqueLinkOf(t, "vless_ws", 1))
	e.rtRstAddMyNode(rtRtUniqueLinkOf(t, "trojan_ws", 2))

	// До взятия и «Применить» группа не применена: select отвечает group_not_applied.
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "Выбор", "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	gid, _ := save.object(t)["group"].(map[string]any)["id"].(string)
	if len(gid) != 8 {
		t.Fatalf("groups/save: идентификатор группы длиной %d, ожидалось 8", len(gid))
	}
	e.rtRtExpect("select: группа не применена", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": gid, "member": "direct"}), http.StatusConflict, "group_not_applied")

	if tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()}); tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	taken = true
	e.rtRtApplyOK("взятие")

	view := e.rtRtView()
	rtAssertContractKeys(t, contract, view, "RoutingView", "routing")
	group := rtRtFindGroup(view, "Выбор")
	if group == nil {
		t.Fatal("представление: нет группы «Выбор»")
	}
	nodeMembers, labels := rtRtNodeOptions(group)
	if len(nodeMembers) < 2 {
		t.Fatalf("представление: узлов среди вариантов группы %d, ожидалось не меньше 2", len(nodeMembers))
	}
	startMember, _ := group["current"].(string)
	if !strings.HasPrefix(startMember, "node:") {
		t.Fatalf("представление: current до выбора не узел (длина %d)", len(startMember))
	}
	if label, _ := group["current_label"].(string); label == "" || label != labels[startMember] {
		t.Errorf("представление: current_label до выбора не совпадает с подписью варианта")
	}
	target := ""
	for _, m := range nodeMembers {
		if m != startMember && labels[m] != labels[startMember] {
			target = m
			break
		}
	}
	if target == "" {
		t.Fatal("среди вариантов нет узла, отличного от текущего")
	}

	// Коды отказа выбора.
	e.rtRtExpect("select: неизвестный участник", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": gid, "member": "node:my/00000000"}), http.StatusBadRequest, "member_invalid")
	e.rtRtExpect("select: неизвестная группа", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": "ffffffff", "member": target}), http.StatusConflict, "group_not_applied")

	var xrayBefore map[string]any
	var pidsBefore []int
	if tg.Core == "xray" {
		var ok bool
		if xrayBefore, ok = rtRtGroupOutbound(t, gid); !ok {
			t.Fatal("Xray: в файлах нет исходящего группы после «Применить»")
		}
		pidsBefore, _ = rtRstKernelPIDs("xray")
	}

	// Выбор участника через API.
	sel := e.callSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": target})
	if sel.Status != http.StatusOK {
		t.Fatalf("select: %d %s", sel.Status, sel.Code)
	}
	rtAssertContractKeys(t, contract, sel.object(t), "SelectResult", "select")
	kernels, _ := sel.object(t)["kernels"].(map[string]any)
	outcome, _ := kernels[tg.Core].(string)
	if outcome != "applied" && outcome != "restarted" {
		t.Errorf("select: исход для активного ядра %q, ожидалось applied или restarted", outcome)
	}

	// «Сейчас» в представлении.
	group = rtRtFindGroup(e.rtRtView(), "Выбор")
	if group == nil || group["current"] != target {
		t.Errorf("представление после выбора: current не равен выбранному участнику")
	}
	if group != nil {
		if label, _ := group["current_label"].(string); label == "" || label != labels[target] {
			t.Errorf("представление после выбора: current_label не совпадает с подписью варианта")
		}
	}

	// Ядро: now группы в Mihomo, исходящий группы в Xray.
	switch tg.Core {
	case "mihomo":
		if now := e.rtRtWaitNow("Выбор", labels[target]); now != labels[target] {
			t.Errorf("Mihomo: now группы не равен имени выбранного узла (длина now %d)", len(now))
		}
	case "xray":
		after, ok := rtRtGroupOutbound(t, gid)
		switch {
		case !ok:
			t.Error("Xray: исходящий группы пропал после выбора")
		case reflect.DeepEqual(after, xrayBefore):
			t.Error("Xray: исходящий группы не сменился после выбора")
		case !rtRtMatchesNode(t, after):
			t.Error("Xray: исходящий группы не совпадает ни с одним узлом «Моих узлов»")
		}
		if len(pidsBefore) > 0 {
			pidsAfter, out := rtRstKernelPIDs("xray")
			if len(pidsAfter) != 1 || (len(pidsBefore) == 1 && pidsAfter[0] == pidsBefore[0]) {
				t.Errorf("Xray: после выбора PID %v (до %v): ядро не перезапущено: %s", pidsAfter, pidsBefore, out)
			}
		}
	}

	// Правка правила и «Применить»: выбор остаётся.
	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule": map[string]any{
			"enabled": true,
			"conds":   []map[string]any{{"type": "domain", "values": []string{"direct.example.test"}}},
			"target":  map[string]string{"kind": "direct"},
		},
	})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	e.rtRtApplyOK("после правки правила")
	group = rtRtFindGroup(e.rtRtView(), "Выбор")
	if group == nil || group["current"] != target {
		t.Errorf("после «Применить» current не равен выбранному участнику")
	}
	switch tg.Core {
	case "mihomo":
		if now := e.rtRtWaitNow("Выбор", labels[target]); now != labels[target] {
			t.Errorf("Mihomo: после «Применить» now группы не равен выбранному узлу (длина now %d)", len(now))
		}
	case "xray":
		again, ok := rtRtGroupOutbound(t, gid)
		if !ok || reflect.DeepEqual(again, xrayBefore) || !rtRtMatchesNode(t, again) {
			t.Error("Xray: после «Применить» исходящий группы не соответствует выбранному узлу")
		}
	}
}

// --- маршруты -----------------------------------------------------------------

var rtRoutingRoutes = []struct{ method, path string }{
	{http.MethodGet, rtRoutingBase},
	{http.MethodPost, rtRoutingBase + "/groups/save"},
	{http.MethodPost, rtRoutingBase + "/groups/preview"},
	{http.MethodPost, rtRoutingBase + "/groups/delete"},
	{http.MethodPost, rtRoutingBase + "/impact"},
	{http.MethodPost, rtRoutingBase + "/rules/save"},
	{http.MethodPost, rtRoutingBase + "/rules/delete"},
	{http.MethodPost, rtRoutingBase + "/rules/move"},
	{http.MethodPost, rtRoutingBase + "/rules/enable"},
	{http.MethodPost, rtRoutingBase + "/final"},
	{http.MethodPost, rtRoutingBase + "/probe"},
	{http.MethodPost, rtRoutingBase + "/take"},
	{http.MethodPost, rtRoutingBase + "/release"},
	{http.MethodGet, rtRoutingBase + "/takeover"},
	{http.MethodPost, rtRoutingBase + "/reclaim"},
	{http.MethodPost, rtRoutingBase + "/select"},
}

// Флаг слоя выключен: каждый маршрут маршрутизации отвечает 404 config_layer_disabled.
func TestRouterRoutingFlagOff404(t *testing.T) {
	e := rtNewSrc(t)
	e.setLayer(false)
	for _, rt := range rtRoutingRoutes {
		r := e.call(rt.method, rt.path, rtBody(rt.method))
		if r.Status != http.StatusNotFound || r.Code != "config_layer_disabled" {
			t.Errorf("%s %s: статус %d, код %q; ожидалось 404 config_layer_disabled", rt.method, rt.path, r.Status, r.Code)
		}
	}
}

// Без сессии маршруты отвечают 401, изменяющие без X-CSRF-Token — 403; метод не из
// контракта — 405. Ответы читаемых маршрутов совпадают с routingContract.json по ключам.
func TestRouterRoutingProtected(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	for _, rt := range rtRoutingRoutes {
		var body []byte
		if rt.method == http.MethodPost {
			body = []byte("{}")
		}
		status, _, err := e.p.Request(rt.method, rt.path, body, routertest.AuthNone)
		if err != nil {
			t.Fatalf("%s %s без сессии: %v", rt.method, rt.path, err)
		}
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s без сессии: статус %d, ожидался 401", rt.method, rt.path, status)
		}
		if rt.method != http.MethodPost {
			continue
		}
		status, _, err = e.p.Request(rt.method, rt.path, body, routertest.AuthNoCSRF)
		if err != nil {
			t.Fatalf("%s %s без CSRF: %v", rt.method, rt.path, err)
		}
		if status != http.StatusForbidden {
			t.Errorf("%s %s без X-CSRF-Token: статус %d, ожидался 403", rt.method, rt.path, status)
		}
	}
	for _, rt := range rtRoutingRoutes {
		other := http.MethodGet
		if rt.method == http.MethodGet {
			other = http.MethodPost
		}
		r := e.call(other, rt.path, rtBody(other))
		if r.Status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: статус %d, ожидался 405", other, rt.path, r.Status)
		}
	}

	// Ключи ответов читаемых маршрутов — по контракту.
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	view := e.call(http.MethodGet, rtRoutingBase, nil)
	if view.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rtRoutingBase, view.Status, view.Code)
	}
	rtAssertContractKeys(t, contract, view.object(t), "RoutingView", "routing")
	tp := e.call(http.MethodGet, rtRoutingBase+"/takeover", nil)
	if tp.Status != http.StatusOK {
		t.Fatalf("GET %s/takeover: %d %s", rtRoutingBase, tp.Status, tp.Code)
	}
	rtAssertContractKeys(t, contract, tp.object(t), "TakeoverPreview", "takeover")
}
