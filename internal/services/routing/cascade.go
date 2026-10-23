package routing

import (
	"encoding/json"
	"errors"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Действия каскада удаления группы (Cascade.Mode).
const (
	// CascadeRedirect — направить правила и «Остальное» на другую цель.
	CascadeRedirect = "redirect"
	// CascadeDrop — удалить правила с целью на группу.
	CascadeDrop = "drop"
)

// reasonFinalRequiresRedirect — на группу ссылается «Остальное»: у него цель
// обязательна, поэтому нужна замена.
const reasonFinalRequiresRedirect = "final_requires_redirect"

// ErrReferenced — на группу есть ссылки, действие каскада не выбрано (код
// group_referenced); детали с Impact — в *ReferencedError.
var ErrReferenced = errors.New("routing: на группу есть ссылки")

// ReferencedError — отказ удаления группы со ссылками: черновик не менялся,
// Impact показывает, что затронет удаление.
type ReferencedError struct{ Impact Impact }

func (e *ReferencedError) Error() string { return ErrReferenced.Error() }

// Is связывает ошибку с ErrReferenced.
func (e *ReferencedError) Is(target error) bool { return target == ErrReferenced }

// Cascade — выбранное действие при удалении группы со ссылками.
type Cascade struct {
	// Mode — "" (действие не выбрано), redirect или drop.
	Mode string `json:"mode"`
	// To — новая цель при redirect: группа, DIRECT или REJECT.
	To *Target `json:"to,omitempty"`
}

// selectionDrop убирает из Live[selection] выбор удалённой группы и выбор самой
// группы как участника в других группах: дальше они выбирают по умолчанию.
func selectionDrop(id string) func(live configlayer.Sections) error {
	return func(live configlayer.Sections) error {
		raw, ok := live[LiveKeySelection]
		if !ok || isEmptyRaw(raw) {
			return nil
		}
		var sel map[string]string
		if err := json.Unmarshal(raw, &sel); err != nil {
			// Порчу живых данных генераторы переносят и так: выбора нет.
			return nil
		}
		changed := false
		for gid, member := range sel {
			if gid == id || member == memberGroup+id {
				delete(sel, gid)
				changed = true
			}
		}
		if !changed {
			return nil
		}
		out, err := json.Marshal(sel)
		if err != nil {
			return err
		}
		live[LiveKeySelection] = out
		return nil
	}
}

// DeleteGroup удаляет группу из черновика одной ревизией вместе с очисткой
// выбора в Live (D-34). Есть ссылки (правила, «Остальное», участие в ручных
// группах) и действие не выбрано — *ReferencedError без изменений. redirect
// направляет правила и «Остальное» на c.To, drop удаляет правила с целью на
// группу; если на неё ссылается «Остальное», нужна замена (ErrInvalidInput,
// final_requires_redirect). Из состава ручных групп удалённая группа уходит
// всегда.
func (s *Service) DeleteGroup(rev int64, id string, c Cascade) (configlayer.DraftEvent, error) {
	switch c.Mode {
	case "", CascadeRedirect, CascadeDrop:
	default:
		return configlayer.DraftEvent{}, invalid("mode", reasonModeInvalid)
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	groups, err := parseGroups(st.Draft[GroupsSection])
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	found := false
	for _, g := range groups.Items {
		if g.ID == id {
			found = true
		}
	}
	if !found {
		return configlayer.DraftEvent{}, ErrGroupNotFound
	}
	rules, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		return configlayer.DraftEvent{}, err
	}

	refs := refsOfGroup(groups.Items, rules, id)
	if refs.any() && c.Mode == "" {
		imp, err := s.impactOf(st, ImpactGroup, id)
		if err != nil {
			return configlayer.DraftEvent{}, err
		}
		return configlayer.DraftEvent{}, &ReferencedError{Impact: imp}
	}
	if refs.final && c.Mode != CascadeRedirect {
		return configlayer.DraftEvent{}, invalid("mode", reasonFinalRequiresRedirect)
	}

	remaining := groupsWithout(groups.Items, id)
	var to Target
	if c.Mode == CascadeRedirect {
		if c.To == nil {
			return configlayer.DraftEvent{}, invalid("to", reasonTargetInvalid)
		}
		to = *c.To
		if err := validateTarget("to", &to, remaining); err != nil {
			return configlayer.DraftEvent{}, err
		}
	}

	// Состав ручных групп: удалённая группа уходит.
	for i := range remaining {
		g := &remaining[i]
		kept := make([]Member, 0, len(g.Members))
		for _, m := range g.Members {
			if m.Kind == KindGroup && m.ID == id {
				continue
			}
			kept = append(kept, m)
		}
		g.Members = kept
	}
	groups.Items = remaining
	rawGroups, err := encodeGroups(groups)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	sections := map[string]json.RawMessage{GroupsSection: rawGroups}

	// Правила и «Остальное» меняются, только если на группу есть ссылки: секция
	// правил без них не создаётся и не переписывается.
	if len(refs.rules) > 0 || refs.final {
		onGroup := func(t Target) bool { return t.Kind == KindGroup && t.ID == id }
		items := make([]Rule, 0, len(rules.Items))
		for _, r := range rules.Items {
			if onGroup(r.Target) {
				if c.Mode == CascadeDrop {
					continue
				}
				r.Target = to
			}
			items = append(items, r)
		}
		rules.Items = items
		if onGroup(rules.Final) {
			rules.Final = to
		}
		rawRules, err := encodeRules(rules)
		if err != nil {
			return configlayer.DraftEvent{}, err
		}
		sections[RulesSection] = rawRules
	}
	return s.d.Layer.EditDraftBatch(rev, sections, selectionDrop(id))
}
