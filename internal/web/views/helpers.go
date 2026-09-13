package views

import (
	"fmt"
	"time"

	"netis/internal/store"
)

// relTime renders an RFC3339 timestamp as a short relative string. On a parse
// failure it returns the input unchanged.
func relTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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

// sessionStarted describes when a session began. Sessions predating the
// created_at column have no answer, and saying so beats inventing one.
func sessionStarted(sess store.Session) string {
	if sess.CreatedAt == nil || *sess.CreatedAt == "" {
		return "unknown"
	}
	return relTime(*sess.CreatedAt)
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
