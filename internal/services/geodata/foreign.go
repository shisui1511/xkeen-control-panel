package geodata

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// ErrForeignRef — на файл ссылаются чужие конфиги: удаление источника блокируется
// (код foreign_ref, D-21). Места ссылок несёт *ForeignRefError.
var ErrForeignRef = &lists.CodedError{Code: "foreign_ref"}

// ForeignRef — место ссылки на файл dat в конфиге вне манифеста панели. Editor —
// абсолютный путь для ссылки «Открыть в Редакторе». Line == 0 — файл или каталог не
// прочитан (ошибка чтения, не обычный файл, больше foreignMaxSize): ссылка не
// исключена, удаление блокируется.
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

// ImpactView — влияние удаления или выключения файла: списки с правилами черновика
// и свои правила ядра (routing.GeodataImpact) плюс места ссылок из чужих конфигов
// (D-21). Непустой Foreign блокирует удаление.
type ImpactView struct {
	routing.GeodataImpact
	Foreign []ForeignRef `json:"foreign"`
}

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
//
// Поиск закрывается при ошибке (WR-05): конфиг или каталог, который не удалось
// прочитать, даёт место со строкой 0 (foreignUnreadable). Такое место блокирует
// удаление наравне с найденной ссылкой, потому что ядро упадёт на ext:<файл>:, если
// ссылка в нечитаемом файле есть. Файл, исчезнувший между чтением каталога и
// проверкой, ссылкой не считается.
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
		if err != nil && errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > foreignMaxSize {
			seen[real] = true
			refs = append(refs, foreignUnreadable(rel, abs))
			return
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			seen[real] = true
			refs = append(refs, foreignUnreadable(rel, abs))
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
	list := func(dir, rel string, exts ...string) []string {
		names, err := foreignList(dir, exts...)
		if err != nil {
			refs = append(refs, foreignUnreadable(rel, ""))
		}
		return names
	}
	for _, name := range list(s.d.XrayConfDir, filepath.Base(s.d.XrayConfDir)+"/", ".json") {
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
	for _, name := range list(s.d.MihomoDir, filepath.Base(s.d.MihomoDir)+"/", ".yaml", ".yml") {
		scan(s.d.MihomoDir, name, mihomoMatch)
	}
	profiles := filepath.Join(s.d.MihomoDir, "profiles")
	for _, name := range list(profiles, "profiles/", ".yaml", ".yml") {
		scan(s.d.MihomoDir, "profiles/"+name, mihomoMatch)
	}
	return refs, nil
}

// foreignUnreadable — место «не прочитан»: строка 0, ссылка не исключена. abs пуст у
// каталога (открывать в Редакторе нечего).
func foreignUnreadable(rel, abs string) ForeignRef {
	return ForeignRef{File: rel, Line: 0, Editor: abs}
}

// foreignList — имена файлов каталога dir (без подкаталогов) с одним из расширений,
// по алфавиту. Нет каталога — пусто без ошибки; любая другая ошибка чтения каталога
// возвращается вместе с тем, что успели прочитать.
func foreignList(dir string, exts ...string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
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
	return out, err
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

// errNoRouting — сервис маршрутизации не передан в Deps.
var errNoRouting = errors.New("geodata: не задан сервис маршрутизации")

// Impact показывает, что затронет удаление или выключение файла: влияние на правила
// черновика и места ссылок в чужих конфигах. Ничего не меняет.
func (s *Service) Impact(file string) (ImpactView, error) {
	if s.d.Routing == nil {
		return ImpactView{}, errNoRouting
	}
	imp, err := s.d.Routing.GeodataImpact(file)
	if err != nil {
		return ImpactView{}, err
	}
	refs, err := s.ForeignRefs(file)
	if err != nil {
		return ImpactView{}, err
	}
	return ImpactView{GeodataImpact: imp, Foreign: refs}, nil
}

// Drop помечает источник на удаление или выключение вместе с каскадом правил
// (routing.GeodataDrop). Чужие конфиги со ссылкой на файл блокируют пометку: ошибка
// *ForeignRefError с местами ссылок, черновик не меняется (T-146.1-22). Файл и запись
// убираются только после «Применить» (AfterApply).
func (s *Service) Drop(rev int64, file, mode string, actions []routing.GeodataAction) (configlayer.DraftEvent, error) {
	if s.d.Routing == nil {
		return configlayer.DraftEvent{}, errNoRouting
	}
	refs, err := s.ForeignRefs(file)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if len(refs) > 0 {
		return configlayer.DraftEvent{}, &ForeignRefError{Refs: refs}
	}
	ev, err := s.d.Routing.GeodataDrop(rev, file, mode, actions)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	s.changed()
	return ev, nil
}

// Restore снимает пометку удаления с файла («Вернуть источник»); правки правил
// остаются в черновике до «Сбросить».
func (s *Service) Restore(rev int64, file string) (configlayer.DraftEvent, error) {
	if s.d.Routing == nil {
		return configlayer.DraftEvent{}, errNoRouting
	}
	ev, err := s.d.Routing.GeodataRestore(rev, file)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	s.changed()
	return ev, nil
}
