package geodata

import (
	"os"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Типы снимка вкладки «Списки» — ровно по frontend/src/lib/geodataContract.json:
// обязательные поля без omitempty, необязательные с omitempty. Интерфейс ничего не
// считает сам: источники, чипы, итоги и пометки приходят от сервера готовыми.

// Источник строки (SourceView.origin).
const (
	OriginStandard = "standard"
	OriginPanel    = "panel"
	OriginOwn      = "own"
	OriginFile     = "file"
)

// heavyFileSize — с этого размера файл считается тяжёлым: Xray дольше стартует.
const heavyFileSize = 4 << 20

// XKeenView — состояние XKeen.
type XKeenView struct {
	Installed   bool   `json:"installed"`
	UserGeodata bool   `json:"user_geodata"`
	Version     string `json:"version,omitempty"`
}

// DownloadView — условия блока «Загрузка»: прокси, jq, geofile_update.
type DownloadView struct {
	GhProxy       string               `json:"gh_proxy"`
	JQ            bool                 `json:"jq"`
	GeofileUpdate bool                 `json:"geofile_update"`
	Reason        string               `json:"reason,omitempty"`
	Invalid       []lists.GeodataEntry `json:"invalid,omitempty"`
	JQError       string               `json:"jq_error,omitempty"`
}

// ScheduleView — расписание панели и часы роутера.
type ScheduleView struct {
	Enabled   bool   `json:"enabled"`
	Day       int    `json:"day"`
	Hour      int    `json:"hour"`
	Minute    int    `json:"minute"`
	Suspended bool   `json:"suspended"`
	Now       string `json:"now"`
	Next      string `json:"next,omitempty"`
	RetryAt   string `json:"retry_at,omitempty"`
}

// CronView — cron XKeen и его отношение к расписанию панели.
type CronView struct {
	Found     bool   `json:"found"`
	Adoptable bool   `json:"adoptable"`
	Two       bool   `json:"two"`
	Kept      bool   `json:"kept"`
	Schedule  string `json:"schedule,omitempty"`
}

// RunView — текущее задание менеджера.
type RunView struct {
	Running      bool   `json:"running"`
	WaitingApply bool   `json:"waiting_apply"`
	Seq          uint64 `json:"seq"`
	Job          string `json:"job,omitempty"`
	Started      string `json:"started,omitempty"`
}

// LastRunView — итог последнего запуска.
type LastRunView struct {
	At         string `json:"at"`
	Finished   string `json:"finished,omitempty"`
	Outcome    string `json:"outcome"`
	NotUpdated int    `json:"not_updated"`
	Parsed     bool   `json:"parsed"`
	Reason     string `json:"reason,omitempty"`
}

// FileResultView — итог последнего запуска по файлу.
type FileResultView struct {
	Status     string `json:"status"`
	At         string `json:"at"`
	Reason     string `json:"reason,omitempty"`
	Unverified bool   `json:"unverified,omitempty"`
	Limit      bool   `json:"limit,omitempty"`
}

// SourceView — строка источника.
type SourceView struct {
	ID      string `json:"id"`
	File    string `json:"file"`
	Origin  string `json:"origin"`
	Kind    string `json:"kind"`
	Present bool   `json:"present"`
	Entry   bool   `json:"entry"`

	SetID    string   `json:"set_id,omitempty"`
	Provider string   `json:"provider,omitempty"`
	URL      string   `json:"url,omitempty"`
	Size     int64    `json:"size,omitempty"`
	MTime    string   `json:"mtime,omitempty"`
	Tags     int      `json:"tags,omitempty"`
	Entries  int      `json:"entries,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`

	Pending          bool            `json:"pending,omitempty"`
	IncludeStuck     bool            `json:"include_stuck,omitempty"`
	Dup              bool            `json:"dup,omitempty"`
	DupURL           bool            `json:"dup_url,omitempty"`
	Draft            string          `json:"draft,omitempty"`
	DeleteIncomplete string          `json:"delete_incomplete,omitempty"`
	Outside          string          `json:"outside,omitempty"`
	Result           *FileResultView `json:"result,omitempty"`
	Invalid          bool            `json:"invalid,omitempty"`
	Heavy            bool            `json:"heavy,omitempty"`
}

// DraftDropView — пометка источника в черновике.
type DraftDropView struct {
	File string `json:"file"`
	Mode string `json:"mode"`
}

// View — снимок менеджера для вкладки «Списки».
type View struct {
	XKeen     XKeenView          `json:"xkeen"`
	Download  DownloadView       `json:"download"`
	Schedule  ScheduleView       `json:"schedule"`
	Cron      CronView           `json:"cron"`
	Run       RunView            `json:"run"`
	LastRun   *LastRunView       `json:"last_run,omitempty"`
	Sources   []SourceView       `json:"sources"`
	Files     int                `json:"files"`
	TotalSize int64              `json:"total_size"`
	Draft     []DraftDropView    `json:"draft"`
	Mihomo    []lists.MihomoFile `json:"mihomo"`
	GeoIPSet  *FileResultView    `json:"geoipset,omitempty"`
}

// OutputChunk — окно вывода задания (ug или jq) для клиента.
type OutputChunk struct {
	Job       string `json:"job"`
	From      int64  `json:"from"`
	Next      int64  `json:"next"`
	Text      string `json:"text"`
	Running   bool   `json:"running"`
	Truncated bool   `json:"truncated,omitempty"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// viewKind переводит вид индекса списков (site, ip) в вид контракта (geosite, geoip).
func viewKind(k string) string {
	if k == lists.KindIP {
		return "geoip"
	}
	return "geosite"
}

// nameKind — вид по имени файла, когда самого файла нет.
func nameKind(name string) string {
	l := strings.ToLower(name)
	if strings.Contains(l, "geoip") || strings.Contains(l, "zkeenip") {
		return "geoip"
	}
	return "geosite"
}

func resultView(r FileResult) *FileResultView {
	return &FileResultView{Status: r.Status, At: rfc3339(r.At), Reason: r.Reason, Unverified: r.Unverified, Limit: r.Limit}
}

// snapCtx — данные одного построения снимка.
type snapCtx struct {
	st       State
	files    map[string]lists.FileInfo // любое из имён пары → файл
	drafts   map[string]string         // имя файла → delete | off
	entries  []lists.GeodataEntry
	entryURL map[string]string // имя файла записи → адрес первой записи
	invalid  map[string]bool
	index    *lists.Index
}

// Snapshot собирает снимок вкладки. Ошибка чтения xkeen.json снимок не роняет:
// download.reason получает код ошибки. Ошибка чтения каталога dat — ошибка снимка.
func (s *Service) Snapshot() (View, error) {
	s.mu.Lock()
	st := s.st.clone()
	run := RunView{Running: s.running, WaitingApply: s.waitingApply, Seq: s.seq, Job: s.job, Started: rfc3339(s.started)}
	s.mu.Unlock()
	now := s.d.Now()

	var v View
	_, statErr := os.Stat(s.d.XKeenBin)
	v.XKeen = XKeenView{
		Installed:   statErr == nil,
		UserGeodata: s.d.Layer.FeatureAvailability(configlayer.FeatureXKeenUserGeodata) != configlayer.Unavailable,
	}

	status, err := s.d.Store.Status()
	if err != nil && status.Reason == "" {
		status.Reason = lists.ErrorCode(err)
		if status.Reason == "" {
			status.Reason = lists.ErrXKeenJSONInvalid.Code
		}
	}
	v.Download = DownloadView{
		GhProxy: status.GhProxy, JQ: status.JQ, GeofileUpdate: status.GeofileUpdate,
		Reason: status.Reason, Invalid: status.Invalid, JQError: st.JQError,
	}

	v.Schedule = ScheduleView{
		Enabled: st.Schedule.Enabled, Day: st.Schedule.Day, Hour: st.Schedule.Hour, Minute: st.Schedule.Minute,
		Suspended: st.Suspended, Now: now.Format(time.RFC3339),
	}
	if st.RetryAt.After(now) {
		v.Schedule.RetryAt = rfc3339(st.RetryAt)
	}
	v.Cron = CronView{Two: st.CronTwo, Kept: st.CronKept != "", Schedule: st.CronSchedule}
	v.Run = run
	if lr := st.LastRun; lr != nil {
		v.LastRun = &LastRunView{
			At: rfc3339(lr.At), Finished: rfc3339(lr.Finished), Outcome: lr.Outcome,
			NotUpdated: lr.NotUpdated, Parsed: lr.Parsed, Reason: lr.Reason,
		}
	}
	if st.GeoIPSet != nil {
		v.GeoIPSet = resultView(*st.GeoIPSet)
	}

	infos, err := s.d.Index.Files()
	if err != nil {
		return View{}, err
	}
	v.Files = len(infos)
	for _, f := range infos {
		v.TotalSize += f.Size
	}

	cat, err := lists.Resolve(s.d.Index, status.Entries)
	if err != nil {
		return View{}, err
	}

	c := &snapCtx{
		st: st, files: map[string]lists.FileInfo{}, drafts: map[string]string{},
		entries: status.Entries, entryURL: map[string]string{}, invalid: map[string]bool{}, index: s.d.Index,
	}
	for _, f := range infos {
		c.files[f.Name] = f
		for _, a := range f.Aliases {
			c.files[a] = f
		}
	}
	v.Draft = []DraftDropView{}
	if state, err := s.d.Layer.ReadState(); err == nil {
		if gd, err := routing.ParseGeodataDraft(state.Draft[routing.GeodataSection]); err == nil {
			for _, d := range gd.Drop {
				c.drafts[d.File] = d.Mode
				v.Draft = append(v.Draft, DraftDropView{File: d.File, Mode: d.Mode})
			}
		}
	}
	for _, e := range status.Entries {
		if _, ok := c.entryURL[e.File]; !ok {
			c.entryURL[e.File] = e.URL
		}
	}
	for _, e := range status.Invalid {
		c.invalid[e.File] = true
		if _, ok := c.entryURL[e.File]; !ok {
			c.entryURL[e.File] = e.URL
		}
	}

	v.Sources = []SourceView{}
	covered := map[string]bool{}
	cover := func(names ...string) {
		for _, n := range names {
			if n != "" {
				covered[n] = true
			}
		}
	}

	// 1. Стандартные наборы XKeen — шесть строк в фиксированном порядке.
	for _, set := range StandardSets {
		names := []string{set.File}
		if set.Alias != "" {
			names = append(names, set.Alias)
		}
		cover(names...)
		row := SourceView{
			ID: set.File, File: set.File, Origin: OriginStandard, Kind: set.Kind,
			SetID: set.ID, Provider: set.Provider, URL: s.vars.URL(set),
		}
		c.fill(&row, names)
		for _, n := range names {
			if u, ok := c.entryURL[n]; ok {
				row.Entry = true
				row.Dup = row.Present && !row.Pending
				row.DupURL = u != row.URL
				break
			}
		}
		v.Sources = append(v.Sources, row)
	}

	// 2. Каталог панели (146): набор не расширяется, строки — по каталогу.
	for _, e := range cat.Extended {
		cover(e.File)
		row := SourceView{
			ID: e.File, File: e.File, Origin: OriginPanel, Kind: viewKind(e.Kind),
			SetID: e.ID, URL: e.URL,
		}
		c.fill(&row, []string{e.File})
		_, row.Entry = c.entryURL[e.File]
		v.Sources = append(v.Sources, row)
	}

	// 3. Записи, вписанные вручную: все остальные записи geodata.
	seenOwn := map[string]bool{}
	for _, e := range append(append([]lists.GeodataEntry{}, status.Entries...), status.Invalid...) {
		if e.File == "" || covered[e.File] || seenOwn[e.File] {
			continue
		}
		seenOwn[e.File] = true
		cover(e.File)
		row := SourceView{
			ID: e.File, File: e.File, Origin: OriginOwn, Kind: nameKind(e.File),
			URL: c.entryURL[e.File], Entry: true, Invalid: c.invalid[e.File],
		}
		c.fill(&row, []string{e.File})
		v.Sources = append(v.Sources, row)
	}

	// 4. Файлы dat без записи и без стандартного имени.
	for _, f := range infos {
		names := append([]string{f.Name}, f.Aliases...)
		skip := false
		for _, n := range names {
			if covered[n] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		row := SourceView{ID: f.Name, File: f.Name, Origin: OriginFile, Kind: viewKind(f.Kind)}
		c.fill(&row, names)
		v.Sources = append(v.Sources, row)
	}

	mihomo, err := lists.MihomoFiles(s.d.MihomoDir)
	if err != nil || mihomo == nil {
		mihomo = []lists.MihomoFile{}
	}
	v.Mihomo = mihomo
	return v, nil
}

// fill дополняет строку по файлу на диске, итогам запуска, пометкам и ожиданиям.
// names — имена, под которыми файл может лежать (основное и второе имя пары).
func (c *snapCtx) fill(row *SourceView, names []string) {
	var info *lists.FileInfo
	for _, n := range names {
		if f, ok := c.files[n]; ok {
			f := f
			info = &f
			break
		}
	}
	all := append([]string{}, names...)
	if info != nil {
		row.Present = true
		row.Size = info.Size
		row.MTime = rfc3339(info.ModTime.UTC())
		row.Kind = viewKind(info.Kind)
		row.Heavy = info.Size >= heavyFileSize
		// Ошибка чтения тегов не роняет строку: поля остаются пустыми.
		if tags, err := c.index.Tags(info.Name); err == nil {
			row.Tags = len(tags)
			for _, t := range tags {
				row.Entries += t.Count
			}
		}
		all = append(all, info.Name)
		all = append(all, info.Aliases...)
		for _, n := range append([]string{info.Name}, info.Aliases...) {
			if n != row.File {
				row.Aliases = append(row.Aliases, n)
			}
		}
	}
	for _, n := range all {
		if r, ok := c.st.Results[n]; ok && row.Result == nil {
			row.Result = resultView(r)
		}
		if at, ok := c.st.Outside[n]; ok && row.Outside == "" {
			row.Outside = rfc3339(at)
		}
		if _, ok := c.st.IncludeStuck[n]; ok {
			row.IncludeStuck = true
		}
		if m, ok := c.drafts[n]; ok && row.Draft == "" {
			row.Draft = m
		}
		for _, p := range c.st.PendingEnables {
			if p.File == n {
				row.Pending = true
			}
		}
		for _, d := range c.st.PendingDeletes {
			if d.File == n && row.DeleteIncomplete == "" {
				row.DeleteIncomplete = d.Reason
			}
		}
	}
}
