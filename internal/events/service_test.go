package events

import (
	"testing"
	"time"

	"netis/internal/store"
)

func TestEmitWritesAndBroadcasts(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := NewBroker()
	svc := NewService(st, b)
	ch, cancel := b.Subscribe()
	defer cancel()

	svc.Emit(t.Context(), "scan_error", nil, "subnet unreachable")

	evs, _ := st.ListEvents(t.Context(), 5)
	if len(evs) != 1 || evs[0].Type != "scan_error" {
		t.Fatalf("db events: %+v", evs)
	}
	select {
	case m := <-ch:
		if m.Topic != "events" {
			t.Fatalf("topic=%q", m.Topic)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}
