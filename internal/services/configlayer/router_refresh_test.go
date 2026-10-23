//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Проверка фонового обновления источника на устройстве: настоящие хранилище,
// конвейер и бинарник Xray роутера (проверка `xray run -test`), временный каталог в
// рабочем каталоге теста. Перезапуск ядра не настроен (Applier == nil): шаг
// пропускается, ядра не трогаются. Вердикт — файл на диске, файл состояния и ответ
// Xray, а не эталон.

const rtRefreshXrayBin = "/opt/sbin/xray"

// rtRefreshGen выдаёт по файлу Xray на Live["probe"]: n — число outbound, bad —
// заведомо невалидный протокол (его отвергает Xray).
type rtRefreshGen struct{}

func (rtRefreshGen) ID() string { return "rt-refresh" }

func (rtRefreshGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	raw, ok := in.Live["probe"]
	if !ok {
		return nil, nil
	}
	var probe struct {
		N   int  `json:"n"`
		Bad bool `json:"bad"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	outbounds := make([]map[string]any, 0, probe.N)
	for i := 0; i < probe.N; i++ {
		protocol := "freedom"
		if probe.Bad {
			protocol = "xcp-no-such-protocol"
		}
		outbounds = append(outbounds, map[string]any{"tag": "xcp-probe-" + string(rune('a'+i)), "protocol": protocol})
	}
	content, err := json.Marshal(map[string]any{"outbounds": outbounds})
	if err != nil {
		return nil, err
	}
	return []GeneratedFile{{Kernel: KernelXray, RelPath: "04_outbounds.xcp-probe.tail.json", Kind: KindXrayJSON, Content: content}}, nil
}

// rtHookCall — один вызов хука фиксации живых данных.
type rtHookCall struct {
	trigger   Trigger
	source    SourceKind
	mode      SourceKind
	usedProbe string
	// manifestHash — хэш записи манифеста внутри хука; diskHash — хэш файла на диске в тот же момент.
	manifestHash, diskHash string
}

type rtRefreshEnv struct {
	layer   *Layer
	roots   Roots
	file    string
	mu      sync.Mutex
	calls   []rtHookCall
	failing bool
	drift   chan struct{}
}

func (e *rtRefreshEnv) hookCalls() []rtHookCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]rtHookCall(nil), e.calls...)
}

func (e *rtRefreshEnv) setFailing(v bool) {
	e.mu.Lock()
	e.failing = v
	e.mu.Unlock()
}

func rtNewRefreshEnv(t *testing.T) *rtRefreshEnv {
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
	e := &rtRefreshEnv{layer: layer, roots: roots, file: filepath.Join(roots.Xray, "04_outbounds.xcp-probe.tail.json"), drift: make(chan struct{}, 8)}
	if err := layer.RegisterGenerator(rtRefreshGen{}); err != nil {
		t.Fatal(err)
	}
	hook := func(st *State, req ApplyRequest, used Input) error {
		call := rtHookCall{trigger: req.Trigger, source: req.Source, mode: used.Mode, usedProbe: string(used.Live["probe"])}
		if entry, ok := st.Manifest["xray:04_outbounds.xcp-probe.tail.json"]; ok {
			call.manifestHash = entry.Hash
		}
		if data, err := os.ReadFile(e.file); err == nil {
			call.diskHash = HashContent(data)
		}
		e.mu.Lock()
		e.calls = append(e.calls, call)
		failing := e.failing
		e.mu.Unlock()
		if failing {
			return errors.New("отказ хука")
		}
		if st.Live == nil {
			st.Live = Sections{}
		}
		st.Live["probe"] = used.Live["probe"]
		return nil
	}
	if err := layer.AddLiveCommitter(hook); err != nil {
		t.Fatal(err)
	}
	if err := layer.OnDriftResolved(func() { e.drift <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	if err := layer.AddLiveCommitter(hook); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("AddLiveCommitter после Start: %v", err)
	}
	if err := layer.OnDriftResolved(func() {}); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("OnDriftResolved после Start: %v", err)
	}
	return e
}

func rtOutboundCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл %s: %v", path, err)
	}
	var parsed struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("разбор файла: %v", err)
	}
	return len(parsed.Outbounds)
}

func rtDrainSteps(ch <-chan Event) []StepView {
	var out []StepView
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			if sv, ok := ev.Data.(StepView); ok && ev.Type == EventApplyStep {
				out = append(out, sv)
			}
		default:
			return out
		}
	}
}

func TestRouterSourceRefresh(t *testing.T) {
	e := rtNewRefreshEnv(t)
	layer := e.layer
	ctx := context.Background()
	events, cancel, err := layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	// Черновик с несколькими правками: ревизия растёт, секция other не применена.
	var rev int64
	for _, v := range []string{`{"a":1}`, `{"a":2}`, `{"a":3}`} {
		ev, err := layer.EditDraft(rev, "other", json.RawMessage(v))
		if err != nil {
			t.Fatalf("EditDraft: %v", err)
		}
		rev = ev.DraftRevision
	}
	draftBefore, err := layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	changesBefore := layer.Snapshot().DraftChanges

	refresh := func(overlay string) (ApplyView, error) {
		return layer.RunSourceRefresh(ctx, Sections{"probe": json.RawMessage(overlay)})
	}
	assertDraftUntouched := func(step string) {
		t.Helper()
		now, err := layer.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		if now.DraftRevision != draftBefore.DraftRevision || !SectionEqual(now.Draft["other"], draftBefore.Draft["other"]) ||
			len(now.Draft) != len(draftBefore.Draft) || len(now.Applied) != len(draftBefore.Applied) {
			t.Errorf("%s: черновик или применённое состояние изменились: ревизия %d -> %d", step, draftBefore.DraftRevision, now.DraftRevision)
		}
		if got := layer.Snapshot().DraftChanges; got != changesBefore {
			t.Errorf("%s: DraftChanges %d -> %d", step, changesBefore, got)
		}
	}

	t.Run("CommitsLiveNotDraft", func(t *testing.T) {
		if view, err := refresh(`{"n":1}`); err != nil || view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("первое обновление: %v, %+v", err, view.Result)
		}
		if got := rtOutboundCount(t, e.file); got != 1 {
			t.Fatalf("outbound в файле: %d, нужен 1", got)
		}
		steps := rtDrainSteps(events)
		for _, sv := range steps {
			if sv.Trigger != TriggerSourceRefresh {
				t.Errorf("шаг %s без trigger source_refresh: %q", sv.ID, sv.Trigger)
			}
		}
		if len(steps) == 0 {
			t.Error("не пришло ни одного apply_step")
		}

		before := len(e.hookCalls())
		view, err := refresh(`{"n":2}`)
		if err != nil || view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("второе обновление: %v, %+v", err, view.Result)
		}
		if got := rtOutboundCount(t, e.file); got != 2 {
			t.Errorf("outbound в файле: %d, нужно 2", got)
		}
		calls := e.hookCalls()
		if len(calls) != before+1 {
			t.Fatalf("вызовов хука %d, нужен %d", len(calls), before+1)
		}
		c := calls[len(calls)-1]
		if c.trigger != TriggerSourceRefresh || c.source != SourceApplied || c.mode != SourceApplied || c.usedProbe != `{"n":2}` {
			t.Errorf("вызов хука: %+v", c)
		}
		if c.manifestHash == "" || c.manifestHash != c.diskHash {
			t.Errorf("внутри хука манифест %q и файл %q расходятся", c.manifestHash, c.diskHash)
		}
		if live, _ := layer.ReadState(); string(live.Live["probe"]) != `{"n":2}` {
			t.Errorf("Live после обновления: %q", live.Live["probe"])
		}
		assertDraftUntouched("после обновления")
	})

	t.Run("EmptyPlanStillCommitsLive", func(t *testing.T) {
		info, err := os.Stat(e.file)
		if err != nil {
			t.Fatal(err)
		}
		before := len(e.hookCalls())
		// Вывод генератора тот же, меняется только Live: ключ x не влияет на файл.
		view, err := refresh(`{"n":2,"x":1}`)
		if err != nil || view.Result == nil || view.Result.Code != ResultNothingToApply {
			t.Fatalf("обновление с пустым планом: %v, %+v", err, view.Result)
		}
		after, err := os.Stat(e.file)
		if err != nil || !after.ModTime().Equal(info.ModTime()) {
			t.Errorf("файл переписан при пустом плане: %v", err)
		}
		if got := len(e.hookCalls()); got != before+1 {
			t.Errorf("вызовов хука %d, нужен %d", got, before+1)
		}
		if live, _ := layer.ReadState(); string(live.Live["probe"]) != `{"n":2,"x":1}` {
			t.Errorf("Live при пустом плане: %q", live.Live["probe"])
		}
		assertDraftUntouched("пустой план")
	})

	t.Run("NotCommittedOnFailure", func(t *testing.T) {
		liveBefore, _ := layer.ReadState()
		fileBefore, err := os.ReadFile(e.file)
		if err != nil {
			t.Fatal(err)
		}
		calls := len(e.hookCalls())

		// Xray отвергает конфигурацию: ни файла, ни Live, ни хука.
		view, err := refresh(`{"n":2,"bad":true}`)
		if err != nil || view.Result == nil || view.Result.OK {
			t.Fatalf("невалидная конфигурация принята: %v, %+v", err, view.Result)
		}
		if got := len(e.hookCalls()); got != calls {
			t.Errorf("хук вызван при отказе проверки: %d -> %d", calls, got)
		}

		// Отказ самого хука: файл возвращён, Live прежний.
		e.setFailing(true)
		view, err = refresh(`{"n":3}`)
		e.setFailing(false)
		if err != nil || view.Result == nil || view.Result.Code != ResultWriteFailed || !view.Result.RolledBack {
			t.Fatalf("отказ хука: %v, %+v", err, view.Result)
		}
		fileAfter, err := os.ReadFile(e.file)
		if err != nil || string(fileAfter) != string(fileBefore) {
			t.Errorf("файл не возвращён после отказа хука: %v", err)
		}
		liveAfter, _ := layer.ReadState()
		if string(liveAfter.Live["probe"]) != string(liveBefore.Live["probe"]) {
			t.Errorf("Live изменился при отказе: %q -> %q", liveBefore.Live["probe"], liveAfter.Live["probe"])
		}
		assertDraftUntouched("отказы")
	})

	t.Run("WaitsForApply", func(t *testing.T) {
		release, err := layer.pipeline.TryBegin(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			view ApplyView
			err  error
		}
		done := make(chan result, 1)
		go func() {
			v, err := refresh(`{"n":3}`)
			done <- result{v, err}
		}()
		select {
		case r := <-done:
			release()
			t.Fatalf("обновление не дождалось идущего применения: %v", r.err)
		case <-time.After(1500 * time.Millisecond):
		}
		release()
		select {
		case r := <-done:
			if r.err != nil || r.view.Result == nil || r.view.Result.Code != ResultApplied {
				t.Errorf("обновление после ожидания: %v, %+v", r.err, r.view.Result)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("обновление не выполнилось после снятия замка")
		}
		if got := rtOutboundCount(t, e.file); got != 3 {
			t.Errorf("outbound в файле: %d, нужно 3", got)
		}
	})

	t.Run("UserApplyCommitsUsedInput", func(t *testing.T) {
		waitIdle := func() {
			t.Helper()
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				if v := layer.pipeline.Current(); !v.Running && v.Result != nil && v.Trigger == TriggerUser {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatal("пользовательское применение не завершилось")
		}
		before := len(e.hookCalls())
		if err := layer.StartApply(true); err != nil {
			t.Fatalf("StartApply: %v", err)
		}
		waitIdle()
		calls := e.hookCalls()
		if len(calls) != before+1 {
			t.Fatalf("вызовов хука после «Применить»: %d, нужен %d", len(calls), before+1)
		}
		c := calls[len(calls)-1]
		if c.trigger != TriggerUser || c.source != SourceDraft || c.mode != SourceDraft || c.usedProbe != `{"n":3}` {
			t.Errorf("вызов хука «Применить»: %+v", c)
		}
		if got := layer.Snapshot().DraftChanges; got != 0 {
			t.Errorf("после «Применить» неприменённых изменений: %d", got)
		}

		// Повтор: применять нечего, хук всё равно вызывается.
		if err := layer.StartApply(true); err != nil {
			t.Fatalf("повторный StartApply: %v", err)
		}
		deadline := time.Now().Add(60 * time.Second)
		for len(e.hookCalls()) != before+2 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if got := len(e.hookCalls()); got != before+2 {
			t.Errorf("пустое «Применить» не вызвало хук: %d", got)
		}
		if v := layer.pipeline.Current(); v.Result != nil && v.Result.Code != ResultNothingToApply && !v.Running {
			t.Errorf("итог повторного «Применить»: %+v", v.Result)
		}
	})

	t.Run("DriftBlocksAndResolves", func(t *testing.T) {
		manual := []byte(`{"outbounds":[{"tag":"manual","protocol":"freedom"}]}`)
		if err := os.WriteFile(e.file, manual, 0o644); err != nil {
			t.Fatal(err)
		}
		calls := len(e.hookCalls())
		view, err := refresh(`{"n":4}`)
		if !errors.Is(err, ErrDriftBlocked) {
			t.Fatalf("при дрейфе нужен ErrDriftBlocked, получено %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultDriftBlocked {
			t.Errorf("итог при дрейфе: %+v", view.Result)
		}
		got, err := os.ReadFile(e.file)
		if err != nil || string(got) != string(manual) {
			t.Errorf("ручная правка затёрта при дрейфе: %v", err)
		}
		if n := len(e.hookCalls()); n != calls {
			t.Errorf("хук вызван при дрейфе: %d -> %d", calls, n)
		}
		if live, _ := layer.ReadState(); string(live.Live["probe"]) != `{"n":3}` {
			t.Errorf("Live изменился при дрейфе: %q", live.Live["probe"])
		}
		if now, _ := layer.ReadState(); now.DraftRevision != draftBefore.DraftRevision || !SectionEqual(now.Draft["other"], draftBefore.Draft["other"]) {
			t.Error("дрейф: черновик изменился")
		}

		// Сверка видит дрейф; колбэк пока молчит.
		if n := layer.checkNow(true).DriftCount; n != 1 {
			t.Fatalf("число файлов с дрейфом: %d", n)
		}
		select {
		case <-e.drift:
			t.Fatal("колбэк «дрейф решён» вызван при нерешённом дрейфе")
		case <-time.After(300 * time.Millisecond):
		}
		if _, err := layer.Release("xray:04_outbounds.xcp-probe.tail.json"); err != nil {
			t.Fatalf("Release: %v", err)
		}
		select {
		case <-e.drift:
		case <-time.After(5 * time.Second):
			t.Fatal("колбэк «дрейф решён» не вызван после «Принять правку»")
		}
		// Повторная сверка без дрейфа колбэк не будит.
		layer.checkNow(true)
		select {
		case <-e.drift:
			t.Error("колбэк вызван повторно без нового дрейфа")
		case <-time.After(300 * time.Millisecond):
		}
	})
}

func TestRouterSourceRefreshDisabled(t *testing.T) {
	dir := rtLiveDir(t)
	layer, err := New(Options{DataDir: dir, Enabled: func() bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	defer layer.Stop()
	if _, err := layer.RunSourceRefresh(context.Background(), Sections{"probe": json.RawMessage(`{}`)}); !errors.Is(err, ErrDisabled) {
		t.Errorf("RunSourceRefresh выключенного слоя: %v", err)
	}
}
