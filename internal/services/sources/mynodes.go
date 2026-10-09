package sources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// NodeRawView — запись «Моих узлов» с исходным текстом без маски (режим
// «Изменить», D-13, D-34).
type NodeRawView struct {
	EntryID string `json:"entry_id"`
	Name    string `json:"name"`
	Format  string `json:"format"`
	Raw     string `json:"raw"`
}

// draftMy читает секцию src_my черновика вместе с ревизией.
func (s *Service) draftMy() (MySourceConfig, int64, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return MySourceConfig{}, 0, err
	}
	cfg, err := parseMy(st.Draft[MySection])
	if err != nil {
		return MySourceConfig{}, 0, err
	}
	return cfg, st.DraftRevision, nil
}

func findEntry(cfg MySourceConfig, id string) int {
	for i, e := range cfg.Entries {
		if e.ID == id {
			return i
		}
	}
	return -1
}

// singleNode распознаёт текст правки: ровно один узел, без URL и без ошибок
// строк, иначе edit_single_node (D-13). Исходные данные в ошибку не попадают.
func singleNode(rec nodes.Recognition) (nodes.Node, *nodes.SourceError) {
	if len(rec.Nodes) != 1 || len(rec.URLs) != 0 || len(rec.Errors) != 0 {
		return nodes.Node{}, nodes.NewError(nodes.CodeEditSingleNode)
	}
	return rec.Nodes[0], nil
}

// PreviewNode — предпросмотр правки узла по исходному тексту. entryID — правимая
// запись (пустой — новый узел): она не считается дублем самой себя. Токена нет:
// сохраняет SaveNode по тому же тексту. Текст, который не даёт ровно один узел,
// возвращается вместе с ошибкой *nodes.SourceError (edit_single_node) и
// предпросмотром, где видны причины по строкам.
func (s *Service) PreviewNode(ctx context.Context, text, entryID string) (PreviewView, error) {
	if err := ctx.Err(); err != nil {
		return PreviewView{}, err
	}
	cfg, _, err := s.draftMy()
	if err != nil {
		return PreviewView{}, err
	}
	if entryID != "" && findEntry(cfg, entryID) < 0 {
		return PreviewView{}, ErrNodeNotFound
	}
	rec := nodes.Recognize(nodes.Input{Text: text})
	view, addable := s.buildView(rec, s.dupIndex(cfg, entryID))
	view.Addable = addable
	if _, serr := singleNode(rec); serr != nil {
		return view, serr
	}
	return view, nil
}

// SaveNode сохраняет узел в черновик: entryID пустой — новая запись, иначе у
// записи заменяются имя, формат и исходный текст, а id остаётся (тег Xray
// xcp-my-<id> и место в группах не меняются при смене ключа узла). Пустое имя —
// имя узла из текста.
func (s *Service) SaveNode(rev int64, entryID, name, text string) (configlayer.DraftEvent, error) {
	n, serr := singleNode(nodes.Recognize(nodes.Input{Text: text}))
	if serr != nil {
		return configlayer.DraftEvent{}, serr
	}
	if n.Text == "" {
		return configlayer.DraftEvent{}, fmt.Errorf("%w: пустой текст узла", ErrInvalidInput)
	}
	cfg, cur, err := s.draftMy()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if cur != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	if name == "" {
		name = displayName(n)
	}
	if entryID == "" {
		taken := make(map[string]bool, len(cfg.Entries))
		for _, e := range cfg.Entries {
			taken[e.ID] = true
		}
		id, err := s.uniqueID(taken)
		if err != nil {
			return configlayer.DraftEvent{}, err
		}
		cfg.Entries = append(cfg.Entries, MyEntry{ID: id, Name: name, Format: n.Format, Raw: n.Text})
	} else {
		i := findEntry(cfg, entryID)
		if i < 0 {
			return configlayer.DraftEvent{}, ErrNodeNotFound
		}
		cfg.Entries[i] = MyEntry{ID: entryID, Name: name, Format: n.Format, Raw: n.Text}
	}
	return s.writeMy(rev, cfg)
}

// DeleteNode удаляет запись; секция src_my остаётся, даже если записей не стало.
func (s *Service) DeleteNode(rev int64, entryID string) (configlayer.DraftEvent, error) {
	cfg, cur, err := s.draftMy()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if cur != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	i := findEntry(cfg, entryID)
	if i < 0 {
		return configlayer.DraftEvent{}, ErrNodeNotFound
	}
	cfg.Entries = append(cfg.Entries[:i], cfg.Entries[i+1:]...)
	return s.writeMy(rev, cfg)
}

// NodeRaw возвращает исходный текст записи полностью, без маски (D-34).
func (s *Service) NodeRaw(entryID string) (NodeRawView, error) {
	cfg, _, err := s.draftMy()
	if err != nil {
		return NodeRawView{}, err
	}
	i := findEntry(cfg, entryID)
	if i < 0 {
		return NodeRawView{}, ErrNodeNotFound
	}
	e := cfg.Entries[i]
	return NodeRawView{EntryID: e.ID, Name: e.Name, Format: e.Format, Raw: e.Raw}, nil
}

// writeMy записывает секцию src_my в черновик.
func (s *Service) writeMy(rev int64, cfg MySourceConfig) (configlayer.DraftEvent, error) {
	section, err := encodeMy(cfg)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.d.Layer.EditDraftBatch(rev, map[string]json.RawMessage{MySection: section}, nil)
}
