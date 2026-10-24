//go:build router

package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Нажатие «Обновить» на подписке с Xray и Mihomo шлёт параллельно
// POST /api/subscriptions/refresh?kernel=xray и PUT /api/proxy-providers/<имя>/refresh.
// По PUT Mihomo скачивает провайдер у адаптера панели (/api/provider.yaml) и ждёт тело
// не дольше 20 с, иначе обновление провайдера падает (503 от Mihomo, 502 в панели).
// Адаптер обещает ответ за providerFetchBudget (14 с): запись кэша и состояния подписки
// на медленную флешку, а с ней ожидание блокировки подписок за записью обновления Xray,
// в этот срок входить не должны. Тест сам забирает тело у адаптера по адресу из
// config.yaml Mihomo — так же, как Mihomo, — пока идёт обновление Xray, а фоновая запись
// с fsync рядом с журналом панели нагружает флешку. Ядро не участвует: вердикт — время
// до полного тела адаптера, ответ Mihomo на PUT от собственной записи на диск не зависит.
func TestRouterProviderAdapterUnderDiskLoad(t *testing.T) {
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
		t.Skip("на стенде нет включённой подписки с Xray и Mihomo")
	}
	adapter := rtAdapterURL(t, sub)

	logPath := config.Default().XCPLogPath
	if logPath == "" {
		logPath = "/opt/var/log/xcp.log"
	}
	before, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("журнал панели: %v", err)
	}
	load := startDiskLoad(t, filepath.Dir(logPath))

	var wg sync.WaitGroup
	var postCode int
	var postBody []byte
	var postErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		postCode, postBody, postErr = p.CallSlow(http.MethodPost, "/api/subscriptions/refresh?id="+url.QueryEscape(sub.ID)+"&kernel=xray", nil)
	}()

	// Mihomo ждёт тело 20 с; клиенту теста — с запасом, чтобы увидеть, сколько на самом деле
	client := &http.Client{Timeout: 2 * time.Minute}
	start := time.Now()
	resp, err := client.Get(adapter)
	var size int
	code := 0
	if err == nil {
		code = resp.StatusCode
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		size = len(body)
		err = readErr
	}
	took := time.Since(start)
	wg.Wait()
	syncs := load()
	// Адаптер пишет кэш и состояние подписки уже после ответа: следующий тест начинается,
	// когда обработчик закончил (строку доступа middleware пишет после его выхода)
	deadline := time.Now().Add(2 * time.Minute)
	for {
		window, readErr := rtReadLogFrom(logPath, before.Size())
		if readErr != nil || strings.Contains(window, "GET /api/provider.yaml ") {
			break
		}
		if time.Now().After(deadline) {
			t.Logf("строка доступа адаптера не появилась за 2 мин")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("fsync под нагрузкой: %s", syncs)
	t.Logf("адаптер: http %d, %d байт за %s; POST refresh kernel=xray: http %d", code, size, took.Round(100*time.Millisecond), postCode)
	if err != nil {
		t.Fatalf("запрос к адаптеру провайдера: %v", err)
	}
	if postErr != nil {
		t.Fatalf("POST refresh kernel=xray: %v", postErr)
	}
	if postCode == http.StatusInternalServerError && strings.Contains(string(postBody), "refresh already in progress") {
		t.Skip("параллельное обновление этой подписки (планировщик или refresh-all): вердикт невозможен")
	}
	if code != http.StatusOK || size == 0 {
		// Без тела и без кэша адаптер отвечает 502: провайдер недоступен
		t.Skipf("адаптер не отдал провайдер: http %d, %d байт", code, size)
	}
	if took > providerAdapterBudget {
		t.Errorf("тело адаптера провайдера при медленной флешке пришло за %s, больше %s", took.Round(100*time.Millisecond), providerAdapterBudget)
	}
}

// providerAdapterBudget — providerFetchBudget адаптера (internal/services): срок, за
// который адаптер обязан отдать тело, чтобы Mihomo успел за свои 20 с.
const providerAdapterBudget = 14 * time.Second

// rtAdapterURL — адрес адаптера провайдера подписки из config.yaml Mihomo, по которому
// ходит Mihomo. В адресе лежит адрес подписки, поэтому он не выводится ни в журнал, ни в ошибки.
func rtAdapterURL(t *testing.T, sub *services.Subscription) string {
	t.Helper()
	dir := config.Default().MihomoConfigDir
	if dir == "" {
		dir = "/opt/etc/mihomo"
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Skipf("config.yaml Mihomo не читается: %v", err)
	}
	want := "/api/provider.yaml?url=" + url.QueryEscape(sub.URL)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "url:") || !strings.Contains(line, want) {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "url:")), `'"`)
	}
	t.Skip("в config.yaml Mihomo нет провайдера этой подписки")
	return ""
}

// Нагрузка на флешку: diskLoadWriters писателей, каждый пишет блоки по diskLoadBlock
// с fsync после каждого в свой файл, по кругу в пределах diskLoadFile.
const (
	diskLoadWriters = 2
	diskLoadBlock   = 4 << 20
	diskLoadFile    = 64 << 20
	diskLoadWarmup  = 5 * time.Second
)

// startDiskLoad нагружает флешку в каталоге dir, пока не вызвана возвращённая функция.
// Она останавливает запись, удаляет файлы и возвращает сводку задержек fsync. Перед
// возвратом нагрузка идёт diskLoadWarmup, чтобы очередь записи успела набраться.
func startDiskLoad(t *testing.T, dir string) func() string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan []time.Duration, diskLoadWriters)
	paths := make([]string, 0, diskLoadWriters)
	for w := 0; w < diskLoadWriters; w++ {
		path := filepath.Join(dir, fmt.Sprintf(".xcp-rt-disk-load-%d", w))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			cancel()
			t.Fatalf("файл нагрузки на флешку: %v", err)
		}
		paths = append(paths, path)
		t.Cleanup(func() { _ = os.Remove(path) })
		go func() {
			defer f.Close()
			block := make([]byte, diskLoadBlock)
			for i := range block {
				block[i] = byte(i)
			}
			var lat []time.Duration
			for n := 0; ctx.Err() == nil; n++ {
				if n%(diskLoadFile/diskLoadBlock) == 0 {
					if _, err := f.Seek(0, io.SeekStart); err != nil {
						break
					}
				}
				if _, err := f.Write(block); err != nil {
					break
				}
				start := time.Now()
				if err := f.Sync(); err != nil {
					break
				}
				lat = append(lat, time.Since(start))
			}
			results <- lat
		}()
	}
	time.Sleep(diskLoadWarmup)

	return func() string {
		cancel()
		var lat []time.Duration
		for w := 0; w < diskLoadWriters; w++ {
			lat = append(lat, <-results...)
		}
		for _, path := range paths {
			_ = os.Remove(path)
		}
		if len(lat) == 0 {
			return "нет замеров"
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		return fmt.Sprintf("n=%d, медиана %s, макс %s", len(lat),
			lat[len(lat)/2].Round(time.Millisecond), lat[len(lat)-1].Round(time.Millisecond))
	}
}
