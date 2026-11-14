package sources

import (
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// CountUsableNodes считает рабочие узлы и источники в наборе секций (черновик
// или применённое) по живым данным слоя — для шага «узлы» мастера первого
// запуска (149.1, D-04.3).
//
// Подписка считается источником всегда, а узлы даёт только включённая: максимум
// Usable снимка (ожидающего, иначе применённого) по установленным ядрам; набор не
// скачан — 0. «Мои узлы» — источник при непустом списке, узлы — число записей у
// включённого. Повреждённые живые данные дают подпискам 0 узлов, как у View:
// настройки источников они не скрывают.
//
// Функция чистая: файлы снимков узлов не читаются (чтение снимков в View —
// секунды на слабых роутерах), ввода-вывода нет, поэтому её можно звать под
// замком хранилища слоя (configlayer.WizardProbe).
func CountUsableNodes(sections, live configlayer.Sections, installed configlayer.InstalledKernels) (nodes, sources int) {
	ls, err := readLive(live)
	if err != nil {
		ls = LiveSources{}
	}
	for name, raw := range sections {
		id, ok := sourceID(name)
		if !ok {
			continue
		}
		switch kindOf(raw) {
		case KindSubscription:
			if id == MySourceID {
				continue
			}
			cfg, err := parseSubscription(raw)
			if err != nil {
				continue
			}
			sources++
			if cfg.Enabled {
				nodes += usableOf(ls[id], installed)
			}
		case KindMy:
			if id != MySourceID {
				continue
			}
			cfg, err := parseMy(raw)
			if err != nil || len(cfg.Entries) == 0 {
				continue
			}
			sources++
			if cfg.Enabled {
				nodes += len(cfg.Entries)
			}
		}
	}
	return nodes, sources
}

// usableOf — рабочие узлы подписки: максимум по установленным ядрам Usable
// показываемого снимка; снимка нет ни у одного ядра — 0.
func usableOf(src *LiveSource, installed configlayer.InstalledKernels) int {
	if src == nil {
		return 0
	}
	best := 0
	for _, k := range previewKernels {
		if !installed.Has(k) {
			continue
		}
		if ref := displayRef(src.Kernels[k]); ref != nil && ref.Usable > best {
			best = ref.Usable
		}
	}
	return best
}
