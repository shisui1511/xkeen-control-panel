package geodata

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// ErrForeignRef — на файл ссылаются чужие конфиги: удаление источника блокируется
// (код foreign_ref, D-21). Места ссылок несёт *ForeignRefError.
var ErrForeignRef = &lists.CodedError{Code: "foreign_ref"}

// ForeignRef — место ссылки на файл dat в конфиге вне манифеста панели. Editor —
// абсолютный путь для ссылки «Открыть в Редакторе».
type ForeignRef struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Editor string `json:"editor,omitempty"`
}

// ForeignRefError — удаление отклонено: файл нужен чужим конфигам. Через errors.Is
// совпадает с ErrForeignRef, поэтому lists.ErrorCode даёт foreign_ref.
type ForeignRefError struct{ Refs []ForeignRef }

func (e *ForeignRefError) Error() string { return ErrForeignRef.Code }

// Unwrap связывает ошибку с кодом foreign_ref.
func (e *ForeignRefError) Unwrap() error { return ErrForeignRef }

// foreignMaxSize — самый большой конфиг, который просматривается на ссылки.
const foreignMaxSize = 16 << 20

// removeNames — все имена файла: он сам и второе имя пары (zkeen.dat и
// geosite_zkeen.dat — один файл). Имена берутся по таблице стандартных наборов и по
// алиасам индекса; имя, не прошедшее проверку имени dat, отбрасывается.
func (s *Service) removeNames(file string) []string {
	set := map[string]bool{}
	add := func(n string) {
		if datNameRe.MatchString(n) {
			set[n] = true
		}
	}
	add(file)
	if std, ok := standardByName(file); ok {
		add(std.File)
		add(std.Alias)
	}
	if files, err := s.d.Index.Files(); err == nil {
		for _, f := range files {
			match := f.Name == file
			for _, a := range f.Aliases {
				match = match || a == file
			}
			if !match {
				continue
			}
			add(f.Name)
			for _, a := range f.Aliases {
				add(a)
			}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ForeignRefs ищет ссылки на файл (и на второе имя пары zkeen) в чужих конфигах:
//   - Xray: файлы XrayConfDir/*.json вне манифеста панели, подстрока ext:<имя>: (в том
//     числе 05_routing.json XKeen, пока маршрутизация не взята, и смешанные файлы);
//   - Mihomo: профили *.yaml и *.yml в корне MihomoDir и в profiles/ вне манифеста,
//     вхождение имени файла (A5).
//
// Файл, достижимый по нескольким путям (симлинк config.yaml на профиль), считается
// один раз. Нет каталога — нет ссылок.
func (s *Service) ForeignRefs(file string) ([]ForeignRef, error) {
	names := s.removeNames(file)
	refs := []ForeignRef{}
	if len(names) == 0 {
		return refs, nil
	}
	seen := map[string]bool{}
	scan := func(dir, rel string, match func(line string) bool) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if s.d.Layer.IsManagedPath(abs) {
			return
		}
		real := abs
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			real = r
		}
		if seen[real] {
			return
		}
		fi, err := os.Stat(abs)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > foreignMaxSize {
			return
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		seen[real] = true
		for i, line := range strings.Split(string(data), "\n") {
			if match(line) {
				refs = append(refs, ForeignRef{File: rel, Line: i + 1, Editor: abs})
			}
		}
	}

	xrayMatch := func(line string) bool {
		for _, n := range names {
			if strings.Contains(line, "ext:"+n+":") {
				return true
			}
		}
		return false
	}
	for _, name := range foreignList(s.d.XrayConfDir, ".json") {
		scan(s.d.XrayConfDir, name, xrayMatch)
	}

	mihomoMatch := func(line string) bool {
		for _, n := range names {
			if containsDatName(line, n) {
				return true
			}
		}
		return false
	}
	for _, name := range foreignList(s.d.MihomoDir, ".yaml", ".yml") {
		scan(s.d.MihomoDir, name, mihomoMatch)
	}
	profiles := filepath.Join(s.d.MihomoDir, "profiles")
	for _, name := range foreignList(profiles, ".yaml", ".yml") {
		scan(s.d.MihomoDir, "profiles/"+name, mihomoMatch)
	}
	return refs, nil
}

// foreignList — имена обычных файлов каталога dir (без подкаталогов) с одним из
// расширений, по алфавиту. Нет каталога — пусто.
func foreignList(dir string, exts ...string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		for _, ext := range exts {
			if strings.HasSuffix(name, ext) {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// containsDatName — строка содержит имя файла целиком: до и после имени нет знака
// имени файла (a.dat не находится в data.dat).
func containsDatName(line, name string) bool {
	isName := func(b byte) bool {
		return b == '.' || b == '_' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	}
	for from := 0; ; {
		i := strings.Index(line[from:], name)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(name)
		if (start == 0 || !isName(line[start-1])) && (end == len(line) || !isName(line[end])) {
			return true
		}
		from = start + 1
	}
}
