//go:build router

// router-area: routing

package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Арбитр выбора ручных групп (SPX-01, SPX-02, D-01, D-04, D-06): окно расписания
// ставит узел окна, по концу окна возвращается исходный выбор, клик внутри окна —
// перехват до конца окна, «Вернуть расписание» снимает перехват, новый экземпляр
// арбитра посреди окна (перезапуск панели) возвращает тот же исходный выбор.
// Время — часы теста через существующий Deps.Now (rtarClock), тик вызывается явно
// (arb.tick, без Start). Трафик проверяется на изолированных процессах ядер в
// каталоге теста: Xray — копия файлов панели с socks-входом и горячей заменой тела
// группы тем же xrayHot, что у конвейера (как hot_swap_manual_group); Mihomo —
// отдельный процесс с профилем панели (rtMihomoCore). Applier не задан, ядро
// стенда тест не читает и не переключает — метки ядра нет.

// rtarClock — часы теста для Deps.Now.
type rtarClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now — текущий момент часов теста.
func (c *rtarClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// set переводит часы теста.
func (c *rtarClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// rtarAt — понедельник недели теста (23.11.2026) плюс day дней, h:m по поясу
// процесса (окна действуют по часам роутера).
func rtarAt(day, h, m int) time.Time {
	return time.Date(2026, time.November, 23+day, h, m, 0, 0, time.Local)
}

// rtarWithClock подключает часы теста к Deps.Now.
func rtarWithClock(c *rtarClock) rtOption {
	return rtWithDeps(func(d *Deps) { d.Now = c.Now })
}

// rtarGroup — ручная группа «Окно» окружения и её узлы по ролям (A — исходный
// выбор, B — узел окна, C — узел перехвата). В журнал идут только роли.
type rtarGroup struct {
	e      *rtEnv
	g      Group
	member map[string]string // роль → участник группы
	ref    map[string]string // роль → Ref узла каталога
}

// role — роль участника m ("none" — выбора нет, "other" — не узел теста).
func (gr *rtarGroup) role(m string) string {
	if m == "" {
		return "none"
	}
	for r, x := range gr.member {
		if x == m {
			return r
		}
	}
	return "other"
}

// live — выбор группы и запись арбитра в Live.
func (gr *rtarGroup) live(t *testing.T) (sel string, rec arbiterRecord, hasRec bool) {
	t.Helper()
	live := gr.e.state(t).Live
	rec, hasRec = readArbiter(live)[gr.g.ID]
	return liveSelection(live)[gr.g.ID], rec, hasRec
}

// expect проверяет роль выбора группы в Live и держателя записи арбитра ("" —
// записи нет); возвращает запись.
func (gr *rtarGroup) expect(t *testing.T, step, wantSel, wantHolder string) arbiterRecord {
	t.Helper()
	sel, rec, has := gr.live(t)
	holder := ""
	if has {
		holder = rec.Holder
	}
	if gr.role(sel) != wantSel || holder != wantHolder {
		t.Errorf("%s: выбор=%s (ожидался %s), держатель=%q (ожидался %q)", step, gr.role(sel), wantSel, holder, wantHolder)
	}
	return rec
}

// view — группа в виде «Прокси» (runtimeSnapshot).
func (gr *rtarGroup) view(t *testing.T) RuntimeGroup {
	t.Helper()
	v, _, err := gr.e.svc.runtimeSnapshot()
	if err != nil {
		t.Fatalf("вид «Прокси»: %v", err)
	}
	for _, g := range v.Groups {
		if g.ID == gr.g.ID {
			return g
		}
	}
	t.Fatalf("группы нет в виде «Прокси» (состояние %s, групп %d)", v.State, len(v.Groups))
	return RuntimeGroup{}
}

// window сохраняет окно группы на узел роли role; отказ — провал теста.
func (gr *rtarGroup) window(t *testing.T, days []int, from, to, role string) ScheduleWindow {
	t.Helper()
	w, err := gr.e.svc.SaveScheduleWindow(gr.g.ID, ScheduleWindow{Days: days, From: from, To: to, Member: gr.member[role], Enabled: true})
	if err != nil {
		t.Fatalf("окно %s–%s: %v", from, to, err)
	}
	return w
}

// rtarSetup создаёт ручную группу «Окно» на всех узлах, применяет состояние и
// ставит исходный выбор — узел первой роли (до создания арбитра). Узлы ролей —
// rtar-<роль> в каталоге cat. only — в применённом состоянии только эта группа
// (группы по умолчанию сняты: файл сценария Xray заменяет routing панели). before
// вызывается до «Применить» (правила и «Остальное» сценария).
func rtarSetup(t *testing.T, e *rtEnv, cat []sources.CatalogNode, only bool, before func(g Group), roles ...string) *rtarGroup {
	t.Helper()
	gr := &rtarGroup{e: e, member: map[string]string{}, ref: map[string]string{}}
	for _, r := range roles {
		n := rtsNode(t, cat, "rtar-"+strings.ToLower(r))
		gr.member[r], gr.ref[r] = memberNode+n.Ref, n.Ref
	}
	_, g, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Окно", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	gr.g = g
	if only {
		e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{g} })
	}
	if before != nil {
		before(g)
	}
	e.takeAndApply(t)
	if _, err := e.svc.Select(context.Background(), g.ID, gr.member[roles[0]]); err != nil {
		t.Fatalf("исходный выбор: %v", err)
	}
	return gr
}

// rtarNewLight — окружение со слоем и установленным Xray без процесса ядра:
// выбор арбитра (selectArbiter) проходит конвейер и фиксируется в Live с исходом
// «ядро не запущено». Узлы A и B — ссылки на порты loopback без сервера: варианты
// группы зависят только от состава, не от живости узла.
func rtarNewLight(t *testing.T, clk *rtarClock) *rtarGroup {
	t.Helper()
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtarWithClock(clk))
	e.addMyRaw(t, "socks5://127.0.0.1:9#rtar-a", "socks5://127.0.0.1:10#rtar-b")
	return rtarSetup(t, e, e.catalog(t).Xray, false, nil, "A", "B")
}

// rtarHotSync переносит в изолированный процесс Xray тело группы, которое выбор
// записал в xcp-groups окружения: горячая замена (rmo→ado) тем же xrayHot, что у
// конвейера при запущенном ядре, затем файл копируется в каталог процесса. Файл не
// менялся — ничего не делает.
func rtarHotSync(t *testing.T, ctx context.Context, e *rtEnv, dir, step string) {
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

// Домены запросов сценария Mihomo: правило направляет их в группу «Окно».
const (
	rtarDomainWindow = "rtar-window.example.test"
	rtarDomainOrigin = "rtar-origin.example.test"
)

// rtarSaw делает запросы на домен через socks процесса Mihomo, пока домен не
// появится в журнале узла (до 30 с). Запрос может упасть на DNS узла — важен
// только журнал узла.
func rtarSaw(port int, accessLog, domain string) bool {
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
		_, _ = rtSocksGet(port, "http://"+domain+"/", 6*time.Second)
		if rtWaitNodeSaw(accessLog, domain, 2*time.Second) {
			return true
		}
	}
	return false
}

func TestRouterArbiter(t *testing.T) {
	// Окно ставит узел окна, клик внутри окна — перехват до конца окна, тик его не
	// откатывает, «Вернуть расписание» ставит узел окна, по концу окна — исходный
	// выбор, окно следующего дня действует снова (D-01, D-04, D-06). Трафик —
	// счётчики CONNECT-прокси узлов за изолированным Xray.
	t.Run("xray_window_restore_override", func(t *testing.T) {
		rtNeedBins(t, rtXrayBin)
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(target.Close)
		u, err := url.Parse(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		proxyA, hitsA := rtxaConnectProxy(t, u.Host)
		proxyB, hitsB := rtxaConnectProxy(t, u.Host)
		proxyC, hitsC := rtxaConnectProxy(t, u.Host)
		version := rtxaRun(t, "version")
		apiPort := rtFreePort(t)
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtarWithClock(clk), rtWithDeps(func(d *Deps) {
			d.XrayAPIPort = func() int { return apiPort }
			d.XrayBin = func() string { return rtXrayBin }
			d.KernelVersion = func(string) string { return version }
		}))
		e.addMyRaw(t, proxyA+"#rtar-a", proxyB+"#rtar-b", proxyC+"#rtar-c")
		gr := rtarSetup(t, e, e.catalog(t).Xray, true, nil, "A", "B", "C")

		dir := rtxaCopyDir(t, e.xrayDir())
		inPort := rtFreePort(t)
		extra, err := json.Marshal(map[string]any{
			"inbounds": []any{map[string]any{"tag": "rt-arb-in", "listen": "127.0.0.1", "port": inPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
			"routing":  map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"rt-arb-in"}, "outboundTag": GroupTag(gr.g.ID)}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "99_zz_rt_arb.json"), extra, 0o600); err != nil {
			t.Fatal(err)
		}
		proc := rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), apiPort)

		hits := map[string]*atomic.Int64{"A": hitsA, "B": hitsB, "C": hitsC}
		// via переносит выбор в процесс и делает запрос через группу; возвращает
		// роль узла, через который прошёл запрос.
		via := func(step string) string {
			t.Helper()
			rtarHotSync(t, ctx, e, dir, step)
			before := map[string]int64{}
			for r, h := range hits {
				before[r] = h.Load()
			}
			status, err := rtSocksGet(inPort, target.URL, 3*time.Second)
			if err != nil || status != http.StatusNoContent {
				t.Fatalf("%s: запрос через группу: статус=%d ошибка=%t", step, status, err != nil)
			}
			got := "none"
			for r, h := range hits {
				if h.Load() == before[r] {
					continue
				}
				if got != "none" {
					return "several"
				}
				got = r
			}
			return got
		}
		check := func(step, wantSel, wantHolder string) arbiterRecord {
			t.Helper()
			rec := gr.expect(t, step, wantSel, wantHolder)
			if r := via(step); r != wantSel {
				t.Errorf("%s: запрос прошёл через %s, ожидался %s", step, r, wantSel)
			}
			return rec
		}

		check("исходный выбор", "A", "")
		arb := NewArbiter(e.svc)
		t.Cleanup(arb.Stop)
		clk.set(rtarAt(0, 12, 0))
		w := gr.window(t, []int{0, 1}, "10:00", "14:00", "B")
		arb.tick(ctx)
		rec := check("окно понедельника", "B", holderSchedule)
		if rec.Window != w.ID || gr.role(rec.Origin) != "A" {
			t.Errorf("окно понедельника: окно записи совпало=%t, исходный выбор=%s", rec.Window == w.ID, gr.role(rec.Origin))
		}

		// Клик по другому узлу внутри окна — перехват до конца окна.
		if _, err := e.svc.Select(ctx, gr.g.ID, gr.member["C"]); err != nil {
			t.Fatalf("клик внутри окна: %v", err)
		}
		rec = check("перехват", "C", holderOverride)
		if gr.role(rec.Override) != "C" || rec.Until != rtarAt(0, 14, 0).Unix() || gr.role(rec.Origin) != "A" {
			t.Errorf("перехват: узел=%s, конец совпал с концом окна=%t, исходный выбор=%s",
				gr.role(rec.Override), rec.Until == rtarAt(0, 14, 0).Unix(), gr.role(rec.Origin))
		}
		arb.tick(ctx)
		check("тик при перехвате", "C", holderOverride)

		// «Вернуть расписание» снимает перехват и ставит узел окна.
		if _, err := e.svc.RestoreSchedule(ctx, gr.g.ID); err != nil {
			t.Fatalf("«Вернуть расписание»: %v", err)
		}
		rec = check("«Вернуть расписание»", "B", holderSchedule)
		if rec.Override != "" || rec.Until != 0 || gr.role(rec.Origin) != "A" {
			t.Errorf("«Вернуть расписание»: перехват остался=%t, исходный выбор=%s", rec.Override != "" || rec.Until != 0, gr.role(rec.Origin))
		}

		// Перехват, живой к концу окна, по концу окна уступает исходному выбору, а
		// не остаётся на узле перехвата; окно вторника действует снова.
		if _, err := e.svc.Select(ctx, gr.g.ID, gr.member["C"]); err != nil {
			t.Fatalf("второй клик внутри окна: %v", err)
		}
		check("второй перехват", "C", holderOverride)
		clk.set(rtarAt(0, 14, 0))
		arb.tick(ctx)
		check("конец окна понедельника", "A", "")
		clk.set(rtarAt(1, 12, 0))
		arb.tick(ctx)
		check("окно вторника", "B", holderSchedule)
		clk.set(rtarAt(1, 14, 0))
		arb.tick(ctx)
		check("конец окна вторника", "A", "")
		if !proc.alive() {
			t.Fatal("изолированный процесс Xray не работает после горячих замен")
		}
	})

	// Перезапуск панели посреди окна (D-06): исходный выбор лежит в Live, поэтому
	// новый экземпляр арбитра на том же слое (прежний больше не тикает) держит окно
	// и по его концу возвращает тот же исходный выбор.
	t.Run("restart_mid_window", func(t *testing.T) {
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		gr := rtarNewLight(t, clk)
		first := NewArbiter(gr.e.svc)
		t.Cleanup(first.Stop)
		clk.set(rtarAt(0, 12, 0))
		gr.window(t, []int{0}, "10:00", "14:00", "B")
		first.tick(ctx)
		gr.expect(t, "окно до перезапуска", "B", holderSchedule)

		second := NewArbiter(gr.e.svc)
		t.Cleanup(second.Stop)
		clk.set(rtarAt(0, 12, 30))
		second.tick(ctx)
		rec := gr.expect(t, "окно после перезапуска", "B", holderSchedule)
		if gr.role(rec.Origin) != "A" {
			t.Errorf("окно после перезапуска: исходный выбор=%s, ожидался A", gr.role(rec.Origin))
		}
		clk.set(rtarAt(0, 14, 0))
		second.tick(ctx)
		gr.expect(t, "конец окна после перезапуска", "A", "")
	})

	// Окно на изолированном Mihomo: тик ставит узел окна в процессе (Clash API),
	// запрос группы доходит до узла окна; по концу окна — исходный узел (D-01, D-06).
	t.Run("mihomo_window", func(t *testing.T) {
		rtNeedBins(t, rtXrayBin, rtMihomoBin)
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		portA, logA := rtStartNode(t)
		portB, logB := rtStartNode(t)
		core := rtNewMihomoCore(t)
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtarWithClock(clk))
		e.writeUserProfile(t)
		e.addMyRaw(t,
			"socks5://127.0.0.1:"+strconv.Itoa(portA)+"#rtar-a",
			"socks5://127.0.0.1:"+strconv.Itoa(portB)+"#rtar-b")
		cat := e.catalog(t).Mihomo
		gr := rtarSetup(t, e, cat, false, func(g Group) {
			if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
				Enabled: true, Target: Target{Kind: KindGroup, ID: g.ID},
				Conds: []Cond{{Type: CondDomain, Values: []string{rtarDomainWindow, rtarDomainOrigin}}},
			}); err != nil {
				t.Fatalf("SaveRule: %v", err)
			}
			if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindReject}); err != nil {
				t.Fatalf("SetFinal: %v", err)
			}
		}, "A", "B")
		nameA, nameB := rtsNode(t, cat, "rtar-a").MihomoName, rtsNode(t, cat, "rtar-b").MihomoName

		rtmtStart(t, e, core, nil)
		e.rtsReconcile(t, ctx)
		rtsWaitNow(t, core, gr.g.Name, nameA, 20*time.Second)

		arb := NewArbiter(e.svc)
		t.Cleanup(arb.Stop)
		clk.set(rtarAt(0, 12, 0))
		gr.window(t, []int{0}, "10:00", "14:00", "B")
		arb.tick(ctx)
		gr.expect(t, "окно Mihomo", "B", holderSchedule)
		rtsWaitNow(t, core, gr.g.Name, nameB, 10*time.Second)
		if !rtarSaw(core.port, logB, rtarDomainWindow) {
			t.Error("окно Mihomo: запрос группы не дошёл до узла окна")
		}
		if rtNodeSaw(logA, rtarDomainWindow) {
			t.Error("окно Mihomo: запрос группы прошёл через исходный узел")
		}

		clk.set(rtarAt(0, 14, 0))
		arb.tick(ctx)
		gr.expect(t, "конец окна Mihomo", "A", "")
		rtsWaitNow(t, core, gr.g.Name, nameA, 10*time.Second)
		if !rtarSaw(core.port, logA, rtarDomainOrigin) {
			t.Error("конец окна Mihomo: запрос группы не дошёл до исходного узла")
		}
		if rtNodeSaw(logB, rtarDomainOrigin) {
			t.Error("конец окна Mihomo: запрос группы прошёл через узел окна")
		}
	})

	// Окно через полночь и пересечения окон (D-06, D-08, D-11): поведение тика и
	// вида, расчёт окна напрямую не вызывается. Окружение со слоем и установленным
	// Xray без процесса ядра: selectArbiter фиксирует выбор в Live конвейером с
	// исходом «ядро не запущено», вид «Прокси» строится из того же Live.
	t.Run("overnight_and_overlap", func(t *testing.T) {
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		gr := rtarNewLight(t, clk)
		arb := NewArbiter(gr.e.svc)
		t.Cleanup(arb.Stop)
		night := gr.window(t, []int{0}, "22:00", "07:00", "B")
		steps := []struct {
			name        string
			at          time.Time
			sel, holder string
			hold        string
			until       int64
		}{
			{"понедельник 21:00", rtarAt(0, 21, 0), "A", "", "", 0},
			{"вторник 03:00", rtarAt(1, 3, 0), "B", holderSchedule, holderSchedule, rtarAt(1, 7, 0).Unix()},
			{"вторник 07:00", rtarAt(1, 7, 0), "A", "", "", 0},
		}
		for _, s := range steps {
			clk.set(s.at)
			arb.tick(ctx)
			gr.expect(t, s.name, s.sel, s.holder)
			v := gr.view(t)
			kind, until := "", int64(0)
			if v.Hold != nil {
				kind, until = v.Hold.Kind, v.Hold.Until
			}
			if kind != s.hold || until != s.until {
				t.Errorf("%s: вид: удержание %q (ожидалось %q), конец удержания совпал=%t", s.name, kind, s.hold, until == s.until)
			}
			if s.hold == holderSchedule && v.Hold != nil && v.Hold.Until != rtarAt(1, 7, 0).Unix() {
				t.Errorf("%s: Hold.Until не равен вторнику 07:00 по часам теста", s.name)
			}
		}

		// Пересечения отклоняются без записи в Live: окна группы после отказа прежние.
		ids := func() string {
			t.Helper()
			var out []string
			for _, w := range readSchedules(gr.e.state(t).Live)[gr.g.ID] {
				out = append(out, w.ID)
			}
			return strings.Join(out, ",")
		}
		reject := func(step string, gid string, w ScheduleWindow, ok func(error) bool) {
			t.Helper()
			before := ids()
			_, err := gr.e.svc.SaveScheduleWindow(gid, w)
			if !ok(err) {
				t.Errorf("%s: отказ не тот: %s", step, rtarErrCode(err))
			}
			if ids() != before {
				t.Errorf("%s: окна группы изменились после отказа", step)
			}
		}
		overlapWith := func(id string) func(error) bool {
			return func(err error) bool {
				var o *ScheduleOverlapError
				return errors.As(err, &o) && o.Window == id
			}
		}
		reject("вторник 06:00–08:00", gr.g.ID,
			ScheduleWindow{Days: []int{1}, From: "06:00", To: "08:00", Member: gr.member["B"], Enabled: true},
			overlapWith(night.ID))
		sunday := gr.window(t, []int{6}, "23:00", "01:00", "B")
		reject("понедельник 00:30–02:00 (перенос через неделю)", gr.g.ID,
			ScheduleWindow{Days: []int{0}, From: "00:30", To: "02:00", Member: gr.member["B"], Enabled: true},
			overlapWith(sunday.ID))
		reject("равные «с» и «по»", gr.g.ID,
			ScheduleWindow{Days: []int{2}, From: "10:00", To: "10:00", Member: gr.member["B"], Enabled: true},
			func(err error) bool { var inv *InvalidInputError; return errors.As(err, &inv) })
		auto, ok := rtGroupByName(gr.e.groups(t), "Авто")
		if !ok {
			t.Fatal("нет группы «Авто»")
		}
		reject("группа «авто»", auto.ID,
			ScheduleWindow{Days: []int{3}, From: "10:00", To: "12:00", Member: gr.member["B"], Enabled: true},
			func(err error) bool { return errors.Is(err, ErrGroupNotApplied) })
		if n := len(readSchedules(gr.e.state(t).Live)[auto.ID]); n != 0 {
			t.Errorf("группа «авто»: окон в Live %d после отказа", n)
		}
	})

	// Узел окна пропал из группы (D-07): окно пропускается — тик не меняет выбор,
	// вид отдаёт hold.kind=node_missing.
	t.Run("node_missing", func(t *testing.T) {
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		gr := rtarNewLight(t, clk)
		arb := NewArbiter(gr.e.svc)
		t.Cleanup(arb.Stop)
		clk.set(rtarAt(0, 12, 0))
		gr.window(t, []int{0}, "10:00", "14:00", "B")
		entryID := strings.TrimPrefix(gr.ref["B"], sources.MySourceID+"/")
		if _, err := gr.e.svc.DeleteNode(gr.e.rev(t), entryID); err != nil {
			t.Fatalf("удаление узла окна из «Моих узлов»: %v", err)
		}
		gr.e.applyOK(t)
		before, _, _ := gr.live(t)
		arb.tick(ctx)
		after, _, hasRec := gr.live(t)
		if after != before || gr.role(after) != "A" || hasRec {
			t.Errorf("пропавший узел: выбор не изменился=%t, выбор=%s, запись арбитра=%t", after == before, gr.role(after), hasRec)
		}
		v := gr.view(t)
		if v.Hold == nil || v.Hold.Kind != holdNodeMissing {
			kind := ""
			if v.Hold != nil {
				kind = v.Hold.Kind
			}
			t.Errorf("пропавший узел: вид: удержание %q, ожидалось %q", kind, holdNodeMissing)
		}
	})

	// Часы с годом меньше 2025 считаются несинхронизированными (A9): окно «на весь
	// день» не применяется. Контроль — те же окна на верных часах действуют.
	t.Run("clock_not_synced", func(t *testing.T) {
		ctx := context.Background()
		clk := &rtarClock{now: rtarAt(0, 9, 0)}
		gr := rtarNewLight(t, clk)
		arb := NewArbiter(gr.e.svc)
		t.Cleanup(arb.Stop)
		old := time.Date(2000, time.January, 5, 12, 0, 0, 0, time.Local)
		clk.set(old)
		day := weekday0Mon(old)
		gr.window(t, []int{day}, "00:00", "23:59", "B")
		arb.tick(ctx)
		gr.expect(t, "часы 2000 года", "A", "")
		if v := gr.view(t); v.Hold != nil {
			t.Errorf("часы 2000 года: вид отдаёт удержание %q", v.Hold.Kind)
		}
		// Тот же день недели и время на часах 2026 года: окно действует.
		clk.set(rtarAt(day, 12, 0))
		arb.tick(ctx)
		gr.expect(t, "контроль на верных часах", "B", holderSchedule)
	})
}

// rtarErrCode — код отказа для журнала без текста ошибки.
func rtarErrCode(err error) string {
	var o *ScheduleOverlapError
	var inv *InvalidInputError
	switch {
	case err == nil:
		return "nil"
	case errors.As(err, &o):
		return "overlap"
	case errors.As(err, &inv):
		return "invalid:" + inv.Field
	case errors.Is(err, ErrGroupNotApplied):
		return "group_not_applied"
	case errors.Is(err, ErrMemberInvalid):
		return "member_invalid"
	}
	return "other"
}
