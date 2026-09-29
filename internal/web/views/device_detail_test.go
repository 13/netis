package views

import (
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestBuildAvailability(t *testing.T) {
	loc := time.FixedZone("CEST", 2*3600)
	now := time.Date(2026, 9, 29, 15, 30, 0, 0, loc)
	hours := []store.AvailabilityBucket{
		// Today, 09:00 and 10:00 local: one full hour, one half.
		{Start: "2026-09-29T07:00:00Z", Up: 1},
		{Start: "2026-09-29T08:00:00Z", Up: 0.5},
		// 23:00 UTC on the 27th is 01:00 on the 28th locally.
		{Start: "2026-09-27T23:00:00Z", Up: 0},
		// Before the 30 days: ignored.
		{Start: "2026-08-01T10:00:00Z", Up: 0},
		{Start: "garbage", Up: 0},
	}
	a := BuildAvailability(hours, now, 30)
	if len(a.Days) != 30 {
		t.Fatalf("days=%d, want 30", len(a.Days))
	}
	if first := a.Days[0].Date; first.Format("2006-01-02") != "2026-08-31" {
		t.Errorf("first day %s, want 2026-08-31", first.Format("2006-01-02"))
	}
	today, yesterday := a.Days[29], a.Days[28]
	if !today.HasData || today.Pct != 75 {
		t.Errorf("today = %+v, want 75%%", today)
	}
	if !yesterday.HasData || yesterday.Pct != 0 {
		t.Errorf("the 28th = %+v, want 0%% from the hour that is local 01:00", yesterday)
	}
	if a.Days[10].HasData {
		t.Error("a day without sweeps has data")
	}
	if !a.HasData || a.Pct != 50 {
		t.Errorf("overall = %v %v, want 50%% over the three swept hours", a.HasData, a.Pct)
	}
	if a.Low.Date != yesterday.Date {
		t.Errorf("low day %v, want the 28th", a.Low.Date)
	}
	if s := availSummary(a); !strings.Contains(s, "50.0% online over the last 30 days") || !strings.Contains(s, "Lowest day: 28 Sep at 0.0%") {
		t.Errorf("summary = %q", s)
	}

	bars := availBars(a)
	if bars[10].Tone != "none" || bars[29].Tone != "low" || bars[28].FillH != "2.00" {
		t.Errorf("bars: no-data %q, today %q, down-day fill %q", bars[10].Tone, bars[29].Tone, bars[28].FillH)
	}

	empty := BuildAvailability(nil, now, 30)
	if empty.HasData || !strings.HasPrefix(availSummary(empty), "No availability recorded") {
		t.Errorf("empty history: %+v %q", empty, availSummary(empty))
	}
}

func TestFmtPct(t *testing.T) {
	for in, want := range map[float64]string{100: "100%", 99.97: "99.9%", 97.25: "97.2%", 0: "0.0%"} {
		if got := fmtPct(in); got != want {
			t.Errorf("fmtPct(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestKindNames(t *testing.T) {
	for in, want := range map[string]string{"wg-peer": "WireGuard peer", "lxc": "LXC container", "switch": "Switch", "other": "Other"} {
		if got := kindName(in); got != want {
			t.Errorf("kindName(%q) = %q, want %q", in, got, want)
		}
	}
}
