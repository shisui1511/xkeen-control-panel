package configlayer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
