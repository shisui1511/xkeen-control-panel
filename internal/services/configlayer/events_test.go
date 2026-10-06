package configlayer

import (
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
