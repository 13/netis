package main

import (
	"context"
	"errors"
	"testing"

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
	if err := st.SetSetting("pihole_url", "http://127.0.0.1:9"); err != nil {
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
