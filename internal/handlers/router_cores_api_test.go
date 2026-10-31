//go:build router

package handlers

import (
	"net/http"
	"slices"
	"testing"

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

// rtCoresXraySections — секции Xray в порядке показа.
var rtCoresXraySections = []string{"log", "dns", "inbounds", "policy", "other"}

// rtCoresOwners — допустимые владельцы секции в виде.
var rtCoresOwners = []string{"panel", "pending", "xkeen", "user", "none", "drift"}

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

// Трасса: слой выключен → 404; слой включён → вид секций Xray и Mihomo; неизвестное
// ядро → 400. Метка ядра: сценарий на шагах задачи 2 читает активное ядро.
func TestRouterCoresAPIFlow(t *testing.T) {
	_ = routertest.CoreSpecific(t)
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
	})
}
