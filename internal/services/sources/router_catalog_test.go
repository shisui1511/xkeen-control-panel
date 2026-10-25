//go:build router

package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Каталог узлов (Service.Catalog) и пакетные правки черновика для каскада групп.
// Вердикт — файлы, которые выдают генераторы источников для того же Input, и
// ядра устройства (`xray run -test`, `mihomo -t`). Имена узлов в сообщения не
// попадают: только числа и ссылки <источник>/<8 hex>.

// rtFilesDigest — отпечаток содержимого всех файлов в порядке выдачи.
func rtFilesDigest(files []configlayer.GeneratedFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Kernel + "\x00" + f.RelPath + "\x00"))
		h.Write(f.Content)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// rtFileTags — теги outbounds файлов Xray в порядке записи (файлы по порядку выдачи).
func rtFileTags(t *testing.T, files []configlayer.GeneratedFile) []string {
	t.Helper()
	var tags []string
	for _, f := range files {
		var doc struct {
			Outbounds []struct {
				Tag string `json:"tag"`
			} `json:"outbounds"`
		}
		if err := json.Unmarshal(f.Content, &doc); err != nil {
			t.Fatalf("файл Xray не JSON: %v", err)
		}
		for _, o := range doc.Outbounds {
			tags = append(tags, o.Tag)
		}
	}
	return tags
}

// rtFileNames — имена прокси файлов Mihomo в порядке записи.
func rtFileNames(t *testing.T, files []configlayer.GeneratedFile) []string {
	t.Helper()
	var names []string
	for _, f := range files {
		var doc struct {
			Proxies []struct {
				Name string `yaml:"name"`
			} `yaml:"proxies"`
		}
		if err := yaml.Unmarshal(f.Content, &doc); err != nil {
			t.Fatalf("файл Mihomo не YAML: %v", err)
		}
		for _, p := range doc.Proxies {
			names = append(names, p.Name)
		}
	}
	return names
}

func rtCatalogTags(list []CatalogNode) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.XrayTag)
	}
	return out
}

func rtCatalogNames(list []CatalogNode) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.MihomoName)
	}
	return out
}

// rtCatalogMatches сверяет каталог с файлами генераторов того же Input: состав,
// порядок, теги Xray и имена Mihomo.
func rtCatalogMatches(t *testing.T, e *rtEnv, in configlayer.Input, installed configlayer.InstalledKernels) (Catalog, []configlayer.GeneratedFile, []configlayer.GeneratedFile) {
	t.Helper()
	cat, err := e.svc.Catalog(in, installed)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	xf := rtGenerate(t, e.svc, kernelXray, in, installed)
	mf := rtGenerate(t, e.svc, kernelMihomo, in, installed)
	if got, want := rtCatalogTags(cat.Xray), rtFileTags(t, xf); len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Errorf("теги Xray каталога %d и файлов %d расходятся (порядок или состав)", len(got), len(want))
	}
	if got, want := rtCatalogNames(cat.Mihomo), rtFileNames(t, mf); len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Errorf("имена Mihomo каталога %d и файлов %d расходятся (порядок или состав)", len(got), len(want))
	}
	for _, n := range cat.Xray {
		if n.XrayTag == "" || n.MihomoName != "" || n.Ref == "" || n.Key == "" || n.Source == "" {
			t.Errorf("узел Xray с неполными полями: ref=%s", n.Ref)
		}
	}
	for _, n := range cat.Mihomo {
		if n.MihomoName == "" || n.XrayTag != "" || n.Ref == "" || n.Key == "" || n.Source == "" {
			t.Errorf("узел Mihomo с неполными полями: ref=%s", n.Ref)
		}
	}
	return cat, xf, mf
}

// rtKernelsAccept прогоняет файлы источника через ядра устройства: xray run -test
// по каталогу с журналом и файлами, mihomo -t по профилю с этим провайдером.
//
// xrayMark — метка известного отказа Xray от mKCP с header/seed (находка вне фазы):
// применяется только к такому отказу, другой отказ остаётся провалом.
func rtKernelsAccept(t *testing.T, e *rtEnv, xf, mf []configlayer.GeneratedFile, xrayMark routertest.KnownMark) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	defer services.AcquireKernelHelper(ctx)()

	if len(xf) > 0 {
		dir, err := os.MkdirTemp(e.dir, "chk-xray-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01_log.json"), []byte("{\"log\":{\"loglevel\":\"none\"}}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, f := range xf {
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(f.RelPath)), f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		out, err := exec.CommandContext(ctx, rtXrayBin, "run", "-test", "-confdir", dir).CombinedOutput()
		switch {
		case xrayMark.Slug != "" && (err == nil || strings.Contains(string(out), rtXrayMKCPRemoved)):
			routertest.Verdict(t, err == nil, xrayMark, "xray run -test отверг файл источника: %v: %s", err, rtKernelReason(out))
		case err != nil:
			t.Errorf("xray run -test отверг файл источника: %v: %s", err, rtKernelReason(out))
		}
	}
	if len(mf) > 0 {
		dir, err := os.MkdirTemp(e.dir, "chk-mihomo-")
		if err != nil {
			t.Fatal(err)
		}
		var uses []string
		for _, f := range mf {
			path := filepath.Join(dir, f.RelPath)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
			uses = append(uses, strings.TrimSuffix(filepath.Base(f.RelPath), ".yaml"))
		}
		var cfg strings.Builder
		cfg.WriteString("mode: rule\nproxy-providers:\n")
		for i, f := range mf {
			cfg.WriteString("  " + uses[i] + ":\n    type: file\n    path: ./" + f.RelPath + "\n")
		}
		cfg.WriteString("proxy-groups:\n  - name: test-group\n    type: select\n    use:\n")
		for _, u := range uses {
			cfg.WriteString("      - " + u + "\n")
		}
		cfg.WriteString("rules:\n  - MATCH,test-group\n")
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(ctx, rtMihomoBin, "-t", "-d", dir, "-f", filepath.Join(dir, "config.yaml")).CombinedOutput(); err != nil {
			t.Errorf("mihomo -t отверг провайдер источника: %v: %s", err, rtKernelReason(out))
		}
	}
}

// rtXrayMKCPRemoved — отказ Xray 26.2.x–26.3.x от kcpSettings.header/seed (todo
// xray-mkcp-header-seed-finalmask).
const rtXrayMKCPRemoved = "mkcp header & seed has been removed"

// rtKernelAddrRe — адреса и имена хостов в выводе ядра: в сообщение теста они не попадают.
var rtKernelAddrRe = regexp.MustCompile(`([0-9]{1,3}\.){3}[0-9]{1,3}|\[?[0-9a-fA-F]*:[0-9a-fA-F:]+\]?|[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+`)

// rtKernelReason — последняя непустая строка вывода ядра без адресов и хостов
// (не больше 300 байт): причина отказа видна в отчёте без данных узлов.
func rtKernelReason(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := rtKernelAddrRe.ReplaceAllString(strings.TrimSpace(lines[len(lines)-1]), "<addr>")
	if len(last) > 300 {
		last = last[len(last)-300:]
	}
	return last
}

// Каталог узлов — те же узлы, теги Xray и имена Mihomo, что в файлах источников;
// вызов каталога не меняет файлов; узел, которого ядро не умеет, выключенный
// источник и исключённые фильтром узлы в каталог ядра не попадают; подписка с
// разными снимками по ядрам даёт разные списки.
func TestRouterCatalogMatchesFiles(t *testing.T) {
	rtNeedKernels(t, true, true)
	e := rtNewEnv(t, true, true)
	link := rtLink(t, "vless_ws")

	// «Мои узлы»: tuic умеет только Mihomo, vless kcp — только Xray, одна ссылка
	// под двумя id даёт одинаковые имена и суффикс дубля в файле Mihomo.
	in := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, true,
		"00000001", link,
		"00000002", link,
		"00000003", rtLink(t, "tuic"),
		"00000004", rtLink(t, "vless_kcp"),
		"00000005", rtLink(t, "trojan_grpc"),
	)})

	xBefore := rtGenerate(t, e.svc, kernelXray, in, rtBoth)
	mBefore := rtGenerate(t, e.svc, kernelMihomo, in, rtBoth)
	dXBefore, dMBefore := rtFilesDigest(xBefore), rtFilesDigest(mBefore)

	cat, xf, mf := rtCatalogMatches(t, e, in, rtBoth)
	if rtFilesDigest(xf) != dXBefore || rtFilesDigest(mf) != dMBefore {
		t.Fatal("вызов каталога изменил содержимое файлов источников")
	}

	// Тег Xray и ссылка для «Моих узлов» строятся от id записи.
	tags := map[string]bool{}
	for _, n := range cat.Xray {
		tags[n.XrayTag] = true
		if n.Source != MySourceID || n.SourceName != "" {
			t.Errorf("источник узла Xray: %s/%q", n.Source, n.SourceName)
		}
	}
	for _, id := range []string{"00000001", "00000002", "00000004", "00000005"} {
		if !tags["xcp-my-"+id] {
			t.Errorf("тега xcp-my-%s нет в каталоге Xray", id)
		}
	}
	if tags["xcp-my-00000003"] {
		t.Error("tuic попал в каталог Xray")
	}
	for _, n := range cat.Xray {
		if n.Ref != "my/"+strings.TrimPrefix(n.XrayTag, "xcp-my-") {
			t.Errorf("ссылка %s не согласована с тегом", n.Ref)
		}
	}
	// В Mihomo нет kcp, tuic есть; одинаковые имена получили суффикс дубля.
	var hasTuic, hasKCP bool
	seen := map[string]bool{}
	for _, n := range cat.Mihomo {
		if n.Ref == "my/00000003" {
			hasTuic = true
		}
		if n.Ref == "my/00000004" {
			hasKCP = true
		}
		if seen[n.MihomoName] {
			t.Errorf("имя Mihomo повторяется: ссылка %s", n.Ref)
		}
		seen[n.MihomoName] = true
	}
	if !hasTuic {
		t.Error("tuic нет в каталоге Mihomo")
	}
	if hasKCP {
		t.Error("vless kcp попал в каталог Mihomo")
	}
	if len(cat.Sources) != 1 || cat.Sources[0].ID != MySourceID || !cat.Sources[0].Enabled || cat.Sources[0].Kind != KindMy {
		t.Errorf("источники каталога: %+v", cat.Sources)
	}

	// Ядра устройства принимают сгенерированные файлы.
	rtKernelsAccept(t, e, xf, mf, routertest.Known("mipsle", "xray-mkcp-header-seed-finalmask"))

	// Установлено одно ядро — список другого пуст.
	only := configlayer.InstalledKernels{Xray: true}
	if c, err := e.svc.Catalog(in, only); err != nil || len(c.Mihomo) != 0 || len(c.Xray) == 0 {
		t.Errorf("каталог при одном ядре: xray=%d mihomo=%d err=%v", len(c.Xray), len(c.Mihomo), err)
	}

	// Выключенный источник — пустые списки, источник в перечне с Enabled=false.
	off := rtInput(map[string]json.RawMessage{MySection: rtEntries(t, false, "00000001", link)})
	c, err := e.svc.Catalog(off, rtBoth)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Xray)+len(c.Mihomo) != 0 || len(c.Sources) != 1 || c.Sources[0].Enabled {
		t.Errorf("выключенный источник: xray=%d mihomo=%d sources=%+v", len(c.Xray), len(c.Mihomo), c.Sources)
	}

	// Подписка с отдельным снимком на каждое ядро и «Мои узлы» в одном Input.
	const sub = "a1b2c3d4"
	xRef := rtPut(t, e, "vless_ws", "trojan_grpc", "ss_plain")
	mRef := rtPut(t, e, "vless_ws", "tuic")
	sections := map[string]json.RawMessage{
		MySection:        rtEntries(t, true, "00000001", link),
		sectionName(sub): rtSubNamed(t, sub, "Провайдер", nil),
	}
	ls := LiveSources{sub: &LiveSource{Kernels: map[string]*KernelLive{
		kernelXray:   {Status: Status{Code: StatusOK, Since: e.clock.Now()}, Pending: xRef},
		kernelMihomo: {Status: Status{Code: StatusOK, Since: e.clock.Now()}, Pending: mRef},
	}}}
	sin := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
	scat, sxf, smf := rtCatalogMatches(t, e, sin, rtBoth)
	if len(scat.Sources) != 2 || scat.Sources[0].ID != sub || scat.Sources[0].Name != "Провайдер" || scat.Sources[1].ID != MySourceID {
		t.Fatalf("источники каталога с подпиской: %+v", scat.Sources)
	}
	count := func(list []CatalogNode, src string) int {
		n := 0
		for _, c := range list {
			if c.Source == src {
				n++
			}
		}
		return n
	}
	if x, m := count(scat.Xray, sub), count(scat.Mihomo, sub); x != 3 || m != 2 {
		t.Errorf("узлов подписки по ядрам: xray=%d mihomo=%d, ожидалось 3 и 2", x, m)
	}
	for _, n := range scat.Xray {
		if n.Source == sub && (n.SourceName != "Провайдер" || !strings.HasPrefix(n.Ref, sub+"/") || n.XrayTag != "xcp-"+sub+"-"+strings.TrimPrefix(n.Ref, sub+"/")) {
			t.Errorf("узел подписки: ссылка %s не согласована с тегом", n.Ref)
		}
	}
	rtKernelsAccept(t, e, sxf, smf, routertest.KnownMark{})

	// Исключение по имени: исключённый узел уходит и из каталога, и из файлов.
	var firstName string
	for _, n := range scat.Xray {
		if n.Source == sub {
			firstName = strings.ToLower(n.Display)
			break
		}
	}
	if firstName == "" {
		t.Fatal("в каталоге Xray нет узлов подписки")
	}
	sections[sectionName(sub)] = rtSubNamed(t, sub, "Провайдер", []string{firstName})
	ein := rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil)
	ecat, _, _ := rtCatalogMatches(t, e, ein, rtBoth)
	if x := count(ecat.Xray, sub); x >= 3 {
		t.Errorf("исключённый фильтром узел остался в каталоге Xray: %d", x)
	}

	// Подписка выключена — её узлов нет ни в одном списке.
	sections[sectionName(sub)] = rtSubSection(t, sub, false)
	dcat, _, _ := rtCatalogMatches(t, e, rtLiveInput(t, configlayer.SourceDraft, sections, ls, nil), rtBoth)
	if count(dcat.Xray, sub)+count(dcat.Mihomo, sub) != 0 {
		t.Error("узлы выключенной подписки остались в каталоге")
	}
}

// rtDraftRaw — секция черновика как строка (пусто, если секции нет).
func rtDraftRaw(t *testing.T, e *rtEnv, name string) string {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return string(st.Draft[name])
}

// DeleteWith и DeleteNodeWith меняют источник и переданные секции одной
// ревизией черновика; секции источников в extra и недопустимые имена отвергаются
// без правки; хук SetDraftExtras кладёт секции в ту же правку, что «Добавить».
func TestRouterDeleteWithAndDraftExtras(t *testing.T) {
	e := rtNewEnv(t, false, false)
	const groups = "test_groups"

	var calls int
	var lastAdded []string
	e.svc.SetDraftExtras(func(st configlayer.StateView, added []string) (map[string]json.RawMessage, error) {
		calls++
		lastAdded = added
		for name := range st.Draft {
			if _, ok := sourceID(name); ok {
				return nil, nil
			}
		}
		return map[string]json.RawMessage{groups: json.RawMessage(`{"v":1}`)}, nil
	})

	// Первый источник: хук кладёт секцию в ту же правку (ревизия +1).
	rev := e.rev(t)
	e.addLinks(t, rtLink(t, "vless_ws"))
	if got := e.rev(t); got != rev+1 {
		t.Fatalf("после первого Add ревизия %d, ожидалась %d", got, rev+1)
	}
	if calls != 1 || len(lastAdded) != 1 || lastAdded[0] != MySourceID {
		t.Errorf("хук после первого Add: вызовов %d, добавлено %v", calls, lastAdded)
	}
	if got := rtDraftRaw(t, e, groups); got != `{"v":1}` {
		t.Errorf("секция групп после первого Add: %q", got)
	}

	// Второй Add: хук вызван, секция не меняется.
	rev = e.rev(t)
	e.addLinks(t, rtLink(t, "trojan_grpc"))
	if calls != 2 || e.rev(t) != rev+1 {
		t.Errorf("второй Add: вызовов %d, ревизия %d (была %d)", calls, e.rev(t), rev)
	}
	if got := rtDraftRaw(t, e, groups); got != `{"v":1}` {
		t.Errorf("секция групп изменилась вторым Add: %q", got)
	}

	// Ошибка хука — ошибка Add без правки черновика, токен остаётся годным.
	e.svc.SetDraftExtras(func(configlayer.StateView, []string) (map[string]json.RawMessage, error) {
		return nil, errors.New("отказ хука")
	})
	view, err := e.svc.Preview(context.Background(), inputOf(rtLink(t, "ss_plain")))
	if err != nil || view.Token == "" {
		t.Fatalf("Preview: %v", err)
	}
	rev = e.rev(t)
	before := rtDraftRaw(t, e, MySection)
	if _, err := e.svc.Add(rev, view.Token); err == nil {
		t.Error("Add прошёл при ошибке хука")
	}
	if e.rev(t) != rev || rtDraftRaw(t, e, MySection) != before {
		t.Error("ошибка хука изменила черновик")
	}
	// Хук с секцией источника — недопустимый ввод, черновик прежний.
	e.svc.SetDraftExtras(func(configlayer.StateView, []string) (map[string]json.RawMessage, error) {
		return map[string]json.RawMessage{MySection: json.RawMessage(`null`)}, nil
	})
	if _, err := e.svc.Add(rev, view.Token); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("Add с секцией источника из хука: %v, ожидался ErrInvalidInput", err)
	}
	if e.rev(t) != rev || rtDraftRaw(t, e, MySection) != before {
		t.Error("секция источника из хука изменила черновик")
	}
	// Без хука — прежнее поведение, тот же токен.
	e.svc.SetDraftExtras(nil)
	if _, err := e.svc.Add(rev, view.Token); err != nil {
		t.Fatalf("Add без хука: %v", err)
	}
	if e.rev(t) != rev+1 {
		t.Errorf("Add без хука: ревизия %d, ожидалась %d", e.rev(t), rev+1)
	}

	// DeleteWith: источник удалён и секция изменена одной ревизией.
	const sub1, sub2 = "a1b2c3d4", "b2c3d4e5"
	rtAddSub(t, e, rtSubNamed(t, sub1, "Провайдер", nil), sub1, false, nil)
	rtAddSub(t, e, rtSubNamed(t, sub2, "Провайдер", nil), sub2, false, nil)
	rev = e.rev(t)
	if _, err := e.svc.DeleteWith(rev, sub1, map[string]json.RawMessage{groups: json.RawMessage(`{"v":2}`)}); err != nil {
		t.Fatalf("DeleteWith: %v", err)
	}
	if e.rev(t) != rev+1 {
		t.Errorf("DeleteWith: ревизия %d, ожидалась %d", e.rev(t), rev+1)
	}
	if rtDraftRaw(t, e, sectionName(sub1)) != "" || rtDraftRaw(t, e, groups) != `{"v":2}` {
		t.Error("DeleteWith: источник не удалён или секция групп не изменена")
	}

	// DeleteWith с секцией источника или недопустимым именем — ошибка, черновик прежний.
	rev = e.rev(t)
	snap := func() string {
		return rtDraftRaw(t, e, groups) + "|" + rtDraftRaw(t, e, sectionName(sub2)) + "|" + rtDraftRaw(t, e, MySection)
	}
	snapshot := snap()
	_, err = e.svc.DeleteWith(rev, sub2, map[string]json.RawMessage{MySection: json.RawMessage(`null`), groups: json.RawMessage(`{"v":3}`)})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("DeleteWith с src_my в extra: %v, ожидался ErrInvalidInput", err)
	}
	_, err = e.svc.DeleteWith(rev, sub2, map[string]json.RawMessage{"Bad-Name": json.RawMessage(`{}`)})
	if !errors.Is(err, configlayer.ErrInvalidSection) {
		t.Errorf("DeleteWith с недопустимым именем секции: %v, ожидался ErrInvalidSection", err)
	}
	if e.rev(t) != rev || snap() != snapshot {
		t.Error("отвергнутый DeleteWith изменил черновик")
	}
	// Устаревшая ревизия — конфликт, ничего не меняется.
	if _, err := e.svc.DeleteWith(rev-1, sub2, map[string]json.RawMessage{groups: json.RawMessage(`{"v":3}`)}); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("DeleteWith на устаревшей ревизии: %v", err)
	}
	if e.rev(t) != rev || snap() != snapshot {
		t.Error("DeleteWith на устаревшей ревизии изменил черновик")
	}

	// DeleteNodeWith: запись «Моих узлов» удалена, секция изменена одной ревизией.
	entries := e.my(t).Entries
	if len(entries) < 2 {
		t.Fatalf("в «Моих узлах» записей %d, нужно не меньше двух", len(entries))
	}
	rev = e.rev(t)
	if _, err := e.svc.DeleteNodeWith(rev, entries[0].ID, map[string]json.RawMessage{groups: json.RawMessage(`{"v":3}`)}); err != nil {
		t.Fatalf("DeleteNodeWith: %v", err)
	}
	if e.rev(t) != rev+1 || rtDraftRaw(t, e, groups) != `{"v":3}` || findEntry(e.my(t), entries[0].ID) >= 0 {
		t.Error("DeleteNodeWith: запись не удалена, секция групп не изменена или ревизия не +1")
	}
	// Секция источника в extra — ошибка, запись на месте.
	rev = e.rev(t)
	left := e.my(t).Entries
	_, err = e.svc.DeleteNodeWith(rev, left[0].ID, map[string]json.RawMessage{sectionName(sub2): nil})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("DeleteNodeWith с src_ в extra: %v, ожидался ErrInvalidInput", err)
	}
	if e.rev(t) != rev || len(e.my(t).Entries) != len(left) {
		t.Error("отвергнутый DeleteNodeWith изменил черновик")
	}
	// nil в extra удаляет секцию.
	if _, err := e.svc.DeleteNodeWith(rev, left[0].ID, map[string]json.RawMessage{groups: nil}); err != nil {
		t.Fatalf("DeleteNodeWith с nil: %v", err)
	}
	if rtDraftRaw(t, e, groups) != "" {
		t.Error("nil в extra не удалил секцию")
	}
	// Delete остаётся обёрткой без extra.
	rev = e.rev(t)
	if _, err := e.svc.Delete(rev, sub2); err != nil || e.rev(t) != rev+1 {
		t.Errorf("Delete: %v, ревизия %d (была %d)", err, e.rev(t), rev)
	}
}
