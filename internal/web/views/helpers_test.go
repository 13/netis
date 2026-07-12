package views

import (
	"testing"
	"time"
)

func TestRelTime(t *testing.T) {
	now := time.Now().UTC()
	cases := map[string]string{
		now.Add(-30 * time.Second).Format(time.RFC3339):   "just now",
		now.Add(-5 * time.Minute).Format(time.RFC3339):    "5m ago",
		now.Add(-2 * time.Hour).Format(time.RFC3339):      "2h ago",
		now.Add(-3 * 24 * time.Hour).Format(time.RFC3339): "3d ago",
		"not-a-time": "not-a-time",
	}
	for in, want := range cases {
		if got := relTime(in); got != want {
			t.Errorf("relTime(%q)=%q want %q", in, got, want)
		}
	}
}
