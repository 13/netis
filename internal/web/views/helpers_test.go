package views

import (
	"context"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestRelTime(t *testing.T) {
	now := time.Now().UTC()
	cases := map[string]string{
		now.Add(-30 * time.Second).Format(time.RFC3339):              "just now",
		now.Add(-5 * time.Minute).Format(time.RFC3339):               "5m ago",
		now.Add(-2 * time.Hour).Format(time.RFC3339):                 "2h ago",
		now.Add(-3 * 24 * time.Hour).Format(time.RFC3339):            "3d ago",
		now.Add(20 * time.Second).Format(time.RFC3339):               "in a moment",
		now.Add(5*time.Minute + 30*time.Second).Format(time.RFC3339): "in 5m",
		now.Add(3*time.Hour + time.Minute).Format(time.RFC3339):      "in 3h",
		now.Add(29*24*time.Hour + time.Hour).Format(time.RFC3339):    "in 29d",
		"not-a-time": "not-a-time",
	}
	for in, want := range cases {
		if got := relTime(in); got != want {
			t.Errorf("relTime(%q)=%q want %q", in, got, want)
		}
	}
}

// A token's expiry reads like its other times, and says when it already passed.
func TestTokenExpiry(t *testing.T) {
	future := time.Now().UTC().Add(29*24*time.Hour + time.Hour).Format(time.RFC3339)
	past := time.Now().UTC().Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	cases := []struct {
		exp  *string
		want string
	}{{nil, "never"}, {&future, ">in 29d</time>"}, {&past, "expired <time"}}
	for _, c := range cases {
		var b strings.Builder
		if err := tokenExpiry(store.APIToken{ExpiresAt: c.exp}).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), c.want) {
			t.Errorf("tokenExpiry(%v)=%q want %q", c.exp, b.String(), c.want)
		}
	}
}

// Every timestamp is a <time> carrying the machine-readable instant and the
// absolute time on hover, with the relative time as its text; an empty one
// is a dash and never an empty cell.
func TestTimeComponent(t *testing.T) {
	ts := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	var b strings.Builder
	if err := Time(ts.Format(time.RFC3339)).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	want := `<time datetime="` + ts.Format(time.RFC3339) + `" title="` + ts.Format("2 Jan 2006, 15:04 UTC") + `">5m ago</time>`
	if b.String() != want {
		t.Errorf("Time = %s\nwant   %s", b.String(), want)
	}
	b.Reset()
	if err := Time("").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "—") {
		t.Errorf("empty Time = %q, want a dash", b.String())
	}
}
