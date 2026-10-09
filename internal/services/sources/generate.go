package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"regexp"
	"sort"
	"strconv"

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

	var out []configlayer.GeneratedFile
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
		default:
			// Подписки материализует 145-08; чужие виды секций пропускаются.
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
	raw, ok := in.Live["sources"]
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
	node    nodes.Node
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
			name = displayName(n)
		}
		list = append(list, keyedNode{entryID: e.ID, name: name, key: n.Key(MySourceID), node: n})
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
		ob, err := nodes.XrayOutbound(k.node, "xcp-"+id+"-"+k.entryID)
		if err != nil {
			// Вердикт OK означает, что пробная эмиссия прошла; сюда не доходим.
			log.Printf("[sources] узел %s не записан для Xray", k.entryID)
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
			log.Printf("[sources] узел %s не записан для Mihomo", k.entryID)
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
