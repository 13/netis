package views

import (
	"fmt"
	"time"
)

// relTime renders an RFC3339 timestamp as a short relative string: "5m ago"
// for the past, "in 5m" for the future. On a parse failure it returns the
// input unchanged.
func relTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
		if d < time.Minute {
			return "in a moment"
		}
		return "in " + shortSpan(d)
	}
	if d < time.Minute {
		return "just now"
	}
	return shortSpan(d) + " ago"
}

// shortSpan renders a positive duration of at least a minute in its largest
// whole unit: "5m", "2h", "29d".
func shortSpan(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// BarPct renders n as a whole-percent width of total (e.g. "25%"), 0% when
// total is zero. Used for the dashboard subnet occupancy bar.
func BarPct(n, total int) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", n*100/total)
}

// orDash renders an optional string, falling back to an em dash so an empty
// table cell reads as "not recorded" rather than looking broken.
func orDash(v *string) string {
	if v == nil || *v == "" {
		return "—"
	}
	return *v
}

// shortAgent trims a user-agent string to something a table cell can hold.
func shortAgent(ua *string) string {
	if ua == nil || *ua == "" {
		return "—"
	}
	const max = 40
	if len(*ua) <= max {
		return *ua
	}
	return (*ua)[:max] + "…"
}
