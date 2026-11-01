//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Регрессия G-147-1: повторное взятие до «Применить» не теряет значения секций
// панели (D-05: отпускание — переход внутри «Применить»; D-08: hosts, ntp и
// sub-rules лежат в «Прочих ключах»; MIHO-01). Путь тот, что в UAT: правка секции
// через API панели → «Применить» → «Отпустить» → «Управлять» без «Применить» →
// «Применить». Вердикт — файлы на диске устройства после настоящего конвейера
// «Применить» с проверкой ядром; ответы панели — вспомогательные. Стенд
// возвращается в t.Cleanup при любом исходе. В сообщения попадают только коды и
// имена файлов, в фикстурах — имена *.example.test и loopback.

// rtCoresRetakeHost — запись hosts, которую сценарий добавляет в «Прочие ключи».
const rtCoresRetakeHost = "xcp-retake.example.test"

// rtCoresRetakeHostAddr — адрес записи hosts (loopback).
const rtCoresRetakeHostAddr = "127.0.0.9"

// rtCoresRetakeLogLevels — значения loglevel, из которых берётся новое.
var rtCoresRetakeLogLevels = []string{"warning", "info", "error", "debug"}

// rtCoresRetakeOwner читает владельца секции из вида ядра; пусто — секции нет.
func (e *rtSrc) rtCoresRetakeOwner(kernel, id string) string {
	e.t.Helper()
	sec := rtCoresSection(e.rtCoresView(kernel), id)
	owner, _ := sec["owner"].(string)
	return owner
}

// rtCoresRetakeProfileOwner читает владельца профиля Mihomo из вида ядра.
func (e *rtSrc) rtCoresRetakeProfileOwner() string {
	e.t.Helper()
	prof, _ := e.rtCoresView("mihomo")["profile"].(map[string]any)
	owner, _ := prof["owner"].(string)
	return owner
}

// rtCoresRetakeHasHost сообщает, что в значении секции «Прочие ключи» есть запись
// hosts сценария.
func rtCoresRetakeHasHost(value any) bool {
	obj, _ := value.(map[string]any)
	hosts, _ := obj["hosts"].(map[string]any)
	_, ok := hosts[rtCoresRetakeHost]
	return ok
}

// rtCoresRetakeProfileTop читает действующий config.yaml Mihomo (профиль панели)
// как отображение верхнего уровня.
func rtCoresRetakeProfileTop(t *testing.T) map[string]any {
	t.Helper()
	now := rtRtScanDisk(t)
	if !strings.HasSuffix(now.mihomoTarget, rtRtPanelProfile) {
		t.Fatalf("config.yaml Mihomo не указывает на %s", rtRtPanelProfile)
	}
	data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml Mihomo не читается: %v", err)
	}
	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		t.Fatalf("профиль панели не YAML: %v", err)
	}
	return top
}

// rtCoresRetakeExpectKeys требует в профиле панели на диске hosts с записью
// сценария, ntp и sub-rules; step — этап для сообщения.
func rtCoresRetakeExpectKeys(t *testing.T, step string) {
	t.Helper()
	top := rtCoresRetakeProfileTop(t)
	hosts, _ := top["hosts"].(map[string]any)
	if _, ok := hosts[rtCoresRetakeHost]; !ok {
		t.Errorf("%s: в xcp-main.yaml нет hosts с записью сценария", step)
	}
	for _, k := range []string{"ntp", "sub-rules"} {
		if top[k] == nil {
			t.Errorf("%s: в xcp-main.yaml нет %s", step, k)
		}
	}
}

// rtCoresRetakeMihomoKeys возвращает «Дополнительно» секции «Прочие ключи» как
// отображение, добавив hosts с записью сценария, ntp и sub-rules, если их нет.
func rtCoresRetakeMihomoKeys(t *testing.T, advanced string) string {
	t.Helper()
	keys := map[string]any{}
	if strings.TrimSpace(advanced) != "" {
		if err := yaml.Unmarshal([]byte(advanced), &keys); err != nil {
			t.Fatalf("«Дополнительно» секции other не YAML: %v", err)
		}
		if keys == nil {
			keys = map[string]any{}
		}
	}
	hosts, _ := keys["hosts"].(map[string]any)
	if hosts == nil {
		hosts = map[string]any{}
	}
	hosts[rtCoresRetakeHost] = rtCoresRetakeHostAddr
	keys["hosts"] = hosts
	if keys["ntp"] == nil {
		keys["ntp"] = map[string]any{"enable": false, "server": "ntp.example.test"}
	}
	if keys["sub-rules"] == nil {
		keys["sub-rules"] = map[string]any{"XCP-RT-SUB": []any{"MATCH,DIRECT"}}
	}
	out, err := yaml.Marshal(keys)
	if err != nil {
		t.Fatalf("«Дополнительно» не собирается в YAML: %v", err)
	}
	return string(out)
}

// rtCoresRetakeMihomo — ветка Mihomo: маршрутизация взята и применена, правка
// «Прочих ключей» применена, затем отпустить и снова взять до «Применить».
func rtCoresRetakeMihomo(t *testing.T, e *rtSrc, tg routertest.Target) {
	t.Helper()
	m := rtCoresLiveMihomoTake(t, e, tg)
	// Выполняется до очистки помощника взятия: если сценарий оборвался в состоянии
	// «отпущена до „Применить“», маршрутизацию берут обратно, и очистка помощника
	// отпускает её обычным путём.
	t.Cleanup(func() {
		c := e.with(t)
		c.rtRstIdle()
		if desired, taken := c.rtRtOwnership("mihomo"); m.taken && taken && !desired {
			c.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": c.rtRtRevision()})
		}
	})

	sec := rtCoresSection(e.rtCoresView("mihomo"), "other")
	if sec == nil {
		t.Fatal("в виде Mihomo нет секции other")
	}
	if owner, _ := sec["owner"].(string); owner != "panel" {
		t.Fatalf("секция other после взятия маршрутизации: владелец %q, ожидался panel", owner)
	}
	advanced, _ := sec["advanced"].(string)
	r := e.rtCoresPost("/save", map[string]any{
		"kernel": "mihomo", "section": "other", "form": map[string]any{},
		"advanced": rtCoresRetakeMihomoKeys(t, advanced),
	})
	if r.Status != http.StatusOK {
		t.Fatalf("save other (mihomo): %d %s", r.Status, r.Code)
	}
	e.rtRtApplyOK("правка «Прочих ключей» Mihomo")
	rtCoresRetakeExpectKeys(t, "после «Применить» правки")

	// Отпустить без «Применить»: профиль ещё у панели, значения — из применённого.
	rel := e.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": e.rtRtRevision()})
	if rel.Status != http.StatusOK {
		t.Fatalf("routing/release: %d %s", rel.Status, rel.Code)
	}
	if owner := e.rtCoresRetakeProfileOwner(); owner != "releasing" {
		t.Errorf("профиль Mihomo после release до «Применить»: владелец %q, ожидался releasing", owner)
	}
	if !rtCoresRetakeHasHost(rtCoresSection(e.rtCoresView("mihomo"), "other")["value"]) {
		t.Error("в releasing значение «Прочих ключей» без записи hosts применённого состояния")
	}

	// Снова взять до «Применить».
	take := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()})
	if take.Status != http.StatusOK {
		t.Fatalf("routing/take после release: %d %s", take.Status, take.Code)
	}
	if owner := e.rtCoresRetakeProfileOwner(); owner != "panel" {
		t.Errorf("профиль Mihomo после повторного взятия: владелец %q, ожидался panel", owner)
	}
	if !rtCoresRetakeHasHost(rtCoresSection(e.rtCoresView("mihomo"), "other")["value"]) {
		t.Error("после повторного взятия в «Прочих ключах» нет записи hosts")
	}

	e.rtRtApplyOK("повторное взятие маршрутизации Mihomo")
	rtCoresRetakeExpectKeys(t, "release → take → «Применить»")
}

// rtCoresRetakeXray — состояние ветки Xray: секция log и что вернуть при выходе.
type rtCoresRetakeXray struct {
	e     *rtSrc
	files []string          // файлы секции до взятия
	left  []string          // из них остающиеся на месте (с другими секциями)
	sums  map[string]string // sha256 файлов секции до взятия
	taken bool
	panel string // файл панели секции
}

// rtCoresRetakeLogLevel читает loglevel из файла панели секции log на диске.
func (x *rtCoresRetakeXray) rtCoresRetakeLogLevel(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtXrayDir, x.panel))
	if err != nil {
		t.Fatalf("на устройстве нет %s: %v", x.panel, err)
	}
	var doc struct {
		Log struct {
			Level string `json:"loglevel"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s не JSON: %v", x.panel, err)
	}
	return doc.Log.Level
}

// rtCoresRetakeCleanup возвращает стенд: отпускает секцию log, если она взята,
// применяет и сверяет исходные файлы по sha256.
func (x *rtCoresRetakeXray) rtCoresRetakeCleanup(t *testing.T) {
	c := x.e.with(t)
	c.rtRstIdle()
	if !x.taken {
		return
	}
	// Секция уже отпущена до «Применить» (сценарий оборвался): остаётся применить.
	if c.rtCoresRetakeOwner("xray", "log") != "releasing" {
		r := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "log"})
		if r.Status != http.StatusOK {
			t.Errorf("очистка: release log: %d %s", r.Status, r.Code)
			return
		}
	}
	c.rtRtApplyOK("возврат секции log Xray")
	x.taken = false
	for name, sum := range x.sums {
		if got := rtCoresFileSum(name); got != sum {
			t.Errorf("после «Отпустить» файл секции log (длина имени %d) вернулся с другим содержимым или не вернулся", len(name))
		}
	}
	if rtCoresFileSum(x.panel) != "" {
		t.Errorf("после «Отпустить» на устройстве остался %s", x.panel)
	}
}

// rtCoresRetakeXrayRun — ветка Xray: секция log взята, loglevel изменён и
// применён, затем отпустить и снова взять до «Применить».
func rtCoresRetakeXrayRun(t *testing.T, e *rtSrc) {
	t.Helper()
	view := e.rtCoresView("xray")
	if installed, _ := view["installed"].(bool); !installed {
		t.Fatal("Xray не установлен на стенде: ветка xray не проверяется")
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
	x := &rtCoresRetakeXray{
		e:     e,
		files: rtCoresStrings(sec["files"]),
		left:  rtCoresStrings(sec["left"]),
		sums:  map[string]string{},
		panel: configlayer.PanelXraySectionFile("log"),
	}
	// Стенд возвращается при любом исходе, в том числе при провале самого взятия.
	t.Cleanup(func() { x.rtCoresRetakeCleanup(t) })
	for _, name := range x.files {
		x.sums[name] = rtCoresFileSum(name)
		if x.sums[name] == "" {
			t.Fatalf("файл секции log (длина имени %d) не читается", len(name))
		}
	}

	pv := e.call(http.MethodGet, rtCoresBase+"/takeover?kernel=xray&sections=log", nil)
	if pv.Status != http.StatusOK {
		t.Fatalf("GET takeover: %d %s", pv.Status, pv.Code)
	}
	ack := false
	if list, _ := pv.object(t)["sections"].([]any); len(list) == 1 {
		m, _ := list[0].(map[string]any)
		comments, _ := m["comments"].([]any)
		ack = len(comments) > 0
	}
	take := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"log"}, "ack_comments": ack})
	if take.Status != http.StatusOK {
		t.Fatalf("take log: %d %s", take.Status, take.Code)
	}
	x.taken = true
	e.rtRtApplyOK("взятие секции log Xray")

	// Новый loglevel — первый из набора, не равный текущему.
	sec = rtCoresSection(e.rtCoresView("xray"), "log")
	form, _ := sec["form"].(map[string]any)
	if form == nil {
		form = map[string]any{}
	}
	cur, _ := form["loglevel"].(string)
	next := ""
	for _, lvl := range rtCoresRetakeLogLevels {
		if lvl != cur {
			next = lvl
			break
		}
	}
	form["loglevel"] = next
	advanced, _ := sec["advanced"].(string)
	sv := e.rtCoresPost("/save", map[string]any{"kernel": "xray", "section": "log", "form": form, "advanced": advanced})
	if sv.Status != http.StatusOK {
		t.Fatalf("save log: %d %s", sv.Status, sv.Code)
	}
	e.rtRtApplyOK("правка loglevel секции log Xray")
	if got := x.rtCoresRetakeLogLevel(t); got != next {
		t.Fatalf("после «Применить» правки loglevel в %s %q, ожидалось %q", x.panel, got, next)
	}

	// Отпустить без «Применить».
	rel := e.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "log"})
	if rel.Status != http.StatusOK {
		t.Fatalf("release log: %d %s", rel.Status, rel.Code)
	}
	if owner := e.rtCoresRetakeOwner("xray", "log"); owner != "releasing" {
		t.Errorf("секция log после release до «Применить»: владелец %q, ожидался releasing", owner)
	}

	// Снова взять до «Применить».
	back := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"log"}, "ack_comments": false})
	if back.Status != http.StatusOK {
		t.Fatalf("take log после release: %d %s", back.Status, back.Code)
	}
	sec = rtCoresSection(e.rtCoresView("xray"), "log")
	if owner, _ := sec["owner"].(string); owner != "panel" {
		t.Errorf("секция log после повторного взятия: владелец %q, ожидался panel", owner)
	}
	if form, _ := sec["form"].(map[string]any); form["loglevel"] != next {
		t.Errorf("release → take → «Применить»: после повторного взятия в форме loglevel %v, ожидалось %q", form["loglevel"], next)
	}

	e.rtRtApplyOK("повторное взятие секции log Xray")
	if got := x.rtCoresRetakeLogLevel(t); got != next {
		t.Errorf("release → take → «Применить»: в %s loglevel %q, ожидалось %q", x.panel, got, next)
	}
	// Исходные файлы секции не вернулись, кроме остающихся на месте с другими секциями.
	for _, name := range x.files {
		if slices.Contains(x.left, name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(rtRtXrayDir, name)); err == nil {
			t.Errorf("release → take → «Применить»: исходный файл секции log (длина имени %d) вернулся в configs/", len(name))
		}
	}
}

// Трасса: маршрутизация Mihomo взята и применена → в «Прочие ключи» записаны
// hosts, ntp и sub-rules → «Применить» → ключи в профиле панели на диске →
// «Отпустить» без «Применить» (releasing, значения из применённого состояния) →
// «Управлять» (panel) → «Применить» → ключи по-прежнему в профиле панели. Ветка
// Xray: секция log взята, loglevel изменён и применён → отпустить (releasing) →
// взять → «Применить» → файл панели хранит изменённый loglevel, исходные файлы не
// вернулись. Метка ядра: ветка выбирается по активному ядру цели.
func TestRouterCoresRetakeBeforeApply(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	switch tg.Core {
	case "xray":
		rtCoresRetakeXrayRun(t, e)
	case "mihomo":
		rtCoresRetakeMihomo(t, e, tg)
	default:
		t.Fatalf("неизвестное ядро цели %q", tg.Core)
	}
}
