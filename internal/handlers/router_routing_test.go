//go:build router

package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
	link := rtLinkText(t, "vless_ws")
	re := regexp.MustCompile(`(@[^:/?#]+):\d+`)
	port := 20000 + int(time.Now().UnixNano()%30000)
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
	e.rtRtExpectTaken(tg, before, 1)

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
