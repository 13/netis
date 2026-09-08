package main

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"netis/internal/store"
)

// Retention defaults, in days. They bound the database without throwing away
// anything a user is likely to still want: a month of events covers "what
// happened while I was away", and a year of hourly availability buckets is what
// the uptime percentages are computed from.
const (
	defaultEventRetentionDays        = 30
	defaultAvailabilityRetentionDays = 365
	retentionInterval                = 6 * time.Hour
)

// startRetention sweeps expired sessions and aged-out history on a slow ticker.
// Nothing in netis deleted rows before this: events and availability buckets
// accumulated forever, and expired session rows outlived their usefulness.
//
// It sweeps once at startup so a long-stopped instance tidies up on boot, then
// every interval. Retention windows are read from settings on each pass, so
// changing them takes effect without a restart.
func startRetention(ctx context.Context, st *store.Store, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			runRetention(ctx, st)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func runRetention(ctx context.Context, st *store.Store) {
	eventDays := retentionSetting(ctx, st, "event_retention_days", defaultEventRetentionDays)
	availDays := retentionSetting(ctx, st, "availability_retention_days", defaultAvailabilityRetentionDays)

	r, err := st.Prune(ctx, time.Now(), eventDays, availDays)
	if err != nil {
		slog.Error("retention sweep failed", "err", err)
		return
	}
	if r.Total() > 0 {
		slog.Info("retention sweep", "sessions", r.Sessions,
			"events", r.Events, "availability", r.Availability)
	}
}

// retentionSetting reads a day count from settings, falling back to def when it
// is unset or unparseable. Zero is a legitimate value meaning "keep forever",
// so it is not treated as missing.
func retentionSetting(ctx context.Context, st *store.Store, key string, def int) int {
	v, err := st.GetSetting(ctx, key)
	if err != nil {
		slog.Error("read retention setting", "key", key, "err", err)
		return def
	}
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		slog.Warn("ignoring invalid retention setting", "key", key, "value", v)
		return def
	}
	return n
}
