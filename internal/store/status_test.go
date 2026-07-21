package store

import (
	"testing"
	"time"
)

func testIface(t *testing.T, s *Store) int64 {
	t.Helper()
	devID, _ := s.CreateDevice(t.Context(), Device{Name: "d", Kind: "other", Source: "manual"})
	ifID, _ := s.AddIface(t.Context(), devID, strp("11:22:33:44:55:66"), nil)
	return ifID
}

func TestMarkSeenAndMissed(t *testing.T) {
	s := openTest(t)
	ifID := testIface(t, s)
	now := time.Now().UTC()

	wasOffline, err := s.MarkSeen(t.Context(), ifID, 1.5, now)
	if err != nil || !wasOffline {
		t.Fatalf("first MarkSeen: wasOffline=%v err=%v", wasOffline, err)
	}
	wasOffline, _ = s.MarkSeen(t.Context(), ifID, 2.0, now)
	if wasOffline {
		t.Fatal("second MarkSeen should report already-online")
	}

	// 3 misses with offlineAfter=3: flips on the third
	for i := 1; i <= 2; i++ {
		if went, _ := s.MarkMissed(t.Context(), ifID, 3); went {
			t.Fatalf("miss %d should not flip", i)
		}
	}
	if went, _ := s.MarkMissed(t.Context(), ifID, 3); !went {
		t.Fatal("third miss should flip offline")
	}
	if went, _ := s.MarkMissed(t.Context(), ifID, 3); went {
		t.Fatal("already offline, no second flip")
	}
}

func TestAvailability(t *testing.T) {
	s := openTest(t)
	ifID := testIface(t, s)
	b := "2026-07-11T10:00:00Z"
	s.RecordAvailability(t.Context(), ifID, true, b)
	s.RecordAvailability(t.Context(), ifID, true, b)
	s.RecordAvailability(t.Context(), ifID, false, b)
	pct, err := s.AvailabilityPct(t.Context(), ifID, "2026-07-01T00:00:00Z")
	if err != nil || pct < 66.0 || pct > 67.0 {
		t.Fatalf("pct=%v err=%v", pct, err)
	}
}

func TestEvents(t *testing.T) {
	s := openTest(t)
	if _, err := s.AddEvent(t.Context(), "scan_error", nil, "boom"); err != nil {
		t.Fatal(err)
	}
	evs, err := s.ListEvents(t.Context(), 10)
	if err != nil || len(evs) != 1 || evs[0].Type != "scan_error" {
		t.Fatalf("evs=%+v err=%v", evs, err)
	}
}
