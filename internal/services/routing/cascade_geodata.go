package routing

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// GeodataSection — секция черновика с пометками на удаление и выключение
// источников dat (D-18): {"v":1,"drop":[{file, mode}]}. Пометка живёт рядом с
// правилами, поэтому «Сбросить» возвращает и её, и каскад.
const GeodataSection = "geodata"

// Режимы пометки источника (GeodataDrop.Mode): удалить запись и файл или выключить
// (для стандартного набора XKeen выключить значит убрать файл).
const (
	DropDelete = "delete"
	DropOff    = "off"
)

// Действия каскада для списка файла (GeodataAction.Action, D-20).
const (
	// ActionReplace — заменить список другим (GeodataAction.With).
	ActionReplace = "replace"
	// ActionRemove — убрать список из условий правил.
	ActionRemove = "remove"
	// ActionDropRules — удалить правила со ссылкой на список целиком.
	ActionDropRules = "drop_rules"
)

// Коды причин ErrInvalidInput каскада по файлу (поле Reason).
const (
	reasonFileInvalid    = "file_invalid"
	reasonActionsInvalid = "actions_invalid"
	reasonActionUnknown  = "action_unknown"
	reasonReplaceInvalid = "replace_invalid"
	reasonDropNotFound   = "drop_not_found"
)

// ErrGeodataRawRef — на файл ссылается своё правило ядра (текст содержит
// ext:<файл>:): каскад его не правит, удаление источника отклоняется (код
// raw_ref); места перечисляет GeodataImpact.Raw.
var ErrGeodataRawRef = &lists.CodedError{Code: "raw_ref"}

// GeodataDrop — пометка источника в секции geodata.
type GeodataDrop struct {
	File string `json:"file"`
	Mode string `json:"mode"`
}

// GeodataDraft — секция geodata черновика.
type GeodataDraft struct {
	V    int           `json:"v"`
	Drop []GeodataDrop `json:"drop"`
}

// GeodataRuleRef — правило в составе влияния: ID и номер (с единицы, как в проверке).
type GeodataRuleRef struct {
	ID string `json:"id"`
	N  int    `json:"n"`
}

// RawRef — своё правило ядра, текст которого ссылается на файл.
type RawRef struct {
	Rule string `json:"rule"`
	N    int    `json:"n"`
}

// GeodataListImpact — влияние на один список файла (файл · тег).
type GeodataListImpact struct {
	Tag  string `json:"tag"`
	Kind string `json:"kind"`
	// Rules — правила черновика, у которых этот список есть в условиях.
	Rules []GeodataRuleRef `json:"rules"`
	// Dies — ID правил, которые будут удалены, если убрать из них все ссылки на
	// файл: других условий у них нет.
	Dies []string `json:"dies"`
	// Suggest — тот же тег в другом установленном файле того же вида.
	Suggest *lists.ListRef `json:"suggest,omitempty"`
}

// GeodataImpact — что затронет удаление или выключение файла: списки с правилами
// и свои правила ядра, которые каскад не правит.
type GeodataImpact struct {
	File  string              `json:"file"`
	Lists []GeodataListImpact `json:"lists"`
	Raw   []RawRef            `json:"raw"`
}

// GeodataAction — выбранное действие для списка (по тегу).
type GeodataAction struct {
	Tag    string `json:"tag"`
	Action string `json:"action"`
	// With — список замены при Action == replace.
	With *lists.ListRef `json:"with,omitempty"`
}

// ParseGeodataDraft разбирает секцию geodata. Нет секции или null — пустой
// черновик; версия по умолчанию 1. Мусор, чужая версия, плохое имя файла или
// режим — ErrInvalidInput (section_invalid): о порче сообщает проверка.
func ParseGeodataDraft(raw json.RawMessage) (GeodataDraft, error) {
	if isEmptyRaw(raw) {
		return GeodataDraft{V: sectionVersion, Drop: []GeodataDrop{}}, nil
	}
	var d GeodataDraft
	if err := json.Unmarshal(raw, &d); err != nil {
		return GeodataDraft{}, invalid(GeodataSection, reasonSectionInvalid)
	}
	if d.V == 0 {
		d.V = sectionVersion
	}
	if d.V != sectionVersion {
		return GeodataDraft{}, invalid(GeodataSection, reasonSectionInvalid)
	}
	if d.Drop == nil {
		d.Drop = []GeodataDrop{}
	}
	for _, x := range d.Drop {
		if !listFileRE.MatchString(x.File) || (x.Mode != DropDelete && x.Mode != DropOff) {
			return GeodataDraft{}, invalid(GeodataSection, reasonSectionInvalid)
		}
	}
	return d, nil
}

// encodeGeodataDraft сериализует секцию geodata; пустой список пометок — nil
// (секция удаляется из черновика).
func encodeGeodataDraft(d GeodataDraft) (json.RawMessage, error) {
	if len(d.Drop) == 0 {
		return nil, nil
	}
	d.V = sectionVersion
	return json.Marshal(d)
}

// datFilesOf возвращает файлы индекса; нет индекса или каталога — пусто.
func datFilesOf(ix *lists.Index) []lists.FileInfo {
	if ix == nil {
		return nil
	}
	files, err := ix.Files()
	if err != nil {
		return nil
	}
	return files
}

// nameSet возвращает все имена файла: реальное и алиасы пары XKeen (zkeen.dat и
// geosite_zkeen.dat — один файл). Файла нет в индексе — только переданное имя.
func nameSet(files []lists.FileInfo, file string) map[string]bool {
	for _, f := range files {
		if f.Name == file || slices.Contains(f.Aliases, file) {
			set := map[string]bool{f.Name: true}
			for _, a := range f.Aliases {
				set[a] = true
			}
			return set
		}
	}
	return map[string]bool{file: true}
}

// dropNames объединяет имена всех файлов с пометкой drop.
func dropNames(files []lists.FileInfo, d GeodataDraft) map[string]bool {
	out := map[string]bool{}
	for _, x := range d.Drop {
		for n := range nameSet(files, x.File) {
			out[n] = true
		}
	}
	return out
}

// rewriteRuleLists применяет fn к каждому списку каждого условия правила. fn
// возвращает (замена, затронут): затронут без замены — список убирается. Условие
// без значений и списков после правки убирается целиком, остальные условия
// остаются («И»). changed — правило изменилось; dies — у изменённого правила не
// осталось условий и своего правила ядра.
func rewriteRuleLists(r Rule, fn func(lists.ListRef) (*lists.ListRef, bool)) (out Rule, changed, dies bool) {
	conds := make([]Cond, 0, len(r.Conds))
	for _, c := range r.Conds {
		touched := false
		refs := make([]lists.ListRef, 0, len(c.Lists))
		seen := map[string]bool{}
		add := func(ref lists.ListRef) {
			if k := listKey(ref.File, ref.Tag); !seen[k] {
				seen[k] = true
				refs = append(refs, ref)
			}
		}
		for _, ref := range c.Lists {
			repl, hit := fn(ref)
			if !hit {
				add(ref)
				continue
			}
			touched = true
			if repl != nil {
				add(*repl)
			}
		}
		if !touched {
			conds = append(conds, c)
			continue
		}
		changed = true
		if len(refs) == 0 && len(c.Values) == 0 {
			continue
		}
		c.Lists = refs
		conds = append(conds, c)
	}
	if !changed {
		return r, false, false
	}
	r.Conds = conds
	return r, true, len(conds) == 0 && r.Raw == nil
}

// geodataImpactOf считает влияние файла по правилам черновика состояния st.
func (s *Service) geodataImpactOf(st configlayer.StateView, file string) (GeodataImpact, error) {
	if !listFileRE.MatchString(file) {
		return GeodataImpact{}, invalid("file", reasonFileInvalid)
	}
	rules, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		return GeodataImpact{}, err
	}
	draft, err := ParseGeodataDraft(st.Draft[GeodataSection])
	if err != nil {
		return GeodataImpact{}, err
	}
	files := datFilesOf(s.d.Lists)
	names := nameSet(files, file)
	// Замена подсказывается только на файл, которого нет ни среди имён этого
	// файла, ни среди уже помеченных.
	exclude := dropNames(files, draft)
	for n := range names {
		exclude[n] = true
	}

	imp := GeodataImpact{File: file, Lists: []GeodataListImpact{}, Raw: []RawRef{}}
	byTag := map[string]int{}
	inFile := func(ref lists.ListRef) bool { return names[ref.File] }
	for i, r := range rules.Items {
		if r.Raw != nil {
			for n := range names {
				if strings.Contains(r.Raw.Text, "ext:"+n+":") {
					imp.Raw = append(imp.Raw, RawRef{Rule: r.ID, N: i + 1})
					break
				}
			}
			continue
		}
		_, _, dies := rewriteRuleLists(r, func(ref lists.ListRef) (*lists.ListRef, bool) { return nil, inFile(ref) })
		seen := map[string]bool{}
		for _, c := range r.Conds {
			for _, ref := range c.Lists {
				if !inFile(ref) {
					continue
				}
				key := strings.ToUpper(ref.Tag)
				idx, ok := byTag[key]
				if !ok {
					idx = len(imp.Lists)
					byTag[key] = idx
					imp.Lists = append(imp.Lists, GeodataListImpact{Tag: ref.Tag, Kind: ref.Kind, Rules: []GeodataRuleRef{}, Dies: []string{}})
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				li := &imp.Lists[idx]
				li.Rules = append(li.Rules, GeodataRuleRef{ID: r.ID, N: i + 1})
				if dies {
					li.Dies = append(li.Dies, r.ID)
				}
			}
		}
	}
	for i := range imp.Lists {
		imp.Lists[i].Suggest = suggestList(s.d.Lists, files, exclude, imp.Lists[i].Tag, imp.Lists[i].Kind)
	}
	return imp, nil
}

// suggestList ищет тот же тег в другом файле индекса того же вида: первый по
// имени файл, которого нет в exclude и где тег есть.
func suggestList(ix *lists.Index, files []lists.FileInfo, exclude map[string]bool, tag, kind string) *lists.ListRef {
	if ix == nil {
		return nil
	}
	for _, f := range files {
		if exclude[f.Name] || (kind != "" && f.Kind != kind) {
			continue
		}
		aliased := false
		for _, a := range f.Aliases {
			aliased = aliased || exclude[a]
		}
		if aliased {
			continue
		}
		cf, ct, ok, err := ix.Has(f.Name, tag)
		if err != nil || !ok {
			continue
		}
		return &lists.ListRef{File: cf, Tag: ct, Kind: f.Kind}
	}
	return nil
}

// GeodataImpact показывает, что затронет удаление или выключение файла: списки
// с правилами черновика (подсказка замены, правила, которые умрут) и свои правила
// ядра. Ничего не меняет.
func (s *Service) GeodataImpact(file string) (GeodataImpact, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return GeodataImpact{}, err
	}
	return s.geodataImpactOf(st, file)
}

// resolveReplacement проверяет список замены по индексу и возвращает каноничную
// ссылку: файл существует, тег в нём есть, файл не из удаляемого и не из уже
// помеченных, вид совпадает с видом заменяемого списка.
func resolveReplacement(ix *lists.Index, files []lists.FileInfo, exclude map[string]bool, with *lists.ListRef, kind string) (lists.ListRef, error) {
	bad := invalid("actions", reasonReplaceInvalid)
	if ix == nil || with == nil || !listFileRE.MatchString(with.File) {
		return lists.ListRef{}, bad
	}
	for n := range nameSet(files, with.File) {
		if exclude[n] {
			return lists.ListRef{}, bad
		}
	}
	cf, ct, ok, err := ix.Has(with.File, with.Tag)
	if err != nil || !ok {
		return lists.ListRef{}, bad
	}
	for _, f := range files {
		if f.Name == cf {
			if kind != "" && f.Kind != kind {
				return lists.ListRef{}, bad
			}
			return lists.ListRef{File: cf, Tag: ct, Kind: f.Kind}, nil
		}
	}
	return lists.ListRef{}, bad
}

// GeodataDrop помечает источник на удаление или выключение и той же ревизией
// правит правила по выбранным действиям (D-18, D-20): у каждого списка файла с
// правилами ровно одно действие. Правило без условий после «убрать» удаляется,
// правило «из пресета» после правки теряет пометку. Свои правила ядра со ссылкой
// на файл — ErrGeodataRawRef. Нет ссылок — только пометка. Ревизия правила и
// секция geodata меняются одним EditDraftBatch: одно событие, «Сбросить»
// возвращает всё.
func (s *Service) GeodataDrop(rev int64, file, mode string, actions []GeodataAction) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if mode != DropDelete && mode != DropOff {
			return nil, invalid("mode", reasonModeInvalid)
		}
		imp, err := s.geodataImpactOf(st, file)
		if err != nil {
			return nil, err
		}
		if len(imp.Raw) > 0 {
			return nil, ErrGeodataRawRef
		}
		draft, err := ParseGeodataDraft(st.Draft[GeodataSection])
		if err != nil {
			return nil, err
		}
		files := datFilesOf(s.d.Lists)
		names := nameSet(files, file)
		exclude := dropNames(files, draft)
		for n := range names {
			exclude[n] = true
		}

		// Ровно одно действие на каждый список файла, лишних тегов нет.
		byTag := map[string]GeodataAction{}
		for _, a := range actions {
			key := strings.ToUpper(a.Tag)
			if _, dup := byTag[key]; dup {
				return nil, invalid("actions", reasonActionsInvalid)
			}
			byTag[key] = a
		}
		if len(byTag) != len(imp.Lists) {
			return nil, invalid("actions", reasonActionsInvalid)
		}
		replacement := map[string]lists.ListRef{}
		action := map[string]string{}
		for _, li := range imp.Lists {
			key := strings.ToUpper(li.Tag)
			a, ok := byTag[key]
			if !ok {
				return nil, invalid("actions", reasonActionsInvalid)
			}
			switch a.Action {
			case ActionRemove, ActionDropRules:
			case ActionReplace:
				ref, err := resolveReplacement(s.d.Lists, files, exclude, a.With, li.Kind)
				if err != nil {
					return nil, err
				}
				replacement[key] = ref
			default:
				return nil, invalid("actions", reasonActionUnknown)
			}
			action[key] = a.Action
		}

		sections := map[string]json.RawMessage{}
		if len(imp.Lists) > 0 {
			rules, err := parseRules(st.Draft[RulesSection])
			if err != nil {
				return nil, err
			}
			inFile := func(ref lists.ListRef) bool { return names[ref.File] }
			items := make([]Rule, 0, len(rules.Items))
			for _, r := range rules.Items {
				if r.Raw != nil {
					items = append(items, r)
					continue
				}
				if ruleHasAction(r, inFile, action, ActionDropRules) {
					continue
				}
				out, changed, dies := rewriteRuleLists(r, func(ref lists.ListRef) (*lists.ListRef, bool) {
					if !inFile(ref) {
						return nil, false
					}
					key := strings.ToUpper(ref.Tag)
					if repl, ok := replacement[key]; ok {
						return &repl, true
					}
					return nil, true
				})
				switch {
				case dies:
					continue
				case changed && out.Origin == OriginPreset:
					// Правка условий снимает пометку «из пресета», как SaveRule (D-24).
					out.Origin = ""
				}
				items = append(items, out)
			}
			rules.Items = items
			raw, err := encodeRules(rules)
			if err != nil {
				return nil, err
			}
			sections[RulesSection] = raw
		}

		marks := make([]GeodataDrop, 0, len(draft.Drop)+1)
		for _, x := range draft.Drop {
			if !names[x.File] {
				marks = append(marks, x)
			}
		}
		draft.Drop = append(marks, GeodataDrop{File: file, Mode: mode})
		raw, err := encodeGeodataDraft(draft)
		if err != nil {
			return nil, err
		}
		sections[GeodataSection] = raw
		return sections, nil
	})
}

// ruleHasAction сообщает, что у правила есть список файла, для которого выбрано
// действие act.
func ruleHasAction(r Rule, inFile func(lists.ListRef) bool, action map[string]string, act string) bool {
	for _, c := range r.Conds {
		for _, ref := range c.Lists {
			if inFile(ref) && action[strings.ToUpper(ref.Tag)] == act {
				return true
			}
		}
	}
	return false
}

// GeodataRestore снимает пометку drop с файла (кнопка «Вернуть источник»): секция
// geodata правится отдельной ревизией, а изменения правил остаются в черновике
// до «Сбросить». Пометки на файл нет — ErrInvalidInput (file, drop_not_found).
// Последняя пометка убирает секцию целиком.
func (s *Service) GeodataRestore(rev int64, file string) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if !listFileRE.MatchString(file) {
			return nil, invalid("file", reasonFileInvalid)
		}
		draft, err := ParseGeodataDraft(st.Draft[GeodataSection])
		if err != nil {
			return nil, err
		}
		names := nameSet(datFilesOf(s.d.Lists), file)
		keep := make([]GeodataDrop, 0, len(draft.Drop))
		found := false
		for _, x := range draft.Drop {
			if names[x.File] {
				found = true
				continue
			}
			keep = append(keep, x)
		}
		if !found {
			return nil, invalid("file", reasonDropNotFound)
		}
		draft.Drop = keep
		raw, err := encodeGeodataDraft(draft)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{GeodataSection: raw}, nil
	})
}

// geodataRefProblems находит включённые правила черновика, которые снова ссылаются
// на файл с пометкой drop: geodata_ref_remains {file, tag, rule}. Список в условии
// даёт ссылку по ListRef, своё правило ядра — по подстроке ext:<файл>:<тег>. Нет
// пометок — нет проблем. Номер правила считается с единицы по всему списку.
func geodataRefProblems(items []Rule, d GeodataDraft, ix *lists.Index) []IssueView {
	if len(d.Drop) == 0 {
		return nil
	}
	hidden := dropNames(datFilesOf(ix), d)
	names := make([]string, 0, len(hidden))
	for n := range hidden {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []IssueView
	issue := func(r Rule, n int, file, tag string) {
		out = append(out, IssueView{Code: issueGeodataRefRemains, Key: "rule:" + r.ID,
			Params: map[string]string{"file": file, "tag": tag, "rule": strconv.Itoa(n)}})
	}
	for i, r := range items {
		if !r.Enabled {
			continue
		}
		if r.Raw != nil {
			for _, n := range names {
				if tag, ok := rawExtTag(r.Raw.Text, n); ok {
					issue(r, i+1, n, tag)
					break
				}
			}
			continue
		}
		seen := map[string]bool{}
		for _, c := range r.Conds {
			for _, ref := range c.Lists {
				if !hidden[ref.File] {
					continue
				}
				if k := listKey(ref.File, ref.Tag); !seen[k] {
					seen[k] = true
					issue(r, i+1, ref.File, ref.Tag)
				}
			}
		}
	}
	return out
}

// rawExtTag ищет в тексте своего правила ядра ссылку ext:<файл>:<тег> и возвращает
// тег (пустой, если за двоеточием ничего нет).
func rawExtTag(text, file string) (string, bool) {
	_, rest, ok := strings.Cut(text, "ext:"+file+":")
	if !ok {
		return "", false
	}
	end := strings.IndexAny(rest, "\",) \t\r\n")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end], true
}

// hideDropped убирает из каталога списки файлов с пометкой drop (в выборе списка
// их предлагать нельзя): записи «Все списки», ссылки сервисов, списки стран.
// Сервис без ссылок остаётся в каталоге, как и при отсутствующих файлах; страна,
// у которой IP-список в скрытом файле, не показывается (IP-список — основа страны).
func hideDropped(cat lists.CatalogView, hidden map[string]bool) lists.CatalogView {
	if len(hidden) == 0 {
		return cat
	}
	keepRefs := func(in []lists.ListRef) []lists.ListRef {
		out := make([]lists.ListRef, 0, len(in))
		for _, r := range in {
			if !hidden[r.File] {
				out = append(out, r)
			}
		}
		return out
	}
	entries := make([]lists.ListEntry, 0, len(cat.Lists))
	for _, l := range cat.Lists {
		if !hidden[l.File] {
			entries = append(entries, l)
		}
	}
	cat.Lists = entries
	services := make([]lists.ServiceView, 0, len(cat.Services))
	for _, sv := range cat.Services {
		sv.Refs = keepRefs(sv.Refs)
		services = append(services, sv)
	}
	cat.Services = services
	countries := make([]lists.CountryView, 0, len(cat.Countries))
	for _, c := range cat.Countries {
		if c.IP != nil && hidden[c.IP.File] {
			continue
		}
		if c.Site != nil && hidden[c.Site.File] {
			c.Site = nil
		}
		c.Blocked = keepRefs(c.Blocked)
		countries = append(countries, c)
	}
	cat.Countries = countries
	return cat
}
