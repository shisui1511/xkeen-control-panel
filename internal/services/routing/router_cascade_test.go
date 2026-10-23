//go:build router

package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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

	// Выбор участников в Live: ключ удалённой группы и выбор её как участника
	// уходят, остальное остаётся.
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
	if len(sel) != 1 || sel[proxyG.ID] != memberDirect {
		t.Errorf("выбор в Live после каскада: %v, ожидался только выбор «Прокси»", sel)
	}

	// «Применить» проходит проверкой ядра, тега удалённой группы нет в файлах.
	e.applyOK(t)
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
	e.applyOK(t)
	rtcFilesLack(t, e.xrayDir(), GroupTag(dropG.ID))
}
