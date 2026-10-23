//go:build router

package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Выбор участника ручной группы без черновика (146-14): Xray — файл xcp-groups,
// Mihomo — Clash API отдельного процесса в каталоге теста. Ядра стенда не
// трогаются (Applier не задан, процесс Mihomo — свой), метка ядра не нужна.

// rtsNode возвращает узел каталога по имени для показа.
func rtsNode(t *testing.T, list []sources.CatalogNode, display string) sources.CatalogNode {
	t.Helper()
	for _, n := range list {
		if n.Display == display {
			return n
		}
	}
	t.Fatalf("в каталоге нет узла %q", display)
	return sources.CatalogNode{}
}

// rtsOutbound ищет исходящий по тегу во всех файлах каталога Xray.
func rtsOutbound(t *testing.T, dir, tag string) map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for _, ob := range doc.Outbounds {
			if ob["tag"] == tag {
				return ob
			}
		}
	}
	t.Fatalf("исходящего с тегом %q нет в файлах %s", tag, filepath.Base(dir))
	return nil
}

// rtsSameOutbound сообщает, что исходящие одинаковы, кроме тега.
func rtsSameOutbound(a, b map[string]any) bool {
	ca, cb := map[string]any{}, map[string]any{}
	for k, v := range a {
		if k != "tag" {
			ca[k] = v
		}
	}
	for k, v := range b {
		if k != "tag" {
			cb[k] = v
		}
	}
	return reflect.DeepEqual(ca, cb)
}

// Выбор участника ручной группы в Xray: Live хранит выбор, черновик не меняется,
// файл xcp-groups получает копию выбранного узла, остановленное ядро не
// запускается (pending_stopped); «Применить» после правки другого правила
// сохраняет выбор.
func TestRouterSelectXray(t *testing.T) {
	ctx := context.Background()
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithSelection())
	e.addMyRaw(t, "socks5://127.0.0.1:9#sel-a", "socks5://127.0.0.1:10#sel-b")
	cat := e.catalog(t).Xray
	nodeA, nodeB := rtsNode(t, cat, "sel-a"), rtsNode(t, cat, "sel-b")

	auto, ok := rtGroupByName(e.groups(t), "Авто")
	if !ok {
		t.Fatal("нет группы «Авто»")
	}
	_, grp, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Выбор", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}

	// До «Применить» группы нет в применённом состоянии.
	if _, err := e.svc.Select(ctx, grp.ID, memberNode+nodeB.Ref); !errors.Is(err, ErrGroupNotApplied) {
		t.Errorf("выбор до «Применить»: %v, ожидалась group_not_applied", err)
	}
	e.takeAndApply(t)
	rev := e.rev(t)

	for _, c := range []struct {
		name, gid, member string
		want              error
	}{
		{"неизвестная группа", "0badc0de", memberDirect, ErrGroupNotApplied},
		{"группа не ручная", auto.ID, memberDirect, ErrGroupNotApplied},
		{"неизвестный узел", grp.ID, memberNode + "my/00000000", ErrMemberInvalid},
		{"неизвестная группа-участник", grp.ID, memberGroup + "0badc0de", ErrMemberInvalid},
		{"произвольная строка", grp.ID, "node:", ErrMemberInvalid},
	} {
		if _, err := e.svc.Select(ctx, c.gid, c.member); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, ожидалась %v", c.name, err, c.want)
		}
	}
	if sel := e.rtcSelection(t); len(sel) != 0 {
		t.Fatalf("отказанные выборы записались в Live: %v", sel)
	}

	res, err := e.svc.Select(ctx, grp.ID, memberNode+nodeB.Ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if res.Member != memberNode+nodeB.Ref {
		t.Errorf("Member = %q", res.Member)
	}
	if got := res.Kernels[configlayer.KernelXray]; got != selPendingStopped {
		t.Errorf("Kernels[xray] = %q, ожидалось %q (ядро окружения не запущено)", got, selPendingStopped)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selNotInstalled {
		t.Errorf("Kernels[mihomo] = %q, ожидалось %q", got, selNotInstalled)
	}
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeB.Ref {
		t.Fatalf("Live[selection] = %q, ожидался узел B", got)
	}
	if got := e.rev(t); got != rev {
		t.Errorf("выбор изменил ревизию черновика: %d → %d", rev, got)
	}

	// Исходящий группы — копия узла B (а не A).
	ob := rtsOutbound(t, e.xrayDir(), GroupTag(grp.ID))
	if !rtsSameOutbound(ob, rtsOutbound(t, e.xrayDir(), nodeB.XrayTag)) {
		t.Errorf("исходящий группы не совпадает с узлом B (адрес и порт)")
	}
	if rtsSameOutbound(ob, rtsOutbound(t, e.xrayDir(), nodeA.XrayTag)) {
		t.Errorf("исходящий группы совпадает с узлом A: выбор не дошёл до файла")
	}

	// «Применить» после правки другого правила сохраняет выбор.
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"direct.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	e.applyOK(t)
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeB.Ref {
		t.Errorf("после «Применить» выбор = %q, ожидался узел B", got)
	}
	if !rtsSameOutbound(rtsOutbound(t, e.xrayDir(), GroupTag(grp.ID)), rtsOutbound(t, e.xrayDir(), nodeB.XrayTag)) {
		t.Errorf("после «Применить» исходящий группы сменился")
	}
}

// rtsNotice возвращает уведомление слоя по идентификатору.
func (e *rtEnv) rtsNotice(id string) (configlayer.NoticeView, bool) {
	for _, n := range e.layer.Snapshot().Notices {
		if n.ID == id {
			return n, true
		}
	}
	return configlayer.NoticeView{}, false
}

// rtsReconcile вызывает сверку Mihomo, как это делает наблюдатель после запуска
// процесса, пока Clash API не ответит.
func (e *rtEnv) rtsReconcile(t *testing.T, ctx context.Context) {
	t.Helper()
	rtWaitCond(t, 40*time.Second, "сверка выбора Mihomo", func() bool {
		return e.svc.reconcileMihomo(ctx) == nil
	})
}

// rtsWaitNow ждёт, пока выбранный участник группы в процессе Mihomo станет want.
func rtsWaitNow(t *testing.T, core *rtMihomoCore, group, want string, wait time.Duration) {
	t.Helper()
	var last string
	rtWaitCond(t, wait, "выбор группы Mihomo", func() bool {
		now, err := core.svc.ProxyNow(context.Background(), group)
		last = now
		return err == nil && now == want
	})
	if last != want {
		t.Fatalf("now = %q, ожидалось %q", last, want)
	}
}

// Сверка now Mihomo с выбором панели: выбор ставится сразу (PUT /proxies),
// переживает переименование группы (D-14) и перезапуск процесса, выбор из cache.db
// не задерживает сверку после «Применить» (D-39). Отдельный процесс Mihomo в
// каталоге теста, ядро стенда не трогается.
func TestRouterSelectMihomoReconcile(t *testing.T) {
	ctx := context.Background()
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtWithSelection())
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:9#sel-a", "socks5://127.0.0.1:10#sel-b")
	cat := e.catalog(t).Mihomo
	nodeA, nodeB := rtsNode(t, cat, "sel-a"), rtsNode(t, cat, "sel-b")
	_, grp, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Выбор", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	e.takeAndApply(t)
	core.start(t, e)

	// Выбор B ставится в запущенном Mihomo сразу.
	res, err := e.svc.Select(ctx, grp.ID, memberNode+nodeB.Ref)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selApplied {
		t.Fatalf("Kernels[mihomo] = %q, ожидалось %q", got, selApplied)
	}
	if got := res.Kernels[configlayer.KernelXray]; got != selNotInstalled {
		t.Errorf("Kernels[xray] = %q, ожидалось %q", got, selNotInstalled)
	}
	rtsWaitNow(t, core, "Выбор", nodeB.MihomoName, 10*time.Second)

	// Переименование: «Применить» перезапускает процесс на новом профиле, в
	// cache.db выбор лежит под прежним именем — сверка возвращает B.
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{ID: grp.ID, Name: "Выбор 2", Mode: ModeManual, Sources: GroupSources{All: true}}); err != nil {
		t.Fatalf("переименование группы: %v", err)
	}
	core.stop()
	e.applyOK(t)
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeB.Ref {
		t.Fatalf("после «Применить» выбор = %q, ожидался узел B", got)
	}
	core.start(t, e)
	e.rtsReconcile(t, ctx)
	rtsWaitNow(t, core, "Выбор 2", nodeB.MihomoName, 10*time.Second)

	// Выбор A во внешнем PUT попадает в cache.db; перезапущенный процесс
	// восстанавливает его, а «Применить» возвращает выбор панели.
	if err := core.svc.SelectProxy(ctx, "Выбор 2", nodeA.MihomoName); err != nil {
		t.Fatalf("внешний PUT: %v", err)
	}
	core.stop()
	core.start(t, e)
	restored := false
	rtWaitCond(t, 20*time.Second, "Clash API после перезапуска", func() bool {
		now, err := core.svc.ProxyNow(ctx, "Выбор 2")
		restored = now == nodeA.MihomoName
		return err == nil
	})
	t.Logf("store-selected восстановил выбор A из cache.db: %s", rtxYesNo(restored))
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"direct.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	e.applyOK(t)
	rtsWaitNow(t, core, "Выбор 2", nodeB.MihomoName, 40*time.Second)
	if _, ok := e.rtsNotice(noticeReconcilePrefix + grp.ID); ok {
		t.Errorf("после успешной сверки осталось уведомление routing_reconcile")
	}
}

// Пропавший выбранный узел заменяется первым живым: фиксатор записывает первого
// по порядку и маркер, затем проверка узлов решает. Без запущенного Mihomo узлы
// проверяет TLSPing (способ tls, мёртвые узлы не отвечают, остаётся первый по
// порядку); с запущенным — delay API Mihomo, выбирается живой узел. DIRECT сам не
// выбирается.
func TestRouterSelectRepick(t *testing.T) {
	ctx := context.Background()

	t.Run("tls_none_answered", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithSelection(), rtWithDeps(func(d *Deps) { d.Ping = rtTLSPing }))
		e.addMyRaw(t, "socks5://127.0.0.1:9#rep-a", "socks5://127.0.0.1:10#rep-b", "socks5://127.0.0.1:11#rep-c")
		cat := e.catalog(t).Xray
		nodeA, nodeB := rtsNode(t, cat, "rep-a"), rtsNode(t, cat, "rep-b")
		_, grp, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Выбор", Mode: ModeManual, Sources: GroupSources{All: true}})
		if err != nil {
			t.Fatalf("SaveGroup: %v", err)
		}
		e.takeAndApply(t)
		if _, err := e.svc.Select(ctx, grp.ID, memberNode+nodeB.Ref); err != nil {
			t.Fatalf("Select: %v", err)
		}

		entryID := strings.TrimPrefix(nodeB.Ref, sources.MySourceID+"/")
		if _, err := e.svc.DeleteNode(e.rev(t), entryID); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		e.applyOK(t)

		var n configlayer.NoticeView
		rtWaitCond(t, 60*time.Second, "уведомление routing_repick", func() bool {
			var ok bool
			n, ok = e.rtsNotice(noticeRepickPrefix + grp.ID)
			return ok
		})
		if n.Params["answered"] != "false" || n.Params["method"] != repickMethodTLS || n.Params["group"] != "Выбор" {
			t.Errorf("параметры уведомления: %v, ожидалось answered=false method=tls group=Выбор", n.Params)
		}
		sel := e.rtcSelection(t)[grp.ID]
		if sel != memberNode+nodeA.Ref {
			t.Errorf("выбор после замены = %q, ожидался первый по порядку (узел A)", sel)
		}
		if sel == memberDirect {
			t.Errorf("панель сама выбрала DIRECT при пропаже узла")
		}
		rtWaitCond(t, 10*time.Second, "снятие маркера повторного выбора", func() bool {
			return len(readRepick(e.state(t).Live)) == 0
		})
	})

	t.Run("delay_live_node", func(t *testing.T) {
		nodePort, _ := rtStartNode(t)
		probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		t.Cleanup(probe.Close)

		core := rtNewMihomoCore(t)
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtWithSelection(), rtWithDeps(func(d *Deps) { d.Ping = rtTLSPing }))
		e.writeUserProfile(t)
		e.addMyRaw(t, "socks5://127.0.0.1:9#rep-dead1", "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rep-live", "socks5://127.0.0.1:10#rep-dead2")
		cat := e.catalog(t).Mihomo
		dead1, live, dead2 := rtsNode(t, cat, "rep-dead1"), rtsNode(t, cat, "rep-live"), rtsNode(t, cat, "rep-dead2")
		// Отдельные узлы (picks) пишутся копиями в proxies профиля: их имена
		// известны точно, delay API проверяет их как обычные прокси.
		_, grp, err := e.svc.SaveGroup(e.rev(t), Group{
			Name: "Выбор", Mode: ModeManual, Picks: []string{dead1.Ref, live.Ref, dead2.Ref},
		})
		if err != nil {
			t.Fatalf("SaveGroup: %v", err)
		}
		if err := e.svcSaveProbe(probe.URL + "/probe"); err != nil {
			t.Fatalf("SaveProbe: %v", err)
		}
		e.takeAndApply(t)
		core.start(t, e)
		if _, err := e.svc.Select(ctx, grp.ID, memberNode+dead2.Ref); err != nil {
			t.Fatalf("Select: %v", err)
		}

		entryID := strings.TrimPrefix(dead2.Ref, sources.MySourceID+"/")
		if _, err := e.svc.DeleteNode(e.rev(t), entryID); err != nil {
			t.Fatalf("DeleteNode: %v", err)
		}
		e.applyOK(t)

		var n configlayer.NoticeView
		rtWaitCond(t, 90*time.Second, "уведомление routing_repick", func() bool {
			var ok bool
			n, ok = e.rtsNotice(noticeRepickPrefix + grp.ID)
			return ok
		})
		if n.Params["answered"] != "true" || n.Params["method"] != repickMethodDelay || n.Params["node"] != "rep-live" {
			t.Errorf("параметры уведомления: %v, ожидалось answered=true method=delay node=rep-live", n.Params)
		}
		if sel := e.rtcSelection(t)[grp.ID]; sel != memberNode+live.Ref {
			t.Errorf("выбор после замены = %q, ожидался живой узел", sel)
		}
		rtsWaitNow(t, core, "Выбор", live.MihomoName, 20*time.Second)
	})
}

// svcSaveProbe задаёт адрес проверки узлов (интервал и таймаут не по умолчанию,
// чтобы проверка укладывалась в сценарий).
func (e *rtEnv) svcSaveProbe(url string) error {
	rev, err := e.layer.ReadState()
	if err != nil {
		return err
	}
	_, err = e.svc.SaveProbe(rev.DraftRevision, ProbeConfig{URL: url, IntervalS: 10, TimeoutS: 5})
	return err
}

// Наблюдатель выбора: выбор панели ставится в каждый новый процесс Mihomo.
// Остановленный процесс получает pending_stopped; новый процесс в том же каталоге
// восстанавливает из cache.db прежний выбор (store-selected), и наблюдатель
// возвращает выбор панели. Внешний PUT при том же процессе наблюдатель не
// перетирает. Тот же путь проходит переключение ядра с Xray на Mihomo: новый
// процесс. Ядро стенда не трогается (отдельный процесс в каталоге теста).
func TestRouterSelectMihomoStart(t *testing.T) {
	ctx := context.Background()
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtWithSelection())
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:9#sel-a", "socks5://127.0.0.1:10#sel-b")
	cat := e.catalog(t).Mihomo
	nodeA, nodeB := rtsNode(t, cat, "sel-a"), rtsNode(t, cat, "sel-b")
	_, grp, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Выбор", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	e.takeAndApply(t)

	// Первый процесс: выбор A, store-selected пишет его в cache.db каталога теста.
	core.start(t, e)
	res, err := e.svc.Select(ctx, grp.ID, memberNode+nodeA.Ref)
	if err != nil {
		t.Fatalf("Select (A): %v", err)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selApplied {
		t.Fatalf("Kernels[mihomo] = %q, ожидалось %q", got, selApplied)
	}
	rtsWaitNow(t, core, "Выбор", nodeA.MihomoName, 10*time.Second)

	w := NewSelectionWatcher(e.svc, core.pidOf, time.Second)
	w.Start()
	t.Cleanup(w.Stop)

	// Остановка процесса: выбор B записывается, Mihomo получит его при запуске.
	core.stop()
	rtWaitCond(t, 10*time.Second, "остановка процесса Mihomo", func() bool { return core.pid() == 0 })
	res, err = e.svc.Select(ctx, grp.ID, memberNode+nodeB.Ref)
	if err != nil {
		t.Fatalf("Select (B, ядро остановлено): %v", err)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selPendingStopped {
		t.Errorf("Kernels[mihomo] при остановленном ядре = %q, ожидалось %q", got, selPendingStopped)
	}
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeB.Ref {
		t.Fatalf("Live[selection] = %q, ожидался узел B", got)
	}

	// Второй процесс в том же каталоге: cache.db восстанавливает A, наблюдатель
	// возвращает B в пределах 15 секунд.
	core.start(t, e)
	rtsWaitNow(t, core, "Выбор", nodeB.MihomoName, 15*time.Second)

	// Внешний выбор при том же процессе наблюдатель не перетирает: сверка идёт
	// только при новом процессе, после «Применить» и после сбоя выбора.
	if err := core.svc.SelectProxy(ctx, "Выбор", nodeA.MihomoName); err != nil {
		t.Fatalf("внешний PUT: %v", err)
	}
	overwritten := false
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		if now, err := core.svc.ProxyNow(ctx, "Выбор"); err == nil && now != nodeA.MihomoName {
			overwritten = true
			break
		}
	}
	if overwritten {
		t.Errorf("наблюдатель перетёр внешний выбор при том же процессе")
	}

	w.Stop()
	w.Stop()
}
