package configlayer

import (
	"errors"
	"testing"
	"time"
)

func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, want event")
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
	return Event{}
}

func TestBroker_FanOut(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	ch1, cancel1, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	defer cancel1()
	ch2, cancel2, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}
	defer cancel2()

	want := DraftEvent{DraftRevision: 1, DraftChanges: 1}
	b.Publish(Event{Type: EventDraft, Data: want})

	for i, ch := range []<-chan Event{ch1, ch2} {
		ev := recvEvent(t, ch)
		if ev.Type != EventDraft {
			t.Fatalf("subscriber %d: type = %q, want %q", i+1, ev.Type, EventDraft)
		}
		got, ok := ev.Data.(DraftEvent)
		if !ok || got != want {
			t.Fatalf("subscriber %d: data = %#v, want %#v", i+1, ev.Data, want)
		}
	}
}

func TestBroker_SlowSubscriberDropped(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	slow, cancelSlow, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe slow: %v", err)
	}
	defer cancelSlow()
	fast, cancelFast, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe fast: %v", err)
	}
	defer cancelFast()

	const total = 33 // буфер подписчика — 32, тридцать третье событие переполняет его
	for i := 1; i <= total; i++ {
		b.Publish(Event{Type: EventDraft, Data: DraftEvent{DraftRevision: int64(i)}})
		// Быстрый подписчик читает сразу — его буфер не копится.
		ev := recvEvent(t, fast)
		if got := ev.Data.(DraftEvent).DraftRevision; got != int64(i) {
			t.Fatalf("fast subscriber: revision = %d, want %d", got, i)
		}
	}

	// Медленный: 32 буферизованных события, затем канал закрыт.
	for i := 1; i <= 32; i++ {
		ev := recvEvent(t, slow)
		if got := ev.Data.(DraftEvent).DraftRevision; got != int64(i) {
			t.Fatalf("slow subscriber: revision = %d, want %d", got, i)
		}
	}
	select {
	case _, ok := <-slow:
		if ok {
			t.Fatal("slow subscriber channel still delivers events, want closed")
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber channel not closed")
	}

	// Быстрый подписчик остаётся подключённым.
	b.Publish(Event{Type: EventDraft, Data: DraftEvent{DraftRevision: 34}})
	if got := recvEvent(t, fast).Data.(DraftEvent).DraftRevision; got != 34 {
		t.Fatalf("fast subscriber after drop: revision = %d, want 34", got)
	}
}

func TestBroker_MaxSubscribers(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	cancels := make([]func(), 0, 16)
	for i := 0; i < 16; i++ {
		_, cancel, err := b.Subscribe()
		if err != nil {
			t.Fatalf("Subscribe #%d: %v", i+1, err)
		}
		cancels = append(cancels, cancel)
	}
	if _, _, err := b.Subscribe(); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("Subscribe #17 err = %v, want ErrTooManySubscribers", err)
	}
	cancels[0]()
	_, cancel, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe after cancel: %v", err)
	}
	cancel()
}

func TestBroker_CancelIdempotent(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	ch, cancel, err := b.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()
	cancel() // повторный вызов не паникует
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed after cancel")
	}
	b.Close()
	b.Close()
}
