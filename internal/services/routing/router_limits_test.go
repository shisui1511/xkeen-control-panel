//go:build router

// router-area: routing

package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Учёт лимитов трафика и удержание ручной группы квотой на обоих ядрах (SPX-01,
// D-02, D-03, D-05, D-06, D-13, D-14). Процессы ядер — изолированные, в каталоге
// теста: Mihomo — профиль панели окружения с socks-портом и сокетом контроллера в
// каталоге теста, Xray — копия файлов панели с socks-входом сценария. Служба лимитов
// настоящая, с файлом лимитов в каталоге теста; цели учёта — TrafficTargets
// окружения; арбитр — настоящий (Start, Kick по смене удержаний). Applier не задан,
// ядро стенда тест не читает и не переключает: активное ядро службы лимитов — функция
// теста. Метки ядра нет. Часы — настоящие (Deps.Now по умолчанию). В журнал — роли,
// коды, да/нет и числа; цели и узлы — на loopback.
//
// Почему отдельные функции, а не подтесты TestRouterMihomoRulesTraffic и
// TestRouterXrayAPIBlock: у тех же подготовок после подтестов spike 152-01
// (chains_facts, group_bodies_and_counters) бюджет gate 60 с уже занят, а учёт с
// удержанием занимает ещё ~45–50 с на тест. Подготовка здесь шире (служба лимитов,
// служба потоков на сокете процесса, арбитр, повторный запуск процесса после сверки
// хвоста), а kernel_switch_continues требует обоих ядер в одном тесте.

const (
	// rtlmBlobPath — путь цели, отдающий rtlmBlobSize байт; остальные пути — 204.
	rtlmBlobPath = "/rtlm-blob"
	// rtlmChunk и rtlmChunks — ответ цели кусками по 32 КБ, всего 256 КБ.
	rtlmChunk    = 32 << 10
	rtlmChunks   = 8
	rtlmBlobSize = rtlmChunk * rtlmChunks
	// rtlmChunkGap и rtlmHold — медленный ответ (?slow=1): соединение живёт
	// несколько снимков /connections Mihomo (раз в секунду), а последние байты
	// видны снимку до закрытия соединения.
	rtlmChunkGap = 200 * time.Millisecond
	rtlmHold     = 1200 * time.Millisecond
	// rtlmLimitBytes — лимит, который исчерпывает один ответ цели.
	rtlmLimitBytes = 64 << 10
	// rtlmBigLimit — лимит, который тест не исчерпывает.
	rtlmBigLimit = int64(1) << 40
)

// rtlmAllDays — все дни недели окна расписания.
var rtlmAllDays = []int{0, 1, 2, 3, 4, 5, 6}

// rtlmTarget поднимает цель на loopback: rtlmBlobPath отдаёт rtlmBlobSize байт без
// Content-Length (конец ответа — после паузы rtlmHold у медленного ответа) и считает
// такие запросы; остальное — 204.
func rtlmTarget(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	chunk := bytes.Repeat([]byte("x"), rtlmChunk)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rtlmBlobPath {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		hits.Add(1)
		slow := r.URL.Query().Get("slow") == "1"
		pause := func(d time.Duration) bool {
			select {
			case <-time.After(d):
				return true
			case <-r.Context().Done():
				return false
			}
		}
		fl, _ := w.(http.Flusher)
		for range rtlmChunks {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			if slow && !pause(rtlmChunkGap) {
				return
			}
		}
		if slow {
			pause(rtlmHold)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// rtlmFetch делает GET через socks процесса ядра и читает ответ целиком.
// Возвращает число прочитанных байт; ошибка — запрос не прошёл или статус не 200.
func rtlmFetch(socksPort int, rawURL string, timeout time.Duration) (int64, error) {
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), nil, proxy.Direct)
	if err != nil {
		return 0, err
	}
	cd, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return 0, io.ErrUnexpectedEOF
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return cd.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = errors.New("неожиданный статус")
	}
	return n, err
}

// rtlmUntil опрашивает cond раз в 200 мс до wait; true — условие выполнилось.
func rtlmUntil(wait time.Duration, cond func() bool) bool {
	for end := time.Now().Add(wait); ; time.Sleep(200 * time.Millisecond) {
		if cond() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// rtlmTraffic гонит трафик fetch раз в секунду, пока cond не выполнится или не
// выйдет wait; true — условие выполнилось.
func rtlmTraffic(wait time.Duration, fetch func(), cond func() bool) bool {
	for end := time.Now().Add(wait); ; time.Sleep(time.Second) {
		fetch()
		if cond() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// rtlmErrCode — код отказа службы лимитов для журнала без текста ошибки.
func rtlmErrCode(err error) string {
	var in *services.LimitInputError
	switch {
	case err == nil:
		return "nil"
	case errors.As(err, &in):
		return in.Field + ":" + in.Reason
	case errors.Is(err, services.ErrLimitNotFound):
		return "not_found"
	}
	return "other"
}

// rtlmSave сохраняет новый лимит (период — месяц, включён) и удаляет его в очистке
// теста; отказ — провал теста.
func rtlmSave(t *testing.T, ls *services.LimitsService, l services.TrafficLimit) services.TrafficLimit {
	t.Helper()
	l.Period, l.Enabled = "monthly", true
	out, err := ls.Save(l)
	if err != nil {
		t.Fatalf("лимит %s: отказ %s", l.Name, rtlmErrCode(err))
	}
	t.Cleanup(func() { _ = ls.Delete(out.ID) })
	return out
}

// rtlmUsed — расход лимита id из списка службы; лимита нет — -1.
func rtlmUsed(ls *services.LimitsService, id string) int64 {
	for _, l := range ls.List().Limits {
		if l.ID == id {
			return l.UsedBytes
		}
	}
	return -1
}

// rtlmSel — выбор группы gid в Live.
func rtlmSel(t *testing.T, e *rtEnv, gid string) string {
	t.Helper()
	return liveSelection(e.state(t).Live)[gid]
}

// rtlmHolder — держатель группы gid в записи арбитра ("" — записи нет).
func rtlmHolder(t *testing.T, e *rtEnv, gid string) string {
	t.Helper()
	rec, ok := readArbiter(e.state(t).Live)[gid]
	if !ok {
		return ""
	}
	return rec.Holder
}

// rtlmTail — снимок служебного хвоста в Live (ключ LiveKeyHiddenTail; нет ключа —
// пустой снимок). Значение разбирается тестом, а не readHiddenTail.
func rtlmTail(t *testing.T, e *rtEnv) hiddenTail {
	t.Helper()
	raw, ok := e.state(t).Live[LiveKeyHiddenTail]
	if !ok || isEmptyRaw(raw) {
		return hiddenTail{}
	}
	var tl hiddenTail
	if err := json.Unmarshal(raw, &tl); err != nil {
		t.Fatalf("Live %s не разобран: %v", LiveKeyHiddenTail, err)
	}
	return tl
}

// rtlmForm — форма группы name в профиле Mihomo окружения: use — группа берёт узлы
// провайдера, hidden — в proxies есть DIRECT или REJECT, tail — proxies оканчивается
// служебными DIRECT и REJECT.
func rtlmForm(t *testing.T, e *rtEnv, name string) (use, hidden, tail bool) {
	t.Helper()
	g, ok := rtmGroups(t, e.mihomoProfile(t))[name]
	if !ok {
		t.Fatalf("в профиле Mihomo нет группы теста")
	}
	use = len(rtmStrings(g["use"])) > 0
	p := rtmStrings(g["proxies"])
	hidden = slices.Contains(p, mihomoDirect) || slices.Contains(p, mihomoReject)
	tail = len(p) >= 2 && p[len(p)-2] == mihomoDirect && p[len(p)-1] == mihomoReject
	return use, hidden, tail
}

// rtlmMihomo — изолированный процесс Mihomo теста и профиль, с которым он запущен.
// Applier в окружении не задан: сверка хвоста пишет профиль панели на диск, а
// процесс теста перечитывает его только перезапуском (sync).
type rtlmMihomo struct {
	e      *rtEnv
	core   *rtMihomoCore
	loaded []byte
}

// profile — профиль панели окружения на диске.
func (m *rtlmMihomo) profile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(m.e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		t.Fatalf("профиль панели: %v", err)
	}
	return data
}

// start (пере)запускает процесс на текущем профиле окружения тем же сбором
// config.yaml, что rtmtStart (процесс принадлежит тесту, создавшему ядро, и
// переживает подтест), и сверяет выбор групп процесса с Live, как наблюдатель
// после запуска ядра.
func (m *rtlmMihomo) start(t *testing.T) {
	t.Helper()
	data := m.profile(t)
	m.core.stop()
	m.core.start(t, m.e)
	m.loaded = data
	m.e.rtsReconcile(t, context.Background())
}

// sync перезапускает процесс, если профиль окружения изменился после запуска;
// true — перезапущен.
func (m *rtlmMihomo) sync(t *testing.T) bool {
	t.Helper()
	if bytes.Equal(m.profile(t), m.loaded) {
		return false
	}
	m.start(t)
	return true
}

// rtlmSaw делает запросы на домен через socks процесса Mihomo, пока домен не
// появится в журнале узла (до 20 с). Запрос может упасть на DNS узла — важен
// только журнал узла.
func rtlmSaw(port int, accessLog, domain string) bool {
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); {
		_, _ = rtSocksGet(port, "http://"+domain+"/", 5*time.Second)
		if rtWaitNodeSaw(accessLog, domain, 2*time.Second) {
			return true
		}
	}
	return false
}

// rtlmHotSync переносит в изолированный процесс Xray тело группы, которое выбор
// арбитра записал в xcp-groups окружения: горячая замена (rmo→ado) тем же xrayHot,
// что у конвейера при запущенном ядре, затем файл копируется в каталог процесса.
// Файл не менялся — ничего не делает.
func rtlmHotSync(t *testing.T, ctx context.Context, e *rtEnv, dir, step string) {
	t.Helper()
	path := filepath.Join(dir, xrayGroupsFile)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := os.ReadFile(filepath.Join(e.xrayDir(), xrayGroupsFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(old, next) {
		return
	}
	files := []configlayer.HotFile{{RelPath: xrayGroupsFile, Action: configlayer.ActionWrite, Old: old, OldExisted: true, New: next}}
	if err := e.svc.xrayHot(ctx, files); err != nil {
		code := "other"
		var hotErr *configlayer.XrayHotError
		if errors.As(err, &hotErr) {
			code = hotErr.Reason
		}
		t.Fatalf("%s: горячая замена тела группы: код %s", step, code)
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rtlmWiring — служба лимитов и арбитр, проводка как в cmd/xcp/main.go: цели учёта —
// маршрутизация окружения, источник удержаний арбитра — служба лимитов, смена
// удержаний будит арбитр. kernel — активное ядро для службы лимитов.
func rtlmWiring(t *testing.T, e *rtEnv, kernel *atomic.Value) (*services.LimitsService, *Arbiter) {
	t.Helper()
	limits := services.NewLimitsService(rtWorkSubdir(t, "rt-lim-"))
	limits.SetTargets(e.svc.TrafficTargets)
	limits.SetActiveKernel(func() string { s, _ := kernel.Load().(string); return s })
	arb := NewArbiter(e.svc)
	arb.SetHoldSource(limits)
	limits.SetOnHoldsChanged(arb.Kick)
	limits.Start()
	t.Cleanup(limits.Stop)
	arb.Start()
	t.Cleanup(arb.Stop)
	return limits, arb
}

// rtlmStream запускает службу потоков на сокете процесса Mihomo с хуком дельт
// службы лимитов и ждёт подключения потока /connections.
func rtlmStream(t *testing.T, core *rtMihomoCore, limits *services.LimitsService) *services.TrafficQuotaService {
	t.Helper()
	traffic := services.NewTrafficQuotaService(rtWorkSubdir(t, "rt-tq-"), "", core.svc)
	traffic.SetKernelAliveCheck(func() bool { return true })
	traffic.SetConnDeltaHook(limits.ObserveMihomo)
	traffic.Start()
	t.Cleanup(traffic.Stop)
	rtlmWaitStream(t, traffic, "подготовка")
	return traffic
}

// rtlmWaitStream ждёт подключения потока /connections (после запуска или
// перезапуска процесса: повтор потока идёт с паузой 5 с).
func rtlmWaitStream(t *testing.T, traffic *services.TrafficQuotaService, step string) {
	t.Helper()
	if !rtlmUntil(30*time.Second, func() bool { return traffic.StreamState() == services.StreamStateOK }) {
		t.Fatalf("%s: поток /connections не подключился за 30 с: состояние %s", step, traffic.StreamState())
	}
}

func TestRouterLimitsMihomo(t *testing.T) {
	routertest.Slow(t, "сверка служебного хвоста — запуск конвейера Mihomo на каждую правку лимита block, перезапуск изолированного Mihomo и медленные ответы цели для снимков /connections")
	rtNeedBins(t, rtXrayBin, rtMihomoBin)
	ctx := context.Background()
	target, _ := rtlmTarget(t)
	// Цель правила DIRECT — отдельный порт: правило по порту отправляет её мимо группы.
	direct, _ := rtlmTarget(t)
	du, err := url.Parse(direct.URL)
	if err != nil {
		t.Fatal(err)
	}
	blobURL := target.URL + rtlmBlobPath + "?slow=1"
	directURL := direct.URL + rtlmBlobPath + "?slow=1"

	portA, logA := rtStartNode(t)
	portB, logB := rtStartNode(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core))
	e.writeUserProfile(t)
	e.addMyRaw(t,
		"socks5://127.0.0.1:"+strconv.Itoa(portA)+"#rtlm-a",
		"socks5://127.0.0.1:"+strconv.Itoa(portB)+"#rtlm-b")
	cat := e.catalog(t).Mihomo
	nodeA, nodeB := rtsNode(t, cat, "rtlm-a"), rtsNode(t, cat, "rtlm-b")
	memberA, memberB := memberNode+nodeA.Ref, memberNode+nodeB.Ref

	_, g, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Лимит", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondPort, Values: []string{du.Port()}}},
	}); err != nil {
		t.Fatalf("SaveRule (порт → DIRECT): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: g.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)
	if _, err := e.svc.Select(ctx, g.ID, memberA); err != nil {
		t.Fatalf("исходный выбор: %v", err)
	}

	// Гибрид D-13: группа только из провайдера без лимита пишется через use без
	// служебного хвоста.
	if use, hidden, _ := rtlmForm(t, e, g.Name); !use || hidden {
		t.Fatalf("подготовка: G не в форме «только провайдер»: use=%s служебные=%s", rtxYesNo(use), rtxYesNo(hidden))
	}

	proc := &rtlmMihomo{e: e, core: core}
	proc.start(t)
	rtsWaitNow(t, core, g.Name, nodeA.MihomoName, 20*time.Second)

	var kernel atomic.Value
	kernel.Store(configlayer.KernelMihomo)
	limits, _ := rtlmWiring(t, e, &kernel)
	traffic := rtlmStream(t, core, limits)
	fetch := func() { _, _ = rtlmFetch(core.port, blobURL, 15*time.Second) }

	// Трафик группы растит расход её лимита и общего, трафик правила DIRECT — нет.
	t.Run("counts_group_not_direct", func(t *testing.T) {
		lg := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm группа", TargetType: services.LimitTargetGroup,
			TargetID: g.ID, LimitBytes: rtlmBigLimit, Action: services.LimitActionNotify})
		la := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm весь", TargetType: services.LimitTargetAll,
			LimitBytes: rtlmBigLimit, Action: services.LimitActionNotify})

		n, err := rtlmFetch(core.port, directURL, 15*time.Second)
		if err != nil || n != rtlmBlobSize {
			t.Fatalf("подготовка: запрос по правилу DIRECT: прошёл=%s байт=%d", rtxYesNo(err == nil), n)
		}
		time.Sleep(3 * time.Second)
		// Группе — ровно ноль; весь трафик — меньше четверти ответа: служебные
		// соединения самого ядра не должны выдать себя за ответ правила DIRECT.
		if ug, ua := rtlmUsed(limits, lg.ID), rtlmUsed(limits, la.ID); ug != 0 || ua >= rtlmBlobSize/4 {
			t.Errorf("трафик правила DIRECT посчитан: группа %d байт, весь трафик %d байт", ug, ua)
		}

		n, err = rtlmFetch(core.port, blobURL, 15*time.Second)
		if err != nil || n != rtlmBlobSize {
			t.Fatalf("подготовка: запрос через группу: прошёл=%s байт=%d", rtxYesNo(err == nil), n)
		}
		if !rtlmUntil(10*time.Second, func() bool { return rtlmUsed(limits, lg.ID) > 0 && rtlmUsed(limits, la.ID) > 0 }) {
			t.Errorf("трафик группы не посчитан за 10 с: группа %d байт, весь трафик %d байт, данные %s",
				rtlmUsed(limits, lg.ID), rtlmUsed(limits, la.ID), limits.List().Data)
		}
		t.Logf("расход за ответ %d байт: группа %d, весь трафик %d", rtlmBlobSize, rtlmUsed(limits, lg.ID), rtlmUsed(limits, la.ID))
	})

	// Гибрид D-13 и удержание квотой (D-05): лимит block, сохранённый без «Применить»,
	// сам пересобирает профиль (служебный хвост и копии узлов, ревизия черновика та
	// же); исчерпание ставит группу на REJECT, клик отклоняется, сброс возвращает
	// исходный выбор; удаление лимита возвращает use без хвоста.
	t.Run("block_and_reset", func(t *testing.T) {
		rev := e.rev(t)
		lb := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm блок", TargetType: services.LimitTargetGroup,
			TargetID: g.ID, LimitBytes: rtlmLimitBytes, Action: services.LimitActionBlock})
		if !rtlmUntil(20*time.Second, func() bool {
			if !rtlmTail(t, e).has(g.ID) {
				return false
			}
			use, _, tail := rtlmForm(t, e, g.Name)
			return !use && tail
		}) {
			use, _, tail := rtlmForm(t, e, g.Name)
			t.Fatalf("сверка хвоста за 20 с: hidden_tail с группой=%s, use=%s, хвост=%s",
				rtxYesNo(rtlmTail(t, e).has(g.ID)), rtxYesNo(use), rtxYesNo(tail))
		}
		if got := e.rev(t); got != rev {
			t.Errorf("сверка хвоста сменила ревизию черновика: %d → %d", rev, got)
		}
		if !proc.sync(t) {
			t.Error("профиль Mihomo после сверки хвоста не изменился")
		}
		rtlmWaitStream(t, traffic, "перезапуск")

		if !rtlmTraffic(40*time.Second, fetch, func() bool { return rtlmSel(t, e, g.ID) == memberReject }) {
			t.Fatalf("исчерпание: выбор группы не REJECT за 40 с: расход %d байт, держатель %q",
				rtlmUsed(limits, lb.ID), rtlmHolder(t, e, g.ID))
		}
		if h := rtlmHolder(t, e, g.ID); h != holderQuota {
			t.Errorf("исчерпание: держатель %q, ожидался %q", h, holderQuota)
		}
		if !rtlmUntil(10*time.Second, func() bool {
			now, err := core.svc.ProxyNow(ctx, g.Name)
			return err == nil && now == mihomoReject
		}) {
			t.Error("исчерпание: процесс Mihomo не на REJECT")
		}
		const blocked = "rtlm-block.example.test"
		_, errBlocked := rtSocksGet(core.port, "http://"+blocked+"/", 4*time.Second)
		time.Sleep(2 * time.Second)
		if errBlocked == nil || rtNodeSaw(logA, blocked) || rtNodeSaw(logB, blocked) {
			t.Errorf("исчерпание: запрос прошёл=%s, узел A видел=%s, узел B видел=%s",
				rtxYesNo(errBlocked == nil), rtxYesNo(rtNodeSaw(logA, blocked)), rtxYesNo(rtNodeSaw(logB, blocked)))
		}
		if _, err := e.svc.Select(ctx, g.ID, memberB); !errors.Is(err, ErrQuotaHeld) {
			t.Errorf("клик при квоте: ожидался ErrQuotaHeld, ошибка=%t", err != nil)
		}

		// Сброс будит арбитр колбэком службы лимитов — без ожидания тика.
		if err := limits.Reset(lb.ID); err != nil {
			t.Fatalf("сброс: %s", rtlmErrCode(err))
		}
		if !rtlmUntil(10*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberA }) {
			t.Fatalf("сброс: исходный выбор не вернулся за 10 с (выбор REJECT=%s)", rtxYesNo(rtlmSel(t, e, g.ID) == memberReject))
		}
		if !rtlmSaw(core.port, logA, "rtlm-reset.example.test") {
			t.Error("сброс: запрос группы не дошёл до исходного узла")
		}

		if err := limits.Delete(lb.ID); err != nil {
			t.Fatalf("удаление: %s", rtlmErrCode(err))
		}
		if !rtlmUntil(20*time.Second, func() bool {
			if rtlmTail(t, e).has(g.ID) {
				return false
			}
			use, hidden, _ := rtlmForm(t, e, g.Name)
			return use && !hidden
		}) {
			use, hidden, _ := rtlmForm(t, e, g.Name)
			t.Errorf("удаление лимита за 20 с: hidden_tail с группой=%s, use=%s, служебные=%s",
				rtxYesNo(rtlmTail(t, e).has(g.ID)), rtxYesNo(use), rtxYesNo(hidden))
		}
		if got := e.rev(t); got != rev {
			t.Errorf("снятие хвоста сменило ревизию черновика: %d → %d", rev, got)
		}
	})

	// Квота старше расписания (D-05, D-06): при действующем окне и исчерпанном лимите
	// группа на REJECT, после сброса — узел окна.
	t.Run("quota_over_schedule", func(t *testing.T) {
		lq := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm окно", TargetType: services.LimitTargetGroup,
			TargetID: g.ID, LimitBytes: rtlmLimitBytes, Action: services.LimitActionBlock})
		if !rtlmUntil(20*time.Second, func() bool { _, _, tail := rtlmForm(t, e, g.Name); return rtlmTail(t, e).has(g.ID) && tail }) {
			t.Fatal("сверка хвоста за 20 с не прошла")
		}
		if proc.sync(t) {
			rtlmWaitStream(t, traffic, "перезапуск")
		}
		// Окно по часам процесса: с минуты назад на полчаса вперёд, все дни (через
		// полночь допустимо, D-08).
		now := time.Now()
		w, err := e.svc.SaveScheduleWindow(g.ID, ScheduleWindow{Days: rtlmAllDays,
			From: now.Add(-time.Minute).Format("15:04"), To: now.Add(30 * time.Minute).Format("15:04"),
			Member: memberB, Enabled: true})
		if err != nil {
			t.Fatalf("окно: %v", err)
		}
		t.Cleanup(func() { _ = e.svc.DeleteScheduleWindow(g.ID, w.ID) })
		if !rtlmUntil(15*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberB }) {
			t.Fatalf("окно: узел окна не поставлен за 15 с, держатель %q", rtlmHolder(t, e, g.ID))
		}
		if !rtlmTraffic(40*time.Second, fetch, func() bool { return rtlmSel(t, e, g.ID) == memberReject }) {
			t.Fatalf("исчерпание в окне: выбор группы не REJECT за 40 с: расход %d байт", rtlmUsed(limits, lq.ID))
		}
		if h := rtlmHolder(t, e, g.ID); h != holderQuota {
			t.Errorf("исчерпание в окне: держатель %q, ожидался %q", h, holderQuota)
		}
		if err := limits.Reset(lq.ID); err != nil {
			t.Fatalf("сброс: %s", rtlmErrCode(err))
		}
		if !rtlmUntil(10*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberB }) {
			t.Fatalf("сброс в окне: узел окна не вернулся за 10 с, исходный A=%s", rtxYesNo(rtlmSel(t, e, g.ID) == memberA))
		}
		if h := rtlmHolder(t, e, g.ID); h != holderSchedule {
			t.Errorf("сброс в окне: держатель %q, ожидался %q", h, holderSchedule)
		}
		if !rtlmSaw(core.port, logB, "rtlm-window.example.test") {
			t.Error("сброс в окне: запрос группы не дошёл до узла окна")
		}
		// Уборка до удаления лимита: окно снимается при хвосте в профиле процесса.
		if err := e.svc.DeleteScheduleWindow(g.ID, w.ID); err != nil {
			t.Fatalf("удаление окна: %v", err)
		}
		if !rtlmUntil(15*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberA && rtlmHolder(t, e, g.ID) == "" }) {
			t.Errorf("после окна: исходный выбор не вернулся за 15 с")
		}
	})

	// D-13, D-14: лимит block на «авто» отклоняется; общий лимит block держит все
	// ручные группы и не трогает «авто».
	t.Run("auto_and_global", func(t *testing.T) {
		auto, ok := rtGroupByName(e.groups(t), "Авто")
		if !ok {
			t.Fatal("нет группы «Авто»")
		}
		px, ok := rtGroupByName(e.groups(t), "Прокси")
		if !ok {
			t.Fatal("нет группы «Прокси»")
		}
		_, err := limits.Save(services.TrafficLimit{Name: "rtlm авто", TargetType: services.LimitTargetGroup,
			TargetID: auto.ID, LimitBytes: rtlmLimitBytes, Period: "monthly", Action: services.LimitActionBlock, Enabled: true})
		var in *services.LimitInputError
		if !errors.As(err, &in) || in.Reason != "action_invalid" {
			t.Errorf("лимит block на «авто»: отказ %s, ожидался action:action_invalid", rtlmErrCode(err))
		}

		autoBefore := rtlmSel(t, e, auto.ID)
		la := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm весь блок", TargetType: services.LimitTargetAll,
			LimitBytes: rtlmLimitBytes, Action: services.LimitActionBlock})
		if !rtlmUntil(20*time.Second, func() bool { _, _, tail := rtlmForm(t, e, g.Name); return rtlmTail(t, e).All && tail }) {
			t.Fatal("сверка хвоста общего лимита за 20 с не прошла")
		}
		if proc.sync(t) {
			rtlmWaitStream(t, traffic, "перезапуск")
		}
		if !rtlmTraffic(40*time.Second, fetch, func() bool {
			return rtlmSel(t, e, g.ID) == memberReject && rtlmSel(t, e, px.ID) == memberReject
		}) {
			t.Fatalf("общий лимит: ручные группы не на REJECT за 40 с: G=%s «Прокси»=%s, расход %d байт",
				rtxYesNo(rtlmSel(t, e, g.ID) == memberReject), rtxYesNo(rtlmSel(t, e, px.ID) == memberReject), rtlmUsed(limits, la.ID))
		}
		if got := rtlmSel(t, e, auto.ID); got != autoBefore || rtlmHolder(t, e, auto.ID) != "" {
			t.Errorf("общий лимит тронул «авто»: выбор изменился=%t, запись арбитра=%q", got != autoBefore, rtlmHolder(t, e, auto.ID))
		}
		if err := limits.Reset(la.ID); err != nil {
			t.Fatalf("сброс: %s", rtlmErrCode(err))
		}
		if !rtlmUntil(15*time.Second, func() bool {
			return rtlmSel(t, e, g.ID) == memberA && rtlmSel(t, e, px.ID) != memberReject &&
				rtlmHolder(t, e, g.ID) == "" && rtlmHolder(t, e, px.ID) == ""
		}) {
			t.Errorf("сброс общего лимита: группы не вернулись за 15 с: G исходный=%s, «Прокси» на REJECT=%s",
				rtxYesNo(rtlmSel(t, e, g.ID) == memberA), rtxYesNo(rtlmSel(t, e, px.ID) == memberReject))
		}
	})
}

func TestRouterLimitsXray(t *testing.T) {
	routertest.Slow(t, "опрос счётчиков Xray раз в 10 с (базовая линия и дельта на каждом шаге), сверка хвоста Mihomo на правки лимитов и запуск Mihomo для смены ядра")
	rtNeedBins(t, rtXrayBin, rtMihomoBin)
	ctx := context.Background()
	target, targetHits := rtlmTarget(t)
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	blobURL := target.URL + rtlmBlobPath
	proxyA, hitsA := rtxaConnectProxy(t, u.Host)
	version := rtxaRun(t, "version")
	apiPort := rtFreePort(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithMihomoCore(core), rtWithDeps(func(d *Deps) {
		d.XrayAPIPort = func() int { return apiPort }
		d.XrayBin = func() string { return rtXrayBin }
		d.KernelVersion = func(string) string { return version }
	}))
	e.writeUserProfile(t)
	e.addMyRaw(t, proxyA+"#rtlm-a")
	memberA := memberNode + rtsNode(t, e.catalog(t).Xray, "rtlm-a").Ref
	_, g, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Лимит", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	// Только группа теста: файл сценария Xray заменяет routing панели целиком.
	e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{g} })
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: g.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)
	if _, err := e.svc.Select(ctx, g.ID, memberA); err != nil {
		t.Fatalf("исходный выбор: %v", err)
	}

	// Каталог процесса — копия файлов панели и файл сценария: socks-вход в группу и
	// policy счётчиков исходящих (152-SPIKE: xray_stats_need_policy: yes).
	dir := rtxaCopyDir(t, e.xrayDir())
	inPort := rtFreePort(t)
	extra, err := json.Marshal(map[string]any{
		"inbounds": []any{map[string]any{"tag": "rt-lim-in", "listen": "127.0.0.1", "port": inPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
		"routing":  map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"rt-lim-in"}, "outboundTag": GroupTag(g.ID)}}},
		"policy":   map[string]any{"system": map[string]any{"statsOutboundUplink": true, "statsOutboundDownlink": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "99_zz_rt_lim.json"), extra, 0o600); err != nil {
		t.Fatal(err)
	}
	proc := rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), apiPort)
	pid := proc.cmd.Process.Pid
	fetchX := func() { _, _ = rtlmFetch(inPort, blobURL, 5*time.Second) }
	// Счётчик тега группы создаётся ядром на первом трафике: без него первый опрос
	// службы лимитов не снимет базовую линию.
	if n, err := rtlmFetch(inPort, blobURL, 5*time.Second); err != nil || n != rtlmBlobSize {
		t.Fatalf("подготовка: запрос через группу: прошёл=%s байт=%d", rtxYesNo(err == nil), n)
	}

	var kernel atomic.Value
	kernel.Store(configlayer.KernelXray)
	limits, _ := rtlmWiring(t, e, &kernel)
	grpcSvc := services.NewXrayGRPCService(net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)))
	grpcSvc.Start()
	t.Cleanup(grpcSvc.Stop)
	limits.SetXrayCounters(grpcSvc)
	lg := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm счёт", TargetType: services.LimitTargetGroup,
		TargetID: g.ID, LimitBytes: rtlmBigLimit, Action: services.LimitActionNotify})

	// sameProc — горячая замена не перезапускала процесс Xray.
	sameProc := func(t *testing.T, step string) {
		t.Helper()
		if !proc.alive() || proc.cmd.Process.Pid != pid {
			t.Errorf("%s: процесс Xray перезапущен или остановлен", step)
		}
	}
	// viaNode — запрос через группу проходит через узел A.
	viaNode := func(t *testing.T, step string) {
		t.Helper()
		a0, d0 := hitsA.Load(), targetHits.Load()
		n, err := rtlmFetch(inPort, blobURL, 5*time.Second)
		if err != nil || n != rtlmBlobSize || hitsA.Load() != a0+1 || targetHits.Load() != d0+1 {
			t.Errorf("%s: через узел: прошёл=%s, узел %d, цель %d", step, rtxYesNo(err == nil), hitsA.Load()-a0, targetHits.Load()-d0)
		}
	}
	// held исчерпывает лимит l и ждёт служебный выбор want; затем переносит тело
	// группы в процесс.
	held := func(t *testing.T, l services.TrafficLimit, want, step string) {
		t.Helper()
		if !rtlmTraffic(40*time.Second, fetchX, func() bool { return rtlmSel(t, e, g.ID) == want }) {
			t.Fatalf("%s: выбор группы не поставлен за 40 с: расход %d байт, данные %s, держатель %q",
				step, rtlmUsed(limits, l.ID), limits.List().Data, rtlmHolder(t, e, g.ID))
		}
		rtlmHotSync(t, ctx, e, dir, step)
		sameProc(t, step)
	}
	// release сбрасывает лимит l, ждёт возврата узла A, переносит тело группы в
	// процесс и удаляет лимит.
	release := func(t *testing.T, l services.TrafficLimit, step string) {
		t.Helper()
		if err := limits.Reset(l.ID); err != nil {
			t.Fatalf("%s: сброс: %s", step, rtlmErrCode(err))
		}
		if !rtlmUntil(10*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberA }) {
			t.Fatalf("%s: сброс: узел не вернулся за 10 с", step)
		}
		rtlmHotSync(t, ctx, e, dir, step+": сброс")
		sameProc(t, step+": сброс")
		viaNode(t, step+": сброс")
		if err := limits.Delete(l.ID); err != nil {
			t.Fatalf("%s: удаление: %s", step, rtlmErrCode(err))
		}
		if !rtlmUntil(10*time.Second, func() bool { return rtlmSel(t, e, g.ID) == memberA && rtlmHolder(t, e, g.ID) == "" }) {
			t.Errorf("%s: после удаления лимита группа не на узле", step)
		}
		rtlmHotSync(t, ctx, e, dir, step+": удаление")
	}

	// Счётчики StatsService изолированного Xray растят расход группы (D-02).
	t.Run("counts_by_stats", func(t *testing.T) {
		u0 := rtlmUsed(limits, lg.ID)
		if !rtlmTraffic(30*time.Second, fetchX, func() bool { return rtlmUsed(limits, lg.ID) > u0 }) {
			t.Fatalf("расход группы не вырос за 30 с: данные %s", limits.List().Data)
		}
		t.Logf("расход группы по счётчикам Xray: %d байт", rtlmUsed(limits, lg.ID)-u0)
	})

	// Лимит direct: тело freedom горячей заменой без перезапуска процесса, трафик
	// мимо узла до цели; сброс возвращает узел (D-13, D-18).
	t.Run("direct_by_hot_swap", func(t *testing.T) {
		ld := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm напрямую", TargetType: services.LimitTargetGroup,
			TargetID: g.ID, LimitBytes: rtlmLimitBytes, Action: services.LimitActionDirect})
		held(t, ld, memberDirect, "напрямую")
		a0, d0 := hitsA.Load(), targetHits.Load()
		n, err := rtlmFetch(inPort, blobURL, 5*time.Second)
		if err != nil || n != rtlmBlobSize || targetHits.Load() != d0+1 {
			t.Errorf("напрямую: запрос не дошёл до цели: прошёл=%s, цель %d", rtxYesNo(err == nil), targetHits.Load()-d0)
		}
		if hitsA.Load() != a0 {
			t.Errorf("напрямую: запрос ушёл в узел (%d)", hitsA.Load()-a0)
		}
		release(t, ld, "напрямую")
	})

	// Лимит block: тело blackhole горячей заменой — запрос не доходит ни до цели, ни
	// до узла (регрессия X1 spike для выбранного механизма).
	t.Run("block_by_hot_swap", func(t *testing.T) {
		lb := rtlmSave(t, limits, services.TrafficLimit{Name: "rtlm блок", TargetType: services.LimitTargetGroup,
			TargetID: g.ID, LimitBytes: rtlmLimitBytes, Action: services.LimitActionBlock})
		held(t, lb, memberReject, "блок")
		a0, d0 := hitsA.Load(), targetHits.Load()
		_, err := rtlmFetch(inPort, blobURL, 3*time.Second)
		if err == nil {
			t.Error("блок: запрос через группу прошёл")
		}
		if hitsA.Load() != a0 || targetHits.Load() != d0 {
			t.Errorf("блок: утечка: узел %d, цель %d", hitsA.Load()-a0, targetHits.Load()-d0)
		}
		release(t, lb, "блок")
	})

	// Счётчик продолжается при смене активного ядра (D-03): тот же экземпляр службы
	// лимитов считает на Mihomo, затем на Xray; расход только растёт.
	t.Run("kernel_switch_continues", func(t *testing.T) {
		// Лимитов block/direct больше нет: профиль Mihomo без служебного хвоста.
		if !rtlmUntil(30*time.Second, func() bool { tl := rtlmTail(t, e); return !tl.All && len(tl.Groups) == 0 }) {
			t.Fatal("подготовка: служебный хвост не снят за 30 с")
		}
		kernel.Store(configlayer.KernelMihomo)
		m := &rtlmMihomo{e: e, core: core}
		m.start(t)
		rtlmStream(t, core, limits)
		u0 := rtlmUsed(limits, lg.ID)
		slowURL := blobURL + "?slow=1"
		if !rtlmTraffic(30*time.Second, func() { _, _ = rtlmFetch(core.port, slowURL, 15*time.Second) },
			func() bool { return rtlmUsed(limits, lg.ID) > u0 }) {
			t.Fatalf("Mihomo: расход группы не вырос за 30 с: данные %s", limits.List().Data)
		}
		u1 := rtlmUsed(limits, lg.ID)
		kernel.Store(configlayer.KernelXray)
		dropped := false
		if !rtlmTraffic(40*time.Second, fetchX, func() bool {
			cur := rtlmUsed(limits, lg.ID)
			dropped = dropped || cur < u1
			return cur > u1
		}) {
			t.Errorf("Xray после смены ядра: расход не вырос за 40 с: данные %s", limits.List().Data)
		}
		if dropped {
			t.Error("расход уменьшился после смены активного ядра")
		}
		t.Logf("расход: до Mihomo %d, после Mihomo %d, после Xray %d байт", u0, u1, rtlmUsed(limits, lg.ID))
	})
}
