package routing

import (
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// maxPreviewNames — сколько имён узлов предпросмотр отдаёт по каждому ядру.
const maxPreviewNames = 500

// Resolved — состав группы для одного ядра.
type Resolved struct {
	// Nodes — узлы группы в порядке каталога (источник, затем порядок записи в
	// файл ядра), без повторов.
	Nodes []sources.CatalogNode
	// Members — участники ручной группы как есть, без ссылок на несуществующие
	// группы; у «авто» и «резерва» пусто.
	Members []Member
	// Stale — отдельные узлы (Picks), которых нет в списке этого ядра: провайдер
	// убрал узел или ядро его не умеет. Для проверки «устарел узел» нужно
	// сопоставить результаты по ядрам.
	Stale []string
}

// nameFilter — фильтр по имени узла: подстроки без учёта регистра или регулярные
// выражения Go (режим «Эксперт»).
type nameFilter struct {
	// active — список непуст: пустой список не фильтрует.
	active bool
	subs   []string
	res    []*regexp.Regexp
}

// newNameFilter строит фильтр из значений группы. Пустые значения пропускаются
// (иначе пустая подстрока совпала бы со всем); выражение, которое не
// компилируется, не совпадает ни с чем: проверка выражений идёт при сохранении.
func newNameFilter(values []string, regex bool) nameFilter {
	var f nameFilter
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		f.active = true
		if regex {
			if re, err := regexp.Compile(v); err == nil {
				f.res = append(f.res, re)
			}
			continue
		}
		f.subs = append(f.subs, strings.ToLower(v))
	}
	return f
}

// match сообщает, подходит ли имя хотя бы под одно значение.
func (f nameFilter) match(display string) bool {
	if len(f.subs) > 0 {
		low := strings.ToLower(display)
		for _, s := range f.subs {
			if strings.Contains(low, s) {
				return true
			}
		}
	}
	for _, re := range f.res {
		if re.MatchString(display) {
			return true
		}
	}
	return false
}

// Resolve считает состав группы по списку узлов одного ядра (шаги D-11):
//  1. узлы выбранных источников (или всех);
//  2. «включить, если в имени есть» — подстроки без учёта регистра; пустой
//     список не фильтрует;
//  3. «исключить, если есть»;
//  4. плюс отдельные узлы по ссылке — их фильтры не отсекают.
//
// Это единственная функция состава: предпросмотр, проверка, представление и
// генераторы обоих ядер вызывают её. groups — группы секции; участник-группа,
// которой в них нет, в Members не попадает (nil — участники копируются как есть).
func Resolve(g Group, nodesOfKernel []sources.CatalogNode, groups []Group) Resolved {
	out := Resolved{Nodes: []sources.CatalogNode{}, Members: []Member{}, Stale: []string{}}

	srcSet := make(map[string]bool, len(g.Sources.IDs))
	for _, id := range g.Sources.IDs {
		srcSet[id] = true
	}
	picks := make(map[string]bool, len(g.Picks))
	for _, ref := range g.Picks {
		picks[ref] = true
	}
	include := newNameFilter(g.Include, g.Regex)
	exclude := newNameFilter(g.Exclude, g.Regex)

	present := make(map[string]bool, len(nodesOfKernel))
	taken := make(map[string]bool, len(nodesOfKernel))
	for _, n := range nodesOfKernel {
		present[n.Ref] = true
		if taken[n.Ref] {
			continue
		}
		byFilter := (g.Sources.All || srcSet[n.Source]) &&
			(!include.active || include.match(n.Display)) &&
			!(exclude.active && exclude.match(n.Display))
		if byFilter || picks[n.Ref] {
			taken[n.Ref] = true
			out.Nodes = append(out.Nodes, n)
		}
	}

	seenStale := make(map[string]bool)
	for _, ref := range g.Picks {
		if !present[ref] && !seenStale[ref] {
			seenStale[ref] = true
			out.Stale = append(out.Stale, ref)
		}
	}

	if g.Mode == ModeManual {
		known := make(map[string]bool, len(groups))
		for _, x := range groups {
			known[x.ID] = true
		}
		for _, m := range g.Members {
			if m.Kind == KindGroup && groups != nil && !known[m.ID] {
				continue
			}
			out.Members = append(out.Members, m)
		}
	}
	return out
}

// KernelPreview — состав группы для одного ядра в предпросмотре.
type KernelPreview struct {
	// Count — число узлов.
	Count int `json:"count"`
	// Names — имена узлов (не больше 500).
	Names []string `json:"names"`
	// Members — участники ручной группы.
	Members []Member `json:"members"`
}

// GroupPreview — состав группы по ядрам; nil — ядро не установлено.
type GroupPreview struct {
	Xray   *KernelPreview `json:"xray"`
	Mihomo *KernelPreview `json:"mihomo"`
}

// kernelPreview превращает результат Resolve в предпросмотр.
func kernelPreview(r Resolved) *KernelPreview {
	p := &KernelPreview{Count: len(r.Nodes), Names: []string{}, Members: r.Members}
	for _, n := range r.Nodes {
		if len(p.Names) >= maxPreviewNames {
			break
		}
		p.Names = append(p.Names, n.Display)
	}
	return p
}

// PreviewGroup показывает, какие узлы попадут в группу, отдельно по каждому
// установленному ядру (D-11): тот же Resolve по каталогу текущего черновика, что
// считают проверка и генераторы. Группа не сохраняется; выражения, которые не
// проходят проверку, — ErrInvalidInput.
func (s *Service) PreviewGroup(g Group) (GroupPreview, error) {
	normalizeGroup(&g)
	if err := validateFilters(&g); err != nil {
		return GroupPreview{}, err
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return GroupPreview{}, err
	}
	cfg, err := parseGroups(st.Draft[GroupsSection])
	if err != nil {
		return GroupPreview{}, err
	}
	installed := s.d.Installed()
	cat, err := s.d.Sources.Catalog(inputOf(st), installed)
	if err != nil {
		return GroupPreview{}, err
	}
	var out GroupPreview
	if installed.Xray {
		out.Xray = kernelPreview(Resolve(g, cat.Xray, cfg.Items))
	}
	if installed.Mihomo {
		out.Mihomo = kernelPreview(Resolve(g, cat.Mihomo, cfg.Items))
	}
	return out, nil
}
