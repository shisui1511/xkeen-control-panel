//go:build router

package routing

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Каскад по файлу dat (D-18, D-20): настоящий слой, настоящие dat стенда (копии
// самого маленького файла во временном каталоге теста; каталог стенда не
// меняется), настоящие `xray run -test` и `mihomo -t` в «Применить». Ядро стенда
// тест не читает и не переключает — метка ядра не нужна.

const (
	// rtgFile и rtgAlt — имена копий настоящего dat в каталоге списков теста.
	rtgFile = "xcp-cascade.dat"
	rtgAlt  = "xcp-cascade-alt.dat"
)

// rtgDat — окружение сценария: слой на каталоге списков с копией dat стенда и два
// тега, на которые ссылаются правила.
type rtgDat struct {
	e          *rtEnv
	kind       string
	tagA, tagB string
}

// rtgPick выбирает на стенде самый маленький dat (geosite или geoip) с двумя
// пригодными для правил тегами. Нет такого — тест пропускается.
func rtgPick(t *testing.T) (src, tagA, tagB string) {
	t.Helper()
	ix := lists.NewIndex("")
	files, err := ix.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Size < files[j].Size })
	for _, f := range files {
		if (f.Kind != lists.KindSite && f.Kind != lists.KindIP) || !listFileRE.MatchString(f.Name) {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil {
			continue
		}
		var picked []string
		for _, tg := range tags {
			if listTagRE.MatchString(tg.Tag) && tg.Count >= 3 && tg.Count <= 3000 {
				picked = append(picked, tg.Tag)
			}
			if len(picked) == 2 {
				return filepath.Join(ix.Dir(), f.Name), picked[0], picked[1]
			}
		}
	}
	t.Skip("в каталоге dat стенда нет файла с двумя подходящими списками")
	return "", "", ""
}

// rtgNewEnv копирует dat стенда во временный каталог списков (alt — ещё и вторая
// копия под другим именем) и поднимает слой с обоими ядрами. Маршрутизация взята,
// есть узел и прежний профиль Mihomo.
func rtgNewEnv(t *testing.T, alt bool) *rtgDat {
	t.Helper()
	src, tagA, tagB := rtgPick(t)
	dir := rtWorkSubdir(t, "rt-dat-")
	rtwCopyDat(t, src, dir, rtgFile)
	if alt {
		rtwCopyDat(t, src, dir, rtgAlt)
	}
	ix := lists.NewIndex(dir)
	infos, err := ix.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	kind := ""
	for _, f := range infos {
		if f.Name == rtgFile {
			kind = f.Kind
		}
	}
	if kind != lists.KindSite && kind != lists.KindIP {
		t.Fatalf("вид копии %s: %q", rtgFile, kind)
	}
	e := rtNewEnv(t, rtWithDeps(func(d *Deps) { d.Lists = ix }))
	e.writeUserProfile(t)
	e.addMyLinks(t, "vless_ws")
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	return &rtgDat{e: e, kind: kind, tagA: tagA, tagB: tagB}
}

// ref — ссылка на список файла.
func (d *rtgDat) ref(file, tag string) lists.ListRef {
	return lists.ListRef{File: file, Tag: tag, Kind: d.kind}
}

// cond — условие «список» нужного вида.
func (d *rtgDat) cond(file, tag string) Cond {
	typ := CondGeosite
	if d.kind == lists.KindIP {
		typ = CondGeoip
	}
	return Cond{Type: typ, Lists: []lists.ListRef{d.ref(file, tag)}}
}

// saveRule сохраняет правило на DIRECT с условиями conds.
func (d *rtgDat) saveRule(t *testing.T, conds ...Cond) Rule {
	t.Helper()
	_, r, err := d.e.svc.SaveRule(d.e.rev(t), Rule{Enabled: true, Target: Target{Kind: KindDirect}, Conds: conds})
	if err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	return r
}

// ruleByID ищет правило черновика по ID.
func rtgRuleByID(cfg RulesConfig, id string) (Rule, bool) {
	for _, r := range cfg.Items {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// geodataDraft читает секцию geodata черновика.
func (d *rtgDat) geodataDraft(t *testing.T) GeodataDraft {
	t.Helper()
	g, err := ParseGeodataDraft(d.e.state(t).Draft[GeodataSection])
	if err != nil {
		t.Fatalf("секция geodata: %v", err)
	}
	return g
}

// generated собирает файлы всех генераторов маршрутизации по текущему черновику.
func (d *rtgDat) generated(t *testing.T) []configlayer.GeneratedFile {
	t.Helper()
	var out []configlayer.GeneratedFile
	for _, g := range d.e.svc.Generators() {
		files, err := g.Generate(inputOf(d.e.state(t)), d.e.installed)
		if err != nil {
			t.Fatalf("генератор %s: %v", g.ID(), err)
		}
		out = append(out, files...)
	}
	return out
}

// providerNames — имена провайдеров Mihomo для списка (все три части).
func rtgProviderNames(file, tag string) []string {
	return []string{
		ProviderName(file, tag, listPartDomain),
		ProviderName(file, tag, listPartClassical),
		ProviderName(file, tag, listPartIP),
	}
}

// rtgMentions сообщает, что среди сгенерированных файлов есть ссылка на файл
// dat: ext:<файл>: в Xray или провайдер правил Mihomo списка (file, tag).
func rtgMentions(files []configlayer.GeneratedFile, file, tag string) (xray, mihomo bool) {
	for _, f := range files {
		if strings.Contains(string(f.Content), "ext:"+file+":") {
			xray = true
		}
		for _, n := range rtgProviderNames(file, tag) {
			if strings.Contains(f.RelPath, n) {
				mihomo = true
			}
		}
	}
	return xray, mihomo
}

// requireNoProviders требует, чтобы на диске Mihomo не было провайдеров списка.
func (d *rtgDat) requireNoProviders(t *testing.T, file, tag string) {
	t.Helper()
	entries, err := os.ReadDir(d.e.mihomoPath(mihomoRuleProviderDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatalf("каталог провайдеров: %v", err)
	}
	for _, en := range entries {
		for _, n := range rtgProviderNames(file, tag) {
			if strings.Contains(en.Name(), n) {
				t.Errorf("после «Применить» остался провайдер Mihomo %s списка %s", en.Name(), tag)
			}
		}
	}
}

func TestRouterGeodataCascade(t *testing.T) {
	// «Убрать список из условий»: правило с одним условием-списком умирает (Impact
	// называет его заранее), у правила с соседним условием остаётся порт; пометка и
	// правила — одной ревизией; после «Применить» оба ядра без ссылок на файл.
	t.Run("remove", func(t *testing.T) {
		d := rtgNewEnv(t, false)
		e := d.e
		r1 := d.saveRule(t, d.cond(rtgFile, d.tagA))
		r2 := d.saveRule(t, d.cond(rtgFile, d.tagA), Cond{Type: CondPort, Values: []string{"443"}})

		// До каскада обе ссылки видны в том, что получили бы ядра: проверка
		// отсутствия после «Применить» ничего не доказывает без этого.
		if x, m := rtgMentions(d.generated(t), rtgFile, d.tagA); !x || !m {
			t.Fatalf("до каскада нет ссылок на файл: xray=%v mihomo=%v", x, m)
		}

		imp, err := e.svc.GeodataImpact(rtgFile)
		if err != nil {
			t.Fatalf("GeodataImpact: %v", err)
		}
		if len(imp.Lists) != 1 || !strings.EqualFold(imp.Lists[0].Tag, d.tagA) || imp.Lists[0].Kind != d.kind {
			t.Fatalf("влияние по спискам: %+v", imp.Lists)
		}
		li := imp.Lists[0]
		if len(li.Rules) != 2 || li.Rules[0] != (GeodataRuleRef{ID: r1.ID, N: 1}) || li.Rules[1] != (GeodataRuleRef{ID: r2.ID, N: 2}) {
			t.Errorf("правила списка: %+v, ожидались %s (1) и %s (2)", li.Rules, r1.ID, r2.ID)
		}
		if len(li.Dies) != 1 || li.Dies[0] != r1.ID {
			t.Errorf("умрёт: %v, ожидалось только %s", li.Dies, r1.ID)
		}
		if len(imp.Raw) != 0 {
			t.Errorf("своих правил ядра нет, а Raw: %+v", imp.Raw)
		}

		before := e.rev(t)
		if _, err := e.svc.GeodataDrop(before, rtgFile, DropDelete, []GeodataAction{{Tag: d.tagA, Action: ActionRemove}}); err != nil {
			t.Fatalf("GeodataDrop: %v", err)
		}
		if got := e.rev(t); got != before+1 {
			t.Errorf("ревизия %d, ожидалась %d (одна правка черновика)", got, before+1)
		}
		cfg := e.rules(t)
		if _, ok := rtgRuleByID(cfg, r1.ID); ok {
			t.Errorf("правило %s без условий осталось в черновике", r1.ID)
		}
		got2, ok := rtgRuleByID(cfg, r2.ID)
		if !ok {
			t.Fatalf("правило %s пропало: у него есть порт", r2.ID)
		}
		if len(got2.Conds) != 1 || got2.Conds[0].Type != CondPort || len(got2.Conds[0].Lists) != 0 {
			t.Errorf("у правила %s ожидалось одно условие «порт», а есть %+v", r2.ID, got2.Conds)
		}
		g := d.geodataDraft(t)
		if len(g.Drop) != 1 || g.Drop[0] != (GeodataDrop{File: rtgFile, Mode: DropDelete}) {
			t.Errorf("секция geodata: %+v", g)
		}

		// «Применить»: ни одно ядро не ссылается на файл.
		e.applyOK(t)
		rtcFilesLack(t, e.xrayDir(), "ext:"+rtgFile+":")
		d.requireNoProviders(t, rtgFile, d.tagA)
	})
}
