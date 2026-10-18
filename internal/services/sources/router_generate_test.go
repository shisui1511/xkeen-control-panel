//go:build router

package sources

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Правила генераторов на настоящих ссылках testdata/links: генератор вызывается
// со входом из секций, вердикт — содержимое выданных файлов и поведение реестра
// слоя. Ядра не запускаются.

var rtBoth = configlayer.InstalledKernels{Xray: true, Mihomo: true}

// rtEntries собирает секцию src_my из пар «id записи, исходный текст».
func rtEntries(t *testing.T, enabled bool, pairs ...string) json.RawMessage {
	t.Helper()
	cfg := MySourceConfig{Kind: KindMy, Enabled: enabled, Entries: []MyEntry{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		cfg.Entries = append(cfg.Entries, MyEntry{ID: pairs[i], Name: "", Format: "link", Raw: pairs[i+1]})
	}
	return rtSection(t, cfg)
}

func rtInput(sections map[string]json.RawMessage) configlayer.Input {
	return configlayer.Input{Sections: configlayer.Sections(sections), Mode: configlayer.SourceDraft}
}

// rtXrayTags — теги outbounds файла Xray.
func rtXrayTags(t *testing.T, files []configlayer.GeneratedFile) []string {
	t.Helper()
	if len(files) == 0 {
		return nil
	}
	if len(files) != 1 || files[0].RelPath != "04_outbounds.xcp-my.tail.json" || files[0].Kind != configlayer.KindXrayJSON {
		t.Fatalf("файлы Xray: %+v", files)
	}
	var f struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(files[0].Content, &f); err != nil {
		t.Fatalf("файл Xray не JSON: %v", err)
	}
	tags := make([]string, 0, len(f.Outbounds))
	for _, o := range f.Outbounds {
		tags = append(tags, o.Tag)
	}
	return tags
}

// rtMihomoProxies — прокси файла Mihomo.
func rtMihomoProxies(t *testing.T, files []configlayer.GeneratedFile) []map[string]any {
	t.Helper()
	if len(files) == 0 {
		return nil
	}
	f := files[0]
	if len(files) != 1 || f.RelPath != "proxy_providers/xcp-my.yaml" || f.Kind != configlayer.KindMihomoProxyProvider || !f.Inert || f.Expect != nil {
		t.Fatalf("файлы Mihomo (нужен один Inert без Expect): %+v", files)
	}
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(f.Content, &doc); err != nil {
		t.Fatalf("файл Mihomo не YAML: %v", err)
	}
	return doc.Proxies
}

func rtNames(proxies []map[string]any) []string {
	out := make([]string, 0, len(proxies))
	for _, p := range proxies {
		s, _ := p["name"].(string)
		out = append(out, s)
	}
	return out
}

func rtProxyTypes(proxies []map[string]any) []string {
	out := make([]string, 0, len(proxies))
	for _, p := range proxies {
		s, _ := p["type"].(string)
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func TestRouterGenerateOnlyCapableKernel(t *testing.T) {
	e := rtNewEnv(t, false, false)
	// tuic умеет только Mihomo, vless kcp — только Xray, hysteria v1 — ни одно ядро.
	in := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000001", rtLink(t, "tuic"),
		"00000002", rtLink(t, "vless_ws"),
		"00000003", rtLink(t, "vless_kcp"),
		"00000004", rtLink(t, "hysteria_v1"),
	)})

	tags := rtXrayTags(t, rtGenerate(t, e.svc, kernelXray, in, rtBoth))
	sort.Strings(tags)
	if want := []string{"xcp-my-00000002", "xcp-my-00000003"}; strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Errorf("теги Xray %v, ожидались %v (tuic и hysteria v1 Xray не умеет)", tags, want)
	}

	proxies := rtMihomoProxies(t, rtGenerate(t, e.svc, kernelMihomo, in, rtBoth))
	if types := rtProxyTypes(proxies); strings.Join(types, ",") != "tuic,vless" {
		t.Errorf("типы Mihomo %v, ожидались tuic и vless (kcp и hysteria v1 не выражаются)", types)
	}
	if len(proxies) != 2 {
		t.Errorf("узлов в файле Mihomo: %d, нужно 2", len(proxies))
	}
}

func TestRouterGenerateDisabledSource(t *testing.T) {
	e := rtNewEnv(t, false, false)
	link := rtLink(t, "vless_ws")
	off := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, false, "00000001", link)})
	for _, k := range []string{kernelXray, kernelMihomo} {
		if files := rtGenerate(t, e.svc, k, off, rtBoth); len(files) != 0 {
			t.Errorf("выключенный источник дал файлы для %s: %d", k, len(files))
		}
	}
	// Записи выключенного источника сохраняются в секции.
	cfg, err := parseMy(off.Sections[MySection])
	if err != nil || len(cfg.Entries) != 1 || cfg.Enabled {
		t.Fatalf("секция выключенного источника: %+v, %v", cfg, err)
	}

	// Включение — правкой черновика слоя: файлы снова генерируются.
	e.addLinks(t, link)
	cur := e.my(t)
	cur.Enabled = false
	if _, err := e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{MySection: rtSection(t, cur)}, nil); err != nil {
		t.Fatal(err)
	}
	if files := rtGenerate(t, e.svc, kernelXray, e.draftInput(t), rtBoth); len(files) != 0 {
		t.Errorf("после выключения в черновике файл Xray остался")
	}
	cur.Enabled = true
	if _, err := e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{MySection: rtSection(t, cur)}, nil); err != nil {
		t.Fatal(err)
	}
	if tags := rtXrayTags(t, rtGenerate(t, e.svc, kernelXray, e.draftInput(t), rtBoth)); len(tags) != 1 {
		t.Errorf("после включения теги Xray: %v", tags)
	}
}

func TestRouterGenerateMihomoNames(t *testing.T) {
	e := rtNewEnv(t, false, false)
	link := func(host string) string {
		return "vless://" + rtUUIDA + "@" + host + ":443?encryption=none&security=none&type=tcp#%D0%A3%D0%B7%D0%B5%D0%BB"
	}
	forward := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000001", link("h1.example"), "00000002", link("h2.example"), "00000003", link("h3.example"))})
	reverse := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000003", link("h3.example"), "00000002", link("h2.example"), "00000001", link("h1.example"))})

	// Имя записи пустое: берётся имя узла; одинаковые имена получают суффиксы по порядку ключей.
	files := rtGenerate(t, e.svc, kernelMihomo, forward, rtBoth)
	names := rtNames(rtMihomoProxies(t, files))
	if want := "Узел,Узел 2,Узел 3"; strings.Join(names, ",") != want {
		t.Errorf("имена %q, ожидались %q", strings.Join(names, ","), want)
	}
	// Перестановка записей в секции не меняет файл ни на байт.
	again := rtGenerate(t, e.svc, kernelMihomo, reverse, rtBoth)
	if len(again) != 1 || !bytes.Equal(files[0].Content, again[0].Content) {
		t.Error("перестановка записей изменила файл Mihomo")
	}
	x1 := rtGenerate(t, e.svc, kernelXray, forward, rtBoth)
	x2 := rtGenerate(t, e.svc, kernelXray, reverse, rtBoth)
	if len(x1) != 1 || len(x2) != 1 || !bytes.Equal(x1[0].Content, x2[0].Content) {
		t.Error("перестановка записей изменила файл Xray")
	}
}

func TestRouterGenerateInstalledOnly(t *testing.T) {
	e := rtNewEnv(t, false, false)
	in := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true, "00000001", rtLink(t, "vless_ws"))})
	reg := configlayer.NewRegistry()
	for _, g := range e.svc.Generators() {
		reg.Register(g)
	}
	onlyXray, err := reg.Build(in, configlayer.InstalledKernels{Xray: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyXray) != 1 || onlyXray[0].Kernel != kernelXray {
		t.Errorf("при одном Xray файлы: %+v", onlyXray)
	}
	both, err := reg.Build(in, rtBoth)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Fatalf("при двух ядрах файлов %d, нужно 2", len(both))
	}
	for _, f := range both {
		if !configlayer.IsPanelFileName(f.Kernel, f.RelPath) {
			t.Errorf("не файл панели: %s %s", f.Kernel, f.RelPath)
		}
	}
	// Генератор сам для неустановленного ядра ничего не выдаёт.
	if files := rtGenerate(t, e.svc, kernelMihomo, in, configlayer.InstalledKernels{Xray: true}); len(files) != 0 {
		t.Errorf("генератор Mihomo выдал файлы без установленного Mihomo")
	}
}

func TestRouterGenerateMemo(t *testing.T) {
	e := rtNewEnv(t, false, false)
	in := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000001", rtLink(t, "vless_ws"), "00000002", rtLink(t, "tuic"), "00000003", rtLink(t, "vless_kcp"))})
	first := rtGenerate(t, e.svc, kernelXray, in, rtBoth)
	afterFirst := e.svc.parses.Load()
	if afterFirst != 3 {
		t.Fatalf("после первой сборки разборов %d, нужно по одному на запись (3)", afterFirst)
	}
	for i := 0; i < 5; i++ {
		again := rtGenerate(t, e.svc, kernelXray, in, rtBoth)
		if len(again) != len(first) || !bytes.Equal(again[0].Content, first[0].Content) {
			t.Fatal("повторная сборка дала другой файл")
		}
		rtGenerate(t, e.svc, kernelMihomo, in, rtBoth)
	}
	if got := e.svc.parses.Load(); got != afterFirst {
		t.Errorf("повторные сборки заново разбирали текст: %d -> %d разборов", afterFirst, got)
	}
	if n := len(e.svc.gens); n != 2 {
		t.Errorf("запомнено результатов сборки: %d, нужно 2 (по ядру)", n)
	}
	// Правка записи — новый текст — разбирается заново, результат другой.
	changed := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000001", strings.Replace(rtLink(t, "vless_ws"), rtUUIDA, rtUUIDB, 1))})
	got := rtGenerate(t, e.svc, kernelXray, changed, rtBoth)
	if len(got) != 1 || bytes.Equal(got[0].Content, first[0].Content) {
		t.Error("смена текста записи не изменила файл")
	}
	if e.svc.parses.Load() != afterFirst+1 {
		t.Errorf("новый текст должен разбираться ровно один раз: %d разборов", e.svc.parses.Load())
	}
}
