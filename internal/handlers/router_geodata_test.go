//go:build router

package handlers

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/geodata"
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

// TestRouterGeodataAPI — API менеджера geodata на настоящей панели: снимок по контракту,
// проверка источника, запись, расписание, запуск -ug с занятостью замка, вывод по
// смещениям, влияние, пометка и возврат, теги, записи, поиск, защита маршрутов.
func TestRouterGeodataAPI(t *testing.T) {
	e := rtNewSrc(t)

	// Флаг слоя выключен: менеджер отвечает 404 config_layer_disabled.
	t.Run("flag_off", func(t *testing.T) {
		e := &rtSrc{t: t, p: e.p}
		e.setLayer(false)
		r := e.call(http.MethodGet, rtGeoBase, nil)
		if r.Status != http.StatusNotFound || r.Code != "config_layer_disabled" {
			t.Errorf("GET %s при выключенном слое: статус %d, код %q; ожидалось 404 config_layer_disabled", rtGeoBase, r.Status, r.Code)
		}
	})

	e.enableLayer()

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
}
