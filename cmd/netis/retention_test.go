package main

import (
	"context"
	"testing"

	"netis/internal/store"
)

func TestRetentionSetting(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Unset falls back to the default.
	if got := retentionSetting(ctx, st, "event_retention_days", 30); got != 30 {
		t.Errorf("unset = %d, want the default 30", got)
	}
	// Zero is a real choice — keep forever — not a missing value.
	if err := st.SetSetting(ctx, "event_retention_days", "0"); err != nil {
		t.Fatal(err)
	}
	if got := retentionSetting(ctx, st, "event_retention_days", 30); got != 0 {
		t.Errorf("explicit 0 = %d, want 0", got)
	}
	if err := st.SetSetting(ctx, "event_retention_days", "7"); err != nil {
		t.Fatal(err)
	}
	if got := retentionSetting(ctx, st, "event_retention_days", 30); got != 7 {
		t.Errorf("set = %d, want 7", got)
	}
	// Garbage and negatives fall back rather than deleting something unexpected.
	for _, bad := range []string{"soon", "-5", "3.5"} {
		if err := st.SetSetting(ctx, "event_retention_days", bad); err != nil {
			t.Fatal(err)
		}
		if got := retentionSetting(ctx, st, "event_retention_days", 30); got != 30 {
			t.Errorf("%q = %d, want the default 30", bad, got)
		}
	}
}

// runRetention must survive a database it cannot read without panicking, since
// it runs unattended on a ticker.
func TestRunRetentionOnBrokenStore(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	runRetention(context.Background(), st) // logs, does not panic
}
