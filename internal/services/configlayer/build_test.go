package configlayer

import (
	"errors"
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
