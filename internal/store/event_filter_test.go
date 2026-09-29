package store

import (
	"testing"
)

func TestListEventsFiltered(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		nas, _ := s.CreateDevice(ctx, Device{Name: "NAS-Box", Kind: "server", Source: "manual"})
		tv, _ := s.CreateDevice(ctx, Device{Name: "tv_50%", Kind: "other", Source: "manual"})
		add := func(ts, typ string, dev *int64, details string) {
			t.Helper()
			if _, err := s.exec(ctx, `INSERT INTO event (ts,type,device_id,details) VALUES (?,?,?,?)`, ts, typ, dev, details); err != nil {
				t.Fatal(err)
			}
		}
		add("2026-09-27T10:00:00Z", "online", &nas, "nas up")
		add("2026-09-28T10:00:00Z", "offline", &tv, "tv down")
		add("2026-09-28T11:00:00Z", "scan_error", nil, "pihole sync failing: timeout")
		add("2026-09-28T12:00:00Z", "scan_error", nil, "subnet 10.0.0.0/24: boom")
		add("2026-09-29T10:00:00Z", "offline", &nas, "nas down")

		get := func(f EventFilter) ([]Event, bool) {
			t.Helper()
			evs, more, err := s.ListEventsFiltered(ctx, f)
			if err != nil {
				t.Fatal(err)
			}
			return evs, more
		}

		if evs, more := get(EventFilter{Limit: 10}); len(evs) != 5 || more || evs[0].Details != "nas down" {
			t.Fatalf("all: len=%d more=%v", len(evs), more)
		}
		if evs, _ := get(EventFilter{Type: "offline"}); len(evs) != 2 {
			t.Errorf("type offline: %d", len(evs))
		}
		if evs, _ := get(EventFilter{Type: "scan_error", DetailPrefixes: []string{"pihole sync failing:"}}); len(evs) != 1 || evs[0].Details != "pihole sync failing: timeout" {
			t.Errorf("sync failures: %+v", evs)
		}
		if evs, _ := get(EventFilter{Type: "scan_error", NotDetailPrefixes: []string{"pihole sync failing:"}}); len(evs) != 1 || evs[0].Details != "subnet 10.0.0.0/24: boom" {
			t.Errorf("scan failures: %+v", evs)
		}
		// Device search ignores case and treats LIKE wildcards literally.
		if evs, _ := get(EventFilter{Device: "nas-b"}); len(evs) != 2 {
			t.Errorf("device nas-b: %d", len(evs))
		}
		if evs, _ := get(EventFilter{Device: "_50%"}); len(evs) != 1 {
			t.Errorf("device _50%%: %d", len(evs))
		}
		if evs, _ := get(EventFilter{Device: "v%0"}); len(evs) != 0 {
			t.Errorf("wildcards in the search matched: %d", len(evs))
		}
		if evs, _ := get(EventFilter{Since: "2026-09-28T00:00:00Z", Until: "2026-09-29T00:00:00Z"}); len(evs) != 3 {
			t.Errorf("one day: %d", len(evs))
		}

		// Paging: two per page, then the rest.
		p1, more := get(EventFilter{Limit: 2})
		if len(p1) != 2 || !more {
			t.Fatalf("page 1: len=%d more=%v", len(p1), more)
		}
		p2, _ := get(EventFilter{Limit: 2, BeforeID: p1[1].ID})
		p3, more3 := get(EventFilter{Limit: 2, BeforeID: p2[1].ID})
		if len(p2) != 2 || len(p3) != 1 || more3 || p3[0].Details != "nas up" {
			t.Errorf("paging: p2=%d p3=%d more=%v", len(p2), len(p3), more3)
		}
	})
}

func TestAlertOfflineDeviceIDs(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		a, _ := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		b, _ := s.CreateDevice(ctx, Device{Name: "b", Kind: "other", Source: "manual"})
		if err := s.SetDeviceAlertOffline(ctx, b, true); err != nil {
			t.Fatal(err)
		}
		ids, err := s.AlertOfflineDeviceIDs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 || !ids[b] || ids[a] {
			t.Errorf("ids=%v, want only %d", ids, b)
		}
	})
}
