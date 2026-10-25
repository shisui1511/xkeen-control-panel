package lists

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

// ListRef — ссылка на список: файл dat, тег и вид («файл · тег»).
type ListRef struct {
	File string `json:"file"`
	Tag  string `json:"tag"`
	Kind string `json:"kind"`
}

// ServiceView — сервис каталога с найденными на роутере ссылками.
type ServiceView struct {
	ID     string    `json:"id"`
	NameRU string    `json:"name_ru"`
	NameEN string    `json:"name_en"`
	Refs   []ListRef `json:"refs"`
}

// ListEntry — строка раздела «Все списки»: один тег одного файла.
type ListEntry struct {
	File     string   `json:"file"`
	Tag      string   `json:"tag"`
	Kind     string   `json:"kind"`
	Count    int      `json:"count"`
	Examples []string `json:"examples"`
}

// CountryView — страна: IP-список (тег с кодом страны в установленном geoip-файле),
// доменная категория и списки блокировок.
type CountryView struct {
	Code    string    `json:"code"`
	IP      *ListRef  `json:"ip"`
	Site    *ListRef  `json:"site"`
	Blocked []ListRef `json:"blocked"`
}

// ExtendedSet — расширенный набор, который можно добавить в XKeen (только GitHub
// release-URL из встроенного каталога).
type ExtendedSet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	File      string `json:"file"`
	URL       string `json:"url"`
	Kind      string `json:"kind"`
	SizeHint  string `json:"size_hint"`
	Heavy     bool   `json:"heavy"`
	Installed bool   `json:"installed"`
	InXKeen   bool   `json:"in_xkeen"`
}

// CatalogView — каталог списков для интерфейса и пресетов. Geodata содержит только
// записи xkeen.json; JQ и GeofileUpdate заполняет вызывающий из GeodataStore.Status.
type CatalogView struct {
	Services  []ServiceView `json:"services"`
	Lists     []ListEntry   `json:"lists"`
	Countries []CountryView `json:"countries"`
	Extended  []ExtendedSet `json:"extended"`
	Geodata   GeodataStatus `json:"geodata"`
}

type catRef struct {
	Kind   string `json:"kind"`
	FileRe string `json:"file_re"`
	Tag    string `json:"tag"`
	re     *regexp.Regexp
}

type catService struct {
	ID     string   `json:"id"`
	NameRU string   `json:"name_ru"`
	NameEN string   `json:"name_en"`
	Refs   []catRef `json:"refs"`
}

type catCountry struct {
	Code    string   `json:"code"`
	Site    []catRef `json:"site"`
	Blocked []catRef `json:"blocked"`
}

type catalogData struct {
	Services  []catService  `json:"services"`
	Countries []catCountry  `json:"countries"`
	Extended  []ExtendedSet `json:"extended"`
}

var catalog = mustLoadCatalog()

var (
	releaseURLRe = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/releases/latest/download/[A-Za-z0-9._-]+$`)
	countryRe    = regexp.MustCompile(`^[A-Za-z]{2}$`)
)

func mustLoadCatalog() *catalogData {
	var c catalogData
	if err := json.Unmarshal(catalogJSON, &c); err != nil {
		panic(fmt.Sprintf("lists: catalog.json: %v", err))
	}
	compile := func(where string, refs []catRef) {
		for i := range refs {
			r := &refs[i]
			if r.Kind != KindSite && r.Kind != KindIP {
				panic(fmt.Sprintf("lists: catalog.json: %s: вид %q", where, r.Kind))
			}
			if r.Tag == "" {
				panic(fmt.Sprintf("lists: catalog.json: %s: пустой тег", where))
			}
			re, err := regexp.Compile(r.FileRe)
			if err != nil {
				panic(fmt.Sprintf("lists: catalog.json: %s: %v", where, err))
			}
			r.re = re
		}
	}
	seen := map[string]bool{}
	for i := range c.Services {
		s := &c.Services[i]
		if s.ID == "" || seen[s.ID] {
			panic(fmt.Sprintf("lists: catalog.json: сервис %q", s.ID))
		}
		seen[s.ID] = true
		compile("service "+s.ID, s.Refs)
	}
	for i := range c.Countries {
		cn := &c.Countries[i]
		if !countryRe.MatchString(cn.Code) {
			panic(fmt.Sprintf("lists: catalog.json: страна %q", cn.Code))
		}
		compile("country "+cn.Code, cn.Site)
		compile("country "+cn.Code, cn.Blocked)
	}
	for _, e := range c.Extended {
		if e.ID == "" || !datNameRe.MatchString(e.File) || !releaseURLRe.MatchString(e.URL) ||
			(e.Kind != KindSite && e.Kind != KindIP) {
			panic(fmt.Sprintf("lists: catalog.json: набор %q", e.ID))
		}
	}
	return &c
}

// extendedSet возвращает набор встроенного каталога по id.
func extendedSet(id string) (ExtendedSet, bool) {
	for _, e := range catalog.Extended {
		if e.ID == id {
			return e, true
		}
	}
	return ExtendedSet{}, false
}

type loadedFile struct {
	info  FileInfo
	tags  []TagInfo
	upper map[string]int
}

func (f *loadedFile) names() []string {
	return append([]string{f.info.Name}, f.info.Aliases...)
}

func findRefs(files []*loadedFile, r catRef) []ListRef {
	var out []ListRef
	for _, f := range files {
		if f.info.Kind != r.Kind {
			continue
		}
		matched := false
		for _, n := range f.names() {
			if r.re.MatchString(n) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if i, ok := f.upper[strings.ToUpper(r.Tag)]; ok {
			out = append(out, ListRef{File: f.info.Name, Tag: f.tags[i].Tag, Kind: f.info.Kind})
		}
	}
	return out
}

func findAll(files []*loadedFile, refs []catRef) []ListRef {
	out := []ListRef{}
	seen := map[ListRef]bool{}
	for _, r := range refs {
		for _, lr := range findRefs(files, r) {
			if !seen[lr] {
				seen[lr] = true
				out = append(out, lr)
			}
		}
	}
	return out
}

// ipRank — порядок выбора geoip-файла для страны: файл XKeen (zkeenip) легче остальных.
func ipRank(name string) int {
	if strings.Contains(name, "zkeenip") {
		return 0
	}
	return 1
}

// Resolve собирает каталог по dat-файлам индекса и записям geodata xkeen.json.
// Сервис попадает в результат только с найденными ссылками, страна — только если
// у неё есть IP-список; порядок детерминирован.
func Resolve(ix *Index, geodata []GeodataEntry) (CatalogView, error) {
	infos, err := ix.Files()
	if err != nil {
		return CatalogView{}, err
	}
	files := make([]*loadedFile, 0, len(infos))
	for _, info := range infos {
		tags, err := ix.Tags(info.Name)
		if err != nil {
			if errors.Is(err, ErrCorruptDat) || errors.Is(err, ErrListNotFound) {
				continue
			}
			return CatalogView{}, err
		}
		lf := &loadedFile{info: info, tags: tags, upper: make(map[string]int, len(tags))}
		for i, t := range tags {
			lf.upper[strings.ToUpper(t.Tag)] = i
		}
		files = append(files, lf)
	}

	view := CatalogView{
		Services:  []ServiceView{},
		Lists:     []ListEntry{},
		Countries: []CountryView{},
		Extended:  []ExtendedSet{},
		Geodata:   GeodataStatus{Entries: append([]GeodataEntry{}, geodata...)},
	}

	for _, f := range files {
		for _, t := range f.tags {
			ex := t.Examples
			if ex == nil {
				ex = []string{}
			}
			view.Lists = append(view.Lists, ListEntry{File: f.info.Name, Tag: t.Tag, Kind: f.info.Kind, Count: t.Count, Examples: ex})
		}
	}

	sort.SliceStable(view.Lists, func(i, j int) bool {
		if view.Lists[i].File != view.Lists[j].File {
			return view.Lists[i].File < view.Lists[j].File
		}
		return view.Lists[i].Tag < view.Lists[j].Tag
	})

	for _, s := range catalog.Services {
		refs := findAll(files, s.Refs)
		if len(refs) == 0 {
			continue
		}
		view.Services = append(view.Services, ServiceView{ID: s.ID, NameRU: s.NameRU, NameEN: s.NameEN, Refs: refs})
	}

	ipFiles := make([]*loadedFile, 0, len(files))
	for _, f := range files {
		if f.info.Kind == KindIP {
			ipFiles = append(ipFiles, f)
		}
	}
	sort.SliceStable(ipFiles, func(i, j int) bool { return ipRank(ipFiles[i].info.Name) < ipRank(ipFiles[j].info.Name) })
	ipByCode := map[string]ListRef{}
	for _, f := range ipFiles {
		for _, t := range f.tags {
			if t.Count == 0 || !countryRe.MatchString(t.Tag) {
				continue
			}
			code := strings.ToUpper(t.Tag)
			if _, ok := ipByCode[code]; !ok {
				ipByCode[code] = ListRef{File: f.info.Name, Tag: t.Tag, Kind: KindIP}
			}
		}
	}
	spec := map[string]catCountry{}
	for _, c := range catalog.Countries {
		spec[strings.ToUpper(c.Code)] = c
	}
	codes := make([]string, 0, len(ipByCode))
	for code := range ipByCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		ip := ipByCode[code]
		cv := CountryView{Code: code, IP: &ip, Blocked: []ListRef{}}
		if c, ok := spec[code]; ok {
			if sites := findAll(files, c.Site); len(sites) > 0 {
				site := sites[0]
				cv.Site = &site
			}
			cv.Blocked = findAll(files, c.Blocked)
		}
		view.Countries = append(view.Countries, cv)
	}

	present := map[string]bool{}
	for _, f := range files {
		for _, n := range f.names() {
			present[n] = true
		}
	}
	inXKeen := map[string]bool{}
	for _, g := range geodata {
		inXKeen[g.File] = true
	}
	for _, e := range catalog.Extended {
		e.Installed = present[e.File]
		e.InXKeen = inXKeen[e.File]
		view.Extended = append(view.Extended, e)
	}
	return view, nil
}
