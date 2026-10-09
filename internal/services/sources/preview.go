package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Пределы кэша токенов предпросмотра (T-145-19).
const (
	previewTTL        = 10 * time.Minute
	maxPreviewEntries = 8
)

// Метки узла в предпросмотре.
const (
	// MarkDupExists — такой узел уже есть в «Моих узлах»: он не добавляется (D-14).
	MarkDupExists = "dup_exists"
	// MarkDupInSub — узел есть в подписке (добавить можно); заполняет 145-08.
	MarkDupInSub = "dup_in_sub"
)

// KernelCounts — число узлов по ядрам.
type KernelCounts struct {
	Xray   int `json:"xray"`
	Mihomo int `json:"mihomo"`
}

// SupportPair — вердикты матрицы возможностей для обоих ядер.
type SupportPair struct {
	Xray   nodes.Verdict `json:"xray"`
	Mihomo nodes.Verdict `json:"mihomo"`
}

// PreviewNode — узел в предпросмотре. В нём только маска учётных данных и адрес:
// исходный текст отдаёт один NodeRaw по явному действию (T-145-18).
type PreviewNode struct {
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	Protocol   string      `json:"protocol"`
	Address    string      `json:"address"`
	CredMask   string      `json:"cred_mask"`
	Support    SupportPair `json:"support"`
	Marks      []string    `json:"marks"`
	DupSubName string      `json:"dup_sub_name,omitempty"`
}

// PreviewMyNodes — сводка по узлам, которые уйдут в «Мои узлы». Only — число
// узлов только для одного ядра; Skipped — узлы, которых не умеет ни одно ядро.
type PreviewMyNodes struct {
	Nodes   []PreviewNode `json:"nodes"`
	Count   int           `json:"count"`
	Only    KernelCounts  `json:"only"`
	Skipped int           `json:"skipped"`
}

// PreviewLineError — ошибка строки или документа ввода.
type PreviewLineError struct {
	Line     int                `json:"line"`
	File     string             `json:"file,omitempty"`
	Fragment string             `json:"fragment"`
	Error    *nodes.SourceError `json:"error"`
}

// PreviewSubscription — подписка в предпросмотре; поля объявляет 145-08.
type PreviewSubscription struct{}

// PreviewView — предпросмотр ввода поля «Добавить». Ключи JSON совпадают с
// контрактом frontend/src/lib/configSources.ts.
type PreviewView struct {
	Token         string                `json:"token"`
	ExpiresAt     time.Time             `json:"expires_at"`
	Subscriptions []PreviewSubscription `json:"subscriptions"`
	MyNodes       *PreviewMyNodes       `json:"my_nodes"`
	Errors        []PreviewLineError    `json:"errors"`
	Addable       int                   `json:"addable"`
	NodesOnly     bool                  `json:"nodes_only"`
	Collapsed     int                   `json:"collapsed"`
}

// AddResult — итог «Добавить»: событие черновика и число добавленного.
type AddResult struct {
	configlayer.DraftEvent
	Subscriptions int `json:"subscriptions"`
	Nodes         int `json:"nodes"`
}

// previewEntry — запомненный результат предпросмотра под токеном: URL и узлы не
// распознаются и не запрашиваются повторно при «Добавить».
type previewEntry struct {
	created time.Time
	nodes   []nodes.Node
	urls    []nodes.URLItem
}

// Preview распознаёт ввод и строит предпросмотр с одноразовым токеном.
func (s *Service) Preview(ctx context.Context, in nodes.Input) (PreviewView, error) {
	if err := ctx.Err(); err != nil {
		return PreviewView{}, err
	}
	rec := nodes.Recognize(in)
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return PreviewView{}, err
	}
	cfg, err := parseMy(st.Draft[MySection])
	if err != nil {
		return PreviewView{}, err
	}
	existing := s.dupIndex(cfg, "")
	view, addable := s.buildView(rec, existing)
	view.Addable = addable
	if addable > 0 || len(rec.URLs) > 0 {
		token, expires, err := s.putPreview(&previewEntry{nodes: rec.Nodes, urls: rec.URLs})
		if err != nil {
			return PreviewView{}, err
		}
		view.Token, view.ExpiresAt = token, expires
	}
	return view, nil
}

// buildView строит представление по результату распознавания; existing — уже
// имеющиеся в «Моих узлах» узлы (DupKey → id записи). Возвращает также число
// добавляемых узлов.
func (s *Service) buildView(rec nodes.Recognition, existing map[string]string) (PreviewView, int) {
	view := PreviewView{
		Subscriptions: []PreviewSubscription{},
		Errors:        make([]PreviewLineError, 0, len(rec.Errors)),
		NodesOnly:     rec.NodesOnly,
		Collapsed:     rec.Collapsed,
	}
	for _, le := range rec.Errors {
		view.Errors = append(view.Errors, PreviewLineError{Line: le.Line, File: le.File, Fragment: le.Fragment, Error: le.Err})
	}
	if len(rec.Nodes) == 0 {
		return view, 0
	}
	caps := s.caps(s.d.Installed())
	my := &PreviewMyNodes{Nodes: make([]PreviewNode, 0, len(rec.Nodes))}
	addable := 0
	for _, n := range rec.Nodes {
		pn := previewNode(n, caps)
		if _, dup := existing[n.DupKey()]; dup {
			pn.Marks = append(pn.Marks, MarkDupExists)
		} else {
			addable++
		}
		my.Nodes = append(my.Nodes, pn)
		x, m := pn.Support.Xray.OK, pn.Support.Mihomo.OK
		switch {
		case x && !m:
			my.Only.Xray++
		case m && !x:
			my.Only.Mihomo++
		case !x && !m:
			my.Skipped++
		}
	}
	my.Count = len(my.Nodes)
	view.MyNodes = my
	return view, addable
}

// previewNode — представление узла: адрес, маска секрета и вердикты ядер.
func previewNode(n nodes.Node, caps nodes.Caps) PreviewNode {
	mask := ""
	if secrets := nodes.Secrets(n); len(secrets) > 0 {
		mask = nodes.MaskSecret(secrets[0])
	}
	return PreviewNode{
		Key:      n.Key(MySourceID),
		Name:     n.Name,
		Protocol: n.Protocol,
		Address:  net.JoinHostPort(n.Host, strconv.Itoa(n.Port)),
		CredMask: mask,
		Support: SupportPair{
			Xray:   nodes.Support(n, kernelXray, caps),
			Mihomo: nodes.Support(n, kernelMihomo, caps),
		},
		Marks: []string{},
	}
}

// dupIndex — узлы записей «Моих узлов»: DupKey → id записи. Запись с id except
// (правимая) в индекс не входит.
func (s *Service) dupIndex(cfg MySourceConfig, except string) map[string]string {
	idx := make(map[string]string, len(cfg.Entries))
	for _, e := range cfg.Entries {
		if e.ID == except {
			continue
		}
		n, serr := s.parseRaw(e.Raw)
		if serr != nil {
			continue
		}
		idx[n.DupKey()] = e.ID
	}
	return idx
}

// putPreview запоминает предпросмотр под новым токеном (128 бит из источника
// случайности), вытесняя старейший сверх предела.
func (s *Service) putPreview(e *previewEntry) (string, time.Time, error) {
	var b [16]byte
	randMu.Lock()
	_, err := io.ReadFull(s.d.Rand, b[:])
	randMu.Unlock()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sources: случайность недоступна: %w", err)
	}
	token := hex.EncodeToString(b[:])
	now := s.d.Now()
	e.created = now

	s.pmu.Lock()
	defer s.pmu.Unlock()
	s.pruneLocked(now)
	s.previews[token] = e
	s.order = append(s.order, token)
	for len(s.order) > maxPreviewEntries {
		delete(s.previews, s.order[0])
		s.order = s.order[1:]
	}
	return token, now.Add(previewTTL), nil
}

// pruneLocked убирает просроченные токены.
func (s *Service) pruneLocked(now time.Time) {
	kept := s.order[:0]
	for _, t := range s.order {
		e, ok := s.previews[t]
		if !ok {
			continue
		}
		if now.Sub(e.created) > previewTTL {
			delete(s.previews, t)
			continue
		}
		kept = append(kept, t)
	}
	s.order = kept
}

// claimPreview забирает предпросмотр: токен одноразовый. Неизвестный или
// просроченный — ErrPreviewExpired.
func (s *Service) claimPreview(token string) (*previewEntry, error) {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	e, ok := s.previews[token]
	if !ok {
		return nil, ErrPreviewExpired
	}
	delete(s.previews, token)
	for i, t := range s.order {
		if t == token {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	if s.d.Now().Sub(e.created) > previewTTL {
		return nil, ErrPreviewExpired
	}
	return e, nil
}

// restorePreview возвращает забранный предпросмотр: «Добавить» не удалось
// (конфликт ревизии), токен остаётся действительным.
func (s *Service) restorePreview(token string, e *previewEntry) {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	s.previews[token] = e
	s.order = append(s.order, token)
	for len(s.order) > maxPreviewEntries {
		delete(s.previews, s.order[0])
		s.order = s.order[1:]
	}
}

// Add кладёт результат предпросмотра в черновик: узлы — в секцию src_my. rev —
// ревизия черновика, на которой клиент строил вид; при расхождении
// configlayer.ErrDraftConflict, токен остаётся действительным.
func (s *Service) Add(rev int64, token string) (AddResult, error) {
	entry, err := s.claimPreview(token)
	if err != nil {
		return AddResult{}, err
	}
	res, err := s.addEntry(rev, entry)
	if err != nil {
		s.restorePreview(token, entry)
		return AddResult{}, err
	}
	return res, nil
}

func (s *Service) addEntry(rev int64, entry *previewEntry) (AddResult, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return AddResult{}, err
	}
	if st.DraftRevision != rev {
		return AddResult{}, configlayer.ErrDraftConflict
	}
	cfg, err := parseMy(st.Draft[MySection])
	if err != nil {
		return AddResult{}, err
	}
	existing := s.dupIndex(cfg, "")
	taken := make(map[string]bool, len(cfg.Entries))
	for _, e := range cfg.Entries {
		taken[e.ID] = true
	}
	added := 0
	for _, n := range entry.nodes {
		if _, dup := existing[n.DupKey()]; dup || n.Text == "" {
			continue
		}
		id, err := s.uniqueID(taken)
		if err != nil {
			return AddResult{}, err
		}
		taken[id] = true
		existing[n.DupKey()] = id
		cfg.Entries = append(cfg.Entries, MyEntry{ID: id, Name: displayName(n), Format: n.Format, Raw: n.Text})
		added++
	}
	if added == 0 {
		return AddResult{}, fmt.Errorf("%w: нечего добавлять", ErrInvalidInput)
	}
	section, err := encodeMy(cfg)
	if err != nil {
		return AddResult{}, err
	}
	ev, err := s.d.Layer.EditDraftBatch(rev, map[string]json.RawMessage{MySection: section}, nil)
	if err != nil {
		return AddResult{}, err
	}
	return AddResult{DraftEvent: ev, Nodes: added}, nil
}
