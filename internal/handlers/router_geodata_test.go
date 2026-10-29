//go:build router

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/geodata"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Проверка API менеджера geodata на настоящей панели устройства: маршруты
// /api/configlayer/geodata* вызываются по HTTPS с сессией, созданной на ПК
// (routertest.Panel). Вердикт — коды и тела ответов панели. Флаг слоя
// «Конфигурация» на время проверки переключается и возвращается прежним, черновик
// после сценария сбрасывается. В сообщения попадают только коды и числа.

const rtGeoBase = "/api/configlayer/geodata"

// rtGeoAPISkipWindow пропускает тест, если отрезок от сейчас на span по часам
// роутера задевает окно 05:20–05:45 (снимок стенда и cron XKeen).
func rtGeoAPISkipWindow(t *testing.T, span time.Duration) {
	t.Helper()
	now := time.Now()
	end := now.Add(span)
	for off := 0; off <= 1; off++ {
		from := time.Date(now.Year(), now.Month(), now.Day()+off, 5, 20, 0, 0, now.Location())
		to := from.Add(25 * time.Minute)
		if now.Before(to) && end.After(from) {
			t.Skip("часы стенда: отрезок теста задевает окно 05:20-05:45 (cron XKeen и снимок)")
		}
	}
}

// rtGeoJSONKeys возвращает ключи json-тегов структуры: обязательные (без omitempty) и
// необязательные. Вложенные безымянные структуры раскрываются.
func rtGeoJSONKeys(typ reflect.Type) (required, optional map[string]reflect.Type) {
	required, optional = map[string]reflect.Type{}, map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			req, opt := rtGeoJSONKeys(f.Type)
			for k, v := range req {
				required[k] = v
			}
			for k, v := range opt {
				optional[k] = v
			}
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		omit := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omit = true
			}
		}
		if omit {
			optional[name] = f.Type
		} else {
			required[name] = f.Type
		}
	}
	return required, optional
}

// rtGeoAssertKeys сверяет ключи ответа с json-тегами типа рекурсивно: обязательные поля
// (без omitempty) обязаны быть в ответе, лишних ключей нет.
func rtGeoAssertKeys(t *testing.T, typ reflect.Type, value any, path string) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		obj, ok := value.(map[string]any)
		if !ok {
			t.Errorf("%s: ожидался объект", path)
			return
		}
		required, optional := rtGeoJSONKeys(typ)
		for k := range required {
			if _, present := obj[k]; !present {
				t.Errorf("%s: нет обязательного ключа %q", path, k)
			}
		}
		for k, v := range obj {
			ft, isReq := required[k]
			if !isReq {
				var isOpt bool
				ft, isOpt = optional[k]
				if !isOpt {
					t.Errorf("%s: лишний ключ %q", path, k)
					continue
				}
			}
			if v != nil {
				rtGeoAssertKeys(t, ft, v, path+"."+k)
			}
		}
	case reflect.Slice:
		list, ok := value.([]any)
		if !ok {
			t.Errorf("%s: ожидался массив", path)
			return
		}
		if typ.Elem().Kind() == reflect.Struct || typ.Elem().Kind() == reflect.Pointer {
			for _, item := range list {
				rtGeoAssertKeys(t, typ.Elem(), item, path+"[]")
			}
		}
	}
}

// Адреса проверки источника — настоящие публичные: панель не ходит по адресам частных
// сетей и loopback (защита от SSRF), поэтому сервер на самом роутере для проверки не годится.
const (
	// rtGeoOKURL — годный dat с размером (тот же файл скачивает стандартный набор XKeen).
	rtGeoOKURL = "https://github.com/jameszeroX/zkeen-domains/releases/latest/download/zkeen.dat"
	// rtGeoPageURL — HTML-страница вместо файла: без Content-Length, запись блокирует size_none
	// (проверка html на loopback покрыта тестами пакета geodata).
	rtGeoPageURL = "https://github.com/jameszeroX/zkeen-domains"
)

const (
	// rtGeoAPIConfig — настоящий xkeen.json стенда.
	rtGeoAPIConfig = "/opt/etc/xkeen/xkeen.json"
	// rtGeoOwn — имя тестового источника.
	rtGeoOwn = "xcp-api-test.dat"
)

// rtGeoAPIKeepConfig запоминает xkeen.json стенда и возвращает его в t.Cleanup;
// файл с тестовым именем из каталога dat убирается там же.
func rtGeoAPIKeepConfig(t *testing.T) {
	t.Helper()
	orig, err := os.ReadFile(rtGeoAPIConfig)
	if err != nil {
		t.Skipf("xkeen.json стенда не читается: %v", err)
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(rtGeoAPIConfig); err == nil {
		mode = st.Mode().Perm()
	}
	t.Cleanup(func() {
		if err := os.WriteFile(rtGeoAPIConfig, orig, mode); err != nil {
			t.Errorf("возврат xkeen.json: %v", err)
		}
		_ = os.Remove(filepath.Join(lists.DefaultDatDir, rtGeoOwn))
	})
}

// rtGeoSnapshot читает снимок менеджера.
func (e *rtSrc) rtGeoSnapshot() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtGeoBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtGeoBase, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtGeoSource ищет строку источника снимка по имени файла.
func rtGeoSource(snap map[string]any, file string) map[string]any {
	rows, _ := snap["sources"].([]any)
	for _, row := range rows {
		if m, _ := row.(map[string]any); m != nil && m["file"] == file {
			return m
		}
	}
	return nil
}

// rtGeoFlag читает вложенный булев признак снимка (например xkeen.installed).
func rtGeoFlag(snap map[string]any, group, key string) bool {
	g, _ := snap[group].(map[string]any)
	v, _ := g[key].(bool)
	return v
}

// rtGeoData разбирает data ответа в объект (для ошибок с данными).
func rtGeoData(t *testing.T, r rtSrcResp) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Data, &m); err != nil {
		t.Fatalf("data ответа %d %s не объект: %v", r.Status, r.Code, err)
	}
	return m
}

// rtGeoOutputRun читает вывод задания со смещения from и номер запуска (run_id): сменился — начался новый
// запуск, вывод читается с начала, смещения прежнего запуска не сравниваются.
func (e *rtSrc) rtGeoOutputRun(job string, from int64) (text string, next int64, running bool, runID float64) {
	e.t.Helper()
	path := rtGeoBase + "/output?job=" + job + "&from=" + strconv.FormatInt(from, 10)
	r := e.call(http.MethodGet, path, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", path, r.Status, r.Code)
	}
	o := r.object(e.t)
	text, _ = o["text"].(string)
	n, _ := o["next"].(float64)
	running, _ = o["running"].(bool)
	runID, _ = o["run_id"].(float64)
	return text, int64(n), running, runID
}

// rtGeoRoutes — все пятнадцать маршрутов менеджера.
var rtGeoRoutes = []struct{ method, path string }{
	{http.MethodGet, rtGeoBase},
	{http.MethodGet, rtGeoBase + "/output?job=ug&from=0"},
	{http.MethodPost, rtGeoBase + "/run"},
	{http.MethodPost, rtGeoBase + "/jq"},
	{http.MethodPost, rtGeoBase + "/schedule"},
	{http.MethodPost, rtGeoBase + "/cron"},
	{http.MethodPost, rtGeoBase + "/probe"},
	{http.MethodPost, rtGeoBase + "/preview"},
	{http.MethodPost, rtGeoBase + "/write"},
	{http.MethodGet, rtGeoBase + "/impact?file=" + rtGeoOwn},
	{http.MethodPost, rtGeoBase + "/drop"},
	{http.MethodPost, rtGeoBase + "/restore"},
	{http.MethodGet, rtGeoBase + "/tags?file=" + rtGeoOwn},
	{http.MethodGet, rtGeoBase + "/entries?file=" + rtGeoOwn + "&tag=X"},
	{http.MethodGet, rtGeoBase + "/lookup?q=example.com"},
}

// TestRouterGeodataAPI — API менеджера geodata на настоящей панели: снимок по контракту,
// проверка источника, запись, расписание, влияние, пометка и возврат, теги, записи,
// поиск, защита маршрутов и настоящий запуск -ug с занятостью замка и выводом по смещениям.
func TestRouterGeodataAPI(t *testing.T) {
	e := rtNewSrc(t)

	// Флаг слоя выключен: каждый маршрут менеджера отвечает 404 config_layer_disabled.
	t.Run("flag_off", func(t *testing.T) {
		e := &rtSrc{t: t, p: e.p}
		e.setLayer(false)
		for _, rt := range rtGeoRoutes {
			r := e.call(rt.method, rt.path, rtBody(rt.method))
			if r.Status != http.StatusNotFound || r.Code != "config_layer_disabled" {
				t.Errorf("%s %s при выключенном слое: статус %d, код %q; ожидалось 404 config_layer_disabled", rt.method, rt.path, r.Status, r.Code)
			}
		}
	})

	e.enableLayer()
	// Запись в xkeen.json и «Обновить сейчас» идут под исключительным замком слоя:
	// пока фоновая сборка после включения флага не закончилась, они отвечают apply_busy.
	e.rtRstIdle()

	// Снимок вкладки: ключи ответа совпадают с json-тегами geodata.View.
	t.Run("snapshot", func(t *testing.T) {
		r := e.call(http.MethodGet, rtGeoBase, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", rtGeoBase, r.Status, r.Code)
		}
		obj := r.object(t)
		rtGeoAssertKeys(t, reflect.TypeOf(geodata.View{}), obj, "geodata")
		standard := 0
		rows, _ := obj["sources"].([]any)
		for _, row := range rows {
			if m, _ := row.(map[string]any); m != nil && m["origin"] == "standard" {
				standard++
			}
		}
		if standard != 6 {
			t.Errorf("стандартных строк в sources: %d, ожидалось 6", standard)
		}
	})

	// Защита: без сессии 401, изменяющие без X-CSRF-Token 403, чужой метод 405.
	t.Run("protected", func(t *testing.T) {
		for _, rt := range rtGeoRoutes {
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
		for _, rt := range rtGeoRoutes {
			other := http.MethodGet
			if rt.method == http.MethodGet {
				other = http.MethodPost
			}
			r := e.call(other, rt.path, rtBody(other))
			if r.Status != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: статус %d, ожидался 405", other, rt.path, r.Status)
			}
		}
	})

	// Недопустимые параметры: имя файла и смещения отклоняются до сервиса.
	t.Run("params", func(t *testing.T) {
		for _, path := range []string{
			rtGeoBase + "/impact?file=../x.dat",
			rtGeoBase + "/impact?file=x.txt",
			rtGeoBase + "/tags?file=a/b.dat",
			rtGeoBase + "/entries?file=",
		} {
			r := e.call(http.MethodGet, path, nil)
			if r.Status != http.StatusBadRequest || r.Code != "file_invalid" {
				t.Errorf("GET %s: статус %d, код %q; ожидалось 400 file_invalid", path, r.Status, r.Code)
			}
		}
		for _, path := range []string{
			rtGeoBase + "/output?job=other&from=0",
			rtGeoBase + "/output?job=ug&from=-1",
			rtGeoBase + "/output?job=ug&from=x",
		} {
			r := e.call(http.MethodGet, path, nil)
			if r.Status != http.StatusBadRequest || r.Code != "invalid_input" {
				t.Errorf("GET %s: статус %d, код %q; ожидалось 400 invalid_input", path, r.Status, r.Code)
			}
		}
		r := e.call(http.MethodPost, rtGeoBase+"/drop", map[string]any{"rev": 0, "file": "../x.dat", "mode": "delete", "actions": []any{}})
		if r.Status != http.StatusBadRequest || r.Code != "file_invalid" {
			t.Errorf("drop с недопустимым именем: статус %d, код %q; ожидалось 400 file_invalid", r.Status, r.Code)
		}
	})

	// Расписание с недопустимым часом: 400 schedule_invalid, снимок не меняется.
	t.Run("schedule_invalid", func(t *testing.T) {
		r := e.call(http.MethodPost, rtGeoBase+"/schedule", map[string]any{"enabled": true, "day": -1, "hour": 24, "minute": 0})
		if r.Status != http.StatusBadRequest || r.Code != "schedule_invalid" {
			t.Errorf("schedule hour=24: статус %d, код %q; ожидалось 400 schedule_invalid", r.Status, r.Code)
		}
	})

	// Поиск: пустой запрос — 400 lookup_query; домен из каталога находит теги.
	t.Run("lookup", func(t *testing.T) {
		r := e.call(http.MethodGet, rtGeoBase+"/lookup?q=", nil)
		if r.Status != http.StatusBadRequest || r.Code != "lookup_query" {
			t.Errorf("lookup с пустым q: статус %d, код %q; ожидалось 400 lookup_query", r.Status, r.Code)
		}
		r = e.call(http.MethodGet, rtGeoBase+"/lookup?q=example.com", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("lookup example.com: %d %s", r.Status, r.Code)
		}
		view := r.object(t)
		matches, _ := view["matches"].([]any)
		more, _ := view["more"].(float64)
		if len(matches) == 0 && more == 0 {
			t.Skip("в dat стенда нет домена example.com: поиск не проверить на непустом ответе")
		}
	})

	// Инспектор: теги первого файла снимка с записями и окно записей первого тега.
	t.Run("inspector", func(t *testing.T) {
		snap := e.rtGeoSnapshot()
		rows, _ := snap["sources"].([]any)
		file := ""
		for _, row := range rows {
			m, _ := row.(map[string]any)
			if m == nil {
				continue
			}
			present, _ := m["present"].(bool)
			tags, _ := m["tags"].(float64)
			name, _ := m["file"].(string)
			if present && tags > 0 && name != "" {
				file = name
				break
			}
		}
		if file == "" {
			t.Skip("в снимке нет файла с тегами")
		}
		r := e.call(http.MethodGet, rtGeoBase+"/tags?file="+url.QueryEscape(file), nil)
		if r.Status != http.StatusOK {
			t.Fatalf("tags %s: %d %s", file, r.Status, r.Code)
		}
		view := r.object(t)
		if view["file"] != file {
			t.Errorf("tags: file %v, ожидалось запрошенное имя", view["file"])
		}
		if k := view["kind"]; k != "site" && k != "ip" && k != "geosite" && k != "geoip" {
			t.Errorf("tags: вид файла %v не из набора вида dat", k)
		}
		tags, _ := view["tags"].([]any)
		if len(tags) == 0 {
			t.Fatalf("tags %s: пустой список тегов при tags>0 в снимке", file)
		}
		first, _ := tags[0].(map[string]any)
		tag, _ := first["tag"].(string)
		r = e.call(http.MethodGet, rtGeoBase+"/entries?file="+url.QueryEscape(file)+"&tag="+url.QueryEscape(tag)+"&offset=0&limit=5", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("entries %s: %d %s", file, r.Status, r.Code)
		}
		page := r.object(t)
		entries, _ := page["entries"].([]any)
		if len(entries) > 5 {
			t.Errorf("entries с limit=5: %d записей", len(entries))
		}
		r = e.call(http.MethodGet, rtGeoBase+"/entries?file="+url.QueryEscape(file)+"&tag="+url.QueryEscape(tag)+"&limit=1000", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("entries limit=1000: %d %s", r.Status, r.Code)
		}
		if n := len(r.object(t)["entries"].([]any)); n > 100 {
			t.Errorf("entries с limit=1000: %d записей, предел 100", n)
		}
	})

	snap := e.rtGeoSnapshot()
	if !rtGeoFlag(snap, "xkeen", "installed") || !rtGeoFlag(snap, "xkeen", "user_geodata") {
		t.Log("XKeen не установлен или без пользовательских баз: запись и запуск не проверяются")
		return
	}
	hasJQ := rtGeoFlag(snap, "download", "jq")

	rtGeoAPIKeepConfig(t)

	// Проверка источника на настоящих публичных адресах: HTML заблокирован, dat с размером — нет.
	t.Run("probe", func(t *testing.T) {
		r := e.call(http.MethodPost, rtGeoBase+"/probe", map[string]string{"file": rtGeoOwn, "url": rtGeoPageURL, "mode": "add"})
		if r.Status != http.StatusOK {
			t.Fatalf("probe page.html: %d %s", r.Status, r.Code)
		}
		res := r.object(t)
		if blocked, _ := res["blocked"].(bool); !blocked {
			t.Errorf("probe HTML-страницы: blocked=false")
		}
		if !rtGeoHasCheck(res, "size_none") {
			t.Errorf("probe HTML-страницы: нет проверки size_none")
		}

		r = e.call(http.MethodPost, rtGeoBase+"/probe", map[string]string{"file": rtGeoOwn, "url": rtGeoOKURL, "mode": "add"})
		if r.Status != http.StatusOK {
			t.Fatalf("probe ok.dat: %d %s", r.Status, r.Code)
		}
		res = r.object(t)
		if blocked, _ := res["blocked"].(bool); blocked {
			t.Errorf("probe годного dat: blocked=true: %v", res["checks"])
		}
		if size, _ := res["size"].(float64); size <= 0 {
			t.Errorf("probe годного dat: размер %v, ожидался положительный", res["size"])
		}
		// Недопустимое имя — блок проверки имени, а не отказ маршрута.
		r = e.call(http.MethodPost, rtGeoBase+"/probe", map[string]string{"file": "../x.dat", "url": rtGeoOKURL, "mode": "add"})
		if r.Status != http.StatusOK || !rtGeoHasCheck(r.object(t), "name_bad") {
			t.Errorf("probe с недопустимым именем: статус %d, ожидалась проверка name_bad", r.Status)
		}
	})

	if !hasJQ {
		t.Log("на стенде нет jq: запись в xkeen.json не проверяется")
		return
	}

	// Запись с блоком проверки: 409 probe_blocked с data.probe, xkeen.json не меняется.
	t.Run("write_blocked", func(t *testing.T) {
		before, err := os.ReadFile(rtGeoAPIConfig)
		if err != nil {
			t.Fatalf("чтение xkeen.json: %v", err)
		}
		r := e.call(http.MethodPost, rtGeoBase+"/write", map[string]any{
			"op": map[string]string{"kind": "add", "file": rtGeoOwn, "url": rtGeoPageURL}, "accept_comment_loss": true,
		})
		if r.Status != http.StatusConflict || r.Code != "probe_blocked" {
			t.Fatalf("write add с HTML-адресом: статус %d, код %q; ожидалось 409 probe_blocked", r.Status, r.Code)
		}
		probe, _ := rtGeoData(t, r)["probe"].(map[string]any)
		if blocked, _ := probe["blocked"].(bool); !blocked {
			t.Errorf("data.probe.blocked=false")
		}
		after, err := os.ReadFile(rtGeoAPIConfig)
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("xkeen.json изменился при блоке проверки")
		}
	})

	// Запись своего источника: в снимке строка own с записью, файла ещё нет.
	t.Run("write_add", func(t *testing.T) {
		op := map[string]string{"kind": "add", "file": rtGeoOwn, "url": rtGeoOKURL}
		pv := e.call(http.MethodPost, rtGeoBase+"/preview", map[string]any{"op": op})
		if pv.Status != http.StatusOK {
			t.Fatalf("preview add: %d %s", pv.Status, pv.Code)
		}
		p := pv.object(t)
		if p["before"] == p["after"] {
			t.Errorf("preview add: xkeen.json до и после совпадают")
		}
		body := map[string]any{"op": op, "base": p["base"], "accept_comment_loss": true, "update_now": false}
		r := e.call(http.MethodPost, rtGeoBase+"/write", body)
		if r.Status == http.StatusConflict && r.Code == "geofile_update_off" {
			// Обновление баз выключено в настройках XKeen: включается тем же API.
			en := e.call(http.MethodPost, rtGeoBase+"/write", map[string]any{"op": map[string]string{"kind": "enable_update"}, "accept_comment_loss": true})
			if en.Status != http.StatusOK {
				t.Fatalf("write enable_update: %d %s", en.Status, en.Code)
			}
			body["base"] = ""
			r = e.call(http.MethodPost, rtGeoBase+"/write", body)
		}
		if r.Status != http.StatusOK {
			t.Fatalf("write add: %d %s", r.Status, r.Code)
		}
		row := rtGeoSource(r.object(t), rtGeoOwn)
		if row == nil {
			t.Fatalf("в снимке после write нет строки %s", rtGeoOwn)
		}
		if row["origin"] != "own" || row["entry"] != true || row["present"] != false {
			t.Errorf("строка %s: origin=%v entry=%v present=%v; ожидалось own, true, false", rtGeoOwn, row["origin"], row["entry"], row["present"])
		}
		// Повтор того же имени — 409 name_taken (имя занято записью).
		again := e.call(http.MethodPost, rtGeoBase+"/write", map[string]any{"op": op, "accept_comment_loss": true})
		if again.Status != http.StatusConflict || again.Code != "name_taken" {
			t.Errorf("повторная запись имени: статус %d, код %q; ожидалось 409 name_taken", again.Status, again.Code)
		}
	})

	// Предпросмотр gh_proxy показывает правку; xkeen.json не меняется.
	t.Run("preview_gh_proxy", func(t *testing.T) {
		before, _ := os.ReadFile(rtGeoAPIConfig)
		r := e.call(http.MethodPost, rtGeoBase+"/preview", map[string]any{"op": map[string]string{"kind": "gh_proxy", "value": "https://gh-proxy.invalid"}})
		if r.Status != http.StatusOK {
			t.Fatalf("preview gh_proxy: %d %s", r.Status, r.Code)
		}
		p := r.object(t)
		if p["before"] == p["after"] {
			t.Errorf("preview gh_proxy: правка не видна в diff")
		}
		after, _ := os.ReadFile(rtGeoAPIConfig)
		if !bytes.Equal(before, after) {
			t.Errorf("preview изменил xkeen.json")
		}
		bad := e.call(http.MethodPost, rtGeoBase+"/preview", map[string]any{"op": map[string]string{"kind": "gh_proxy", "value": "ftp://x y"}})
		if bad.Status != http.StatusBadRequest || bad.Code != "url_invalid" {
			t.Errorf("preview gh_proxy с недопустимым адресом: статус %d, код %q; ожидалось 400 url_invalid", bad.Status, bad.Code)
		}
		unk := e.call(http.MethodPost, rtGeoBase+"/preview", map[string]any{"op": map[string]string{"kind": "no_such_kind"}})
		if unk.Status != http.StatusBadRequest || unk.Code != "invalid_input" {
			t.Errorf("preview неизвестного вида: статус %d, код %q; ожидалось 400 invalid_input", unk.Status, unk.Code)
		}
	})

	// Влияние, пометка и возврат источника без ссылок.
	t.Run("impact_drop_restore", func(t *testing.T) {
		imp := e.call(http.MethodGet, rtGeoBase+"/impact?file="+rtGeoOwn, nil)
		if imp.Status != http.StatusOK {
			t.Fatalf("impact: %d %s", imp.Status, imp.Code)
		}
		iv := imp.object(t)
		if foreign, _ := iv["foreign"].([]any); len(foreign) != 0 {
			t.Fatalf("impact: чужих ссылок %d у тестового файла", len(foreign))
		}
		r := e.call(http.MethodPost, rtGeoBase+"/drop", map[string]any{"rev": e.revision(), "file": rtGeoOwn, "mode": "delete", "actions": []any{}})
		if r.Status != http.StatusOK {
			t.Fatalf("drop: %d %s", r.Status, r.Code)
		}
		row := rtGeoSource(e.rtGeoSnapshot(), rtGeoOwn)
		if row == nil || row["draft"] != "delete" {
			t.Errorf("после drop у строки %s нет пометки delete: %v", rtGeoOwn, row)
		}
		draft, _ := e.rtGeoSnapshot()["draft"].([]any)
		found := false
		for _, d := range draft {
			if m, _ := d.(map[string]any); m != nil && m["file"] == rtGeoOwn {
				found = true
			}
		}
		if !found {
			t.Errorf("draft снимка не содержит %s после drop", rtGeoOwn)
		}
		// Режим вне контракта — 400 mode_invalid.
		bad := e.call(http.MethodPost, rtGeoBase+"/drop", map[string]any{"rev": e.revision(), "file": rtGeoOwn, "mode": "weird", "actions": []any{}})
		if bad.Status != http.StatusBadRequest || bad.Code != "mode_invalid" {
			t.Errorf("drop с режимом weird: статус %d, код %q; ожидалось 400 mode_invalid", bad.Status, bad.Code)
		}
		// Возврат снимает пометку; повторный возврат — 409 not_pending.
		rs := e.call(http.MethodPost, rtGeoBase+"/restore", map[string]any{"rev": e.revision(), "file": rtGeoOwn})
		if rs.Status != http.StatusOK {
			t.Fatalf("restore: %d %s", rs.Status, rs.Code)
		}
		if row := rtGeoSource(e.rtGeoSnapshot(), rtGeoOwn); row == nil || row["draft"] != nil {
			t.Errorf("после restore у строки %s осталась пометка: %v", rtGeoOwn, row)
		}
		again := e.call(http.MethodPost, rtGeoBase+"/restore", map[string]any{"rev": e.revision(), "file": rtGeoOwn})
		if again.Status != http.StatusConflict || again.Code != "not_pending" {
			t.Errorf("повторный restore: статус %d, код %q; ожидалось 409 not_pending", again.Status, again.Code)
		}
	})

	// Настоящий -ug по кнопке: ответ сразу, замок применения занят (geodata_busy),
	// вывод читается по смещениям, итог попадает в снимок.
	t.Run("run", func(t *testing.T) {
		rtGeoAPISkipWindow(t, 25*time.Minute)
		e.resetDraft()
		first := e.call(http.MethodPost, rtGeoBase+"/run", map[string]any{})
		if first.Status != http.StatusOK {
			t.Fatalf("run: %d %s", first.Status, first.Code)
		}
		if running, _ := first.object(t)["running"].(bool); !running {
			t.Errorf("run: running=false в ответе сразу после запуска")
		}
		ap := e.call(http.MethodPost, "/api/configlayer/apply", map[string]any{})
		switch {
		case ap.Status == http.StatusConflict && ap.Code == "geodata_busy":
		case ap.Status == http.StatusConflict && ap.Code == "apply_blocked":
			// Проверка черновика стенда отказала раньше замка: код занятости не проверить.
			t.Log("«Применить» отклонён проверкой черновика до замка")
		default:
			t.Errorf("«Применить» во время -ug: статус %d, код %q; ожидалось 409 geodata_busy", ap.Status, ap.Code)
		}
		second := e.call(http.MethodPost, rtGeoBase+"/run", map[string]any{})
		if second.Status != http.StatusConflict || second.Code != "geodata_busy" {
			t.Errorf("второй run: статус %d, код %q; ожидалось 409 geodata_busy", second.Status, second.Code)
		}
		// Запись во время -ug тоже занята.
		wr := e.call(http.MethodPost, rtGeoBase+"/write", map[string]any{"op": map[string]string{"kind": "gh_proxy", "value": ""}, "accept_comment_loss": true})
		if wr.Status != http.StatusConflict || wr.Code != "geodata_busy" {
			t.Errorf("write во время -ug: статус %d, код %q; ожидалось 409 geodata_busy", wr.Status, wr.Code)
		}

		// Вывод: непустой текст, смещение не убывает.
		deadline := time.Now().Add(20 * time.Minute)
		var text string
		var next int64
		var runID float64
		for time.Now().Before(deadline) {
			var running bool
			text, next, running, runID = e.rtGeoOutputRun("ug", 0)
			if text != "" || !running {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if text == "" {
			t.Fatalf("вывод ug пуст")
		}
		var prev int64 = -1
		for time.Now().Before(deadline) {
			_, n, running, id := e.rtGeoOutputRun("ug", next)
			if id != runID {
				// Первое чтение застало вывод прежнего запуска: новый запуск начинается
				// с нуля, смещения прежнего не сравниваются.
				runID, prev, next = id, -1, 0
				time.Sleep(time.Second)
				continue
			}
			if n < next {
				t.Errorf("смещение уменьшилось: %d после %d", n, next)
			}
			if prev >= 0 && n < prev {
				t.Errorf("смещение уменьшилось между опросами: %d после %d", n, prev)
			}
			prev, next = n, max(n, next)
			if !running {
				break
			}
			time.Sleep(5 * time.Second)
		}
		// Конец запуска: running=false, итог есть.
		for time.Now().Before(deadline) {
			if r, _ := e.rtGeoSnapshot()["run"].(map[string]any); r != nil && r["running"] == false {
				break
			}
			time.Sleep(5 * time.Second)
		}
		snap := e.rtGeoSnapshot()
		if run, _ := snap["run"].(map[string]any); run == nil || run["running"] != false {
			t.Fatalf("запуск -ug не завершился за 20 минут")
		}
		if snap["last_run"] == nil {
			t.Errorf("в снимке нет last_run после запуска")
		}
		// Замок освобождён: держателя в снимке слоя нет.
		st := e.call(http.MethodGet, "/api/configlayer/state", nil)
		if st.Status != http.StatusOK {
			t.Fatalf("GET /api/configlayer/state: %d %s", st.Status, st.Code)
		}
		if holder, _ := st.object(t)["busy_holder"].(string); holder != "" {
			t.Errorf("после -ug замок слоя держит %q", holder)
		}
	})
}

// rtGeoHasCheck — в результате проверки источника есть проверка с кодом.
func rtGeoHasCheck(res map[string]any, code string) bool {
	checks, _ := res["checks"].([]any)
	for _, c := range checks {
		if m, _ := c.(map[string]any); m != nil && m["code"] == code {
			return true
		}
	}
	return false
}
