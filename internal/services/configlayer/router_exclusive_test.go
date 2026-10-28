//go:build router

package configlayer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Исключительный замок менеджера geodata на устройстве: настоящий слой, настоящее
// хранилище и настоящий `xray run -test` роутера. Тест ядро не читает и не
// переключает — метка ядра не нужна.

// rtGeodataEventHolder ждёт событие geodata и возвращает держателя из него.
func rtGeodataEventHolder(t *testing.T, ch <-chan Event, what string) string {
	t.Helper()
	ev, ok := rtNextEvent(ch, EventGeodata, 5*time.Second)
	if !ok {
		t.Fatalf("%s: события geodata нет", what)
	}
	data, ok := ev.Data.(GeodataEvent)
	if !ok {
		t.Fatalf("%s: данные события geodata — %T", what, ev.Data)
	}
	return data.Holder
}

func TestRouterExclusiveGeodata(t *testing.T) {
	layer, _ := rtNewCheckLayer(t, func(*Layer) {})
	ctx := context.Background()
	events, unsubscribe, err := layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	release, err := layer.BeginExclusive(ctx, false, HolderGeodataUpdate)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	if h := rtGeodataEventHolder(t, events, "взятие замка"); h != HolderGeodataUpdate {
		t.Errorf("держатель в событии при взятии: %q", h)
	}

	// «Применить» по кнопке получает отказ с понятной причиной, а не apply_busy.
	if err := layer.StartApply(true); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("StartApply при замке менеджера: %v, ожидалась ErrGeodataBusy", err)
	}
	if h := layer.Snapshot().BusyHolder; h != HolderGeodataUpdate {
		t.Errorf("Snapshot().BusyHolder = %q", h)
	}
	if h := layer.BusyHolder(); h != HolderGeodataUpdate {
		t.Errorf("BusyHolder() = %q", h)
	}
	// Второй -ug по кнопке и выключение слоя — тоже geodata_busy.
	if _, err := layer.BeginExclusive(ctx, true, HolderGeodataUpdate); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("второй BeginExclusive по кнопке: %v", err)
	}
	committed := false
	if err := layer.Disable(ctx, func() error { committed = true; return nil }); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("Disable при замке менеджера: %v", err)
	}
	if committed {
		t.Error("Disable вызвал commit при занятом замке")
	}

	release()
	release() // повторный вызов безопасен
	if h := rtGeodataEventHolder(t, events, "снятие замка"); h != "" {
		t.Errorf("держатель в событии после снятия: %q", h)
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("BusyHolder() после release = %q", h)
	}
	if h := layer.Snapshot().BusyHolder; h != "" {
		t.Errorf("Snapshot().BusyHolder после release = %q", h)
	}

	// Замок свободен: «Применить» проходит.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply после release: %v", err)
	}
	rtWaitUserApply(t, layer)

	// Фоновый вызов ждёт конца идущего применения.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("второй StartApply: %v", err)
	}
	sawRunning := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if layer.pipeline.Current().Running {
			sawRunning = true
			break
		}
	}
	var acquired atomic.Bool
	var runningAtAcquire atomic.Bool
	var holderAtAcquire atomic.Value
	errCh := make(chan error, 1)
	go func() {
		rel, err := layer.BeginExclusive(ctx, false, HolderGeodataRemove)
		if err != nil {
			errCh <- err
			return
		}
		runningAtAcquire.Store(layer.pipeline.Current().Running)
		holderAtAcquire.Store(layer.BusyHolder())
		acquired.Store(true)
		rel()
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("фоновый BeginExclusive: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("фоновый BeginExclusive не получил замок после применения")
	}
	if !acquired.Load() {
		t.Fatal("замок не взят")
	}
	if sawRunning && runningAtAcquire.Load() {
		t.Error("фоновый BeginExclusive взял замок, пока применение ещё шло")
	}
	if h, _ := holderAtAcquire.Load().(string); h != HolderGeodataRemove {
		t.Errorf("держатель в замке фонового вызова: %q", h)
	}
	if !sawRunning {
		t.Log("применение закончилось раньше, чем тест увидел Running: порядок ожидания не проверен")
	}
	rtWaitUserApply(t, layer)
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("BusyHolder() в конце теста = %q", h)
	}
}
