//go:build router

package handlers

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Обновление подписки с kernel=xray не должно слать Mihomo команду обновления
// провайдера. Интерфейс обновляет провайдер Mihomo своим запросом
// PUT /api/proxy-providers/<имя>/refresh; если бы тот же клик ещё и просил
// POST /api/subscriptions/refresh обновить Mihomo, провайдер получал бы вторую
// загрузку и вторую команду (слоты HWID, ответ адаптера опаздывал за срок Mihomo).
// Вердикт — журнал панели в окне запроса: строки «Mihomo reload triggered for
// provider <имя этой подписки> (» быть не должно.
func TestRouterSubscriptionRefreshXrayOnly(t *testing.T) {
	if routertest.Current(t).Core != "mihomo" {
		t.Skip("ядро ветви не Mihomo: команда обновления провайдера не проверяется")
	}
	p := routertest.NewPanel(t)

	var subs []services.Subscription
	if code, err := p.GetJSON("/api/subscriptions", &subs); err != nil || code != http.StatusOK {
		t.Fatalf("GET /api/subscriptions: http %d, ошибка %v", code, err)
	}
	var sub *services.Subscription
	for i := range subs {
		if subs[i].Enabled && subs[i].EnableXray && subs[i].EnableMihomo {
			sub = &subs[i]
			break
		}
	}
	if sub == nil {
		t.Skip("на стенде нет включённой подписки с Xray и Mihomo: двойная команда невозможна")
	}
	id := url.QueryEscape(sub.ID)

	// Неизвестное ядро отвергается до обращения к провайдеру.
	if code, _, err := p.Call(http.MethodPost, "/api/subscriptions/refresh?id="+id+"&kernel=bogus", nil); err != nil {
		t.Fatalf("POST refresh kernel=bogus: %v", err)
	} else if code != http.StatusBadRequest {
		t.Fatalf("POST refresh kernel=bogus: http %d, ожидался 400", code)
	}

	// Строка журнала записывается тем же именем, что отдаёт GetProviderName.
	marker := "Mihomo reload triggered for provider " + sub.GetProviderName() + " ("

	logPath := config.Default().XCPLogPath
	if logPath == "" {
		logPath = "/opt/var/log/xcp.log"
	}
	before, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("журнал панели: %v", err)
	}
	offset := before.Size()

	code, body, err := p.CallSlow(http.MethodPost, "/api/subscriptions/refresh?id="+id+"&kernel=xray", nil)
	if err != nil {
		t.Fatalf("POST refresh kernel=xray: %v", err)
	}
	if code == http.StatusBadRequest {
		t.Fatalf("POST refresh kernel=xray: http 400 для подписки с Xray")
	}
	if code == http.StatusInternalServerError && strings.Contains(string(body), "refresh already in progress") {
		t.Skip("параллельное обновление этой подписки (планировщик или refresh-all): вердикт невозможен")
	}
	// 200 или 500 (отказ провайдера для Xray) допустимы: команда Mihomo не должна уйти
	// в обоих случаях.
	t.Logf("POST refresh kernel=xray: http %d", code)

	// Окно журнала: от смещения до строки доступа этого запроса. Строку доступа
	// middleware пишет после ответа обработчика, поэтому внутри окна лежит всё, что
	// обработчик успел записать.
	var window string
	deadline := time.Now().Add(5 * time.Second)
	for {
		window, err = rtReadLogFrom(logPath, offset)
		if err != nil {
			t.Skipf("журнал ротирован во время проверки: %v", err)
		}
		if i := strings.Index(window, "POST /api/subscriptions/refresh "); i >= 0 {
			window = window[:i]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("строка доступа запроса не найдена — окно журнала неверно")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Почему строка с именем этой подписки в окне — именно от этого запроса: refresh
	// пишет её, только пока держит ongoing по id. Ответ не «already in progress»
	// означает, что на время запроса ongoing держал этот запрос, а чужое обновление
	// той же подписки не могло ни начаться, ни закончиться внутри окна: между записью
	// строки и освобождением ongoing идёт запрос к Mihomo, а он длится секунды. Строки
	// той же формы для других провайдеров вердикт не меняют.
	if n := strings.Count(window, marker); n > 0 {
		t.Errorf("kernel=xray отправил Mihomo команду обновления провайдера: строк в журнале %d", n)
	}
}

// rtReadLogFrom читает журнал с заданного смещения. Файл меньше смещения — ошибка
// (журнал ротирован).
func rtReadLogFrom(path string, offset int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() < offset {
		return "", io.ErrUnexpectedEOF
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(f, 8<<20))
	return string(raw), err
}
