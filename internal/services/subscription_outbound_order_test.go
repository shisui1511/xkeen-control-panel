package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// simulateXrayConfdirMerge повторяет документированное правило мерджа Xray для
// -confdir («Multiple config files»): файлы обрабатываются по имени; для каждого
// outbound файла — тег уже есть в результате: замена на месте; тега нет: при
// «tail» в имени файла (без учёта регистра) добавление в конец, иначе новые
// outbounds файла блоком ставятся в начало с сохранением порядка внутри файла.
// Первый outbound итогового списка — дефолтный.
//
// Симулятор доказывает только соответствие документированному правилу.
// Авторитетная проверка — `xray run -confdir … -dump` и внешний IP на роутере
// (план 133-07).
func simulateXrayConfdirMerge(t *testing.T, dir string) []map[string]interface{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var merged []map[string]interface{}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var wrapper struct {
			Outbounds []map[string]interface{} `json:"outbounds"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			continue
		}
		tail := strings.Contains(strings.ToLower(name), "tail")
		var fresh []map[string]interface{}
		for _, ob := range wrapper.Outbounds {
			tag, _ := ob["tag"].(string)
			replaced := false
			if tag != "" {
				for i := range merged {
					if merged[i]["tag"] == tag {
						merged[i] = ob
						replaced = true
						break
					}
				}
			}
			if replaced {
				continue
			}
			if tail {
				merged = append(merged, ob)
			} else {
				fresh = append(fresh, ob)
			}
		}
		if len(fresh) > 0 {
			merged = append(append([]map[string]interface{}{}, fresh...), merged...)
		}
	}
	return merged
}

// outboundAddress достаёт адрес сервера из outbound (vnext или servers).
func outboundAddress(ob map[string]interface{}) string {
	settings, _ := ob["settings"].(map[string]interface{})
	for _, key := range []string{"vnext", "servers"} {
		if list, ok := settings[key].([]interface{}); ok && len(list) > 0 {
			if first, ok := list[0].(map[string]interface{}); ok {
				if addr, ok := first["address"].(string); ok {
					return addr
				}
			}
		}
	}
	return ""
}

const orderBaseOutbounds = `{
  "outbounds": [
    {"tag": "direct", "protocol": "freedom"},
    {"tag": "block", "protocol": "blackhole"}
  ]
}
`

// newOrderEnv готовит окружение: базовый 04_outbounds.json XKeen и подписка
// sub_1 с двумя рабочими узлами после Refresh.
func newOrderEnv(t *testing.T) *stubProviderEnv {
	t.Helper()
	env := newStubProviderEnv(t, Subscription{ID: "sub_1", Name: "S"})
	if err := os.WriteFile(filepath.Join(env.xrayDir, "04_outbounds.json"), []byte(orderBaseOutbounds), 0600); err != nil {
		t.Fatal(err)
	}
	env.body.Store(stubProviderBody(
		"1.1.1.1:443|"+stubWorkingUUID+"|first",
		"2.2.2.2:443|"+stubWorkingUUID+"|second",
	))
	if err := env.svc.Refresh("sub_1"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return env
}

func nodeTagByServer(t *testing.T, env *stubProviderEnv, id, server string) string {
	t.Helper()
	sub := env.svc.Get(id)
	if sub == nil {
		t.Fatalf("subscription %s not found", id)
	}
	for _, n := range sub.Nodes {
		if n.Server == server {
			return n.Tag
		}
	}
	t.Fatalf("node with server %q not found in %+v", server, sub.Nodes)
	return ""
}

func TestSelectNode_BecomesFirstOutbound(t *testing.T) {
	env := newOrderEnv(t)
	basePath := filepath.Join(env.xrayDir, "04_outbounds.json")
	baseBefore, _ := os.ReadFile(basePath)

	tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("SetActiveNode: %v", err)
	}

	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if len(merged) == 0 {
		t.Fatal("merged outbounds are empty")
	}
	if merged[0]["tag"] != "xcp-sub_1" {
		t.Fatalf("first outbound must be xcp-sub_1, got %v", merged[0]["tag"])
	}
	if addr := outboundAddress(merged[0]); addr != "2.2.2.2" {
		t.Errorf("default outbound address = %q, want 2.2.2.2", addr)
	}

	baseAfter, _ := os.ReadFile(basePath)
	if !bytes.Equal(baseBefore, baseAfter) {
		t.Error("04_outbounds.json must stay byte-identical")
	}

	sub := env.svc.Get("sub_1")
	if sub.SelectedTag != tag || !sub.IsDefault || sub.SelectedServer != "2.2.2.2:443" {
		t.Errorf("selection state not saved: %+v", sub)
	}

	// Повторный выбор того же узла оставляет файл дефолта побайтно тем же.
	defPath := filepath.Join(env.xrayDir, selectionDefaultFileName)
	first, _ := os.ReadFile(defPath)
	restartsBefore := env.restartCalls(t)
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("repeat SetActiveNode: %v", err)
	}
	second, _ := os.ReadFile(defPath)
	if !bytes.Equal(first, second) {
		t.Error("default file changed on repeated selection of the same node")
	}
	if got := env.restartCalls(t); got != restartsBefore {
		t.Errorf("repeated selection must not restart kernel: %d -> %d", restartsBefore, got)
	}

	// Выбор другого узла перезаписывает дефолт.
	other := nodeTagByServer(t, env, "sub_1", "1.1.1.1:443")
	if err := env.svc.SetActiveNode("sub_1", other); err != nil {
		t.Fatalf("SetActiveNode other: %v", err)
	}
	merged = simulateXrayConfdirMerge(t, env.xrayDir)
	if addr := outboundAddress(merged[0]); addr != "1.1.1.1" || merged[0]["tag"] != "xcp-sub_1" {
		t.Errorf("after reselect first = %v (%s)", merged[0]["tag"], addr)
	}
}

func TestSelectNode_StubRejected(t *testing.T) {
	env := newOrderEnv(t)

	// Заглушка в списке узлов (как её отдаёт refresh с device_rejected).
	env.svc.mu.Lock()
	live := env.svc.GetLocked("sub_1")
	live.Nodes = append(live.Nodes, SubscriptionNode{Tag: "stub-1", Name: "stub", Protocol: "vless", Stub: true, Server: "0.0.0.0:1"})
	env.svc.mu.Unlock()

	err := env.svc.SetActiveNode("sub_1", "stub-1")
	if !errors.Is(err, ErrStubNodeSelection) {
		t.Fatalf("expected ErrStubNodeSelection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(statErr) {
		t.Error("default file must not be created for a stub node")
	}

	err = env.svc.SetActiveNode("sub_1", "no-such-tag")
	if !errors.Is(err, ErrSelectionNodeNotFound) {
		t.Fatalf("expected ErrSelectionNodeNotFound, got %v", err)
	}
}

func TestSelectNode_StoppedKernelNotStarted(t *testing.T) {
	env := newOrderEnv(t)
	env.svc.SetKernelService(&statusKernelService{status: map[string]string{"xray": "stopped", "mihomo": "not_installed"}})
	before := env.restartCalls(t)

	tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("SetActiveNode: %v", err)
	}
	if got := env.restartCalls(t); got != before {
		t.Fatalf("xkeen -restart must not start a stopped kernel: restarts %d -> %d", before, got)
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); err != nil {
		t.Errorf("default file must still be written: %v", err)
	}
}

func TestNoSelection_BaseDirectStaysFirst(t *testing.T) {
	env := newOrderEnv(t)

	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if len(merged) != 4 {
		t.Fatalf("expected direct, block and 2 subscription nodes, got %d", len(merged))
	}
	if merged[0]["tag"] != "direct" {
		t.Fatalf("without selection the first outbound must stay direct, got %v", merged[0]["tag"])
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
		t.Error("default file must not exist without a selection")
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, "04_outbounds.sub_1.tail.json")); err != nil {
		t.Errorf("subscription fragment must use the tail name: %v", err)
	}
}

func TestLegacyFragmentMigratedOnLoad(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatal(err)
	}
	first := NewSubscriptionService(tmp, xrayDir, tmp)
	for _, id := range []string{"sub_1", "sub_2"} {
		if err := first.Add(&Subscription{ID: id, Name: id, URL: "https://example.com/" + id, Enabled: true, EnableXray: true, Interval: 1}); err != nil {
			t.Fatal(err)
		}
	}

	const legacyBody = `{"outbounds":[{"tag":"legacy","protocol":"freedom"}]}`
	const tailBody = `{"outbounds":[{"tag":"already-new","protocol":"freedom"}]}`
	legacy1 := filepath.Join(xrayDir, "04_outbounds.sub_1.json")
	tail1 := filepath.Join(xrayDir, "04_outbounds.sub_1.tail.json")
	legacy2 := filepath.Join(xrayDir, "04_outbounds.sub_2.json")
	tail2 := filepath.Join(xrayDir, "04_outbounds.sub_2.tail.json")
	baseBody := []byte(orderBaseOutbounds)
	basePath := filepath.Join(xrayDir, "04_outbounds.json")
	for path, body := range map[string][]byte{
		legacy1:  []byte(legacyBody),
		legacy2:  []byte(legacyBody),
		tail2:    []byte(tailBody),
		basePath: baseBody,
	} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}

	NewSubscriptionService(tmp, xrayDir, tmp)

	// Только legacy: переименован, содержимое то же.
	if _, err := os.Stat(legacy1); !os.IsNotExist(err) {
		t.Error("legacy fragment must be renamed")
	}
	if got, err := os.ReadFile(tail1); err != nil || string(got) != legacyBody {
		t.Errorf("renamed fragment content = %q, err %v", got, err)
	}
	// Оба файла: legacy удалён, новый не тронут.
	if _, err := os.Stat(legacy2); !os.IsNotExist(err) {
		t.Error("legacy fragment must be removed when the tail file already exists")
	}
	if got, _ := os.ReadFile(tail2); string(got) != tailBody {
		t.Errorf("existing tail fragment must stay untouched, got %q", got)
	}
	// Файлы XKeen не тронуты.
	if got, _ := os.ReadFile(basePath); !bytes.Equal(got, baseBody) {
		t.Error("04_outbounds.json must not be touched by migration")
	}
}

func TestSelection_DroppedOnDeleteDisableAuto(t *testing.T) {
	cases := []struct {
		name string
		act  func(env *stubProviderEnv) error
		gone bool
	}{
		{"delete", func(env *stubProviderEnv) error { return env.svc.Delete("sub_1") }, true},
		{"disable xray", func(env *stubProviderEnv) error {
			return env.svc.Update("sub_1", &Subscription{Name: "S", URL: "https://example.com/s", Enabled: true, EnableXray: false, Interval: 1})
		}, false},
		{"disable subscription", func(env *stubProviderEnv) error {
			return env.svc.Update("sub_1", &Subscription{Name: "S", URL: "https://example.com/s", Enabled: false, EnableXray: true, Interval: 1})
		}, false},
		{"auto routing", func(env *stubProviderEnv) error {
			return env.svc.Update("sub_1", &Subscription{Name: "S", URL: "https://example.com/s", Enabled: true, EnableXray: true, RoutingMode: "auto", Interval: 1})
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newOrderEnv(t)
			tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
			if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
				t.Fatalf("SetActiveNode: %v", err)
			}
			if merged := simulateXrayConfdirMerge(t, env.xrayDir); merged[0]["tag"] != "xcp-sub_1" {
				t.Fatalf("precondition: selected node must be first, got %v", merged[0]["tag"])
			}

			if err := tc.act(env); err != nil {
				t.Fatalf("action: %v", err)
			}

			if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
				t.Error("default file must be removed")
			}
			if merged := simulateXrayConfdirMerge(t, env.xrayDir); len(merged) == 0 || merged[0]["tag"] != "direct" {
				t.Errorf("first outbound must be direct again, got %v", merged)
			}
			if !tc.gone {
				sub := env.svc.Get("sub_1")
				if sub == nil || sub.SelectedTag != "" || sub.IsDefault || sub.SelectedServer != "" {
					t.Errorf("selection must be dropped: %+v", sub)
				}
				for _, n := range sub.Nodes {
					if n.Active {
						t.Errorf("node %s must not stay active", n.Tag)
					}
				}
			}
		})
	}
}

func TestClearDefault_DirectFirstAgain(t *testing.T) {
	env := newOrderEnv(t)
	tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("SetActiveNode: %v", err)
	}
	restartsBefore := env.restartCalls(t)

	if err := env.svc.ClearActiveNode("sub_1"); err != nil {
		t.Fatalf("ClearActiveNode: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
		t.Error("default file must be removed")
	}
	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if len(merged) == 0 || merged[0]["tag"] != "direct" {
		t.Fatalf("first outbound must be direct again, got %v", merged)
	}
	sub := env.svc.Get("sub_1")
	if sub.IsDefault {
		t.Error("subscription must stop being the default")
	}
	if sub.SelectedTag != tag {
		t.Errorf("selected tag must be kept for by-tag use, got %q", sub.SelectedTag)
	}
	if got := env.restartCalls(t); got != restartsBefore+1 {
		t.Errorf("running kernel must be restarted once: %d -> %d", restartsBefore, got)
	}

	// Повторный выбор после снятия снова делает узел дефолтным.
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("SetActiveNode again: %v", err)
	}
	if merged := simulateXrayConfdirMerge(t, env.xrayDir); merged[0]["tag"] != "xcp-sub_1" {
		t.Errorf("reselected node must be first, got %v", merged[0]["tag"])
	}
}

func TestClearActiveNode_NoopWhenNotDefault(t *testing.T) {
	env := newOrderEnv(t)
	restartsBefore := env.restartCalls(t)
	fragBefore := env.fragment(t, "sub_1")

	if err := env.svc.ClearActiveNode("sub_1"); err != nil {
		t.Fatalf("ClearActiveNode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
		t.Error("default file must not appear")
	}
	if !bytes.Equal(fragBefore, env.fragment(t, "sub_1")) {
		t.Error("fragment must stay unchanged")
	}
	if got := env.restartCalls(t); got != restartsBefore {
		t.Errorf("no-op clear must not restart kernel: %d -> %d", restartsBefore, got)
	}

	if err := env.svc.ClearActiveNode("nope"); err == nil || err.Error() != "subscription not found" {
		t.Errorf("unknown subscription must give 'subscription not found', got %v", err)
	}
}

func TestClearActiveNode_StoppedKernelNotStarted(t *testing.T) {
	env := newOrderEnv(t)
	tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatalf("SetActiveNode: %v", err)
	}
	env.svc.SetKernelService(&statusKernelService{status: map[string]string{"xray": "stopped", "mihomo": "not_installed"}})
	before := env.restartCalls(t)

	if err := env.svc.ClearActiveNode("sub_1"); err != nil {
		t.Fatalf("ClearActiveNode: %v", err)
	}
	if got := env.restartCalls(t); got != before {
		t.Fatalf("xkeen -restart must not start a stopped kernel: %d -> %d", before, got)
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
		t.Error("default file must still be removed")
	}
}

// addOrderSub добавляет в окружение ещё одну Xray-подписку со своим
// httptest-провайдером и выполняет refresh (узлы 3.3.3.3 и 4.4.4.4).
func addOrderSub(t *testing.T, env *stubProviderEnv, id string) {
	t.Helper()
	body := stubProviderBody(
		"3.3.3.3:443|"+stubWorkingUUID+"|third",
		"4.4.4.4:443|"+stubWorkingUUID+"|fourth",
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	sub := Subscription{ID: id, Name: id, URL: srv.URL, Enabled: true, EnableXray: true, Interval: 1}
	if err := env.svc.Add(&sub); err != nil {
		t.Fatalf("Add %s: %v", id, err)
	}
	if err := env.svc.Refresh(id); err != nil {
		t.Fatalf("Refresh %s: %v", id, err)
	}
}

// mergedByTag ищет outbound в итоговом мердже по тегу.
func mergedByTag(merged []map[string]interface{}, tag string) (map[string]interface{}, int) {
	for i, ob := range merged {
		if ob["tag"] == tag {
			return ob, i
		}
	}
	return nil, -1
}

func TestMultiSubscription_LastSelectedIsDefault(t *testing.T) {
	env := newOrderEnv(t)
	addOrderSub(t, env, "sub_b")
	baseBefore, _ := os.ReadFile(filepath.Join(env.xrayDir, "04_outbounds.json"))

	tagA := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	tagB := nodeTagByServer(t, env, "sub_b", "4.4.4.4:443")

	if err := env.svc.SetActiveNode("sub_1", tagA); err != nil {
		t.Fatalf("select in sub_1: %v", err)
	}
	if err := env.svc.SetActiveNode("sub_b", tagB); err != nil {
		t.Fatalf("select in sub_b: %v", err)
	}

	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "xcp-sub_b" || outboundAddress(merged[0]) != "4.4.4.4" {
		t.Fatalf("last selected must be first, got %v (%s)", merged[0]["tag"], outboundAddress(merged[0]))
	}
	first, idx := mergedByTag(merged, "xcp-sub_1")
	if first == nil {
		t.Fatal("xcp-sub_1 must stay reachable by its stable tag")
	}
	if outboundAddress(first) != "2.2.2.2" {
		t.Errorf("xcp-sub_1 address = %q, want 2.2.2.2", outboundAddress(first))
	}
	_, directIdx := mergedByTag(merged, "direct")
	if idx <= directIdx {
		t.Errorf("by-tag node must be after XKeen outbounds: xcp-sub_1 at %d, direct at %d", idx, directIdx)
	}

	subA, subB := env.svc.Get("sub_1"), env.svc.Get("sub_b")
	if subA.IsDefault || !subB.IsDefault {
		t.Errorf("default flags: sub_1=%v sub_b=%v", subA.IsDefault, subB.IsDefault)
	}
	if subA.StableTag != "xcp-sub_1" || subB.StableTag != "xcp-sub_b" {
		t.Errorf("stable tags: %q, %q", subA.StableTag, subB.StableTag)
	}

	// Выбор в первой подписке возвращает ей дефолт; вторая остаётся по тегу.
	if err := env.svc.SetActiveNode("sub_1", tagA); err != nil {
		t.Fatalf("reselect in sub_1: %v", err)
	}
	merged = simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "xcp-sub_1" {
		t.Fatalf("after reselect first must be xcp-sub_1, got %v", merged[0]["tag"])
	}
	if ob, _ := mergedByTag(merged, "xcp-sub_b"); ob == nil || outboundAddress(ob) != "4.4.4.4" {
		t.Errorf("xcp-sub_b must stay reachable, got %v", ob)
	}

	// Инвариант: стабильный тег встречается ровно один раз в мердже.
	seen := map[string]int{}
	for _, ob := range merged {
		if tag, _ := ob["tag"].(string); strings.HasPrefix(tag, stableTagPrefix) {
			seen[tag]++
		}
	}
	for tag, n := range seen {
		if n != 1 {
			t.Errorf("tag %s occurs %d times in merged outbounds", tag, n)
		}
	}

	if after, _ := os.ReadFile(filepath.Join(env.xrayDir, "04_outbounds.json")); !bytes.Equal(baseBefore, after) {
		t.Error("04_outbounds.json must stay byte-identical")
	}
}

func TestStableTag_SurvivesNodeChange(t *testing.T) {
	env := newOrderEnv(t)

	tag1 := nodeTagByServer(t, env, "sub_1", "1.1.1.1:443")
	tag2 := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag1); err != nil {
		t.Fatal(err)
	}
	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "xcp-sub_1" || outboundAddress(merged[0]) != "1.1.1.1" {
		t.Fatalf("first selection: %v (%s)", merged[0]["tag"], outboundAddress(merged[0]))
	}

	if err := env.svc.SetActiveNode("sub_1", tag2); err != nil {
		t.Fatal(err)
	}
	merged = simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "xcp-sub_1" {
		t.Fatalf("stable tag must not change with the node, got %v", merged[0]["tag"])
	}
	if outboundAddress(merged[0]) != "2.2.2.2" {
		t.Errorf("stable tag must point at the new node, got %s", outboundAddress(merged[0]))
	}
	if _, n := mergedByTag(merged, "xcp-sub_1"); n != 0 {
		t.Errorf("xcp-sub_1 must occur once and first, at %d", n)
	}
	if sub := env.svc.Get("sub_1"); sub.StableTag != "xcp-sub_1" {
		t.Errorf("stable_tag in API = %q", sub.StableTag)
	}
}

func TestClearDefault_StableTagStaysReachable(t *testing.T) {
	env := newOrderEnv(t)
	tag := nodeTagByServer(t, env, "sub_1", "2.2.2.2:443")
	if err := env.svc.SetActiveNode("sub_1", tag); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.ClearActiveNode("sub_1"); err != nil {
		t.Fatal(err)
	}

	merged := simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "direct" {
		t.Fatalf("first outbound must be direct again, got %v", merged[0]["tag"])
	}
	ob, _ := mergedByTag(merged, "xcp-sub_1")
	if ob == nil {
		t.Fatal("stable tag must stay in the config after clearing the default")
	}
	if outboundAddress(ob) != "2.2.2.2" {
		t.Errorf("xcp-sub_1 address = %q, want 2.2.2.2", outboundAddress(ob))
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionDefaultFileName)); !os.IsNotExist(err) {
		t.Error("default file must be removed")
	}
	if _, err := os.Stat(filepath.Join(env.xrayDir, selectionTailFileName)); err != nil {
		t.Errorf("tail file must hold the by-tag node: %v", err)
	}
	if sub := env.svc.Get("sub_1"); sub.StableTag != "xcp-sub_1" || sub.IsDefault {
		t.Errorf("stable_tag/is_default after clear: %+v", sub)
	}

	// Снятие выбора у выбранного, но недефолтного узла в нескольких подписках.
	addOrderSub(t, env, "sub_b")
	tagB := nodeTagByServer(t, env, "sub_b", "3.3.3.3:443")
	if err := env.svc.SetActiveNode("sub_b", tagB); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.ClearActiveNode("sub_b"); err != nil {
		t.Fatal(err)
	}
	merged = simulateXrayConfdirMerge(t, env.xrayDir)
	if merged[0]["tag"] != "direct" {
		t.Fatalf("first outbound must be direct, got %v", merged[0]["tag"])
	}
	for _, want := range []string{"xcp-sub_1", "xcp-sub_b"} {
		if ob, _ := mergedByTag(merged, want); ob == nil {
			t.Errorf("%s must stay reachable", want)
		}
	}
}
