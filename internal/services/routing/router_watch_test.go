//go:build router

package routing

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Списки dat в проверке и в фоне (D-21, D-22): источник истины — настоящие dat
// XKeen на устройстве (копии во временном каталоге теста; каталог стенда не
// меняется), вердикт «Применить» и файлы слоя. Ядро тесты не читают и не
// переключают — метка ядра не нужна.

// rtwCopyDat копирует dat-файл (симлинк разворачивается) в каталог dir под именем
// name.
func rtwCopyDat(t *testing.T, src, dir, name string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("dat стенда: %v", err)
	}
	defer in.Close()
	tmp := filepath.Join(dir, "."+name+".tmp")
	out, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("копия dat: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

// Правило на список, которого нет на роутере, получает ошибку list_not_found с
// файлом и тегом: «Применить» блокируется проверкой до записи (раньше, чем
// xray run -test упал бы с «failed to check code»), то же видно в представлении;
// пропавший файл dat ломает и правило на прежде существовавший тег.
func TestRouterListNotFoundBlocksApply(t *testing.T) {
	datIx := lists.NewIndex("")
	site, _ := rtmDatRefs(datIx)
	if site == nil {
		t.Skip("в каталоге dat стенда нет подходящего списка geosite")
	}
	files, err := datIx.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	var src string
	for _, f := range files {
		if f.Name == site.File {
			src = filepath.Join(datIx.Dir(), f.Name)
		}
	}
	if src == "" {
		t.Fatalf("файл %s не найден в каталоге dat", site.File)
	}
	datDir := rtWorkSubdir(t, "rt-dat-")
	rtwCopyDat(t, src, datDir, site.File)

	e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) { d.Lists = lists.NewIndex(datDir) }))
	e.addMyLinks(t, "vless_ws")
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	direct := Target{Kind: KindDirect}
	_, good, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: direct,
		Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{*site}}}})
	if err != nil {
		t.Fatalf("SaveRule (существующий тег): %v", err)
	}
	// Правило на существующий тег: «Применить» проходит.
	e.applyOK(t)

	const fake = "RT-NO-SUCH-TAG"
	_, rule, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: direct,
		Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{{File: site.File, Tag: fake, Kind: site.Kind}}}}})
	if err != nil {
		t.Fatalf("SaveRule (выдуманный тег): %v", err)
	}

	_, err = e.applyResult(t)
	var ce *configlayer.ChecksError
	if !errors.Is(err, configlayer.ErrChecksFailed) || !errors.As(err, &ce) {
		t.Fatalf("«Применить» с выдуманным тегом: err=%v, ожидался отказ проверки", err)
	}
	if !rtHasIssue(ce.Issues, "list_not_found", "tag", fake) || !rtHasIssue(ce.Issues, "list_not_found", "file", site.File) {
		t.Errorf("в отказе нет list_not_found с файлом и тегом: %+v", ce.Issues)
	}
	for _, is := range ce.Issues {
		if is.Code == "list_not_found" && is.Params["tag"] == fake && is.Params["rule"] == "" {
			t.Errorf("list_not_found без номера правила: %+v", is)
		}
	}
	rv := rtcRuleView(t, e.view(t), rule.ID)
	if !rtViewHas(rv.Issues, "list_not_found", "file", site.File, "tag", fake) {
		t.Errorf("представление правила без list_not_found: %+v", rv.Issues)
	}
	if gv := rtcRuleView(t, e.view(t), good.ID); rtViewHas(gv.Issues, "list_not_found") {
		t.Errorf("правило на существующий тег получило list_not_found: %+v", gv.Issues)
	}

	// Убрали правило с выдуманным тегом: «Применить» снова проходит.
	if _, err := e.svc.DeleteRule(e.rev(t), rule.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	e.applyOK(t)

	// Файл dat пропал: правило на прежний тег тоже получает list_not_found.
	if err := os.Remove(filepath.Join(datDir, site.File)); err != nil {
		t.Fatal(err)
	}
	e.layer.InvalidateChecks()
	if !rtHasIssue(e.check(t), "list_not_found", "tag", site.Tag) {
		t.Errorf("после удаления dat проверка без list_not_found: %+v", e.check(t))
	}
	if _, err := e.applyResult(t); !errors.Is(err, configlayer.ErrChecksFailed) {
		t.Errorf("«Применить» без файла dat: err=%v, ожидался отказ проверки", err)
	}
	rv = rtcRuleView(t, e.view(t), good.ID)
	if !rtViewHas(rv.Issues, "list_not_found", "file", site.File, "tag", site.Tag) {
		t.Errorf("представление без list_not_found после удаления dat: %+v", rv.Issues)
	}
}
