package sources

import (
	"encoding/json"
	"log"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// maxViewSnapshots — сколько разобранных снимков держит кэш представления.
// Представление перечитывается по каждому событию sources, а чтение и разбор
// снимков с флеша MIPS дороги; кэш ограничен, чтобы не расходовать память.
const maxViewSnapshots = 16

// Состояния черновика источника относительно применённого (draft_state).
const (
	DraftApplied = "applied"
	DraftAdded   = "added"
	DraftChanged = "changed"
)

// KernelFlags — признак по каждому ядру (установлено, запущено).
type KernelFlags struct {
	Xray   bool `json:"xray"`
	Mihomo bool `json:"mihomo"`
}

// SourcesView — представление источников для API и интерфейса: только маски,
// полного URL подписки, HWID и учётных данных узлов в нём нет (D-33, D-34).
type SourcesView struct {
	DraftRevision int64       `json:"draft_revision"`
	Installed     KernelFlags `json:"installed"`
	Running       KernelFlags `json:"running"`
	// DefaultKernel — ядро просмотра по умолчанию: активное, иначе выбранное в
	// XKeen (его даёт ActiveKernel), иначе первое установленное; пусто — ядер нет.
	DefaultKernel string       `json:"default_kernel"`
	Sources       []SourceView `json:"sources"`
}

// MetaView — метаданные провайдера, видимые интерфейсу.
type MetaView struct {
	Title      string     `json:"title,omitempty"`
	Upload     int64      `json:"upload,omitempty"`
	Download   int64      `json:"download,omitempty"`
	Total      int64      `json:"total,omitempty"`
	Expire     int64      `json:"expire,omitempty"`
	IntervalH  int        `json:"interval_h,omitempty"`
	SupportURL string     `json:"support_url,omitempty"`
	WebURL     string     `json:"web_url,omitempty"`
	Announce   string     `json:"announce,omitempty"`
	HWIDLocked bool       `json:"hwid_locked,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

// SettingsView — настройки подписки. Сам HWID не выводится: только признак, что
// задан свой.
type SettingsView struct {
	Exclude    []string `json:"exclude"`
	Interval   Interval `json:"interval"`
	HWIDCustom bool     `json:"hwid_custom"`
}

// SourceView — один источник в представлении.
type SourceView struct {
	ID         string                     `json:"id"`
	Kind       string                     `json:"kind"`
	Name       string                     `json:"name"`
	Enabled    bool                       `json:"enabled"`
	DraftState string                     `json:"draft_state"`
	URLMasked  string                     `json:"url_masked,omitempty"`
	Host       string                     `json:"host,omitempty"`
	Meta       *MetaView                  `json:"meta,omitempty"`
	Settings   *SettingsView              `json:"settings,omitempty"`
	Kernels    map[string]KernelStateView `json:"kernels"`
	Nodes      []NodeView                 `json:"nodes"`
	// Stubs — заглушек провайдера (наибольшее число среди ядер); Skipped — прокси,
	// которые не стали узлами; Unsupported — узлы, которых не умеет ни одно ядро
	// (остаются в списке с причиной, D-23).
	Stubs       int `json:"stubs"`
	Skipped     int `json:"skipped"`
	Unsupported int `json:"unsupported"`
}

// StatusView — состояние источника по ядру (контракт Status с необязательным
// сроком повтора). Params несёт только безопасные значения.
type StatusView struct {
	Code      string            `json:"code"`
	ErrorCode string            `json:"error_code,omitempty"`
	HTTP      int               `json:"http,omitempty"`
	Since     time.Time         `json:"since"`
	RetryAt   *time.Time        `json:"retry_at,omitempty"`
	Attempts  int               `json:"attempts,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

// KernelStateView — состояние источника по одному установленному ядру.
type KernelStateView struct {
	// Count — узлов, которые ядро получит: набор ядра без исключённых и без
	// неподдерживаемых этим ядром.
	Count      int        `json:"count"`
	Status     StatusView `json:"status"`
	SnapshotAt *time.Time `json:"snapshot_at,omitempty"`
	Delta      *Delta     `json:"delta,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	// Pending — свежий набор ждёт «Применить».
	Pending bool `json:"pending"`
	// Stopped — ядро установлено, но не запущено.
	Stopped bool `json:"stopped"`
}

// VerdictView — вердикт матрицы возможностей для узла и ядра.
type VerdictView = nodes.Verdict

// SupportView — вердикты по обоим ядрам.
type SupportView struct {
	Xray   VerdictView `json:"xray"`
	Mihomo VerdictView `json:"mihomo"`
}

// NodeView — узел источника. CredMask — четыре символа учётных данных и «…»;
// полный текст узла отдаёт только NodeRaw по явному действию.
type NodeView struct {
	Key      string      `json:"key"`
	Ref      string      `json:"ref"`
	EntryID  string      `json:"entry_id,omitempty"`
	Name     string      `json:"name"`
	Protocol string      `json:"protocol"`
	Address  string      `json:"address"`
	CredMask string      `json:"cred_mask"`
	Support  SupportView `json:"support"`
	InSet    KernelFlags `json:"in_set"`
	Excluded bool        `json:"excluded"`
	// DupIn — подписки, в которых есть узел с той же идентичностью (только у узлов
	// «Моих узлов», D-14).
	DupIn []string `json:"dup_in,omitempty"`
}

// snapshotNodes читает набор узлов снимка через небольшой кэш по хэшу: снимок
// адресуется содержимым, поэтому записи кэша не устаревают. Возвращаемый срез
// только для чтения.
func (s *Service) snapshotNodes(hash string) ([]nodes.Node, error) {
	s.vmu.Lock()
	if list, ok := s.viewNodes[hash]; ok {
		s.vmu.Unlock()
		return list, nil
	}
	s.vmu.Unlock()

	list, err := s.blobs.get(hash)
	if err != nil {
		return nil, err
	}

	s.vmu.Lock()
	defer s.vmu.Unlock()
	if _, ok := s.viewNodes[hash]; !ok {
		s.viewOrder = append(s.viewOrder, hash)
		for len(s.viewOrder) > maxViewSnapshots {
			delete(s.viewNodes, s.viewOrder[0])
			s.viewOrder = s.viewOrder[1:]
		}
	}
	s.viewNodes[hash] = list
	return list, nil
}

// View строит представление источников по черновику, применённому состоянию и
// живым данным слоя.
func (s *Service) View() (SourcesView, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return SourcesView{}, err
	}
	live, err := readLive(st.Live)
	if err != nil {
		// Повреждённые живые данные не должны скрывать настройки источников.
		log.Printf("[sources] живые данные источников не читаются: представление без снимков")
		live = LiveSources{}
	}
	installed := s.d.Installed()
	caps := s.caps(installed)

	view := SourcesView{
		DraftRevision: st.DraftRevision,
		Installed:     KernelFlags{Xray: installed.Xray, Mihomo: installed.Mihomo},
		Sources:       []SourceView{},
	}
	view.Running = KernelFlags{
		Xray:   installed.Xray && s.d.Running(kernelXray),
		Mihomo: installed.Mihomo && s.d.Running(kernelMihomo),
	}
	view.DefaultKernel = s.defaultKernel(installed)

	type subItem struct {
		id  string
		cfg SubscriptionConfig
	}
	var subs []subItem
	var haveMy bool
	for name, raw := range st.Draft {
		id, ok := sourceID(name)
		if !ok {
			continue
		}
		switch kindOf(raw) {
		case KindSubscription:
			if id == MySourceID {
				continue
			}
			cfg, err := parseSubscription(raw)
			if err != nil {
				log.Printf("[sources] секция источника %s не разобрана", id)
				continue
			}
			cfg.ID = id
			subs = append(subs, subItem{id: id, cfg: cfg})
		case KindMy:
			haveMy = id == MySourceID
		}
	}
	// Новые подписки сверху; при равной дате — по идентификатору (порядок стабилен).
	sort.Slice(subs, func(i, j int) bool {
		if !subs[i].cfg.AddedAt.Equal(subs[j].cfg.AddedAt) {
			return subs[i].cfg.AddedAt.After(subs[j].cfg.AddedAt)
		}
		return subs[i].id < subs[j].id
	})

	// identNames — идентичность узла → имена подписок, где он есть (для dup_in).
	identNames := map[string][]string{}
	for _, it := range subs {
		sv := s.subViewOf(it.id, it.cfg, st.Draft[sectionName(it.id)], st.Applied, live[it.id], installed, caps)
		view.Sources = append(view.Sources, sv.view)
		for ident := range sv.idents {
			identNames[ident] = append(identNames[ident], sv.view.Name)
		}
	}
	for _, names := range identNames {
		sort.Strings(names)
	}
	if haveMy {
		cfg, err := parseMy(st.Draft[MySection])
		if err != nil {
			log.Printf("[sources] секция источника %s не разобрана", MySourceID)
		} else {
			view.Sources = append(view.Sources, s.myView(cfg, st.Draft[MySection], st.Applied, installed, caps, identNames))
		}
	}
	return view, nil
}

// defaultKernel — ядро просмотра по умолчанию (D-16): активное, если оно
// установлено, иначе первое установленное (Xray, затем Mihomo).
func (s *Service) defaultKernel(installed configlayer.InstalledKernels) string {
	if k := s.d.ActiveKernel(); (k == kernelXray || k == kernelMihomo) && installed.Has(k) {
		return k
	}
	for _, k := range previewKernels {
		if installed.Has(k) {
			return k
		}
	}
	return ""
}

// draftState сравнивает секцию черновика с применённой по значению: порядок ключей
// и пробелы в JSON значения не имеют.
func draftState(draft json.RawMessage, applied configlayer.Sections, name string) string {
	cur, ok := applied[name]
	if !ok {
		return DraftAdded
	}
	var a, b any
	if json.Unmarshal(draft, &a) != nil || json.Unmarshal(cur, &b) != nil {
		return DraftChanged
	}
	if reflect.DeepEqual(a, b) {
		return DraftApplied
	}
	return DraftChanged
}

// subView — представление подписки и идентичности её узлов (для dup_in).
type subView struct {
	view   SourceView
	idents map[string]bool
}

// displayRef — снимок, который показывается для ядра: ожидающий, иначе применённый.
func displayRef(kl *KernelLive) *Ref {
	if kl == nil {
		return nil
	}
	if kl.Pending != nil {
		return kl.Pending
	}
	return kl.Applied
}

// subViewOf строит представление одной подписки: наборы по установленным
// ядрам объединяются по ключу узла, у каждого узла — вхождение в наборы и
// вердикты по обоим ядрам.
func (s *Service) subViewOf(id string, cfg SubscriptionConfig, draft json.RawMessage, applied configlayer.Sections, src *LiveSource, installed configlayer.InstalledKernels, caps nodes.Caps) subView {
	v := SourceView{
		ID:         id,
		Kind:       KindSubscription,
		Enabled:    cfg.Enabled,
		DraftState: draftState(draft, applied, sectionName(id)),
		URLMasked:  nodes.MaskURL(cfg.URL),
		Kernels:    map[string]KernelStateView{},
		Nodes:      []NodeView{},
	}
	if u, err := url.Parse(strings.TrimSpace(cfg.URL)); err == nil {
		v.Host = u.Hostname()
	}
	exclude := cfg.Exclude
	if exclude == nil {
		exclude = []string{}
	}
	v.Settings = &SettingsView{Exclude: exclude, Interval: cfg.Interval, HWIDCustom: cfg.HWID != ""}
	if src != nil {
		v.Meta = metaView(src.Meta)
	}
	v.Name = cfg.Name
	if v.Name == "" && src != nil {
		v.Name = src.Meta.Title
	}
	if v.Name == "" {
		v.Name = v.Host
	}
	subs := excludeSubs(cfg.Exclude)

	// entry — узел объединённого списка.
	type entry struct {
		key  string
		node nodes.Node
		in   map[string]bool
	}
	byKey := map[string]*entry{}
	var order []*entry
	sets := map[string][]nodes.Keyed{}
	// Представители полных дублей с неисключённым именем по ядрам — как в
	// генераторе (keptByDup): каждое ядро строится из своего ответа.
	kepts := map[string]map[string]nodes.Node{}
	for _, kernel := range previewKernels {
		if !installed.Has(kernel) {
			continue
		}
		var kl *KernelLive
		if src != nil {
			kl = src.Kernels[kernel]
		}
		ref := displayRef(kl)
		if ref == nil {
			continue
		}
		if ref.Stubs > v.Stubs {
			v.Stubs = ref.Stubs
		}
		v.Skipped += ref.Skipped
		list, err := s.snapshotNodes(ref.Hash)
		if err != nil {
			// Нечитаемый снимок — пустой набор ядра, статус остаётся из живых данных.
			log.Printf("[sources] снимок источника %s для ядра %s не прочитан", id, kernel)
			continue
		}
		keyed, _ := nodes.Uniq(id, list)
		sets[kernel] = keyed
		kepts[kernel] = keptByDup(list, subs)
		for _, k := range keyed {
			e := byKey[k.Key]
			if e == nil {
				e = &entry{key: k.Key, node: k.Node, in: map[string]bool{}}
				byKey[k.Key] = e
				order = append(order, e)
			}
			e.in[kernel] = true
		}
	}

	// Идентификаторы — как у тега Xray в генераторе (nodeIDs): при совпадении
	// Hash8 ссылка группы получает тот же суффикс, что и outbound.
	all := make([]nodes.Keyed, 0, len(order))
	for _, e := range order {
		all = append(all, nodes.Keyed{Key: e.key, Node: e.node})
	}
	ids := nodeIDs(all)
	idents := make(map[string]bool, len(order))
	views := make([]NodeView, 0, len(order))
	for _, e := range order {
		// Узел исключён, только если исключены все его имена; иначе строка идёт под
		// неисключённым именем, как в файле ядра.
		node, excludedAll := e.node, false
		if len(subs) > 0 {
			found := false
			for _, kernel := range previewKernels {
				if !e.in[kernel] {
					continue
				}
				if n, ok := kepts[kernel][e.node.DupKey()]; ok && (!found || nodeBefore(n, node)) {
					node, found = n, true
				}
			}
			excludedAll = !found
		}
		nv := nodeViewOf(node, e.key, nodes.Ref(id, ids[e.key]), "", caps)
		nv.InSet = KernelFlags{Xray: e.in[kernelXray], Mihomo: e.in[kernelMihomo]}
		nv.Excluded = excludedAll
		if !nv.Excluded && !nv.Support.Xray.OK && !nv.Support.Mihomo.OK {
			v.Unsupported++
		}
		idents[e.node.Identity()] = true
		views = append(views, nv)
	}
	sortNodeViews(views)
	v.Nodes = views

	for _, kernel := range previewKernels {
		if !installed.Has(kernel) {
			continue
		}
		var kl *KernelLive
		if src != nil {
			kl = src.Kernels[kernel]
		}
		ks := kernelStateOf(kl, s.d.Running(kernel))
		for _, k := range sets[kernel] {
			n, ok := representative(k, kepts[kernel])
			if !ok {
				continue
			}
			if nodes.Support(n, kernel, caps).OK {
				ks.Count++
			}
		}
		v.Kernels[kernel] = ks
	}
	return subView{view: v, idents: idents}
}

// myView строит представление источника «Мои узлы»: набор у обоих ядер общий,
// различаются только вердикты матрицы. Запись, которая не даёт ровно один узел,
// считается пропущенной.
func (s *Service) myView(cfg MySourceConfig, draft json.RawMessage, applied configlayer.Sections, installed configlayer.InstalledKernels, caps nodes.Caps, identNames map[string][]string) SourceView {
	v := SourceView{
		ID:         MySourceID,
		Kind:       KindMy,
		Enabled:    cfg.Enabled,
		DraftState: draftState(draft, applied, MySection),
		Kernels:    map[string]KernelStateView{},
		Nodes:      []NodeView{},
	}
	views := make([]NodeView, 0, len(cfg.Entries))
	counts := map[string]int{}
	for _, e := range s.myNodes(cfg) {
		nv := nodeViewOf(e.node, e.key, nodes.Ref(MySourceID, e.entryID), e.entryID, caps)
		nv.Name = e.name
		nv.InSet = KernelFlags{Xray: installed.Xray, Mihomo: installed.Mihomo}
		nv.DupIn = identNames[e.node.Identity()]
		if !nv.Support.Xray.OK && !nv.Support.Mihomo.OK {
			v.Unsupported++
		}
		for _, kernel := range previewKernels {
			if installed.Has(kernel) && nodes.Support(e.node, kernel, caps).OK {
				counts[kernel]++
			}
		}
		views = append(views, nv)
	}
	v.Skipped = len(cfg.Entries) - len(views)
	sortNodeViews(views)
	v.Nodes = views
	for _, kernel := range previewKernels {
		if installed.Has(kernel) {
			ks := kernelStateOf(nil, s.d.Running(kernel))
			ks.Status = StatusView{Code: StatusOK}
			ks.Count = counts[kernel]
			v.Kernels[kernel] = ks
		}
	}
	return v
}

// nodeViewOf — общие поля узла: адрес, маска учётных данных, вердикты обоих ядер.
func nodeViewOf(n nodes.Node, key, ref, entryID string, caps nodes.Caps) NodeView {
	mask := ""
	if secrets := nodes.Secrets(n); len(secrets) > 0 {
		mask = nodes.MaskSecret(secrets[0])
	}
	return NodeView{
		Key:      key,
		Ref:      ref,
		EntryID:  entryID,
		Name:     displayName(n),
		Protocol: n.Protocol,
		Address:  net.JoinHostPort(n.Host, strconv.Itoa(n.Port)),
		CredMask: mask,
		Support: SupportView{
			Xray:   nodes.Support(n, kernelXray, caps),
			Mihomo: nodes.Support(n, kernelMihomo, caps),
		},
	}
}

// sortNodeViews упорядочивает узлы по имени без учёта регистра, затем по ключу:
// порядок провайдера в снимке не хранится.
func sortNodeViews(list []NodeView) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name)
		if a != b {
			return a < b
		}
		return list[i].Key < list[j].Key
	})
}

// kernelStateOf — состояние источника по ядру без счётчика; kl == nil — запроса
// под ядро ещё не было.
func kernelStateOf(kl *KernelLive, running bool) KernelStateView {
	ks := KernelStateView{Stopped: !running, Status: StatusView{Code: StatusNone}}
	if kl == nil {
		return ks
	}
	st := kl.Status
	ks.Status = StatusView{
		Code:      st.Code,
		ErrorCode: st.ErrorCode,
		HTTP:      st.HTTP,
		Since:     st.Since,
		Attempts:  st.Attempts,
		Params:    st.Params,
	}
	if !st.RetryAt.IsZero() {
		t := st.RetryAt
		ks.Status.RetryAt = &t
	}
	if ref := displayRef(kl); ref != nil && !ref.FetchedAt.IsZero() {
		t := ref.FetchedAt
		ks.SnapshotAt = &t
	}
	if kl.Delta.Added != 0 || kl.Delta.Removed != 0 {
		d := kl.Delta
		ks.Delta = &d
	}
	if !kl.UpdatedAt.IsZero() {
		t := kl.UpdatedAt
		ks.UpdatedAt = &t
	}
	ks.Pending = kl.Pending != nil
	return ks
}

// metaView переводит метаданные провайдера в представление; пустые — nil.
func metaView(m Meta) *MetaView {
	mv := &MetaView{
		Title: m.Title, Upload: m.Upload, Download: m.Download, Total: m.Total, Expire: m.Expire,
		IntervalH: m.IntervalH, SupportURL: m.SupportURL, WebURL: m.WebURL, Announce: m.Announce,
		HWIDLocked: m.HWIDLocked,
	}
	if !m.CheckedAt.IsZero() {
		t := m.CheckedAt
		mv.CheckedAt = &t
	}
	return mv
}
