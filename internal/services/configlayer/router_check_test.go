//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Проверка до записи на устройстве: настоящий слой, настоящее хранилище и
// настоящий `xray run -test` роутера, временный каталог в рабочем каталоге
// теста. Тест ядро не читает и не переключает — метка ядра не нужна.

// rtCheckGen выдаёт один файл Xray с blackhole-исходящим; тег берётся из
// Live["check_tag"] (по умолчанию "base"), поэтому оверлей живых данных меняет
// содержимое файла.
type rtCheckGen struct{}

func (rtCheckGen) ID() string { return "rt-check" }

func (rtCheckGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	tag := "base"
	if raw, ok := in.Live["check_tag"]; ok {
		if err := json.Unmarshal(raw, &tag); err != nil {
			return nil, err
		}
	}
	content, err := json.Marshal(map[string]any{"outbounds": []map[string]any{{"tag": "xcp-check-" + tag, "protocol": "blackhole"}}})
	if err != nil {
		return nil, err
	}
	return []GeneratedFile{{Kernel: KernelXray, RelPath: "04_outbounds.xcp-check.tail.json", Kind: KindXrayJSON, Content: content}}, nil
}

// rtCheckChecker возвращает проблему, пока в черновике есть секция check_block
// или в живых данных есть ключ check_block (оверлей фонового запуска). Считает вызовы.
type rtCheckChecker struct{ calls atomic.Int64 }

func (c *rtCheckChecker) ID() string { return "rt-checker" }

func (c *rtCheckChecker) Check(in Input, _ InstalledKernels) []BuildIssue {
	c.calls.Add(1)
	_, inDraft := in.Sections["check_block"]
	_, inLive := in.Live["check_block"]
	if !inDraft && !inLive {
		return nil
	}
	return []BuildIssue{{Key: "group:g1", Code: "group_empty", Params: map[string]string{"group": "g1"}}}
}

// rtPanicChecker падает, пока в черновике есть секция check_panic: слой обязан
// остаться живым и отдать checker_failed.
type rtPanicChecker struct{}

func (rtPanicChecker) ID() string { return "rt-panic" }

func (rtPanicChecker) Check(in Input, _ InstalledKernels) []BuildIssue {
	if _, ok := in.Sections["check_panic"]; ok {
		panic("испорченная секция")
	}
	return nil
}

// rtWaitUserApply ждёт завершения пользовательского применения.
func rtWaitUserApply(t *testing.T, layer *Layer) ApplyView {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if v := layer.pipeline.Current(); !v.Running && v.Result != nil && v.Trigger == TriggerUser {
			return v
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("пользовательское применение не завершилось")
	return ApplyView{}
}

func rtNewCheckLayer(t *testing.T, register func(l *Layer)) (*Layer, string) {
	t.Helper()
	if _, err := os.Stat(rtRefreshXrayBin); err != nil {
		t.Skipf("Xray не установлен на устройстве: %v", err)
	}
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, roots.Mihomo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(rtCheckGen{}); err != nil {
		t.Fatal(err)
	}
	register(layer)
	layer.Start()
	return layer, filepath.Join(roots.Xray, "04_outbounds.xcp-check.tail.json")
}

func TestRouterCheckerBlocksApply(t *testing.T) {
	checker := &rtCheckChecker{}
	layer, file := rtNewCheckLayer(t, func(l *Layer) {
		if err := l.RegisterChecker(checker); err != nil {
			t.Fatal(err)
		}
		if err := l.RegisterChecker(checker); err == nil {
			t.Error("повторная регистрация проверки с тем же ID принята")
		}
		if err := l.RegisterChecker(rtPanicChecker{}); err != nil {
			t.Fatal(err)
		}
	})
	if err := layer.RegisterChecker(&rtCheckChecker{}); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("RegisterChecker после Start: %v", err)
	}
	ctx := context.Background()

	// Секция есть: «Применить» отказывает до начала применения, файла нет.
	ev, err := layer.EditDraft(0, "check_block", json.RawMessage(`{"on":true}`))
	if err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	err = layer.StartApply(true)
	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("StartApply при сработавшей проверке: %v", err)
	}
	var ce *ChecksError
	if !errors.As(err, &ce) {
		t.Fatalf("ошибка не *ChecksError: %T", err)
	}
	found := false
	for _, is := range ce.Issues {
		if is.Code == "group_empty" && is.Params["group"] == "g1" {
			found = true
		}
	}
	if !found {
		t.Errorf("в отказе нет проблемы с Params: %+v", ce.Issues)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("файл записан при отказе проверки: %v", err)
	}
	if v := layer.pipeline.Current(); v.Running || v.Result != nil {
		t.Errorf("применение началось при отказе проверки: %+v", v)
	}

	// Фоновый запуск обходит черновик, но проверка идёт и в нём: оверлей
	// живых данных вызывает проблему — build_failed, файл не записан.
	view, err := layer.RunSourceRefresh(ctx, Sections{"check_block": json.RawMessage(`true`)})
	if err != nil {
		t.Fatalf("RunSourceRefresh: %v", err)
	}
	if view.Result == nil || view.Result.Code != ResultBuildFailed {
		t.Fatalf("итог фонового запуска при сработавшей проверке: %+v", view.Result)
	}
	hasIssue := false
	for _, is := range view.Result.Issues {
		if is.Code == "group_empty" {
			hasIssue = true
		}
	}
	if !hasIssue {
		t.Errorf("в итоге фонового запуска нет проблемы проверки: %+v", view.Result.Issues)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("файл записан фоновым запуском при отказе проверки: %v", err)
	}

	// Паника проверки на испорченной секции: checker_failed с ID проверки, слой жив.
	ev, err = layer.EditDraft(ev.DraftRevision, "check_block", nil)
	if err != nil {
		t.Fatalf("удаление секции: %v", err)
	}
	ev, err = layer.EditDraft(ev.DraftRevision, "check_panic", json.RawMessage(`{"on":true}`))
	if err != nil {
		t.Fatalf("EditDraft check_panic: %v", err)
	}
	err = layer.StartApply(true)
	if !errors.As(err, &ce) || len(ce.Issues) != 1 || ce.Issues[0].Code != IssueCheckerFailed || ce.Issues[0].Key != "rt-panic" {
		t.Fatalf("StartApply с паникующей проверкой: %v", err)
	}
	if _, err = layer.EditDraft(ev.DraftRevision, "check_panic", nil); err != nil {
		t.Fatalf("удаление секции check_panic: %v", err)
	}

	// Секций нет: «Применить» проходит, файл записан.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply без проблем: %v", err)
	}
	if v := rtWaitUserApply(t, layer); v.Result.Code != ResultApplied {
		t.Fatalf("итог применения: %+v", v.Result)
	}
	if got := rtOutboundCount(t, file); got != 1 {
		t.Errorf("outbound в файле: %d, нужен 1", got)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// Фоновый запуск с оверлеем, меняющим файл и вызывающим проверку, файл не трогает.
	view, err = layer.RunSourceRefresh(ctx, Sections{"check_block": json.RawMessage(`true`), "check_tag": json.RawMessage(`"changed"`)})
	if err != nil || view.Result == nil || view.Result.Code != ResultBuildFailed {
		t.Fatalf("фоновый запуск после применения: %v, %+v", err, view.Result)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(before) {
		t.Errorf("файл изменён фоновым запуском при отказе проверки: %v", err)
	}
}
