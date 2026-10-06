package configlayer

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// layerOpts — настройка тестового слоя.
type layerOpts struct {
	Enabled bool
	DevMode bool
	Bins    Binaries
	// XrayStatus, MihomoStatus — статусы процессов ядер ("" — not_installed).
	XrayStatus, MihomoStatus string
	Generators               []Generator
	DriftInterval            time.Duration
	DebounceDelay            time.Duration
	// NoStart — не вызывать Start (тест сам решает, когда запускать слой).
	NoStart bool
	// Restart подменяет рестарт ядра (nil — все запущенные ядра получают новый PID).
	Restart func() (string, error)
}

// layerEnv — слой на фейковых ядрах и временных каталогах.
type layerEnv struct {
	L       *Layer
	Roots   Roots
	DataDir string
	Procs   *fakeProcs
	Applier *procApplier
	Mihomo  *fakeMihomo

	enabled  atomic.Bool
	devMode  atomic.Bool
	restarts atomic.Int32

	binsMu sync.Mutex
	bins   Binaries
	// Lifecycle — общий замок жизненного цикла ядер.
	Lifecycle *sync.Mutex
}

func (e *layerEnv) setBins(b Binaries) {
	e.binsMu.Lock()
	e.bins = b
	e.binsMu.Unlock()
}

func (e *layerEnv) getBins() Binaries {
	e.binsMu.Lock()
	defer e.binsMu.Unlock()
	return e.bins
}

// newTestLayer собирает слой с настоящими Store, Broker, Registry, Pipeline и
// фейковыми ядрами. Stop вызывается в t.Cleanup.
func newTestLayer(t *testing.T, lo layerOpts) *layerEnv {
	t.Helper()
	env := &layerEnv{
		Roots:     newTestRoots(t),
		DataDir:   t.TempDir(),
		Procs:     newFakeProcs(),
		Mihomo:    &fakeMihomo{},
		Lifecycle: &sync.Mutex{},
		bins:      lo.Bins,
	}
	env.enabled.Store(lo.Enabled)
	env.devMode.Store(lo.DevMode)
	if lo.XrayStatus != "" {
		env.Procs.set("xray", lo.XrayStatus, 100)
	}
	if lo.MihomoStatus != "" {
		env.Procs.set("mihomo", lo.MihomoStatus, 200)
	}
	restart := lo.Restart
	if restart == nil {
		restart = func() (string, error) {
			for _, st := range env.Procs.states() {
				if st.Status == "running" {
					env.Procs.set(st.Name, "running", st.PID+1)
				}
			}
			return "", nil
		}
	}
	env.Applier = newProcApplier(env.Procs, func() (string, error) {
		env.restarts.Add(1)
		return restart()
	})

	opts := Options{
		DataDir:       env.DataDir,
		Roots:         env.Roots,
		Enabled:       env.enabled.Load,
		DevMode:       env.devMode.Load,
		Binaries:      env.getBins,
		XrayEnv:       func(string) []string { return nil },
		Applier:       env.Applier,
		ProcessStates: env.Procs.states,
		Mihomo:        env.Mihomo,
		MihomoAPIReady: func() bool {
			return true
		},
		Lifecycle:     env.Lifecycle,
		DriftInterval: lo.DriftInterval,
		DebounceDelay: lo.DebounceDelay,
		KernelVersions: func() []KernelVersionInput {
			b := env.getBins()
			return []KernelVersionInput{
				{Name: "xray", Installed: b.Xray != "", Version: "1.8.24"},
				{Name: "mihomo", Installed: b.Mihomo != "", Version: "1.19.2"},
			}
		},
	}
	l, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, g := range lo.Generators {
		l.registry.Register(g)
	}
	env.L = l
	t.Cleanup(l.Stop)
	if !lo.NoStart {
		l.Start()
	}
	return env
}

// waitEvent ждёт событие нужного типа, для которого pred (может быть nil) вернул true.
func waitEvent(t *testing.T, ch <-chan Event, typ string, timeout time.Duration, pred func(Event) bool) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("канал событий закрыт, ждали %s", typ)
			}
			if ev.Type == typ && (pred == nil || pred(ev)) {
				return ev
			}
		case <-deadline:
			t.Fatalf("событие %s не получено за %v", typ, timeout)
		}
	}
}

// requireNoEvent проверяет, что за d событий нужного типа (или любого, если typ пуст) не пришло.
func requireNoEvent(t *testing.T, ch <-chan Event, typ string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if typ == "" || ev.Type == typ {
				t.Fatalf("неожиданное событие %s: %+v", ev.Type, ev.Data)
			}
		case <-deadline:
			return
		}
	}
}

func findFileView(files []FileView, key string) (FileView, bool) {
	for _, f := range files {
		if f.Key == key {
			return f, true
		}
	}
	return FileView{}, false
}

// waitStop проверяет, что Stop возвращается за разумное время.
func waitStop(t *testing.T, l *Layer) {
	t.Helper()
	done := make(chan struct{})
	go func() { l.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop не завершился за 5 с")
	}
}

var diagXrayKey = ManifestKey(KernelXray, DiagXrayRel)

func TestLayer_Tracer(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	env := newTestLayer(t, layerOpts{Enabled: true, DevMode: true, Bins: Binaries{Xray: xray}, XrayStatus: "running"})
	events, cancel, err := env.L.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	ev, err := env.L.Diag(0, "add")
	if err != nil {
		t.Fatalf("Diag: %v", err)
	}
	if ev != (DraftEvent{DraftRevision: 1, DraftChanges: 1}) {
		t.Errorf("DraftEvent = %+v, want {1 1}", ev)
	}

	snap := env.L.Snapshot()
	if !snap.Enabled || !snap.DevMode || snap.DraftRevision != 1 || snap.DraftChanges != 1 {
		t.Errorf("Snapshot = %+v, want enabled, dev_mode, revision 1, changes 1", snap)
	}
	fv, ok := findFileView(snap.Files, diagXrayKey)
	if !ok {
		t.Fatalf("в снимке нет %s: %+v", diagXrayKey, snap.Files)
	}
	if fv.State != StatePending || fv.Owner != "panel" || fv.Kernel != KernelXray {
		t.Errorf("FileView = %+v, want pending, owner panel, kernel xray", fv)
	}
	if want := filepath.Join(env.Roots.Xray, DiagXrayRel); fv.Path != want {
		t.Errorf("Path = %q, want %q", fv.Path, want)
	}
	if _, ok := findFileView(snap.Files, ManifestKey(KernelMihomo, DiagMihomoRel)); ok {
		t.Error("файл неустановленного Mihomo попал в снимок")
	}
	if len(snap.Kernels) != 3 {
		t.Errorf("Kernels = %+v, want три строки", snap.Kernels)
	}

	if err := env.L.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	done := waitEvent(t, events, EventApplyDone, 10*time.Second, nil)
	view, ok := done.Data.(ApplyView)
	if !ok || view.Result == nil || view.Result.Code != ResultApplied {
		t.Fatalf("apply_done = %+v, want applied", done.Data)
	}

	snap = env.L.Snapshot()
	fv, ok = findFileView(snap.Files, diagXrayKey)
	if !ok || fv.State != StateOK {
		t.Errorf("после применения FileView = %+v (found %v), want ok", fv, ok)
	}
	if snap.DraftChanges != 0 || snap.Apply.Running || snap.DriftCount != 0 {
		t.Errorf("Snapshot = %+v, want draft_changes 0, не идёт, drift 0", snap)
	}
	if env.restarts.Load() != 1 {
		t.Errorf("рестартов xray = %d, want 1", env.restarts.Load())
	}
	waitStop(t, env.L)
}

func TestLayer_DisabledNoop(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	env := newTestLayer(t, layerOpts{
		Enabled: false, DevMode: true, Bins: Binaries{Xray: xray}, XrayStatus: "running",
		NoStart: true, DriftInterval: 20 * time.Millisecond, DebounceDelay: 10 * time.Millisecond,
	})
	events, cancel, err := env.L.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	env.L.Start()
	env.L.RequestCheck()
	requireNoEvent(t, events, "", 200*time.Millisecond)

	for _, root := range []string{env.Roots.Xray, env.Roots.Mihomo} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("в %s появились файлы: %v", root, entries)
		}
	}
	if err := env.L.StartApply(true); err != ErrDisabled {
		t.Errorf("StartApply = %v, want ErrDisabled", err)
	}
	if _, err := env.L.Release(diagXrayKey); err != ErrDisabled {
		t.Errorf("Release = %v, want ErrDisabled", err)
	}
	if err := env.L.Rebuild(nil, true); err != ErrDisabled {
		t.Errorf("Rebuild = %v, want ErrDisabled", err)
	}
	if _, err := env.L.Diag(0, "add"); err != ErrDisabled {
		t.Errorf("Diag = %v, want ErrDisabled", err)
	}
	if snap := env.L.Snapshot(); snap.Enabled || len(snap.Files) != 0 {
		t.Errorf("Snapshot = %+v, want disabled без файлов", snap)
	}
	if env.restarts.Load() != 0 {
		t.Errorf("ядро перезапускалось %d раз при выключенном слое", env.restarts.Load())
	}
	waitStop(t, env.L)
}

func TestLayer_DiagRequiresDevMode(t *testing.T) {
	env := newTestLayer(t, layerOpts{Enabled: true, DevMode: false})
	if _, err := env.L.Diag(0, "add"); err != ErrDevModeRequired {
		t.Errorf("Diag вне dev_mode = %v, want ErrDevModeRequired", err)
	}
	if env.L.Snapshot().DraftRevision != 0 {
		t.Error("черновик изменён вне dev_mode")
	}
	env.devMode.Store(true)
	if _, err := env.L.Diag(5, "add"); err == nil {
		t.Error("Diag на устаревшей ревизии принят")
	}
	if _, err := env.L.Diag(0, "bogus"); err == nil {
		t.Error("неизвестное действие диагностики принято")
	}
}
