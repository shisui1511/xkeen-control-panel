package configlayer

import (
	"bytes"
	"crypto/md5" //nolint:gosec // контроль целостности по D-09, не защита от злоумышленника
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// obsoleteSuffix — суффикс, который XKeen и пользователь дописывают к файлу,
// выводя его из работы: xcp-a.json → xcp-a.json.obsolete (D-09).
const obsoleteSuffix = ".obsolete"

// Коды проблем содержимого провайдера Mihomo.
const (
	problemYAMLInvalid = "provider_yaml_invalid"
	problemMissingKey  = "provider_missing_key"
	problemEmpty       = "provider_empty"
)

// FileState — результат сверки записи манифеста с диском.
type FileState string

// Состояния файла.
const (
	StateOK            FileState = "ok"
	StatePending       FileState = "pending"
	StateDriftModified FileState = "drift_modified"
	StateDriftMissing  FileState = "drift_missing"
	StateDriftRenamed  FileState = "drift_renamed"
	StateDriftEmpty    FileState = "drift_empty"
	StateReleased      FileState = "released"
)

// FileCheck — итог сверки одной записи манифеста.
type FileCheck struct {
	Key          string
	Kernel       string
	RelPath      string
	AbsPath      string
	Kind         FileKind
	State        FileState
	ObsoleteName string
	ActualHash   string
}

// IsDrift — состояние означает расхождение с тем, что записала панель.
func (s FileState) IsDrift() bool {
	switch s {
	case StateDriftModified, StateDriftMissing, StateDriftRenamed, StateDriftEmpty:
		return true
	}
	return false
}

// HashContent возвращает md5 содержимого в hex. Это контроль целостности по
// D-09 (файл изменили снаружи или нет), а не мера безопасности. Если CodeQL
// заменит алгоритм на sha256, схема не меняется: поле называется hash.
func HashContent(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // см. комментарий выше
	return hex.EncodeToString(sum[:])
}

// providerKey — обязательный ключ файла-провайдера Mihomo по виду файла.
func providerKey(kind FileKind) (string, bool) {
	switch kind {
	case KindMihomoProxyProvider:
		return "proxies", true
	case KindMihomoRuleProvider:
		return "payload", true
	}
	return "", false
}

// ProviderContentProblem проверяет содержимое провайдера Mihomo: пустой файл,
// неразбираемый YAML, нет обязательного ключа (proxies или payload) или список
// пуст. Пустой file-провайдер Mihomo не загружает, поэтому это дрейф. Для
// остальных видов возвращается "": JSON Xray проверяет сборка (144-04).
func ProviderContentProblem(kind FileKind, content []byte) string {
	key, ok := providerKey(kind)
	if !ok {
		return ""
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return problemEmpty
	}
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return problemYAMLInvalid
	}
	value, present := doc[key]
	if !present {
		return problemMissingKey
	}
	switch v := value.(type) {
	case nil:
		return problemEmpty
	case []any:
		if len(v) == 0 {
			return problemEmpty
		}
		return ""
	default:
		return problemYAMLInvalid
	}
}

// CheckEntry сверяет запись манифеста с диском (D-09, D-10). Порядок проверок:
// отпущенная запись (диск не читается); файла нет — переименован в .obsolete
// или пропал; хэш совпал — ok; провайдер с проблемой содержимого — пустой;
// иначе изменён.
func CheckEntry(roots Roots, e ManifestEntry) FileCheck {
	fc := FileCheck{
		Key:     ManifestKey(e.Kernel, e.RelPath),
		Kernel:  e.Kernel,
		RelPath: e.RelPath,
		Kind:    e.Kind,
	}
	if e.Status == StatusReleased {
		fc.State = StateReleased
		return fc
	}
	abs, err := roots.Abs(e.Kernel, e.RelPath)
	if err != nil {
		fc.State = StateDriftModified
		return fc
	}
	fc.AbsPath = abs
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if _, statErr := os.Stat(abs + obsoleteSuffix); statErr == nil {
				fc.State = StateDriftRenamed
				fc.ObsoleteName = filepath.Base(abs) + obsoleteSuffix
				return fc
			}
			fc.State = StateDriftMissing
			return fc
		}
		fc.State = StateDriftModified
		return fc
	}
	fc.ActualHash = HashContent(data)
	switch {
	case fc.ActualHash == e.Hash:
		fc.State = StateOK
	case ProviderContentProblem(e.Kind, data) != "":
		fc.State = StateDriftEmpty
	default:
		fc.State = StateDriftModified
	}
	return fc
}

// CheckManifest сверяет весь манифест; результат отсортирован по Key.
func CheckManifest(roots Roots, m map[string]ManifestEntry) []FileCheck {
	out := make([]FileCheck, 0, len(m))
	for _, e := range m {
		out = append(out, CheckEntry(roots, e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// OrphanFile — файл с именем панели, которого нет в манифесте.
type OrphanFile struct {
	Key     string
	Kernel  string
	RelPath string
	AbsPath string
}

// ScanOrphans ищет сирот: файлы с именем панели в сканируемых каталогах,
// которых нет в манифесте. Корень Xray сканируется на верхнем уровне, Mihomo —
// только каталоги MihomoPanelDirs; корень Mihomo не сканируется (там лежит
// config.yaml.xcp-link старого слоя). foreignOwned (может быть nil)
// исключает файлы, принадлежащие старому слою (D-13). Отсутствующий каталог
// ошибкой не считается. Сироты только находятся: перенос в резервную копию
// делает конвейер (144-06).
func ScanOrphans(roots Roots, m map[string]ManifestEntry, foreignOwned func(kernel, rel string) bool) ([]OrphanFile, error) {
	var out []OrphanFile
	scan := func(kernel, dirAbs, relPrefix string) error {
		entries, err := os.ReadDir(dirAbs)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, de := range entries {
			if de.Type()&fs.ModeDir != 0 {
				continue
			}
			rel := relPrefix + de.Name()
			if !IsPanelFileName(kernel, rel) {
				continue
			}
			key := ManifestKey(kernel, rel)
			if _, known := m[key]; known {
				continue
			}
			if foreignOwned != nil && foreignOwned(kernel, rel) {
				continue
			}
			out = append(out, OrphanFile{Key: key, Kernel: kernel, RelPath: rel, AbsPath: filepath.Join(dirAbs, de.Name())})
		}
		return nil
	}
	if roots.Xray != "" {
		if err := scan(KernelXray, roots.Xray, ""); err != nil {
			return nil, err
		}
	}
	if roots.Mihomo != "" {
		for _, dir := range MihomoPanelDirs {
			if err := scan(KernelMihomo, filepath.Join(roots.Mihomo, dir), dir+"/"); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
