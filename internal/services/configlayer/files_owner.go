package configlayer

import (
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Владелец файла во вкладке «Файлы» (D-05): те же значения, что у владельцев
// секций ядер (OwnerXKeen, OwnerUser), плюс файл панели.
const OwnerPanel = "panel"

// xkeenConfigFile — файл настроек XKeen в его каталоге: владелец — XKeen.
const xkeenConfigFile = "xkeen.json"

// FileOwnerInfo — владелец файла и секции, которые панель может взять из него
// (D-03, D-05).
type FileOwnerInfo struct {
	// Owner — panel (файл манифеста, не отпущенный), xkeen (шаблоны XKeen,
	// config.yaml Mihomo, xkeen.json) или user (остальное и отпущенные файлы).
	Owner string
	// TakeKernel — ядро, секции которого панель может взять из файла: xray или
	// mihomo; пусто — взять нечего.
	TakeKernel string
	// TakeSections — идентификаторы секций ядра, которые панель ещё не держит.
	TakeSections []string
}

// FileOwnerIndex — снимок того, что нужно для определения владельца файлов:
// манифест, скан каталога Xray и активный профиль Mihomo. Строится один раз на
// список файлов: скан каталога Xray читает каждый файл, повторять его на каждый
// файл списка незачем. Только читает снимок состояния и диск, замок применения не
// берёт.
type FileOwnerIndex struct {
	enabled bool
	// managed и released — развёрнутые пути файлов манифеста по статусу.
	managed  map[string]bool
	released map[string]bool
	// xrayRoot, mihomoRoot, xkeenRoot — корни в развёрнутом виде.
	xrayRoot   string
	mihomoRoot string
	xkeenRoot  string
	// xrayTake — для имени чужого файла каталога Xray: секции, которые панель
	// может взять из него (ядро Xray установлено).
	xrayTake map[string][]string
	// mihomoActive — развёрнутый путь активного профиля Mihomo, который панель
	// может взять (не профиль панели); mihomoTake — его секции.
	mihomoActive string
	mihomoTake   []string
}

// FileOwnerIndex строит индекс владельцев. Выключенный слой — пустой индекс:
// FileOwner у него возвращает пустого владельца.
func (l *Layer) FileOwnerIndex() *FileOwnerIndex {
	idx := &FileOwnerIndex{
		managed:  map[string]bool{},
		released: map[string]bool{},
		xrayTake: map[string][]string{},
	}
	if !l.Enabled() {
		return idx
	}
	idx.enabled = true
	st := l.store.Snapshot()
	installed := l.installed()
	for _, e := range manifestOfScope(st.Manifest, installed, "") {
		abs, err := l.opts.Roots.Abs(e.Kernel, e.RelPath)
		if err != nil {
			continue
		}
		if e.Status == StatusReleased {
			idx.released[resolveFullSymlinks(abs)] = true
			continue
		}
		idx.managed[resolveFullSymlinks(abs)] = true
	}
	if l.opts.Roots.Xray != "" {
		idx.xrayRoot = resolveFullSymlinks(filepath.Clean(l.opts.Roots.Xray))
	}
	if l.opts.Roots.Mihomo != "" {
		idx.mihomoRoot = resolveFullSymlinks(filepath.Clean(l.opts.Roots.Mihomo))
	}
	idx.xkeenRoot = resolveFullSymlinks(filepath.Clean(services.DefaultXKeenConfigDir))

	desired := DesiredCores(st.Draft)
	if installed.Xray {
		if scans, _, err := l.ScanXraySections(); err == nil {
			for _, id := range XraySectionIDs {
				if desired[CoreSectionName(KernelXray, id)].Managed || st.Cores.Xray[id] != nil {
					continue
				}
				for _, name := range scans[id].Files {
					idx.xrayTake[name] = append(idx.xrayTake[name], id)
				}
			}
		}
	}
	routingMihomo := st.Routing.Mihomo != nil || DesiredRouting(st.Draft).ManagedFor(KernelMihomo)
	if installed.Mihomo && !routingMihomo && mihomoActiveName(l.opts.Roots.Mihomo) != "" {
		idx.mihomoActive = resolveFullSymlinks(filepath.Join(filepath.Clean(l.opts.Roots.Mihomo), mihomoConfigFile))
		for _, id := range MihomoSectionIDs {
			if !desired[CoreSectionName(KernelMihomo, id)].Managed {
				idx.mihomoTake = append(idx.mihomoTake, id)
			}
		}
	}
	return idx
}

// FileOwner определяет владельца файла по абсолютному пути (D-05) и секции,
// которые панель может взять из него (D-03). Путь сравнивается после
// разворачивания симлинков: config.yaml, указывающий на профиль панели, — файл
// панели. Выключенный слой — пустой владелец.
func (idx *FileOwnerIndex) FileOwner(path string) FileOwnerInfo {
	if idx == nil || !idx.enabled || !filepath.IsAbs(path) {
		return FileOwnerInfo{}
	}
	resolved := resolveFullSymlinks(path)
	if idx.managed[resolved] {
		return FileOwnerInfo{Owner: OwnerPanel}
	}
	info := FileOwnerInfo{Owner: OwnerUser}
	// Отпущенный файл манифеста — файл пользователя (144 D-10), даже если его имя
	// совпадает с шаблоном XKeen.
	released := idx.released[resolved]
	dir, name := filepath.Split(resolveDirSymlinks(path))
	dir = filepath.Clean(dir)
	switch {
	case released:
	case idx.xrayRoot != "" && dir == idx.xrayRoot && xkeenTemplateFiles[name]:
		info.Owner = OwnerXKeen
	case idx.mihomoRoot != "" && dir == idx.mihomoRoot && name == mihomoConfigFile:
		info.Owner = OwnerXKeen
	case dir == idx.xkeenRoot && name == xkeenConfigFile:
		info.Owner = OwnerXKeen
	}
	if idx.xrayRoot != "" && dir == idx.xrayRoot {
		if ids := idx.xrayTake[name]; len(ids) > 0 {
			info.TakeKernel = KernelXray
			info.TakeSections = append([]string(nil), ids...)
		}
	}
	if idx.mihomoActive != "" && resolved == idx.mihomoActive && len(idx.mihomoTake) > 0 {
		info.TakeKernel = KernelMihomo
		info.TakeSections = append([]string(nil), idx.mihomoTake...)
	}
	return info
}

// FileOwner — владелец одного файла (FileOwnerIndex на один путь). Для списка
// файлов строить индекс один раз через FileOwnerIndex.
func (l *Layer) FileOwner(path string) FileOwnerInfo {
	return l.FileOwnerIndex().FileOwner(path)
}
