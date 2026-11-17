//go:build router

// router-area: routing

package routing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/proxyping"
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
	_, outer, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Внешняя", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: grp.ID}},
	})
	if err != nil {
		t.Fatal("runtime_outer_save")
	}
	_, direct, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Прямо", Mode: ModeManual, Members: []Member{{Kind: KindDirect}},
	})
	if err != nil {
		t.Fatal("runtime_direct_save")
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
	if readUnconfirmed(e.state(t).Live)[grp.ID] != "" || res.RestartReason != "" || res.Reverted || len(res.Unconfirmed) != 0 {
		t.Fatalf("остановленное ядро: маркер=%t причина=%t возврат=%t неизвестно=%d", readUnconfirmed(e.state(t).Live)[grp.ID] != "", res.RestartReason != "", res.Reverted, len(res.Unconfirmed))
	}
	if err := e.layer.UpdateLive(func(live configlayer.Sections) error {
		return writeUnconfirmed(live, map[string]string{grp.ID: configlayer.KernelXray})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Select(ctx, grp.ID, memberNode+nodeA.Ref); !errors.Is(err, ErrSelectionUnconfirmed) {
		t.Fatalf("неподтверждённый выбор: ожидаемый код=%t", errors.Is(err, ErrSelectionUnconfirmed))
	}
	confirmed, err := e.svc.Confirm(ctx, grp.ID)
	if err != nil || !confirmed.Confirmed || confirmed.Group != grp.ID {
		t.Fatalf("Confirm: ошибка=%t подтверждён=%t группа=%t", err != nil, confirmed.Confirmed, confirmed.Group == grp.ID)
	}
	if readUnconfirmed(e.state(t).Live)[grp.ID] != "" {
		t.Fatal("Confirm: маркер остался")
	}
	// Файл уже совпадал с Live: Confirm обязан реально переприменить его,
	// а не снять маркер на основании nothing_to_apply.
	apply := e.layer.Snapshot().Apply.Result
	if apply == nil || !apply.OK || apply.Code != configlayer.ResultApplied || apply.Written == 0 {
		t.Fatal("Confirm: неизменившийся файл не применён повторно")
	}
	confirmed, err = e.svc.Confirm(ctx, grp.ID)
	if err != nil || !confirmed.Confirmed {
		t.Fatalf("Confirm без маркера: ошибка=%t подтверждён=%t", err != nil, confirmed.Confirmed)
	}
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeB.Ref {
		t.Fatalf("Live[selection] = %q, ожидался узел B", got)
	}
	if got := e.rev(t); got != rev {
		t.Errorf("выбор изменил ревизию черновика: %d → %d", rev, got)
	}
	view, err := e.svc.RuntimeView()
	if err != nil || view.State != RuntimeStateStopped || view.Running == nil || view.Modes.Xray != RuntimeModePanel || view.Modes.Mihomo != RuntimeModeNotInstalled {
		t.Fatalf("runtime_view: error=%t state=%s running_nil=%t modes=%+v", err != nil, view.State, view.Running == nil, view.Modes)
	}
	findRuntime := func(id string) RuntimeGroup {
		t.Helper()
		for _, g := range view.Groups {
			if g.ID == id {
				return g
			}
		}
		t.Fatal("runtime_group_missing")
		return RuntimeGroup{}
	}
	runtimeGroup := findRuntime(grp.ID)
	if runtimeGroup.Mode != string(ModeManual) || runtimeGroup.Current != memberNode+nodeB.Ref || runtimeGroup.CurrentLabel != nodeB.Display || !runtimeGroup.Selectable || runtimeGroup.Unconfirmed {
		t.Fatalf("runtime_selection: mode=%s current=%t label=%t selectable=%t unconfirmed=%t", runtimeGroup.Mode, runtimeGroup.Current == memberNode+nodeB.Ref, runtimeGroup.CurrentLabel == nodeB.Display, runtimeGroup.Selectable, runtimeGroup.Unconfirmed)
	}
	if !reflect.DeepEqual(runtimeGroup.LockedWith, []string{outer.ID}) || !reflect.DeepEqual(findRuntime(outer.ID).LockedWith, []string{grp.ID}) {
		t.Fatal("runtime_linked_component")
	}
	chain := findRuntime(outer.ID).Chain
	if len(chain) != 2 || chain[0].Kind != KindGroup || chain[0].ID != grp.ID || chain[0].Name != grp.Name || chain[1].Kind != "node" || chain[1].Ref != nodeB.Ref {
		t.Fatalf("runtime_chain: length=%d", len(chain))
	}
	nodes := 0
	for _, m := range runtimeGroup.Members {
		if m.Kind == "node" {
			nodes++
			if !m.Xray || m.Mihomo || m.Proto == "" || m.Source != sources.MySourceID {
				t.Fatal("runtime_node_compatibility")
			}
		}
	}
	if nodes != len(cat) || runtimeGroup.Counts.Xray == nil || *runtimeGroup.Counts.Xray != nodes || runtimeGroup.Counts.Mihomo != nil {
		t.Fatalf("runtime_counts: nodes=%d", nodes)
	}
	request, err := e.svc.PingRequest("group", grp.ID)
	if err != nil || request.Method != proxyping.MethodXray || len(request.Targets) != nodes || request.URL != view.Probe.URL || request.Timeout != time.Duration(view.Probe.TimeoutS)*time.Second {
		t.Fatalf("runtime_ping: error=%t method=%s targets=%d", err != nil, request.Method, len(request.Targets))
	}
	for _, target := range request.Targets {
		if target.Fingerprint == "" || !target.Xray {
			t.Fatal("runtime_ping_fingerprint")
		}
	}
	if _, err := e.svc.PingRequest("bad", grp.ID); err == nil {
		t.Fatal("runtime_ping_scope_accepted")
	} else {
		var input *InvalidInputError
		if !errors.As(err, &input) || input.Field != "scope" {
			t.Fatal("runtime_ping_scope_code")
		}
	}
	if _, err := e.svc.PingRequest("group", direct.ID); !errors.Is(err, ErrGroupNoNodes) {
		t.Fatalf("runtime_ping_empty: expected_code=%t", errors.Is(err, ErrGroupNoNodes))
	}
	if _, err := e.svc.PingRequest("group", "0badc0de"); !errors.Is(err, ErrGroupNotFound) {
		t.Fatal("runtime_ping_unknown_group")
	}
	all, err := e.svc.PingRequest("all", "")
	if err != nil || len(all.Targets) != nodes {
		t.Fatalf("runtime_ping_all: error=%t targets=%d", err != nil, len(all.Targets))
	}
	fingerprints, err := e.svc.NodeFingerprints()
	if err != nil || len(fingerprints) != nodes {
		t.Fatalf("runtime_fingerprints: error=%t count=%d", err != nil, len(fingerprints))
	}
	for _, target := range all.Targets {
		if fingerprints[target.Ref] != target.Fingerprint {
			t.Fatal("runtime_fingerprint_mismatch")
		}
	}
	now, err := e.svc.RuntimeNow(ctx)
	if err != nil || now.Groups == nil || now.History == nil || len(now.Groups) != 0 || len(now.History) != 0 {
		t.Fatal("runtime_stopped_now")
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
	// Неприменённые правки не меняют вид работающих групп и порядок.
	e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{} })
	after, err := e.svc.RuntimeView()
	if err != nil || !reflect.DeepEqual(view.Groups, after.Groups) {
		t.Fatal("runtime_read_draft")
	}
}

// Варианты «Сейчас» и проверка Select идут по составу группы (D-38): узел вне
// фильтра «включить» не предлагается и отвергается member_invalid, выбор ядра
// не меняется; узел из фильтра выбирается.
func TestRouterSelectOutsideComposition(t *testing.T) {
	ctx := context.Background()
	e := rtNewEnv(t, rtWithSelection())
	e.addMyRaw(t, "socks5://127.0.0.1:9#scope-in-a", "socks5://127.0.0.1:10#scope-out-b")
	cat := e.catalog(t).Xray
	nodeIn, nodeOut := rtsNode(t, cat, "scope-in-a"), rtsNode(t, cat, "scope-out-b")

	_, grp, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Фильтр", Mode: ModeManual,
		Sources: GroupSources{All: true}, Include: []string{"scope-in"},
	})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	e.takeAndApply(t)

	opts := rtViewGroup(t, e.view(t), "Фильтр").MemberOptions
	has := func(member string) bool {
		for _, o := range opts {
			if o.Member == member {
				return true
			}
		}
		return false
	}
	if has(memberNode + nodeOut.Ref) {
		t.Errorf("вариант вне состава группы: узел вне фильтра «включить» среди member_options (%d вариантов)", len(opts))
	}
	if !has(memberNode + nodeIn.Ref) {
		t.Errorf("вариант состава группы пропал: узла из фильтра нет среди member_options (%d вариантов)", len(opts))
	}

	if _, err := e.svc.Select(ctx, grp.ID, memberNode+nodeOut.Ref); !errors.Is(err, ErrMemberInvalid) {
		t.Errorf("выбор вне состава принят: %v, ожидалась member_invalid", err)
	}
	if sel := e.rtcSelection(t); len(sel) != 0 {
		t.Fatalf("отвергнутый выбор записался в Live: %v", sel)
	}

	if _, err := e.svc.Select(ctx, grp.ID, memberNode+nodeIn.Ref); err != nil {
		t.Fatalf("Select узла из фильтра: %v", err)
	}
	if got := e.rtcSelection(t)[grp.ID]; got != memberNode+nodeIn.Ref {
		t.Errorf("Live[selection] = %q, ожидался узел из фильтра", got)
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
	proxies, err := core.svc.ProxiesState(ctx)
	if err != nil {
		t.Fatalf("ProxiesState: %v", err)
	}
	if now := proxies["Выбор"].Now; now != nodeB.MihomoName {
		t.Errorf("ProxiesState: выбранный узел совпал=%t, длина now=%d", now == nodeB.MihomoName, len(now))
	}
	if proxies[nodeB.MihomoName].Type == "" {
		t.Error("ProxiesState: тип выбранного узла пуст")
	}

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

// rtsCurrent — current группы в виде «Прокси» (RuntimeView).
func (e *rtEnv) rtsCurrent(t *testing.T, gid string) string {
	t.Helper()
	v, err := e.svc.RuntimeView()
	if err != nil {
		t.Fatalf("RuntimeView: %v", err)
	}
	for _, g := range v.Groups {
		if g.ID == gid {
			return g.Current
		}
	}
	t.Fatalf("в виде «Прокси» нет группы %s", gid)
	return ""
}

// rtsGroup — ручная группа сценария быстрого пути: ID и имя (оно же имя группы в
// профиле Mihomo).
type rtsGroup struct{ id, name string }

// rtsFast — окружение быстрого пути выбора: свой процесс Mihomo в каталоге теста
// запущен, Xray установлен и остановлен, две ручные группы на узлах mf-a и mf-b
// применены, выбор в Mihomo сверен с Live. Ядро стенда не трогается.
type rtsFast struct {
	e          *rtEnv
	core       *rtMihomoCore
	refs       []string
	xrayTag    map[string]string
	mihomoName map[string]string
	groups     [2]rtsGroup
}

// rtsNewFast готовит окружение быстрого пути с группами names.
func rtsNewFast(t *testing.T, names [2]string) *rtsFast {
	t.Helper()
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithMihomoCore(core), rtWithSelection())
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:9#mf-a", "socks5://127.0.0.1:10#mf-b")
	cat := e.catalog(t)
	f := &rtsFast{e: e, core: core, xrayTag: map[string]string{}, mihomoName: map[string]string{}}
	for _, d := range []string{"mf-a", "mf-b"} {
		x, m := rtsNode(t, cat.Xray, d), rtsNode(t, cat.Mihomo, d)
		f.refs = append(f.refs, x.Ref)
		f.xrayTag[memberNode+x.Ref], f.mihomoName[memberNode+m.Ref] = x.XrayTag, m.MihomoName
	}
	for i, name := range names {
		_, g, err := e.svc.SaveGroup(e.rev(t), Group{Name: name, Mode: ModeManual, Sources: GroupSources{All: true}})
		if err != nil {
			t.Fatalf("SaveGroup №%d: %v", i+1, err)
		}
		f.groups[i] = rtsGroup{g.ID, name}
	}
	e.takeAndApply(t)
	core.start(t, e)
	e.rtsReconcile(t, context.Background())
	return f
}

// other — узел, отличный от current группы.
func (f *rtsFast) other(t *testing.T, gid string) string {
	t.Helper()
	cur := f.e.rtsCurrent(t, gid)
	for _, r := range f.refs {
		if memberNode+r != cur {
			return memberNode + r
		}
	}
	t.Fatalf("у группы %s нет узла, отличного от текущего", gid)
	return ""
}

// settle ждёт снятия отметок обеих групп (конец фоновых проходов).
func (f *rtsFast) settle(t *testing.T) {
	t.Helper()
	rtWaitCond(t, 60*time.Second, "конец фоновых проходов выбора", func() bool {
		k := f.e.svc.inflightKinds()
		return k[f.groups[0].id] == "" && k[f.groups[1].id] == ""
	})
}

// rtsExpectFast проверяет ответ быстрого пути: Mihomo applied, Xray pending_stopped.
func rtsExpectFast(t *testing.T, label string, res SelectResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selApplied {
		t.Errorf("%s: Kernels[mihomo] = %q, ожидалось %q", label, got, selApplied)
	}
	if got := res.Kernels[configlayer.KernelXray]; got != selPendingStopped {
		t.Errorf("%s: Kernels[xray] = %q, ожидалось %q", label, got, selPendingStopped)
	}
}

// Быстрый путь выбора при запущенном Mihomo и установленном, но остановленном Xray
// (план 150-19, SC2): ответ после Clash API, xcp-groups дописывает фоновый проход.
// Вид сразу показывает выбранный узел, now процесса — тот же узел; сверка Mihomo,
// вызванная, пока фоновый проход независимой группы ждёт хода и Live группы ещё
// прежний, не возвращает прежний узел (группа с идущим выбором пропускается). После
// проходов Live, xcp-groups и now сходятся, уведомлений routing_select: и маркеров
// нет. Отдельный процесс Mihomo в каталоге теста, ядро стенда не трогается.
func TestRouterSelectMihomoFirst(t *testing.T) {
	ctx := context.Background()
	f := rtsNewFast(t, [2]string{"Ранний 1", "Ранний 2"})
	e, core, xrayTag, mihomoName := f.e, f.core, f.xrayTag, f.mihomoName
	g1, g2 := f.groups[0], f.groups[1]
	other := func(gid string) string { return f.other(t, gid) }
	settle := func() { f.settle(t) }
	expectFast := func(label string, res SelectResult, err error) {
		t.Helper()
		rtsExpectFast(t, label, res, err)
	}

	executed, attempts, livePrev, nowKept, repending := false, 0, false, false, false
	var want1, want2 string
	for attempts < 3 && !executed {
		attempts++
		settle()
		want1, want2 = other(g1.id), other(g2.id)

		// Шаг 1: ранний ответ, вид и now процесса.
		res, err := e.svc.Select(ctx, g1.id, want1)
		expectFast("выбор группы 1", res, err)
		if cur := e.rtsCurrent(t, g1.id); cur != want1 {
			t.Errorf("попытка %d: current группы 1 сразу после ответа не равен выбранному узлу", attempts)
		}
		rtsWaitNow(t, core, g1.name, mihomoName[want1], 5*time.Second)

		// Шаг 2: выбор независимой группы, пока фоновый проход группы 1 держит ход.
		res, err = e.svc.Select(ctx, g2.id, want2)
		expectFast("выбор группы 2", res, err)
		markBefore := e.svc.inflightKinds()[g2.id] == inflightSelect
		liveBefore := e.rtcSelection(t)[g2.id]
		// Наблюдатель в тесте не работает: флаг повтора сверки читается напрямую.
		e.svc.reconcilePending.Swap(false)
		if err := e.svc.reconcileMihomo(ctx); err != nil {
			t.Fatalf("попытка %d: сверка Mihomo: %v", attempts, err)
		}
		repending = e.svc.reconcilePending.Load()
		now, err := core.svc.ProxyNow(ctx, g2.name)
		if err != nil {
			t.Fatalf("попытка %d: ProxyNow: %v", attempts, err)
		}
		markAfter := e.svc.inflightKinds()[g2.id] == inflightSelect
		liveAfter := e.rtcSelection(t)[g2.id]
		livePrev = liveBefore != want2 && liveAfter != want2
		nowKept = now == mihomoName[want2]
		executed = markBefore && markAfter && livePrev
		if executed && !nowKept {
			t.Errorf("попытка %d: сверка во время фонового прохода вернула в Mihomo прежний узел группы 2", attempts)
		}
		if executed && !repending {
			t.Errorf("попытка %d: сверка пропустила группу с идущим выбором и не запросила повтор", attempts)
		}
	}
	t.Logf("mihomo-reconcile-skip: executed=%t attempts=%d live_prev=%t now_kept=%t repending=%t", executed, attempts, livePrev, nowKept, repending)
	if !executed {
		t.Errorf("окно сверки не поймано: за %d попыток фоновый проход группы 2 не ждал хода во время сверки", attempts)
	}

	// После фоновых проходов Live, xcp-groups и now сходятся.
	settle()
	sel := e.rtcSelection(t)
	for _, c := range []struct {
		g    rtsGroup
		want string
	}{{g1, want1}, {g2, want2}} {
		if sel[c.g.id] != c.want {
			t.Errorf("Live группы %s не равен выбранному узлу", c.g.id)
		}
		if !rtsSameOutbound(rtsOutbound(t, e.xrayDir(), GroupTag(c.g.id)), rtsOutbound(t, e.xrayDir(), xrayTag[c.want])) {
			t.Errorf("исходящий группы %s в xcp-groups не совпадает с выбранным узлом", c.g.id)
		}
		rtsWaitNow(t, core, c.g.name, mihomoName[c.want], 10*time.Second)
	}
	for _, n := range e.layer.Snapshot().Notices {
		if strings.HasPrefix(n.ID, noticeSelectPrefix) {
			t.Errorf("после проходов осталось уведомление routing_select: (%s)", n.Params["code"])
		}
	}
	if m := readUnconfirmed(e.state(t).Live); len(m) != 0 {
		t.Errorf("после проходов остались маркеры selection_unconfirmed: %d", len(m))
	}
}

// Внесение сбоя в быстрый путь выбора (план 150-32) на своём процессе Mihomo в
// каталоге теста, без подмены функций сервиса: сбой создают настоящий процесс
// (SIGSTOP), настоящий замок применения и убранный сокет контроллера.
//
// put_timeout_after_accept (WR-02): процесс остановлен, замок применения занят.
// Ранний PUT отказывает по сроку, синхронный путь не пишет выбор и отвечает
// занятостью; Live и маркеры прежние, сверка Mihomo запрошена. После SIGCONT ядро,
// принявшее PUT уже после таймаута клиента, сходится к Live.
//
// tail_reput_unreachable (пробел 3, D-17): сразу после ответа быстрого пути сокет
// контроллера убирается, пока фоновый проход держит отметку группы. Успешный
// хвост не повторяет PUT подтверждённого значения: маркера selection_unconfirmed и
// уведомления routing_select: нет, Live и xcp-groups — цель; новый процесс после
// сверки показывает цель.
//
// revert_unreachable (WR-03, план 150-35): дрейф управляемого файла Xray вне
// xcp-groups, выбор быстрым путём, сразу — убранный сокет контроллера. Фоновый
// проход отказывает, Live возвращается к прежнему узлу, а Mihomo выбор из Live не
// принимает: маркер Mihomo у группы (D-16), уведомление routing_select: с
// reason=rejected и reverted=false — «Прежний узел возвращён» было бы ложью.
//
// repick_fast_path (WR-01 ревью 2, план 150-35): дрейф управляемого файла Xray,
// маркер повторного выбора у группы и настоящий repickOne (тот же вход, что
// repickAll после «Применить»). Проход отказывает, поэтому замена не поставлена:
// маркер повторного выбора остаётся для следующего применения, уведомления
// routing_repick: нет, Live прежний. После снятия дрейфа повторный repickOne
// ставит первый по порядку узел (оба узла — мёртвые адреса, никто не отвечает).
//
// Ядро стенда не трогается, метка ядра не нужна. Тест не ссылается на символы,
// которых нет в продукте до исправления: проход mf-before собирает его с прежними
// selection.go и selection_gate.go.
func TestRouterSelectMihomoFaults(t *testing.T) {
	routertest.Tier(t, routertest.Full, "внесение сбоя: остановка своего процесса Mihomo и убранный сокет контроллера посреди быстрого пути выбора")
	ctx := context.Background()
	f := rtsNewFast(t, [2]string{"Сбой 1", "Сбой 2"})
	e, core := f.e, f.core
	g := f.groups[0]

	t.Run("put_timeout_after_accept", func(t *testing.T) {
		f.settle(t)
		want := f.other(t, g.id)
		livePrev := e.rtcSelection(t)[g.id]
		nowPrev, err := core.svc.ProxyNow(ctx, g.name)
		if err != nil {
			t.Fatalf("ProxyNow до сбоя: %v", err)
		}
		// Наблюдатель в тесте не работает: флаг сверки читается напрямую.
		e.svc.reconcilePending.Swap(false)
		release, err := e.layer.BeginExclusive(ctx, false, configlayer.HolderGeodataWrite)
		if err != nil {
			t.Fatalf("замок применения: %v", err)
		}
		t.Cleanup(release)
		pid := core.pid()
		if pid == 0 {
			t.Fatalf("процесс Mihomo теста не запущен")
		}
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			t.Fatalf("SIGSTOP: %v", err)
		}
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGCONT) })

		_, err = e.svc.Select(context.Background(), g.id, want)
		busy := "none"
		switch {
		case errors.Is(err, ErrApplyBusy):
			busy = "apply_busy"
		case errors.Is(err, configlayer.ErrGeodataBusy):
			busy = "geodata_busy"
		default:
			code, _ := selectFailClass(err, "")
			t.Errorf("Select при занятом замке применения: код %s, ожидалась ошибка занятости", code)
		}
		if got := e.rtcSelection(t)[g.id]; got != livePrev {
			t.Errorf("Live группы изменился, хотя выбор не записан")
		}
		if m := readUnconfirmed(e.state(t).Live); m[g.id] != "" {
			t.Errorf("у группы маркер selection_unconfirmed=%s, хотя выбор не записан", m[g.id])
		}
		requested := e.svc.reconcilePending.Load()
		if !requested {
			t.Errorf("WR-02: после неудачного раннего PUT сверка Mihomo не запрошена")
		}

		release()
		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
			t.Fatalf("SIGCONT: %v", err)
		}
		rtWaitCond(t, 20*time.Second, "Clash API после SIGCONT", func() bool {
			_, err := core.svc.ProxyNow(ctx, g.name)
			return err == nil
		})
		// Запросы, принятые сокетом до SIGCONT, процесс может разобрать уже после
		// ответа на первое чтение: now читается после короткой паузы.
		time.Sleep(time.Second)
		now, _ := core.svc.ProxyNow(ctx, g.name)
		lateApplied := now == f.mihomoName[want]
		if requested {
			e.rtsReconcile(t, ctx)
		}
		converged, _ := routertest.Poll(10*time.Second, 200*time.Millisecond, func() (bool, string) {
			now, err := core.svc.ProxyNow(ctx, g.name)
			return err == nil && now == nowPrev, ""
		})
		t.Logf("wr02: busy=%s reconcile_requested=%t early_applied_late=%t converged=%t", busy, requested, lateApplied, converged)
		if !converged {
			t.Errorf("WR-02: now группы в Mihomo не сошёлся с Live после неудачного раннего PUT (early_applied_late=%t)", lateApplied)
		}
	})

	t.Run("tail_reput_unreachable", func(t *testing.T) {
		sock := filepath.Join(core.dir, "c.sock")
		executed, attempts := false, 0
		var want string
		for attempts < 3 && !executed {
			attempts++
			f.settle(t)
			want = f.other(t, g.id)
			res, err := e.svc.Select(ctx, g.id, want)
			rtsExpectFast(t, "выбор быстрым путём", res, err)
			// Ответ пришёл после раннего PUT: контроллер убирается, пока фоновый
			// проход файла Xray ещё держит отметку группы.
			if err := os.Remove(sock); err != nil {
				t.Fatalf("сокет контроллера: %v", err)
			}
			executed = e.svc.inflightKinds()[g.id] == inflightSelect
			if !executed {
				// Хвост закончился до удаления сокета: контроллер возвращается, выбор
				// в Mihomo сверяется с Live, попытка повторяется.
				core.stop()
				core.start(t, e)
				e.rtsReconcile(t, ctx)
			}
		}
		marker := "none"
		if executed {
			f.settle(t)
			if k := readUnconfirmed(e.state(t).Live)[g.id]; k != "" {
				marker = k
			}
		}
		t.Logf("tail-reput: executed=%t attempts=%d marker=%s", executed, attempts, marker)
		if !executed {
			t.Errorf("окно хвоста не поймано: за %d попыток фоновый проход закончился до удаления сокета контроллера", attempts)
			return
		}
		if marker != "none" {
			t.Errorf("D-17: после успешного выбора у группы маркер selection_unconfirmed=%s: хвост повторил PUT подтверждённого значения", marker)
		}
		for _, n := range e.layer.Snapshot().Notices {
			if strings.HasPrefix(n.ID, noticeSelectPrefix) {
				t.Errorf("после успешного выбора уведомление routing_select: (%s)", n.Params["code"])
			}
		}
		if e.rtcSelection(t)[g.id] != want {
			t.Errorf("Live группы не равен выбранному узлу")
		}
		if !rtsSameOutbound(rtsOutbound(t, e.xrayDir(), GroupTag(g.id)), rtsOutbound(t, e.xrayDir(), f.xrayTag[want])) {
			t.Errorf("исходящий группы в xcp-groups не совпадает с выбранным узлом")
		}

		// Контроллер возвращается новым процессом: сверка ставит выбор из Live.
		core.stop()
		core.start(t, e)
		e.rtsReconcile(t, ctx)
		rtsWaitNow(t, core, g.name, f.mihomoName[want], 10*time.Second)
	})

	// Файл дрейфа ищется один раз: после возврата байтов его состояние в снимке слоя
	// становится ok только следующей проверкой файлов.
	driftPath := ""
	drift := func(t *testing.T) func() {
		t.Helper()
		if driftPath == "" {
			driftPath = rtsXrayDriftFile(t, e)
		}
		return rtsDriftFile(t, driftPath)
	}

	t.Run("revert_unreachable", func(t *testing.T) {
		sock := filepath.Join(core.dir, "c.sock")
		f.settle(t)
		livePrev := e.rtcSelection(t)[g.id]
		curPrev := e.rtsCurrent(t, g.id)
		restoreFile := drift(t)
		executed, attempts, marker := false, 0, "none"
		for attempts < 3 && !executed {
			attempts++
			f.settle(t)
			want := f.other(t, g.id)
			res, err := e.svc.Select(ctx, g.id, want)
			rtsExpectFast(t, "выбор быстрым путём при дрейфе", res, err)
			// Ответ пришёл после раннего PUT: контроллер убирается, пока фоновый
			// проход ещё держит отметку группы и Mihomo по Live не поставлен.
			if err := os.Remove(sock); err != nil {
				t.Fatalf("сокет контроллера: %v", err)
			}
			f.settle(t)
			marker = "none"
			if k := readUnconfirmed(e.state(t).Live)[g.id]; k != "" {
				marker = k
			}
			// Сценарий исполнен, только если хвост не дошёл до Mihomo до удаления
			// сокета: ранний PUT прошёл, значит, отказ постановки по Live и маркер
			// Mihomo у группы — следствие убранного сокета.
			executed = marker == configlayer.KernelMihomo
			if !executed {
				// Хвост поставил Mihomo по Live до удаления сокета: контроллер
				// возвращается, попытка повторяется.
				core.stop()
				core.start(t, e)
				e.rtsReconcile(t, ctx)
			}
		}
		var note configlayer.NoticeView
		noted := false
		for _, n := range e.layer.Snapshot().Notices {
			if strings.HasPrefix(n.ID, noticeSelectPrefix+g.id+":") {
				note, noted = n, true
			}
		}
		reverted, reason := "none", "none"
		if noted {
			reverted, reason = note.Params["reverted"], note.Params["reason"]
		}
		t.Logf("wr03: executed=%t attempts=%d reverted=%s reason=%s marker=%s", executed, attempts, reverted, reason, marker)
		if !executed {
			t.Errorf("окно хвоста не поймано: за %d попыток Mihomo получил выбор из Live до удаления сокета контроллера", attempts)
		} else {
			if got := e.rtcSelection(t)[g.id]; got != livePrev {
				t.Errorf("Live группы не вернулся к прежнему узлу после отказа прохода")
			}
			if !noted {
				t.Errorf("WR-03: нет уведомления routing_select: группы после отказа фонового прохода")
			} else {
				if reason != selReasonRejected {
					t.Errorf("WR-03: класс причины уведомления %q, ожидался %q (дрейф)", reason, selReasonRejected)
				}
				if reverted != "false" {
					t.Errorf("WR-03: уведомление при невозвращённом выборе (маркер Mihomo, ядро на цели) несёт reverted=%q, ожидалось \"false\"", reverted)
				}
			}
		}

		// Файл возвращается, контроллер — новым процессом: сверка ставит прежний
		// узел и снимает маркер.
		restoreFile()
		core.stop()
		core.start(t, e)
		e.rtsReconcile(t, ctx)
		if name := f.mihomoName[curPrev]; name != "" {
			rtsWaitNow(t, core, g.name, name, 10*time.Second)
		}
		if m := readUnconfirmed(e.state(t).Live); len(m) != 0 {
			t.Errorf("после сверки остались маркеры selection_unconfirmed: %d", len(m))
		}
	})

	t.Run("repick_fast_path", func(t *testing.T) {
		f.settle(t)
		// Ожидаемая замена — первый узел группы по порядку участников: проверка
		// мёртвых узлов не отвечает, pickAlive оставляет первого (answered=false).
		am, err := e.svc.appliedModelOf(e.state(t))
		if err != nil {
			t.Fatalf("repick_setup: применённая модель: %v", err)
		}
		grp, ok := am.group(g.id)
		if !ok {
			t.Fatalf("repick_setup: нет группы в применённой модели")
		}
		first := ""
		for _, c := range am.candidates(grp) {
			if strings.HasPrefix(c, memberNode) {
				first = c
				break
			}
		}
		if first == "" || f.mihomoName[first] == "" {
			t.Fatalf("repick_setup: у группы нет первого узла с именем в Mihomo")
		}
		display := map[string]string{}
		for _, n := range e.catalog(t).Mihomo {
			display[memberNode+n.Ref] = n.Display
		}
		// Текущий выбор — не первый по порядку: иначе повторному выбору нечего менять.
		if e.rtcSelection(t)[g.id] == first {
			res, err := e.svc.Select(ctx, g.id, f.other(t, g.id))
			rtsExpectFast(t, "repick_setup: выбор второго узла", res, err)
			f.settle(t)
		}
		livePrev := e.rtcSelection(t)[g.id]
		if livePrev == first || livePrev == "" {
			t.Fatalf("repick_setup: текущий выбор группы — первый узел или пуст")
		}
		if _, ok := e.rtsNotice(noticeRepickPrefix + g.id); ok {
			t.Fatalf("repick_setup: уведомление routing_repick: группы уже есть до сценария")
		}

		restoreFile := drift(t)
		if err := e.layer.UpdateLive(func(live configlayer.Sections) error {
			return writeRepick(live, append(without(readRepick(live), g.id), g.id))
		}); err != nil {
			t.Fatalf("repick_setup: маркер повторного выбора: %v", err)
		}
		t.Cleanup(func() { e.svc.clearRepick(g.id) })

		// Тот же вход, что repickAll после «Применить»: проход файла Xray отказывает
		// (дрейф), замена не поставлена.
		e.svc.repickOne(ctx, g.id)
		markerKept := e.rtsHasRepick(t, g.id)
		note, noticed := e.rtsNotice(noticeRepickPrefix + g.id)
		if noticed && note.Params["node"] == display[livePrev] {
			t.Fatalf("repick_setup: повторный выбор оставил текущий узел — замена не пробовалась")
		}
		if !markerKept {
			t.Errorf("WR-01: маркер повторного выбора снят до исхода прохода: отказ прохода не оставил его для следующего применения")
		}
		if noticed {
			t.Errorf("WR-01: замена объявлена до исхода прохода: уведомление routing_repick: при отказе прохода файла Xray")
		}
		f.settle(t)
		liveKept := e.rtcSelection(t)[g.id] == livePrev
		if !liveKept {
			t.Errorf("Live группы после отказа повторного выбора не равен прежнему узлу")
		}

		// Без дрейфа повторный выбор ставит первый узел и снимает маркер.
		restoreFile()
		e.svc.repickOne(ctx, g.id)
		f.settle(t)
		_, noticed2 := e.rtsNotice(noticeRepickPrefix + g.id)
		markerGone := !e.rtsHasRepick(t, g.id)
		liveFirst := e.rtcSelection(t)[g.id] == first
		secondOK := noticed2 && markerGone && liveFirst
		t.Logf("wr01: marker_kept=%t repick_notice=%t live_prev=%t second_ok=%t", markerKept, noticed, liveKept, secondOK)
		if !markerGone {
			t.Errorf("повторный выбор без дрейфа не снял маркер повторного выбора")
		}
		if !noticed2 {
			t.Errorf("повторный выбор без дрейфа не опубликовал уведомление routing_repick:")
		}
		if !liveFirst {
			t.Errorf("Live группы после повторного выбора не равен первому узлу по порядку")
		}
		rtsWaitNow(t, core, g.name, f.mihomoName[first], 10*time.Second)
	})
}

// rtsHasRepick — группа gid в маркере повторного выбора Live.
func (e *rtEnv) rtsHasRepick(t *testing.T, gid string) bool {
	t.Helper()
	for _, id := range readRepick(e.state(t).Live) {
		if id == gid {
			return true
		}
	}
	return false
}

// rtsXrayDriftFile — управляемый панелью файл Xray в каталоге теста вне xcp-groups
// в состоянии ok (по снимку слоя): его правка — дрейф, на котором проход выбора
// отказывает.
func rtsXrayDriftFile(t *testing.T, e *rtEnv) string {
	t.Helper()
	for _, fv := range e.layer.Snapshot().Files {
		if fv.Kernel != configlayer.KernelXray || fv.Owner != configlayer.OwnerPanel || fv.State != configlayer.StateOK ||
			filepath.Dir(fv.Path) != e.xrayDir() || strings.Contains(filepath.Base(fv.Path), "xcp-groups") {
			continue
		}
		if _, err := os.Stat(fv.Path); err == nil {
			return fv.Path
		}
	}
	t.Fatalf("нет управляемого файла Xray вне xcp-groups в состоянии ok")
	return ""
}

// rtsDriftFile дописывает перевод строки в файл path (дрейф) и возвращает возврат
// исходных байтов; возврат повторно не пишет и вызывается ещё и в t.Cleanup.
func rtsDriftFile(t *testing.T, path string) func() {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("файл дрейфа: %v", err)
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл дрейфа не читается: %v", err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		if err := os.WriteFile(path, orig, info.Mode().Perm()); err != nil {
			t.Errorf("файл дрейфа не возвращён: %v", err)
		}
	}
	t.Cleanup(restore)
	if err := os.WriteFile(path, append(append([]byte(nil), orig...), '\n'), info.Mode().Perm()); err != nil {
		t.Fatalf("дрейф файла Xray: %v", err)
	}
	return restore
}

// Связанные группы Mihomo (WR-03, WR-04): маркер сбоя выбора стоит только на группе со
// сбоем и не блокирует выбор в связанной группе; снятие отсутствующего маркера не пишет
// состояние и не публикует событие; сверка снимает маркеры подтверждённых групп одной
// записью. Сбой выбора — убранный сокет контроллера при живом процессе. Отдельный процесс
// Mihomo в каталоге теста, ядро стенда не трогается.
func TestRouterSelectMihomoLinked(t *testing.T) {
	ctx := context.Background()
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core), rtWithSelection())
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:9#lnk-a", "socks5://127.0.0.1:10#lnk-b")
	nodeB := rtsNode(t, e.catalog(t).Mihomo, "lnk-b")
	_, inner, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Внутр", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup inner: %v", err)
	}
	_, outer, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Внеш", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: inner.ID}}})
	if err != nil {
		t.Fatalf("SaveGroup outer: %v", err)
	}
	e.takeAndApply(t)
	core.start(t, e)
	comp := []string{inner.ID, outer.ID}
	markers := func() map[string]string {
		st, err := e.layer.ReadState()
		if err != nil {
			t.Fatalf("ReadState: %v", err)
		}
		return readUnconfirmed(st.Live)
	}

	// WR-04: маркеров нет — снятие не пишет состояние и не публикует событие proxies.
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()
	if err := e.svc.clearUnconfirmed(comp, configlayer.KernelMihomo); err != nil {
		t.Fatalf("clearUnconfirmed без маркера: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Type == configlayer.EventProxies {
			t.Errorf("снятие отсутствующего маркера опубликовало событие %q", ev.Type)
		}
	case <-time.After(500 * time.Millisecond):
	}

	// Сбой выбора во внутренней группе: маркер только на ней.
	if err := os.Remove(filepath.Join(core.dir, "c.sock")); err != nil {
		t.Fatalf("сокет контроллера: %v", err)
	}
	res, err := e.svc.Select(ctx, inner.ID, memberNode+nodeB.Ref)
	if err != nil {
		t.Fatalf("Select внутренней: %v", err)
	}
	if got := res.Kernels[configlayer.KernelMihomo]; got != selFailed {
		t.Fatalf("Kernels[mihomo] = %q, ожидалось %q", got, selFailed)
	}
	if m := markers(); m[inner.ID] != configlayer.KernelMihomo || m[outer.ID] != "" {
		t.Fatalf("маркеры после сбоя: внутренняя=%q внешняя=%q", m[inner.ID], m[outer.ID])
	}

	// WR-03: связанная группа без своего маркера выбирается; сама помеченная — нет.
	if _, err := e.svc.Select(ctx, outer.ID, memberGroup+inner.ID); errors.Is(err, ErrSelectionUnconfirmed) {
		t.Fatalf("маркер Mihomo внутренней группы заблокировал связанную: %v", err)
	} else if err != nil {
		t.Fatalf("Select внешней: %v", err)
	}
	if _, err := e.svc.Select(ctx, inner.ID, memberNode+nodeB.Ref); !errors.Is(err, ErrSelectionUnconfirmed) {
		t.Fatalf("Select помеченной группы: err = %v, ожидалось ErrSelectionUnconfirmed", err)
	}

	// Процесс с контроллером возвращается: сверка снимает маркеры обеих групп.
	core.stop()
	core.start(t, e)
	e.rtsReconcile(t, ctx)
	if m := markers(); len(m) != 0 {
		t.Fatalf("после сверки остались маркеры: %d", len(m))
	}
}

// Пропавший выбранный узел заменяется первым живым: фиксатор записывает первого
// по порядку и маркер, затем проверка узлов решает. Без запущенного Mihomo узлы
// проверяет TLSPing (способ tls, мёртвые узлы не отвечают, остаётся первый по
// порядку); с запущенным — delay API Mihomo, выбирается живой узел. DIRECT сам не
// выбирается.
func TestRouterSelectRepick(t *testing.T) {
	ctx := context.Background()

	t.Run("tunnel_live_node", func(t *testing.T) {
		rtNeedBins(t, rtXrayBin)
		routertest.Quarantine(t, "150-22-select-repick-tunnel-flaky")
		probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(probe.Close)
		liveURL, hits := rtxaConnectProxy(t, strings.TrimPrefix(probe.URL, "http://"))
		id, err := newID(rand.Reader)
		if err != nil {
			t.Fatal("tunnel_root_id")
		}
		root := "/opt/tmp/xcp-ping-rt-" + id
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal("tunnel_root_create")
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		var e *rtEnv
		runner, err := proxyping.New(proxyping.Deps{
			ActiveKernel: func() string { return "" }, TLSPing: rtTLSPing,
			XrayBin: func() string { return rtXrayBin }, TmpRoot: root,
			Fingerprints: func() (map[string]string, error) { return e.svc.NodeFingerprints() },
			Publish:      func(string) {},
		})
		if err != nil {
			t.Fatal("tunnel_runner_create")
		}
		e = rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithSelection(), rtWithDeps(func(d *Deps) {
			d.Probe = runner.Probe
		}))
		runner.Start()
		t.Cleanup(runner.Stop)
		// Сначала назначаем роли по порядку каталога: случайные ID не должны
		// превращать тест в выбор уже первого живого узла без проверки мёртвого.
		e.addMyRaw(t, "http://127.0.0.1:9#tunnel-a", "http://127.0.0.1:10#tunnel-b", "http://127.0.0.1:11#tunnel-c")
		cat := e.catalog(t).Xray
		if len(cat) != 3 {
			t.Fatalf("tunnel_catalog: count=%d", len(cat))
		}
		deadURL := "http://127.0.0.1:" + strconv.Itoa(rtFreePort(t))
		if _, err := e.src.SaveNode(e.rev(t), strings.TrimPrefix(cat[0].Ref, sources.MySourceID+"/"), "tunnel-dead", deadURL); err != nil {
			t.Fatal("tunnel_dead_save")
		}
		if _, err := e.src.SaveNode(e.rev(t), strings.TrimPrefix(cat[1].Ref, sources.MySourceID+"/"), "tunnel-live", liveURL); err != nil {
			t.Fatal("tunnel_live_save")
		}
		cat = e.catalog(t).Xray
		live, removed := cat[1], cat[2]
		_, grp, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Туннель", Mode: ModeManual, Sources: GroupSources{All: true}})
		if err != nil || e.svcSaveProbe(probe.URL) != nil {
			t.Fatal("tunnel_group_probe_save")
		}
		e.takeAndApply(t)
		if _, err := e.svc.Select(ctx, grp.ID, memberNode+removed.Ref); err != nil {
			t.Fatal("tunnel_initial_selection")
		}
		if _, err := e.svc.DeleteNode(e.rev(t), strings.TrimPrefix(removed.Ref, sources.MySourceID+"/")); err != nil {
			t.Fatal("tunnel_remove_selected")
		}
		e.applyOK(t)
		var note configlayer.NoticeView
		rtWaitCond(t, 30*time.Second, "повторный выбор через туннель", func() bool {
			var ok bool
			note, ok = e.rtsNotice(noticeRepickPrefix + grp.ID)
			return ok
		})
		if note.Params["answered"] != "true" || note.Params["method"] != repickMethodTunnel || e.rtcSelection(t)[grp.ID] != memberNode+live.Ref || hits.Load() == 0 {
			t.Fatalf("tunnel_repick: answered=%s method=%s selected=%t hits=%d", note.Params["answered"], note.Params["method"], e.rtcSelection(t)[grp.ID] == memberNode+live.Ref, hits.Load())
		}
		runner.Stop()
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("tunnel_cleanup: error=%t entries=%d", err != nil, len(entries))
		}
	})

	t.Run("tls_none_answered", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithSelection(), rtWithDeps(func(d *Deps) { d.Ping = rtTLSPing }))
		e.addMyRaw(t, "socks5://127.0.0.1:9#rep-a", "socks5://127.0.0.1:10#rep-b", "socks5://127.0.0.1:11#rep-c")
		cat := e.catalog(t).Xray
		nodeB := rtsNode(t, cat, "rep-b")
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
		// «Первый по порядку» — первый узел группы в порядке каталога после удаления B
		// (порядок каталога — по id записи, а не по порядку добавления).
		first := e.catalog(t).Xray
		if len(first) == 0 {
			t.Fatal("каталог Xray пуст после удаления узла B")
		}

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
		if sel != memberNode+first[0].Ref {
			t.Errorf("выбор после замены = %q, ожидался первый по порядку каталога %q", sel, memberNode+first[0].Ref)
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
