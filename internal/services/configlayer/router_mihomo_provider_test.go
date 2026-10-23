//go:build router

package configlayer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Проверка файлов провайдеров Mihomo на устройстве: настоящие хранилище, конвейер
// и бинарник Mihomo роутера (проверка `mihomo -t`), временный каталог в рабочем
// каталоге теста. Работающее ядро стенда тесты не трогают, перезапуск не
// настроен (Applier == nil). Вердикт — файлы на диске, ответ слоя и код выхода
// `mihomo -t`, а не эталон.

const rtMhBin = "/opt/sbin/mihomo"

// rtMhTextBody — содержимое тестового текстового провайдера правил.
const rtMhTextBody = "+.example.com\nfull.example.org\n+.example.net\n"

// rtMhTextGen выдаёт один текстовый rule-provider с управляемым содержимым.
type rtMhTextGen struct {
	mu      sync.Mutex
	content string
}

func (g *rtMhTextGen) set(c string) {
	g.mu.Lock()
	g.content = c
	g.mu.Unlock()
}

func (g *rtMhTextGen) ID() string { return "rt-mh-text" }

func (g *rtMhTextGen) Generate(Input, InstalledKernels) ([]GeneratedFile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return []GeneratedFile{{
		Kernel:  KernelMihomo,
		RelPath: "rule_providers/xcp-ls-test-d.txt",
		Kind:    KindMihomoRuleProviderText,
		Content: []byte(g.content),
		Inert:   true,
	}}, nil
}

func TestRouterTextRuleProvider(t *testing.T) {
	if _, err := os.Stat(rtMhBin); err != nil {
		t.Skipf("Mihomo не установлен на устройстве: %v", err)
	}
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, filepath.Join(roots.Mihomo, "rule_providers")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(roots.Mihomo, "config.yaml"), []byte("mode: rule\nrules:\n  - MATCH,DIRECT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		Binaries:      func() Binaries { return Binaries{Mihomo: rtMhBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	gen := &rtMhTextGen{content: rtMhTextBody}
	if err := layer.RegisterGenerator(gen); err != nil {
		t.Fatal(err)
	}
	layer.Start()

	const rel = "rule_providers/xcp-ls-test-d.txt"
	key := ManifestKey(KernelMihomo, rel)
	file := filepath.Join(roots.Mihomo, rel)
	ctx := context.Background()

	if !IsPanelFileName(KernelMihomo, rel) {
		t.Errorf("%s не распознан как файл панели", rel)
	}
	if err := ValidatePanelName(KernelMihomo, rel); err != nil {
		t.Errorf("ValidatePanelName(%s): %v", rel, err)
	}
	// Расширение .txt допустимо только в rule_providers.
	for _, bad := range []string{"proxy_providers/xcp-ls-test-d.txt", "profiles/xcp-ls-test-d.txt", "rule_providers/ls-test-d.txt"} {
		if IsPanelFileName(KernelMihomo, bad) {
			t.Errorf("%s принят как файл панели", bad)
		}
	}

	t.Run("WrittenAndInManifest", func(t *testing.T) {
		view, err := layer.RunSourceRefresh(ctx, nil)
		if err != nil || view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("применение: %v, %+v", err, view.Result)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != rtMhTextBody {
			t.Fatalf("файл провайдера: %v, %q", err, data)
		}
		entry, ok := layer.store.Snapshot().Manifest[key]
		if !ok || entry.Kind != KindMihomoRuleProviderText || entry.Status != StatusManaged {
			t.Fatalf("запись манифеста: %+v (есть: %v)", entry, ok)
		}
		if snap := layer.Snapshot(); snap.DriftCount != 0 {
			t.Errorf("дрейф сразу после записи: %d", snap.DriftCount)
		}
	})

	t.Run("DriftOnEdit", func(t *testing.T) {
		if err := os.WriteFile(file, []byte(rtMhTextBody+"+.edited.example\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		snap := layer.Snapshot()
		var state FileState
		for _, f := range snap.Files {
			if f.Key == key {
				state = f.State
			}
		}
		if state != StateDriftModified || snap.DriftCount != 1 {
			t.Errorf("правка файла: состояние %q, дрейф %d", state, snap.DriftCount)
		}
		if err := os.WriteFile(file, []byte(rtMhTextBody), 0o644); err != nil {
			t.Fatal(err)
		}
		if snap := layer.Snapshot(); snap.DriftCount != 0 {
			t.Errorf("дрейф после возврата содержимого: %d", snap.DriftCount)
		}
	})

	t.Run("EmptyRejected", func(t *testing.T) {
		for _, empty := range []string{"", "  \n\t\n", "# только комментарий\n\n"} {
			gen.set(empty)
			view, err := layer.RunSourceRefresh(ctx, nil)
			if err != nil || view.Result == nil || view.Result.OK || view.Result.Code != ResultBuildFailed {
				t.Fatalf("пустой текст %q принят: %v, %+v", empty, err, view.Result)
			}
			found := false
			for _, is := range view.Result.Issues {
				if is.Key == key && is.Code == IssueProviderEmpty {
					found = true
				}
			}
			if !found {
				t.Errorf("для %q нет проблемы provider_empty: %+v", empty, view.Result.Issues)
			}
		}
		// NUL-байт — содержимое не текст.
		gen.set("+.example.com\x00\n")
		view, err := layer.RunSourceRefresh(ctx, nil)
		if err != nil || view.Result == nil || view.Result.OK {
			t.Fatalf("текст с NUL принят: %v, %+v", err, view.Result)
		}
		data, err := os.ReadFile(file)
		if err != nil || string(data) != rtMhTextBody {
			t.Errorf("файл изменился при отказе сборки: %v, %q", err, data)
		}
	})

	t.Run("MihomoAcceptsTextFormat", func(t *testing.T) {
		work := filepath.Join(dir, "mihomo-t")
		if err := os.MkdirAll(filepath.Join(work, "rule_providers"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, rel), []byte(rtMhTextBody), 0o644); err != nil {
			t.Fatal(err)
		}
		profile := "mode: rule\n" +
			"rule-providers:\n" +
			"  xcp-ls-test-d:\n" +
			"    type: file\n" +
			"    behavior: domain\n" +
			"    format: text\n" +
			"    path: ./rule_providers/xcp-ls-test-d.txt\n" +
			"rules:\n" +
			"  - RULE-SET,xcp-ls-test-d,DIRECT\n" +
			"  - MATCH,DIRECT\n"
		cfg := filepath.Join(work, "config.yaml")
		if err := os.WriteFile(cfg, []byte(profile), 0o644); err != nil {
			t.Fatal(err)
		}
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, rtMhBin, "-t", "-d", work, "-f", cfg).CombinedOutput()
		if err != nil {
			t.Fatalf("mihomo -t отверг профиль со ссылкой на текстовый провайдер: %v: %s", err, strings.TrimSpace(string(out)))
		}
	})
}
