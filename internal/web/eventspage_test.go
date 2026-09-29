package web

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// The events log pages backwards through everything instead of stopping at
// a fixed count.
func TestEventsPagePages(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	for i := range eventsPageSize + 10 {
		st.AddEvent(t.Context(), "online", nil, fmt.Sprintf("ev-%03d-", i))
	}
	first := authedGet(t, srv, st, "/events").Body.String()
	if n := strings.Count(first, `class="ev"`); n != eventsPageSize {
		t.Fatalf("first page shows %d events, want %d", n, eventsPageSize)
	}
	if !strings.Contains(first, "ev-059-") || strings.Contains(first, "ev-009-") {
		t.Error("first page is not the newest events")
	}
	m := regexp.MustCompile(`href="(/events\?before=\d+)"`).FindStringSubmatch(first)
	if m == nil {
		t.Fatal("no link to older events")
	}
	second := authedGet(t, srv, st, m[1]).Body.String()
	if n := strings.Count(second, `class="ev"`); n != 10 {
		t.Errorf("second page shows %d events, want 10", n)
	}
	if !strings.Contains(second, "ev-000-") || strings.Contains(second, "ev-010-") {
		t.Error("second page is not the older events")
	}
	if strings.Contains(second, "before=") || !strings.Contains(second, "Newest events") {
		t.Error("last page should link back to the newest, not further back")
	}
}

func TestEventsPageFilters(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	nas := mkDevice(t, st, "nas-box")
	tv := mkDevice(t, st, "tv")
	st.AddEvent(ctx, "offline", &nas, "nas-detail")
	st.AddEvent(ctx, "offline", &tv, "tv-detail")
	st.AddEvent(ctx, "scan_error", nil, "pihole sync failing: timeout")
	st.AddEvent(ctx, "scan_error", nil, "subnet 10.0.0.0/24: boom")
	// A failed backup, as written now and as written before sentence case.
	st.AddEvent(ctx, "scan_error", nil, "Scheduled backup failing")
	st.AddEvent(ctx, "scan_error", nil, "scheduled backup failing")
	if _, err := st.DB.Exec(`INSERT INTO event (ts,type,details) VALUES ('2020-01-01T12:00:00Z','online','old-detail')`); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct{ want, not []string }{
		"/events?type=sync_failed":                        {[]string{"Pi-hole: timeout", "Sync failed"}, []string{"boom", "nas-detail"}},
		"/events?type=scan_failed":                        {[]string{"boom", "Scan failed"}, []string{"Pi-hole: timeout", "backup failing"}},
		"/events?type=backup_failed":                      {[]string{"Scheduled backup failing", "scheduled backup failing"}, []string{"boom"}},
		"/events?type=scan_error":                         {[]string{"boom", "Pi-hole: timeout"}, []string{"nas-detail"}},
		"/events?device=NAS":                              {[]string{"nas-detail"}, []string{"tv-detail", "boom"}},
		"/events?from=2020-01-01&to=2020-01-01":           {[]string{"old-detail"}, []string{"nas-detail"}},
		"/events?from=" + time.Now().Format("2006-01-02"): {[]string{"nas-detail"}, []string{"old-detail"}},
		"/events?type=bogus&from=junk":                    {[]string{"nas-detail", "old-detail"}, nil},
	}
	for path, c := range cases {
		body := authedGet(t, srv, st, path).Body.String()
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", path, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(body, n) {
				t.Errorf("%s: shows %q", path, n)
			}
		}
	}
	// Filters survive paging, and the empty result says so.
	empty := authedGet(t, srv, st, "/events?device=nothing-like-this").Body.String()
	if !strings.Contains(empty, "No matching events") {
		t.Error("filtered empty page has no empty state")
	}
}

// Events show human words, never the stored slugs.
func TestEventsPageUsesWords(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	for _, typ := range []string{"device_new", "ip_changed", "ip_conflict", "scan_error", "sync_recovered", "device_missing", "device_returned"} {
		st.AddEvent(t.Context(), typ, nil, "x")
	}
	for _, path := range []string{"/events", "/"} {
		text := tagRE.ReplaceAllString(authedGet(t, srv, st, path).Body.String(), " ")
		for _, slug := range []string{"device_new", "ip_changed", "ip_conflict", "scan_error", "sync_recovered", "device_missing", "device_returned"} {
			if strings.Contains(text, slug) {
				t.Errorf("%s shows the slug %q", path, slug)
			}
		}
	}
	empty, st2 := testServer(t)
	st2.SetSetting(t.Context(), "onboarded", "1")
	if body := authedGet(t, empty, st2, "/events").Body.String(); !strings.Contains(body, "No events yet") {
		t.Error("an empty log has no empty state")
	}
}

func mkDevice(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	id, err := st.CreateDevice(t.Context(), store.Device{Name: name, Kind: "other", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Every event row has the same four cells, so an event without a device
// keeps its details in the details column instead of sliding into the
// device's (on the events page and the dashboard alike).
func TestEventRowsKeepTheirColumns(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.AddEvent(t.Context(), "scan_error", nil, "Subnet 10.0.0.0/24: boom")
	for _, path := range []string{"/events", "/"} {
		body := authedGet(t, srv, st, path).Body.String()
		if !strings.Contains(body, `<span class="ev-dev"></span> <span class="ev-detail">Subnet 10.0.0.0/24: boom</span>`) {
			t.Errorf("%s: device-less event row does not keep an empty device cell", path)
		}
	}
	css := authedGet(t, srv, st, "/static/pages/events.css").Body.String()
	if strings.Contains(css, "grid-column:2 / 4") {
		t.Error("events.css still spans details over the device column")
	}
}
