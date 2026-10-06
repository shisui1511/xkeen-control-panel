package configlayer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFakeKernelScript пишет исполняемый скрипт #!/bin/sh с произвольным
// телом. Настоящие xray/mihomo/xkeen на машине разработчика не запускаются.
func writeFakeKernelScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeFakeKernel пишет фейковое ядро: дописывает свои аргументы в
// <dir>/<name>.args.log, спит sleep (если больше нуля), печатает output и
// завершается с exitCode.
func writeFakeKernel(t *testing.T, dir, name string, exitCode int, output string, sleep time.Duration) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "echo \"$@\" >> %q\n", filepath.Join(dir, name+".args.log"))
	if sleep > 0 {
		fmt.Fprintf(&b, "sleep %g\n", sleep.Seconds())
	}
	if output != "" {
		fmt.Fprintf(&b, "printf '%%s\\n' %s\n", shellQuote(output))
	}
	fmt.Fprintf(&b, "exit %d", exitCode)
	return writeFakeKernelScript(t, dir, name, b.String())
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// readArgsLog возвращает строки <dir>/<name>.args.log (пусто, если ядро не
// запускалось).
func readArgsLog(t *testing.T, dir, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name+".args.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// newTestRoots создаёт корни ядер: у Xray 01_log.json, у Mihomo config.yaml.
func newTestRoots(t *testing.T) Roots {
	t.Helper()
	roots := Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	writeTestFile(t, filepath.Join(roots.Xray, "01_log.json"), `{"log":{}}`)
	writeTestFile(t, filepath.Join(roots.Mihomo, "config.yaml"), "mixed-port: 7890\n")
	return roots
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pipeOpts — настройка тестового конвейера.
type pipeOpts struct {
	// Bins — пути бинарников; пустой путь — ядро не установлено.
	Bins Binaries
	// DevMode — включает диагностический генератор.
	DevMode bool
	// WriteFile подменяет запись (nil — настоящая).
	WriteFile func(path string, data []byte) error
	// Foreign — файлы старого слоя (nil — нет).
	Foreign func(kernel, rel string) bool
}

// testEnv — собранный тестовый конвейер с настоящими Store, Broker, Registry.
type testEnv struct {
	P       *Pipeline
	Store   *Store
	Broker  *Broker
	Roots   Roots
	DataDir string
	// Bins читается конвейером при каждом запуске: тест может менять.
	Bins Binaries
	// Events — подписка, заведённая до первого запуска.
	Events <-chan Event
}

// newTestPipeline собирает конвейер с реестром из диагностического генератора.
func newTestPipeline(t *testing.T, opts pipeOpts) *testEnv {
	t.Helper()
	env := &testEnv{
		Roots:   newTestRoots(t),
		DataDir: t.TempDir(),
		Bins:    opts.Bins,
		Broker:  NewBroker(),
	}
	t.Cleanup(env.Broker.Close)
	store, err := OpenStore(env.DataDir, env.Broker)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	env.Store = store
	events, cancel, err := env.Broker.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	env.Events = events

	reg := NewRegistry()
	reg.Register(NewDiagGenerator(func() bool { return opts.DevMode }))
	env.P = NewPipeline(PipelineDeps{
		Store:        store,
		Broker:       env.Broker,
		Registry:     reg,
		Roots:        env.Roots,
		DataDir:      env.DataDir,
		Binaries:     func() Binaries { return env.Bins },
		XrayEnv:      func(string) []string { return nil },
		ForeignOwned: opts.Foreign,
		WriteFile:    opts.WriteFile,
	})
	return env
}

// setDraft кладёт секцию в черновик на текущей ревизии.
func (e *testEnv) setDraft(t *testing.T, section, value string) {
	t.Helper()
	if _, err := e.Store.EditDraft(e.Store.DraftRevision(), section, []byte(value)); err != nil {
		t.Fatalf("EditDraft(%s): %v", section, err)
	}
}

// drainEvents забирает все накопленные события шины.
func (e *testEnv) drainEvents() []Event {
	var out []Event
	for {
		select {
		case ev, ok := <-e.Events:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

// requireNoApplyDirs проверяет, что в <data_dir>/tmp не осталось apply-*.
func requireNoApplyDirs(t *testing.T, dataDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, "tmp"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "apply-") {
			t.Errorf("временный каталог не удалён: %s", e.Name())
		}
	}
}
