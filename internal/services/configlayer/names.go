package configlayer

import (
	"errors"
	"path/filepath"
	"strings"
)

// PanelPrefix — префикс имени файла, которым владеет панель. Через дефис:
// имена старого слоя (zz_xcp_*, xcp_hwid.txt) панели не принадлежат.
const PanelPrefix = "xcp-"

// MihomoPanelDirs — единственный список каталогов Mihomo, где живут файлы панели.
var MihomoPanelDirs = []string{"proxy_providers", "rule_providers", "profiles"}

// Roots — корни каталогов ядер: каталог конфигураций Xray и каталог Mihomo.
type Roots struct {
	Xray   string
	Mihomo string
}

// ErrInvalidPanelPath — путь не может быть превращён в путь на диске.
var ErrInvalidPanelPath = errors.New("недопустимый путь файла панели")

// Abs превращает путь внутри каталога ядра (в манифесте он хранится со
// слэшами) в путь на диске. Абсолютный путь, сегмент "..", пустой путь,
// неизвестное ядро и выход за корень дают ErrInvalidPanelPath. Для Xray
// путь — одно имя файла в корне каталога конфигураций, без подкаталогов.
func (r Roots) Abs(kernel, rel string) (string, error) {
	var root string
	switch kernel {
	case KernelXray:
		root = r.Xray
	case KernelMihomo:
		root = r.Mihomo
	default:
		return "", ErrInvalidPanelPath
	}
	if root == "" || rel == "" || strings.ContainsRune(rel, 0) {
		return "", ErrInvalidPanelPath
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", ErrInvalidPanelPath
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range segments {
		if seg == ".." || seg == "" || seg == "." {
			return "", ErrInvalidPanelPath
		}
	}
	if kernel == KernelXray && len(segments) != 1 {
		return "", ErrInvalidPanelPath
	}
	cleanRoot := filepath.Clean(root)
	abs := filepath.Join(cleanRoot, filepath.FromSlash(rel))
	if !strings.HasPrefix(abs, cleanRoot+string(filepath.Separator)) {
		return "", ErrInvalidPanelPath
	}
	return abs, nil
}

// ErrInvalidPanelName — имя не подпадает под правило файла панели.
var ErrInvalidPanelName = errors.New("имя не относится к файлам панели")

// ErrStoplistName — имя файла панели совпало со стоп-списком XKeen.
var ErrStoplistName = errors.New("имя файла совпадает со стоп-списком XKeen")

// IsPanelFileName — путь внутри каталога ядра принадлежит панели.
func IsPanelFileName(kernel, rel string) bool { return false }

// ValidatePanelName проверяет имя файла панели перед записью.
func ValidatePanelName(kernel, rel string) error { return nil }

// HasForbiddenTransport — в тексте есть запрещённый ключ transport.
func HasForbiddenTransport(content []byte) bool { return false }
