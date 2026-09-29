package views

import (
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

func TestEventInfoNamesEveryKind(t *testing.T) {
	cases := []struct {
		typ, details, key, label, tone string
	}{
		{"device_new", "", "device_new", "New device", "is-new"},
		{"online", "", "online", "Came online", "is-online"},
		{"offline", "", "offline", "Went offline", "is-offline"},
		{"ip_changed", "", "ip_changed", "IP changed", ""},
		{"ip_conflict", "", "ip_conflict", "IP conflict", "is-conflict"},
		{"scan_error", "subnet 10.0.0.0/24: sendto: operation not permitted", "scan_failed", "Scan failed", "is-error"},
		{"scan_error", "pihole sync failing: timeout", "sync_failed", "Sync failed", "is-error"},
		{"scan_error", "opnsense sync failing: configuration error", "sync_failed", "Sync failed", "is-error"},
		{"scan_error", "scheduled backup failing", "backup_failed", "Backup failed", "is-error"},
		{"sync_recovered", "pihole sync working again", "sync_recovered", "Sync recovered", "is-online"},
		{"device_missing", "", "device_missing", "Gone upstream", "is-missing"},
		{"device_returned", "", "device_returned", "Back upstream", "is-online"},
		{"port_opened", "", "port_opened", "Port opened", ""},
	}
	for _, c := range cases {
		k := EventInfo(store.Event{Type: c.typ, Details: c.details})
		if k.Key != c.key || k.Label != c.label || k.Tone != c.tone || k.Icon == "" {
			t.Errorf("%s %q: got %+v, want key=%s label=%q tone=%q", c.typ, c.details, k, c.key, c.label, c.tone)
		}
	}
}

func TestEventDetailUsesDisplayNames(t *testing.T) {
	cases := map[[2]string]string{
		{"scan_error", "pihole sync failing: timeout"}:                  "Pi-hole: timeout",
		{"scan_error", "adguard sync failing: auth failed"}:             "AdGuard Home: auth failed",
		{"sync_recovered", "wireguard sync working again"}:              "WireGuard sync working again",
		{"device_missing", "proxmox guest web is no longer in Proxmox"}: "Proxmox guest web is no longer in Proxmox",
		{"device_new", "opnsense device tv at 10.0.0.9"}:                "OPNsense device tv at 10.0.0.9",
		{"offline", "nas (10.0.0.2) went offline"}:                      "nas (10.0.0.2) went offline",
		{"scan_error", "subnet 10.0.0.0/24: boom"}:                      "subnet 10.0.0.0/24: boom",
		{"offline", "pihole went offline"}:                                            "pihole went offline",
	}
	for in, want := range cases {
		if got := EventDetail(store.Event{Type: in[0], Details: in[1]}); got != want {
			t.Errorf("EventDetail(%q) = %q, want %q", in[1], got, want)
		}
	}
}

func TestIntegrationName(t *testing.T) {
	for slug, want := range map[string]string{
		"pihole": "Pi-hole", "proxmox": "Proxmox", "wireguard": "WireGuard",
		"adguard": "AdGuard Home", "opnsense": "OPNsense", "scan": "Network scan", "other": "other",
	} {
		if got := IntegrationName(slug); got != want {
			t.Errorf("IntegrationName(%q) = %q, want %q", slug, got, want)
		}
	}
}

// Every kind the filter offers maps to a store filter, and the stored type
// scan_error still means every failure.
func TestEventKindFilter(t *testing.T) {
	for _, k := range EventKinds() {
		f, ok := EventKindFilter(k.Key)
		if !ok || f.Type == "" {
			t.Errorf("%s: no filter", k.Key)
		}
	}
	if f, _ := EventKindFilter("sync_failed"); f.Type != "scan_error" || len(f.DetailPrefixes) != len(IntegrationTitles) {
		t.Errorf("sync_failed = %+v", f)
	}
	if f, _ := EventKindFilter("scan_failed"); f.Type != "scan_error" || len(f.NotDetailPrefixes) != len(IntegrationTitles)+1 {
		t.Errorf("scan_failed = %+v", f)
	}
	if f, ok := EventKindFilter("scan_error"); !ok || f.Type != "scan_error" || f.DetailPrefixes != nil || f.NotDetailPrefixes != nil {
		t.Errorf("scan_error = %+v", f)
	}
	if _, ok := EventKindFilter("bogus"); ok {
		t.Error("bogus kind accepted")
	}
	for _, k := range EventKinds() {
		if strings.Contains(k.Label, "_") {
			t.Errorf("label %q is a slug", k.Label)
		}
	}
}

func TestGroupEventsByDay(t *testing.T) {
	loc := time.FixedZone("X", 2*3600)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, loc)
	evs := []store.Event{
		{ID: 6, TS: "2026-09-29T07:00:00Z"}, // 09:00 local, today
		{ID: 5, TS: "2026-09-28T23:30:00Z"}, // 01:30 local on the 29th: today, not yesterday
		{ID: 4, TS: "2026-09-28T12:00:00Z"},
		{ID: 3, TS: "garbage"},
		{ID: 2, TS: "2026-09-20T12:00:00Z"},
		{ID: 1, TS: "2025-12-31T12:00:00Z"},
	}
	days := GroupEventsByDay(evs, now)
	want := []struct {
		label string
		n     int
	}{{"Today", 2}, {"Yesterday", 2}, {"Sunday 20 September", 1}, {"Wednesday 31 December 2025", 1}}
	if len(days) != len(want) {
		t.Fatalf("days = %+v", days)
	}
	for i, w := range want {
		if days[i].Label != w.label || len(days[i].Events) != w.n {
			t.Errorf("day %d = %q with %d, want %q with %d", i, days[i].Label, len(days[i].Events), w.label, w.n)
		}
	}
	if days[0].Date != "2026-09-29" {
		t.Errorf("date = %q", days[0].Date)
	}
}
