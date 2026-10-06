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
// build_failed:xray, build_failed:mihomo; Kind: "warning" или "error".
type NoticeView struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Kernel string `json:"kernel,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// FilesEvent — данные события files.
type FilesEvent struct {
	Files      []FileView `json:"files"`
	DriftCount int        `json:"drift_count"`
}

// NoticesEvent — данные события notices.
type NoticesEvent struct {
	Notices []NoticeView `json:"notices"`
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
