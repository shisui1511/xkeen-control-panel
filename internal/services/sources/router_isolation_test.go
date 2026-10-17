//go:build router

package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// ISOL-05 (D-37.1): генерация и применение для обоих ядер пишут только файлы
// панели xcp-*; посторонние файлы обоих каталогов и каталог неустановленного ядра
// не меняются. Вердикт — хэши деревьев каталогов до и после настоящего «Применить»
// (проверка ядрами устройства идёт конвейером слоя). Процессы ядер тест не
// запускает и не останавливает: перезапуск у слоя теста не настроен.

// rtTree — хэши всех обычных файлов каталога по относительным путям со слэшами.
func rtTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", root, err)
	}
	return out
}

// rtForeign кладёт в каталоги ядер посторонние файлы: чужие конфигурации и имена
// старого слоя. Ни один не подпадает под правило файлов панели.
func rtForeign(t *testing.T, roots configlayer.Roots) {
	t.Helper()
	files := map[string]string{
		filepath.Join(roots.Xray, "03_inbounds.json"):                  `{"inbounds":[{"tag":"foreign-in","listen":"127.0.0.1","port":10999,"protocol":"socks","settings":{"udp":false}}]}`,
		filepath.Join(roots.Xray, "04_outbounds.json"):                 `{"outbounds":[{"tag":"foreign-direct","protocol":"freedom"}]}`,
		filepath.Join(roots.Xray, "04_outbounds.sub1.tail.json"):       `{"outbounds":[{"tag":"foreign-sub1","protocol":"freedom"}]}`,
		filepath.Join(roots.Mihomo, "config.yaml"):                     "mode: direct\nlog-level: silent\n",
		filepath.Join(roots.Mihomo, "proxy_providers", "foreign.yaml"): "proxies: []\n",
		filepath.Join(roots.Mihomo, "rule_providers", "r.yaml"):        "payload: []\n",
	}
	for p, content := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// rtDiff сравнивает деревья: изменённые и удалённые файлы, новые файлы.
func rtDiff(before, after map[string]string) (changed, removed, added []string) {
	for p, h := range before {
		switch got, ok := after[p]; {
		case !ok:
			removed = append(removed, p)
		case got != h:
			changed = append(changed, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			added = append(added, p)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	sort.Strings(added)
	return
}

func TestRouterIsolationOnlyPanelFiles(t *testing.T) {
	rtNeedKernels(t, true, true)
	e := rtNewEnv(t, true, true)
	rtForeign(t, e.roots)
	beforeX, beforeM := rtTree(t, e.roots.Xray), rtTree(t, e.roots.Mihomo)

	// Узлы обоих типов: vless уходит в оба ядра, tuic только в Mihomo.
	e.addLinks(t, rtLink(t, "vless_ws"), rtLink(t, "tuic"))

	// Каждый файл, который выдают генераторы, — файл панели (ISOL-05).
	in := e.draftInput(t)
	for _, k := range []string{kernelXray, kernelMihomo} {
		files := rtGenerate(t, e.svc, k, in, rtBoth)
		if len(files) == 0 {
			t.Fatalf("генератор %s не выдал файлов", k)
		}
		for _, f := range files {
			if f.Kernel != k || !configlayer.IsPanelFileName(f.Kernel, f.RelPath) || !strings.Contains(f.RelPath, "xcp-") {
				t.Errorf("генератор %s выдал не файл панели: %s %s", k, f.Kernel, f.RelPath)
			}
			if err := configlayer.ValidatePanelName(f.Kernel, f.RelPath); err != nil {
				t.Errorf("имя %s не прошло правило панели: %v", f.RelPath, err)
			}
		}
	}

	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение: %+v", done.Result)
	}

	afterX, afterM := rtTree(t, e.roots.Xray), rtTree(t, e.roots.Mihomo)
	for kernel, pair := range map[string][2]map[string]string{kernelXray: {beforeX, afterX}, kernelMihomo: {beforeM, afterM}} {
		changed, removed, added := rtDiff(pair[0], pair[1])
		if len(changed) != 0 || len(removed) != 0 {
			t.Errorf("%s: посторонние файлы изменены %v или удалены %v", kernel, changed, removed)
		}
		for _, rel := range added {
			base := filepath.Base(rel)
			if !configlayer.IsPanelFileName(kernel, rel) || !strings.Contains(base, "xcp-") {
				t.Errorf("%s: новый файл не панели: %s", kernel, rel)
			}
		}
	}
	_, _, addedX := rtDiff(beforeX, afterX)
	_, _, addedM := rtDiff(beforeM, afterM)
	if strings.Join(addedX, ",") != "04_outbounds.xcp-my.tail.json" {
		t.Errorf("новые файлы Xray: %v", addedX)
	}
	if strings.Join(addedM, ",") != "proxy_providers/xcp-my.yaml" {
		t.Errorf("новые файлы Mihomo: %v", addedM)
	}
}

func TestRouterIsolationInactiveKernelDir(t *testing.T) {
	rtNeedKernels(t, true, true)

	// Ядро не установлено: его каталог не меняется вовсе, а на Xray узлы идут.
	e := rtNewEnv(t, true, false)
	rtForeign(t, e.roots)
	beforeM := rtTree(t, e.roots.Mihomo)
	beforeX := rtTree(t, e.roots.Xray)
	e.addLinks(t, rtLink(t, "vless_ws"), rtLink(t, "tuic"))
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение при одном ядре: %+v", done.Result)
	}
	if changed, removed, added := rtDiff(beforeM, rtTree(t, e.roots.Mihomo)); len(changed)+len(removed)+len(added) != 0 {
		t.Errorf("каталог неустановленного Mihomo изменился: %v %v %v", changed, removed, added)
	}
	if _, _, added := rtDiff(beforeX, rtTree(t, e.roots.Xray)); strings.Join(added, ",") != "04_outbounds.xcp-my.tail.json" {
		t.Errorf("новые файлы Xray при одном ядре: %v", added)
	}

	// Оба ядра установлены, работает только Xray: в дереве Mihomo различаются
	// только файлы proxy_providers/xcp-*.
	e2 := rtNewEnv(t, true, true)
	rtForeign(t, e2.roots)
	before2 := rtTree(t, e2.roots.Mihomo)
	e2.addLinks(t, rtLink(t, "vless_ws"), rtLink(t, "tuic"))
	if done := e2.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение при двух ядрах: %+v", done.Result)
	}
	changed, removed, added := rtDiff(before2, rtTree(t, e2.roots.Mihomo))
	if len(changed) != 0 || len(removed) != 0 || strings.Join(added, ",") != "proxy_providers/xcp-my.yaml" {
		t.Errorf("дерево Mihomo: изменены %v, удалены %v, добавлены %v", changed, removed, added)
	}
}

func TestRouterIsolationSwitchKernelKeepsBoth(t *testing.T) {
	rtNeedKernels(t, true, true)
	e := rtNewEnv(t, true, true)
	e.addLinks(t, rtLink(t, "vless_ws"))
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("первое применение: %+v", done.Result)
	}
	beforeX, beforeM := rtTree(t, e.roots.Xray), rtTree(t, e.roots.Mihomo)
	if len(beforeX) != 1 || len(beforeM) != 1 {
		t.Fatalf("файлы после применения: Xray %v, Mihomo %v", beforeX, beforeM)
	}

	// Смена активного ядра пересборки файлов не требует: черновик тот же, применять нечего.
	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultNothingToApply {
		t.Fatalf("повторное применение: %+v", done.Result)
	}
	afterX, afterM := rtTree(t, e.roots.Xray), rtTree(t, e.roots.Mihomo)
	for kernel, pair := range map[string][2]map[string]string{kernelXray: {beforeX, afterX}, kernelMihomo: {beforeM, afterM}} {
		if changed, removed, added := rtDiff(pair[0], pair[1]); len(changed)+len(removed)+len(added) != 0 {
			t.Errorf("%s: файлы изменились при пустом применении: %v %v %v", kernel, changed, removed, added)
		}
	}
}

func TestRouterIsolationGeneratorPathsTable(t *testing.T) {
	e := rtNewEnv(t, false, false)
	// Идентификаторы источников — «my» и 8 символов [0-9a-f]. В этом алфавите нет
	// слов стоп-списка XKeen, поэтому проверяются значения «my», образец из
	// смеси и строки из одного повторённого hex-знака.
	ids := []string{"my", "ab12cd34"}
	for _, c := range "0123456789abcdef" {
		ids = append(ids, strings.Repeat(string(c), 8))
	}
	cfg := MySourceConfig{Kind: KindMy, Enabled: true, Entries: []MyEntry{{ID: "00000001", Raw: rtLink(t, "vless_ws"), Format: "link"}}}
	list := e.svc.myNodes(cfg)
	if len(list) != 1 {
		t.Fatalf("узел для проверки путей не разобран: %d", len(list))
	}
	caps := e.svc.caps(rtBoth)
	for _, id := range ids {
		if !idRE.MatchString(id) {
			t.Errorf("идентификатор %q не подходит под правило источников", id)
			continue
		}
		if sec := sectionName(id); sec != SectionPrefix+id {
			t.Errorf("имя секции %q", sec)
		}
		xf, err := xrayFile(id, list, caps)
		if err != nil || xf == nil {
			t.Fatalf("файл Xray для %s: %v", id, err)
		}
		mf, err := mihomoFile(id, list, caps)
		if err != nil || mf == nil {
			t.Fatalf("файл Mihomo для %s: %v", id, err)
		}
		for _, f := range []*configlayer.GeneratedFile{xf, mf} {
			if err := configlayer.ValidatePanelName(f.Kernel, f.RelPath); err != nil {
				t.Errorf("путь %s %s: %v", f.Kernel, f.RelPath, err)
			}
			if !configlayer.IsPanelFileName(f.Kernel, f.RelPath) || !strings.Contains(f.RelPath, "xcp-"+id) {
				t.Errorf("путь не файл панели источника %s: %s", id, f.RelPath)
			}
		}
		if configlayer.HasForbiddenTransport(xf.Content) {
			t.Errorf("в файле Xray источника %s есть ключ \"transport\":", id)
		}
	}
	// Идентификатор вне алфавита не превращается в путь: из имени секции не берётся.
	for _, bad := range []string{"src_../etc", "src_MY", "src_my2", "src_0123456", "src_0123456g", "other"} {
		if id, ok := sourceID(bad); ok {
			t.Errorf("секция %q принята как источник %q", bad, id)
		}
	}
}
