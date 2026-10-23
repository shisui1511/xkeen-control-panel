//go:build router

package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Каскад удаления: настоящий слой, настоящий `xray run -test` устройства в
// «Применить», файлы Xray читаются с диска. Ядро стенда не трогается, ядро тест
// не читает и не переключает — метка ядра не нужна.

// rtcFilesLack требует, чтобы ни в одном файле каталога не было подстроки.
func rtcFilesLack(t *testing.T, dir, needle string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(needle)) {
			t.Errorf("в файле %s осталась ссылка %q", filepath.Base(path), needle)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход каталога %s: %v", dir, err)
	}
}

// rtcFilesHave требует, чтобы подстрока нашлась хотя бы в одном файле каталога.
func rtcFilesHave(t *testing.T, dir, needle string) {
	t.Helper()
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte(needle)) {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("до удаления %q нет ни в одном файле: проверка отсутствия ничего не доказывает", needle)
	}
}

// rtcSetSelection записывает выбор участников в живые данные слоя.
func (e *rtEnv) rtcSetSelection(t *testing.T, sel map[string]string) {
	t.Helper()
	raw, err := json.Marshal(sel)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.layer.UpdateLive(func(live configlayer.Sections) error {
		live[LiveKeySelection] = raw
		return nil
	}); err != nil {
		t.Fatalf("UpdateLive: %v", err)
	}
}

// rtcSelection читает выбор участников из живых данных слоя.
func (e *rtEnv) rtcSelection(t *testing.T) map[string]string {
	t.Helper()
	return liveSelection(e.state(t).Live)
}

// Удаление группы показывает зависимости и последствия по ядрам; без выбранного
// действия черновик не меняется; перенаправление одной ревизией меняет цели
// правил и «Остального», убирает группу из состава ручных групп и чистит выбор
// в Live; после «Применить» тега удалённой группы в файлах Xray нет.
func TestRouterCascadeGroup(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")

	cfg := e.groups(t)
	auto, okA := rtGroupByName(cfg, "Авто")
	proxyG, okP := rtGroupByName(cfg, "Прокси")
	if !okA || !okP {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	_, service, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Сервис", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: proxyG.ID}, {Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	var autoRules []Rule
	for _, domain := range []string{"first.example.test", "second.example.test"} {
		_, saved, err := e.svc.SaveRule(e.rev(t), Rule{
			Enabled: true, Target: Target{Kind: KindGroup, ID: auto.ID},
			Conds: []Cond{{Type: CondDomain, Values: []string{domain}}},
		})
		if err != nil {
			t.Fatalf("SaveRule (%s → Авто): %v", domain, err)
		}
		autoRules = append(autoRules, saved)
	}
	_, directRule, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"direct.example.test"}}},
	})
	if err != nil {
		t.Fatalf("SaveRule (домен → DIRECT): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: auto.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)
	rtcFilesHave(t, e.xrayDir(), GroupTag(auto.ID))
	nodesXray := len(e.catalog(t).Xray)

	// Зависимости: оба правила (номера 1 и 2), «Остальное», ручные группы-участники
	// и узлы, которые потеряет balancer «Авто».
	imp, err := e.svc.Impact(ImpactGroup, auto.ID)
	if err != nil {
		t.Fatalf("Impact(group): %v", err)
	}
	if len(imp.Rules) != 2 || imp.Rules[0].ID != autoRules[0].ID || imp.Rules[0].Index != 1 ||
		imp.Rules[1].ID != autoRules[1].ID || imp.Rules[1].Index != 2 {
		t.Errorf("правила в Impact: %+v", imp.Rules)
	}
	if imp.Rules[0].Summary != "domain: first.example.test" {
		t.Errorf("краткое условие: %q", imp.Rules[0].Summary)
	}
	if !imp.Final {
		t.Error("Impact не показал, что на группу ссылается «Остальное»")
	}
	members := map[string]string{}
	for _, g := range imp.Groups {
		members[g.Name] = g.Role
	}
	if members["Сервис"] != RoleMember || members["Прокси"] != RoleMember || len(imp.Groups) != 2 {
		t.Errorf("группы-участники в Impact: %+v", imp.Groups)
	}
	if len(imp.Kernels.Xray) != 1 || imp.Kernels.Xray[0] != (BalancerLoss{Group: "Авто", Lost: nodesXray}) {
		t.Errorf("потери balancer Xray: %+v, ожидалась «Авто» с %d узлами", imp.Kernels.Xray, nodesXray)
	}
	if len(imp.Kernels.Mihomo) != 0 {
		t.Errorf("Mihomo не установлен, а в Impact есть потери: %+v", imp.Kernels.Mihomo)
	}
	if imp.Rules == nil || imp.Empties == nil || imp.Kernels.Mihomo == nil {
		t.Error("в Impact срезы должны быть [], а не null")
	}
	if ruleImp, err := e.svc.Impact(ImpactRule, directRule.ID); err != nil || len(ruleImp.Rules)+len(ruleImp.Groups) != 0 || ruleImp.Final {
		t.Errorf("Impact(rule): %+v, %v", ruleImp, err)
	}
	if _, err := e.svc.Impact(ImpactGroup, "0badc0de"); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Impact неизвестной группы: %v", err)
	}
	if _, err := e.svc.Impact(ImpactRule, "0badc0de"); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("Impact неизвестного правила: %v", err)
	}

	// Без действия — отказ с Impact, черновик не меняется.
	rev := e.rev(t)
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{})
	var re *ReferencedError
	if !errors.Is(err, ErrReferenced) || !errors.As(err, &re) {
		t.Fatalf("удаление без действия: %v", err)
	}
	if len(re.Impact.Rules) != 2 || !re.Impact.Final || len(re.Impact.Groups) != 2 || len(re.Impact.Kernels.Xray) != 1 {
		t.Errorf("Impact в отказе: %+v", re.Impact)
	}
	if e.rev(t) != rev {
		t.Error("отказ без действия изменил черновик")
	}

	// Отказы по входу: ничего не меняется.
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: CascadeDrop})
	rtWantInvalid(t, "drop при «Остальном» на группе", err, "mode", "final_requires_redirect")
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: CascadeRedirect})
	rtWantInvalid(t, "redirect без цели", err, "to", "target_invalid")
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: CascadeRedirect, To: &Target{Kind: KindGroup, ID: auto.ID}})
	rtWantInvalid(t, "redirect на удаляемую группу", err, "to", "target_invalid")
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: CascadeRedirect, To: &Target{Kind: KindGroup, ID: "0badc0de"}})
	rtWantInvalid(t, "redirect на несуществующую группу", err, "to", "target_invalid")
	_, err = e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: "wipe"})
	rtWantInvalid(t, "неизвестное действие", err, "mode", "mode_invalid")
	if _, err := e.svc.DeleteGroup(rev, "0badc0de", Cascade{Mode: CascadeDrop}); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("удаление неизвестной группы: %v", err)
	}
	if _, err := e.svc.DeleteGroup(rev-1, auto.ID, Cascade{Mode: CascadeRedirect, To: &Target{Kind: KindDirect}}); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия: %v", err)
	}
	if e.rev(t) != rev {
		t.Error("отказы изменили черновик")
	}

	// Выбор участников в Live: удаление в черновике его не трогает (группа в ядре
	// остаётся до «Применить», сброс черновика вернул бы её с выбором); ключ
	// удалённой группы и выбор её как участника убирает «Применить».
	e.rtcSetSelection(t, map[string]string{service.ID: memberGroup + auto.ID, auto.ID: memberDirect, proxyG.ID: memberDirect})

	// Перенаправление на «Прокси» — одна ревизия.
	ev, err := e.svc.DeleteGroup(rev, auto.ID, Cascade{Mode: CascadeRedirect, To: &Target{Kind: KindGroup, ID: proxyG.ID}})
	if err != nil {
		t.Fatalf("DeleteGroup(redirect): %v", err)
	}
	if ev.DraftRevision != rev+1 {
		t.Errorf("каскад занял %d ревизий, ожидалась одна", ev.DraftRevision-rev)
	}
	if _, ok := rtGroupByName(e.groups(t), "Авто"); ok {
		t.Error("группа «Авто» осталась в секции groups")
	}
	for _, g := range e.groups(t).Items {
		for _, m := range g.Members {
			if m.Kind == KindGroup && m.ID == auto.ID {
				t.Errorf("группа %q сохранила участника-удалённую группу", g.Name)
			}
		}
	}
	svcAfter, _ := rtGroupByName(e.groups(t), "Сервис")
	if len(svcAfter.Members) != 2 || svcAfter.Members[0].ID != proxyG.ID || svcAfter.Members[1].Kind != KindDirect {
		t.Errorf("состав «Сервис» после каскада: %+v", svcAfter.Members)
	}
	rules := e.rules(t)
	toProxy := Target{Kind: KindGroup, ID: proxyG.ID}
	if len(rules.Items) != 3 || rules.Items[0].Target != toProxy || rules.Items[1].Target != toProxy ||
		rules.Items[2].Target != (Target{Kind: KindDirect}) || rules.Final != toProxy {
		t.Errorf("правила после каскада: %+v, «Остальное» %+v", rules.Items, rules.Final)
	}
	sel := e.rtcSelection(t)
	if len(sel) != 3 || sel[auto.ID] != memberDirect || sel[service.ID] != memberGroup+auto.ID || sel[proxyG.ID] != memberDirect {
		t.Errorf("выбор в Live изменился до «Применить»: %v", sel)
	}

	// «Применить» проходит проверкой ядра, тега удалённой группы нет в файлах.
	e.applyOK(t)
	sel = e.rtcSelection(t)
	if _, ok := sel[auto.ID]; ok {
		t.Errorf("после «Применить» остался выбор удалённой группы: %v", sel)
	}
	if sel[service.ID] == memberGroup+auto.ID {
		t.Errorf("после «Применить» «Сервис» по-прежнему выбрал удалённую группу: %v", sel)
	}
	rtcFilesLack(t, e.xrayDir(), GroupTag(auto.ID))
	if bals, _ := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))["balancers"].([]any); len(bals) != 0 {
		t.Errorf("balancer-ов после удаления «Авто» %d, ожидалось 0", len(bals))
	}

	// Удаление группы без ссылок идёт без действия; «Остальное» держит замену.
	_, lonely, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Одинокая", Mode: ModeManual, Members: []Member{{Kind: KindDirect}}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, err := e.svc.DeleteGroup(e.rev(t), lonely.ID, Cascade{}); err != nil {
		t.Errorf("удаление группы без ссылок: %v", err)
	}
	if _, ok := rtGroupByName(e.groups(t), "Одинокая"); ok {
		t.Error("группа без ссылок не удалена")
	}

	// drop: правила с целью на группу удаляются, «Остальное» на ней не стоит.
	_, dropG, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Под нож", Mode: ModeManual, Members: []Member{{Kind: KindDirect}}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindGroup, ID: dropG.ID},
		Conds: []Cond{{Type: CondDomain, Values: []string{"drop.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	before := len(e.rules(t).Items)
	if _, err := e.svc.DeleteGroup(e.rev(t), dropG.ID, Cascade{Mode: CascadeDrop}); err != nil {
		t.Fatalf("DeleteGroup(drop): %v", err)
	}
	if after := len(e.rules(t).Items); after != before-1 {
		t.Errorf("правил после drop %d, ожидалось %d", after, before-1)
	}
	// Группы «Одинокая» и «Под нож» созданы и удалены после прошлого применения:
	// черновик совпадает с применённым, «Применить» проходит без записи.
	e.applyPasses(t)
	rtcFilesLack(t, e.xrayDir(), GroupTag(dropG.ID))
}

// Удаление узла «Моих узлов» и подписки показывает группы со ссылками, потери
// узлов по ядру и группы, которые станут пустыми; ссылки убираются из групп той
// же ревизией; опустевшая группа блокирует «Применить» проверкой group_empty, а
// новый узел, проходящий фильтр, снимает блок; тега удалённого узла в файлах Xray
// нет.
func TestRouterCascadeSourceAndNode(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")

	// Единственный узел «Только мои»: остальные исключены по полным именам.
	nodesXray := e.catalog(t).Xray
	if len(nodesXray) != 3 {
		t.Fatalf("в каталоге Xray %d узлов, ожидалось 3", len(nodesXray))
	}
	keep := nodesXray[0]
	var exclude []string
	for _, n := range nodesXray[1:] {
		if strings.Contains(strings.ToLower(keep.Display), strings.ToLower(n.Display)) {
			t.Fatalf("имя одного узла содержит имя другого: исключение по подстроке неоднозначно")
		}
		exclude = append(exclude, n.Display)
	}
	entryID := strings.TrimPrefix(keep.Ref, sources.MySourceID+"/")
	if entryID == keep.Ref {
		t.Fatalf("ссылка узла %q не из источника «Мои узлы»", keep.Ref)
	}
	_, only, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Только мои", Mode: ModeAuto, Sources: GroupSources{IDs: []string{sources.MySourceID}}, Exclude: exclude,
	})
	if err != nil {
		t.Fatalf("SaveGroup (Только мои): %v", err)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Выбор", Mode: ModeManual, Picks: []string{keep.Ref}, Members: []Member{{Kind: KindDirect}},
	}); err != nil {
		t.Fatalf("SaveGroup (Выбор): %v", err)
	}
	e.takeAndApply(t)
	rtcFilesHave(t, e.xrayDir(), "xcp-my-"+entryID)

	imp, err := e.svc.Impact(ImpactNode, entryID)
	if err != nil {
		t.Fatalf("Impact(node): %v", err)
	}
	if len(imp.Groups) != 1 || imp.Groups[0].Name != "Выбор" || imp.Groups[0].Role != RoleComposition {
		t.Errorf("группы со ссылкой на узел: %+v", imp.Groups)
	}
	lost := map[string]int{}
	for _, l := range imp.Kernels.Xray {
		lost[l.Group] = l.Lost
	}
	if lost["Только мои"] != 1 || lost["Авто"] != 1 || lost["Выбор"] != 1 {
		t.Errorf("потери узлов Xray: %+v", imp.Kernels.Xray)
	}
	if len(imp.Empties) != 1 || imp.Empties[0].ID != only.ID {
		t.Errorf("станут пустыми: %+v, ожидалась «Только мои»", imp.Empties)
	}
	if _, err := e.svc.Impact(ImpactNode, "0badc0de"); !errors.Is(err, sources.ErrNodeNotFound) {
		t.Errorf("Impact неизвестного узла: %v", err)
	}
	if _, err := e.svc.Impact(ImpactSource, "0badc0de"); !errors.Is(err, sources.ErrSourceNotFound) {
		t.Errorf("Impact неизвестного источника: %v", err)
	}

	// Удаление: узел, ссылка на него и группа правятся одной ревизией.
	rev := e.rev(t)
	if _, err := e.svc.DeleteNode(rev-1, entryID); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия: %v", err)
	}
	ev, err := e.svc.DeleteNode(rev, entryID)
	if err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if ev.DraftRevision != rev+1 {
		t.Errorf("удаление узла заняло %d ревизий, ожидалась одна", ev.DraftRevision-rev)
	}
	choice, _ := rtGroupByName(e.groups(t), "Выбор")
	if len(choice.Picks) != 0 || len(choice.Members) != 1 {
		t.Errorf("«Выбор» после удаления узла: picks %v, участники %v", choice.Picks, choice.Members)
	}
	for _, n := range e.catalog(t).Xray {
		if n.Ref == keep.Ref {
			t.Errorf("узел %s остался в каталоге", keep.Ref)
		}
	}

	// Группа осталась без узлов: «Применить» отвергается проверкой.
	issues := e.check(t)
	if !rtHasIssue(issues, "group_empty", "group", "Только мои") || len(issues) != 1 {
		t.Fatalf("проверка после удаления единственного узла: %+v", issues)
	}
	_, err = e.applyResult(t)
	var ce *configlayer.ChecksError
	if !errors.Is(err, configlayer.ErrChecksFailed) || !errors.As(err, &ce) || !rtHasIssue(ce.Issues, "group_empty", "group", "Только мои") {
		t.Fatalf("«Применить» не заблокировано пустой группой: %v", err)
	}

	// Новый узел, проходящий фильтр, снимает блок.
	e.addMyLinks(t, "vless_reality")
	if issues := e.check(t); len(issues) != 0 {
		t.Fatalf("после добавления узла остались проблемы: %+v", issues)
	}
	e.applyOK(t)
	rtcFilesLack(t, e.xrayDir(), "xcp-my-"+entryID)

	// «Мои узлы» не удаляются как источник; черновик не меняется.
	rev = e.rev(t)
	if _, err := e.svc.DeleteSource(rev, sources.MySourceID); err == nil {
		t.Error("источник «Мои узлы» удалён")
	}
	if e.rev(t) != rev {
		t.Error("отказ удалить «Мои узлы» изменил черновик")
	}

	// Подписка: секция источника в черновике без снимка узлов; ссылки на неё
	// (источник, отдельный узел, резерв) уходят из групп той же ревизией.
	const subID = "a1b2c3d4"
	subRaw, err := json.Marshal(sources.SubscriptionConfig{
		Kind: sources.KindSubscription, ID: subID, Name: "Подписка", URL: "https://sub.example.test/list",
		Enabled: true, Exclude: []string{}, Interval: sources.Interval{Mode: sources.IntervalProvider},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, sources.SectionPrefix+subID, subRaw)
	_, mixed, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Смешанная", Mode: ModeAuto, Sources: GroupSources{IDs: []string{subID, sources.MySourceID}},
		Picks: []string{subID + "/deadbeef", keepOther(e.catalog(t).Xray)},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Смешанная): %v", err)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "С резервом", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: subID + "/cafebabe",
	}); err != nil {
		t.Fatalf("SaveGroup (С резервом): %v", err)
	}
	srcImp, err := e.svc.Impact(ImpactSource, subID)
	if err != nil {
		t.Fatalf("Impact(source): %v", err)
	}
	roles := map[string]string{}
	for _, g := range srcImp.Groups {
		roles[g.Name] = g.Role
	}
	if roles["Смешанная"] != RoleComposition || roles["С резервом"] != RoleReserve || len(srcImp.Groups) != 2 {
		t.Errorf("группы со ссылкой на подписку: %+v", srcImp.Groups)
	}
	if len(srcImp.Empties) != 0 {
		t.Errorf("подписка без снимка узлов не опустошает группы: %+v", srcImp.Empties)
	}
	rev = e.rev(t)
	ev, err = e.svc.DeleteSource(rev, subID)
	if err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if ev.DraftRevision != rev+1 {
		t.Errorf("удаление подписки заняло %d ревизий, ожидалась одна", ev.DraftRevision-rev)
	}
	if _, has := e.state(t).Draft[sources.SectionPrefix+subID]; has {
		t.Error("секция подписки осталась в черновике")
	}
	mixedAfter, _ := rtGroupByName(e.groups(t), "Смешанная")
	if len(mixedAfter.Sources.IDs) != 1 || mixedAfter.Sources.IDs[0] != sources.MySourceID || len(mixedAfter.Picks) != 1 || mixedAfter.ID != mixed.ID {
		t.Errorf("«Смешанная» после удаления подписки: источники %v, picks %v", mixedAfter.Sources.IDs, mixedAfter.Picks)
	}
	if reserved, _ := rtGroupByName(e.groups(t), "С резервом"); reserved.Reserve != "" {
		t.Errorf("резерв удалённой подписки остался: %q", reserved.Reserve)
	}
	if _, err := e.svc.DeleteSource(e.rev(t), subID); !errors.Is(err, sources.ErrSourceNotFound) {
		t.Errorf("повторное удаление подписки: %v", err)
	}
}

// keepOther возвращает ссылку первого узла списка (отдельный узел «Моих узлов»,
// который останется после удаления подписки).
func keepOther(list []sources.CatalogNode) string {
	if len(list) == 0 {
		return ""
	}
	return list[0].Ref
}

// rtcWarn ищет предупреждение с кодом и возвращает его номер n.
func rtcWarn(list []IssueView, code string) (string, bool) {
	for _, w := range list {
		if w.Code == code {
			return w.Params["n"], true
		}
	}
	return "", false
}

// rtcRuleView находит правило в представлении по ID.
func rtcRuleView(t *testing.T, v RoutingView, id string) RuleView {
	t.Helper()
	for _, r := range v.Rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("правила %s нет в представлении", id)
	return RuleView{}
}

// Проверки качества правил: точный дубль, перекрытие другой целью, правило ниже
// «всё подряд»; выключенное правило ничего не перекрывает; «Применить» эти
// предупреждения не блокируют.
func TestRouterRulesLint(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws")
	proxyG, ok := rtGroupByName(e.groups(t), "Прокси")
	if !ok {
		t.Fatal("нет группы «Прокси»")
	}
	direct, reject, viaProxy := Target{Kind: KindDirect}, Target{Kind: KindReject}, Target{Kind: KindGroup, ID: proxyG.ID}
	add := func(target Target, conds ...Cond) Rule {
		t.Helper()
		_, saved, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: target, Conds: conds})
		if err != nil {
			t.Fatalf("SaveRule: %v", err)
		}
		return saved
	}
	r1 := add(direct, Cond{Type: CondDomain, Values: []string{"a.example.test"}})
	r2 := add(viaProxy, Cond{Type: CondDomain, Values: []string{"sub.a.example.test"}})
	r3 := add(direct, Cond{Type: CondDomain, Values: []string{"a.example.test"}})
	r4 := add(direct, Cond{Type: CondIP, Values: []string{"10.0.0.0/8"}})
	r5 := add(reject, Cond{Type: CondIP, Values: []string{"10.1.0.0/16"}})
	r6 := add(reject, Cond{Type: CondNetwork, Values: []string{"tcp", "udp"}})
	r7 := add(direct, Cond{Type: CondDomain, Values: []string{"b.example.test"}})

	view, err := e.svc.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	want := []struct {
		rule Rule
		code string
		n    string
	}{
		{r1, "", ""}, {r2, warnOverlap, "1"}, {r3, warnExactDup, "1"}, {r4, "", ""},
		{r5, warnOverlap, "4"}, {r6, "", ""}, {r7, warnBelowCatchall, "6"},
	}
	for _, w := range want {
		rv := rtcRuleView(t, view, w.rule.ID)
		if w.code == "" {
			if len(rv.Warnings) != 0 {
				t.Errorf("правило %d: лишние предупреждения %+v", rv.Index, rv.Warnings)
			}
			continue
		}
		if n, ok := rtcWarn(rv.Warnings, w.code); !ok || n != w.n || len(rv.Warnings) != 1 {
			t.Errorf("правило %d: ожидалось %s {n: %s}, получено %+v", rv.Index, w.code, w.n, rv.Warnings)
		}
		if len(rv.Issues) != 0 {
			t.Errorf("правило %d: предупреждение качества стало проблемой: %+v", rv.Index, rv.Issues)
		}
	}

	// Предупреждения не блокируют «Применить».
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("проверка блокирует из-за предупреждений: %+v", issues)
	}
	e.takeAndApply(t)

	// Выключенное правило ничего не перекрывает и само предупреждений не получает.
	if _, err := e.svc.SetRuleEnabled(e.rev(t), r1.ID, false); err != nil {
		t.Fatalf("SetRuleEnabled: %v", err)
	}
	if _, err := e.svc.SetRuleEnabled(e.rev(t), r7.ID, false); err != nil {
		t.Fatalf("SetRuleEnabled: %v", err)
	}
	view, err = e.svc.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	for _, r := range []Rule{r1, r2, r3, r7} {
		if rv := rtcRuleView(t, view, r.ID); len(rv.Warnings) != 0 {
			t.Errorf("правило %d при выключенном перекрывающем: %+v", rv.Index, rv.Warnings)
		}
	}
	if n, ok := rtcWarn(rtcRuleView(t, view, r5.ID).Warnings, warnOverlap); !ok || n != "4" {
		t.Errorf("перекрытие IP пропало после выключения других правил: %q", n)
	}
	e.applyOK(t)
}

// Диалог удаления называет пустыми ровно те группы, которые после удаления
// заблокирует проверка group_empty (правило D-36, состав после каскада): при
// единственном узле удаление узла опустошает «Авто», «Прокси» и обе группы с
// DIRECT не на первом месте — «Сервис» и «Запасную»; удаление «Авто» опустошает
// только «Запасную»: каскад не ставит DIRECT на первое место (D-36), поэтому
// после неё DIRECT не становится выбором по умолчанию и группу заблокирует
// group_empty, а у «Сервиса» после «Прокси» с узлом пустоты нет.
// Ядро стенда не читается и не переключается; «Применить» не вызывается.
func TestRouterCascadeServiceGroupEmpty(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws")

	cfg := e.groups(t)
	auto, okA := rtGroupByName(cfg, "Авто")
	proxyG, okP := rtGroupByName(cfg, "Прокси")
	if !okA || !okP {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Сервис", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: proxyG.ID}, {Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	}); err != nil {
		t.Fatalf("SaveGroup (Сервис): %v", err)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Запасная", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	}); err != nil {
		t.Fatalf("SaveGroup (Запасная): %v", err)
	}
	// Желание «взять маршрутизацию» в черновике: без него проверка проблем не даёт.
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}

	nodesXray := e.catalog(t).Xray
	if len(nodesXray) != 1 {
		t.Fatalf("в каталоге Xray %d узлов, ожидался 1", len(nodesXray))
	}
	entryID := strings.TrimPrefix(nodesXray[0].Ref, sources.MySourceID+"/")
	if entryID == nodesXray[0].Ref {
		t.Fatalf("ссылка узла %q не из источника «Мои узлы»", nodesXray[0].Ref)
	}
	names := []string{"Авто", "Прокси", "Сервис", "Запасная"}

	imp, err := e.svc.Impact(ImpactNode, entryID)
	if err != nil {
		t.Fatalf("Impact(node): %v", err)
	}
	named := map[string]bool{}
	var got []string
	for _, g := range imp.Empties {
		named[g.Name] = true
		got = append(got, g.Name)
	}
	for _, name := range names {
		if !named[name] {
			t.Errorf("диалог удаления не называет опустевшие группы: названы %v, нет «%s»", got, name)
		}
	}

	impAuto, err := e.svc.Impact(ImpactGroup, auto.ID)
	if err != nil {
		t.Fatalf("Impact(group Авто): %v", err)
	}
	if len(impAuto.Empties) != 1 || impAuto.Empties[0].Name != "Запасная" {
		var emp []string
		for _, g := range impAuto.Empties {
			emp = append(emp, g.Name)
		}
		t.Errorf("диалог удаления «Авто» называет пустыми %v: ожидалась только «Запасная» (DIRECT после каскада не первый), у «Прокси» остаётся узел", emp)
	}

	// «Сейчас» (маршрутизация не применена — по модели черновика): контроль, что
	// проверка после удаления не пуста сама по себе.
	if cur := rtViewGroup(t, e.view(t), "Сервис").Current; cur != memberGroup+proxyG.ID {
		t.Errorf("до удаления «Сейчас» у «Сервиса» — %q, ожидалась «Прокси»", cur)
	}

	if _, err := e.svc.DeleteNode(e.rev(t), entryID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	issues := e.check(t)
	for _, name := range names {
		if !rtHasIssue(issues, "group_empty", "group", name) {
			t.Errorf("проверка не блокирует опустевшую группу «%s»", name)
		}
	}
	v := e.view(t)
	for _, name := range []string{"Сервис", "Прокси"} {
		if cur := rtViewGroup(t, v, name).Current; cur != "" {
			t.Errorf("у опустевшей группы «%s» «Сейчас» — %q: участник, которого ядро не выберет", name, cur)
		}
	}
}
