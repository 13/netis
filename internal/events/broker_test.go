package events

import (
	"testing"
	"time"
)

func TestPubSub(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	defer cancel()
	b.Publish("events", "hello")
	select {
	case m := <-ch:
		if m.Topic != "events" || m.Data != "hello" {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	cancel()
	b.Publish("events", "x") // must not panic on closed set
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received after cancel")
		}
	case <-time.After(100 * time.Millisecond):
	}
}
