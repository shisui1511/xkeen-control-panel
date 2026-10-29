package lists

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultMihomoDir — каталог данных Mihomo.
const DefaultMihomoDir = "/opt/etc/mihomo"

// MihomoFile — файл геоданных Mihomo. Панель его не читает и не меняет:
// только имя, размер и время изменения.
type MihomoFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

// mihomoDatNames — имена dat-файлов Mihomo (регистр не учитывается).
var mihomoDatNames = map[string]bool{
	"geosite.dat": true,
	"geoip.dat":   true,
}

func isMihomoGeoFile(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".mmdb") || strings.HasSuffix(l, ".metadb") || mihomoDatNames[l]
}

// MihomoFiles перечисляет файлы геоданных каталога Mihomo, по имени. Нет каталога —
// пустой список без ошибки. Содержимое файлов не открывается.
func MihomoFiles(dir string) ([]MihomoFile, error) {
	if dir == "" {
		dir = DefaultMihomoDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []MihomoFile{}, nil
		}
		return nil, err
	}
	out := []MihomoFile{}
	for _, de := range entries {
		name := de.Name()
		if !isMihomoGeoFile(name) {
			continue
		}
		// Stat (не Lstat): симлинк на файл вне каталога тоже показывается по размеру цели.
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		out = append(out, MihomoFile{Name: name, Size: fi.Size(), ModTime: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
