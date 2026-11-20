package services

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// ErrXKeenStoplistName — имя файла панели совпало со стоп-списком XKeen:
// init-скрипт отменит запуск Xray, если такой файл лежит в корне каталога
// конфигураций.
var ErrXKeenStoplistName = errors.New("file name matches the XKeen stop-list")

// guardXrayRootName проверяет имя файла, который панель собирается записать
// в корень каталога Xray. Вызывается перед записью своих файлов; откаты к
// прежнему содержимому и удаления не проверяются.
func guardXrayRootName(path string) error {
	base := filepath.Base(path)
	if word, hit := utils.XKeenStoplistMatch(base); hit {
		return fmt.Errorf("%w: %s (%s)", ErrXKeenStoplistName, base, word)
	}
	return nil
}
