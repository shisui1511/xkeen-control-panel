//go:build router

package handlers

import (
	"net/http"
	"testing"
)

// Старые маршруты записи DAT на настоящей панели устройства: при включённом слое
// «Конфигурация» /api/dat/update и /api/dat/rollback отвечают как несуществующие
// (404 unknown API endpoint, на любой метод), с выключенным — прежнее поведение;
// маршруты чтения DAT не закрываются. Вердикт — коды и тела ответов панели.
// POST /api/dat/rollback не вызывается: прежний обработчик откатил бы .bak на
// устройстве. Ядро не читается и не переключается.
func TestRouterDatLegacyFlag(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()

	body := map[string]string{"file": "xcp-rt-absent.dat", "type": "xray"}
	closed := []struct {
		method string
		path   string
		in     any
	}{
		{http.MethodGet, "/api/dat/update", nil},
		{http.MethodGet, "/api/dat/rollback", nil},
		{http.MethodPost, "/api/dat/update", body},
	}
	for _, c := range closed {
		r := e.call(c.method, c.path, c.in)
		if r.Status != http.StatusNotFound || r.Error != "unknown API endpoint" {
			t.Errorf("старый маршрут DAT при включённом флаге: %s %s — статус %d, ошибка %q; ожидалось 404 unknown API endpoint",
				c.method, c.path, r.Status, r.Error)
		}
	}
	if r := e.call(http.MethodGet, "/api/dat/list", nil); r.Status != http.StatusOK {
		t.Errorf("чтение DAT при включённом флаге: GET /api/dat/list — статус %d, ожидался 200", r.Status)
	}

	e.setLayer(false)
	for _, path := range []string{"/api/dat/update", "/api/dat/rollback"} {
		if r := e.call(http.MethodGet, path, nil); r.Status != http.StatusMethodNotAllowed {
			t.Errorf("старый маршрут DAT без флага: GET %s — статус %d, ожидался 405", path, r.Status)
		}
	}
	if r := e.call(http.MethodPost, "/api/dat/update", body); r.Status == http.StatusNotFound {
		t.Errorf("старый маршрут DAT без флага: POST /api/dat/update — статус %d, ожидался ответ прежнего обработчика", r.Status)
	}
}
