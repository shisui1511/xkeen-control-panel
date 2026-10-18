//go:build router

package sources

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сквозная проверка сервиса источников на устройстве: «Мои узлы» от вставки до
// файлов обоих ядер. Слой и бинарники ядер настоящие (проверка `xray run -test` и
// `mihomo -t` идёт конвейером слоя), вердикт — файлы на диске и ответы сервиса.
// Перезапуск ядер не настроен (у слоя нет KernelApplier): шаг пропускается, ядра
// не трогаются.

func TestRouterSourcesTracer(t *testing.T) {
	rtNeedKernels(t, true, true)
	e := rtNewEnv(t, true, true)
	link := rtLink(t, "vless_ws")
	ctx := context.Background()

	view, err := e.svc.Preview(ctx, inputOf(link))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if view.MyNodes == nil || view.MyNodes.Count != 1 || view.Addable != 1 || view.Token == "" {
		t.Fatalf("предпросмотр: %+v", view)
	}
	if !view.MyNodes.Nodes[0].Support.Xray.OK || !view.MyNodes.Nodes[0].Support.Mihomo.OK {
		t.Errorf("vless ws должен быть доступен обоим ядрам: %+v", view.MyNodes.Nodes[0].Support)
	}
	// Предпросмотр ничего не пишет в черновик.
	if got := e.layer.Snapshot().DraftChanges; got != 0 {
		t.Errorf("предпросмотр изменил черновик: %d", got)
	}

	res, err := e.svc.Add(e.rev(t), view.Token)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if res.DraftChanges != 1 || res.Nodes != 1 {
		t.Errorf("итог «Добавить»: %+v", res)
	}
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseMy(st.Draft[MySection])
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Entries) != 1 || cfg.Entries[0].Raw != link || !cfg.Enabled {
		t.Fatalf("секция src_my после «Добавить»: %+v", cfg)
	}
	entryID := cfg.Entries[0].ID

	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение: %+v", done.Result)
	}

	// Xray: один outbound со стабильным тегом и меткой 255.
	data, err := os.ReadFile(filepath.Join(e.roots.Xray, "04_outbounds.xcp-my.tail.json"))
	if err != nil {
		t.Fatalf("файл Xray не записан: %v", err)
	}
	var xf struct {
		Outbounds []struct {
			Tag            string `json:"tag"`
			StreamSettings struct {
				Sockopt struct {
					Mark int `json:"mark"`
				} `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &xf); err != nil {
		t.Fatalf("файл Xray не JSON: %v", err)
	}
	if len(xf.Outbounds) != 1 || xf.Outbounds[0].Tag != "xcp-my-"+entryID || xf.Outbounds[0].StreamSettings.Sockopt.Mark != 255 {
		t.Errorf("outbounds Xray: %+v", xf.Outbounds)
	}

	// Mihomo: провайдер с одним узлом, имя из ссылки.
	data, err = os.ReadFile(filepath.Join(e.roots.Mihomo, "proxy_providers", "xcp-my.yaml"))
	if err != nil {
		t.Fatalf("файл Mihomo не записан: %v", err)
	}
	var mf struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &mf); err != nil {
		t.Fatalf("файл Mihomo не YAML: %v", err)
	}
	if len(mf.Proxies) != 1 || mf.Proxies[0]["name"] != "VLESS WS" {
		t.Errorf("proxies Mihomo: %+v", mf.Proxies)
	}

	if got := e.layer.Snapshot().DraftChanges; got != 0 {
		t.Errorf("после «Применить» неприменённых изменений: %d", got)
	}
}

func TestRouterPreviewTokenSingleUse(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	ctx := context.Background()

	view, err := e.svc.Preview(ctx, inputOf(rtLink(t, "vless_ws")))
	if err != nil || view.Token == "" {
		t.Fatalf("Preview: %v, %+v", err, view)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Errorf("повторный Add с тем же токеном: %v", err)
	}
	if _, err := e.svc.Add(e.rev(t), "0123456789abcdef0123456789abcdef"); !errors.Is(err, ErrPreviewExpired) {
		t.Errorf("Add с неизвестным токеном: %v", err)
	}

	// Токен старше десяти минут.
	old, err := e.svc.Preview(ctx, inputOf(rtLink(t, "vless_reality")))
	if err != nil || old.Token == "" {
		t.Fatalf("Preview: %v, %+v", err, old)
	}
	e.clock.Advance(10*time.Minute + time.Second)
	if _, err := e.svc.Add(e.rev(t), old.Token); !errors.Is(err, ErrPreviewExpired) {
		t.Errorf("Add с просроченным токеном: %v", err)
	}

	// В кэше не больше восьми предпросмотров: самый старый вытесняется.
	var tokens []string
	for i := 0; i < maxPreviewEntries+1; i++ {
		link := strings.Replace(rtLink(t, "vless_ws"), "node1.example", "cache"+string(rune('a'+i))+".example", 1)
		v, err := e.svc.Preview(ctx, inputOf(link))
		if err != nil || v.Token == "" {
			t.Fatalf("Preview %d: %v, %+v", i, err, v)
		}
		tokens = append(tokens, v.Token)
	}
	if _, err := e.svc.Add(e.rev(t), tokens[0]); !errors.Is(err, ErrPreviewExpired) {
		t.Errorf("вытесненный токен: %v", err)
	}
	if _, err := e.svc.Add(e.rev(t), tokens[len(tokens)-1]); err != nil {
		t.Errorf("последний токен: %v", err)
	}
}

func TestRouterAddRevisionConflict(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	view, err := e.svc.Preview(context.Background(), inputOf(rtLink(t, "vless_ws")))
	if err != nil || view.Token == "" {
		t.Fatalf("Preview: %v, %+v", err, view)
	}
	stale := e.rev(t)
	// Чужая правка черновика меняет ревизию.
	if _, err := e.layer.EditDraft(stale, "other", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Add(stale, view.Token); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Fatalf("Add на устаревшей ревизии: %v", err)
	}
	// Токен остаётся действительным: «Добавить» на свежей ревизии проходит.
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Errorf("Add после конфликта: %v", err)
	}
}

// rtAssertKeys проверяет, что ключи объекта — подмножество allowed и содержат required.
func rtAssertKeys(t *testing.T, where string, obj map[string]any, required, allowed []string) {
	t.Helper()
	allow := map[string]bool{}
	for _, k := range allowed {
		allow[k] = true
	}
	for _, k := range required {
		allow[k] = true
		if _, ok := obj[k]; !ok {
			t.Errorf("%s: нет ключа %q", where, k)
		}
	}
	for k := range obj {
		if !allow[k] {
			t.Errorf("%s: лишний ключ %q", where, k)
		}
	}
}

// rtNoUpperKeys обходит JSON и ловит ключи с заглавной буквой (CamelCase Go).
func rtNoUpperKeys(t *testing.T, where string, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			for _, r := range k {
				if unicode.IsUpper(r) {
					t.Errorf("%s: ключ %q с заглавной буквой", where, k)
					break
				}
			}
			rtNoUpperKeys(t, where+"."+k, val)
		}
	case []any:
		for _, val := range x {
			rtNoUpperKeys(t, where+"[]", val)
		}
	}
}

func TestRouterPreviewViewJSONKeys(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	ctx := context.Background()

	view, err := e.svc.Preview(ctx, inputOf(rtLink(t, "vless_ws")+"\nfoo://x"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	rtAssertKeys(t, "предпросмотр", top, []string{"token", "expires_at", "subscriptions", "my_nodes", "errors", "addable", "nodes_only", "collapsed"}, nil)
	rtNoUpperKeys(t, "предпросмотр", top)

	my, _ := top["my_nodes"].(map[string]any)
	if my == nil {
		t.Fatalf("my_nodes пуст: %s", raw)
	}
	rtAssertKeys(t, "my_nodes", my, []string{"nodes", "count", "only", "skipped"}, nil)
	if only, _ := my["only"].(map[string]any); only != nil {
		rtAssertKeys(t, "only", only, []string{"xray", "mihomo"}, nil)
	} else {
		t.Error("my_nodes.only не объект")
	}
	list, _ := my["nodes"].([]any)
	if len(list) != 1 {
		t.Fatalf("узлов в предпросмотре: %d", len(list))
	}
	node, _ := list[0].(map[string]any)
	rtAssertKeys(t, "узел", node, []string{"key", "name", "protocol", "address", "cred_mask", "support", "marks"}, []string{"dup_sub_name"})
	if support, _ := node["support"].(map[string]any); support != nil {
		rtAssertKeys(t, "support", support, []string{"xray", "mihomo"}, nil)
		for _, kernel := range []string{"xray", "mihomo"} {
			v, _ := support[kernel].(map[string]any)
			rtAssertKeys(t, "вердикт "+kernel, v, []string{"ok"}, []string{"code", "params", "notes"})
		}
	}
	if marks, ok := node["marks"].([]any); !ok || marks == nil {
		t.Errorf("marks должен быть списком: %v", node["marks"])
	}

	errs, _ := top["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("ошибок строк: %d, ответ %s", len(errs), raw)
	}
	le, _ := errs[0].(map[string]any)
	rtAssertKeys(t, "ошибка строки", le, []string{"line", "fragment", "error"}, []string{"file"})
	if se, _ := le["error"].(map[string]any); se != nil {
		rtAssertKeys(t, "SourceError", se, []string{"code"}, []string{"params", "http_status", "line", "fragment", "detail"})
	}

	// Пустой ввод: списки пустые, а не null.
	empty, err := e.svc.Preview(ctx, inputOf(""))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	var emptyTop map[string]any
	if err := json.Unmarshal(raw, &emptyTop); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"subscriptions", "errors"} {
		if v, ok := emptyTop[k].([]any); !ok || v == nil || len(v) != 0 {
			t.Errorf("%s пустого ввода: %v, нужен []", k, emptyTop[k])
		}
	}
	if emptyTop["my_nodes"] != nil {
		t.Errorf("my_nodes пустого ввода: %v, нужен null", emptyTop["my_nodes"])
	}
}
