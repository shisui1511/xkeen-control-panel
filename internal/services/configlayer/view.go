package configlayer

import "sort"

// FileView — файл слоя для снимка и события files. Owner: "panel" (managed и
// pending) или "manual" (released).
type FileView struct {
	Key          string    `json:"key"`
	Kernel       string    `json:"kernel"`
	Path         string    `json:"path"`
	Owner        string    `json:"owner"`
	State        FileState `json:"state"`
	ObsoleteName string    `json:"obsolete_name,omitempty"`
	// AliasPaths — другие пути, которые разрешаются в этот файл (config.yaml —
	// симлинк на профиль панели): Редактор защищает их так же, как Path.
	AliasPaths []string `json:"alias_paths,omitempty"`
}

// NoticeView — уведомление слоя. ID: schema_reset, recovered_from_journal,
// journal_recovery_failed, build_failed:xray, build_failed:mihomo и уведомления
// сервиса источников source_rejected:<id>:<ядро>, source_deferred:<id>,
// source_kernel_refused:<id>:<ядро>, source_network:<id>:<ядро>; Kind: "warning"
// или "error". Params — параметры подстановки в текст (имя источника, дата).
type NoticeView struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Kernel string            `json:"kernel,omitempty"`
	Reason string            `json:"reason,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// FilesEvent — данные события files.
type FilesEvent struct {
	Files []FileView `json:"files"`
	// DriftCount — файлы манифеста с расхождением плюс расхождения маршрутизации.
	DriftCount int `json:"drift_count"`
	// Routing — расхождения маршрутизации (они уже входят в DriftCount); в событие
	// не попадают, клиент берёт их из блока routing снимка.
	Routing []RoutingDriftItem `json:"-"`
	// Sections — расхождения взятых секций Xray (section_foreign, уже входят в
	// DriftCount); в событие не попадают, клиент берёт их из блока cores снимка.
	Sections []RoutingDriftItem `json:"-"`
}

// NoticesEvent — данные события notices.
type NoticesEvent struct {
	Notices []NoticeView `json:"notices"`
}

// RoutingKernelView — владение маршрутизацией одного ядра в снимке слоя.
type RoutingKernelView struct {
	// Taken — панель взяла маршрутизацию ядра.
	Taken bool `json:"taken"`
	// Moved — чужие файлы routing Xray, перенесённые в копии при взятии.
	Moved []string `json:"moved"`
	// Left — файлы routing Xray, оставленные на месте (смешанные).
	Left []string `json:"left"`
	// Drift — расхождения: имена файлов или "config.yaml".
	Drift []string `json:"drift"`
	// PrevProfile — прежний активный профиль Mihomo (для Xray пусто).
	PrevProfile string `json:"prev_profile,omitempty"`
}

// RoutingSnapshot — раздел routing снимка: желаемое владение (секция routing
// черновика) и фактическое по установленным ядрам; неустановленное ядро — null.
//
// Desired — панель держит хотя бы одно ядро; DesiredXray и DesiredMihomo — решение
// по ядрам (D-06 из 147.1). MihomoActive — имя активного своего профиля Mihomo
// (пусто при профиле панели, без файла или при взятом Mihomo; "config.yaml" для
// обычного файла); MihomoSaved — значения секций Mihomo сохранены (D-09).
type RoutingSnapshot struct {
	Desired       bool               `json:"desired"`
	DesiredXray   bool               `json:"desired_xray"`
	DesiredMihomo bool               `json:"desired_mihomo"`
	MihomoActive  string             `json:"mihomo_active"`
	MihomoSaved   bool               `json:"mihomo_saved"`
	Xray          *RoutingKernelView `json:"xray"`
	Mihomo        *RoutingKernelView `json:"mihomo"`
}

// CoreSectionSnap — владение одной секцией Xray в снимке слоя.
type CoreSectionSnap struct {
	// Desired — секция черновика «Управлять в панели» (managed).
	Desired bool `json:"desired"`
	// Taken — панель фактически ведёт секцию (запись в State.Cores).
	Taken bool `json:"taken"`
	// File — файл панели секции.
	File string `json:"file"`
	// Moved — чужие файлы секции, перенесённые в копии при взятии.
	Moved []string `json:"moved"`
	// Left — файлы с ключом секции, оставленные на месте (смешанные).
	Left []string `json:"left"`
	// Drift — чужие файлы с ключом секции, появившиеся после взятия или
	// идущие после файла панели.
	Drift []string `json:"drift"`
}

// RestartNote — объявление о перезапуске ядра, нужном для вступления секций в
// силу (заполняет план 147-16).
type RestartNote struct {
	Kernel string `json:"kernel"`
	Reason string `json:"reason"`
}

// CoresSnapshot — раздел cores снимка: владение секциями Xray по идентификатору
// секции и объявления перезапуска.
type CoresSnapshot struct {
	Xray    map[string]CoreSectionSnap `json:"xray"`
	Restart []RestartNote              `json:"restart"`
}

// SnapshotView — состояние слоя целиком для GET-снимка и первого события SSE.
type SnapshotView struct {
	Enabled       bool                     `json:"enabled"`
	DevMode       bool                     `json:"dev_mode"`
	DraftRevision int64                    `json:"draft_revision"`
	DraftChanges  int                      `json:"draft_changes"`
	DriftCount    int                      `json:"drift_count"`
	Files         []FileView               `json:"files"`
	Kernels       []KernelVersionView      `json:"kernels"`
	Features      map[Feature]Availability `json:"features"`
	Apply         ApplyView                `json:"apply"`
	Notices       []NoticeView             `json:"notices"`
	// Checks — блокирующие проблемы проверки черновика (пустая группа, висячая
	// ссылка): пока список не пуст, «Применить» отказывает. Пустой список — [].
	Checks []BuildIssue `json:"checks"`
	// Routing — владение маршрутизацией: желаемое, взятое, перенесённое и дрейф.
	Routing RoutingSnapshot `json:"routing"`
	// Cores — владение секциями Xray: желаемое, взятое, перенесённое и дрейф.
	Cores CoresSnapshot `json:"cores"`
	// BusyHolder — кто держит исключительный замок применения вне конвейера
	// (менеджер geodata: geodata_update, geodata_remove); пусто — замок свободен.
	BusyHolder string `json:"busy_holder,omitempty"`
}

// sortFileViews упорядочивает файлы для UI: drift, pending, ok, released;
// внутри группы — по пути, затем по ключу.
func sortFileViews(files []FileView) {
	sort.SliceStable(files, func(i, j int) bool {
		ri, rj := fileStateRank(files[i].State), fileStateRank(files[j].State)
		if ri != rj {
			return ri < rj
		}
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Key < files[j].Key
	})
}

// fileStateRank — место состояния в списке: drift_*, pending, ok, released.
func fileStateRank(s FileState) int {
	switch {
	case s.IsDrift():
		return 0
	case s == StatePending:
		return 1
	case s == StateOK:
		return 2
	default:
		return 3
	}
}
