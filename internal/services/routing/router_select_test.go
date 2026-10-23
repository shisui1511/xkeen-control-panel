//go:build router

package routing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
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
