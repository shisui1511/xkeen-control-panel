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

	// Замена и удаление правил целиком: подсказка замены, ровно одно действие на
	// список, замена не на файл из drop, пометка «из пресета» снимается, правило со
	// ссылкой, добавленное после пометки, блокирует «Применить», каталог скрывает
	// списки файла, «Вернуть источник» снимает пометку и оставляет правки правил.
	t.Run("actions", func(t *testing.T) {
		d := rtgNewEnv(t, true)
		e := d.e
		r1 := d.saveRule(t, d.cond(rtgFile, d.tagA))
		r2 := d.saveRule(t, d.cond(rtgFile, d.tagA), Cond{Type: CondPort, Values: []string{"443"}})
		r3 := d.saveRule(t, d.cond(rtgFile, d.tagB))
		r4 := d.saveRule(t, d.cond(rtgFile, d.tagB))
		// Пресетная пометка ставится прямой записью секции: через SaveRule её
		// поставить нельзя.
		cfg := e.rules(t)
		for i := range cfg.Items {
			if cfg.Items[i].ID == r4.ID {
				cfg.Items[i].Origin = OriginPreset
			}
		}
		raw, err := encodeRules(cfg)
		if err != nil {
			t.Fatal(err)
		}
		e.putSection(t, RulesSection, raw)
		if r, _ := rtgRuleByID(e.rules(t), r4.ID); r.Origin != OriginPreset {
			t.Fatalf("правило %s без пометки «из пресета»: %q", r4.ID, r.Origin)
		}

		catHas := func(file string) bool {
			t.Helper()
			cat, err := e.svc.Catalog()
			if err != nil {
				t.Fatalf("Catalog: %v", err)
			}
			for _, l := range cat.Lists {
				if l.File == file {
					return true
				}
			}
			return false
		}
		if !catHas(rtgFile) || !catHas(rtgAlt) {
			t.Fatalf("до пометки в каталоге нет списков копий dat")
		}

		imp, err := e.svc.GeodataImpact(rtgFile)
		if err != nil {
			t.Fatalf("GeodataImpact: %v", err)
		}
		if len(imp.Lists) != 2 {
			t.Fatalf("списков в влиянии %d, ожидалось 2: %+v", len(imp.Lists), imp.Lists)
		}
		for _, li := range imp.Lists {
			if li.Suggest == nil || li.Suggest.File != rtgAlt || !strings.EqualFold(li.Suggest.Tag, li.Tag) {
				t.Errorf("подсказка замены для %s: %+v, ожидался тот же тег в %s", li.Tag, li.Suggest, rtgAlt)
			}
		}

		replaceB := GeodataAction{Tag: d.tagB, Action: ActionReplace, With: &lists.ListRef{File: rtgAlt, Tag: d.tagB, Kind: d.kind}}
		dropA := GeodataAction{Tag: d.tagA, Action: ActionDropRules}
		before := e.rev(t)
		requireInvalid := func(what string, actions []GeodataAction, file string) {
			t.Helper()
			if _, err := e.svc.GeodataDrop(e.rev(t), file, DropDelete, actions); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s: err=%v, ожидался ErrInvalidInput", what, err)
			}
			if got := e.rev(t); got != before {
				t.Errorf("%s: ревизия изменилась (%d, было %d)", what, got, before)
			}
		}
		requireInvalid("нет действия для одного тега", []GeodataAction{dropA}, rtgFile)
		requireInvalid("два действия на один тег", []GeodataAction{dropA, {Tag: d.tagA, Action: ActionRemove}, replaceB}, rtgFile)
		requireInvalid("неизвестное действие", []GeodataAction{dropA, {Tag: d.tagB, Action: "merge"}}, rtgFile)
		requireInvalid("замена без списка", []GeodataAction{dropA, {Tag: d.tagB, Action: ActionReplace}}, rtgFile)
		requireInvalid("замена на тот же файл", []GeodataAction{dropA,
			{Tag: d.tagB, Action: ActionReplace, With: &lists.ListRef{File: rtgFile, Tag: d.tagB, Kind: d.kind}}}, rtgFile)
		requireInvalid("замена на несуществующий тег", []GeodataAction{dropA,
			{Tag: d.tagB, Action: ActionReplace, With: &lists.ListRef{File: rtgAlt, Tag: "XCP-NO-SUCH-TAG", Kind: d.kind}}}, rtgFile)
		requireInvalid("имя файла не dat", []GeodataAction{dropA, replaceB}, "../etc/passwd")

		// Замена на файл, уже помеченный на удаление, недопустима.
		if _, err := e.svc.GeodataDrop(e.rev(t), rtgAlt, DropOff, nil); err != nil {
			t.Fatalf("пометка файла без ссылок: %v", err)
		}
		if g := d.geodataDraft(t); len(g.Drop) != 1 || g.Drop[0] != (GeodataDrop{File: rtgAlt, Mode: DropOff}) {
			t.Fatalf("пометка файла без ссылок: %+v", g)
		}
		before = e.rev(t)
		requireInvalid("замена на файл из drop", []GeodataAction{dropA, replaceB}, rtgFile)
		if _, err := e.svc.GeodataRestore(e.rev(t), rtgAlt); err != nil {
			t.Fatalf("GeodataRestore (%s): %v", rtgAlt, err)
		}
		if st := e.state(t); len(st.Draft[GeodataSection]) != 0 {
			t.Errorf("после возврата единственной пометки секция geodata осталась: %s", st.Draft[GeodataSection])
		}

		// Допустимое сочетание: A — удалить правила, B — заменить на такой же список
		// из другого файла.
		before = e.rev(t)
		if _, err := e.svc.GeodataDrop(before, rtgFile, DropDelete, []GeodataAction{dropA, replaceB}); err != nil {
			t.Fatalf("GeodataDrop: %v", err)
		}
		if got := e.rev(t); got != before+1 {
			t.Errorf("ревизия %d, ожидалась %d", got, before+1)
		}
		cfg = e.rules(t)
		for _, id := range []string{r1.ID, r2.ID} {
			if _, ok := rtgRuleByID(cfg, id); ok {
				t.Errorf("правило %s осталось после «удалить правила»", id)
			}
		}
		for _, id := range []string{r3.ID, r4.ID} {
			r, ok := rtgRuleByID(cfg, id)
			if !ok {
				t.Fatalf("правило %s пропало при замене", id)
			}
			if len(r.Conds) != 1 || len(r.Conds[0].Lists) != 1 || r.Conds[0].Lists[0].File != rtgAlt ||
				!strings.EqualFold(r.Conds[0].Lists[0].Tag, d.tagB) {
				t.Errorf("у правила %s ссылка не заменена: %+v", id, r.Conds)
			}
		}
		if r, _ := rtgRuleByID(cfg, r4.ID); r.Origin != "" {
			t.Errorf("правило %s после замены сохранило origin %q", r4.ID, r.Origin)
		}
		if g := d.geodataDraft(t); len(g.Drop) != 1 || g.Drop[0] != (GeodataDrop{File: rtgFile, Mode: DropDelete}) {
			t.Errorf("секция geodata: %+v", g)
		}
		if catHas(rtgFile) {
			t.Errorf("в каталоге остались списки файла %s с пометкой drop", rtgFile)
		}
		if !catHas(rtgAlt) {
			t.Errorf("в каталоге пропали списки файла %s без пометки", rtgAlt)
		}

		// Правило со ссылкой на помеченный файл, добавленное после пометки: ошибка
		// проверки, «Применить» заблокировано.
		d.saveRule(t, d.cond(rtgFile, d.tagA))
		e.layer.InvalidateChecks()
		if !rtHasIssue(e.check(t), "geodata_ref_remains", "file", rtgFile) {
			t.Errorf("проверка без geodata_ref_remains: %+v", e.check(t))
		}
		_, err = e.applyResult(t)
		var ce *configlayer.ChecksError
		if !errors.Is(err, configlayer.ErrChecksFailed) || !errors.As(err, &ce) {
			t.Fatalf("«Применить» со ссылкой на удаляемый файл: err=%v, ожидался отказ проверки", err)
		}
		if !rtHasIssue(ce.Issues, "geodata_ref_remains", "tag", d.tagA) || !rtHasIssue(ce.Issues, "geodata_ref_remains", "file", rtgFile) {
			t.Errorf("в отказе нет geodata_ref_remains с файлом и тегом: %+v", ce.Issues)
		}
		for _, is := range ce.Issues {
			if is.Code == "geodata_ref_remains" && is.Params["rule"] == "" {
				t.Errorf("geodata_ref_remains без номера правила: %+v", is)
			}
		}

		// «Вернуть источник»: пометка снята, правки правил остаются до «Сбросить».
		rulesBefore := e.rules(t)
		if _, err := e.svc.GeodataRestore(e.rev(t), "xcp-never-marked.dat"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("GeodataRestore файла без пометки: err=%v, ожидался ErrInvalidInput", err)
		}
		if _, err := e.svc.GeodataRestore(e.rev(t), rtgFile); err != nil {
			t.Fatalf("GeodataRestore: %v", err)
		}
		if st := e.state(t); len(st.Draft[GeodataSection]) != 0 {
			t.Errorf("после возврата секция geodata осталась: %s", st.Draft[GeodataSection])
		}
		if !catHas(rtgFile) {
			t.Errorf("после возврата в каталоге нет списков файла %s", rtgFile)
		}
		if got := e.rules(t); len(got.Items) != len(rulesBefore.Items) {
			t.Errorf("возврат источника изменил правила: было %d, стало %d", len(rulesBefore.Items), len(got.Items))
		}
		e.layer.InvalidateChecks()
		if rtHasIssue(e.check(t), "geodata_ref_remains", "file", rtgFile) {
			t.Errorf("после возврата проверка всё ещё видит geodata_ref_remains")
		}
	})

	// Своё правило ядра со ссылкой ext:<файл>: каскад не правит: влияние
	// перечисляет его, удаление отклоняется кодом raw_ref без изменений черновика.
	t.Run("raw_ref", func(t *testing.T) {
		d := rtgNewEnv(t, false)
		e := d.e
		d.saveRule(t, d.cond(rtgFile, d.tagA))
		_, raw, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: Target{Kind: KindDirect},
			Raw: &RawRule{Kernel: configlayer.KernelXray, Text: `{"type":"field","domain":["ext:` + rtgFile + `:` + d.tagA + `"]}`}})
		if err != nil {
			t.Fatalf("SaveRule (своё правило Xray): %v", err)
		}
		imp, err := e.svc.GeodataImpact(rtgFile)
		if err != nil {
			t.Fatalf("GeodataImpact: %v", err)
		}
		if len(imp.Raw) != 1 || imp.Raw[0] != (RawRef{Rule: raw.ID, N: 2}) {
			t.Fatalf("влияние на свои правила ядра: %+v, ожидалось правило %s (2)", imp.Raw, raw.ID)
		}
		before := e.rev(t)
		_, err = e.svc.GeodataDrop(before, rtgFile, DropDelete, []GeodataAction{{Tag: d.tagA, Action: ActionRemove}})
		var coded *lists.CodedError
		if !errors.As(err, &coded) || coded.Code != "raw_ref" || !errors.Is(err, ErrGeodataRawRef) {
			t.Fatalf("GeodataDrop со своим правилом ядра: err=%v, ожидался raw_ref", err)
		}
		if got := e.rev(t); got != before {
			t.Errorf("отказ raw_ref изменил черновик: ревизия %d, было %d", got, before)
		}
		if len(e.state(t).Draft[GeodataSection]) != 0 {
			t.Errorf("отказ raw_ref оставил пометку в секции geodata")
		}
	})
}
