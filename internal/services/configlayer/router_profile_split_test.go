//go:build router

package configlayer

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// Разделение владения маршрутизацией по ядрам на устройстве (147.1, D-06, D-07):
// настоящий слой, хранилище, `xray run -test` и `mihomo -t` роутера, временный
// каталог в рабочем каталоге теста. Работающие ядра стенда не трогаются, ядро
// тест не читает и не переключает — метка ядра не нужна.

// rtSplitGen выдаёт файлы по ядрам: routing Xray — когда панель держит Xray,
// профиль Mihomo с портами прежнего профиля — когда панель держит Mihomo.
type rtSplitGen struct{}

func (rtSplitGen) ID() string { return "rt-split" }

func (rtSplitGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	desired := DesiredRouting(in.Sections)
	var out []GeneratedFile
	if desired.ManagedFor(KernelXray) {
		route, err := json.Marshal(map[string]any{"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          []map[string]any{{"type": "field", "network": "tcp,udp", "outboundTag": "xcp-block"}},
		}})
		if err != nil {
			return nil, err
		}
		tail, err := json.Marshal(map[string]any{"outbounds": []map[string]any{{"tag": "xcp-block", "protocol": "blackhole"}}})
		if err != nil {
			return nil, err
		}
		out = append(out,
			GeneratedFile{Kernel: KernelXray, RelPath: rtTkPanelRoute, Kind: KindXrayJSON, Content: route},
			GeneratedFile{Kernel: KernelXray, RelPath: rtTkPanelTail, Kind: KindXrayJSON, Content: tail},
		)
	}
	if desired.ManagedFor(KernelMihomo) {
		profile := fmt.Sprintf("mode: rule\nredir-port: %d\ntproxy-port: %d\nrules:\n  - MATCH,DIRECT\n",
			in.Routing.Mihomo.Redir, in.Routing.Mihomo.TProxy)
		out = append(out, GeneratedFile{Kernel: KernelMihomo, RelPath: PanelMihomoProfile, Kind: KindMihomoProfile, Content: []byte(profile)})
	}
	return out, nil
}

// rtNewSplitEnv — слой с раскладкой rtWriteTakeoverLayout и генератором rtSplitGen.
func rtNewSplitEnv(t *testing.T) *rtTakeoverEnv {
	t.Helper()
	for _, bin := range []string{rtRefreshXrayBin, rtMhBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("ядро не установлено на устройстве: %v", err)
		}
	}
	dir := rtLiveDir(t)
	roots, hashes, link := rtWriteTakeoverLayout(t, dir, false)
	e := &rtTakeoverEnv{gen: &rtTakeoverGen{}, roots: roots, hashes: hashes, link: link}
	e.enabled.Store(true)
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       e.enabled.Load,
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin, Mihomo: rtMhBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(rtSplitGen{}); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	e.layer = layer
	return e
}

// setRouting записывает секцию routing как есть и применяет черновик.
func (e *rtTakeoverEnv) setRouting(t *testing.T, raw string) *ResultView {
	t.Helper()
	e.edit(t, RoutingSection, json.RawMessage(raw))
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("применение секции %s: %+v", raw, res)
	}
	return res
}

func TestRouterProfileSplitOps(t *testing.T) {
	e := rtNewSplitEnv(t)

	// Оба ядра у панели: секция без поля mihomo ведёт себя как раньше.
	e.setRouting(t, `{"v":1,"managed":true}`)
	st := e.layer.store.Snapshot()
	if st.Routing.Xray == nil || st.Routing.Mihomo == nil {
		t.Fatalf("оба ядра должны быть взяты: %+v", st.Routing)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Mihomo на своём профиле, Xray у панели: нужен только возврат Mihomo.
	e.setRouting(t, `{"v":1,"managed":true,"mihomo":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo != nil {
		t.Errorf("State.Routing.Mihomo не снят: %+v", st.Routing.Mihomo)
	}
	if st.Routing.Xray == nil || len(st.Routing.Xray.Moved) != 1 || st.Routing.Xray.Moved[0].Name != rtTkRoutingFile {
		t.Errorf("State.Routing.Xray тронут отпусканием Mihomo: %+v", st.Routing.Xray)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("файл routing Xray вернулся при отпускании Mihomo: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл routing панели Xray пропал: %v", err)
	}
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); !os.IsNotExist(err) {
		t.Errorf("профиль панели Mihomo остался на диске: %v", err)
	}
	e.assertConfigLink(t, e.link)

	// Снимок слоя: владение по ядрам, активный свой профиль Mihomo.
	snap := e.layer.Snapshot().Routing
	if !snap.Desired || !snap.DesiredXray || snap.DesiredMihomo {
		t.Errorf("снимок: desired=%v desired_xray=%v desired_mihomo=%v", snap.Desired, snap.DesiredXray, snap.DesiredMihomo)
	}
	if snap.MihomoActive != "user" {
		t.Errorf("mihomo_active = %q, ожидалось user", snap.MihomoActive)
	}

	// Всё отпущено: Xray возвращается, Mihomo остаётся на своём профиле.
	e.setRouting(t, `{"v":1,"managed":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Xray != nil || st.Routing.Mihomo != nil {
		t.Errorf("State.Routing не пуст после отпускания: %+v", st.Routing)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("чужой файл routing вернулся не побайтно: %q", got)
	}
	e.assertConfigLink(t, e.link)

	// Только Mihomo у панели, Xray за XKeen.
	e.setRouting(t, `{"v":1,"managed":false,"mihomo":true}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo == nil || st.Routing.Xray != nil {
		t.Fatalf("ожидался взятый Mihomo без Xray: %+v", st.Routing)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); err != nil {
		t.Errorf("файл routing Xray тронут взятием Mihomo: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); !os.IsNotExist(err) {
		t.Errorf("файл routing панели Xray записан без управления: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	snap = e.layer.Snapshot().Routing
	if !snap.Desired || snap.DesiredXray || !snap.DesiredMihomo || snap.MihomoActive != "" {
		t.Errorf("снимок: %+v", snap)
	}

	// Секция без поля после этого ведёт себя как раньше: Xray берётся, Mihomo
	// остаётся у панели, затем отпускание возвращает оба.
	e.setRouting(t, `{"v":1,"managed":true}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo == nil || st.Routing.Xray == nil {
		t.Fatalf("ожидались оба ядра: %+v", st.Routing)
	}
	e.setRouting(t, `{"v":1,"managed":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo != nil || st.Routing.Xray != nil {
		t.Errorf("State.Routing не пуст: %+v", st.Routing)
	}
	e.assertConfigLink(t, e.link)
	e.assertUntouched(t)
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("чужой файл routing вернулся не побайтно: %q", got)
	}
}
