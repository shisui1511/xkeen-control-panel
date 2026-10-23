package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Части провайдера списка dat для Mihomo (D-19): домены (behavior domain),
// записи keyword и regexp (behavior classical), сети (behavior ipcidr).
const (
	listPartDomain    = "d"
	listPartClassical = "k"
	listPartIP        = "i"
)

// Каталог текстовых провайдеров правил в каталоге Mihomo и префикс их имён.
const (
	mihomoRuleProviderDir = "rule_providers"
	listProviderPrefix    = "xcp-ls-"
)

// Коды ошибок генератора при выгрузке списка dat.
const (
	genErrListNotFound = "list_not_found"
	genErrListTooLarge = "list_too_large"
)

// maxProviderMemo и maxProviderMemoBytes — пределы мемоизации содержимого
// провайдеров: выгрузка большого списка не должна повторяться на каждый вызов
// Registry.Build.
const (
	maxProviderMemo      = 64
	maxProviderMemoBytes = 32 << 20
)

// ProviderName — имя текстового провайдера правил Mihomo для списка dat:
// "xcp-ls-" + первые 8 hex sha256(файл|ТЕГ) + "-" + часть (d, k или i). Тег
// сравнивается без регистра, как в dat. Имя читают наблюдатель dat (146-13) и
// сверка ruleCount.
func ProviderName(file, tag, part string) string {
	sum := sha256.Sum256([]byte(file + "|" + strings.ToUpper(tag)))
	return listProviderPrefix + hex.EncodeToString(sum[:])[:8] + "-" + part
}

// mihomoList — один список dat, на который ссылаются правила: выгрузка категории
// и имена её провайдеров (пустая часть провайдера не получает).
type mihomoList struct {
	file, tag string
	cat       lists.Category
}

// name возвращает имя провайдера части; пусто — в этой части записей нет.
func (l *mihomoList) name(part string) string {
	var n int
	switch part {
	case listPartDomain:
		n = len(l.cat.Domains)
	case listPartClassical:
		n = len(l.cat.Classical)
	case listPartIP:
		n = len(l.cat.CIDRs)
	}
	if n == 0 {
		return ""
	}
	return ProviderName(l.file, l.tag, part)
}

// mihomoProvider — текстовый провайдер правил: файл rule_providers/<имя>.txt.
type mihomoProvider struct {
	name     string
	behavior string
	lines    []string
	hash     string
}

// listKey — ключ списка: файл и тег без регистра.
func listKey(file, tag string) string { return file + "|" + strings.ToUpper(tag) }

// collectLists выгружает все списки dat, на которые ссылаются правила, пишущиеся
// в Mihomo (включённые, не raw и не «только Xray»): один список — одна выгрузка и
// один набор провайдеров на все правила. Ошибка выгрузки — ошибка генератора с
// кодом list_not_found или list_too_large (проверка 146-13 обычно ловит раньше).
func (b *mihomoBuild) collectLists(ix *lists.Index) error {
	b.lists = map[string]*mihomoList{}
	var keys []string
	for _, r := range b.rules.Items {
		if !mihomoApplicable(r) || r.Raw != nil {
			continue
		}
		for _, c := range r.Conds {
			if c.Type != CondGeosite && c.Type != CondGeoip {
				continue
			}
			for _, ref := range c.Lists {
				k := listKey(ref.File, ref.Tag)
				if _, ok := b.lists[k]; ok {
					continue
				}
				if ix == nil {
					return genErr(genErrListNotFound, ref.File+":"+ref.Tag)
				}
				cat, err := ix.Extract(ref.File, ref.Tag)
				switch {
				case errors.Is(err, lists.ErrListNotFound):
					return genErr(genErrListNotFound, ref.File+":"+ref.Tag)
				case errors.Is(err, lists.ErrListTooLarge):
					return genErr(genErrListTooLarge, ref.File+":"+ref.Tag)
				case err != nil:
					return err
				}
				b.lists[k] = &mihomoList{file: ref.File, tag: ref.Tag, cat: cat}
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		l := b.lists[k]
		for _, p := range []struct {
			part, behavior string
			lines          []string
		}{
			{listPartDomain, "domain", l.cat.Domains},
			{listPartClassical, "classical", l.cat.Classical},
			{listPartIP, "ipcidr", l.cat.CIDRs},
		} {
			if len(p.lines) == 0 {
				continue
			}
			b.providers = append(b.providers, mihomoProvider{
				name: ProviderName(l.file, l.tag, p.part), behavior: p.behavior,
				lines: p.lines, hash: l.cat.Hash,
			})
		}
	}
	sort.Slice(b.providers, func(i, j int) bool { return b.providers[i].name < b.providers[j].name })
	return nil
}

// providerFiles превращает провайдеры в файлы панели: rule_providers/<имя>.txt,
// строки через перевод строки, Expect — число строк (то, что Mihomo покажет в
// ruleCount). Файл действует сразу (Inert не ставится): на него ссылается профиль
// панели.
func (s *Service) providerFiles(ps []mihomoProvider) []configlayer.GeneratedFile {
	out := make([]configlayer.GeneratedFile, 0, len(ps))
	for _, p := range ps {
		out = append(out, configlayer.GeneratedFile{
			Kernel:  configlayer.KernelMihomo,
			RelPath: mihomoRuleProviderDir + "/" + p.name + ".txt",
			Kind:    configlayer.KindMihomoRuleProviderText,
			Content: s.providerContent(p),
			Expect:  &configlayer.Expectation{ProviderType: "rules", ProviderName: p.name, Count: len(p.lines)},
		})
	}
	return out
}

// providerContent возвращает текст провайдера; мемоизирован по имени (оно
// содержит файл и тег) и хэшу категории dat.
func (s *Service) providerContent(p mihomoProvider) []byte {
	key := p.name + "|" + p.hash + "|" + strconv.Itoa(len(p.lines))
	s.pmu.Lock()
	if c, ok := s.provs[key]; ok {
		s.pmu.Unlock()
		return c
	}
	s.pmu.Unlock()

	var sb strings.Builder
	for _, l := range p.lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	content := []byte(sb.String())

	s.pmu.Lock()
	defer s.pmu.Unlock()
	if _, ok := s.provs[key]; ok {
		return s.provs[key]
	}
	if len(content) > maxProviderMemoBytes {
		return content
	}
	for len(s.provOrder) > 0 && (len(s.provOrder) >= maxProviderMemo || s.provBytes+len(content) > maxProviderMemoBytes) {
		old := s.provOrder[0]
		s.provOrder = s.provOrder[1:]
		s.provBytes -= len(s.provs[old])
		delete(s.provs, old)
	}
	s.provs[key] = content
	s.provOrder = append(s.provOrder, key)
	s.provBytes += len(content)
	return content
}
