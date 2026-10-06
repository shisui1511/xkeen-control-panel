package configlayer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeBins — пара фейковых ядер, которые всегда принимают конфигурацию.
func fakeBins(t *testing.T) Binaries {
	t.Helper()
	binDir := t.TempDir()
	return Binaries{
		Xray:   writeFakeKernel(t, binDir, "xray", 0, "", 0),
		Mihomo: writeFakeKernel(t, binDir, "mihomo", 0, "", 0),
	}
}

// backupSets возвращает пути каталогов apply-* в <data_dir>/backup/config-layer.
func backupSets(t *testing.T, dataDir string) []string {
	t.Helper()
	root := filepath.Join(dataDir, "backup", "config-layer")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "apply-") {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("файл существует, want отсутствует: %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// seedManagedXray кладёт в манифест и на диск прежний файл панели Xray.
func seedManagedXray(t *testing.T, env *testEnv, rel, content string) string {
	t.Helper()
	abs := filepath.Join(env.Roots.Xray, rel)
	writeTestFile(t, abs, content)
	err := env.Store.Update(func(st *State) error {
		st.Manifest[ManifestKey(KernelXray, rel)] = ManifestEntry{
			Kernel: KernelXray, RelPath: rel, Kind: KindXrayJSON,
			Hash: HashContent([]byte(content)), Status: StatusManaged,
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestApply_WriteFailureRollsBack(t *testing.T) {
	calls := 0
	// На втором файле запись рвётся посреди: в файле остаётся обрывок, а шов
	// возвращает ошибку. Откат должен вернуть прежнюю версию из копии.
	failing := func(path string, data []byte) error {
		calls++
		if calls == 2 {
			_ = os.WriteFile(path, []byte("PARTIAL"), 0o644)
			return os.ErrPermission
		}
		return os.WriteFile(path, data, 0o644)
	}
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true, WriteFile: failing})
	xrayAbs := seedManagedXray(t, env, DiagXrayRel, "OLD")
	env.setDraft(t, DiagSection, `{"enabled":true}`)
	before := env.Store.Snapshot()

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultWriteFailed || !r.RolledBack {
		t.Fatalf("Result = %+v, want write_failed_rolled_back с RolledBack", r)
	}
	if got := readFileString(t, xrayAbs); got != "OLD" {
		t.Errorf("файл Xray = %q, want прежние байты %q", got, "OLD")
	}
	requireAbsent(t, filepath.Join(env.Roots.Mihomo, DiagMihomoRel))

	st := env.Store.Snapshot()
	if st.Journal != nil {
		t.Errorf("Journal = %+v, want nil после отката", st.Journal)
	}
	if len(st.Applied) != len(before.Applied) {
		t.Errorf("Applied = %v, want прежний %v", st.Applied, before.Applied)
	}
	if e := st.Manifest[ManifestKey(KernelXray, DiagXrayRel)]; e.Hash != HashContent([]byte("OLD")) {
		t.Errorf("запись манифеста изменена: %+v", e)
	}
	if _, ok := st.Manifest[ManifestKey(KernelMihomo, DiagMihomoRel)]; ok {
		t.Error("в манифесте появился файл Mihomo")
	}

	sets := backupSets(t, env.DataDir)
	if len(sets) != 1 {
		t.Fatalf("наборов копий = %d, want 1: %v", len(sets), sets)
	}
	if got := readFileString(t, filepath.Join(sets[0], "xray", DiagXrayRel)); got != "OLD" {
		t.Errorf("копия Xray = %q, want OLD", got)
	}
	if _, err := os.Stat(filepath.Join(sets[0], "meta.json")); err != nil {
		t.Errorf("нет meta.json: %v", err)
	}
	if s := stepOf(t, view, StepWrite); s.State != StepFailed {
		t.Errorf("write = %+v, want failed", s)
	}
}

func TestWrite_JournalBeforeFirstWrite(t *testing.T) {
	var env *testEnv
	var seen *Journal
	first := true
	probe := func(path string, data []byte) error {
		if first {
			first = false
			seen = env.Store.Snapshot().Journal
		}
		return os.WriteFile(path, data, 0o644)
	}
	env = newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true, WriteFile: probe})
	env.setDraft(t, DiagSection, `{"enabled":true}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || view.Result.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if seen == nil {
		t.Fatal("перед первой записью журнала нет")
	}
	if seen.Trigger != string(TriggerUser) || seen.StartedAt.IsZero() {
		t.Errorf("журнал = %+v, want trigger user и StartedAt", seen)
	}
	if st, err := os.Stat(seen.BackupDir); err != nil || !st.IsDir() {
		t.Errorf("каталог копии журнала не создан: %v", err)
	}
	want := map[string]string{
		ManifestKey(KernelXray, DiagXrayRel):     HashContent([]byte(diagXrayOK)),
		ManifestKey(KernelMihomo, DiagMihomoRel): HashContent([]byte(diagMihomoProvider)),
	}
	if len(seen.Files) != len(want) {
		t.Fatalf("файлов в журнале %d, want %d: %+v", len(seen.Files), len(want), seen.Files)
	}
	for _, f := range seen.Files {
		if hash, ok := want[f.Key]; !ok || f.NewHash != hash || f.Existed {
			t.Errorf("запись журнала %+v, want NewHash %q и Existed=false", f, hash)
		}
	}
	if j := env.Store.Snapshot().Journal; j != nil {
		t.Errorf("Journal после успеха = %+v, want nil", j)
	}
}

// metaOf возвращает запись набора для файла <ядро>/<путь> или ok=false.
func metaOf(t *testing.T, setDir, kernel, rel string) (BackupFileMeta, bool) {
	t.Helper()
	set, err := LoadBackupSet(setDir)
	if err != nil {
		t.Fatalf("LoadBackupSet: %v", err)
	}
	for _, m := range set.Meta.Files {
		if m.Kernel == kernel && m.RelPath == rel {
			return m, true
		}
	}
	return BackupFileMeta{}, false
}

func TestApply_OrphanMovedToBackup(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true})
	orphan := filepath.Join(env.Roots.Xray, "xcp-orphan.json")
	selected := filepath.Join(env.Roots.Xray, "04_outbounds.zz_xcp_selected.tail.json")
	inbounds := filepath.Join(env.Roots.Xray, "03_inbounds.json")
	link := filepath.Join(env.Roots.Mihomo, "config.yaml.xcp-link")
	writeTestFile(t, orphan, `{"orphan":true}`)
	writeTestFile(t, selected, `{"old":"layer"}`)
	writeTestFile(t, inbounds, `{"inbounds":[]}`)
	writeTestFile(t, link, "old-link\n")

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerRebuild, Source: SourceDraft})

	r := view.Result
	if r == nil || !r.OK || r.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", r)
	}
	if len(r.OrphansRemoved) != 1 || r.OrphansRemoved[0] != "xcp-orphan.json" {
		t.Errorf("OrphansRemoved = %v, want [xcp-orphan.json]", r.OrphansRemoved)
	}
	requireAbsent(t, orphan)
	sets := backupSets(t, env.DataDir)
	if len(sets) != 1 {
		t.Fatalf("наборов копий = %d, want 1", len(sets))
	}
	if got := readFileString(t, filepath.Join(sets[0], "xray", "xcp-orphan.json")); got != `{"orphan":true}` {
		t.Errorf("копия сироты = %q", got)
	}
	if m, ok := metaOf(t, sets[0], KernelXray, "xcp-orphan.json"); !ok || m.Reason != ReasonOrphan || !m.Existed {
		t.Errorf("запись меты сироты = %+v ok=%v, want reason orphan", m, ok)
	}
	// Старый слой и чужие файлы не тронуты никогда.
	for path, want := range map[string]string{
		selected: `{"old":"layer"}`,
		inbounds: `{"inbounds":[]}`,
		link:     "old-link\n",
	} {
		if got := readFileString(t, path); got != want {
			t.Errorf("%s = %q, want прежние байты %q", path, got, want)
		}
	}
	if st := env.Store.Snapshot(); st.Journal != nil {
		t.Errorf("Journal = %+v, want nil", st.Journal)
	}
}

func TestApply_StaleEntryDeleted(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true})
	const rel = "04_outbounds.xcp-stale.tail.json"
	abs := seedManagedXray(t, env, rel, `{"stale":1}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if r := view.Result; r == nil || !r.OK || r.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	requireAbsent(t, abs)
	if _, ok := env.Store.Snapshot().Manifest[ManifestKey(KernelXray, rel)]; ok {
		t.Error("ключ остался в манифесте")
	}
	sets := backupSets(t, env.DataDir)
	if len(sets) != 1 {
		t.Fatalf("наборов копий = %d, want 1", len(sets))
	}
	if got := readFileString(t, filepath.Join(sets[0], "xray", rel)); got != `{"stale":1}` {
		t.Errorf("копия = %q", got)
	}
	if m, ok := metaOf(t, sets[0], KernelXray, rel); !ok || m.Reason != ReasonDelete {
		t.Errorf("запись меты = %+v ok=%v, want reason delete", m, ok)
	}
}

func TestApply_RenamedObsoleteRemoved(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true})
	abs := seedManagedXray(t, env, DiagXrayRel, "OLD")
	// XKeen вывел файл из работы: <имя>.json.obsolete, самого файла нет.
	if err := os.Rename(abs, abs+".obsolete"); err != nil {
		t.Fatal(err)
	}
	env.setDraft(t, DiagSection, `{"enabled":true}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerRebuild, Source: SourceDraft})

	if r := view.Result; r == nil || !r.OK || r.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if got := readFileString(t, abs); got != diagXrayOK {
		t.Errorf("файл = %q, want записанный заново %q", got, diagXrayOK)
	}
	requireAbsent(t, abs+".obsolete")
	sets := backupSets(t, env.DataDir)
	if len(sets) != 1 {
		t.Fatalf("наборов копий = %d, want 1", len(sets))
	}
	if got := readFileString(t, filepath.Join(sets[0], "xray", DiagXrayRel+".obsolete")); got != "OLD" {
		t.Errorf("копия .obsolete = %q, want OLD", got)
	}
	if m, ok := metaOf(t, sets[0], KernelXray, DiagXrayRel+".obsolete"); !ok || m.Reason != ReasonObsolete {
		t.Errorf("запись меты .obsolete = %+v ok=%v, want reason obsolete", m, ok)
	}
}

func TestWrite_SymlinkOutsideRootRejected(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t), DevMode: true})
	outside := filepath.Join(t.TempDir(), "secret.json")
	writeTestFile(t, outside, "TARGET")
	link := filepath.Join(env.Roots.Xray, DiagXrayRel)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	env.setDraft(t, DiagSection, `{"enabled":true}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultWriteFailed || !r.RolledBack {
		t.Fatalf("Result = %+v, want write_failed_rolled_back", r)
	}
	if !strings.Contains(r.Message, ErrSymlinkOutsideRoot.Error()) {
		t.Errorf("Message = %q, want текст ErrSymlinkOutsideRoot", r.Message)
	}
	if got := readFileString(t, outside); got != "TARGET" {
		t.Errorf("цель симлинка изменена: %q", got)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("симлинк заменён: %v", err)
	}
	// Отказ до первой записи: ни один файл не записан.
	requireAbsent(t, filepath.Join(env.Roots.Mihomo, DiagMihomoRel))
	if env.Store.Snapshot().Journal != nil {
		t.Error("журнал не очищен")
	}
}

func TestWrite_NonPanelNameNeverDeleted(t *testing.T) {
	env := newTestPipeline(t, pipeOpts{Bins: fakeBins(t)})
	inbounds := filepath.Join(env.Roots.Xray, "03_inbounds.json")
	writeTestFile(t, inbounds, `{"inbounds":[]}`)
	plan := Plan{Files: []FilePlan{{
		Key: ManifestKey(KernelXray, "03_inbounds.json"), Kernel: KernelXray,
		RelPath: "03_inbounds.json", AbsPath: inbounds, Action: ActionDelete,
	}}}

	out, err := env.P.writePlan(plan, TriggerUser)

	if err == nil {
		t.Fatal("writePlan удалил файл не по правилу панели без ошибки")
	}
	if out.RollbackErr != nil {
		t.Errorf("RollbackErr = %v", out.RollbackErr)
	}
	if got := readFileString(t, inbounds); got != `{"inbounds":[]}` {
		t.Errorf("03_inbounds.json = %q, want нетронутый", got)
	}
}
