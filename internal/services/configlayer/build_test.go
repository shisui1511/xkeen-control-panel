package configlayer

import (
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
