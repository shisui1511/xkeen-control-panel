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
