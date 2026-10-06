package configlayer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func stepOf(t *testing.T, v ApplyView, id StepID) StepView {
	t.Helper()
	for _, s := range v.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("шаг %q не найден в %+v", id, v.Steps)
	return StepView{}
}

func TestPipeline_TracerApplyDiag(t *testing.T) {
	binDir := t.TempDir()
	// Фейковый xray на время проверки снимает содержимое каталога проверки.
	dump := filepath.Join(binDir, "xray.dump")
	xray := writeFakeKernelScript(t, binDir, "xray", strings.Join([]string{
		`echo "$@" >> ` + shellQuote(filepath.Join(binDir, "xray.args.log")),
		`for f in "$4"/*.json; do echo "$(basename "$f")" >> ` + shellQuote(dump) + `; done`,
		`exit 0`,
	}, "\n"))
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true})
	env.setDraft(t, DiagSection, `{"enabled":true}`)
	startSnap := env.Store.Snapshot()

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Running {
		t.Error("Running = true после Run")
	}
	if view.Result == nil || view.Result.Code != ResultApplied || !view.Result.OK {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if view.Result.Written != 1 {
		t.Errorf("Written = %d, want 1", view.Result.Written)
	}

	diagPath := filepath.Join(env.Roots.Xray, DiagXrayRel)
	data, err := os.ReadFile(diagPath)
	if err != nil {
		t.Fatalf("файл диагностики не записан: %v", err)
	}
	if string(data) != "{}\n" {
		t.Errorf("содержимое = %q, want %q", data, "{}\n")
	}
	if _, err := os.Stat(filepath.Join(env.Roots.Mihomo, DiagMihomoRel)); err == nil {
		t.Error("файл Mihomo записан, хотя ядро не установлено")
	}

	args := readArgsLog(t, binDir, "xray")
	if len(args) != 1 {
		t.Fatalf("xray запущен %d раз, want 1: %v", len(args), args)
	}
	wantPrefix := "run -test -confdir " + filepath.Join(env.DataDir, "tmp", "apply-")
	if !strings.HasPrefix(args[0], wantPrefix) {
		t.Errorf("аргументы = %q, want префикс %q", args[0], wantPrefix)
	}
	dumped, _ := os.ReadFile(dump)
	if !strings.Contains(string(dumped), DiagXrayRel) || !strings.Contains(string(dumped), "01_log.json") {
		t.Errorf("в каталоге проверки были файлы %q, want 01_log.json и %s", dumped, DiagXrayRel)
	}

	st := env.Store.Snapshot()
	entry, ok := st.Manifest[ManifestKey(KernelXray, DiagXrayRel)]
	if !ok {
		t.Fatalf("нет записи манифеста: %+v", st.Manifest)
	}
	if entry.Hash != HashContent([]byte("{}\n")) || entry.Status != StatusManaged || entry.Kind != KindXrayJSON {
		t.Errorf("запись манифеста = %+v", entry)
	}
	if entry.WrittenAt.IsZero() {
		t.Error("written_at не заполнен")
	}
	if !SectionEqual(st.Applied[DiagSection], startSnap.Draft[DiagSection]) {
		t.Errorf("Applied = %s, want черновик на момент старта %s", st.Applied[DiagSection], startSnap.Draft[DiagSection])
	}

	if s := stepOf(t, view, StepValidateXray); s.State != StepDone {
		t.Errorf("validate_xray = %+v, want done", s)
	}
	if s := stepOf(t, view, StepValidateMihomo); s.State != StepSkipped || s.NoteCode != "kernel_not_installed" {
		t.Errorf("validate_mihomo = %+v, want skipped kernel_not_installed", s)
	}
	if s := stepOf(t, view, StepWrite); s.State != StepDone {
		t.Errorf("write = %+v, want done", s)
	}

	var steps, done int
	for _, ev := range env.drainEvents() {
		switch ev.Type {
		case EventApplyStep:
			steps++
		case EventApplyDone:
			done++
		}
	}
	if steps == 0 || done != 1 {
		t.Errorf("событий apply_step=%d apply_done=%d, want >0 и 1", steps, done)
	}

	requireNoApplyDirs(t, env.DataDir)
	if cur := env.P.Current(); cur.Result == nil || cur.Result.Code != ResultApplied {
		t.Errorf("Current().Result = %+v", cur.Result)
	}
}

func writeDiagBoth(t *testing.T, env *testEnv) {
	t.Helper()
	env.setDraft(t, DiagSection, `{"enabled":true}`)
}

func requireNothingWritten(t *testing.T, env *testEnv) {
	t.Helper()
	for _, p := range []string{
		filepath.Join(env.Roots.Xray, DiagXrayRel),
		filepath.Join(env.Roots.Mihomo, DiagMihomoRel),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("файл записан: %s", p)
		}
	}
	st := env.Store.Snapshot()
	if len(st.Manifest) != 0 {
		t.Errorf("манифест не пуст: %+v", st.Manifest)
	}
	if len(st.Applied) != 0 {
		t.Errorf("Applied не пуст: %v", st.Applied)
	}
	if len(st.Draft) == 0 {
		t.Error("черновик тронут")
	}
	requireNoApplyDirs(t, env.DataDir)
}

func TestApply_BothValidatedBeforeWrite(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	mihomo := writeFakeKernel(t, binDir, "mihomo", 1, "yaml: line 2: did not find expected key", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray, Mihomo: mihomo}, DevMode: true})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != "validation_failed" || r.Kernel != KernelMihomo || r.HintCode != "mihomo_yaml_syntax" {
		t.Fatalf("Result = %+v, want validation_failed mihomo mihomo_yaml_syntax", r)
	}
	if got := len(readArgsLog(t, binDir, "xray")); got != 1 {
		t.Errorf("xray запущен %d раз, want 1 (обе проверки до записи)", got)
	}
	requireNothingWritten(t, env)
	if s := stepOf(t, view, StepValidateXray); s.State != StepDone {
		t.Errorf("validate_xray = %+v, want done", s)
	}
	if s := stepOf(t, view, StepValidateMihomo); s.State != StepFailed {
		t.Errorf("validate_mihomo = %+v, want failed", s)
	}
	if s := stepOf(t, view, StepWrite); s.State != StepPending {
		t.Errorf("write = %+v, want pending", s)
	}
}

func TestApply_ValidationFailsNothingWritten(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernelScript(t, binDir, "xray", strings.Join([]string{
		`echo "Failed to start: main: failed to load config files: [$4/01_log.json $4/04_outbounds.xcp-diag.tail.json] > infra/conf: unknown config id: nope"`,
		`exit 23`,
	}, "\n"))
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != "validation_failed" || r.Kernel != KernelXray {
		t.Fatalf("Result = %+v, want validation_failed xray", r)
	}
	if r.HintCode != "xray_unknown_protocol" {
		t.Errorf("HintCode = %q, want xray_unknown_protocol", r.HintCode)
	}
	if !strings.Contains(r.Message, env.Roots.Xray) {
		t.Errorf("Message = %q, want путь рабочего корня %s", r.Message, env.Roots.Xray)
	}
	if strings.Contains(r.Message, filepath.Join(env.DataDir, "tmp")) {
		t.Errorf("Message выдаёт путь временного каталога: %q", r.Message)
	}
	requireNothingWritten(t, env)
}

// setValidateTimeout понижает таймаут проверки на время теста.
func setValidateTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := ValidateTimeout
	ValidateTimeout = d
	t.Cleanup(func() { ValidateTimeout = old })
}

func TestApply_ValidationTimeout(t *testing.T) {
	setValidateTimeout(t, 300*time.Millisecond)
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 5*time.Second)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true})
	writeDiagBoth(t, env)

	start := time.Now()
	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Run занял %v, want < 4s (WaitDelay не должен давать зависнуть на внуке)", elapsed)
	}

	r := view.Result
	if r == nil || r.OK || r.Code != "validation_timeout" || r.Kernel != KernelXray {
		t.Fatalf("Result = %+v, want validation_timeout xray", r)
	}
	if r.Code == "validation_failed" {
		t.Error("таймаут выдан как «конфиг неверен»")
	}
	requireNothingWritten(t, env)
}

func TestApply_ValidationNotRun(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nodir", "xray")
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: missing}, DevMode: true})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != "validation_not_run" || r.Kernel != KernelXray {
		t.Fatalf("Result = %+v, want validation_not_run xray", r)
	}
	requireNothingWritten(t, env)
}

func TestApply_KernelNotInstalledSkipped(t *testing.T) {
	binDir := t.TempDir()
	mihomo := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Mihomo: mihomo}, DevMode: true})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || view.Result.Code != ResultApplied || view.Result.Written != 1 {
		t.Fatalf("Result = %+v, want applied, Written 1", view.Result)
	}
	if s := stepOf(t, view, StepValidateXray); s.State != StepSkipped || s.NoteCode != "kernel_not_installed" {
		t.Errorf("validate_xray = %+v, want skipped kernel_not_installed", s)
	}
	if _, err := os.Stat(filepath.Join(env.Roots.Xray, DiagXrayRel)); err == nil {
		t.Error("файл Xray записан, хотя ядро не установлено")
	}
	if _, err := os.Stat(filepath.Join(env.Roots.Mihomo, DiagMihomoRel)); err != nil {
		t.Errorf("файл Mihomo не записан: %v", err)
	}
}

// Записи манифеста неустановленного ядра не уходят в удаление: файлы такого
// ядра остаются нетронутыми, пока ядро не вернётся.
func TestApply_UninstalledKernelManifestUntouched(t *testing.T) {
	binDir := t.TempDir()
	mihomo := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Mihomo: mihomo}})

	xrayRel := "04_outbounds.xcp-keep.tail.json"
	xrayAbs := filepath.Join(env.Roots.Xray, xrayRel)
	writeTestFile(t, xrayAbs, "{}\n")
	key := ManifestKey(KernelXray, xrayRel)
	entry := ManifestEntry{Kernel: KernelXray, RelPath: xrayRel, Kind: KindXrayJSON,
		Hash: HashContent([]byte("{}\n")), Status: StatusManaged}
	if err := env.Store.Update(func(st *State) error {
		st.Manifest[key] = entry
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultNothingToApply {
		t.Fatalf("Result = %+v, want nothing_to_apply", view.Result)
	}
	if got := mustRead(t, xrayAbs); got != "{}\n" {
		t.Errorf("файл неустановленного ядра изменён: %q", got)
	}
	if got, ok := env.Store.Snapshot().Manifest[key]; !ok || got.Hash != entry.Hash {
		t.Errorf("запись манифеста потеряна или изменена: %+v", got)
	}
	if got := len(readArgsLog(t, binDir, "mihomo")); got != 0 {
		t.Errorf("mihomo запущен %d раз без изменений", got)
	}
}

func TestApply_BuildFailed(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	mihomo := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray, Mihomo: mihomo}, DevMode: true})
	env.setDraft(t, DiagSection, `{"enabled":true,"broken_mihomo":true}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultBuildFailed {
		t.Fatalf("Result = %+v, want build_failed", r)
	}
	if len(r.Issues) == 0 || r.Issues[0].Code != "provider_yaml_invalid" {
		t.Errorf("Issues = %+v, want provider_yaml_invalid первой", r.Issues)
	}
	if s := stepOf(t, view, StepBuild); s.State != StepFailed {
		t.Errorf("build = %+v, want failed", s)
	}
	if s := stepOf(t, view, StepWrite); s.State != StepPending {
		t.Errorf("write = %+v, want pending", s)
	}
	if got := len(readArgsLog(t, binDir, "xray")); got != 0 {
		t.Errorf("xray запущен %d раз при ошибке сборки", got)
	}
	requireNothingWritten(t, env)
}

func TestApply_NothingToApply(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	mihomo := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray, Mihomo: mihomo}, DevMode: true})
	writeDiagBoth(t, env)

	first := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})
	if first.Result == nil || first.Result.Code != ResultApplied || first.Result.Written != 2 {
		t.Fatalf("первый запуск = %+v, want applied, Written 2", first.Result)
	}
	paths := []string{
		filepath.Join(env.Roots.Xray, DiagXrayRel),
		filepath.Join(env.Roots.Mihomo, DiagMihomoRel),
	}
	before := make([]os.FileInfo, len(paths))
	for i, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = st
	}
	runsXray, runsMihomo := len(readArgsLog(t, binDir, "xray")), len(readArgsLog(t, binDir, "mihomo"))

	// Секция без файлов: Applied должен догнать черновик и без записи.
	env.setDraft(t, "note", `{"a":1}`)
	second := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := second.Result
	if r == nil || !r.OK || r.Code != ResultNothingToApply || r.Written != 0 {
		t.Fatalf("второй запуск = %+v, want nothing_to_apply", r)
	}
	for i, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before[i], st) || !st.ModTime().Equal(before[i].ModTime()) {
			t.Errorf("%s перезаписан", p)
		}
	}
	for _, id := range []StepID{StepValidateXray, StepValidateMihomo, StepWrite} {
		if s := stepOf(t, second, id); s.State != StepSkipped || s.NoteCode != "no_changes" {
			t.Errorf("%s = %+v, want skipped no_changes", id, s)
		}
	}
	if got := len(readArgsLog(t, binDir, "xray")); got != runsXray {
		t.Errorf("xray запущен повторно: %d -> %d", runsXray, got)
	}
	if got := len(readArgsLog(t, binDir, "mihomo")); got != runsMihomo {
		t.Errorf("mihomo запущен повторно: %d -> %d", runsMihomo, got)
	}
	if _, ok := env.Store.Snapshot().Applied["note"]; !ok {
		t.Error("Applied не догнал черновик при nothing_to_apply")
	}
}

// Сбой записи второго файла возвращает диск и состояние к прежнему.
func TestApply_WriteFailureRestoresPrevious(t *testing.T) {
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	mihomo := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	calls := 0
	failing := func(path string, data []byte) error {
		calls++
		if calls == 2 {
			return os.ErrPermission
		}
		return utils.AtomicReplaceFile(path, data)
	}
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray, Mihomo: mihomo}, DevMode: true, WriteFile: failing})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	r := view.Result
	if r == nil || r.OK || r.Code != ResultWriteFailed || !r.RolledBack {
		t.Fatalf("Result = %+v, want write_failed с RolledBack", r)
	}
	if s := stepOf(t, view, StepWrite); s.State != StepFailed {
		t.Errorf("write = %+v, want failed", s)
	}
	requireNothingWritten(t, env)
}
