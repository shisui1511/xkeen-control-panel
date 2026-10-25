package routing

import (
	"encoding/json"
	"errors"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
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

// DeleteGroup удаляет группу из черновика одной ревизией (D-34). Живой выбор
// (Live[selection]) не трогается: пока удаление не применено, группа в ядре
// остаётся, а «Сбросить черновик» вернул бы группу без выбора. Выбор удалённой
// группы и выбор её как участника убирает CommitLive после «Применить». Есть
// ссылки (правила, «Остальное», участие в ручных группах) и действие не выбрано — *ReferencedError без изменений. redirect
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

	// Состав ручных групп: удалённая группа уходит (и из участников).
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
	return s.d.Layer.EditDraftBatch(rev, sections, nil)
}

// scrubGroups убирает из групп ссылки на удаляемый источник или узел: источник
// из sources.ids, отдельные узлы из picks, резервный узел. Меняется только то,
// что ссылалось; changed сообщает, правились ли группы.
func scrubGroups(groups []Group, sc sourceScope) ([]Group, bool) {
	out := make([]Group, len(groups))
	changed := false
	for i, g := range groups {
		if sc.source != "" {
			ids := make([]string, 0, len(g.Sources.IDs))
			for _, id := range g.Sources.IDs {
				if id != sc.source {
					ids = append(ids, id)
				}
			}
			if len(ids) != len(g.Sources.IDs) {
				g.Sources.IDs, changed = ids, true
			}
		}
		picks := make([]string, 0, len(g.Picks))
		for _, ref := range g.Picks {
			if !sc.hasRef(ref) {
				picks = append(picks, ref)
			}
		}
		if len(picks) != len(g.Picks) {
			g.Picks, changed = picks, true
		}
		if g.Reserve != "" && sc.hasRef(g.Reserve) {
			g.Reserve, changed = "", true
		}
		out[i] = g
	}
	return out, changed
}

// scrubSections возвращает секцию groups без ссылок на удаляемое (nil — править
// нечего). Испорченная секция групп удалению не мешает: о ней сообщает проверка.
func scrubSections(st configlayer.StateView, sc sourceScope) (map[string]json.RawMessage, error) {
	cfg, err := parseGroups(st.Draft[GroupsSection])
	if err != nil {
		return nil, nil
	}
	items, changed := scrubGroups(cfg.Items, sc)
	if !changed {
		return nil, nil
	}
	cfg.Items = items
	raw, err := encodeGroups(cfg)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{GroupsSection: raw}, nil
}

// DeleteSource удаляет подписку из черновика и той же ревизией убирает ссылки на
// неё и на её узлы из групп (sources.ids, picks, резерв, D-35). Удаление
// разрешено, даже если группа станет пустой: «Применить» заблокирует проверка
// group_empty. Выбор в Live, ссылающийся на узел источника, остаётся до
// «Применить»: генератор выберет участника по умолчанию.
func (s *Service) DeleteSource(rev int64, id string) (configlayer.DraftEvent, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	extra, err := scrubSections(st, sourceScope{source: id})
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.d.Sources.DeleteWith(rev, id, extra)
}

// DeleteNode удаляет запись «Моих узлов» и той же ревизией убирает ссылки на её
// узел (picks, резерв) из групп (D-35).
func (s *Service) DeleteNode(rev int64, entryID string) (configlayer.DraftEvent, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	extra, err := scrubSections(st, sourceScope{ref: nodes.Ref(sources.MySourceID, entryID)})
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.d.Sources.DeleteNodeWith(rev, entryID, extra)
}
