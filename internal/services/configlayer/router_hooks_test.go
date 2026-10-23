//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Несколько фиксаторов живых данных, RunLive и хук после применения на
// устройстве: настоящий слой, хранилище и `xray run -test` роутера. Тест ядро не
// читает и не переключает — метка ядра не нужна.

// rtAfterRec — вызовы хука после применения.
type rtAfterRec struct {
	mu    sync.Mutex
	calls []AfterApplyInfo
	// lockFree — на момент вызова замок применения свободен (TryBegin прошёл).
	lockFree []bool
}

func (r *rtAfterRec) snapshot() []AfterApplyInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AfterApplyInfo(nil), r.calls...)
}

// rtWaitFor опрашивает условие с таймаутом: фиксированного sleep на ожидание нет.
func rtWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func TestRouterLiveCommittersAndAfterApply(t *testing.T) {
	checker := &rtCheckChecker{}
	after := &rtAfterRec{}
	var orderMu sync.Mutex
	var order [][]string
	var current []string
	commitA := func(st *State, req ApplyRequest, _ Input) error {
		orderMu.Lock()
		current = []string{"a"}
		orderMu.Unlock()
		if st.Live == nil {
			st.Live = Sections{}
		}
		raw, err := json.Marshal(string(req.Trigger))
		if err != nil {
			return err
		}
		st.Live["hook_a"] = raw
		return nil
	}
	commitB := func(st *State, req ApplyRequest, _ Input) error {
		orderMu.Lock()
		current = append(current, "b")
		order = append(order, current)
		orderMu.Unlock()
		if st.Live == nil {
			st.Live = Sections{}
		}
		raw, err := json.Marshal("b-" + string(req.Trigger))
		if err != nil {
			return err
		}
		st.Live["hook_b"] = raw
		return nil
	}
	layer, file := rtNewCheckLayer(t, func(l *Layer) {
		if err := l.RegisterChecker(checker); err != nil {
			t.Fatal(err)
		}
		if err := l.AddLiveCommitter(commitA); err != nil {
			t.Fatal(err)
		}
		if err := l.AddLiveCommitter(commitB); err != nil {
			t.Fatal(err)
		}
		if err := l.AddLiveCommitter(nil); err == nil {
			t.Error("пустой хук фиксации принят")
		}
		if err := l.AddAfterApply(func(ctx context.Context, info AfterApplyInfo) {
			// Хук вызван после снятия замка применения: он может сам запускать конвейер.
			release, err := l.pipeline.TryBegin(ctx, true)
			if err == nil {
				release()
			}
			after.mu.Lock()
			after.calls = append(after.calls, info)
			after.lockFree = append(after.lockFree, err == nil)
			after.mu.Unlock()
		}); err != nil {
			t.Fatal(err)
		}
		if err := l.AddAfterApply(nil); err == nil {
			t.Error("пустой хук после применения принят")
		}
	})
	if err := layer.AddLiveCommitter(commitA); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("AddLiveCommitter после Start: %v", err)
	}
	if err := layer.AddAfterApply(func(context.Context, AfterApplyInfo) {}); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("AddAfterApply после Start: %v", err)
	}

	// Применение, отвергнутое проверкой: ни фиксаторов, ни хука после применения.
	ev, err := layer.EditDraft(0, "check_block", json.RawMessage(`{"on":true}`))
	if err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	if err := layer.StartApply(true); !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("StartApply при сработавшей проверке: %v", err)
	}
	if _, err := layer.EditDraft(ev.DraftRevision, "check_block", nil); err != nil {
		t.Fatalf("удаление секции: %v", err)
	}

	// Успешное «Применить»: оба фиксатора пишут Live в одной записи, хук — один раз.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	if v := rtWaitUserApply(t, layer); v.Result.Code != ResultApplied {
		t.Fatalf("итог применения: %+v", v.Result)
	}
	rtWaitFor(t, "вызов хука после «Применить»", func() bool { return len(after.snapshot()) >= 1 })
	calls := after.snapshot()
	if len(calls) != 1 || calls[0].Trigger != TriggerUser || calls[0].Source != SourceDraft || !calls[0].Result.OK || calls[0].Result.Code != ResultApplied {
		t.Fatalf("вызовы хука после «Применить»: %+v", calls)
	}
	state, err := layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if string(state.Live["hook_a"]) != `"user"` || string(state.Live["hook_b"]) != `"b-user"` {
		t.Errorf("Live после «Применить»: a=%s b=%s", state.Live["hook_a"], state.Live["hook_b"])
	}
	orderMu.Lock()
	if len(order) != 1 || strings.Join(order[0], ",") != "a,b" {
		t.Errorf("порядок фиксаторов: %v", order)
	}
	orderMu.Unlock()

	// RunLive в обход черновика: ревизия и черновик прежние, хук получает select.
	revBefore := state.DraftRevision
	view, err := layer.RunLive(context.Background(), LiveRun{Trigger: TriggerSelect, Overlay: Sections{"check_tag": json.RawMessage(`"sel"`)}})
	if err != nil || view.Result == nil || view.Result.Code != ResultApplied {
		t.Fatalf("RunLive: %v, %+v", err, view.Result)
	}
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), "xcp-check-sel") {
		t.Errorf("оверлей не попал в файл: %v", err)
	}
	rtWaitFor(t, "вызов хука после RunLive", func() bool { return len(after.snapshot()) >= 2 })
	calls = after.snapshot()
	if len(calls) != 2 || calls[1].Trigger != TriggerSelect || calls[1].Source != SourceApplied {
		t.Fatalf("вызовы хука после RunLive: %+v", calls)
	}
	state, err = layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.DraftRevision != revBefore {
		t.Errorf("RunLive изменил ревизию черновика: %d -> %d", revBefore, state.DraftRevision)
	}
	if string(state.Live["hook_a"]) != `"select"` {
		t.Errorf("Live после RunLive: a=%s", state.Live["hook_a"])
	}
	if _, err := layer.RunLive(context.Background(), LiveRun{}); err == nil {
		t.Error("RunLive без триггера принят")
	}

	// Фиксаторы вызваны ровно по числу успешных запусков (отвергнутый проверкой
	// запуск их не вызывал), замок применения при вызове хука был свободен.
	orderMu.Lock()
	committed := len(order)
	orderMu.Unlock()
	if committed != 2 {
		t.Errorf("запусков фиксаторов %d, нужно 2", committed)
	}
	after.mu.Lock()
	for i, free := range after.lockFree {
		if !free {
			t.Errorf("хук %d вызван под замком применения", i)
		}
	}
	after.mu.Unlock()
}
