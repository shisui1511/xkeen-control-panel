//go:build router

package routing

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// rtwWatchWait — сколько ждать реакции наблюдателя: период опроса 1 с плюс
// запуск конвейера (mihomo -t, запись, коммит).
const rtwWatchWait = 20 * time.Second

// rtwPair — пара настоящих dat-файлов стенда с общим тегом и разным составом.
type rtwPair struct {
	from, to string // файлы со списком: сначала from, затем to
	other    string // файл без тега (пусто — такого нет)
	tag      string
}

// rtwFindPair ищет на стенде тег, который есть в двух разных файлах geosite с
// разным составом (например YOUTUBE в zkeen и v2fly), и файл без этого тега.
func rtwFindPair(ix *lists.Index) (rtwPair, bool) {
	files, err := ix.Files()
	if err != nil {
		return rtwPair{}, false
	}
	var site []string
	for _, f := range files {
		if f.Kind == lists.KindSite && listFileRE.MatchString(f.Name) {
			site = append(site, f.Name)
		}
	}
	extracts := 0
	for _, from := range site {
		tags, err := ix.Tags(from)
		if err != nil {
			continue
		}
		for _, ti := range tags {
			if ti.Count < 3 || ti.Count > 3000 || !listTagRE.MatchString(ti.Tag) {
				continue
			}
			for _, to := range site {
				if to == from {
					continue
				}
				if _, _, ok, err := ix.Has(to, ti.Tag); err != nil || !ok {
					continue
				}
				if extracts++; extracts > 200 {
					return rtwPair{}, false
				}
				a, errA := ix.Extract(from, ti.Tag)
				b, errB := ix.Extract(to, ti.Tag)
				if errA != nil || errB != nil || a.Hash == b.Hash || a.Entries == 0 || b.Entries == 0 || b.Entries > 3000 {
					continue
				}
				p := rtwPair{from: from, to: to, tag: ti.Tag}
				for _, f := range files {
					if f.Name == from || f.Name == to {
						continue
					}
					if _, _, ok, err := ix.Has(f.Name, ti.Tag); err == nil && !ok {
						p.other = f.Name
						break
					}
				}
				return p, true
			}
		}
	}
	return rtwPair{}, false
}

// rtwReplaceDat подменяет dat-файл name в каталоге dir содержимым настоящего
// файла стенда srcName; время изменения каждый раз новое (сигнатура — размер и
// mtime в секундах).
func rtwReplaceDat(t *testing.T, ix *lists.Index, dir, name, srcName string, n int) {
	t.Helper()
	rtwCopyDat(t, filepath.Join(ix.Dir(), srcName), dir, name)
	mt := time.Now().Add(time.Duration(10*n) * time.Second)
	if err := os.Chtimes(filepath.Join(dir, name), mt, mt); err != nil {
		t.Fatal(err)
	}
}

// rtwProviders читает файлы провайдеров списков панели: имя файла → содержимое.
func (e *rtEnv) rtwProviders(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(e.mihomoPath(mihomoRuleProviderDir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}
		}
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, en := range entries {
		data, err := os.ReadFile(e.mihomoPath(mihomoRuleProviderDir + "/" + en.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[en.Name()] = string(data)
	}
	return out
}

// rtwSameFiles сообщает, что два набора файлов провайдеров совпадают.
func rtwSameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// Наблюдатель dat: XKeen обновил базы (файл подменён, тег тот же) — провайдеры
// Mihomo пересобираются без участия пользователя и без правок черновика; тег
// пропал — уведомление routing_list_missing, файлы провайдеров не меняются;
// список вернулся — уведомление снято.
func TestRouterDatWatchRebuild(t *testing.T) {
	datIx := lists.NewIndex("")
	pair, ok := rtwFindPair(datIx)
	if !ok {
		t.Skip("на стенде нет тега, который есть в двух файлах geosite с разным составом")
	}
	rtNeedKernels(t)
	datDir := rtWorkSubdir(t, "rt-datwatch-")
	rtwReplaceDat(t, datIx, datDir, pair.from, pair.from, 0)

	e := rtNewEnv(t, rtWithDeps(func(d *Deps) {
		d.Lists = lists.NewIndex(datDir)
		d.WatchInterval = time.Second
	}))
	e.writeUserProfile(t)
	e.addMyLinks(t, "vless_ws")
	ref := lists.ListRef{File: pair.from, Tag: pair.tag, Kind: lists.KindSite}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{ref}}}}); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	e.takeAndApply(t)
	first := e.rtwProviders(t)
	if len(first) == 0 {
		t.Fatal("после «Применить» нет файлов провайдеров списка")
	}
	rev0 := e.rev(t)

	// Наблюдатель запоминает сигнатуры применённых файлов.
	rtWaitCond(t, rtwWatchWait, "сигнатуры dat в Live[geodata]", func() bool {
		_, ok := readGeodata(e.state(t).Live)[pair.from]
		return ok
	})
	sig0 := readGeodata(e.state(t).Live)[pair.from]

	// 1. XKeen обновил файл: тот же тег, другой состав.
	rtwReplaceDat(t, datIx, datDir, pair.from, pair.to, 1)
	rtWaitCond(t, rtwWatchWait, "пересборка провайдеров после обновления dat", func() bool {
		return !rtwSameFiles(e.rtwProviders(t), first)
	})
	rtWaitCond(t, rtwWatchWait, "запись сигнатур после пересборки", func() bool {
		return readGeodata(e.state(t).Live)[pair.from] != sig0
	})
	second := e.rtwProviders(t)
	cat, err := datIx.Extract(pair.to, pair.tag)
	if err != nil {
		t.Fatalf("Extract %s:%s: %v", pair.to, pair.tag, err)
	}
	gens := e.svc.Generators()
	generated, err := gens[1].Generate(inputOf(e.state(t)), e.installed)
	if err != nil {
		t.Fatalf("генератор %s: %v", gens[1].ID(), err)
	}
	total := 0
	for _, f := range generated {
		if f.Kind != configlayer.KindMihomoRuleProviderText {
			continue
		}
		if f.Expect == nil {
			t.Errorf("провайдер %s без Expect", f.RelPath)
			continue
		}
		n := rtmCountLines(t, e.mihomoPath(f.RelPath))
		if n != f.Expect.Count {
			t.Errorf("в %s строк %d, Expect.Count %d", f.RelPath, n, f.Expect.Count)
		}
		total += f.Expect.Count
	}
	if total != cat.Entries {
		t.Errorf("строк в провайдерах %d, в новом составе списка %d", total, cat.Entries)
	}
	if apply := e.layer.Snapshot().Apply; apply.Trigger != configlayer.TriggerDatRefresh || apply.Result == nil || !apply.Result.OK {
		t.Errorf("последний запуск: trigger=%q result=%+v, ожидался успешный dat_refresh", apply.Trigger, apply.Result)
	}
	if got := e.rev(t); got != rev0 {
		t.Errorf("ревизия черновика %d, была %d: пересборка не должна её менять", got, rev0)
	}

	if pair.other == "" {
		t.Log("на стенде нет файла без этого тега: пропавший список не проверен")
		return
	}

	// 2. Тег пропал после обновления: уведомление, файлы не трогаются.
	noticeID := noticeListMissingPrefix + pair.from + ":" + pair.tag
	rtwReplaceDat(t, datIx, datDir, pair.from, pair.other, 2)
	rtWaitCond(t, rtwWatchWait, "уведомление о пропавшем списке", func() bool {
		_, ok := e.rtsNotice(noticeID)
		return ok
	})
	n, _ := e.rtsNotice(noticeID)
	if n.Kind != noticeListMissingKind || n.Params["file"] != pair.from || n.Params["tag"] != pair.tag {
		t.Errorf("уведомление %+v: ожидались вид %s и параметры file/tag", n, noticeListMissingKind)
	}
	time.Sleep(3 * time.Second)
	if got := e.rtwProviders(t); !rtwSameFiles(got, second) {
		t.Error("файлы провайдеров изменились, хотя тег пропал")
	}
	if got := e.rev(t); got != rev0 {
		t.Errorf("ревизия черновика %d, была %d", got, rev0)
	}

	// 3. Список вернулся: уведомление снято, файлы те же.
	rtwReplaceDat(t, datIx, datDir, pair.from, pair.to, 3)
	rtWaitCond(t, rtwWatchWait, "снятие уведомления о пропавшем списке", func() bool {
		_, ok := e.rtsNotice(noticeID)
		return !ok
	})
	if got := e.rtwProviders(t); !rtwSameFiles(got, second) {
		t.Error("файлы провайдеров изменились после возврата того же состава")
	}
}
