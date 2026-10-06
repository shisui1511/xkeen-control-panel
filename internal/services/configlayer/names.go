package configlayer

import "errors"

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

// Abs превращает путь внутри каталога ядра в путь на диске.
func (r Roots) Abs(kernel, rel string) (string, error) {
	return "", ErrInvalidPanelPath
}
