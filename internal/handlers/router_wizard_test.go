//go:build router

// router-area: configlayer

package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// API мастера первого запуска на настоящей панели устройства (фаза 149.1):
// GET /api/configlayer/wizard и POST …/close, …/hide, …/reset. Вердикт — ответы
// панели и процессы ядер устройства. Режим разработчика, признак мастера и
// черновик возвращаются прежними. Проверки статуса после сброса идут
// при удержанном открытом черновике: шаг «применение» тогда не выполнен, и
// наблюдатель службы (период tick_s ответа) не ставит done между сбросом и
// проверкой, какими бы ни были шаги 1–6 стенда. Сроки ожидания отметки и
// обновления вида берутся у панели (tick_s), а не константой. В сообщениях
// провалов — только коды, статусы и счётчики, без адресов и имён узлов.

const (
	rtWizardBase = "/api/configlayer/wizard"
	// rtWizardDeadSub — подписка на loopback без сервера: защита от SSRF не даёт
	// скачать набор узлов, источник есть, узлов нет.
	rtWizardDeadSub = "http://127.0.0.1:9/xcp-wizard-check"
	// rtWizardPostMax — предел ответа POST close|hide|reset: панель ждёт записи
	// признака не дольше 1 с (wizardWriteWait службы мастера), дальше отвечает
	// статусом с учётом этой записи; плюс запас на сеть и процессор стенда. Предел
	// общий для всех POST: и POST без смены признака идёт через очередь записей и
	// при занятом замке хранилища отвечает по истечении того же ожидания.
	rtWizardPostMax = 2 * time.Second
	// rtWizardGetMax — предел ответа GET мастера: панель ждёт наблюдения (факты
	// слоя, ядра, секции Xray) не дольше 1 с (viewWait службы мастера), дальше
	// отдаёт последнее наблюдение; плюс запас.
	rtWizardGetMax = 2 * time.Second
)

// rtWizardStepIDs — шаги мастера в порядке ответа.
var rtWizardStepIDs = []string{"kernel", "sections", "nodes", "routing", "apply", "start"}

// rtWizardRoutes — маршруты мастера и их методы.
var rtWizardRoutes = []struct{ method, path string }{
	{http.MethodGet, rtWizardBase},
	{http.MethodPost, rtWizardBase + "/close"},
	{http.MethodPost, rtWizardBase + "/hide"},
	{http.MethodPost, rtWizardBase + "/reset"},
}

// rtWizardView читает статус мастера; ответ не 200 — провал.
func rtWizardView(e *rtSrc) map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtWizardBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtWizardBase, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtWizardTick — период проверки наблюдателем из поля tick_s вида; нет поля или
// оно не больше нуля — провал.
func rtWizardTick(t *testing.T, v map[string]any) time.Duration {
	t.Helper()
	tick := rtWizardNum(v, "tick_s")
	if tick <= 0 {
		t.Fatalf("ответ мастера без tick_s (%v)", v["tick_s"])
	}
	return time.Duration(tick) * time.Second
}

// rtWizardViewUntil опрашивает GET мастера, пока cond не выполнится, не дольше
// tick_s + 10 с (tick_s — из первого прочитанного вида), шаг 500 мс. GET, не
// дождавшийся наблюдения за секунду, отдаёт прежнее наблюдение; наблюдатель
// публикует отличие не позже tick_s, поэтому вид после правки черновика
// опрашивается, а не читается один раз. Не дождался — провал с what и последним
// наблюдением cond. Возвращает вид, на котором cond выполнилось.
func rtWizardViewUntil(e *rtSrc, what string, cond func(v map[string]any) (bool, string)) map[string]any {
	e.t.Helper()
	v := rtWizardView(e)
	limit := rtWizardTick(e.t, v) + 10*time.Second
	if ok, _ := cond(v); ok {
		return v
	}
	ok, seen := routertest.Poll(limit, 500*time.Millisecond, func() (bool, string) {
		v = rtWizardView(e)
		return cond(v)
	})
	if !ok {
		e.t.Fatalf("%s: за %v не дождались вида мастера: %s", what, limit, seen)
	}
	return v
}

// rtWizardStep — шаг ответа по id; нет шага — провал.
func rtWizardStep(t *testing.T, v map[string]any, id string) map[string]any {
	t.Helper()
	steps, _ := v["steps"].([]any)
	for _, s := range steps {
		st, _ := s.(map[string]any)
		if st["id"] == id {
			return st
		}
	}
	t.Fatalf("в ответе мастера нет шага %q", id)
	return nil
}

// rtWizardNum — число поля ответа (JSON-числа приходят float64).
func rtWizardNum(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

// rtWizardDone — признаки done всех шагов по id.
func rtWizardDone(t *testing.T, v map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, id := range rtWizardStepIDs {
		out[id], _ = rtWizardStep(t, v, id)["done"].(bool)
	}
	return out
}

// rtWizardStatus — статус мастера из ответа.
func rtWizardStatus(v map[string]any) string {
	s, _ := v["status"].(string)
	return s
}

// rtSetDevMode ставит режим разработчика и возвращает прежнее значение.
func rtSetDevMode(e *rtSrc, on bool) bool {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/settings", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/settings: %d", r.Status)
	}
	prev, _ := r.object(e.t)["dev_mode"].(bool)
	if prev == on {
		return prev
	}
	if s := e.call(http.MethodPost, "/api/settings/dev-mode", map[string]bool{"enabled": on}); s.Status != http.StatusOK {
		e.t.Fatalf("режим разработчика %v: %d", on, s.Status)
	}
	return prev
}

// rtDevMode ставит режим разработчика; прежнее значение возвращается в t.Cleanup.
func rtDevMode(e *rtSrc, on bool) {
	e.t.Helper()
	prev := rtSetDevMode(e, on)
	e.t.Cleanup(func() { rtSetDevMode(e, prev) })
}

// rtWizardPortRe — порт сервера в ссылке узла.
var rtWizardPortRe = regexp.MustCompile(`(@[^:/?#]+:)\d+`)

// rtWizardHold держит черновик открытым: добавляет в «Мои узлы» узел из testdata
// (порт свой на каждый вызов — узел не совпадает с уже добавленными) и проверяет,
// что шаг «применение» не выполнен. Возвращает сброс черновика.
func rtWizardHold(e *rtSrc) (release func()) {
	e.t.Helper()
	port := 20000 + int(time.Now().UnixNano()%40000)
	link := rtWizardPortRe.ReplaceAllString(rtLinkText(e.t, "vless_ws"), "${1}"+strconv.Itoa(port))
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link})
	if pv.Status != http.StatusOK {
		e.t.Fatalf("удержание черновика, preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(e.t)["token"].(string)
	if token == "" {
		e.t.Fatal("удержание черновика: preview без токена")
	}
	add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token})
	if add.Status != http.StatusOK {
		e.t.Fatalf("удержание черновика, add: %d %s", add.Status, add.Code)
	}
	rtWizardViewUntil(e, "черновик не удержан", func(v map[string]any) (bool, string) {
		apply := rtWizardStep(e.t, v, "apply")
		done, _ := apply["done"].(bool)
		changes := rtWizardNum(apply, "draft_changes")
		return !done && changes > 0, fmt.Sprintf("apply.done %v, draft_changes %d", apply["done"], changes)
	})
	return func() { e.resetDraft() }
}

// rtWizardReset снимает признак мастера (нужен режим разработчика). Сброс и
// отметка done наблюдателем пишутся по очереди под одним замком, а статус ответа
// сброса — результат этой записи, без отметки следующей проверки наблюдателя:
// он обязан быть пустым с первого сброса.
func rtWizardReset(e *rtSrc) map[string]any {
	e.t.Helper()
	v := rtWizardPost(e, "reset")
	if st := rtWizardStatus(v); st != "" {
		e.t.Fatalf("после сброса статус мастера %q, ожидался пустой", st)
	}
	return v
}

// rtWizardRestore возвращает признак мастера prev при удержанном черновике:
// сначала сброс (признак монотонен, hidden иначе не опустить до closed), затем
// closed — «Закрыть», hidden — «Скрыть»; пустой и done — только сброс (done
// наблюдатель поставит снова, если шаги 1–6 выполнены). Затем черновик
// отпускается.
func rtWizardRestore(e *rtSrc, prev string) {
	e.t.Helper()
	dev := rtSetDevMode(e, true)
	release := rtWizardHold(e)
	rtWizardReset(e)
	switch prev {
	case "closed":
		if r := e.call(http.MethodPost, rtWizardBase+"/close", nil); r.Status != http.StatusOK {
			e.t.Errorf("возврат статуса closed: %d %s", r.Status, r.Code)
		}
	case "hidden":
		if r := e.call(http.MethodPost, rtWizardBase+"/hide", nil); r.Status != http.StatusOK {
			e.t.Errorf("возврат статуса hidden: %d %s", r.Status, r.Code)
		}
	}
	release()
	rtSetDevMode(e, dev)
}

// rtWizardPost — POST-действие мастера; ответ не 200 — провал. Возвращает вид.
func rtWizardPost(e *rtSrc, action string) map[string]any {
	e.t.Helper()
	r := e.call(http.MethodPost, rtWizardBase+"/"+action, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST %s/%s: %d %s", rtWizardBase, action, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtWizardPostTimed — rtWizardPost с замером времени ответа.
func rtWizardPostTimed(e *rtSrc, action string) (map[string]any, time.Duration) {
	e.t.Helper()
	start := time.Now()
	r := e.call(http.MethodPost, rtWizardBase+"/"+action, nil)
	took := time.Since(start)
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST %s/%s: %d %s", rtWizardBase, action, r.Status, r.Code)
	}
	return r.object(e.t), took
}

// rtWizardCheckPost — провал, если POST мастера ответил дольше rtWizardPostMax.
func rtWizardCheckPost(t *testing.T, action string, took time.Duration) {
	t.Helper()
	if took > rtWizardPostMax {
		t.Errorf("POST %s ответил за %v (предел %v): ответ POST мастера ждёт записи признака на носитель или замка хранилища (G-149.1-6)",
			action, took.Round(time.Millisecond), rtWizardPostMax)
	}
}

// rtWizardContractIssues сверяет вид мастера v с состоянием слоя
// (steps[apply].draft_changes) и, при установленном Xray, пометку «частично» с
// видом секций Xray; возвращает расхождения (пусто — сошлось). sectionsLog
// получает строку счётчиков секций Xray последней сверки.
//
// Частичное взятие Xray — по признаку «секция в черновике» (desired снимка) и
// блокировке вида, без владельца секции: ожидание верно при любом владении на
// стенде, в том числе при drift у взятой секции.
func rtWizardContractIssues(e *rtSrc, v map[string]any, sectionsLog *string) []string {
	e.t.Helper()
	var issues []string
	st := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if st.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", st.Status, st.Code)
	}
	state := st.object(e.t)
	if got, want := rtWizardNum(rtWizardStep(e.t, v, "apply"), "draft_changes"), rtWizardNum(state, "draft_changes"); got != want {
		issues = append(issues, fmt.Sprintf("steps[apply].draft_changes %d, в состоянии слоя %d", got, want))
	}

	kernels, _ := v["kernels"].(map[string]any)
	if xray, _ := kernels["xray"].(bool); !xray {
		return issues
	}
	desired := rtWizardXrayDesired(state)
	cv := e.call(http.MethodGet, "/api/configlayer/cores?kernel=xray", nil)
	if cv.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/cores?kernel=xray: %d %s", cv.Status, cv.Code)
	}
	inDraft, takeable, drift, releasing := rtWizardXrayCounts(desired, cv.object(e.t))
	*sectionsLog = fmt.Sprintf("xray-sections: в черновике %d, можно взять %d, drift %d, releasing %d", inDraft, takeable, drift, releasing)
	sections := rtWizardStep(e.t, v, "sections")
	takes, _ := sections["kernels"].(map[string]any)
	xt, _ := takes["xray"].(map[string]any)
	if got, _ := xt["done"].(bool); got != (inDraft >= 1) {
		issues = append(issues, fmt.Sprintf("sections.kernels.xray.done %v, секций в черновике %d", got, inDraft))
	}
	partial, has := sections["partial"].(map[string]any)
	switch {
	case inDraft >= 1 && takeable > 0:
		if !has || rtWizardNum(partial, "done") != inDraft || rtWizardNum(partial, "total") != inDraft+takeable {
			issues = append(issues, fmt.Sprintf("partial %v, ожидалось {done: %d, total: %d}", sections["partial"], inDraft, inDraft+takeable))
		}
	case has:
		issues = append(issues, fmt.Sprintf("partial %v при %d в черновике и %d доступных", partial, inDraft, takeable))
	}
	return issues
}

func rtWizardXrayDesired(state map[string]any) map[string]bool {
	coresSnap, _ := state["cores"].(map[string]any)
	desired := map[string]bool{}
	if xs, ok := coresSnap["xray"].(map[string]any); ok {
		for id, raw := range xs {
			sec, _ := raw.(map[string]any)
			d, _ := sec["desired"].(bool)
			desired[id] = d
		}
	}
	return desired
}

func rtWizardXrayCounts(desired map[string]bool, view map[string]any) (inDraft, takeable, drift, releasing int) {
	for _, d := range desired {
		if d {
			inDraft++
		}
	}
	secs, _ := view["sections"].([]any)
	for _, s := range secs {
		sec, _ := s.(map[string]any)
		id, _ := sec["id"].(string)
		switch sec["owner"] {
		case "drift":
			drift++
		case "releasing":
			releasing++
		}
		if blocked, _ := sec["blocked"].(string); blocked == "" && !desired[id] {
			takeable++
		}
	}
	return
}

// Статус мастера и шагов 1–6, защита маршрутов, узлы без скачанного набора,
// монотонность признака и отметка «пройдено» сервером без запросов клиента.
func TestRouterWizardAPI(t *testing.T) {
	// Шаг «запуск» и отметка «пройдено» зависят от запущенного ядра стенда.
	routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.freshDraft()

	start := rtWizardView(e)
	t.Logf("статус мастера стенда до теста: %q, шагов выполнено %d", rtWizardStatus(start), rtWizardNum(start, "done_count"))
	// Зарегистрирован раньше очисток подтестов внутри теста, поэтому идёт после них:
	// фактический статус после всех возвратов.
	t.Cleanup(func() {
		r := e.call(http.MethodGet, rtWizardBase, nil)
		if r.Status != http.StatusOK {
			t.Logf("статус мастера стенда после теста: GET %d %s", r.Status, r.Code)
			return
		}
		v := r.object(t)
		t.Logf("статус мастера стенда после теста: %q, шагов выполнено %d", rtWizardStatus(v), rtWizardNum(v, "done_count"))
	})

	t.Run("protected", func(t *testing.T) { rtWizardCheckProtected(e.with(t)) })

	t.Run("contract", func(t *testing.T) { rtWizardCheckContract(e.with(t)) })

	t.Run("nodes_not_downloaded", func(t *testing.T) { rtWizardCheckNodesNotDownloaded(e.with(t)) })

	t.Run("nodes_consistent", func(t *testing.T) { rtWizardCheckNodesConsistent(e.with(t)) })

	t.Run("status_monotonic", func(t *testing.T) { rtWizardCheckStatusMonotonic(e.with(t)) })

	t.Run("done_stamped_by_server", func(t *testing.T) { rtWizardCheckDoneStamped(e.with(t)) })
}

func rtWizardCheckProtected(e *rtSrc) {
	t := e.t
	t.Helper()
	for _, rt := range rtWizardRoutes {
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
		if rt.method == http.MethodPost {
			status, _, err = e.p.Request(rt.method, rt.path, body, routertest.AuthNoCSRF)
			if err != nil {
				t.Fatalf("%s %s без CSRF: %v", rt.method, rt.path, err)
			}
			if status != http.StatusForbidden {
				t.Errorf("%s %s без X-CSRF-Token: статус %d, ожидался 403", rt.method, rt.path, status)
			}
		}
		other := http.MethodGet
		if rt.method == http.MethodGet {
			other = http.MethodPost
		}
		if r := e.call(other, rt.path, rtBody(other)); r.Status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: статус %d, ожидался 405", other, rt.path, r.Status)
		}
	}
}

func rtWizardCheckContract(e *rtSrc) {
	t := e.t
	t.Helper()
	r := e.call(http.MethodGet, rtWizardBase, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rtWizardBase, r.Status, r.Code)
	}
	rtWizardCheckSecrets(t, r.Raw)
	v := r.object(t)
	if total := rtWizardNum(v, "total"); total != 6 {
		t.Errorf("total %d, ожидалось 6", total)
	}
	steps, _ := v["steps"].([]any)
	if len(steps) != len(rtWizardStepIDs) {
		t.Fatalf("шагов %d, ожидалось %d", len(steps), len(rtWizardStepIDs))
	}
	doneCount, current := 0, 0
	for i, s := range steps {
		st, _ := s.(map[string]any)
		if st["id"] != rtWizardStepIDs[i] {
			t.Errorf("шаг %d: id %v, ожидался %s", i+1, st["id"], rtWizardStepIDs[i])
		}
		if done, _ := st["done"].(bool); done {
			doneCount++
		} else if current == 0 {
			current = i + 1
		}
	}
	if got := rtWizardNum(v, "done_count"); got != doneCount {
		t.Errorf("done_count %d, выполнено шагов %d", got, doneCount)
	}
	if got := rtWizardNum(v, "current"); got != current {
		t.Errorf("current %d, первый невыполненный %d", got, current)
	}
	status := rtWizardStatus(v)
	if card, _ := v["card_visible"].(bool); card && (status == "done" || status == "hidden") {
		t.Errorf("card_visible при статусе %q", status)
	}

	// Сверки со состоянием слоя и видом секций Xray — в опросе: GET, не
	// дождавшийся наблюдения за секунду, отдаёт прежнее, и каждая попытка
	// перечитывает вид мастера, состояние слоя и вид секций. Провал — по
	// последнему наблюдению.
	var sectionsLog string
	rtWizardViewUntil(e, "вид мастера не сошёлся с состоянием слоя и видом секций Xray", func(v map[string]any) (bool, string) {
		issues := rtWizardContractIssues(e, v, &sectionsLog)
		return len(issues) == 0, strings.Join(issues, "; ")
	})
	if sectionsLog != "" {
		t.Log(sectionsLog)
	}
}

func rtWizardCheckNodesNotDownloaded(e *rtSrc) {
	t := e.t
	t.Helper()
	before := rtWizardStep(t, rtWizardView(e), "nodes")
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": rtWizardDeadSub})
	if pv.Status != http.StatusOK {
		t.Fatalf("preview подписки на loopback: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if token == "" {
		t.Fatalf("preview подписки на loopback без токена (ответ %d): шаг «узлы» без скачанного набора на устройстве не проверить", pv.Status)
	}
	t.Cleanup(func() { e.resetDraft() })
	add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token})
	if add.Status != http.StatusOK {
		t.Fatalf("add подписки на loopback: %d %s", add.Status, add.Code)
	}
	wantSources := rtWizardNum(before, "sources") + 1
	v := rtWizardViewUntil(e, "источник не появился в шаге «узлы»", func(v map[string]any) (bool, string) {
		got := rtWizardNum(rtWizardStep(t, v, "nodes"), "sources")
		return got == wantSources, fmt.Sprintf("источников %d, ожидалось %d", got, wantSources)
	})
	after := rtWizardStep(t, v, "nodes")
	if got, want := rtWizardNum(after, "nodes"), rtWizardNum(before, "nodes"); got != want {
		t.Errorf("узлов %d, ожидалось %d: источник без скачанного набора дал узлы", got, want)
	}
}

func rtWizardCheckNodesConsistent(e *rtSrc) {
	t := e.t
	t.Helper()
	nodes := rtWizardNum(rtWizardStep(t, rtWizardView(e), "nodes"), "nodes")
	r := e.call(http.MethodGet, rtSourcesBase, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rtSourcesBase, r.Status, r.Code)
	}
	withNodes := false
	srcs, _ := r.object(t)["sources"].([]any)
	for _, s := range srcs {
		src, _ := s.(map[string]any)
		list, _ := src["nodes"].([]any)
		if on, _ := src["enabled"].(bool); on && len(list) > 0 {
			withNodes = true
		}
	}
	if (nodes > 0) != withNodes {
		t.Errorf("шаг «узлы»: %d узлов, во вкладке «Узлы» включённый источник с узлами: %v", nodes, withNodes)
	}
}

func rtWizardCheckStatusMonotonic(e *rtSrc) {
	t := e.t
	t.Helper()
	rtDevMode(e, true)
	prev := rtWizardStatus(rtWizardView(e))
	release := rtWizardHold(e)
	t.Cleanup(release)
	// Возврат статуса — при удержанном черновике (rtWizardRestore держит его
	// сам); затем отпускается удержание подтеста.
	t.Cleanup(func() { rtWizardRestore(e, prev) })

	// Три GET вида с замером: панель ждёт наблюдения не дольше секунды.
	var gets [3]time.Duration
	for i := range gets {
		start := time.Now()
		r := e.call(http.MethodGet, rtWizardBase, nil)
		gets[i] = time.Since(start)
		if r.Status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", rtWizardBase, r.Status, r.Code)
		}
	}

	// Длительности трёх GET и пяти POST — в строке wizard-timing: (план-сессия
	// сверяет их с журналом панели).
	v, resetTook := rtWizardPostTimed(e, "reset")
	if st := rtWizardStatus(v); st != "" {
		t.Fatalf("после сброса статус мастера %q, ожидался пустой", st)
	}
	v, closeTook := rtWizardPostTimed(e, "close")
	if st := rtWizardStatus(v); st != "closed" {
		t.Errorf("после close статус %q", st)
	}
	v, repeatTook := rtWizardPostTimed(e, "close")
	if st := rtWizardStatus(v); st != "closed" {
		t.Errorf("после повторного close статус %q", st)
	}
	v, hideTook := rtWizardPostTimed(e, "hide")
	if st := rtWizardStatus(v); st != "hidden" {
		t.Errorf("после hide статус %q", st)
	}
	v, afterHideTook := rtWizardPostTimed(e, "close")
	if st := rtWizardStatus(v); st != "hidden" {
		t.Errorf("close после hide понизил статус до %q", st)
	}
	if card, _ := v["card_visible"].(bool); card {
		t.Error("card_visible при статусе hidden")
	}
	t.Logf("wizard-timing: get=%dms,%dms,%dms reset=%dms close=%dms close-repeat=%dms hide=%dms close-after-hide=%dms",
		gets[0].Milliseconds(), gets[1].Milliseconds(), gets[2].Milliseconds(),
		resetTook.Milliseconds(), closeTook.Milliseconds(), repeatTook.Milliseconds(),
		hideTook.Milliseconds(), afterHideTook.Milliseconds())
	for i, d := range gets {
		if d > rtWizardGetMax {
			t.Errorf("GET %d ответил за %v (предел %v): GET мастера ждёт хранилища или чтения ядер дольше предела (G-149.1-6)",
				i+1, d.Round(time.Millisecond), rtWizardGetMax)
		}
	}
	rtWizardCheckPost(t, "reset", resetTook)
	rtWizardCheckPost(t, "close", closeTook)
	rtWizardCheckPost(t, "close (повтор)", repeatTook)
	rtWizardCheckPost(t, "hide", hideTook)
	rtWizardCheckPost(t, "close после hide", afterHideTook)
	rtSetDevMode(e, false)
	r := e.call(http.MethodPost, rtWizardBase+"/reset", nil)
	rtSetDevMode(e, true)
	if r.Status != http.StatusForbidden || r.Code != "dev_mode_required" {
		t.Errorf("reset без режима разработчика: %d %q, ожидалось 403 dev_mode_required", r.Status, r.Code)
	}
}

func rtWizardCheckDoneStamped(e *rtSrc) {
	t := e.t
	t.Helper()
	rtDevMode(e, true)
	v := rtWizardView(e)
	prev := rtWizardStatus(v)
	// Период наблюдателя — у процесса панели (tick_s), а не константой.
	tick := rtWizardTick(t, v)
	t.Cleanup(func() { rtWizardRestore(e, prev) })
	rtWizardReset(e)
	resetAt := time.Now()
	// Число выполненных шагов заранее не берётся: GET может отдать прежнее наблюдение, а
	// черновик после предыдущих подтестов отпускается не мгновенно (шагов 5 → 6). Ждём
	// отметку сервера: тик не позже периода, ещё период — запас на тик, начатый до сброса,
	// и 10 с — на медленную панель.
	limit := 2*tick + 10*time.Second
	var last map[string]any
	ok, seen := routertest.Poll(limit, time.Second, func() (bool, string) {
		last = rtWizardView(e)
		card, _ := last["card_visible"].(bool)
		return rtWizardStatus(last) == "done" && !card,
			fmt.Sprintf("статус %q, card_visible %v, шагов %d", rtWizardStatus(last), card, rtWizardNum(last, "done_count"))
	})
	if ok {
		t.Logf("ветка: шаги 1–6 выполнены — отметка сервера через %.1fs (tick=%ds)", time.Since(resetAt).Seconds(), int(tick/time.Second))
		t.Logf("wizard-timing: done=%.1fs tick=%ds", time.Since(resetAt).Seconds(), int(tick/time.Second))
		return
	}
	// Отметки нет: допустимо, только пока шагов меньше шести (отметку ставят лишь при 1–6).
	if n := rtWizardNum(last, "done_count"); n == 6 {
		t.Errorf("за %v сервер не отметил «пройдено» при выполненных шагах 1–6: %s", limit, seen)
		return
	}
	if st := rtWizardStatus(last); st != "" {
		t.Errorf("без выполненных шагов статус изменился: %s", seen)
		return
	}
	t.Logf("ветка: выполнено шагов %d из 6 — отметки нет, как и должно быть", rtWizardNum(last, "done_count"))
}

// rtWizardStartCore запускает отдельный процесс ядра во временном каталоге без
// слушающих портов (образец — rtRpStartCore). Процесс убивается в t.Cleanup при
// любом исходе. Возвращает канал, закрытый при выходе процесса, и хвост журнала.
func rtWizardStartCore(t *testing.T, kernel, dir string) (<-chan struct{}, func() string) {
	t.Helper()
	var cmd *exec.Cmd
	switch kernel {
	case "mihomo":
		// Без mixed-port, tun и redir/tproxy: контроллер — unix-сокет в каталоге теста.
		cfg := "mode: rule\n" +
			"log-level: warning\n" +
			"external-controller-unix: " + filepath.Join(dir, "ctl.sock") + "\n" +
			"proxies:\n" +
			"  - {name: rt-direct, type: direct}\n" +
			"rules:\n" +
			"  - MATCH,DIRECT\n"
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command(rtRpMihomoBin, "-d", dir, "-f", filepath.Join(dir, "config.yaml"))
	case "xray":
		// Только журнал и один freedom-outbound: без inbounds.
		cfg := `{"log":{"loglevel":"warning"},"outbounds":[{"protocol":"freedom","tag":"direct"}]}`
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command(rtCoresXrayBin, "run", "-c", filepath.Join(dir, "config.json"))
	default:
		t.Fatalf("неизвестное ядро %q", kernel)
	}
	logPath := filepath.Join(dir, "core.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("процесс %s не запустился: %v", kernel, err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			select {
			case <-exited:
			case <-time.After(20 * time.Second):
			}
		}
		_ = logFile.Close()
	})
	tail := func() string {
		data, _ := os.ReadFile(logPath)
		if len(data) > 2000 {
			data = data[len(data)-2000:]
		}
		return string(data)
	}
	return exited, tail
}

// Конфликт ядер (D-15): второй процесс ядра рядом с активным. GET /wizard даёт
// conflict и при чистом черновике, шаг «запуск» не выполнен, шаги 1–5 не
// меняются; запуск ядра (путь кнопки шага 6) отвечает 409 kernel_conflict без
// действий; остановка второго ядра тем же маршрутом, что кнопка мастера, снимает
// конфликт и возвращает шаг «запуск». Активное ядро тест не трогает (FND-05).
func TestRouterWizardConflict(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	routertest.Tier(t, routertest.Full, "внесение сбоя: второй процесс ядра рядом с активным — конфликт ядер (D-15)")
	e := rtNewSrc(t)
	e.freshDraft()

	core := tg.Core
	other, otherBin := "mihomo", rtRpMihomoBin
	if core == "mihomo" {
		other, otherBin = "xray", rtCoresXrayBin
	}
	if _, err := os.Stat(otherBin); err != nil {
		t.Fatalf("на стенде нет второго ядра %s: %v", other, err)
	}
	routertest.WaitKernelReady(t, core, 60*time.Second)

	// Конфликт проверяется при чистом черновике: шаг «применение» по серверу
	// выполнен, а мастер всё равно показывает причину и кнопки остановки.
	e.resetDraft()
	before := rtWizardView(e)
	if n := rtWizardNum(rtWizardStep(t, before, "apply"), "draft_changes"); n != 0 {
		t.Fatalf("черновик не чист после сброса: draft_changes %d", n)
	}
	was := rtWizardDone(t, before)

	dir, err := os.MkdirTemp(routertest.Current(t).WorkDir, "wizard-conflict-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// Каталог удаляется после остановки процесса: t.Cleanup идут в обратном порядке.
	exited, tail := rtWizardStartCore(t, other, dir)

	ok, seen := routertest.Poll(15*time.Second, time.Second, func() (bool, string) {
		select {
		case <-exited:
			t.Fatalf("второе ядро %s завершилось до конфликта: %s", other, tail())
		default:
		}
		ks, _ := rtWizardView(e)["kernels"].(map[string]any)
		conflict, _ := ks["conflict"].(bool)
		running, _ := ks["running"].([]any)
		return conflict && len(running) == 2, fmt.Sprintf("conflict %v, запущено %d", conflict, len(running))
	})
	if !ok {
		t.Fatalf("конфликт ядер не виден за 15 с: %s", seen)
	}
	rtWizardCheckConflictSteps(e, was)

	// Запуск при конфликте — ядро цели: ошибочно пропущенный конфликт только
	// перезапустил бы активное ядро.
	sw := e.callSlow(http.MethodPost, "/api/service/control?action=switch_kernel&kernel="+core, nil)
	if sw.Status != http.StatusConflict || sw.Code != "kernel_conflict" {
		t.Fatalf("switch_kernel при конфликте: %d %q, ожидалось 409 kernel_conflict", sw.Status, sw.Code)
	}

	stop := e.callSlow(http.MethodPost, "/api/service/control?action=stop&kernel="+other, nil)
	if stop.Status != http.StatusOK {
		t.Fatalf("остановка %s: %d %s", other, stop.Status, stop.Code)
	}
	ok, seen = routertest.Poll(15*time.Second, time.Second, func() (bool, string) {
		ks, _ := rtWizardView(e)["kernels"].(map[string]any)
		conflict, _ := ks["conflict"].(bool)
		return !conflict, fmt.Sprintf("conflict %v", conflict)
	})
	if !ok {
		t.Fatalf("конфликт не снят за 15 с после остановки %s: %s", other, seen)
	}
	if after := rtWizardDone(t, rtWizardView(e)); after["start"] != was["start"] {
		t.Errorf("после остановки %s шаг «запуск»: done %v, до конфликта %v", other, after["start"], was["start"])
	}
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Errorf("процесс %s не завершился после остановки маршрутом мастера", other)
	}
}

func rtWizardCheckConflictSteps(e *rtSrc, was map[string]bool) {
	t := e.t
	t.Helper()
	during := rtWizardDone(t, rtWizardView(e))
	if during["start"] {
		t.Error("при конфликте шаг «запуск» выполнен")
	}
	for _, id := range []string{"kernel", "sections", "nodes", "routing", "apply"} {
		if during[id] != was[id] {
			t.Errorf("при конфликте шаг %q: done %v, до конфликта %v", id, during[id], was[id])
		}
	}

}

func rtWizardCheckSecrets(t *testing.T, raw []byte) {
	t.Helper()
	if sub := os.Getenv("XCP_RT_SUBSCRIPTION_URL"); sub != "" {
		secrets := []string{sub, url.QueryEscape(sub)}
		if u, err := url.Parse(sub); err == nil {
			secrets = append(secrets, strings.Trim(u.Path, "/"), u.RawQuery)
		}
		rtNoSecrets(t, "wizard", raw, secrets...)
	}
}
