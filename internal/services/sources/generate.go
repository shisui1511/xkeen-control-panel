package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Идентификаторы генераторов в реестре слоя.
const (
	xrayGenID   = "sources_xray"
	mihomoGenID = "sources_mihomo"
)

// maxGenMemo — предел мемоизации результатов генераторов.
const maxGenMemo = 64

// entryIDRE — допустимый id записи «Моих узлов»: из него строится тег Xray.
var entryIDRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// memoFile — запомненный результат источника для одного ядра; nil — файла нет.
type memoFile struct {
	file *configlayer.GeneratedFile
}

// xrayGen — генератор файлов Xray: 04_outbounds.xcp-<id>.tail.json по источнику.
type xrayGen struct{ s *Service }

// ID — идентификатор генератора в реестре.
func (xrayGen) ID() string { return xrayGenID }

// Generate выдаёт файл outbounds каждого включённого источника.
func (g xrayGen) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	return g.s.generate(in, installed, kernelXray)
}

// mihomoGen — генератор файлов Mihomo: proxy_providers/xcp-<id>.yaml по источнику.
type mihomoGen struct{ s *Service }

// ID — идентификатор генератора в реестре.
func (mihomoGen) ID() string { return mihomoGenID }

// Generate выдаёт файл провайдера каждого включённого источника.
func (g mihomoGen) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	return g.s.generate(in, installed, kernelMihomo)
}

// generate собирает файлы источников для одного ядра. Пути строятся только из
// идентификаторов «my» и 8 hex, поэтому файлы всегда имеют вид xcp-<id> в
// каталогах панели; чужой config.yaml не трогается (D-03, ISOL-05). Узел
// попадает в файл ядра, только если матрица возможностей его пропускает; один
// и тот же источник питает оба ядра (SRC-02, SRC-03).
func (s *Service) generate(in configlayer.Input, installed configlayer.InstalledKernels, kernel string) ([]configlayer.GeneratedFile, error) {
	if !installed.Has(kernel) {
		return nil, nil
	}
	caps := s.caps(installed)
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(in.Sections))
	for name := range in.Sections {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		out    []configlayer.GeneratedFile
		live   LiveSources
		cands  Candidates
		loaded bool
	)
	for _, name := range names {
		id, ok := sourceID(name)
		if !ok {
			continue
		}
		raw := in.Sections[name]
		var file *configlayer.GeneratedFile
		switch kindOf(raw) {
		case KindMy:
			if id != MySourceID {
				continue
			}
			key := hash(kernel, id, string(raw), string(capsJSON), string(liveOf(in, id)))
			if m, ok := s.memoGet(key); ok {
				file = m.file
				break
			}
			file, err = s.myFile(kernel, id, raw, caps)
			if err != nil {
				return nil, err
			}
			s.memoPut(key, memoFile{file: file})
		case KindSubscription:
			if !loaded {
				if live, err = readLive(in.Live); err != nil {
					return nil, err
				}
				if cands, err = readCandidates(in.Live, in.Sections); err != nil {
					return nil, err
				}
				loaded = true
			}
			file, err = s.subscriptionFile(in.Mode, kernel, id, raw, live, cands, caps, string(capsJSON))
			if err != nil {
				return nil, err
			}
		default:
			// Чужие виды секций пропускаются.
			continue
		}
		if file != nil {
			out = append(out, *file)
		}
	}
	return out, nil
}

// liveOf — живые данные источника из Live["sources"][id] (оверлей и снимки
// провайдера); отсутствие — nil. Входят в ключ мемоизации.
func liveOf(in configlayer.Input, id string) json.RawMessage {
	raw, ok := in.Live[LiveKeySources]
	if !ok {
		return nil
	}
	var byID map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byID); err != nil {
		return nil
	}
	return byID[id]
}

// keyedNode — узел записи с ключом для упорядочивания.
type keyedNode struct {
	entryID string
	name    string
	key     string
	// tag — тег Xray: xcp-<источник>-<id записи> у «Моих узлов», xcp-<источник>-<8 hex
	// от ключа> у подписок; не зависит от имени узла.
	tag  string
	node nodes.Node
}

// myNodes разбирает записи «Моих узлов» в упорядоченный список. Запись, которая
// не даёт ровно один узел, и запись с повреждённым id пропускаются: причина
// (только код) идёт в журнал один раз на разбор, секция остаётся как есть.
// Порядок — по ключу узла, затем по id записи: перестановка записей в секции
// не меняет файлы.
func (s *Service) myNodes(cfg MySourceConfig) []keyedNode {
	seen := make(map[string]bool, len(cfg.Entries))
	list := make([]keyedNode, 0, len(cfg.Entries))
	for _, e := range cfg.Entries {
		if !entryIDRE.MatchString(e.ID) || seen[e.ID] {
			log.Printf("[sources] запись источника %s пропущена: недопустимый или повторный id", MySourceID)
			continue
		}
		seen[e.ID] = true
		n, serr := s.parseRaw(e.Raw)
		if serr != nil {
			log.Printf("[sources] запись %s источника %s пропущена: %s", e.ID, MySourceID, serr.Code)
			continue
		}
		name := e.Name
		if name == "" {
			name = entryName(n)
		}
		list = append(list, keyedNode{entryID: e.ID, name: name, key: n.Key(MySourceID), tag: "xcp-" + MySourceID + "-" + e.ID, node: n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].key != list[j].key {
			return list[i].key < list[j].key
		}
		return list[i].entryID < list[j].entryID
	})
	return list
}

// myFile строит файл источника «Мои узлы» для ядра; nil — файла нет (источник
// выключен, узлов для ядра нет).
func (s *Service) myFile(kernel, id string, raw json.RawMessage, caps nodes.Caps) (*configlayer.GeneratedFile, error) {
	cfg, err := parseMy(raw)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	list := s.myNodes(cfg)
	switch kernel {
	case kernelXray:
		return xrayFile(id, list, caps)
	case kernelMihomo:
		return mihomoFile(id, list, caps)
	}
	return nil, nil
}

// xrayFile — outbounds узлов, которые умеет Xray; тег xcp-my-<id записи> не
// зависит от текста узла (D-22).
func xrayFile(id string, list []keyedNode, caps nodes.Caps) (*configlayer.GeneratedFile, error) {
	outbounds := make([]map[string]any, 0, len(list))
	for _, k := range list {
		if !nodes.Support(k.node, kernelXray, caps).OK {
			continue
		}
		ob, err := nodes.XrayOutbound(k.node, k.tag)
		if err != nil {
			// Вердикт OK означает, что пробная эмиссия прошла; сюда не доходим.
			log.Printf("[sources] узел %s источника %s не записан для Xray", k.tag, id)
			continue
		}
		outbounds = append(outbounds, ob)
	}
	if len(outbounds) == 0 {
		return nil, nil
	}
	content, err := nodes.XrayFile(outbounds)
	if err != nil {
		return nil, err
	}
	return &configlayer.GeneratedFile{
		Kernel:  kernelXray,
		RelPath: "04_outbounds.xcp-" + id + ".tail.json",
		Kind:    configlayer.KindXrayJSON,
		Content: content,
	}, nil
}

// mihomoFile — провайдер Mihomo из узлов, которые умеет Mihomo. Файл Inert и без
// Expect: пока провайдер не подключён к config.yaml (Phase 146), перезапускать
// ядро и сверять число узлов в рантайме нечем.
func mihomoFile(id string, list []keyedNode, caps nodes.Caps) (*configlayer.GeneratedFile, error) {
	used := make(map[string]bool, len(list))
	proxies := make([]map[string]any, 0, len(list))
	for _, k := range list {
		if !nodes.Support(k.node, kernelMihomo, caps).OK {
			continue
		}
		name := uniqueName(k.name, used)
		p, err := nodes.ClashProxy(k.node, name)
		if err != nil {
			log.Printf("[sources] узел %s источника %s не записан для Mihomo", k.tag, id)
			continue
		}
		proxies = append(proxies, p)
	}
	if len(proxies) == 0 {
		return nil, nil
	}
	content, err := nodes.ClashFile(proxies)
	if err != nil {
		return nil, err
	}
	return &configlayer.GeneratedFile{
		Kernel:  kernelMihomo,
		RelPath: "proxy_providers/xcp-" + id + ".yaml",
		Kind:    configlayer.KindMihomoProxyProvider,
		Content: content,
		Inert:   true,
	}, nil
}

// displayName — имя узла; пустое заменяется адресом host:port.
func displayName(n nodes.Node) string {
	if n.Name != "" {
		return n.Name
	}
	return net.JoinHostPort(n.Host, strconv.Itoa(n.Port))
}

// uniqueName даёт имя, которого нет среди занятых: дубли получают суффикс « 2»,
// « 3» в порядке обхода (D-22).
func uniqueName(base string, used map[string]bool) string {
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; ; i++ {
		c := base + " " + strconv.Itoa(i)
		if !used[c] {
			used[c] = true
			return c
		}
	}
}

// hash — sha256 от частей, разделённых нулевым байтом.
func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Service) memoGet(key string) (memoFile, bool) {
	s.mmu.Lock()
	defer s.mmu.Unlock()
	m, ok := s.gens[key]
	return m, ok
}

func (s *Service) memoPut(key string, m memoFile) {
	s.mmu.Lock()
	defer s.mmu.Unlock()
	if _, exists := s.gens[key]; !exists {
		s.genOrder = append(s.genOrder, key)
		for len(s.genOrder) > maxGenMemo {
			delete(s.gens, s.genOrder[0])
			s.genOrder = s.genOrder[1:]
		}
	}
	s.gens[key] = m
}

// parseSubscription разбирает секцию src_<id> подписки.
func parseSubscription(raw json.RawMessage) (SubscriptionConfig, error) {
	var cfg SubscriptionConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return SubscriptionConfig{}, fmt.Errorf("%w: секция подписки: %v", ErrInvalidInput, err)
	}
	cfg.Kind = KindSubscription
	return cfg, nil
}

// refFor выбирает снимок набора подписки для ядра. Черновик (Mode SourceDraft):
// ожидающий снимок, иначе применённый. Применённое состояние (Mode SourceApplied):
// кандидат запуска (оверлей), иначе применённый. nil — для ядра набора нет, файла
// не будет.
func refFor(mode configlayer.SourceKind, kl *KernelLive, cand *Ref) *Ref {
	if mode == configlayer.SourceApplied {
		if cand != nil {
			return cand
		}
		if kl != nil {
			return kl.Applied
		}
		return nil
	}
	if kl == nil {
		return nil
	}
	if kl.Pending != nil {
		return kl.Pending
	}
	return kl.Applied
}

// subscriptionFile строит файл подписки для ядра из её снимка. Источник без снимка
// для ядра файла не даёт; снимок есть, а прочитать его нельзя — ошибка генератора,
// а не пустой набор: пустой набор удалил бы рабочие файлы ядра (Pitfall 7, T-145-23).
func (s *Service) subscriptionFile(mode configlayer.SourceKind, kernel, id string, raw json.RawMessage, live LiveSources, cands Candidates, caps nodes.Caps, capsJSON string) (*configlayer.GeneratedFile, error) {
	cfg, err := parseSubscription(raw)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	var kl *KernelLive
	if src := live[id]; src != nil {
		kl = src.Kernels[kernel]
	}
	var cand *Ref
	if c, ok := cands[id][kernel]; ok {
		cand = &c
	}
	ref := refFor(mode, kl, cand)
	if ref == nil {
		return nil, nil
	}
	key := hash(kernel, id, string(raw), capsJSON, ref.Hash)
	if m, ok := s.memoGet(key); ok {
		return m.file, nil
	}
	list, err := s.blobs.get(ref.Hash)
	if err != nil {
		return nil, fmt.Errorf("источник %s, ядро %s: %w", id, kernel, err)
	}
	keyed := s.subscriptionNodes(id, list, cfg.Exclude)
	var file *configlayer.GeneratedFile
	switch kernel {
	case kernelXray:
		file, err = xrayFile(id, keyed, caps)
	case kernelMihomo:
		file, err = mihomoFile(id, keyed, caps)
	}
	if err != nil {
		return nil, err
	}
	s.memoPut(key, memoFile{file: file})
	return file, nil
}

// excludeSubs приводит шаблоны исключения к подстрокам: нижний регистр, без
// пробелов по краям, пустые отброшены. Исключение по подстроке имени без учёта
// регистра — единственный фильтр источника (D-04); действует для обоих ядер,
// «Мои узлы» им не пользуются.
func excludeSubs(patterns []string) []string {
	var subs []string
	for _, p := range patterns {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			subs = append(subs, p)
		}
	}
	return subs
}

// nameExcluded — имя узла содержит любую из подстрок (subs из excludeSubs).
func nameExcluded(n nodes.Node, subs []string) bool {
	name := strings.ToLower(displayName(n))
	for _, p := range subs {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

// subscriptionNodes присваивает узлам снимка ключи: полные дубли схлопываются,
// порядок — по ключу. Тег Xray — от ключа, поэтому не меняется при переименовании
// узла у провайдера (Pitfall 4). Ключи и идентификаторы (nodeIDs) считаются от
// полного снимка, исключение по имени (D-04) действует после: так ключ и тег
// узла совпадают с ref представления, а исключение одного узла не меняет тег
// другого. Полный дубль под несколькими именами остаётся, если хотя бы одно имя
// не исключено, и идёт под ним (keptByDup).
func (s *Service) subscriptionNodes(id string, list []nodes.Node, exclude []string) []keyedNode {
	uniq, _ := nodes.Uniq(id, list)
	ids := nodeIDs(uniq)
	kept := keptByDup(list, excludeSubs(exclude))
	out := make([]keyedNode, 0, len(uniq))
	for _, k := range uniq {
		n, ok := representative(k, kept)
		if !ok {
			continue
		}
		out = append(out, keyedNode{
			name: displayName(n),
			key:  k.Key,
			tag:  "xcp-" + id + "-" + ids[k.Key],
			node: n,
		})
	}
	return out
}

// keptByDup — представители полных дублей среди узлов, чьё имя не исключено:
// DupKey → узел с наименьшим именем, затем текстом (тот же порядок, что у
// nodes.Uniq). Узел, повторённый провайдером под двумя именами, остаётся, если
// хотя бы одно имя не подпадает под исключение (D-04), а результат не зависит от
// порядка имён. nil — исключений нет.
func keptByDup(list []nodes.Node, subs []string) map[string]nodes.Node {
	if len(subs) == 0 {
		return nil
	}
	out := map[string]nodes.Node{}
	for _, n := range list {
		if nameExcluded(n, subs) {
			continue
		}
		dk := n.DupKey()
		if cur, ok := out[dk]; !ok || nodeBefore(n, cur) {
			out[dk] = n
		}
	}
	return out
}

// nodeBefore — порядок выбора представителя полных дублей: имя, затем текст.
func nodeBefore(a, b nodes.Node) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Text < b.Text
}

// representative — узел ключа с учётом исключения по имени (kept из keptByDup);
// false — все имена полного дубля исключены. Без исключений — узел Uniq.
func representative(k nodes.Keyed, kept map[string]nodes.Node) (nodes.Node, bool) {
	if kept == nil {
		return k.Node, true
	}
	n, ok := kept[k.Node.DupKey()]
	return n, ok
}

// nodeIDs — идентификаторы узлов источника по ключу для тега Xray и ref групп:
// 8 hex от sha256 ключа (nodes.Hash8). Это 32 бита: совпадение у двух разных
// узлов дало бы повторяющийся тег outbound, и xray -test отверг бы весь файл.
// Совпавший идентификатор получает суффикс «-2», «-3» в порядке ключей, поэтому
// остаётся стабильным при том же наборе.
func nodeIDs(keyed []nodes.Keyed) map[string]string {
	keys := make([]string, 0, len(keyed))
	for _, k := range keyed {
		keys = append(keys, k.Key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	used := make(map[string]bool, len(keys))
	for _, key := range keys {
		if _, ok := out[key]; ok {
			continue
		}
		id8 := nodes.Hash8(key)
		if used[id8] {
			base := id8
			for i := 2; used[id8]; i++ {
				id8 = base + "-" + strconv.Itoa(i)
			}
		}
		used[id8] = true
		out[key] = id8
	}
	return out
}
