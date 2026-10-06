package configlayer

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

// Правила имён. Форма Xray проверена трассером 143: файл
// 04_outbounds.xcp-<id>.tail.json принимается XKeen и подхватывается Xray;
// сегменты NN_роль. и .tail необязательны. Имена старого слоя (zz_xcp_*)
// под правило не попадают: после префикса обязателен дефис.
var (
	xrayPanelName   = regexp.MustCompile(`^(?:[0-9]{2}_[a-z0-9]+\.|[0-9]{2}_)?xcp-[a-z0-9][a-z0-9-]*(?:\.tail)?\.json$`)
	mihomoPanelName = regexp.MustCompile(`^xcp-[a-z0-9][a-z0-9-]*\.yaml$`)

	// forbiddenTransport — тот же шаблон, что у XKeen: он ищет ключ текстом
	// (`grep -qE`, 02_install_xray.sh), JSON не разбирается.
	forbiddenTransport = regexp.MustCompile(`"transport"[[:space:]]*:`)
)

// IsPanelFileName — путь внутри каталога ядра принадлежит панели: для Xray
// имя в корне каталога конфигураций, для Mihomo имя в одном из каталогов
// MihomoPanelDirs.
func IsPanelFileName(kernel, rel string) bool {
	switch kernel {
	case KernelXray:
		return !strings.Contains(rel, "/") && xrayPanelName.MatchString(rel)
	case KernelMihomo:
		dir, name := path.Split(rel)
		if dir == "" || !slices.Contains(MihomoPanelDirs, strings.TrimSuffix(dir, "/")) {
			return false
		}
		return mihomoPanelName.MatchString(name)
	}
	return false
}

// ValidatePanelName проверяет имя файла панели перед записью: оно должно
// подпадать под правило панели и не совпадать со стоп-списком XKeen (иначе
// XKeen отменит запуск Xray). Имя не заменяется и другое не подбирается:
// совпадение со стоп-списком — ошибка сборки со словом.
func ValidatePanelName(kernel, rel string) error {
	if !IsPanelFileName(kernel, rel) {
		return fmt.Errorf("%w: %s", ErrInvalidPanelName, rel)
	}
	base := path.Base(rel)
	// Идентификаторы у ядер одинаковые, поэтому имя Mihomo проверяется как
	// <основа>.json: так фрагмент Xray с тем же ID не нарушит стоп-список.
	probe := base
	if kernel == KernelMihomo {
		probe = strings.TrimSuffix(base, ".yaml") + ".json"
	}
	if word, hit := utils.XKeenStoplistMatch(probe); hit {
		return fmt.Errorf("%w: %s (%s)", ErrStoplistName, base, word)
	}
	return nil
}

// HasForbiddenTransport — в тексте есть ключ "transport" перед двоеточием
// (пробелы допускаются, вложенность не важна). XKeen при таком ключе в корне
// каталога конфигураций отменяет запуск Xray.
func HasForbiddenTransport(content []byte) bool {
	return forbiddenTransport.Match(content)
}
