//go:build router

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка API источников на настоящей панели устройства: маршруты
// /api/configlayer/sources* вызываются по HTTPS с сессией, созданной на ПК
// (routertest.Panel). Вердикт — коды и тела ответов панели, контракт ключей —
// frontend/src/lib/sourcesContract.json. Флаг слоя «Конфигурация» на время
// проверки переключается и возвращается прежним; черновик после сценариев
// сбрасывается. Подписка идёт из XCP_RT_SUBSCRIPTION_URL, полный адрес в
// сообщения не попадает.

const rtSourcesBase = "/api/configlayer/sources"

// rtSrcResp — разобранный ответ API.
type rtSrcResp struct {
	Status  int
	Raw     []byte
	Success bool              `json:"success"`
	Data    json.RawMessage   `json:"data"`
	Error   string            `json:"error"`
	Code    string            `json:"code"`
	Detail  string            `json:"detail"`
	Params  map[string]string `json:"params"`
}

// object разбирает data ответа в объект.
func (r rtSrcResp) object(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Data, &m); err != nil {
		t.Fatalf("data ответа %d не объект: %v", r.Status, err)
	}
	return m
}

type rtSrc struct {
	t *testing.T
	p *routertest.Panel
}

func rtNewSrc(t *testing.T) *rtSrc {
	t.Helper()
	e := &rtSrc{t: t, p: routertest.NewPanel(t)}
	// Флаг слоя возвращается прежним в любом случае.
	orig := e.layerFlag()
	t.Cleanup(func() { e.setLayer(orig) })
	return e
}

func (e *rtSrc) decode(status int, raw []byte) rtSrcResp {
	r := rtSrcResp{Status: status, Raw: raw}
	_ = json.Unmarshal(raw, &r)
	return r
}

func (e *rtSrc) call(method, path string, in any) rtSrcResp {
	e.t.Helper()
	status, raw, err := e.p.Call(method, path, in)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return e.decode(status, raw)
}

// callSlow — как call, но с увеличенным пределом ожидания ответа (запрос идёт
// вместе с перезапуском ядра на панели).
func (e *rtSrc) callSlow(method, path string, in any) rtSrcResp {
	e.t.Helper()
	status, raw, err := e.p.CallSlow(method, path, in)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return e.decode(status, raw)
}

func (e *rtSrc) layerFlag() bool {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/settings", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/settings: %d", r.Status)
	}
	flag, _ := r.object(e.t)["config_layer"].(bool)
	return flag
}

func (e *rtSrc) setLayer(on bool) {
	e.t.Helper()
	if e.layerFlag() == on {
		return
	}
	// Включение запускает фоновую сборку (flag_on): пока она идёт, выключение отвечает
	// 409 apply_busy (на arm64 сборка не успевает закончиться к очистке теста).
	deadline := time.Now().Add(90 * time.Second)
	for {
		// Выключение слоя с файлами перезапускает ядра внутри запроса.
		r := e.callSlow(http.MethodPost, "/api/settings/config-layer", map[string]bool{"enabled": on})
		if r.Status == http.StatusOK {
			return
		}
		busy := r.Status == http.StatusConflict && (r.Code == "apply_busy" || r.Code == "kernel_op_in_progress")
		if !busy || time.Now().After(deadline) {
			e.t.Fatalf("переключение слоя в %v: %d %s", on, r.Status, r.Code)
		}
		time.Sleep(time.Second)
	}
}

// revision читает ревизию черновика из представления источников.
func (e *rtSrc) revision() int64 {
	e.t.Helper()
	r := e.call(http.MethodGet, rtSourcesBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtSourcesBase, r.Status, r.Code)
	}
	rev, _ := r.object(e.t)["draft_revision"].(float64)
	return int64(rev)
}

// resetDraft сбрасывает черновик к применённому состоянию.
func (e *rtSrc) resetDraft() {
	e.t.Helper()
	rev := e.revision()
	r := e.call(http.MethodPost, "/api/configlayer/draft/reset", map[string]int64{"revision": rev})
	if r.Status != http.StatusOK {
		e.t.Fatalf("сброс черновика: %d %s", r.Status, r.Code)
	}
}

// enableLayer включает слой и приводит черновик к чистому состоянию; сброс
// повторяется при выходе из теста.
func (e *rtSrc) enableLayer() {
	e.t.Helper()
	e.setLayer(true)
	e.resetDraft()
	e.t.Cleanup(func() { e.resetDraft() })
}

func rtLinkText(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(routertest.LinksDir(t), name+".txt"))
	if err != nil {
		t.Fatalf("данные ссылки %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

// rtNoSecrets проверяет, что в теле нет ни одной из секретных строк. Сами строки
// в сообщение не попадают.
func rtNoSecrets(t *testing.T, label string, raw []byte, secrets ...string) {
	t.Helper()
	for i, s := range secrets {
		if s != "" && bytes.Contains(raw, []byte(s)) {
			t.Errorf("%s: в ответе есть секрет №%d", label, i+1)
		}
	}
}

// --- контракт ключей --------------------------------------------------------

type rtContractType struct {
	Required []string          `json:"required"`
	Optional []string          `json:"optional"`
	Children map[string]string `json:"children"`
}

func rtLoadContract(t *testing.T) map[string]rtContractType {
	t.Helper()
	return rtLoadContractFile(t, routertest.ContractFile(t))
}

// rtLoadContractFile читает файл контракта ключей по пути (источники и маршрутизация).
func rtLoadContractFile(t *testing.T, path string) map[string]rtContractType {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("контракт ответов: %v", err)
	}
	var c map[string]rtContractType
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("контракт ответов не JSON: %v", err)
	}
	return c
}

// rtAssertContractKeys обходит значение по контракту рекурсивно: у каждого объекта
// есть все обязательные ключи и нет ключей вне обязательных и необязательных.
// Нарушение называет путь и ключ.
func rtAssertContractKeys(t *testing.T, c map[string]rtContractType, value any, typeName, path string) {
	t.Helper()
	spec, ok := c[typeName]
	if !ok {
		t.Fatalf("%s: в контракте нет типа %s", path, typeName)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		t.Errorf("%s: ожидался объект типа %s", path, typeName)
		return
	}
	allowed := map[string]bool{}
	for _, k := range spec.Required {
		allowed[k] = true
		if _, present := obj[k]; !present {
			t.Errorf("%s: нет обязательного ключа %q", path, k)
		}
	}
	for _, k := range spec.Optional {
		allowed[k] = true
	}
	for k := range obj {
		if !allowed[k] {
			t.Errorf("%s: лишний ключ %q", path, k)
		}
	}
	for key, child := range spec.Children {
		val, present := obj[key]
		if !present || val == nil {
			continue
		}
		switch {
		case strings.HasSuffix(child, "[]"):
			list, ok := val.([]any)
			if !ok {
				t.Errorf("%s.%s: ожидался массив", path, key)
				continue
			}
			for i, item := range list {
				rtAssertContractKeys(t, c, item, strings.TrimSuffix(child, "[]"), path+"."+key+"["+strconv.Itoa(i)+"]")
			}
		case strings.HasSuffix(child, "{}"):
			m, ok := val.(map[string]any)
			if !ok {
				t.Errorf("%s.%s: ожидалась карта", path, key)
				continue
			}
			for k, item := range m {
				rtAssertContractKeys(t, c, item, strings.TrimSuffix(child, "{}"), path+"."+key+"."+k)
			}
		default:
			rtAssertContractKeys(t, c, val, child, path+"."+key)
		}
	}
}

// --- маршруты -----------------------------------------------------------------

var rtSourceRoutes = []struct{ method, path string }{
	{http.MethodGet, rtSourcesBase},
	{http.MethodPost, rtSourcesBase + "/preview"},
	{http.MethodPost, rtSourcesBase + "/add"},
	{http.MethodPost, rtSourcesBase + "/update"},
	{http.MethodPost, rtSourcesBase + "/delete"},
	{http.MethodPost, rtSourcesBase + "/refresh"},
	{http.MethodPost, rtSourcesBase + "/reveal"},
	{http.MethodGet, rtSourcesBase + "/diagnostics?id=0&kernel=xray"},
	{http.MethodPost, rtSourcesBase + "/nodes/preview"},
	{http.MethodPost, rtSourcesBase + "/nodes/raw"},
	{http.MethodPost, rtSourcesBase + "/nodes/save"},
	{http.MethodPost, rtSourcesBase + "/nodes/delete"},
}

func rtBody(method string) any {
	if method == http.MethodPost {
		return map[string]any{}
	}
	return nil
}

// Флаг слоя выключен: все двенадцать маршрутов отвечают 404 config_layer_disabled.
func TestRouterSourcesFlagOff404(t *testing.T) {
	e := rtNewSrc(t)
	e.setLayer(false)
	for _, rt := range rtSourceRoutes {
		r := e.call(rt.method, rt.path, rtBody(rt.method))
		if r.Status != http.StatusNotFound || r.Code != "config_layer_disabled" {
			t.Errorf("%s %s: статус %d, код %q; ожидалось 404 config_layer_disabled", rt.method, rt.path, r.Status, r.Code)
		}
	}
}

// Без сессии маршруты отвечают 401, изменяющие без X-CSRF-Token — 403; метод не
// из контракта — 405.
func TestRouterSourcesProtected(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	for _, rt := range rtSourceRoutes {
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
	for _, rt := range rtSourceRoutes {
		other := http.MethodGet
		if rt.method == http.MethodGet {
			other = http.MethodPost
		}
		r := e.call(other, rt.path, rtBody(other))
		if r.Status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: статус %d, ожидался 405", other, rt.path, r.Status)
		}
	}
}

// Трасса: вставка ссылки → предпросмотр → «Добавить» → представление; затем
// остальные маршруты «Моих узлов» и коды ошибок.
func TestRouterSourcesMyNodesFlow(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	contract := rtLoadContract(t)
	link := rtLinkText(t, "vless_ws")
	secret := "11111111-1111-4111-8111-111111111111"
	other := rtLinkText(t, "trojan_ws")

	// preview
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link})
	if pv.Status != http.StatusOK {
		t.Fatalf("preview: %d %s", pv.Status, pv.Code)
	}
	rtNoSecrets(t, "preview", pv.Raw, secret)
	pvObj := pv.object(t)
	rtAssertContractKeys(t, contract, pvObj, "PreviewView", "data")
	token, _ := pvObj["token"].(string)
	my, _ := pvObj["my_nodes"].(map[string]any)
	if token == "" || my == nil || my["count"] != float64(1) {
		t.Fatalf("preview: токен %q, my_nodes %v", token, my)
	}

	// add
	rev := e.revision()
	add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": rev, "token": token})
	if add.Status != http.StatusOK {
		t.Fatalf("add: %d %s", add.Status, add.Code)
	}
	rtNoSecrets(t, "add", add.Raw, secret)
	if got, _ := add.object(t)["draft_revision"].(float64); int64(got) <= rev {
		t.Errorf("add: ревизия %v не выросла с %d", got, rev)
	}
	if again := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); again.Status != http.StatusGone || again.Code != "preview_expired" {
		t.Errorf("повторный add: %d %q, ожидалось 410 preview_expired", again.Status, again.Code)
	}
	if bad := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": rev, "token": "0000"}); bad.Status != http.StatusGone || bad.Code != "preview_expired" {
		t.Errorf("add с неизвестным токеном: %d %q", bad.Status, bad.Code)
	}

	// представление
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	if view.Status != http.StatusOK {
		t.Fatalf("GET sources: %d", view.Status)
	}
	rtNoSecrets(t, "sources", view.Raw, secret)
	entryID := ""
	for _, s := range view.object(t)["sources"].([]any) {
		src := s.(map[string]any)
		if src["kind"] != "my" {
			continue
		}
		nodesList, _ := src["nodes"].([]any)
		if len(nodesList) != 1 {
			t.Fatalf("«Мои узлы»: узлов %d, ожидался 1", len(nodesList))
		}
		entryID, _ = nodesList[0].(map[string]any)["entry_id"].(string)
	}
	if entryID == "" {
		t.Fatal("в представлении нет узла «Моих узлов» с entry_id")
	}

	// nodes/raw отдаёт исходный текст целиком
	raw := e.call(http.MethodPost, rtSourcesBase+"/nodes/raw", map[string]string{"entry_id": entryID})
	if raw.Status != http.StatusOK || raw.object(t)["raw"] != link {
		t.Errorf("nodes/raw: статус %d, текст совпал: %v", raw.Status, raw.Status == http.StatusOK && raw.object(t)["raw"] == link)
	}
	if nf := e.call(http.MethodPost, rtSourcesBase+"/nodes/raw", map[string]string{"entry_id": "ffffffff"}); nf.Status != http.StatusNotFound || nf.Code != "node_not_found" {
		t.Errorf("nodes/raw неизвестной записи: %d %q", nf.Status, nf.Code)
	}

	// nodes/preview: тот же текст и запись; два узла — edit_single_node
	np := e.call(http.MethodPost, rtSourcesBase+"/nodes/preview", map[string]string{"text": link, "entry_id": entryID})
	if np.Status != http.StatusOK {
		t.Fatalf("nodes/preview: %d %s", np.Status, np.Code)
	}
	rtNoSecrets(t, "nodes/preview", np.Raw, secret)
	rtAssertContractKeys(t, contract, np.object(t), "PreviewView", "data")
	two := e.call(http.MethodPost, rtSourcesBase+"/nodes/preview", map[string]string{"text": link + "\n" + other})
	if two.Status != http.StatusBadRequest || two.Code != "edit_single_node" {
		t.Errorf("nodes/preview двух ссылок: %d %q", two.Status, two.Code)
	}
	// Нераспознанная строка: тот же отказ, а в data — предпросмотр с причиной по строке.
	broken := e.call(http.MethodPost, rtSourcesBase+"/nodes/preview", map[string]string{"text": "foo://broken"})
	if broken.Status != http.StatusBadRequest || broken.Code != "edit_single_node" {
		t.Errorf("nodes/preview нераспознанной строки: %d %q", broken.Status, broken.Code)
	} else {
		bv := broken.object(t)
		rtAssertContractKeys(t, contract, bv, "PreviewView", "data")
		if errs, _ := bv["errors"].([]any); len(errs) != 1 {
			t.Errorf("предпросмотр отказа без причины по строке: %v", bv["errors"])
		}
	}

	// nodes/save: устаревшая ревизия, две ссылки, успех
	if st := e.call(http.MethodPost, rtSourcesBase+"/nodes/save", map[string]any{"revision": 0, "entry_id": entryID, "name": "x", "text": link}); st.Status != http.StatusConflict || st.Code != "draft_conflict" {
		t.Errorf("nodes/save со старой ревизией: %d %q", st.Status, st.Code)
	}
	if sv := e.call(http.MethodPost, rtSourcesBase+"/nodes/save", map[string]any{"revision": e.revision(), "entry_id": entryID, "name": "x", "text": link + "\n" + other}); sv.Status != http.StatusBadRequest || sv.Code != "edit_single_node" {
		t.Errorf("nodes/save двух ссылок: %d %q", sv.Status, sv.Code)
	}
	ok := e.call(http.MethodPost, rtSourcesBase+"/nodes/save", map[string]any{"revision": e.revision(), "entry_id": entryID, "name": "Переименован", "text": link})
	if ok.Status != http.StatusOK {
		t.Fatalf("nodes/save: %d %s", ok.Status, ok.Code)
	}
	rtNoSecrets(t, "nodes/save", ok.Raw, secret)

	// update «Моих узлов»: выключить и включить; лишнее поле — 400
	for _, enabled := range []bool{false, true} {
		up := e.call(http.MethodPost, rtSourcesBase+"/update", map[string]any{"revision": e.revision(), "id": "my", "patch": map[string]any{"enabled": enabled}})
		if up.Status != http.StatusOK {
			t.Errorf("update enabled=%v: %d %s", enabled, up.Status, up.Code)
		}
	}
	if up := e.call(http.MethodPost, rtSourcesBase+"/update", map[string]any{"revision": e.revision(), "id": "my", "patch": map[string]any{"name": "x"}}); up.Status != http.StatusBadRequest || up.Code != "invalid_request" {
		t.Errorf("update имени «Моих узлов»: %d %q", up.Status, up.Code)
	}
	if up := e.call(http.MethodPost, rtSourcesBase+"/update", map[string]any{"revision": e.revision(), "id": "ffffffff", "patch": map[string]any{"enabled": false}}); up.Status != http.StatusNotFound || up.Code != "source_not_found" {
		t.Errorf("update неизвестного источника: %d %q", up.Status, up.Code)
	}

	// диагностика: неизвестный источник и «Мои узлы» (ответа провайдера нет)
	if d := e.call(http.MethodGet, rtSourcesBase+"/diagnostics?id=nope&kernel=xray", nil); d.Status != http.StatusNotFound || d.Code != "source_not_found" {
		t.Errorf("diagnostics неизвестного источника: %d %q", d.Status, d.Code)
	}
	if d := e.call(http.MethodGet, rtSourcesBase+"/diagnostics?id=my&kernel=xray", nil); d.Status != http.StatusBadRequest || d.Code != "invalid_request" {
		t.Errorf("diagnostics «Моих узлов»: %d %q", d.Status, d.Code)
	}

	// ввод больше 1 МиБ доходит до распознавателя и получает too_large, а не
	// безликий invalid_request; тело больше общего предела API (2 МиБ) отсекает
	// middleware.MaxBytes ответом 413 до обработчика
	big := []byte(`{"text":"` + strings.Repeat("a", (1<<20)+4096) + `"}`)
	status, bigRaw, err := e.p.Request(http.MethodPost, rtSourcesBase+"/preview", big, routertest.AuthFull)
	if err != nil {
		t.Fatalf("большой ввод: %v", err)
	}
	if br := e.decode(status, bigRaw); br.Status != http.StatusOK {
		t.Errorf("ввод больше 1 МиБ: %d %q", br.Status, br.Code)
	} else if errs, _ := br.object(t)["errors"].([]any); len(errs) != 1 ||
		!strings.Contains(string(br.Data), `"too_large"`) {
		t.Errorf("ввод больше 1 МиБ без too_large: %.200s", br.Data)
	}
	huge := []byte(`{"text":"` + strings.Repeat("a", (3<<20)) + `"}`)
	status, hugeRaw, err := e.p.Request(http.MethodPost, rtSourcesBase+"/preview", huge, routertest.AuthFull)
	if err != nil {
		t.Fatalf("большое тело: %v", err)
	}
	if hr := e.decode(status, hugeRaw); hr.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("тело больше общего предела API: %d %q", hr.Status, hr.Code)
	}

	// nodes/delete: запись удаляется, повторное удаление — 404
	del := e.call(http.MethodPost, rtSourcesBase+"/nodes/delete", map[string]any{"revision": e.revision(), "entry_id": entryID})
	if del.Status != http.StatusOK {
		t.Fatalf("nodes/delete: %d %s", del.Status, del.Code)
	}
	if again := e.call(http.MethodPost, rtSourcesBase+"/nodes/delete", map[string]any{"revision": e.revision(), "entry_id": entryID}); again.Status != http.StatusNotFound || again.Code != "node_not_found" {
		t.Errorf("повторное nodes/delete: %d %q", again.Status, again.Code)
	}
}

// Предпросмотр смешанного ввода: ссылка узла, адрес подписки из окружения и
// строка, которую панель не распознаёт. Ключи ответа сверяются с контрактом на
// каждом уровне. Подписка настоящая: без XCP_RT_SUBSCRIPTION_URL тест пропущен.
func TestRouterSourcesSubscriptionFlow(t *testing.T) {
	subURL := os.Getenv("XCP_RT_SUBSCRIPTION_URL")
	if subURL == "" {
		t.Skip("нет XCP_RT_SUBSCRIPTION_URL: настоящая подписка не задана")
	}
	e := rtNewSrc(t)
	e.enableLayer()
	contract := rtLoadContract(t)
	link := rtLinkText(t, "vless_ws")
	secrets := []string{subURL, url.QueryEscape(subURL)}
	if u, err := url.Parse(subURL); err == nil {
		secrets = append(secrets, strings.Trim(u.Path, "/"), u.RawQuery)
	}

	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link + "\n" + subURL + "\nfoo://x"})
	if pv.Status != http.StatusOK {
		t.Fatalf("preview: %d %s", pv.Status, pv.Code)
	}
	rtNoSecrets(t, "preview", pv.Raw, secrets...)
	pvObj := pv.object(t)
	rtAssertContractKeys(t, contract, pvObj, "PreviewView", "data")
	subs, _ := pvObj["subscriptions"].([]any)
	if len(subs) != 1 {
		t.Fatalf("preview: подписок %d, ожидалась 1", len(subs))
	}
	kernels, _ := subs[0].(map[string]any)["kernels"].(map[string]any)
	for _, k := range []string{"xray", "mihomo"} {
		if _, ok := kernels[k]; !ok {
			t.Errorf("preview: у подписки нет ядра %s", k)
		}
	}
	if errs, _ := pvObj["errors"].([]any); len(errs) == 0 {
		t.Error("preview: строка foo://x не дала ошибки")
	}
	token, _ := pvObj["token"].(string)

	add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token})
	if add.Status != http.StatusOK {
		t.Fatalf("add: %d %s", add.Status, add.Code)
	}
	rtNoSecrets(t, "add", add.Raw, secrets...)

	view := e.call(http.MethodGet, rtSourcesBase, nil)
	rtNoSecrets(t, "sources", view.Raw, secrets...)
	subID := ""
	for _, s := range view.object(t)["sources"].([]any) {
		src := s.(map[string]any)
		if src["kind"] != "my" {
			subID, _ = src["id"].(string)
		}
	}
	if subID == "" {
		t.Fatal("в представлении нет подписки")
	}

	// reveal отдаёт адрес целиком, и только он
	rv := e.call(http.MethodPost, rtSourcesBase+"/reveal", map[string]string{"id": subID})
	if rv.Status != http.StatusOK || rv.object(t)["url"] != subURL {
		t.Errorf("reveal: статус %d, адрес совпал: %v", rv.Status, rv.Status == http.StatusOK && rv.object(t)["url"] == subURL)
	}

	// диагностика и правка не раскрывают адрес
	for _, k := range []string{"xray", "mihomo"} {
		d := e.call(http.MethodGet, rtSourcesBase+"/diagnostics?id="+subID+"&kernel="+k, nil)
		if d.Status != http.StatusOK {
			t.Errorf("diagnostics %s: %d %s", k, d.Status, d.Code)
			continue
		}
		rtNoSecrets(t, "diagnostics "+k, d.Raw, secrets...)
	}
	up := e.call(http.MethodPost, rtSourcesBase+"/update", map[string]any{"revision": e.revision(), "id": subID, "patch": map[string]any{"name": "Проверка"}})
	if up.Status != http.StatusOK {
		t.Errorf("update: %d %s", up.Status, up.Code)
	}
	rtNoSecrets(t, "update", up.Raw, secrets...)

	// refresh: итог совпадает с контрактом; адрес не раскрыт
	rf := e.call(http.MethodPost, rtSourcesBase+"/refresh", map[string]string{"id": subID})
	if rf.Status != http.StatusOK {
		t.Fatalf("refresh: %d %s", rf.Status, rf.Code)
	}
	rtNoSecrets(t, "refresh", rf.Raw, secrets...)
	rtAssertContractKeys(t, contract, rf.object(t), "RefreshOutcome", "data")

	// delete подписки
	del := e.call(http.MethodPost, rtSourcesBase+"/delete", map[string]any{"revision": e.revision(), "id": subID})
	if del.Status != http.StatusOK {
		t.Errorf("delete: %d %s", del.Status, del.Code)
	}
	rtNoSecrets(t, "delete", del.Raw, secrets...)
}
