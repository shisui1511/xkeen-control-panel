package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Скан чужих файлов по ключам верхнего уровня (SEC-01, SEC-02, XCFG-01,
// XCFG-03). Скан только читает: чужой файл configs/ и профиль Mihomo не
// изменяются, не переименовываются и не перезаписываются (U-02). Значения
// секций нигде не логируются.

// maxXrayKeyFileBytes — файл configs/ крупнее считается неразобранным: чужой
// каталог недоверенный, а разбор целиком держит файл в памяти.
const maxXrayKeyFileBytes = 8 << 20

// xkeenTemplateFiles — файлы из шаблонов XKeen: секция, где все файлы из этого
// набора, принадлежит «XKeen» (допущение A3 исследования).
var xkeenTemplateFiles = map[string]bool{
	"01_log.json":       true,
	"02_dns.json":       true,
	"03_inbounds.json":  true,
	"04_outbounds.json": true,
	"05_routing.json":   true,
	"06_policy.json":    true,
}

// Владелец секции по скану.
const (
	OwnerNone  = "none"
	OwnerXKeen = "xkeen"
	OwnerUser  = "user"
)

// Теги входящих XKeen, к которым привязано правило force-proxy (D-12).
var forceProxyTags = map[string]bool{
	"force-proxy":          true,
	"force-proxy-redirect": true,
	"force-proxy-tproxy":   true,
}

// XrayKeyFile — результат скана одного файла configs/*.json по ключам верхнего
// уровня.
type XrayKeyFile struct {
	Name string
	// Keys — ключи верхнего уровня в порядке файла; у неразобранного файла —
	// ключи, найденные терпимым просмотром текста (без значений).
	Keys []string
	// Values — сырые значения ключей (у неразобранного файла пусто).
	Values map[string]json.RawMessage
	// Comments — в файле есть комментарии JSONC (D-02): при переносе они пропадут.
	Comments bool
	// Legacy — файл старого слоя: он переписывается старым слоем и не переносится.
	Legacy bool
	// Symlink — файл — симлинк: содержимое лежит вне configs/, не переносится.
	Symlink bool
	// Broken — файл не разобрался (в том числе JSONC с висячей запятой, слишком
	// большой или нечитаемый): он смешанный и не трогается.
	Broken bool
	// Hash — md5 содержимого, как оно лежит на диске.
	Hash string
}

// ScanXrayKeyFiles сканирует верхний уровень каталога конфигураций Xray: *.json
// без подкаталогов, .obsolete и файлов панели, по возрастанию имени. foreignOwned
// (может быть nil) отмечает файлы старого слоя. Каталога нет — пустой результат.
func ScanXrayKeyFiles(root string, foreignOwned func(kernel, rel string) bool) ([]XrayKeyFile, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("каталог Xray: %w", err)
	}
	var files []XrayKeyFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || IsPanelFileName(KernelXray, name) {
			continue
		}
		abs := filepath.Join(root, name)
		data, readErr := readLimited(abs, maxXrayKeyFileBytes)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue // оборванный симлинк
		}
		f := XrayKeyFile{
			Name:    name,
			Symlink: e.Type()&fs.ModeSymlink != 0,
			Legacy:  foreignOwned != nil && foreignOwned(KernelXray, name),
		}
		if readErr != nil {
			f.Broken = true
			files = append(files, f)
			continue
		}
		f.Hash = HashContent(data)
		text := string(data)
		stripped := services.StripJSONComments(text)
		f.Comments = stripped != text
		if obj, perr := ParseOrderedObject([]byte(stripped)); perr == nil {
			f.Keys = obj.Keys()
			f.Values = make(map[string]json.RawMessage, len(f.Keys))
			for _, k := range f.Keys {
				f.Values[k], _ = obj.Get(k)
			}
		} else {
			f.Broken = true
			f.Keys = looseTopLevelKeys(stripped)
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// readLimited читает файл целиком, но не больше limit байт: больший файл —
// ошибка.
func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("файл больше %d байт", limit)
	}
	return data, nil
}

// looseTopLevelKeys находит ключи верхнего уровня объекта в тексте, который не
// разбирается как JSON (висячая запятая и подобное): строка на глубине 1,
// за которой следует двоеточие. Текст, не начинающийся с объекта, ключей не даёт.
func looseTopLevelKeys(text string) []string {
	i := 0
	for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i++
	}
	if i >= len(text) || text[i] != '{' {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	depth := 0
	for ; i < len(text); i++ {
		switch c := text[i]; c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '"':
			start := i
			i++
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			if depth != 1 || i >= len(text) {
				continue
			}
			j := i + 1
			for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\r' || text[j] == '\n') {
				j++
			}
			if j >= len(text) || text[j] != ':' {
				continue
			}
			key, err := strconv.Unquote(text[start : i+1])
			if err != nil {
				key = text[start+1 : i]
			}
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// XraySectionScan — результат скана одной секции Xray.
type XraySectionScan struct {
	ID string
	// Files — файлы с ключом секции, по возрастанию имени.
	Files []string
	// Pure — «чистые» файлы: все ключи принадлежат секции, файл разобран, не
	// симлинк и не старого слоя; только они переносятся в резервный набор.
	Pure []string
	// Mixed — остальные файлы с ключом секции: остаются на месте.
	Mixed []string
	// Comments — файлы из Files с комментариями JSONC.
	Comments []string
	// Shadowing — файлы с ключом секции, которые сортируются после файла панели
	// секции и перекрыли бы его (не считается для inbounds: они объединяются).
	Shadowing []string
	// Effective — действующее значение секции (D-28): log, dns, policy и «Прочие
	// ключи» — глубокое слияние сырых значений ключа из файлов секции по порядку
	// имён по правилам Xray (mergeXrayValue); с дампом ядра оно сверяется в
	// роутерных сценариях, но не берётся из него (дамп нормализован ядром).
	// «Входящие» — слияние по tag, поздний входящий заменяет ранний целиком. nil —
	// значения нет или оно неизвестно (см. EffectiveUnknown).
	Effective json.RawMessage
	// EffectiveUnknown — среди файлов с ключом секции есть неразобранный (Broken):
	// его значения достоверно не знаем, Effective пуст, секция не берётся (D-28).
	// Для «Входящих» всегда false.
	EffectiveUnknown bool
	// Owner — none, xkeen или user.
	Owner string
	// Sources — {имя, md5} всех файлов из Files.
	Sources []SourceFile
	// InboundsMixed — для inbounds: среди файлов есть смешанный не-Legacy файл,
	// «Входящие» не берутся (D-25).
	InboundsMixed bool
	// ForceProxyTags — для inbounds: теги force-proxy* (D-12).
	ForceProxyTags []string
}

// ScanXraySections строит скан всех пяти секций Xray по результату
// ScanXrayKeyFiles; ключ карты — идентификатор секции.
func ScanXraySections(files []XrayKeyFile) map[string]XraySectionScan {
	sorted := append([]XrayKeyFile(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	out := make(map[string]XraySectionScan, len(XraySectionIDs))
	for _, id := range XraySectionIDs {
		out[id] = scanXraySection(id, sorted)
	}
	return out
}

// sectionKeyCount считает ключи файла, принадлежащие секции id.
func sectionKeyCount(f XrayKeyFile, id string) int {
	n := 0
	for _, k := range f.Keys {
		if XraySectionOfKey(k) == id {
			n++
		}
	}
	return n
}

func scanXraySection(id string, files []XrayKeyFile) XraySectionScan {
	s := XraySectionScan{
		ID:        id,
		Files:     []string{},
		Pure:      []string{},
		Mixed:     []string{},
		Comments:  []string{},
		Shadowing: []string{},
		Sources:   []SourceFile{},
		Owner:     OwnerNone,
	}
	panelFile := PanelXraySectionFile(id)
	allTemplates := true
	var members []XrayKeyFile
	for _, f := range files {
		n := sectionKeyCount(f, id)
		if n == 0 {
			continue
		}
		members = append(members, f)
		s.Files = append(s.Files, f.Name)
		s.Sources = append(s.Sources, SourceFile{Name: f.Name, Hash: f.Hash})
		if n == len(f.Keys) && !f.Broken && !f.Legacy && !f.Symlink {
			s.Pure = append(s.Pure, f.Name)
		} else {
			s.Mixed = append(s.Mixed, f.Name)
			if id == xraySectionInbounds && !f.Legacy {
				s.InboundsMixed = true
			}
		}
		if f.Comments {
			s.Comments = append(s.Comments, f.Name)
		}
		if id != xraySectionInbounds && f.Name > panelFile {
			s.Shadowing = append(s.Shadowing, f.Name)
		}
		if !xkeenTemplateFiles[f.Name] {
			allTemplates = false
		}
	}
	switch {
	case len(members) == 0:
	case allTemplates:
		s.Owner = OwnerXKeen
	default:
		s.Owner = OwnerUser
	}
	switch id {
	case xraySectionInbounds:
		s.Effective = mergeInbounds(members)
		s.ForceProxyTags = collectForceProxyTags(members)
	default:
		for _, f := range members {
			if f.Broken {
				s.EffectiveUnknown = true
			}
		}
		if !s.EffectiveUnknown {
			s.Effective = mergeSectionKeys(id, members)
		}
	}
	return s
}

// mergeSectionKeys сливает значения ключей секции id (log, dns, policy, other) из
// разобранных файлов по порядку имён (D-28). Для log, dns и policy результат —
// слитое значение одноимённого ключа, для other — объект из слитых значений
// ключей секции в порядке первого появления. nil — значений нет.
func mergeSectionKeys(id string, files []XrayKeyFile) json.RawMessage {
	merged := NewOrderedObject()
	for _, f := range files {
		for _, k := range f.Keys {
			if XraySectionOfKey(k) != id {
				continue
			}
			v, ok := f.Values[k]
			if !ok {
				continue
			}
			if prev, seen := merged.Get(k); seen {
				v = mergeXrayValue(prev, v)
			}
			merged.Set(k, v)
		}
	}
	if merged.Len() == 0 {
		return nil
	}
	if id != xraySectionOther {
		v, _ := merged.Get(id)
		return compactOrSame(v)
	}
	b, err := merged.MarshalJSON()
	if err != nil {
		return nil
	}
	return b
}

// mergeXrayValue — правило слияния двух значений одного ключа из файлов каталога
// Xray (dst — ранний файл, src — поздний), как его применяет ядро при
// `-confdir` (infra/conf/merge): объекты сливаются рекурсивно, порядок ключей —
// по первому появлению; массивы дополняются (ГИПОТЕЗА D-28: ранний массив +
// поздний, без дедупликации); любые другие значения заменяются поздним. Правило
// проверяет роутерный сценарий TestRouterCoresMergeSemantics, сверяя слитое
// значение с дампом ядра: расхождение значит, что правило надо поправить здесь.
func mergeXrayValue(dst, src json.RawMessage) json.RawMessage {
	switch {
	case isJSONKind(dst, '{') && isJSONKind(src, '{'):
		d, derr := ParseOrderedObject(dst)
		s, serr := ParseOrderedObject(src)
		if derr != nil || serr != nil {
			return compactOrSame(src)
		}
		for _, k := range s.Keys() {
			sv, _ := s.Get(k)
			if dv, ok := d.Get(k); ok {
				sv = mergeXrayValue(dv, sv)
			}
			d.Set(k, sv)
		}
		b, err := d.MarshalJSON()
		if err != nil {
			return compactOrSame(src)
		}
		return b
	case isJSONKind(dst, '[') && isJSONKind(src, '['):
		var da, sa []json.RawMessage
		if json.Unmarshal(dst, &da) != nil || json.Unmarshal(src, &sa) != nil {
			return compactOrSame(src)
		}
		b, err := json.Marshal(append(da, sa...))
		if err != nil {
			return compactOrSame(src)
		}
		return b
	}
	return compactOrSame(src)
}

// isJSONKind — значение начинается с символа first ('{' или '[').
func isJSONKind(raw json.RawMessage, first byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == first
}

// compactOrSame убирает пробелы между токенами значения; при ошибке отдаёт как есть.
func compactOrSame(raw json.RawMessage) json.RawMessage {
	if c, err := compactRaw(raw); err == nil {
		return c
	}
	return raw
}

// lastKeyValue — значение ключа из последнего по имени разобранного файла.
// Нужен только для burstObservatory (основа проб, D-27): это не секция панели.
func lastKeyValue(files []XrayKeyFile, key string) json.RawMessage {
	for i := len(files) - 1; i >= 0; i-- {
		if files[i].Broken {
			continue
		}
		if v, ok := files[i].Values[key]; ok {
			return v
		}
	}
	return nil
}

// mergeInbounds объединяет входящие по порядку файлов так, как это делает Xray:
// входящий с уже встреченным tag заменяет прежний на его месте, без tag или с
// новым tag дописывается в конец.
func mergeInbounds(files []XrayKeyFile) json.RawMessage {
	var list []json.RawMessage
	index := map[string]int{}
	found := false
	for _, f := range files {
		if f.Broken {
			continue
		}
		raw, ok := f.Values["inbounds"]
		if !ok {
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			continue
		}
		found = true
		for _, item := range arr {
			var t struct {
				Tag string `json:"tag"`
			}
			_ = json.Unmarshal(item, &t)
			if t.Tag != "" {
				if i, dup := index[t.Tag]; dup {
					list[i] = item
					continue
				}
				index[t.Tag] = len(list)
			}
			list = append(list, item)
		}
	}
	if !found {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, item := range list {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := json.Compact(&buf, item); err != nil {
			return nil
		}
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// inboundTags возвращает теги входящих разобранного файла в порядке файла.
func inboundTags(f XrayKeyFile) []string {
	if f.Broken {
		return nil
	}
	raw, ok := f.Values["inbounds"]
	if !ok {
		return nil
	}
	var arr []struct {
		Tag string `json:"tag"`
	}
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	tags := make([]string, 0, len(arr))
	for _, in := range arr {
		tags = append(tags, in.Tag)
	}
	return tags
}

// collectForceProxyTags собирает теги force-proxy* входящих без повторов, по
// порядку файлов.
func collectForceProxyTags(files []XrayKeyFile) []string {
	tags := []string{}
	seen := map[string]bool{}
	for _, f := range files {
		for _, tag := range inboundTags(f) {
			if forceProxyTags[tag] && !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// IsTransparentInbound сообщает, выбрал бы XKeen входящий как прозрачный: по
// форме, а не по тегу (143 Q7) — protocol dokodemo-door или tunnel,
// settings.followRedirect == true и streamSettings.sockopt.tproxy пуст, redirect
// или tproxy. mode — "tproxy", если sockopt.tproxy == "tproxy", иначе "redirect".
func IsTransparentInbound(raw json.RawMessage) (mode string, ok bool) {
	var in struct {
		Protocol string `json:"protocol"`
		Settings struct {
			FollowRedirect bool `json:"followRedirect"`
		} `json:"settings"`
		StreamSettings struct {
			Sockopt struct {
				TProxy string `json:"tproxy"`
			} `json:"sockopt"`
		} `json:"streamSettings"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return "", false
	}
	if in.Protocol != "dokodemo-door" && in.Protocol != "tunnel" {
		return "", false
	}
	if !in.Settings.FollowRedirect {
		return "", false
	}
	switch in.StreamSettings.Sockopt.TProxy {
	case "", "redirect":
		return "redirect", true
	case "tproxy":
		return "tproxy", true
	}
	return "", false
}

// XrayScanFacts — факты о чужих файлах для генераторов Input.Routing.
type XrayScanFacts struct {
	// ForeignAPI — в каком-либо файле (в том числе старого слоя) есть ключ api.
	ForeignAPI bool
	// Observatory — есть ключ observatory или burstObservatory.
	Observatory bool
	// ForceProxyTags — теги входящих force-proxy* (D-12), без повторов.
	ForceProxyTags []string
	// BurstObservatory — значение burstObservatory из последнего файла (основа
	// проб, D-27); nil — ключа нет.
	BurstObservatory json.RawMessage
}

// XrayFacts считает факты для генераторов по результату скана.
func XrayFacts(files []XrayKeyFile) XrayScanFacts {
	sorted := append([]XrayKeyFile(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	facts := XrayScanFacts{ForceProxyTags: collectForceProxyTags(sorted)}
	for _, f := range sorted {
		for _, k := range f.Keys {
			switch k {
			case "api":
				facts.ForeignAPI = true
			case "observatory", "burstObservatory":
				facts.Observatory = true
			}
		}
	}
	facts.BurstObservatory = lastKeyValue(sorted, "burstObservatory")
	return facts
}

// hasAPIInbound — в каком-либо разобранном файле есть входящий с тегом api.
func hasAPIInbound(files []XrayKeyFile) bool {
	for _, f := range files {
		for _, tag := range inboundTags(f) {
			if tag == "api" {
				return true
			}
		}
	}
	return false
}

// ReadMihomoActive читает активный профиль Mihomo по config.yaml: симлинк —
// имя цели относительно корня (profiles/<имя>.yaml; цель должна лежать внутри
// корня), обычный файл — "config.yaml". Возвращает имя и байты профиля. Файл
// только читается.
func ReadMihomoActive(root string) (name string, data []byte, err error) {
	if root == "" {
		return "", nil, errors.New("каталог Mihomo не задан")
	}
	cfg := filepath.Join(root, "config.yaml")
	fi, err := os.Lstat(cfg)
	if err != nil {
		return "", nil, fmt.Errorf("config.yaml: %w", err)
	}
	path, name := cfg, "config.yaml"
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(cfg)
		if err != nil {
			return "", nil, fmt.Errorf("config.yaml: %w", err)
		}
		target := link
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		rel, ok := relInsideRoot(root, target)
		if !ok {
			return "", nil, fmt.Errorf("config.yaml: %w", ErrSymlinkOutsideRoot)
		}
		// Цепочка симлинков внутри корня не должна выводить за его пределы.
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return "", nil, fmt.Errorf("config.yaml: %w", err)
		}
		if _, ok := relInsideRoot(root, resolved); !ok {
			return "", nil, fmt.Errorf("config.yaml: %w", ErrSymlinkOutsideRoot)
		}
		path, name = target, rel
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", name, err)
	}
	return name, data, nil
}

// XrayDump возвращает дамп каталога Xray слоя: ключи верхнего уровня слитого
// ядром конфига. Дамп нормализован ядром (добавлены значения по умолчанию), поэтому
// это диагностика и арбитр сверки, а не значение секции (D-28). Ядро не установлено
// или без флага -dump, корень Xray не задан — ErrXrayDumpUnavailable, отказ ядра —
// ErrXrayDumpFailed. Каждый вызов запускает ядро.
func (l *Layer) XrayDump(ctx context.Context) (map[string]json.RawMessage, error) {
	return XrayDump(ctx, l.binaries().Xray, l.opts.Roots.Xray, l.opts.XrayEnv)
}

// ScanXraySections сканирует каталог Xray слоя: карта секций по идентификатору
// и файлы скана. Значения секций — слияние сырых значений файлов (D-28), ядро не
// запускается. Корень Xray не задан — пустая карта без ошибки.
func (l *Layer) ScanXraySections() (map[string]XraySectionScan, []XrayKeyFile, error) {
	if l.opts.Roots.Xray == "" {
		return map[string]XraySectionScan{}, nil, nil
	}
	files, err := ScanXrayKeyFiles(l.opts.Roots.Xray, l.opts.ForeignOwned)
	if err != nil {
		return nil, nil, err
	}
	return ScanXraySections(files), files, nil
}

// MihomoActiveProfile возвращает активный профиль Mihomo слоя: имя, байты и md5.
func (l *Layer) MihomoActiveProfile() (name string, data []byte, hash string, err error) {
	name, data, err = ReadMihomoActive(l.opts.Roots.Mihomo)
	if err != nil {
		return "", nil, "", err
	}
	return name, data, HashContent(data), nil
}
