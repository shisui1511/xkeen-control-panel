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
