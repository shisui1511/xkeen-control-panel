package configlayer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diagRegistry — реестр с одним диагностическим генератором.
func diagRegistry(devMode bool) *Registry {
	r := NewRegistry()
	r.Register(NewDiagGenerator(func() bool { return devMode }))
	return r
}

func TestPlan_TracerDiag(t *testing.T) {
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	src := Sections{DiagSection: []byte(`{"enabled":true}`)}
	both := InstalledKernels{Xray: true, Mihomo: true}

	files, err := diagRegistry(true).Build(src, both)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("Build вернул %d файлов, want 2", len(files))
	}
	wantKeys := []string{
		ManifestKey(KernelMihomo, DiagMihomoRel),
		ManifestKey(KernelXray, DiagXrayRel),
	}
	for i, want := range wantKeys {
		if got := files[i].Key(); got != want {
			t.Fatalf("files[%d].Key() = %q, want %q", i, got, want)
		}
	}

	if err := CheckGenerated(files, nil); err != nil {
		t.Fatalf("CheckGenerated: %v", err)
	}

	plan, err := ComputePlan(roots, files, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("в плане %d файлов, want 2", len(plan.Files))
	}
	for _, fp := range plan.Files {
		if fp.Action != ActionWrite {
			t.Errorf("%s: Action = %q, want %q", fp.Key, fp.Action, ActionWrite)
		}
		var content []byte
		for _, f := range files {
			if f.Key() == fp.Key {
				content = f.Content
			}
		}
		if fp.NewHash != HashContent(content) {
			t.Errorf("%s: NewHash = %q, want %q", fp.Key, fp.NewHash, HashContent(content))
		}
		if fp.AbsPath == "" {
			t.Errorf("%s: пустой AbsPath", fp.Key)
		}
	}
	if len(plan.Orphans) != 0 {
		t.Errorf("Orphans = %v, want пусто", plan.Orphans)
	}
	if plan.Empty() {
		t.Error("Plan.Empty() = true для плана с двумя записями")
	}
}

// issueFor возвращает проблему сборки с указанным кодом; ok=false, если ошибки
// нет или такого кода среди проблем нет.
func issueFor(err error, code string) (BuildIssue, bool) {
	var be *BuildError
	if !errors.As(err, &be) {
		return BuildIssue{}, false
	}
	for _, is := range be.Issues {
		if is.Code == code {
			return is, true
		}
	}
	return BuildIssue{}, false
}

func xrayFile(rel, content string) GeneratedFile {
	return GeneratedFile{Kernel: KernelXray, RelPath: rel, Kind: KindXrayJSON, Content: []byte(content)}
}

func providerFile(content string) GeneratedFile {
	return GeneratedFile{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-a.yaml", Kind: KindMihomoProxyProvider, Content: []byte(content)}
}

func TestBuild_StoplistName(t *testing.T) {
	files := []GeneratedFile{xrayFile("04_outbounds.xcp-folder.tail.json", "{}\n")}
	err := CheckGenerated(files, nil)
	is, ok := issueFor(err, IssueStoplistName)
	if !ok {
		t.Fatalf("нет проблемы %q, err = %v", IssueStoplistName, err)
	}
	if !strings.Contains(is.Detail, "old") {
		t.Errorf("Detail = %q, want упоминание слова old", is.Detail)
	}
	if files[0].RelPath != "04_outbounds.xcp-folder.tail.json" {
		t.Errorf("имя файла изменено: %q", files[0].RelPath)
	}
}

func TestBuild_TransportKeyRejected(t *testing.T) {
	files := []GeneratedFile{xrayFile("04_outbounds.xcp-a.tail.json", `{"outbounds":[{"streamSettings":{"transport" : {}}}]}`)}
	if _, ok := issueFor(CheckGenerated(files, nil), IssueTransportKeyForbidden); !ok {
		t.Fatalf("ключ transport не отклонён")
	}
}

func TestBuild_InvalidJSONAndNotObject(t *testing.T) {
	err := CheckGenerated([]GeneratedFile{xrayFile("04_outbounds.xcp-a.tail.json", "{ broken")}, nil)
	if _, ok := issueFor(err, IssueInvalidJSON); !ok {
		t.Errorf("битый JSON: нет %q, err = %v", IssueInvalidJSON, err)
	}
	if _, ok := issueFor(err, IssueNotObject); ok {
		t.Errorf("битый JSON не должен давать %q", IssueNotObject)
	}
	err = CheckGenerated([]GeneratedFile{xrayFile("04_outbounds.xcp-a.tail.json", "[1,2]")}, nil)
	if _, ok := issueFor(err, IssueNotObject); !ok {
		t.Errorf("массив: нет %q, err = %v", IssueNotObject, err)
	}
}

func TestBuild_MihomoProviderContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		code    string
	}{
		{"невалидный YAML", "proxies: [unclosed\n", IssueProviderYAMLInvalid},
		{"нет ключа proxies", "a: 1\n", IssueProviderMissingKey},
		{"пустой список", "proxies: []\n", IssueProviderEmpty},
		{"пустой файл", "\n", IssueProviderEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckGenerated([]GeneratedFile{providerFile(tc.content)}, nil)
			if _, ok := issueFor(err, tc.code); !ok {
				t.Fatalf("нет проблемы %q, err = %v", tc.code, err)
			}
		})
	}
	ok := "proxies:\n  - name: n\n    type: socks5\n    server: 127.0.0.1\n    port: 1\n"
	if err := CheckGenerated([]GeneratedFile{providerFile(ok)}, nil); err != nil {
		t.Errorf("валидный провайдер отклонён: %v", err)
	}

	broken, err := diagRegistry(true).Build(Sections{DiagSection: []byte(`{"enabled":true,"broken_mihomo":true}`)}, InstalledKernels{Mihomo: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := issueFor(CheckGenerated(broken, nil), IssueProviderYAMLInvalid); !ok {
		t.Errorf("диагностический broken_mihomo не отклонён собственной проверкой")
	}
}

func TestBuild_NameTakenByForeign(t *testing.T) {
	files := []GeneratedFile{xrayFile(DiagXrayRel, "{}\n")}
	foreign := func(kernel, rel string) bool { return kernel == KernelXray && rel == DiagXrayRel }
	if _, ok := issueFor(CheckGenerated(files, foreign), IssueNameTaken); !ok {
		t.Fatal("имя, занятое старым слоем, не отклонено")
	}
	if err := CheckGenerated(files, func(string, string) bool { return false }); err != nil {
		t.Errorf("свободное имя отклонено: %v", err)
	}
}

func TestBuild_InvalidPanelName(t *testing.T) {
	err := CheckGenerated([]GeneratedFile{xrayFile("zz_xcp_x.json", "{}\n")}, nil)
	if _, ok := issueFor(err, IssueInvalidPanelName); !ok {
		t.Fatalf("нет %q, err = %v", IssueInvalidPanelName, err)
	}
	if _, ok := issueFor(err, IssueStoplistName); ok {
		t.Errorf("имя вне правила панели не должно давать %q", IssueStoplistName)
	}
}

func TestBuild_AllIssuesAtOnce(t *testing.T) {
	files := []GeneratedFile{
		xrayFile("04_outbounds.xcp-a.tail.json", `{"transport":{}}`),
		xrayFile("zz_xcp_x.json", "{}\n"),
	}
	var be *BuildError
	if !errors.As(CheckGenerated(files, nil), &be) || len(be.Issues) != 2 {
		t.Fatalf("ожидалось две проблемы разом, got %+v", be)
	}
}

// planFixture — корни во временных каталогах и два желаемых файла (по одному
// на ядро) с готовой записью манифеста.
type planFixture struct {
	roots   Roots
	xray    GeneratedFile
	mihomo  GeneratedFile
	desired []GeneratedFile
}

func newPlanFixture(t *testing.T) planFixture {
	t.Helper()
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	xray := xrayFile("04_outbounds.xcp-a.tail.json", "{\"outbounds\":[]}\n")
	mihomo := providerFile("proxies:\n  - name: n\n    type: socks5\n    server: 127.0.0.1\n    port: 1\n")
	return planFixture{roots: roots, xray: xray, mihomo: mihomo, desired: []GeneratedFile{xray, mihomo}}
}

// put записывает файл на диск (создавая каталоги) по пути из Roots.Abs.
func (fx planFixture) put(t *testing.T, f GeneratedFile, content string) string {
	t.Helper()
	abs, err := fx.roots.Abs(f.Kernel, f.RelPath)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return abs
}

func entryFor(f GeneratedFile, status EntryStatus) ManifestEntry {
	return ManifestEntry{Kernel: f.Kernel, RelPath: f.RelPath, Kind: f.Kind, Hash: HashContent(f.Content), Status: status}
}

func findFile(p Plan, key string) (FilePlan, bool) {
	for _, fp := range p.Files {
		if fp.Key == key {
			return fp, true
		}
	}
	return FilePlan{}, false
}

func TestComputePlan_Idempotent(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, fx.xray, string(fx.xray.Content))
	fx.put(t, fx.mihomo, string(fx.mihomo.Content))
	manifest := map[string]ManifestEntry{
		fx.xray.Key():   entryFor(fx.xray, StatusManaged),
		fx.mihomo.Key(): entryFor(fx.mihomo, StatusManaged),
	}
	plan, err := ComputePlan(fx.roots, fx.desired, manifest, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if !plan.Empty() {
		t.Fatalf("повторное применение без изменений дало непустой план: %+v", plan)
	}
}

func TestComputePlan_ReleasedSkipped(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, fx.xray, "{\"edited\":true}\n")
	manifest := map[string]ManifestEntry{fx.xray.Key(): entryFor(fx.xray, StatusReleased)}

	plan, err := ComputePlan(fx.roots, []GeneratedFile{fx.xray}, manifest, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	fp, ok := findFile(plan, fx.xray.Key())
	if !ok || fp.Action != ActionSkipReleased {
		t.Fatalf("отпущенный файл: %+v (found=%v), want %q", fp, ok, ActionSkipReleased)
	}
	if !plan.Empty() {
		t.Errorf("skip_released не должен считаться действием: %+v", plan)
	}

	plan, err = ComputePlan(fx.roots, []GeneratedFile{fx.xray}, manifest, map[string]bool{fx.xray.Key(): true}, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	fp, ok = findFile(plan, fx.xray.Key())
	if !ok || fp.Action != ActionWrite || !fp.WasReleased {
		t.Fatalf("«Пересобрать» по ключу: %+v (found=%v), want write с WasReleased", fp, ok)
	}
}

func TestComputePlan_StaleDeleted(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, fx.xray, string(fx.xray.Content))
	manifest := map[string]ManifestEntry{
		fx.xray.Key():   entryFor(fx.xray, StatusManaged),
		fx.mihomo.Key(): entryFor(fx.mihomo, StatusReleased),
	}
	plan, err := ComputePlan(fx.roots, nil, manifest, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	fp, ok := findFile(plan, fx.xray.Key())
	if !ok || fp.Action != ActionDelete {
		t.Fatalf("запись без генерации: %+v (found=%v), want %q", fp, ok, ActionDelete)
	}
	if fp.AbsPath == "" {
		t.Error("у удаления пустой AbsPath")
	}
	if _, ok := findFile(plan, fx.mihomo.Key()); ok {
		t.Error("отпущенная запись без генерации не должна попадать в план")
	}
}

func TestComputePlan_RenamedRemovesObsolete(t *testing.T) {
	fx := newPlanFixture(t)
	abs := fx.put(t, fx.xray, string(fx.xray.Content))
	if err := os.Rename(abs, abs+".obsolete"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	manifest := map[string]ManifestEntry{fx.xray.Key(): entryFor(fx.xray, StatusManaged)}
	plan, err := ComputePlan(fx.roots, []GeneratedFile{fx.xray}, manifest, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	fp, ok := findFile(plan, fx.xray.Key())
	if !ok || fp.Action != ActionWrite || !fp.RemoveObsolete {
		t.Fatalf("переименованный в .obsolete: %+v (found=%v), want write с RemoveObsolete", fp, ok)
	}
}

func TestComputePlan_OverwriteUnmanifestedDesired(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, fx.xray, "{\"old\":1}\n")
	plan, err := ComputePlan(fx.roots, []GeneratedFile{fx.xray}, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	fp, ok := findFile(plan, fx.xray.Key())
	if !ok || fp.Action != ActionWrite {
		t.Fatalf("файл панели вне манифеста, но генерируемый: %+v (found=%v), want write", fp, ok)
	}
	if len(plan.Orphans) != 0 {
		t.Errorf("генерируемый файл попал в сироты: %+v", plan.Orphans)
	}
}

func TestComputePlan_Orphans(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, xrayFile("xcp-orphan.json", ""), "{}\n")
	fx.put(t, xrayFile("04_outbounds.zz_xcp_selected.tail.json", ""), "{}\n")
	plan, err := ComputePlan(fx.roots, nil, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plan.Orphans) != 1 || plan.Orphans[0].Key != ManifestKey(KernelXray, "xcp-orphan.json") {
		t.Fatalf("Orphans = %+v, want только xcp-orphan.json", plan.Orphans)
	}
	if plan.Empty() {
		t.Error("план с сиротой не должен быть пустым")
	}
}

func TestComputePlan_OnlyRestricts(t *testing.T) {
	fx := newPlanFixture(t)
	fx.put(t, xrayFile("xcp-orphan.json", ""), "{}\n")
	only := map[string]bool{fx.xray.Key(): true}
	manifest := map[string]ManifestEntry{
		ManifestKey(KernelMihomo, "proxy_providers/xcp-gone.yaml"): {Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-gone.yaml", Kind: KindMihomoProxyProvider, Status: StatusManaged},
	}
	plan, err := ComputePlan(fx.roots, fx.desired, manifest, only, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Key != fx.xray.Key() {
		t.Fatalf("Only не сузил план до названного файла: %+v", plan.Files)
	}
	if len(plan.Orphans) != 0 {
		t.Errorf("при Only сироты не собираются: %+v", plan.Orphans)
	}
}

func TestComputePlan_Changes(t *testing.T) {
	fx := newPlanFixture(t)
	plan, err := ComputePlan(fx.roots, []GeneratedFile{fx.xray}, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if !plan.Changes(KernelXray) || plan.Changes(KernelMihomo) {
		t.Errorf("Changes: xray=%v mihomo=%v, want true/false", plan.Changes(KernelXray), plan.Changes(KernelMihomo))
	}

	fx.put(t, GeneratedFile{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-stray.yaml"}, "proxies: []\n")
	plan, err = ComputePlan(fx.roots, nil, map[string]ManifestEntry{}, nil, nil)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Changes(KernelXray) || !plan.Changes(KernelMihomo) {
		t.Errorf("сирота Mihomo: xray=%v mihomo=%v, want false/true", plan.Changes(KernelXray), plan.Changes(KernelMihomo))
	}
}
