//go:build router

// router-area: configlayer

package handlers

import (
	"net/http"
	"testing"
)

// Старые входы настройки, которые пишут конфиг ядра мимо слоя «Конфигурация»
// (D-17), на настоящей панели устройства: при включённом флаге изменяющие
// маршруты старой модели подписок, слияние шаблона и сохранение правил
// пользователя отвечают 409 legacy_blocked с переведённым текстом, маршруты
// чтения — как раньше. Без флага ответ даёт прежний обработчик. Вердикт — коды и
// тела ответов панели; ядро не читается и не переключается.
//
// Все запросы несут тела и параметры, которые прежний обработчик отклоняет до
// записи и сети (нет id или URL, тело не объект): даже без закрытия они не
// создают подписку и не меняют правила на стенде. /api/subscriptions/refresh-all
// без флага не вызывается — прежний обработчик сразу обновил бы все подписки
// старой модели.
func TestRouterLegacyEntriesBlocked(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	// Тест выключает слой посреди сценария, а очистка enableLayer ходит в API слоя:
	// флаг возвращается до неё (очистки идут в обратном порядке).
	t.Cleanup(func() { e.setLayer(true) })

	// notObject — тело-строка JSON: прежние обработчики слияния и правил отвечают
	// 400 «Invalid request body». Пустой объект обработчик правил принял бы и
	// записал пустой список.
	const notObject = "xcp-rt-not-an-object"
	empty := map[string]string{}

	type route struct {
		path string
		in   any
	}
	// Маршруты, у которых прежний обработчик отказывает без записи и сети.
	safe := []route{
		{"/api/subscriptions/add", empty},
		{"/api/subscriptions/update", empty},
		{"/api/subscriptions/delete", nil},
		{"/api/subscriptions/refresh", nil},
		{"/api/subscriptions/active", empty},
		{"/api/subscriptions/active/clear", nil},
		{"/api/subscriptions/node-dialer-proxy", empty},
		{"/api/config/smart-merge", notObject},
		{"/api/rules/custom", notObject},
	}

	closed := append([]route{{"/api/subscriptions/refresh-all", nil}}, safe...)
	for _, c := range closed {
		r := e.call(http.MethodPost, c.path, c.in)
		if r.Status != http.StatusConflict || r.Code != "legacy_blocked" || r.Error == "" {
			t.Errorf("старый вход при включённом флаге: POST %s — статус %d, код %q, текст пуст: %v; ожидалось 409 legacy_blocked с текстом",
				c.path, r.Status, r.Code, r.Error == "")
		}
	}
	for _, path := range []string{"/api/subscriptions", "/api/rules/custom"} {
		if r := e.call(http.MethodGet, path, nil); r.Status != http.StatusOK {
			t.Errorf("чтение при включённом флаге: GET %s — статус %d, ожидался 200", path, r.Status)
		}
	}

	e.setLayer(false)
	for _, c := range safe {
		r := e.call(http.MethodPost, c.path, c.in)
		if r.Status == http.StatusConflict || r.Code == "legacy_blocked" {
			t.Errorf("старый вход без флага: POST %s — статус %d, код %q; ожидался ответ прежнего обработчика",
				c.path, r.Status, r.Code)
		}
		if r.Status != http.StatusBadRequest {
			t.Errorf("старый вход без флага: POST %s — статус %d, ожидался 400 прежнего обработчика (отказ до записи)",
				c.path, r.Status)
		}
	}
}
