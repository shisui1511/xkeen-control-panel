package legacyarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
	"gopkg.in/yaml.v3"
)

const (
	XrayDefault  = "04_outbounds.zz_xcp_default.json"
	XraySelected = "04_outbounds.zz_xcp_selected.tail.json"
	xrayRules    = "05_routing.json"
	mihomoConfig = "config.yaml"
	beginRules   = "# --- BEGIN USER RULES ---"
	endRules     = "# --- END USER RULES ---"
)

var errAmbiguous = errors.New("вставки старой панели не удалось однозначно выделить")

// errPreserved и errNotOwned не становятся находками: профиль пользователя и
// файл без доказанного владения остаются без уведомления и без правки.
var (
	errPreserved = errors.New("профиль пользователя сохраняется")
	errNotOwned  = errors.New("владение старой панели не доказано")
)

// Finding содержит только путь внутри каталога ядра, счётчик и безопасную причину.
type Finding struct {
	Kernel string
	File   string
	Count  int
	Reason string
}

type kernelTree struct {
	kernel string
	// path — корень с разрешёнными симлинками, закреплённый идентичностью;
	// configured — тот же корень в записи настроек (так его пишут алиасы профилей).
	path, configured string
	archiveDir
}

func openKernelTree(kernel, path string) (*kernelTree, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	configured, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(configured); err != nil {
		return nil, err
	}
	validator := utils.NewPathValidator([]string{configured})
	canonical, err := validator.ValidateUnder(configured, configured)
	if err != nil {
		return nil, errUnsafePath
	}
	// Корень, заданный через симлинк каталога, разрешается один раз; дальше
	// каждая операция сверяет идентичность разрешённого пути.
	info, err := os.Lstat(canonical)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errUnsafePath
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, err
	}
	tree := &kernelTree{kernel, canonical, configured, archiveDir{root, validator, info}}
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) || tree.checkLocation() != nil {
		root.Close()
		return nil, errUnsafePath
	}
	return tree, nil
}

func (tree *kernelTree) checkLocation() error {
	current, err := os.Lstat(tree.path)
	if err != nil || !os.SameFile(tree.info, current) {
		return errUnsafePath
	}
	return tree.archiveDir.checkLocation()
}

type kernelFile struct {
	name, target  string
	info, alias   os.FileInfo
	before, after []byte
}

// profileLink сообщает, что алиас ведёт в profiles/ своего корня. Профили
// (в том числе прошедшие adopt) — файлы пользователя: их не читают и не правят.
func (tree *kernelTree) profileLink(target string) bool {
	rel := target
	if filepath.IsAbs(target) {
		rel = ""
		for _, root := range []string{tree.configured, tree.path} {
			if r, err := filepath.Rel(root, filepath.Clean(target)); err == nil && filepath.IsLocal(r) {
				rel = r
				break
			}
		}
	}
	return filepath.IsLocal(rel) && strings.HasPrefix(filepath.ToSlash(filepath.Clean(rel)), "profiles/")
}

// openConfig прослеживает только один относительный алиас верхнего уровня.
// profiles/ сохраняется целиком: у старого adopt нет отдельной метки владения.
// Абсолютная ссылка вне profiles/ и выход из корня — отказ без чтения цели.
func (tree *kernelTree) openConfig(name string) (*kernelFile, error) {
	if err := tree.checkLocation(); err != nil {
		return nil, err
	}
	file := &kernelFile{name: name, target: name}
	if tree.kernel == configlayer.KernelMihomo && name == mihomoConfig {
		info, err := tree.Root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := tree.Root.Readlink(name)
			if err != nil {
				return nil, errUnsafePath
			}
			if tree.profileLink(target) {
				return nil, errPreserved
			}
			if !filepath.IsLocal(target) || filepath.Clean(target) != target {
				return nil, errUnsafePath
			}
			if _, err := tree.validator.ValidateUnder(filepath.Join(tree.Name(), target), tree.Name()); err != nil {
				return nil, errUnsafePath
			}
			if filepath.Base(target) != target || !strings.HasSuffix(target, ".yaml") || target == name {
				return nil, errUnsafePath
			}
			file.target, file.alias = target, info
		}
	}
	info, err := tree.lookup(file.target)
	if err != nil {
		return nil, err
	}
	file.info = info
	f, err := tree.openRegular(file.target, info)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	file.before, err = io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if err := tree.checkUnchanged(file.target, info, f); err != nil {
		return nil, err
	}
	return file, tree.checkAlias(file)
}

func (tree *kernelTree) checkAlias(file *kernelFile) error {
	if err := tree.checkLocation(); err != nil {
		return err
	}
	if file.alias == nil {
		return nil
	}
	info, err := tree.Root.Lstat(file.name)
	if err != nil || !os.SameFile(file.alias, info) {
		return errChanged
	}
	if target, err := tree.Root.Readlink(file.name); err != nil || target != file.target {
		return errChanged
	}
	return nil
}

func (tree *kernelTree) scan(m *marker, selectionOnly bool) ([]Finding, []*kernelFile) {
	var findings []Finding
	var edits []*kernelFile
	find := func(name string, check func(*kernelFile) (int, error), fileOnly bool) *kernelFile {
		file, err := tree.openConfig(name)
		if os.IsNotExist(err) || errors.Is(err, errPreserved) {
			return nil
		}
		finding := Finding{Kernel: tree.kernel, File: name}
		if err == nil {
			finding.Count, err = check(file)
		}
		if errors.Is(err, errNotOwned) {
			return nil
		}
		if err != nil {
			switch {
			case errors.Is(err, errUnsafePath), errors.Is(err, errChanged):
				finding.Reason = errUnsafePath.Error()
			default:
				finding.Reason = errAmbiguous.Error()
			}
		} else if fileOnly || finding.Count > 0 {
			edits = append(edits, file)
		} else {
			return file
		}
		findings = append(findings, finding)
		return file
	}

	switch tree.kernel {
	case configlayer.KernelXray:
		for _, name := range []string{XrayDefault, XraySelected} {
			find(name, func(*kernelFile) (int, error) { return 1, nil }, true)
		}
		if selectionOnly {
			break
		}
		find(xrayRules, func(file *kernelFile) (count int, err error) {
			file.after, count, err = cutXrayRules(file.before)
			return count, err
		}, false)
		if m != nil {
			resolved := make(map[string]ownership)
			conflicts := make(map[string]bool)
			for _, owner := range m.Ownership {
				id := strings.ToLower(owner.ID)
				if previous, ok := resolved[id]; ok && previous != owner {
					conflicts[id] = true
				}
				resolved[id] = owner
			}
			considered := make(map[string]bool)
			for _, owner := range m.Ownership {
				id := strings.ToLower(owner.ID)
				if !registryName.MatchString(id) || conflicts[id] || considered[id] || len(owner.XrayHash) != 64 {
					continue
				}
				considered[id] = true
				var tags []string
				for _, name := range subscriptionFragments(id) {
					find(name, func(file *kernelFile) (int, error) {
						if legacyDigest(file.before) != owner.XrayHash {
							// Совпало только имя: файл мог написать пользователь.
							return 0, errNotOwned
						}
						tags = outboundTags(file.before)
						return 1, nil
					}, true)
				}
				if owner.RoutingMode == "auto" && len(tags) > 0 && id != "json" {
					find("05_routing."+id+".json", func(file *kernelFile) (int, error) {
						if !matchesSubscriptionRouting(file.before, owner, tags[0]) {
							return 0, errNotOwned
						}
						return 1, nil
					}, true)
				}
			}
		}
	case configlayer.KernelMihomo:
		find(mihomoConfig, func(file *kernelFile) (count int, err error) {
			file.after, count, err = cutMihomoRules(file.before)
			return count, err
		}, false)
	}
	return findings, edits
}

func legacyDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func subscriptionFragments(id string) []string {
	tail := "04_outbounds." + id + ".tail.json"
	if tail == XraySelected {
		tail = "04_outbounds." + id + "_sub.tail.json"
	}
	old := "04_outbounds." + id + ".json"
	if old == XrayDefault || old == XraySelected || old == "04_outbounds.manual.json" {
		return []string{tail}
	}
	return []string{tail, old}
}

func outboundTags(data []byte) []string {
	var fragment struct {
		Outbounds []struct{ Tag string }
	}
	if json.Unmarshal(data, &fragment) != nil {
		return nil
	}
	var tags []string
	for _, outbound := range fragment.Outbounds {
		if outbound.Tag != "" {
			tags = append(tags, outbound.Tag)
		}
	}
	return tags
}

// Старый генератор писал этот фрагмент целиком. Сверяем точное содержимое,
// включая отсутствие пользовательских полей и дополнительных правил.
func matchesSubscriptionRouting(data []byte, owner ownership, firstTag string) bool {
	type rule struct {
		Type     string   `json:"type"`
		Domain   []string `json:"domain"`
		Outbound string   `json:"outboundTag,omitempty"`
		Balancer string   `json:"balancerTag,omitempty"`
	}
	type balancer struct {
		Tag      string   `json:"tag"`
		Selector []string `json:"selector"`
	}
	type routing struct {
		Balancers []balancer `json:"balancers,omitempty"`
		Rules     []rule     `json:"rules"`
	}
	want := struct {
		Routing routing `json:"routing"`
	}{}
	r := rule{Type: "field", Domain: []string{"geosite:geolocation-!cn", "geoip:!cn"}, Outbound: firstTag}
	if owner.TagPrefix != "" {
		r.Outbound, r.Balancer = "", owner.ID+"-balancer"
		want.Routing.Balancers = []balancer{{r.Balancer, []string{owner.TagPrefix + "-"}}}
	}
	want.Routing.Rules = []rule{r}
	expected, err := json.Marshal(want)
	var actual bytes.Buffer
	return err == nil && json.Compact(&actual, data) == nil && bytes.Equal(expected, actual.Bytes())
}

type span struct{ start, end int }

// objectFields читает прямые поля и отказывается от неоднозначных дубликатов.
// Decoder отслеживает байтовую позицию, RawMessage сохраняет исходные байты.
func objectFields(data []byte) (map[string]span, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return nil, errAmbiguous
	}
	fields := make(map[string]span)
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return nil, errAmbiguous
		}
		if _, exists := fields[key]; exists {
			return nil, errAmbiguous
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errAmbiguous
		}
		end := int(dec.InputOffset())
		fields[key] = span{end - len(raw), end}
	}
	if close, err := dec.Token(); err != nil || close != json.Delim('}') {
		return nil, errAmbiguous
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errAmbiguous
	}
	return fields, nil
}

func cutXrayRules(data []byte) ([]byte, int, error) {
	// Без признака владения файл не разбирается: комментарии и прочий синтаксис
	// пользователя не превращаются в находку.
	if !bytes.Contains(data, []byte(`"user_rule"`)) {
		return nil, 0, nil
	}
	fields, err := objectFields(data)
	if err != nil {
		return nil, 0, err
	}
	var base int
	if routing, ok := fields["routing"]; ok {
		if _, other := fields["rules"]; other {
			return nil, 0, errAmbiguous
		}
		base = routing.start
		fields, err = objectFields(data[routing.start:routing.end])
		if err != nil {
			return nil, 0, err
		}
	}
	rules, ok := fields["rules"]
	if !ok {
		return nil, 0, nil
	}
	base += rules.start
	array := data[base : base+rules.end-rules.start]
	dec := json.NewDecoder(bytes.NewReader(array))
	if open, err := dec.Token(); err != nil || open != json.Delim('[') {
		return nil, 0, errAmbiguous
	}
	type element struct {
		span
		owned bool
	}
	var elements []element
	lastKept := -1
	count := 0
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, 0, errAmbiguous
		}
		end := base + int(dec.InputOffset())
		item := element{span: span{end - len(raw), end}}
		if raw[0] == '{' {
			fields, err := objectFields(raw)
			if err != nil {
				return nil, 0, err
			}
			if flag, ok := fields["user_rule"]; ok {
				item.owned = bytes.Equal(raw[flag.start:flag.end], []byte("true"))
			}
		}
		if item.owned {
			count++
		} else {
			lastKept = len(elements)
		}
		elements = append(elements, item)
	}
	if count == 0 {
		return nil, 0, nil
	}
	var cuts []span
	for i, item := range elements {
		if item.owned {
			cuts = append(cuts, item.span)
		}
		if i+1 < len(elements) && (item.owned || i == lastKept) {
			comma := bytes.IndexByte(data[item.end:elements[i+1].start], ',')
			if comma < 0 {
				return nil, 0, errAmbiguous
			}
			cuts = append(cuts, span{item.end + comma, item.end + comma + 1})
		}
	}
	out := cutSpans(data, cuts)
	if !json.Valid(out) {
		return nil, 0, errAmbiguous
	}
	return out, count, nil
}

func cutMihomoRules(data []byte) ([]byte, int, error) {
	if !bytes.Contains(data, []byte(beginRules)) && !bytes.Contains(data, []byte(endRules)) {
		return nil, 0, nil
	}
	var doc, extra yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if dec.Decode(&doc) != nil || dec.Decode(&extra) != io.EOF ||
		len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, 0, errAmbiguous
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	var rules *yaml.Node
	var first, limit int
	fields := doc.Content[0].Content
	for i := 0; i < len(fields); i += 2 {
		if fields[i].Value != "rules" {
			continue
		}
		if rules != nil {
			return nil, 0, errAmbiguous
		}
		rules = fields[i+1]
		first, limit = fields[i].Line, len(lines)
		if i+2 < len(fields) {
			limit = fields[i+2].Line - 1
		}
	}
	if rules == nil || (rules.Kind != yaml.SequenceNode && rules.Tag != "!!null") {
		return nil, 0, errAmbiguous
	}
	scalars := make(map[int]int, len(rules.Content))
	items := make(map[int]bool, len(rules.Content))
	for _, item := range rules.Content {
		items[item.Line-1] = true
		if item.Kind == yaml.ScalarNode && item.Tag == "!!str" && item.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
			scalars[item.Line-1] = item.Column - 1
		}
	}
	var cuts []span
	var start, offset, indent, previousRule int
	open := false
	for i, line := range lines {
		left := bytes.TrimLeft(line, " ")
		trim := string(bytes.TrimSpace(line))
		column := len(line) - len(left)
		switch {
		case (trim == beginRules || trim == endRules) && i >= first && i < limit:
			if trim == beginRules && !open {
				// Старый генератор ставил блок перед правилами верхнего уровня.
				// Маркер глубже предшествующего скаляра может быть его текстом.
				if previousRule != 0 && column > previousRule {
					return nil, 0, errAmbiguous
				}
				start, indent, open = offset, column, true
			} else if trim == endRules && open && column == indent {
				cuts = append(cuts, span{start, offset + len(line)})
				open = false
			} else {
				return nil, 0, errAmbiguous
			}
		case open && trim != "":
			scalarColumn, scalar := scalars[i]
			if !scalar || column != indent || scalarColumn <= column || !bytes.HasPrefix(left, []byte("- ")) {
				return nil, 0, errAmbiguous
			}
		case i >= first && i < limit && trim != "":
			if items[i] {
				previousRule = column + 1
			}
		}
		offset += len(line)
	}
	if open || len(cuts) == 0 {
		return nil, 0, errAmbiguous
	}
	out := cutSpans(data, cuts)
	if yaml.Unmarshal(out, &doc) != nil {
		return nil, 0, errAmbiguous
	}
	return out, len(cuts), nil
}

func cutSpans(data []byte, cuts []span) []byte {
	out := make([]byte, 0, len(data))
	var end int
	for _, cut := range cuts {
		out = append(out, data[end:cut.start]...)
		end = cut.end
	}
	return append(out, data[end:]...)
}
