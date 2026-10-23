//go:build router

package configlayer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Взятие и возврат маршрутизации на устройстве: настоящий слой, хранилище,
// `xray run -test` и `mihomo -t` роутера, временный каталог в рабочем каталоге
// теста. Работающие ядра стенда не трогаются (Applier не задан), тест ядро не
// читает и не переключает — метка ядра не нужна.

const (
	rtTkRoutingFile = "05_routing.json"
	rtTkAPIFile     = "00_api.json"
	rtTkInbounds    = "03_inbounds.json"
	rtTkOutbounds   = "04_outbounds.json"
	rtTkPanelRoute  = "99_xcp-routing.json"
	rtTkPanelTail   = "04_outbounds.xcp-takeover.tail.json"
)

const (
	// Чистый файл routing: единственный ключ routing.
	rtTkRoutingBody = `{
  // чужой файл routing без других ключей
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {"type": "field", "network": "tcp,udp", "outboundTag": "direct"}
    ]
  }
}
`
	// Смешанный файл: api, входящий api и правило api (блок панели).
	rtTkAPIBody = `{
  "api": {"tag": "api", "services": ["HandlerService", "StatsService"]},
  "stats": {},
  "inbounds": [
    {"tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}
  ],
  "routing": {
    "rules": [
      {"type": "field", "inboundTag": ["api"], "outboundTag": "api"}
    ]
  }
}
`
	rtTkInboundsBody = `{
  "inbounds": [
    {
      "tag": "redirect", "port": 61219, "protocol": "dokodemo-door",
      "settings": {"network": "tcp", "followRedirect": true},
      "sniffing": {"enabled": true, "destOverride": ["http", "tls"]}
    }
  ]
}
`
	rtTkOutboundsBody = `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}
`
	rtTkUserProfile = "mode: rule\n" +
		"redir-port: 7000\n" +
		"tproxy-port: 7001\n" +
		"allow-lan: true\n" +
		"proxy-groups:\n" +
		"  - name: PROXY\n" +
		"    type: select\n" +
		"    proxies:\n" +
		"      - DIRECT\n" +
		"rules:\n" +
		"  - MATCH,PROXY\n"
)

// rtTakeoverGen при желаемом managed=true выдаёт файл routing Xray (правило api
// первым, если в configs/ есть входящий api), хвост с blackhole и профиль Mihomo
// с портами прежнего профиля. Секция rt_bad_routing делает файл routing
// невалидным для Xray (неизвестный balancerTag).
type rtTakeoverGen struct {
	mu      sync.Mutex
	sawAPI  bool
	sawPort MihomoPorts
}

func (g *rtTakeoverGen) ID() string { return "rt-takeover" }

func (g *rtTakeoverGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	if !DesiredRouting(in.Sections).Managed {
		return nil, nil
	}
	g.mu.Lock()
	g.sawAPI = g.sawAPI || in.Routing.XrayAPIInbound
	g.sawPort = in.Routing.Mihomo
	g.mu.Unlock()

	rules := []map[string]any{}
	if in.Routing.XrayAPIInbound {
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"})
	}
	if _, bad := in.Sections["rt_bad_routing"]; bad {
		rules = append(rules, map[string]any{"type": "field", "network": "tcp,udp", "balancerTag": "rt-no-such-balancer"})
	} else {
		rules = append(rules, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "xcp-block"})
	}
	route, err := json.Marshal(map[string]any{"routing": map[string]any{"domainStrategy": "AsIs", "rules": rules}})
	if err != nil {
		return nil, err
	}
	tail, err := json.Marshal(map[string]any{"outbounds": []map[string]any{{"tag": "xcp-block", "protocol": "blackhole"}}})
	if err != nil {
		return nil, err
	}
	profile := fmt.Sprintf("mode: rule\nredir-port: %d\ntproxy-port: %d\nrules:\n  - MATCH,DIRECT\n",
		in.Routing.Mihomo.Redir, in.Routing.Mihomo.TProxy)
	return []GeneratedFile{
		{Kernel: KernelXray, RelPath: rtTkPanelRoute, Kind: KindXrayJSON, Content: route},
		{Kernel: KernelXray, RelPath: rtTkPanelTail, Kind: KindXrayJSON, Content: tail},
		{Kernel: KernelMihomo, RelPath: PanelMihomoProfile, Kind: KindMihomoProfile, Content: []byte(profile)},
	}, nil
}

func (g *rtTakeoverGen) facts() (bool, MihomoPorts) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sawAPI, g.sawPort
}

// rtTakeoverEnv — слой с чужими файлами routing и симлинком config.yaml.
type rtTakeoverEnv struct {
	layer  *Layer
	gen    *rtTakeoverGen
	roots  Roots
	rev    int64
	hashes map[string]string
	link   string
}

func rtNewTakeoverEnv(t *testing.T) *rtTakeoverEnv {
	t.Helper()
	for _, bin := range []string{rtRefreshXrayBin, rtMhBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("ядро не установлено на устройстве: %v", err)
		}
	}
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, filepath.Join(roots.Mihomo, "profiles")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := &rtTakeoverEnv{gen: &rtTakeoverGen{}, roots: roots, hashes: map[string]string{}}
	for name, body := range map[string]string{
		rtTkRoutingFile: rtTkRoutingBody,
		rtTkAPIFile:     rtTkAPIBody,
		rtTkInbounds:    rtTkInboundsBody,
		rtTkOutbounds:   rtTkOutboundsBody,
	} {
		if err := os.WriteFile(filepath.Join(roots.Xray, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		e.hashes[name] = HashContent([]byte(body))
	}
	userProfile := filepath.Join(roots.Mihomo, "profiles", "user.yaml")
	if err := os.WriteFile(userProfile, []byte(rtTkUserProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	e.hashes["profiles/user.yaml"] = HashContent([]byte(rtTkUserProfile))
	e.link = userProfile
	if err := os.Symlink(userProfile, filepath.Join(roots.Mihomo, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin, Mihomo: rtMhBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(e.gen); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	e.layer = layer
	return e
}

// edit меняет секцию черновика (nil — удаляет) и запоминает ревизию.
func (e *rtTakeoverEnv) edit(t *testing.T, section string, value json.RawMessage) {
	t.Helper()
	ev, err := e.layer.EditDraft(e.rev, section, value)
	if err != nil {
		t.Fatalf("EditDraft %s: %v", section, err)
	}
	e.rev = ev.DraftRevision
}

func (e *rtTakeoverEnv) setManaged(t *testing.T, managed bool) {
	t.Helper()
	e.edit(t, RoutingSection, json.RawMessage(fmt.Sprintf(`{"v":1,"managed":%v}`, managed)))
}

// apply запускает «Применить» и ждёт итог.
func (e *rtTakeoverEnv) apply(t *testing.T) *ResultView {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка на события закрыта до итога применения")
			}
			if ev.Type == EventApplyDone {
				view, ok := ev.Data.(ApplyView)
				if !ok || view.Result == nil {
					t.Fatalf("apply_done без итога: %+v", ev.Data)
				}
				return view.Result
			}
		case <-deadline:
			t.Fatal("применение не завершилось")
		}
	}
}

func (e *rtTakeoverEnv) xray(name string) string { return filepath.Join(e.roots.Xray, name) }

func (e *rtTakeoverEnv) mihomo(rel string) string {
	return filepath.Join(e.roots.Mihomo, filepath.FromSlash(rel))
}

// assertUntouched проверяет, что смешанные файлы Xray и профиль пользователя
// не изменились.
func (e *rtTakeoverEnv) assertUntouched(t *testing.T) {
	t.Helper()
	for _, name := range []string{rtTkAPIFile, rtTkInbounds, rtTkOutbounds} {
		if got := hashOf(e.xray(name)); got != e.hashes[name] {
			t.Errorf("%s изменён: %s, ожидался %s", name, got, e.hashes[name])
		}
	}
	if got := hashOf(e.mihomo("profiles/user.yaml")); got != e.hashes["profiles/user.yaml"] {
		t.Errorf("profiles/user.yaml изменён")
	}
}

// assertConfigLink проверяет, на что указывает config.yaml.
func (e *rtTakeoverEnv) assertConfigLink(t *testing.T, want string) {
	t.Helper()
	got, err := os.Readlink(e.mihomo("config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml не симлинк: %v", err)
	}
	if got != want {
		t.Errorf("config.yaml указывает на %q, ожидалось %q", got, want)
	}
}

// assertNoPanelFiles проверяет, что файлов панели нет на диске.
func (e *rtTakeoverEnv) assertNoPanelFiles(t *testing.T) {
	t.Helper()
	for _, p := range []string{e.xray(rtTkPanelRoute), e.xray(rtTkPanelTail), e.mihomo(PanelMihomoProfile)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("файл панели остался на диске: %s (%v)", filepath.Base(p), err)
		}
	}
}

func TestRouterTakeoverApplyAndRelease(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// Взятие: желаемое managed=true проходит обычным «Применить».
	e.setManaged(t, true)
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}

	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("чистый файл routing остался в configs/: %v", err)
	}
	e.assertUntouched(t)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	st := e.layer.store.Snapshot()
	if st.Journal != nil {
		t.Errorf("журнал не снят после применения: %+v", st.Journal)
	}
	if st.Routing.Xray == nil || len(st.Routing.Xray.Moved) != 1 || st.Routing.Xray.Moved[0].Name != rtTkRoutingFile {
		t.Fatalf("State.Routing.Xray.Moved: %+v", st.Routing.Xray)
	}
	moved := st.Routing.Xray.Moved[0]
	if moved.Hash != e.hashes[rtTkRoutingFile] {
		t.Errorf("хэш перенесённого файла %s, ожидался %s", moved.Hash, e.hashes[rtTkRoutingFile])
	}
	if got := hashOf(movedCopyPath(moved)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("копия перенесённого файла в наборе не совпадает: %q", got)
	}
	if !containsString(st.Routing.Xray.Left, rtTkAPIFile) {
		t.Errorf("в Left нет смешанного файла %s: %v", rtTkAPIFile, st.Routing.Xray.Left)
	}
	if containsString(st.Routing.Xray.Left, rtTkRoutingFile) {
		t.Errorf("перенесённый файл числится оставленным: %v", st.Routing.Xray.Left)
	}
	m := st.Routing.Mihomo
	if m == nil || m.PrevKind != configKindSymlink || m.PrevProfile != "profiles/user.yaml" {
		t.Fatalf("State.Routing.Mihomo: %+v", m)
	}
	if m.Ports.Redir != 7000 || m.Ports.TProxy != 7001 || m.Ports.AllowLAN == nil || !*m.Ports.AllowLAN {
		t.Errorf("порты прежнего профиля не запомнены: %+v", m.Ports)
	}

	// Генератор видел входящий api и порты прежнего профиля, файлы панели на месте.
	sawAPI, ports := e.gen.facts()
	if !sawAPI {
		t.Error("генератор не получил XrayAPIInbound")
	}
	if ports.Redir != 7000 || ports.TProxy != 7001 {
		t.Errorf("генератор получил порты %+v", ports)
	}
	routeRaw, err := os.ReadFile(e.xray(rtTkPanelRoute))
	if err != nil {
		t.Fatalf("файл routing панели не записан: %v", err)
	}
	var route struct {
		Routing struct {
			Rules []struct {
				InboundTag  []string `json:"inboundTag"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(routeRaw, &route); err != nil || len(route.Routing.Rules) < 2 {
		t.Fatalf("файл routing панели: %v: %s", err, routeRaw)
	}
	if first := route.Routing.Rules[0]; len(first.InboundTag) != 1 || first.InboundTag[0] != "api" || first.OutboundTag != "api" {
		t.Errorf("правило api не первое: %+v", first)
	}
	profile, err := os.ReadFile(e.mihomo(PanelMihomoProfile))
	if err != nil || !strings.Contains(string(profile), "redir-port: 7000") || !strings.Contains(string(profile), "tproxy-port: 7001") {
		t.Errorf("профиль панели без портов прежнего профиля: %v: %s", err, profile)
	}

	// Возврат: желаемое managed=false проходит тем же «Применить».
	e.setManaged(t, false)
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("возврат: %+v", res)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("чужой файл routing вернулся не побайтно: %q", got)
	}
	e.assertUntouched(t)
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	st = e.layer.store.Snapshot()
	if st.Routing.Xray != nil || st.Routing.Mihomo != nil {
		t.Errorf("State.Routing не пуст после возврата: %+v", st.Routing)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят после возврата: %+v", st.Journal)
	}
}

func TestRouterTakeoverValidationFailKeepsAll(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// Невалидный для Xray файл routing: проверка ядром отвергает итоговую раскладку,
	// на диске всё как было.
	e.edit(t, "rt_bad_routing", json.RawMessage(`{"on":true}`))
	e.setManaged(t, true)
	res := e.apply(t)
	if res.OK || res.Code != CodeValidationFailed || res.Kernel != KernelXray {
		t.Fatalf("итог при невалидном routing: %+v", res)
	}
	e.assertKeptAll(t)

	// Сбой записи после переноса: перекладке config.yaml мешает непустой каталог на
	// месте временного симлинка (root обходит права, каталог — нет). Откат возвращает
	// перенесённый файл и убирает уже записанные файлы панели.
	e.edit(t, "rt_bad_routing", nil)
	blocker := e.mihomo("config.yaml.xcp-link")
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	res = e.apply(t)
	if res.OK || res.Code != ResultWriteFailed || !res.RolledBack {
		t.Fatalf("итог при сбое записи: %+v", res)
	}
	e.assertKeptAll(t)

	// После отката состояние пригодно: без помехи то же применение проходит.
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие после отката: %+v", res)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("файл routing остался в configs/ после успешного взятия: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
}

// assertKeptAll — после отказанного применения всё как было до него.
func (e *rtTakeoverEnv) assertKeptAll(t *testing.T) {
	t.Helper()
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("%s не на месте или изменён: %q", rtTkRoutingFile, got)
	}
	e.assertUntouched(t)
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	st := e.layer.store.Snapshot()
	if st.Routing.Xray != nil || st.Routing.Mihomo != nil {
		t.Errorf("State.Routing изменён при отказе: %+v", st.Routing)
	}
	if st.Journal != nil {
		t.Errorf("журнал остался после отказа: %+v", st.Journal)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
