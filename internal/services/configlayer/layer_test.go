package configlayer

import (
	"os"
	"path/filepath"
	"strings"
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

// --- задача 2: дрейф, «Применить», «Пересобрать», «Принять правку» ---

// drain забирает накопленные события без ожидания.
func drain(ch <-chan Event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// settleApply ждёт конец запуска конвейера целиком (apply_done, затем draft из
// afterRun), берёт синхронную сверку и очищает очередь событий: дальше тест видит
// только новые события.
func (e *layerEnv) settleApply(t *testing.T, events <-chan Event) ApplyView {
	t.Helper()
	done := waitEvent(t, events, EventApplyDone, 10*time.Second, nil)
	waitEvent(t, events, EventDraft, 5*time.Second, nil)
	e.L.checkNow(true)
	drain(events)
	view, ok := done.Data.(ApplyView)
	if !ok {
		t.Fatalf("apply_done: данные %T", done.Data)
	}
	return view
}

// applyDraft запускает «Применить» и дожидается конца запуска.
func (e *layerEnv) applyDraft(t *testing.T, events <-chan Event) ApplyView {
	t.Helper()
	if err := e.L.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	return e.settleApply(t, events)
}

// newApplyLayer — слой с запущенным xray, фейковым xray-бинарником и подпиской;
// диагностика уже применена: файл xray:04_outbounds.xcp-diag.tail.json в манифесте.
func newApplyLayer(t *testing.T, lo layerOpts) (*layerEnv, <-chan Event) {
	t.Helper()
	fastRestartTimings(t)
	lo.Enabled, lo.DevMode = true, true
	lo.XrayStatus = "running"
	if lo.Bins.Xray == "" {
		lo.Bins.Xray = writeFakeKernel(t, t.TempDir(), "xray", 0, "", 0)
	}
	env := newTestLayer(t, lo)
	events, cancel, err := env.L.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	if _, err := env.L.Diag(0, "add"); err != nil {
		t.Fatalf("Diag: %v", err)
	}
	if view := env.applyDraft(t, events); view.Result == nil || view.Result.Code != ResultApplied {
		t.Fatalf("первое применение: %+v", view.Result)
	}
	return env, events
}

func (e *layerEnv) diagXrayPath() string { return filepath.Join(e.Roots.Xray, DiagXrayRel) }

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitIdle ждёт, пока конвейер отпустит замки (горутина запуска завершилась).
func waitIdle(t *testing.T, l *Layer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rel, err := l.pipeline.TryBegin(t.Context(), true); err == nil {
			rel()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("замок применения не освободился за 5 с")
}

func filesEventWith(pred func(FilesEvent) bool) func(Event) bool {
	return func(ev Event) bool {
		fe, ok := ev.Data.(FilesEvent)
		return ok && pred(fe)
	}
}

const manualEdit = "{\"manual\":true}\n"

func TestLayer_DriftLoopPublishesOnChange(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{DebounceDelay: 50 * time.Millisecond, DriftInterval: time.Hour})

	mustWriteFile(t, env.diagXrayPath(), manualEdit)
	env.L.RequestCheck()
	ev := waitEvent(t, events, EventFiles, time.Second, nil)
	fe := ev.Data.(FilesEvent)
	fv, ok := findFileView(fe.Files, diagXrayKey)
	if fe.DriftCount != 1 || !ok || fv.State != StateDriftModified {
		t.Fatalf("FilesEvent = %+v, want drift_count 1 и drift_modified", fe)
	}

	env.L.RequestCheck()
	requireNoEvent(t, events, EventFiles, 300*time.Millisecond)
}

func TestLayer_DriftLoopTicker(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{DebounceDelay: 50 * time.Millisecond, DriftInterval: 100 * time.Millisecond})

	if err := os.Remove(env.diagXrayPath()); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, events, EventFiles, 3*time.Second, filesEventWith(func(fe FilesEvent) bool { return fe.DriftCount == 1 }))
	fv, _ := findFileView(ev.Data.(FilesEvent).Files, diagXrayKey)
	if fv.State != StateDriftMissing {
		t.Errorf("state = %q, want drift_missing", fv.State)
	}
}

func TestLayer_ApplyBlockedByDrift(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})
	manifestBefore := env.L.store.Snapshot().Manifest[diagXrayKey]
	mustWriteFile(t, env.diagXrayPath(), manualEdit)
	if _, err := env.L.Diag(env.L.store.DraftRevision(), "add_broken_xray"); err != nil {
		t.Fatal(err)
	}
	restartsBefore := env.restarts.Load()

	if err := env.L.StartApply(true); err != ErrDriftBlocked {
		t.Fatalf("StartApply = %v, want ErrDriftBlocked", err)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != manualEdit {
		t.Errorf("файл тронут: %q", got)
	}
	if got := env.L.store.Snapshot().Manifest[diagXrayKey]; got != manifestBefore {
		t.Errorf("манифест изменился: %+v → %+v", manifestBefore, got)
	}
	if env.restarts.Load() != restartsBefore {
		t.Error("ядро перезапущено при заблокированном применении")
	}
	if cur := env.L.Snapshot().Apply; cur.Running {
		t.Errorf("запуск начался: %+v", cur)
	}
}

func TestLayer_ReleaseNotOverwritten(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	mustWriteFile(t, env.diagXrayPath(), manualEdit)

	fe, err := env.L.Release(diagXrayKey)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	fv, ok := findFileView(fe.Files, diagXrayKey)
	if !ok || fv.State != StateReleased || fv.Owner != "manual" || fe.DriftCount != 0 {
		t.Fatalf("после Release: %+v, want released/manual без дрейфа", fe)
	}
	if got := env.L.store.Snapshot().Manifest[diagXrayKey].Status; got != StatusReleased {
		t.Errorf("статус манифеста = %q, want released", got)
	}

	if _, err := env.L.Diag(env.L.store.DraftRevision(), "add_broken_xray"); err != nil {
		t.Fatal(err)
	}
	view := env.applyDraft(t, events)
	if view.Result == nil || !view.Result.OK {
		t.Fatalf("применение после Release: %+v", view.Result)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != manualEdit {
		t.Errorf("отпущенный файл перезаписан: %q", got)
	}
	for _, name := range view.Result.OrphansRemoved {
		if name == filepath.Base(DiagXrayRel) {
			t.Errorf("отпущенный файл уехал в сироты: %v", view.Result.OrphansRemoved)
		}
	}
	if got := env.L.store.Snapshot().Manifest[diagXrayKey].Status; got != StatusReleased {
		t.Errorf("статус после применения = %q, want released", got)
	}
}

func TestRebuild_ReleasedBackToManaged(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	mustWriteFile(t, env.diagXrayPath(), manualEdit)
	if _, err := env.L.Release(diagXrayKey); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, env.L)

	if err := env.L.Rebuild([]string{diagXrayKey}, false); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	view := env.settleApply(t, events)
	if view.Result == nil || !view.Result.OK || view.Result.Written != 1 {
		t.Fatalf("Result = %+v, want applied, Written 1", view.Result)
	}
	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл = %q, want содержимое из applied", got)
	}
	if got := env.L.store.Snapshot().Manifest[diagXrayKey].Status; got != StatusManaged {
		t.Errorf("статус = %q, want managed", got)
	}
	fv, _ := findFileView(env.L.Snapshot().Files, diagXrayKey)
	if fv.State != StateOK {
		t.Errorf("state = %q, want ok", fv.State)
	}
}

func TestLayer_RebuildUsesAppliedNotDraft(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	if _, err := env.L.Diag(env.L.store.DraftRevision(), "add_broken_xray"); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, env.diagXrayPath(), manualEdit)

	if err := env.L.Rebuild([]string{diagXrayKey}, false); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	env.settleApply(t, events)

	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл = %q, want содержимое из applied, а не черновика", got)
	}
	if snap := env.L.Snapshot(); snap.DraftChanges != 1 {
		t.Errorf("draft_changes = %d, want 1: черновик не применялся", snap.DraftChanges)
	}
}

func TestLayer_RebuildRenamed(t *testing.T) {
	env, events := newApplyLayer(t, layerOpts{})
	if err := os.Rename(env.diagXrayPath(), env.diagXrayPath()+".obsolete"); err != nil {
		t.Fatal(err)
	}
	if fv, _ := findFileView(env.L.Snapshot().Files, diagXrayKey); fv.State != StateDriftRenamed {
		t.Fatalf("state = %q, want drift_renamed", fv.State)
	}

	if err := env.L.Rebuild([]string{diagXrayKey}, false); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	env.settleApply(t, events)

	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл = %q, want {}", got)
	}
	if _, err := os.Stat(env.diagXrayPath() + ".obsolete"); !os.IsNotExist(err) {
		t.Errorf(".obsolete не удалён: %v", err)
	}
}

func TestLayer_RebuildAllOnlyDrift(t *testing.T) {
	gen := newXrayGen("{\"a\":1}\n")
	env, events := newApplyLayer(t, layerOpts{Generators: []Generator{gen}})
	otherPath := filepath.Join(env.Roots.Xray, gen.rel)
	if got := mustReadFile(t, otherPath); got != "{\"a\":1}\n" {
		t.Fatalf("второй файл = %q", got)
	}
	// Генерация второго файла изменилась, но дрейфа у него нет: «Пересобрать всё» его не трогает.
	gen.set("{\"a\":2}\n")
	mustWriteFile(t, env.diagXrayPath(), manualEdit)

	if err := env.L.Rebuild(nil, true); err != nil {
		t.Fatalf("Rebuild(all): %v", err)
	}
	env.settleApply(t, events)

	if got := mustReadFile(t, env.diagXrayPath()); got != "{}\n" {
		t.Errorf("файл с дрейфом = %q, want {}", got)
	}
	if got := mustReadFile(t, otherPath); got != "{\"a\":1}\n" {
		t.Errorf("файл без дрейфа переписан: %q", got)
	}

	waitIdle(t, env.L)
	if err := env.L.Rebuild([]string{"xray:xcp-nope.json"}, false); err != ErrUnknownKey {
		t.Errorf("Rebuild(неизвестный) = %v, want ErrUnknownKey", err)
	}
	if err := env.L.Rebuild(nil, false); err != ErrUnknownKey {
		t.Errorf("Rebuild(пусто) = %v, want ErrUnknownKey", err)
	}
	if err := env.L.Rebuild(nil, true); err != ErrUnknownKey {
		t.Errorf("Rebuild(all без дрейфа) = %v, want ErrUnknownKey", err)
	}
}

func TestLayer_DiffExpectedActual(t *testing.T) {
	env, _ := newApplyLayer(t, layerOpts{})

	mustWriteFile(t, env.diagXrayPath(), manualEdit)
	d, err := env.L.Diff(diagXrayKey)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if d.Key != diagXrayKey || d.Expected != "{}\n" || d.Actual != manualEdit || d.Missing || d.Truncated {
		t.Errorf("Diff = %+v, want expected {} / actual manual", d)
	}

	big := strings.Repeat("a", 300*1024)
	mustWriteFile(t, env.diagXrayPath(), big)
	d, err = env.L.Diff(diagXrayKey)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || len(d.Actual) == 0 || len(d.Actual) > 256*1024 {
		t.Errorf("Actual: len %d, truncated %v; want 0 < len ≤ 256 КБ и truncated", len(d.Actual), d.Truncated)
	}

	if err := os.Rename(env.diagXrayPath(), env.diagXrayPath()+".obsolete"); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, env.diagXrayPath()+".obsolete", manualEdit)
	if d, err = env.L.Diff(diagXrayKey); err != nil || d.Actual != manualEdit || d.Missing {
		t.Errorf("Diff переименованного = %+v, %v; want actual из .obsolete", d, err)
	}

	if err := os.Remove(env.diagXrayPath() + ".obsolete"); err != nil {
		t.Fatal(err)
	}
	d, err = env.L.Diff(diagXrayKey)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Missing || d.Actual != "" || d.Expected != "{}\n" {
		t.Errorf("Diff пропавшего = %+v, want missing, expected {}", d)
	}

	if _, err := env.L.Diff("xray:xcp-nope.json"); err != ErrUnknownKey {
		t.Errorf("Diff(неизвестный) = %v, want ErrUnknownKey", err)
	}
	if _, err := env.L.Release(diagXrayKey); err != nil {
		t.Fatal(err)
	}
	if _, err := env.L.Diff(diagXrayKey); err != ErrFileReleased {
		t.Errorf("Diff(released) = %v, want ErrFileReleased", err)
	}
}
