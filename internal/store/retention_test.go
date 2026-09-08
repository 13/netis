package store

import (
	"context"
	"testing"
	"time"
)

// Nothing in netis deleted rows before retention existed, so these tests care
// about the boundary: what is old enough to go, and what must stay.
func TestPrune(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

		devID, err := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		ifID, err := s.AddIface(ctx, devID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		uID, err := s.CreateUser(ctx, "u", "hash", "admin")
		if err != nil {
			t.Fatal(err)
		}

		// Sessions: one already expired, one still valid. The live one is dated
		// from real time, not the fixed `now` above, because GetSession checks
		// expiry against the wall clock.
		if err := s.CreateSession(ctx, "dead", uID,
			now.Add(-time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(ctx, "live", uID,
			time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}

		// Events: ts defaults to now, so the old one is written explicitly.
		if _, err := s.AddEvent(ctx, "online", &devID, "recent"); err != nil {
			t.Fatal(err)
		}
		old := now.AddDate(0, 0, -60).Format(time.RFC3339)
		if _, err := s.exec(ctx,
			`INSERT INTO event (ts,type,device_id,details) VALUES (?,?,?,?)`,
			old, "offline", devID, "ancient"); err != nil {
			t.Fatal(err)
		}

		// Availability buckets either side of the cutoff.
		for _, b := range []time.Time{now.AddDate(0, 0, -400), now.AddDate(0, 0, -10)} {
			if err := s.RecordAvailability(ctx, ifID, true, b.Format(time.RFC3339)); err != nil {
				t.Fatal(err)
			}
		}

		r, err := s.Prune(ctx, now, 30, 365)
		if err != nil {
			t.Fatal(err)
		}
		if r.Sessions != 1 || r.Events != 1 || r.Availability != 1 {
			t.Fatalf("Prune = %+v, want one of each", r)
		}
		if r.Total() != 3 {
			t.Errorf("Total() = %d, want 3", r.Total())
		}

		// The still-valid session survives, the expired one is gone.
		if _, ok, err := s.GetSession(ctx, "live"); err != nil || !ok {
			t.Errorf("live session: ok=%v err=%v", ok, err)
		}
		var n int
		if err := s.queryRow(ctx, `SELECT count(*) FROM session WHERE token=?`, "dead").
			Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Error("expired session was not deleted")
		}

		evs, err := s.ListEvents(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 1 || evs[0].Details != "recent" {
			t.Errorf("events after prune = %+v, want only the recent one", evs)
		}

		// The remaining bucket still backs the availability percentage.
		if pct, err := s.AvailabilityPct(ctx, ifID,
			now.AddDate(0, 0, -30).Format(time.RFC3339)); err != nil || pct != 100 {
			t.Errorf("AvailabilityPct = %v, %v", pct, err)
		}

		// A second sweep has nothing left to do.
		r2, err := s.Prune(ctx, now, 30, 365)
		if err != nil {
			t.Fatal(err)
		}
		if r2.Total() != 0 {
			t.Errorf("second sweep removed %+v, want nothing", r2)
		}
	})
}

// Zero means keep forever, and must not be read as "delete everything".
func TestPruneZeroRetentionKeepsEverything(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
		devID, err := s.CreateDevice(ctx, Device{Name: "d", Kind: "other", Source: "manual"})
		if err != nil {
			t.Fatal(err)
		}
		ifID, err := s.AddIface(ctx, devID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.exec(ctx, `INSERT INTO event (ts,type,device_id,details) VALUES (?,?,?,?)`,
			now.AddDate(-5, 0, 0).Format(time.RFC3339), "online", devID, "very old"); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordAvailability(ctx, ifID, true,
			now.AddDate(-5, 0, 0).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}

		r, err := s.Prune(ctx, now, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r.Events != 0 || r.Availability != 0 {
			t.Errorf("Prune with zero retention removed %+v, want nothing", r)
		}
		if evs, err := s.ListEvents(ctx, 10); err != nil || len(evs) != 1 {
			t.Errorf("events = %+v, %v", evs, err)
		}
	})
}
