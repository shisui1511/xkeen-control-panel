package services

import (
	"bytes"
	"encoding/json"
	"errors"
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
