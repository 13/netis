package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

func TestIntegrationRunnerReadsCurrentSettings(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runner := newIntegrationRunner(st, events.NewService(st, events.NewBroker()))

	// Unconfigured → the not-configured sentinel.
	if err := runner.Run(context.Background(), "pihole"); !errors.Is(err, errNotConfigured) {
		t.Fatalf("unconfigured pihole: got %v, want errNotConfigured", err)
	}

	// Configured but unreachable → a real error that is NOT the sentinel, proving
	// the runner read the current setting and attempted the run (run-now no longer
	// reports "not configured" for a configured integration).
	if err := st.SetSetting(t.Context(), "pihole_url", "http://127.0.0.1:9"); err != nil {
		t.Fatal(err)
	}
	err = runner.Run(context.Background(), "pihole")
	if err == nil || errors.Is(err, errNotConfigured) {
		t.Fatalf("configured pihole: got %v, want a non-nil non-sentinel error", err)
	}

	// Unknown integration name → error.
	if err := runner.Run(context.Background(), "bogus"); err == nil {
		t.Fatal("bogus integration should error")
	}
}

func TestRunIntegrationLoopRunsThenStops(t *testing.T) {
	var calls int32
	runner := integrationRunner{
		"x": func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return errNotConfigured
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runIntegrationLoop(ctx, runner, "x", time.Hour) // long interval: only the immediate run fires
		close(done)
	}()
	// The loop runs the closure once immediately, before waiting on the ticker.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("loop never ran the closure")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return after context cancel")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("closure called %d times, want exactly 1 (immediate run only)", got)
	}
}
