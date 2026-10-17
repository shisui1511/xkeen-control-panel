//go:build router

package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Опрос Clash API Mihomo через панель (/api/mihomo/proxy/...) во время перезапуска
// ядра кнопкой панели. XKeen выполняет -restart в фоне: панель отвечает на POST
// restart, пока старый процесс ядра ещё жив, а гасит его XKeen позже; несколько
// секунд API ядра не принимает соединения. Страницы панели опрашивают API по гейту
// из capabilities, который в это окно ещё открыт (он видел старый процесс), поэтому
// опрос в окне — штатная ситуация. Ответ 5xx на него браузер пишет в консоль
// ошибкой: перезапуск, начатый самой панелью, не должен выглядеть сбоем сервера.
// Ожидаемый ответ в окне — сразу 409 с кодом kernel_restarting, вне окна — 200.
//
// Вердикт — статусы ответов панели и сокет API ядра, который тест опрашивает
// напрямую: окно, когда API не принимает соединения, должно наблюдаться, и в него
// должны попасть опросы панели, иначе сценарий ничего не проверил.

const (
	// rtMprPollers — потоков опроса API через панель.
	rtMprPollers = 3
	// rtMprPollGap — пауза между опросами одного потока.
	rtMprPollGap = 200 * time.Millisecond
	// rtMprProbeGap — шаг прямой пробы сокета API ядра.
	rtMprProbeGap = 100 * time.Millisecond
	// rtMprRestartWait — сколько ждать нового процесса ядра и ответа его API.
	rtMprRestartWait = 90 * time.Second
	// rtMprCallLimit — предел одного опроса: дольше — опрос завис.
	rtMprCallLimit = 30 * time.Second
	// rtMprTail — сколько опросы идут после того, как API снова принял соединение.
	rtMprTail = 3 * time.Second
)

// rtMprPaths — опросы страниц панели: провайдеры (дашборд, «Прокси») и прокси («Прокси»).
var rtMprPaths = []string{
	"/api/mihomo/proxy/providers/proxies",
	"/api/mihomo/proxy/proxies",
}

// rtMprCall — один опрос панели: начало и конец относительно старта сценария.
type rtMprCall struct {
	path       string
	start, end time.Duration
	status     int
	code       string
	err        string
	body       string
}

// rtMprProbe — состояние сокета API ядра на момент пробы.
type rtMprProbe struct {
	at time.Duration
	up bool
}

// rtMprAPIAddr — сеть и адрес Clash API из config.yaml Mihomo (как его видит панель).
func rtMprAPIAddr(t *testing.T) (network, addr string) {
	t.Helper()
	info, err := services.NewMihomoService("mihomo", "xkeen", "/opt/etc/mihomo").ParseControllerConfig()
	if err != nil {
		t.Fatalf("config.yaml Mihomo не читается: %v", err)
	}
	switch info.Type {
	case "unix":
		return "unix", info.Target
	case "tcp":
		target := strings.TrimPrefix(strings.TrimPrefix(info.Target, "http://"), "https://")
		return "tcp", strings.TrimRight(target, "/")
	}
	// Без external-controller у ядра нет Clash API: панели нечего проксировать,
	// окно перезапуска наблюдать нечем. Это настройка стенда, а не дефект.
	t.Skipf("в config.yaml Mihomo нет external-controller (тип %q): Clash API выключен", info.Type)
	return "", ""
}

// rtMprDial — принимает ли API ядра соединение прямо сейчас.
func rtMprDial(network, addr string) bool {
	c, err := net.DialTimeout(network, addr, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func TestRouterMihomoProxyDuringRestart(t *testing.T) {
	tg := routertest.Current(t)
	if tg.Core != "mihomo" {
		t.Skip("ядро ветви не Mihomo: Clash API через панель не опрашивается")
	}
	network, addr := rtMprAPIAddr(t)
	pid := rtRstKernelPID(t, "mihomo")
	p := routertest.NewPanel(t)

	// Ядро, оставшееся лежать после провала, поднимается: следующие проверки
	// работают с тем же стендом.
	t.Cleanup(func() {
		if pids, _ := rtRstKernelPIDs("mihomo"); len(pids) > 0 {
			return
		}
		_, _, _ = p.CallSlow(http.MethodPost, "/api/service/control?action=start", nil)
		rtHelperWaitNewPID("mihomo", 0, rtMprRestartWait)
	})

	// Исходное состояние: API ядра отвечает и напрямую, и через панель.
	if !rtMprDial(network, addr) {
		t.Fatalf("API Mihomo (%s) не принимает соединения до перезапуска", network)
	}
	for _, path := range rtMprPaths {
		status, raw, err := p.Call(http.MethodGet, path, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("до перезапуска GET %s: %d %v %.200s", path, status, err, raw)
		}
	}

	begin := time.Now()
	since := func() time.Duration { return time.Since(begin) }
	var (
		mu     sync.Mutex
		calls  []rtMprCall
		probes []rtMprProbe
		stop   = make(chan struct{})
		wg     sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			up := rtMprDial(network, addr)
			mu.Lock()
			probes = append(probes, rtMprProbe{at: since(), up: up})
			mu.Unlock()
			time.Sleep(rtMprProbeGap)
		}
	}()
	for i := 0; i < rtMprPollers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := i; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				path := rtMprPaths[n%len(rtMprPaths)]
				c := rtMprCall{path: path, start: since()}
				status, raw, err := p.Call(http.MethodGet, path, nil)
				c.end, c.status = since(), status
				if err != nil {
					c.err = err.Error()
				}
				if status >= 300 || err != nil {
					c.body = fmt.Sprintf("%.200s", raw)
					var env struct {
						Code string `json:"code"`
					}
					if json.Unmarshal(raw, &env) == nil {
						c.code = env.Code
					}
				}
				mu.Lock()
				calls = append(calls, c)
				mu.Unlock()
				time.Sleep(rtMprPollGap)
			}
		}(i)
	}
	stopAll := func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		wg.Wait()
	}

	// Опросы уже идут: перезапуск кнопкой панели.
	time.Sleep(time.Second)
	status, raw, err := p.CallSlow(http.MethodPost, "/api/service/control?action=restart", nil)
	answered := since()
	if err != nil || status != http.StatusOK {
		stopAll()
		t.Fatalf("POST restart: %d %v %.300s", status, err, raw)
	}
	next, out := rtHelperWaitNewPID("mihomo", pid, rtMprRestartWait)
	if next == 0 {
		stopAll()
		t.Fatalf("за %v после перезапуска нет нового процесса Mihomo (pidof: %q)", rtMprRestartWait, out)
	}
	newPID := since()

	// API нового процесса принял соединение; опросы идут ещё rtMprTail.
	deadline := time.Now().Add(rtMprRestartWait)
	for !rtMprDial(network, addr) && time.Now().Before(deadline) {
		time.Sleep(rtMprProbeGap)
	}
	if !rtMprDial(network, addr) {
		stopAll()
		t.Fatalf("за %v API нового процесса Mihomo (PID %d) не принял соединение", rtMprRestartWait, next)
	}
	time.Sleep(rtMprTail)
	stopAll()

	mu.Lock()
	defer mu.Unlock()
	sort.Slice(calls, func(i, j int) bool { return calls[i].start < calls[j].start })

	// Окно недоступности API по прямой пробе: от первого отказа до последнего.
	var downFrom, downTo time.Duration = -1, -1
	for _, pr := range probes {
		if pr.up {
			continue
		}
		if downFrom < 0 {
			downFrom = pr.at
		}
		downTo = pr.at
	}
	round := func(d time.Duration) time.Duration { return d.Round(10 * time.Millisecond) }
	t.Logf("ответ на restart через %v, новый PID %d через %v, API не принимал соединения с %v по %v (проб %d)",
		round(answered), next, round(newPID), round(downFrom), round(downTo), len(probes))
	if downFrom < 0 {
		t.Fatalf("прямая проба ни разу не застала API ядра недоступным: окно перезапуска не проверено")
	}

	byStatus := map[string]int{}
	var inWindow, okAfter int
	var bad, other, slow []string
	for _, c := range calls {
		key := fmt.Sprint(c.status)
		if c.code != "" {
			key += " " + c.code
		}
		if c.err != "" {
			key = "ошибка"
		}
		byStatus[key]++
		if c.start <= downTo && c.end >= downFrom {
			inWindow++
		}
		if c.start > downTo && c.status == http.StatusOK {
			okAfter++
		}
		line := fmt.Sprintf("GET %s: начало %v, длился %v, статус %d %s%s",
			c.path, round(c.start), round(c.end-c.start), c.status, c.err, c.body)
		switch {
		case c.err != "" || c.status >= 500:
			bad = append(bad, line)
		case c.status != http.StatusOK && !(c.status == http.StatusConflict && c.code == "kernel_restarting"):
			other = append(other, line)
		}
		if c.end-c.start > rtMprCallLimit {
			slow = append(slow, line)
		}
	}
	t.Logf("опросов через панель: %d, по статусам %v, пересекли окно недоступности API: %d", len(calls), byStatus, inWindow)
	if inWindow == 0 {
		t.Fatalf("ни один опрос панели не пересёк окно недоступности API (%v–%v): сценарий не проверен", round(downFrom), round(downTo))
	}

	first := func(lines []string) string {
		if len(lines) > 5 {
			lines = lines[:5]
		}
		return strings.Join(lines, "\n")
	}
	routertest.Verdict(t, len(bad) == 0, routertest.KnownMark{},
		"во время перезапуска ядра панель ответила ошибкой сервера или оборвала опрос %d раз из %d:\n%s",
		len(bad), len(calls), first(bad))
	routertest.Verdict(t, len(other) == 0, routertest.KnownMark{},
		"ответы кроме 200 и 409 kernel_restarting: %d\n%s", len(other), first(other))
	routertest.Verdict(t, len(slow) == 0, routertest.KnownMark{},
		"опросы дольше %v: %d\n%s", rtMprCallLimit, len(slow), first(slow))
	routertest.Verdict(t, okAfter > 0, routertest.KnownMark{},
		"после возврата API ни один опрос через панель не получил 200")
}
