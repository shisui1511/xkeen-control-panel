package configlayer

import (
	"errors"
	"sort"
	"testing"
)

// fakeGenerator выдаёт заранее заданные файлы (или ошибку).
type fakeGenerator struct {
	id    string
	files []GeneratedFile
	err   error
}

func (g fakeGenerator) ID() string { return g.id }

func (g fakeGenerator) Generate(Sections, InstalledKernels) ([]GeneratedFile, error) {
	return g.files, g.err
}

func TestRegistry_DuplicateAndSorted(t *testing.T) {
	a := xrayFile("04_outbounds.xcp-b.tail.json", "{}\n")
	b := xrayFile("04_outbounds.xcp-a.tail.json", "{}\n")
	m := GeneratedFile{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-a.yaml", Kind: KindMihomoProxyProvider, Content: []byte("x")}

	r := NewRegistry()
	r.Register(fakeGenerator{id: "one", files: []GeneratedFile{a, m}})
	r.Register(fakeGenerator{id: "two", files: []GeneratedFile{b}})
	got, err := r.Build(Sections{}, InstalledKernels{Xray: true, Mihomo: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	keys := make([]string, len(got))
	for i, f := range got {
		keys[i] = f.Key()
	}
	if !sort.StringsAreSorted(keys) || len(keys) != 3 {
		t.Fatalf("ключи не отсортированы или их не три: %v", keys)
	}

	dup := NewRegistry()
	dup.Register(fakeGenerator{id: "one", files: []GeneratedFile{a}})
	dup.Register(fakeGenerator{id: "two", files: []GeneratedFile{a}})
	_, err = dup.Build(Sections{}, InstalledKernels{Xray: true})
	if _, ok := issueFor(err, IssueDuplicateFile); !ok {
		t.Fatalf("конфликт ключей без %q, err = %v", IssueDuplicateFile, err)
	}

	only := NewRegistry()
	only.Register(fakeGenerator{id: "one", files: []GeneratedFile{a, m}})
	got, err = only.Build(Sections{}, InstalledKernels{Xray: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got) != 1 || got[0].Kernel != KernelXray {
		t.Fatalf("файлы неустановленного ядра не отброшены: %+v", got)
	}
}

func TestRegistry_GeneratorFailed(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeGenerator{id: "broken", err: errors.New("сбой")})
	_, err := r.Build(Sections{}, InstalledKernels{Xray: true})
	is, ok := issueFor(err, IssueGeneratorFailed)
	if !ok {
		t.Fatalf("нет %q, err = %v", IssueGeneratorFailed, err)
	}
	if is.Detail == "" || is.Key != "broken" {
		t.Errorf("issue = %+v, want Key=broken и Detail с ID генератора", is)
	}
}

func TestDiag_DevModeOff(t *testing.T) {
	src := Sections{DiagSection: []byte(`{"enabled":true}`)}
	got, err := diagRegistry(false).Build(src, InstalledKernels{Xray: true, Mihomo: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("вне dev_mode выдано %d файлов, want 0", len(got))
	}

	got, err = diagRegistry(true).Build(Sections{DiagSection: []byte(`{"enabled":false}`)}, InstalledKernels{Xray: true, Mihomo: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("enabled=false: files=%d err=%v, want 0 и nil", len(got), err)
	}

	if _, err := DiagSectionFor("nope"); !errors.Is(err, ErrUnknownDiagAction) {
		t.Errorf("DiagSectionFor(nope) err = %v, want ErrUnknownDiagAction", err)
	}
	if raw, err := DiagSectionFor("remove"); err != nil || raw != nil {
		t.Errorf("DiagSectionFor(remove) = %q, %v, want nil, nil", raw, err)
	}
	raw, err := DiagSectionFor("add_broken_xray")
	if err != nil {
		t.Fatalf("add_broken_xray: %v", err)
	}
	files, err := diagRegistry(true).Build(Sections{DiagSection: raw}, InstalledKernels{Xray: true})
	if err != nil || len(files) != 1 || string(files[0].Content) == "{}\n" {
		t.Errorf("add_broken_xray не даёт битый файл Xray: files=%+v err=%v", files, err)
	}
}
