package services

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Зависимые профили (D-05): какие профили пользователя ссылаются на файлы
// источников панели proxy_providers/xcp-<id>.yaml. Чтение только для предупреждения
// в диалогах удаления источника и выключения слоя: содержимое профилей в состояние
// панели не переносится (U-02), файлы пользователя не правятся.

// ErrInvalidSourceID — идентификатор источника не my и не 8 шестнадцатеричных знаков.
var ErrInvalidSourceID = errors.New("invalid source id")

// maxDepsProfileBytes — предел чтения одного профиля при поиске ссылок: файл
// больше читается только началом (T-147.1-40).
const maxDepsProfileBytes = 2 << 20

// depsProviderFileRe — имя файла источника панели.
var depsProviderFileRe = regexp.MustCompile(`^xcp-[a-z0-9][a-z0-9-]*\.yaml$`)

// depsTextRefRe — запасной текстовый поиск, когда профиль не разбирается как YAML.
var depsTextRefRe = regexp.MustCompile(`(?:^|[\s"'/])proxy_providers/(xcp-[a-z0-9][a-z0-9-]*)\.yaml`)

// DependentProfiles возвращает имена профилей пользователя, ссылающихся на файл
// источника proxy_providers/xcp-<sourceID>.yaml; пустой sourceID — на любой файл
// xcp-*. Профили с зарезервированными именами (профиль панели и сироты) не
// просматриваются. Имена отсортированы; нет каталога профилей — пустой список.
func (s *MihomoProfileService) DependentProfiles(sourceID string) ([]string, error) {
	if sourceID != "" && !templateSourceIDRE.MatchString(sourceID) {
		return nil, ErrInvalidSourceID
	}
	entries, err := os.ReadDir(s.profilesDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || filepath.Ext(file) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(file, ".yaml")
		if !profileNameRe.MatchString(name) || isReservedProfileName(name) {
			continue
		}
		data, err := readProfileHead(filepath.Join(s.profilesDir(), file))
		if err != nil {
			// Профиль мог исчезнуть между чтением каталога и файла; предупреждению
			// это не мешает.
			continue
		}
		if s.profileRefersSource(data, sourceID) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// readProfileHead читает начало файла не больше maxDepsProfileBytes.
func readProfileHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxDepsProfileBytes))
}

// profileRefersSource — профиль ссылается на файл источника sourceID (пусто — на любой
// xcp-*). Сначала разбор yaml.v3 (proxy-providers.*.path), при ошибке разбора — поиск
// по тексту.
func (s *MihomoProfileService) profileRefersSource(data []byte, sourceID string) bool {
	want := ""
	if sourceID != "" {
		want = "xcp-" + sourceID
	}
	var doc struct {
		Providers map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		for _, m := range depsTextRefRe.FindAllStringSubmatch(string(data), -1) {
			if want == "" || m[1] == want {
				return true
			}
		}
		return false
	}
	dir := filepath.Join(s.dir, "proxy_providers")
	for _, p := range doc.Providers {
		if p.Path == "" {
			continue
		}
		path := filepath.Clean(p.Path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.dir, path)
		}
		if filepath.Dir(path) != dir {
			continue
		}
		base := filepath.Base(path)
		if !depsProviderFileRe.MatchString(base) {
			continue
		}
		if want == "" || base == want+".yaml" {
			return true
		}
	}
	return false
}
