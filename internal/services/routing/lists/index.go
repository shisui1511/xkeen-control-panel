package lists

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// DefaultDatDir — каталог dat-файлов XKeen (geo_dir).
const DefaultDatDir = "/opt/etc/xray/dat"

const (
	// KindSite — файл geosite (домены).
	KindSite = "site"
	// KindIP — файл geoip (сети).
	KindIP = "ip"
)

// readBufSize — размер буфера чтения dat (как у загрузчика ядра Xray).
const readBufSize = 64 * 1024

// maxElemBytes ограничивает один вложенный элемент (Domain, CIDR): настоящие
// значения — десятки байт, больше — повреждённый или подменённый файл.
const maxElemBytes = 64 * 1024

// maxCodeBytes ограничивает тег записи.
const maxCodeBytes = 1024

// kindProbeEntries — сколько записей просматривается при определении вида файла.
const kindProbeEntries = 64

// Ошибки пакета.
var (
	// ErrBadFileName — имя dat-файла не проходит проверку.
	ErrBadFileName = errors.New("недопустимое имя dat-файла")
	// ErrListNotFound — нет файла или тега в файле.
	ErrListNotFound = errors.New("список не найден")
	// ErrCorruptDat — файл не разбирается как dat.
	ErrCorruptDat = errors.New("повреждённый dat-файл")

	errStopWalk = errors.New("stop")
)

var datNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dat$`)

// FileInfo — dat-файл каталога. Пара симлинков XKeen (zkeen.dat и
// geosite_zkeen.dat) даёт одну запись: Name — реальный файл, Aliases — остальные имена.
type FileInfo struct {
	Name    string    `json:"name"`
	Aliases []string  `json:"aliases"`
	Kind    string    `json:"kind"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// TagInfo — тег файла: число записей и до трёх первых значений для каталога.
type TagInfo struct {
	Tag      string   `json:"tag"`
	Count    int      `json:"count"`
	Examples []string `json:"examples"`
}

const tagExamples = 3

type stamp struct {
	size int64
	mod  int64
}

func stampOf(fi os.FileInfo) stamp { return stamp{size: fi.Size(), mod: fi.ModTime().UnixNano()} }

type kindEntry struct {
	st   stamp
	kind string
}

type fileData struct {
	st    stamp
	kind  string
	tags  []TagInfo
	upper map[string]int
}

const (
	maxCachedCategories = 32
	maxCacheBytes       = 16 << 20
)

// Index — потоковый индекс dat-файлов каталога. Безопасен для параллельного использования.
type Index struct {
	dir string

	// scanMu сериализует чтение файлов: две одновременные выгрузки на роутере
	// удваивали бы нагрузку на диск и память.
	scanMu sync.Mutex

	mu       sync.Mutex
	kinds    map[string]kindEntry
	data     map[string]*fileData
	cats     map[string]Category
	catOrder []string
	catBytes int
}

// NewIndex создаёт индекс каталога dir (пусто — DefaultDatDir).
func NewIndex(dir string) *Index {
	if dir == "" {
		dir = DefaultDatDir
	}
	return &Index{
		dir:   dir,
		kinds: map[string]kindEntry{},
		data:  map[string]*fileData{},
		cats:  map[string]Category{},
	}
}

// Dir возвращает каталог индекса.
func (ix *Index) Dir() string { return ix.dir }

// resolve проверяет имя, раскрывает симлинки и возвращает реальное имя файла.
// Файл вне каталога (симлинк за его пределы) считается отсутствующим.
func (ix *Index) resolve(name string) (string, os.FileInfo, error) {
	if !datNameRe.MatchString(name) {
		return "", nil, fmt.Errorf("%w: %q", ErrBadFileName, name)
	}
	realDir, err := filepath.EvalSymlinks(ix.dir)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s", ErrListNotFound, name)
	}
	p, err := filepath.EvalSymlinks(filepath.Join(ix.dir, name))
	if err != nil || filepath.Dir(p) != realDir {
		return "", nil, fmt.Errorf("%w: %s", ErrListNotFound, name)
	}
	base := filepath.Base(p)
	if !datNameRe.MatchString(base) {
		return "", nil, fmt.Errorf("%w: %q", ErrBadFileName, base)
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%w: %s", ErrListNotFound, name)
	}
	return base, fi, nil
}

func (ix *Index) path(real string) string { return filepath.Join(ix.dir, real) }

// Files возвращает dat-файлы каталога: только *.dat с допустимым именем, пары-симлинки
// схлопнуты. Нет каталога — пустой список без ошибки.
func (ix *Index) Files() ([]FileInfo, error) {
	entries, err := os.ReadDir(ix.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []FileInfo{}, nil
		}
		return nil, err
	}
	groups := map[string]*FileInfo{}
	for _, de := range entries {
		name := de.Name()
		if !datNameRe.MatchString(name) {
			continue
		}
		real, fi, err := ix.resolve(name)
		if err != nil {
			continue
		}
		g := groups[real]
		if g == nil {
			g = &FileInfo{Name: real, Aliases: []string{}, Size: fi.Size(), ModTime: fi.ModTime()}
			groups[real] = g
		}
		if name != real {
			g.Aliases = append(g.Aliases, name)
		}
	}
	out := make([]FileInfo, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.Aliases)
		g.Kind = ix.kindOf(g.Name, g.Size, g.ModTime)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// kindHint — вид по имени; используется, только если разбор не дал ответа.
func kindHint(name string) string {
	l := strings.ToLower(name)
	if strings.Contains(l, "geoip") || strings.Contains(l, "zkeenip") {
		return KindIP
	}
	return KindSite
}

func (ix *Index) kindOf(real string, size int64, mod time.Time) string {
	st := stamp{size: size, mod: mod.UnixNano()}
	ix.mu.Lock()
	if k, ok := ix.kinds[real]; ok && k.st == st {
		ix.mu.Unlock()
		return k.kind
	}
	if d, ok := ix.data[real]; ok && d.st == st && d.kind != "" {
		ix.mu.Unlock()
		return d.kind
	}
	ix.mu.Unlock()

	kind := ""
	if f, err := os.Open(ix.path(real)); err == nil {
		kind = detectKind(f)
		_ = f.Close()
	}
	if kind == "" {
		kind = kindHint(real)
	}
	ix.mu.Lock()
	ix.kinds[real] = kindEntry{st: st, kind: kind}
	ix.mu.Unlock()
	return kind
}

// kindOfElem определяет вид по первому вложенному элементу: у CIDR поле 1 — байты
// адреса, у Domain поле 1 — varint типа (или сразу поле 2 — строка значения).
func kindOfElem(elem []byte) string {
	num, typ, n := protowire.ConsumeTag(elem)
	if n < 0 {
		return ""
	}
	switch {
	case num == 1 && typ == protowire.BytesType:
		return KindIP
	case num == 1 && typ == protowire.VarintType, num == 2 && typ == protowire.BytesType:
		return KindSite
	}
	return ""
}

func detectKind(r io.Reader) string {
	kind := ""
	seen := 0
	_ = walkEntries(r, func(_ string, e *entryBody) error {
		seen++
		for {
			num, typ, err := e.tag()
			if err != nil {
				break
			}
			if num == 2 && typ == protowire.BytesType {
				elem, err := e.bytes(maxElemBytes)
				if err != nil {
					return err
				}
				if k := kindOfElem(elem); k != "" {
					kind = k
					return errStopWalk
				}
				continue
			}
			if err := e.skip(typ); err != nil {
				return err
			}
		}
		if seen >= kindProbeEntries {
			return errStopWalk
		}
		return nil
	})
	return kind
}

// Signature возвращает для имён файлов строку "<размер>:<mtime unix>" — основа
// проверки изменений для Live и наблюдателя. Отсутствующие файлы пропускаются.
func (ix *Index) Signature(files []string) map[string]string {
	out := make(map[string]string, len(files))
	for _, name := range files {
		_, fi, err := ix.resolve(name)
		if err != nil {
			continue
		}
		out[name] = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().Unix())
	}
	return out
}

// Tags возвращает теги файла (имя может быть любым из имён пары) с числом записей
// и примерами. Файл читается потоково один раз на каждое состояние (размер, mtime).
func (ix *Index) Tags(file string) ([]TagInfo, error) {
	d, _, err := ix.load(file)
	if err != nil {
		return nil, err
	}
	out := make([]TagInfo, len(d.tags))
	copy(out, d.tags)
	return out, nil
}

// Has проверяет наличие тега в файле: имя файла — любое из пары, тег сравнивается без
// учёта регистра. Возвращает каноничные имя файла и тег. Нет файла или тега — ok=false.
func (ix *Index) Has(file, tag string) (canonicalFile, canonicalTag string, ok bool, err error) {
	d, real, err := ix.load(file)
	if err != nil {
		if errors.Is(err, ErrListNotFound) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	i, found := d.upper[strings.ToUpper(tag)]
	if !found {
		return "", "", false, nil
	}
	return real, d.tags[i].Tag, true, nil
}

// load возвращает (из кэша или потоковым проходом) данные файла.
func (ix *Index) load(file string) (*fileData, string, error) {
	real, fi, err := ix.resolve(file)
	if err != nil {
		return nil, "", err
	}
	st := stampOf(fi)
	if d := ix.cachedData(real, st); d != nil {
		return d, real, nil
	}
	ix.scanMu.Lock()
	defer ix.scanMu.Unlock()
	if d := ix.cachedData(real, st); d != nil {
		return d, real, nil
	}
	f, err := os.Open(ix.path(real))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrListNotFound, file)
	}
	defer f.Close()
	d, err := scanTags(f)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrCorruptDat, real, err)
	}
	d.st = st
	if d.kind == "" {
		d.kind = kindHint(real)
	}
	ix.mu.Lock()
	ix.data[real] = d
	ix.kinds[real] = kindEntry{st: st, kind: d.kind}
	ix.mu.Unlock()
	return d, real, nil
}

func (ix *Index) cachedData(real string, st stamp) *fileData {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if d, ok := ix.data[real]; ok && d.st == st {
		return d
	}
	return nil
}

// scanTags — один потоковый проход: теги, число записей, примеры, вид файла.
func scanTags(r io.Reader) (*fileData, error) {
	d := &fileData{upper: map[string]int{}}
	type rawTag struct {
		count int
		elems [][]byte
	}
	byIdx := map[int]*rawTag{}
	err := walkEntries(r, func(code string, e *entryBody) error {
		key := strings.ToUpper(code)
		i, ok := d.upper[key]
		if !ok {
			i = len(d.tags)
			d.upper[key] = i
			d.tags = append(d.tags, TagInfo{Tag: code, Examples: []string{}})
			byIdx[i] = &rawTag{}
		}
		rt := byIdx[i]
		for {
			num, typ, err := e.tag()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if num == 2 && typ == protowire.BytesType {
				rt.count++
				if len(rt.elems) < tagExamples {
					elem, err := e.bytes(maxElemBytes)
					if err != nil {
						return err
					}
					rt.elems = append(rt.elems, elem)
					if d.kind == "" {
						d.kind = kindOfElem(elem)
					}
					continue
				}
				if err := e.skipBytes(); err != nil {
					return err
				}
				continue
			}
			if err := e.skip(typ); err != nil {
				return err
			}
		}
	})
	if err != nil {
		return nil, err
	}
	for i := range d.tags {
		rt := byIdx[i]
		d.tags[i].Count = rt.count
		for _, elem := range rt.elems {
			if s, ok := formatElem(d.kind, elem); ok {
				d.tags[i].Examples = append(d.tags[i].Examples, s)
			}
		}
	}
	return d, nil
}

// formatElem — значение вложенного элемента для показа: домен либо сеть.
func formatElem(kind string, elem []byte) (string, bool) {
	if kind == KindIP {
		p, ok := parseCIDR(elem)
		if !ok {
			return "", false
		}
		return p.String(), true
	}
	_, v, ok := parseDomain(elem)
	return v, ok && v != ""
}

// parseDomain разбирает Domain{type=1, value=2}; атрибуты игнорируются.
func parseDomain(b []byte) (typ uint64, value string, ok bool) {
	for len(b) > 0 {
		num, wt, n := protowire.ConsumeTag(b)
		if n < 0 {
			return 0, "", false
		}
		b = b[n:]
		switch {
		case num == 1 && wt == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return 0, "", false
			}
			typ = v
			b = b[m:]
		case num == 2 && wt == protowire.BytesType:
			s, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return 0, "", false
			}
			value = string(s)
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, wt, b)
			if m < 0 {
				return 0, "", false
			}
			b = b[m:]
		}
	}
	return typ, value, true
}

// parseCIDR разбирает CIDR{ip=1 (4 или 16 байт), prefix=2}.
func parseCIDR(b []byte) (netip.Prefix, bool) {
	var ip []byte
	var bits uint64
	for len(b) > 0 {
		num, wt, n := protowire.ConsumeTag(b)
		if n < 0 {
			return netip.Prefix{}, false
		}
		b = b[n:]
		switch {
		case num == 1 && wt == protowire.BytesType:
			s, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return netip.Prefix{}, false
			}
			ip = s
			b = b[m:]
		case num == 2 && wt == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return netip.Prefix{}, false
			}
			bits = v
			b = b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, wt, b)
			if m < 0 {
				return netip.Prefix{}, false
			}
			b = b[m:]
		}
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || (len(ip) != 4 && len(ip) != 16) || bits > uint64(addr.BitLen()) {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, int(bits)), true
}

// --- потоковое чтение protobuf ---

type pbReader struct{ br *bufio.Reader }

// varint читает varint из потока; возвращает значение и число прочитанных байт.
func (p *pbReader) varint() (uint64, int64, error) {
	var buf [binary.MaxVarintLen64]byte
	for i := range buf {
		b, err := p.br.ReadByte()
		if err != nil {
			if i > 0 && errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return 0, 0, err
		}
		buf[i] = b
		if b < 0x80 {
			v, n := protowire.ConsumeVarint(buf[:i+1])
			if n < 0 {
				return 0, 0, protowire.ParseError(n)
			}
			return v, int64(n), nil
		}
	}
	return 0, 0, ErrCorruptDat
}

func (p *pbReader) discard(n int64) error {
	if n < 0 {
		return ErrCorruptDat
	}
	_, err := io.CopyN(io.Discard, p.br, n)
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// entryBody — тело одной записи внешнего сообщения; rem — сколько байт тела не прочитано.
type entryBody struct {
	p   *pbReader
	rem int64
}

// tag читает тег поля; io.EOF — тело закончилось.
func (e *entryBody) tag() (protowire.Number, protowire.Type, error) {
	if e.rem <= 0 {
		return 0, 0, io.EOF
	}
	v, n, err := e.p.varint()
	if err != nil {
		return 0, 0, err
	}
	e.rem -= n
	num, typ := protowire.DecodeTag(v)
	if num < 1 || e.rem < 0 {
		return 0, 0, ErrCorruptDat
	}
	return num, typ, nil
}

func (e *entryBody) length() (int64, error) {
	v, n, err := e.p.varint()
	if err != nil {
		return 0, err
	}
	e.rem -= n
	if e.rem < 0 || v > uint64(e.rem) {
		return 0, ErrCorruptDat
	}
	return int64(v), nil
}

// bytes читает поле типа bytes целиком (не больше max).
func (e *entryBody) bytes(max int64) ([]byte, error) {
	l, err := e.length()
	if err != nil {
		return nil, err
	}
	if l > max {
		return nil, ErrCorruptDat
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(e.p.br, buf); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	e.rem -= l
	return buf, nil
}

// skipBytes пропускает поле типа bytes без чтения в память.
func (e *entryBody) skipBytes() error {
	l, err := e.length()
	if err != nil {
		return err
	}
	if err := e.p.discard(l); err != nil {
		return err
	}
	e.rem -= l
	return nil
}

// skip пропускает значение поля после его тега.
func (e *entryBody) skip(typ protowire.Type) error {
	switch typ {
	case protowire.VarintType:
		_, n, err := e.p.varint()
		e.rem -= n
		return err
	case protowire.Fixed32Type:
		e.rem -= 4
		return e.p.discard(4)
	case protowire.Fixed64Type:
		e.rem -= 8
		return e.p.discard(8)
	case protowire.BytesType:
		return e.skipBytes()
	}
	return ErrCorruptDat
}

func (e *entryBody) discardRest() error {
	n := e.rem
	e.rem = 0
	return e.p.discard(n)
}

// walkEntries вызывает visit для каждой записи внешнего сообщения (GeoSiteList или
// GeoIPList, поле 1). Тег записи (country_code, поле 1 тела) читается до visit; всё,
// что visit не дочитал, пропускается без загрузки в память. errStopWalk из visit
// останавливает обход без ошибки.
func walkEntries(r io.Reader, visit func(code string, e *entryBody) error) error {
	p := &pbReader{br: bufio.NewReaderSize(r, readBufSize)}
	for {
		v, _, err := p.varint()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		num, typ := protowire.DecodeTag(v)
		if num < 1 {
			return ErrCorruptDat
		}
		if num != 1 || typ != protowire.BytesType {
			if err := skipTop(p, typ); err != nil {
				return err
			}
			continue
		}
		l, _, err := p.varint()
		if err != nil {
			return err
		}
		if l > 1<<40 {
			return ErrCorruptDat
		}
		e := &entryBody{p: p, rem: int64(l)}
		code := ""
		for {
			n, t, err := e.tag()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if n == 1 && t == protowire.BytesType {
				b, err := e.bytes(maxCodeBytes)
				if err != nil {
					return err
				}
				code = string(b)
				break
			}
			if err := e.skip(t); err != nil {
				return err
			}
		}
		verr := visit(code, e)
		if verr != nil && !errors.Is(verr, errStopWalk) {
			return verr
		}
		if verr != nil {
			return nil
		}
		if err := e.discardRest(); err != nil {
			return err
		}
	}
}

func skipTop(p *pbReader, typ protowire.Type) error {
	switch typ {
	case protowire.VarintType:
		_, _, err := p.varint()
		return err
	case protowire.Fixed32Type:
		return p.discard(4)
	case protowire.Fixed64Type:
		return p.discard(8)
	case protowire.BytesType:
		l, _, err := p.varint()
		if err != nil {
			return err
		}
		if l > 1<<40 {
			return ErrCorruptDat
		}
		return p.discard(int64(l))
	}
	return ErrCorruptDat
}
