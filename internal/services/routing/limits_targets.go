package routing

import (
	"sort"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Цели учёта лимитов трафика (D-02): что и где считает служба лимитов на каждом
// ядре. Правила — по измерениям spike фазы 152:
//   - Mihomo: расход группы — дельты соединений, в chains которых есть имя группы
//     в профиле (внешней и вложенной); «резерв» — ещё и скрытая группа основных
//     узлов.
//   - Xray: ручная группа — собственный тег xcp-g-<id> (тело — копия узла,
//     счётчик копится на теге группы); «авто» и «резерв» — сумма тегов узлов
//     балансировщика. Узел в нескольких таких группах или вложенная группа —
//     расход приблизительный.
//   - Весь трафик Xray — теги узлов всех «авто»/«резерв» и теги ручных групп,
//     чья цепочка выбора кончается узлом: loopback на балансировщик уже посчитан
//     тегами узлов, DIRECT и REJECT расходом не считаются.

// TrafficTargets возвращает цели учёта по применённому состоянию маршрутизации.
// Ошибка чтения состояния или каталога — ошибка: служба лимитов считает цели
// неизвестными.
func (s *Service) TrafficTargets() (services.TrafficTargets, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return services.TrafficTargets{}, err
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return services.TrafficTargets{}, err
	}
	desired := configlayer.DesiredRouting(st.Applied)
	out := services.TrafficTargets{
		Groups:        make(map[string]services.GroupTarget, len(am.groups.Items)),
		MihomoManaged: am.installed.Mihomo && desired.ManagedFor(configlayer.KernelMihomo),
		XrayManaged:   am.installed.Xray && desired.ManagedFor(configlayer.KernelXray),
	}

	var mb *mihomoBuild
	if out.MihomoManaged {
		if mb, err = s.mihomoBuildOf(st, am.installed); err != nil {
			return services.TrafficTargets{}, err
		}
	}

	// Вложенные группы: участник другой группы. У Mihomo в chains внешнего
	// трафика есть и имя вложенной группы, у Xray тег вложенной ручной группы
	// этот трафик не видит — расход расходится между ядрами.
	nested := map[string]bool{}
	for _, g := range am.groups.Items {
		for _, m := range g.Members {
			if m.Kind == KindGroup {
				nested[m.ID] = true
			}
		}
	}

	// Теги узлов «авто»/«резерв» в Xray и число таких групп у каждого тега.
	nodeTags := map[string][]string{}
	tagGroups := map[string]int{}
	if out.XrayManaged {
		for _, g := range am.groups.Items {
			if g.Mode == ModeManual {
				continue
			}
			tags := xrayNodeTags(Resolve(g, am.cat.Xray, am.groups.Items).Nodes)
			nodeTags[g.ID] = tags
			for _, t := range tags {
				tagGroups[t]++
			}
		}
	}

	live := liveSelection(st.Live)
	global := map[string]bool{}
	for _, g := range am.groups.Items {
		gt := services.GroupTarget{Name: g.Name, Mode: string(g.Mode), Approx: nested[g.ID]}
		if mb != nil && mb.isEmitted(g.ID) {
			gt.MihomoNames = []string{g.Name}
			if g.Mode == ModeFallback {
				gt.MihomoNames = append(gt.MihomoNames, hiddenName(g.ID))
			}
		}
		if g.Mode == ModeManual {
			final := manualFinal(g, am, live)
			// Сырой выбор Live смотрится отдельно: служебный DIRECT/REJECT арбитра
			// ставится и вне состава группы, поле не зависит от того, как его
			// трактует действующий выбор.
			gt.Direct = hiddenMember(live[g.ID]) || final == memberDirect || final == memberReject
			if out.XrayManaged {
				tag := GroupTag(g.ID)
				gt.XrayTags = []string{tag}
				if !gt.Direct && final == memberNode {
					global[tag] = true
				}
			}
		} else if out.XrayManaged {
			gt.XrayTags = nodeTags[g.ID]
			for _, t := range gt.XrayTags {
				global[t] = true
				if tagGroups[t] > 1 {
					gt.Approx = true
				}
			}
		}
		out.Groups[g.ID] = gt
	}

	out.XrayGlobalTags = make([]string, 0, len(global))
	for t := range global {
		out.XrayGlobalTags = append(out.XrayGlobalTags, t)
	}
	sort.Strings(out.XrayGlobalTags)
	return out, nil
}

// manualFinal — чем кончается цепочка выбора ручной группы (как «Сейчас»
// runtimeChain): memberNode — узлом, memberDirect или memberReject — служебным
// вариантом, memberGroup — группой «авто»/«резерв» (loopback на балансировщик
// Xray), пусто — действующего участника нет.
func manualFinal(g Group, am appliedModel, live map[string]string) string {
	chain := runtimeChain(g, am, live)
	if len(chain) == 0 {
		return ""
	}
	switch last := chain[len(chain)-1]; last.Kind {
	case "node":
		return memberNode
	case KindDirect:
		return memberDirect
	case KindReject:
		return memberReject
	case KindGroup:
		if h, ok := am.group(last.ID); ok && h.Mode != ModeManual {
			return memberGroup
		}
	}
	return ""
}

// xrayNodeTags — теги Xray узлов списка без повторов и пустых, по возрастанию.
func xrayNodeTags(list []sources.CatalogNode) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, n := range list {
		if n.XrayTag == "" || seen[n.XrayTag] {
			continue
		}
		seen[n.XrayTag] = true
		out = append(out, n.XrayTag)
	}
	sort.Strings(out)
	return out
}
