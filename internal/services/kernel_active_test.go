package services

import (
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// fakeKernelProc подменяет procDir каталогом t.TempDir() и создаёт файлы-бинарники
// ядер; возвращает корень фейкового /proc и пути бинарников.
func fakeKernelProc(t *testing.T) (root, xrayBin, mihomoBin string) {
	t.Helper()
	root = t.TempDir()
	orig := procDir
	procDir = root
	t.Cleanup(func() { procDir = orig })

	bins := t.TempDir()
	xrayBin = filepath.Join(bins, "xray")
	mihomoBin = filepath.Join(bins, "mihomo")
	for _, p := range []string{xrayBin, mihomoBin} {
		if err := os.WriteFile(p, []byte("fake binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, xrayBin, mihomoBin
}

// addFakeProc создаёт запись процесса <root>/<pid> с cmdline и симлинком exe.
func addFakeProc(t *testing.T, root, pid, bin string, args ...string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdline := bin + "\x00"
	for _, a := range args {
		cmdline += a + "\x00"
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(dir, "exe")); err != nil {
		t.Fatal(err)
	}
}

func newActiveTestService(t *testing.T, xrayBin, mihomoBin string) *KernelService {
	t.Helper()
	svc := NewKernelService(t.TempDir())
	svc.kernels["xray"].BinaryPath = xrayBin
	svc.kernels["mihomo"].BinaryPath = mihomoBin
	return svc
}

func TestActiveState_BothRunningIsConflict(t *testing.T) {
	root, xrayBin, mihomoBin := fakeKernelProc(t)
	addFakeProc(t, root, "1000", xrayBin, "-config", "/opt/etc/xray.json")
	addFakeProc(t, root, "1001", mihomoBin, "-d", "/opt/etc/mihomo")
	svc := newActiveTestService(t, xrayBin, mihomoBin)

	st := svc.ActiveState()
	if !st.Conflict {
		t.Fatalf("Conflict = false, want true: %+v", st)
	}
	if st.Kernel != "" {
		t.Errorf("Kernel = %q, при конфликте ядро не выбирается", st.Kernel)
	}
	if want := []string{"xray", "mihomo"}; !reflect.DeepEqual(st.Running, want) {
		t.Errorf("Running = %v, want %v", st.Running, want)
	}
	if st.Label() != "both" {
		t.Errorf("Label() = %q, want both", st.Label())
	}
}

func TestActiveState_SingleRunning(t *testing.T) {
	root, xrayBin, mihomoBin := fakeKernelProc(t)
	addFakeProc(t, root, "1001", mihomoBin, "-d", "/opt/etc/mihomo")
	svc := newActiveTestService(t, xrayBin, mihomoBin)

	st := svc.ActiveState()
	if st.Conflict || st.Kernel != "mihomo" {
		t.Fatalf("got %+v, want kernel=mihomo без конфликта", st)
	}
	if want := []string{"mihomo"}; !reflect.DeepEqual(st.Running, want) {
		t.Errorf("Running = %v, want %v", st.Running, want)
	}
	if st.Label() != "mihomo" {
		t.Errorf("Label() = %q, want mihomo", st.Label())
	}
}

func runningStates(names ...string) []KernelProcessState {
	var out []KernelProcessState
	for _, n := range names {
		out = append(out, KernelProcessState{Name: n, Status: "running", PID: 1})
	}
	return out
}

func TestActiveState_IdleUsesFreshRawThenConfigured(t *testing.T) {
	stopped := []KernelProcessState{
		{Name: "xray", Status: "stopped"},
		{Name: "mihomo", Status: "stopped"},
	}
	cases := []struct {
		name       string
		raw        string
		fresh      bool
		configured string
		want       string
	}{
		{"fresh raw single kernel", "mihomo is running", true, "xray", "mihomo"},
		{"fresh raw both words falls back to configured", "xray and mihomo", true, "mihomo", "mihomo"},
		{"stale raw ignored", "xray is running", false, "mihomo", "mihomo"},
		{"nothing known", "", false, "", "none"},
		{"unknown configured value", "", false, "sing-box", "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := NewKernelService(t.TempDir())
			svc.SetProcessStatesSource(func() []KernelProcessState { return stopped })
			svc.SetActiveFallbacks(
				func() (string, bool) { return c.raw, c.fresh },
				func() string { return c.configured },
			)
			st := svc.ActiveState()
			if st.Conflict || st.Kernel != c.want {
				t.Fatalf("got %+v, want kernel=%q без конфликта", st, c.want)
			}
			if len(st.Running) != 0 {
				t.Errorf("Running = %v, want пусто", st.Running)
			}
		})
	}
}

// Конфликт определяется только по процессам: текст статуса с обоими словами
// при одном запущенном ядре его не создаёт.
func TestActiveState_RawWordsDoNotCreateConflict(t *testing.T) {
	svc := NewKernelService(t.TempDir())
	svc.SetProcessStatesSource(func() []KernelProcessState { return runningStates("xray") })
	svc.SetActiveFallbacks(func() (string, bool) { return "xray mihomo", true }, nil)
	if st := svc.ActiveState(); st.Conflict || st.Kernel != "xray" {
		t.Fatalf("got %+v, want kernel=xray без конфликта", st)
	}
}

func TestActiveState_NilReceiver(t *testing.T) {
	var svc *KernelService
	st := svc.ActiveState()
	if st.Kernel != "none" || st.Conflict || st.Label() != "none" {
		t.Fatalf("got %+v (label %q), want none", st, st.Label())
	}
	svc.InvalidateActiveState() // не должно паниковать
}

func withConflictMinAge(t *testing.T, d time.Duration) {
	t.Helper()
	orig := conflictMinAge
	conflictMinAge = d
	t.Cleanup(func() { conflictMinAge = orig })
}

func ageState(name string, age time.Duration, known bool) KernelProcessState {
	return KernelProcessState{Name: name, Status: "running", PID: 1, Age: age, AgeKnown: known}
}

func resolveWith(states ...KernelProcessState) ActiveKernelState {
	return ResolveActiveState(states, nil, nil)
}

// Новичок моложе conflictMinAge рядом с установившимся ядром: это переключение
// в ходе, конфликта нет, активным остаётся установившееся ядро.
func TestActiveState_YoungProcessPending(t *testing.T) {
	withConflictMinAge(t, 2*time.Second)

	st := resolveWith(ageState("xray", 60*time.Second, true), ageState("mihomo", 500*time.Millisecond, true))
	if st.Conflict || st.Kernel != "xray" {
		t.Fatalf("новичок mihomo: got %+v, want kernel=xray без конфликта", st)
	}
	if want := []string{"xray", "mihomo"}; !reflect.DeepEqual(st.Running, want) {
		t.Errorf("Running = %v, want %v", st.Running, want)
	}

	// Зеркальный случай: новичок xray, установившееся mihomo.
	st = resolveWith(ageState("xray", 500*time.Millisecond, true), ageState("mihomo", 60*time.Second, true))
	if st.Conflict || st.Kernel != "mihomo" {
		t.Fatalf("новичок xray: got %+v, want kernel=mihomo без конфликта", st)
	}
}

func TestActiveState_ConflictByAge(t *testing.T) {
	withConflictMinAge(t, 2*time.Second)
	cases := []struct {
		name   string
		states []KernelProcessState
	}{
		{"оба установившиеся", []KernelProcessState{ageState("xray", 10*time.Second, true), ageState("mihomo", 2*time.Second, true)}},
		{"оба молодые: не переключение", []KernelProcessState{ageState("xray", time.Second, true), ageState("mihomo", 500*time.Millisecond, true)}},
		{"возраст неизвестен у обоих", []KernelProcessState{ageState("xray", 0, false), ageState("mihomo", 0, false)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := resolveWith(c.states...)
			if !st.Conflict || st.Kernel != "" {
				t.Fatalf("got %+v, want конфликт без выбранного ядра", st)
			}
		})
	}
}

// Вспомогательный процесс ядра (разовая конвертация MRS) не считается запущенным
// ядром: рядом с работающим xray конфликта нет.
func TestActiveState_HelperProcessIgnored(t *testing.T) {
	root, xrayBin, mihomoBin := fakeKernelProc(t)
	addFakeProc(t, root, "1000", xrayBin, "-config", "/opt/etc/xray.json")
	addFakeProc(t, root, "1001", mihomoBin, "convert-ruleset", "domain", "mrs", "a.txt", "b.mrs")
	svc := newActiveTestService(t, xrayBin, mihomoBin)

	st := svc.ActiveState()
	if st.Conflict || st.Kernel != "xray" {
		t.Fatalf("got %+v, want kernel=xray без конфликта", st)
	}
	if want := []string{"xray"}; !reflect.DeepEqual(st.Running, want) {
		t.Errorf("Running = %v, want %v", st.Running, want)
	}
}

func TestActiveState_TTLCache(t *testing.T) {
	orig := activeStateTTL
	activeStateTTL = time.Hour
	t.Cleanup(func() { activeStateTTL = orig })

	var calls atomic.Int32
	svc := NewKernelService(t.TempDir())
	svc.SetProcessStatesSource(func() []KernelProcessState {
		calls.Add(1)
		return runningStates("xray")
	})

	first := svc.ActiveState()
	second := svc.ActiveState()
	if calls.Load() != 1 {
		t.Fatalf("источник вызван %d раз за два вызова в пределах TTL, want 1", calls.Load())
	}
	if first.Kernel != "xray" || second.Kernel != "xray" {
		t.Fatalf("got %+v / %+v, want xray", first, second)
	}

	// Срез Running из кэша — копия: правка снаружи кэш не портит.
	second.Running[0] = "mihomo"
	if third := svc.ActiveState(); third.Running[0] != "xray" {
		t.Errorf("кэш изменился через возвращённый срез: %v", third.Running)
	}

	svc.InvalidateActiveState()
	svc.ActiveState()
	if calls.Load() != 2 {
		t.Fatalf("после InvalidateActiveState источник вызван %d раз, want 2", calls.Load())
	}

	// SetProcessStatesSource тоже сбрасывает кэш.
	svc.SetProcessStatesSource(func() []KernelProcessState {
		calls.Add(1)
		return runningStates("mihomo")
	})
	if st := svc.ActiveState(); st.Kernel != "mihomo" {
		t.Fatalf("после смены источника got %+v, want mihomo", st)
	}
}
