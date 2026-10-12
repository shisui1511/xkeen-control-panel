//go:build router

package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Фильтр исключений, несовместимые родные узлы Xray-JSON и пометка «есть в
// подписке X». Вердикт — настоящая матрица возможностей и настоящие эмиттеры; ядро
// Xray устройства проверяет итоговый файл при «Применить».

// rtInputNodes разбирает входной файл testdata/inputs как тело ответа провайдера.
func rtInputNodes(t *testing.T, file string) []nodes.Node {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(routertest.InputsDir(t), file))
	if err != nil {
		t.Fatalf("входной файл %s: %v", file, err)
	}
	res, err := nodes.ParseBody(data)
	if err != nil {
		t.Fatalf("разбор %s: %v", file, err)
	}
	return res.Nodes
}

func rtNamed(n nodes.Node, name string) nodes.Node {
	n.Name = name
	return n
}

// rtPutList записывает набор узлов в снимок сервиса.
func rtPutList(t *testing.T, e *rtEnv, list []nodes.Node) *Ref {
	t.Helper()
	hash, err := e.svc.blobs.put(list)
	if err != nil {
		t.Fatal(err)
	}
	return rtRefOf(e, hash, list)
}

func rtXrayNames(t *testing.T, e *rtEnv, files []configlayer.GeneratedFile) int {
	t.Helper()
	if len(files) == 0 {
		return 0
	}
	var f struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(files[0].Content, &f); err != nil {
		t.Fatal(err)
	}
	return len(f.Outbounds)
}

// Фильтр «исключить узлы по имени»: подстрока без учёта регистра убирает узлы из
// обоих ядер; пустые шаблоны игнорируются; «Мои узлы» фильтр не затрагивает.
func TestRouterGenerateExcludeFilter(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const id = "a1b2c3d4"
	base := rtNodes(t, "vless_ws", "trojan_grpc", "ss_plain", "vless_reality")
	list := []nodes.Node{
		rtNamed(base[0], "RU-1 быстрый"),
		rtNamed(base[1], "Нидерланды тестовый"),
		rtNamed(base[2], "DE-2"),
		rtNamed(base[3], "FR-3"),
	}
	ref := rtPutList(t, e, list)
	ls := LiveSources{id: {Kernels: map[string]*KernelLive{
		kernelXray:   {Pending: ref, Status: Status{Code: StatusOK}},
		kernelMihomo: {Pending: ref, Status: Status{Code: StatusOK}},
	}}}
	gen := func(exclude []string) (xray, mihomo int, mihomoText string) {
		sections := map[string]json.RawMessage{sectionName(id): rtSubNamed(t, id, "Провайдер", exclude)}
		in := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
		xray = rtXrayNames(t, e, rtGenerate(t, e.svc, kernelXray, in, rtBoth))
		if files := rtGenerate(t, e.svc, kernelMihomo, in, rtBoth); len(files) == 1 {
			mihomoText = string(files[0].Content)
			mihomo = strings.Count(mihomoText, "- name:")
		}
		return
	}

	x, m, _ := gen(nil)
	if x != 4 || m != 4 {
		t.Fatalf("без фильтра: Xray %d, Mihomo %d, нужно по 4", x, m)
	}
	x, m, text := gen([]string{"ru", "Тест", "", "   "})
	if x != 2 || m != 2 {
		t.Errorf("с фильтром [ru, Тест]: Xray %d, Mihomo %d, нужно по 2", x, m)
	}
	for _, gone := range []string{"RU-1", "тестовый"} {
		if strings.Contains(text, gone) {
			t.Errorf("исключённый узел %q остался в файле Mihomo", gone)
		}
	}
	for _, kept := range []string{"DE-2", "FR-3"} {
		if !strings.Contains(text, kept) {
			t.Errorf("узел %q пропал из файла Mihomo", kept)
		}
	}
	if x, m, _ = gen([]string{"", "  "}); x != 4 || m != 4 {
		t.Errorf("пустые шаблоны: Xray %d, Mihomo %d, нужно по 4", x, m)
	}
	// Ключи и теги считаются от полного снимка: исключение одних узлов не меняет
	// теги остальных.
	tagsOf := func(exclude []string) []string {
		sections := map[string]json.RawMessage{sectionName(id): rtSubNamed(t, id, "Провайдер", exclude)}
		in := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
		return rtSubXrayTags(t, rtGenerate(t, e.svc, kernelXray, in, rtBoth), id)
	}
	full := map[string]bool{}
	for _, tag := range tagsOf(nil) {
		full[tag] = true
	}
	for _, tag := range tagsOf([]string{"ru", "Тест"}) {
		if !full[tag] {
			t.Errorf("тег %s при фильтре отличается от тегов полного набора", tag)
		}
	}

	// «Мои узлы»: фильтра нет, узел с «ru» в имени остаётся.
	myRaw := rtEntries(t, true, "00000001", rtLink(t, "vless_ws"))
	var cfg MySourceConfig
	if err := json.Unmarshal(myRaw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Entries[0].Name = "RU-свой"
	if myRaw, err := encodeMy(cfg); err != nil {
		t.Fatal(err)
	} else if got := rtXrayNames(t, e, rtGenerate(t, e.svc, kernelXray, rtInput(map[string]json.RawMessage{MySection: myRaw}), rtBoth)); got != 1 {
		t.Errorf("«Мои узлы»: узлов Xray %d, нужен 1", got)
	}
}

// rtIncompatible — родные узлы Xray-JSON с запретами Xray 26.x (allowInsecure, h2,
// httpSettings, Host в заголовках xhttp) и один совместимый.
func rtIncompatible(t *testing.T) []nodes.Node {
	t.Helper()
	bad := rtInputNodes(t, "xray_forbidden_fields.json")
	good := rtInputNodes(t, "xray_forbidden_transport.json")
	if len(bad) != 4 || len(good) != 1 {
		t.Fatalf("входные файлы: несовместимых %d, совместимых %d", len(bad), len(good))
	}
	return append(append([]nodes.Node(nil), bad...), good...)
}

// Родной узел Xray-JSON, несовместимый с Xray 26.x, не попадает в файл Xray и виден
// с кодом причины; остальные узлы подписки и файл Mihomo не страдают.
func TestRouterGenerateRawIncompatibleExcluded(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const id = "a1b2c3d4"
	list := rtIncompatible(t)
	wantCodes := []string{nodes.CodeXrayNoInsecure, nodes.CodeXrayNoHTTPTransport, nodes.CodeXrayNoHTTPTransport, nodes.CodeXrayForbiddenKey}
	caps := e.svc.caps(rtBoth)
	for i, n := range list[:4] {
		v := nodes.Support(n, kernelXray, caps)
		if v.OK || v.Code != wantCodes[i] {
			t.Errorf("узел %q: вердикт Xray %+v, нужен код %s", n.Name, v, wantCodes[i])
		}
		if !nodes.Support(n, kernelMihomo, caps).OK {
			t.Errorf("узел %q: Mihomo должен принимать узел", n.Name)
		}
	}

	ref := rtPutList(t, e, list)
	ls := LiveSources{id: {Kernels: map[string]*KernelLive{
		kernelXray:   {Pending: ref, Status: Status{Code: StatusOK}},
		kernelMihomo: {Pending: ref, Status: Status{Code: StatusOK}},
	}}}
	sections := map[string]json.RawMessage{sectionName(id): rtSubNamed(t, id, "Провайдер", nil)}
	in := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)

	xrayFiles := rtGenerate(t, e.svc, kernelXray, in, rtBoth)
	if got := rtXrayNames(t, e, xrayFiles); got != 1 {
		t.Fatalf("outbound в файле Xray: %d, нужен 1 (остальные несовместимы)", got)
	}
	text := string(xrayFiles[0].Content)
	for _, forbidden := range []string{"allowInsecure", `"h2"`, "httpSettings"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("в файле Xray запрещённый ключ %s", forbidden)
		}
	}
	mfiles := rtGenerate(t, e.svc, kernelMihomo, in, rtBoth)
	if len(mfiles) != 1 || strings.Count(string(mfiles[0].Content), "- name:") != 5 {
		t.Errorf("файл Mihomo должен содержать все 5 узлов: %d файлов", len(mfiles))
	}
}

// Один несовместимый узел не блокирует «Применить»: ядро Xray устройства проверяет
// файл (xray run -test), остальные узлы подписки применяются.
func TestRouterApplyWithIncompatibleNodes(t *testing.T) {
	rtNeedKernels(t, true, false)
	e := rtNewEnv(t, true, false)
	const id = "a1b2c3d4"
	ref := rtPutList(t, e, rtIncompatible(t))
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, false, map[string]*Ref{kernelXray: ref})
	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение подписки с несовместимыми узлами: %+v", done.Result)
	}
	data, err := os.ReadFile(filepath.Join(e.roots.Xray, "04_outbounds.xcp-"+id+".tail.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "allowInsecure") {
		t.Error("в применённом файле Xray allowInsecure")
	}
}

// Узел «Моих узлов», который есть в применённом наборе подписки, помечается «есть в
// подписке X»; добавить его можно.
func TestRouterPreviewDupInSubscription(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws", "trojan_ws")
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, true, map[string]*Ref{kernelXray: ref})

	view, err := e.svc.Preview(context.Background(), inputOf(rtLink(t, "vless_ws")+"\n"+rtLink(t, "tuic")))
	if err != nil || view.MyNodes == nil || len(view.MyNodes.Nodes) != 2 {
		t.Fatalf("Preview: %v, %+v", err, view.MyNodes)
	}
	marked := 0
	for _, n := range view.MyNodes.Nodes {
		has := false
		for _, m := range n.Marks {
			if m == MarkDupInSub {
				has = true
			}
		}
		switch n.Protocol {
		case "vless":
			if !has || n.DupSubName != "Провайдер" {
				t.Errorf("узел vless: метки %v, подписка %q", n.Marks, n.DupSubName)
			}
			marked++
		default:
			if has || n.DupSubName != "" {
				t.Errorf("узел %s помечен без совпадения: %v %q", n.Protocol, n.Marks, n.DupSubName)
			}
		}
	}
	if marked != 1 {
		t.Errorf("помеченных узлов %d, нужен 1", marked)
	}
	if view.Addable != 2 || view.Token == "" {
		t.Errorf("добавляемых %d (нужно 2), токен %q", view.Addable, view.Token)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add узла из подписки: %v", err)
	}
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseMy(st.Draft[MySection])
	if err != nil || len(cfg.Entries) != 2 {
		t.Fatalf("записей «Моих узлов» после добавления: %d, %v", len(cfg.Entries), err)
	}
	var protocols []string
	for _, en := range cfg.Entries {
		n, serr := e.svc.parseRaw(en.Raw)
		if serr != nil {
			t.Fatal(serr)
		}
		protocols = append(protocols, n.Protocol)
	}
	sort.Strings(protocols)
	if strings.Join(protocols, ",") != "tuic,vless" {
		t.Errorf("добавлены: %v", protocols)
	}
}
