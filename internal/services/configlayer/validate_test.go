package configlayer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mihomoPlanWrite — план с записью одного файла Mihomo.
func mihomoPlanWrite(rel, content string) Plan {
	return Plan{Files: []FilePlan{{
		Key: ManifestKey(KernelMihomo, rel), Kernel: KernelMihomo, RelPath: rel,
		Action: ActionWrite, Kind: KindMihomoProxyProvider, Content: []byte(content),
	}}}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestValidate_MihomoMirrorAndArgs(t *testing.T) {
	root := t.TempDir()
	profile := "mixed-port: 7890\nmode: rule\n"
	writeTestFile(t, filepath.Join(root, "profiles", "a.yaml"), profile)
	if err := os.Symlink(filepath.Join("profiles", "a.yaml"), filepath.Join(root, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "geoip.dat"), "geo")
	writeTestFile(t, filepath.Join(root, "proxy_providers", "sub.yaml"), "proxies: []\n")
	writeTestFile(t, filepath.Join(root, "rules", "r.yaml"), "payload: []\n")

	binDir := t.TempDir()
	dump := filepath.Join(binDir, "mihomo.dump")
	bin := writeFakeKernelScript(t, binDir, "mihomo", strings.Join([]string{
		`echo "$@" >> ` + shellQuote(filepath.Join(binDir, "mihomo.args.log")),
		`d="$3"`,
		`for p in "$d/config.yaml" "$d/geoip.dat" "$d/proxy_providers/xcp-diag.yaml" "$d/proxy_providers/sub.yaml" "$d/rules/r.yaml" "$d/profiles"; do`,
		`  if [ -L "$p" ]; then echo "L ${p#$d/}"`,
		`  elif [ -f "$p" ]; then echo "F ${p#$d/} $(tr '\n' '|' < "$p")"`,
		`  elif [ -e "$p" ]; then echo "E ${p#$d/}"`,
		`  else echo "M ${p#$d/}"; fi`,
		`done > ` + shellQuote(dump),
		`exit 0`,
	}, "\n"))

	subBefore := mustRead(t, filepath.Join(root, "proxy_providers", "sub.yaml"))
	dataDir := t.TempDir()
	plan := mihomoPlanWrite(DiagMihomoRel, "proxies:\n  - name: x\n")
	res := ValidateMihomo(context.Background(), filepath.Join(dataDir, "tmp"), Roots{Mihomo: root}, bin, plan)
	if !res.OK || res.Skipped {
		t.Fatalf("результат = %+v, want OK", res)
	}

	args := readArgsLog(t, binDir, "mihomo")
	if len(args) != 1 {
		t.Fatalf("запусков = %d: %v", len(args), args)
	}
	f := strings.Fields(args[0])
	if len(f) != 5 || f[0] != "-t" || f[1] != "-d" || f[3] != "-f" ||
		!strings.HasPrefix(f[2], filepath.Join(dataDir, "tmp", "apply-")) || f[4] != f[2]+"/config.yaml" {
		t.Errorf("аргументы = %q, want -t -d <tmp> -f <tmp>/config.yaml", args[0])
	}

	lines := strings.Split(strings.TrimSpace(mustRead(t, dump)), "\n")
	want := []string{
		"F config.yaml " + strings.ReplaceAll(profile, "\n", "|"),
		"L geoip.dat",
		"F proxy_providers/xcp-diag.yaml proxies:|  - name: x|",
		"L proxy_providers/sub.yaml",
		"L rules/r.yaml",
		"M profiles",
	}
	if len(lines) != len(want) {
		t.Fatalf("снимок = %q", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("снимок[%d] = %q, want %q", i, lines[i], want[i])
		}
	}

	if got := mustRead(t, filepath.Join(root, "proxy_providers", "sub.yaml")); got != subBefore {
		t.Errorf("sub.yaml изменён: %q", got)
	}
	if got := mustRead(t, filepath.Join(root, "profiles", "a.yaml")); got != profile {
		t.Errorf("profiles/a.yaml изменён: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, DiagMihomoRel)); err == nil {
		t.Error("файл плана записан в рабочий каталог")
	}
	requireNoApplyDirs(t, dataDir)
}

func TestValidate_MihomoOverlayNoWriteThrough(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config.yaml"), "mixed-port: 7890\n")
	work := filepath.Join(root, DiagMihomoRel)
	oldContent := "proxies:\n  - name: old\n"
	writeTestFile(t, work, oldContent)

	binDir := t.TempDir()
	bin := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	plan := mihomoPlanWrite(DiagMihomoRel, "proxies:\n  - name: new\n")

	res := ValidateMihomo(context.Background(), filepath.Join(t.TempDir(), "tmp"), Roots{Mihomo: root}, bin, plan)
	if !res.OK {
		t.Fatalf("результат = %+v, want OK", res)
	}
	if got := mustRead(t, work); got != oldContent {
		t.Errorf("рабочий файл изменён сквозь симлинк: %q, want %q", got, oldContent)
	}
}

func TestValidate_MihomoNoConfig(t *testing.T) {
	root := t.TempDir()
	bin := writeFakeKernel(t, t.TempDir(), "mihomo", 0, "", 0)
	res := ValidateMihomo(context.Background(), filepath.Join(t.TempDir(), "tmp"), Roots{Mihomo: root}, bin,
		mihomoPlanWrite(DiagMihomoRel, "proxies:\n  - name: x\n"))
	if !res.Skipped || res.NoteCode != NoteNoConfig {
		t.Errorf("результат = %+v, want skipped no_config", res)
	}
}

func TestValidateTimeout_Arch(t *testing.T) {
	cases := []struct {
		goarch string
		want   time.Duration
	}{
		{"mipsle", 180 * time.Second},
		{"mips", 180 * time.Second},
		{"arm64", 60 * time.Second},
		{"amd64", 60 * time.Second},
	}
	for _, c := range cases {
		if got := defaultValidateTimeout(c.goarch); got != c.want {
			t.Errorf("defaultValidateTimeout(%q) = %v, want %v", c.goarch, got, c.want)
		}
	}
}
