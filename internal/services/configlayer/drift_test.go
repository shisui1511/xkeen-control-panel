package configlayer

import (
	"os"
	"path/filepath"
	"testing"
)

// writeXrayFile кладёт файл в корень Xray временного каталога.
func writeXrayFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestDrift_TracerOKAndModified(t *testing.T) {
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	const name = "04_outbounds.xcp-diag.tail.json"
	body := []byte("{}\n")
	writeXrayFile(t, roots.Xray, name, string(body))

	entry := ManifestEntry{
		Kernel:  KernelXray,
		RelPath: name,
		Kind:    KindXrayJSON,
		Hash:    HashContent(body),
		Status:  StatusManaged,
	}

	got := CheckEntry(roots, entry)
	if got.State != StateOK {
		t.Fatalf("State = %q, want %q", got.State, StateOK)
	}
	if got.AbsPath != filepath.Join(roots.Xray, name) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, filepath.Join(roots.Xray, name))
	}

	writeXrayFile(t, roots.Xray, name, "{\"changed\":true}\n")
	got = CheckEntry(roots, entry)
	if got.State != StateDriftModified {
		t.Fatalf("State after edit = %q, want %q", got.State, StateDriftModified)
	}
	if got.ActualHash == "" || got.ActualHash == entry.Hash {
		t.Fatalf("ActualHash = %q, must differ from manifest hash %q", got.ActualHash, entry.Hash)
	}
}

// writeFile создаёт файл (с недостающими каталогами) по пути относительно корня.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestDrift_Kinds(t *testing.T) {
	const (
		xrayName   = "04_outbounds.xcp-a.tail.json"
		proxiesRel = "proxy_providers/xcp-a.yaml"
		rulesRel   = "rule_providers/xcp-r.yaml"
	)
	tests := []struct {
		name         string
		kernel       string
		rel          string
		kind         FileKind
		status       EntryStatus
		onDisk       map[string]string // путь относительно корня ядра → содержимое
		wantState    FileState
		wantObsolete string
	}{
		{name: "файла нет", kernel: KernelXray, rel: xrayName, kind: KindXrayJSON, status: StatusManaged,
			wantState: StateDriftMissing},
		{name: "файл переименован снаружи в .obsolete", kernel: KernelXray, rel: xrayName, kind: KindXrayJSON, status: StatusManaged,
			onDisk:    map[string]string{xrayName + ".obsolete": "{}"},
			wantState: StateDriftRenamed, wantObsolete: xrayName + ".obsolete"},
		{name: "провайдер Mihomo пустой", kernel: KernelMihomo, rel: proxiesRel, kind: KindMihomoProxyProvider, status: StatusManaged,
			onDisk: map[string]string{proxiesRel: ""}, wantState: StateDriftEmpty},
		{name: "провайдер Mihomo из пробелов", kernel: KernelMihomo, rel: proxiesRel, kind: KindMihomoProxyProvider, status: StatusManaged,
			onDisk: map[string]string{proxiesRel: "  \n\t\n"}, wantState: StateDriftEmpty},
		{name: "провайдер без ключа proxies", kernel: KernelMihomo, rel: proxiesRel, kind: KindMihomoProxyProvider, status: StatusManaged,
			onDisk: map[string]string{proxiesRel: "other: 1\n"}, wantState: StateDriftEmpty},
		{name: "правило-провайдер без payload", kernel: KernelMihomo, rel: rulesRel, kind: KindMihomoRuleProvider, status: StatusManaged,
			onDisk: map[string]string{rulesRel: "other: 1\n"}, wantState: StateDriftEmpty},
		{name: "провайдер с пустым списком proxies", kernel: KernelMihomo, rel: proxiesRel, kind: KindMihomoProxyProvider, status: StatusManaged,
			onDisk: map[string]string{proxiesRel: "proxies: []\n"}, wantState: StateDriftEmpty},
		{name: "провайдер изменён, но валиден", kernel: KernelMihomo, rel: proxiesRel, kind: KindMihomoProxyProvider, status: StatusManaged,
			onDisk: map[string]string{proxiesRel: "proxies:\n  - name: x\n    type: ss\n"}, wantState: StateDriftModified},
		{name: "отпущенная запись без файла", kernel: KernelXray, rel: xrayName, kind: KindXrayJSON, status: StatusReleased,
			wantState: StateReleased},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
			for rel, content := range tc.onDisk {
				root := roots.Xray
				if tc.kernel == KernelMihomo {
					root = roots.Mihomo
				}
				writeFile(t, root, rel, content)
			}
			// хэш из манифеста заведомо не совпадает с содержимым на диске
			entry := ManifestEntry{Kernel: tc.kernel, RelPath: tc.rel, Kind: tc.kind, Hash: HashContent([]byte("записано панелью")), Status: tc.status}
			got := CheckEntry(roots, entry)
			if got.State != tc.wantState {
				t.Fatalf("State = %q, want %q", got.State, tc.wantState)
			}
			if got.ObsoleteName != tc.wantObsolete {
				t.Fatalf("ObsoleteName = %q, want %q", got.ObsoleteName, tc.wantObsolete)
			}
			if got.Key != ManifestKey(tc.kernel, tc.rel) {
				t.Fatalf("Key = %q, want %q", got.Key, ManifestKey(tc.kernel, tc.rel))
			}
		})
	}
}

func TestProviderContentProblem(t *testing.T) {
	tests := []struct {
		name    string
		kind    FileKind
		content string
		want    string
	}{
		{"невалидный YAML", KindMihomoProxyProvider, "proxies: [unclosed\n", "provider_yaml_invalid"},
		{"нет ключа proxies", KindMihomoProxyProvider, "other: 1\n", "provider_missing_key"},
		{"нет ключа payload", KindMihomoRuleProvider, "proxies:\n  - a\n", "provider_missing_key"},
		{"пустой список", KindMihomoProxyProvider, "proxies: []\n", "provider_empty"},
		{"пустой файл", KindMihomoRuleProvider, "", "provider_empty"},
		{"один узел", KindMihomoProxyProvider, "proxies:\n  - name: x\n", ""},
		{"одно правило", KindMihomoRuleProvider, "payload:\n  - DOMAIN,example.org\n", ""},
		{"Xray проверяет сборка", KindXrayJSON, "", ""},
		{"профиль Mihomo не проверяется", KindMihomoProfile, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProviderContentProblem(tc.kind, []byte(tc.content)); got != tc.want {
				t.Fatalf("ProviderContentProblem = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckManifest_Sorted(t *testing.T) {
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	m := map[string]ManifestEntry{}
	for _, name := range []string{"xcp-c.json", "xcp-a.json", "xcp-b.json"} {
		e := ManifestEntry{Kernel: KernelXray, RelPath: name, Kind: KindXrayJSON, Status: StatusManaged}
		m[ManifestKey(e.Kernel, e.RelPath)] = e
	}
	e := ManifestEntry{Kernel: KernelMihomo, RelPath: "proxy_providers/xcp-z.yaml", Kind: KindMihomoProxyProvider, Status: StatusReleased}
	m[ManifestKey(e.Kernel, e.RelPath)] = e

	got := CheckManifest(roots, m)
	want := []string{"mihomo:proxy_providers/xcp-z.yaml", "xray:xcp-a.json", "xray:xcp-b.json", "xray:xcp-c.json"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("got[%d].Key = %q, want %q", i, got[i].Key, k)
		}
	}
}

func TestScanOrphans(t *testing.T) {
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	for _, name := range []string{
		"xcp-orphan.json",
		"xcp-known.json",
		"04_outbounds.zz_xcp_selected.tail.json",
		"01_log.json",
		"xcp-a.json.obsolete",
		"atomic-77",
	} {
		writeFile(t, roots.Xray, name, "{}")
	}
	writeFile(t, roots.Mihomo, "proxy_providers/xcp-gone.yaml", "proxies: []\n")
	writeFile(t, roots.Mihomo, "proxy_providers/sub.yaml", "proxies: []\n")
	writeFile(t, roots.Mihomo, "config.yaml.xcp-link", "x")
	writeFile(t, roots.Mihomo, "xcp-root.yaml", "x")

	m := map[string]ManifestEntry{
		"xray:xcp-known.json": {Kernel: KernelXray, RelPath: "xcp-known.json", Kind: KindXrayJSON, Status: StatusManaged},
	}

	got, err := ScanOrphans(roots, m, nil)
	if err != nil {
		t.Fatalf("ScanOrphans: %v", err)
	}
	var keys []string
	for _, o := range got {
		keys = append(keys, o.Key)
	}
	want := []string{"mihomo:proxy_providers/xcp-gone.yaml", "xray:xcp-orphan.json"}
	if len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("orphans = %v, want %v", keys, want)
	}
	if got[1].AbsPath != filepath.Join(roots.Xray, "xcp-orphan.json") {
		t.Fatalf("AbsPath = %q", got[1].AbsPath)
	}

	// файл, принадлежащий старому слою, сиротой не считается
	got, err = ScanOrphans(roots, m, func(kernel, rel string) bool { return rel == "xcp-orphan.json" })
	if err != nil {
		t.Fatalf("ScanOrphans foreign: %v", err)
	}
	if len(got) != 1 || got[0].Key != "mihomo:proxy_providers/xcp-gone.yaml" {
		t.Fatalf("with foreignOwned orphans = %+v", got)
	}

	// отсутствующие каталоги Mihomo — не ошибка
	empty := Roots{Xray: t.TempDir(), Mihomo: filepath.Join(t.TempDir(), "нет-такого")}
	got, err = ScanOrphans(empty, m, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("пустые корни: orphans = %v, err = %v", got, err)
	}
}
