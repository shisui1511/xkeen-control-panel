package sources

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// CatalogNode — узел источника в том виде, в каком он записан в файл ядра:
// группы и предпросмотр берут состав, имена Mihomo и теги Xray только отсюда
// (D-11), второго расчёта узлов нет. Тип внутренний: Node несёт секреты узла и
// наружу в ответах API не уходит, только маскированные представления.
type CatalogNode struct {
	// Source — идентификатор источника («my» или 8 hex).
	Source string
	// SourceName — имя подписки; у «Моих узлов» пусто (подпись подставляет
	// интерфейс).
	SourceName string
	// Ref — ссылка на узел для групп: <источник>/<8 hex> (nodes.Ref).
	Ref string
	// Key — ключ узла в источнике.
	Key string
	// Display — имя узла для показа; пустое имя заменено адресом host:port.
	Display string
	// MihomoName — имя прокси в файле провайдера Mihomo (после uniqueName);
	// заполнено только в списке ядра Mihomo.
	MihomoName string
	// XrayTag — тег outbound в файле Xray; заполнен только в списке ядра Xray.
	XrayTag string
	// Node — разобранный узел.
	Node nodes.Node
}

// CatalogSource — источник, участвующий в каталоге.
type CatalogSource struct {
	ID   string
	Name string
	Kind string
	// Enabled — выключенный источник файлов не даёт, его узлов в каталоге нет.
	Enabled bool
}

// Catalog — узлы по установленным ядрам. Порядок узлов: источник (по имени
// секции), затем порядок записи в файл ядра. Значение только для чтения: его
// разделяют вызовы через мемоизацию.
type Catalog struct {
	Sources []CatalogSource
	Xray    []CatalogNode
	Mihomo  []CatalogNode
}

// kernelList собирает упорядоченный список узлов одного источника для ядра:
// узлы, которые ядро не умеет, отброшены (nodes.Support), имена Mihomo получают
// суффиксы uniqueName в порядке обхода, теги Xray берутся от ключа узла. Это
// единственное место, где решается, что и под каким именем попадёт в файл:
// xrayFile, mihomoFile и Service.Catalog читают его результат.
func kernelList(kernel, id, srcName string, list []keyedNode, caps nodes.Caps) []CatalogNode {
	used := make(map[string]bool, len(list))
	out := make([]CatalogNode, 0, len(list))
	for _, k := range list {
		if !nodes.Support(k.node, kernel, caps).OK {
			continue
		}
		cn := CatalogNode{
			Source:     id,
			SourceName: srcName,
			Ref:        nodes.Ref(id, k.id8),
			Key:        k.key,
			Display:    k.name,
			Node:       k.node,
		}
		switch kernel {
		case kernelXray:
			cn.XrayTag = k.tag
		case kernelMihomo:
			cn.MihomoName = uniqueName(k.name, used)
		}
		out = append(out, cn)
	}
	return out
}

// kernelNodes — узлы источника для ядра из сырой секции и живых данных: те же
// шаги, что у файла источника (разбор секции, выбор снимка подписки, ключи,
// исключения). Выключенный источник и подписка без снимка для ядра дают пустой
// список. Снимок есть, а прочитать его нельзя — ошибка, как у генератора.
func (s *Service) kernelNodes(mode configlayer.SourceKind, kernel, id string, raw json.RawMessage, live LiveSources, cands Candidates, caps nodes.Caps) ([]CatalogNode, error) {
	switch kindOf(raw) {
	case KindMy:
		if id != MySourceID {
			return nil, nil
		}
		cfg, err := parseMy(raw)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			return nil, nil
		}
		return kernelList(kernel, id, "", s.myNodes(cfg), caps), nil
	case KindSubscription:
		cfg, err := parseSubscription(raw)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			return nil, nil
		}
		ref := subscriptionRef(mode, kernel, id, live, cands)
		if ref == nil {
			return nil, nil
		}
		list, err := s.blobs.get(ref.Hash)
		if err != nil {
			return nil, fmt.Errorf("источник %s, ядро %s: %w", id, kernel, err)
		}
		return kernelList(kernel, id, cfg.Name, s.subscriptionNodes(id, list, cfg.Exclude), caps), nil
	}
	return nil, nil
}

// subscriptionRef — снимок подписки для ядра (nil — набора для ядра нет).
func subscriptionRef(mode configlayer.SourceKind, kernel, id string, live LiveSources, cands Candidates) *Ref {
	var kl *KernelLive
	if src := live[id]; src != nil {
		kl = src.Kernels[kernel]
	}
	var cand *Ref
	if c, ok := cands[id][kernel]; ok {
		cand = &c
	}
	return refFor(mode, kl, cand)
}

// Catalog отдаёт узлы всех включённых источников по установленным ядрам — ровно
// те, что пишутся в файлы источников для того же Input (одна ветка кода,
// kernelNodes). Ничего не пишет и не меняет; читает только снимки подписок, как
// и генерация. Результат мемоизируется в том же ограниченном кэше, что файлы.
func (s *Service) Catalog(in configlayer.Input, installed configlayer.InstalledKernels) (Catalog, error) {
	caps := s.caps(installed)
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return Catalog{}, err
	}
	names := make([]string, 0, len(in.Sections))
	for name := range in.Sections {
		if _, ok := sourceID(name); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	parts := []string{"catalog", fmt.Sprint(in.Mode), fmt.Sprint(installed.Xray), fmt.Sprint(installed.Mihomo), string(capsJSON),
		string(in.Live[LiveKeyCandidates]), string(in.Live[LiveKeyCandidatePrints])}
	for _, name := range names {
		id, _ := sourceID(name)
		parts = append(parts, name, string(in.Sections[name]), string(liveOf(in, id)))
	}
	key := hash(parts...)
	if m, ok := s.memoGet(key); ok && m.catalog != nil {
		return *m.catalog, nil
	}

	var (
		cat    Catalog
		live   LiveSources
		cands  Candidates
		loaded bool
	)
	for _, name := range names {
		id, _ := sourceID(name)
		raw := in.Sections[name]
		src := CatalogSource{ID: id, Kind: kindOf(raw)}
		switch src.Kind {
		case KindMy:
			if id != MySourceID {
				continue
			}
			cfg, err := parseMy(raw)
			if err != nil {
				return Catalog{}, err
			}
			src.Enabled = cfg.Enabled
		case KindSubscription:
			cfg, err := parseSubscription(raw)
			if err != nil {
				return Catalog{}, err
			}
			src.Name, src.Enabled = cfg.Name, cfg.Enabled
			if !loaded {
				if live, err = readLive(in.Live); err != nil {
					return Catalog{}, err
				}
				if cands, err = readCandidates(in.Live, in.Sections); err != nil {
					return Catalog{}, err
				}
				loaded = true
			}
		default:
			// Чужие виды секций пропускаются, как в генерации.
			continue
		}
		cat.Sources = append(cat.Sources, src)
		if installed.Has(kernelXray) {
			list, err := s.kernelNodes(in.Mode, kernelXray, id, raw, live, cands, caps)
			if err != nil {
				return Catalog{}, err
			}
			cat.Xray = append(cat.Xray, list...)
		}
		if installed.Has(kernelMihomo) {
			list, err := s.kernelNodes(in.Mode, kernelMihomo, id, raw, live, cands, caps)
			if err != nil {
				return Catalog{}, err
			}
			cat.Mihomo = append(cat.Mihomo, list...)
		}
	}
	s.memoPut(key, memoFile{catalog: &cat})
	return cat, nil
}
