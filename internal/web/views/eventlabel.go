package views

import (
	"sort"
	"strings"
	"time"

	"netis/internal/store"
)

// IntegrationTitles are the display names of the integrations, keyed by the
// slug they are stored and configured under. The scanner records its status
// as "scan" alongside them.
var IntegrationTitles = map[string]string{
	"proxmox":   "Proxmox",
	"wireguard": "WireGuard",
	"pihole":    "Pi-hole",
	"adguard":   "AdGuard Home",
	"opnsense":  "OPNsense",
}

// IntegrationName is an integration's display name. The scanner's status
// row is "Network scan"; a slug netis does not know is shown as stored.
func IntegrationName(slug string) string {
	if t, ok := IntegrationTitles[slug]; ok {
		return t
	}
	if slug == "scan" {
		return "Network scan"
	}
	return slug
}

// EventKind is how the UI names one kind of event: the words, an icon from
// the sprite and the status tone class its label takes.
type EventKind struct {
	// Key identifies the kind in the events filter. It is the stored type,
	// except for the kinds scan_error is split into.
	Key   string
	Label string
	Icon  string
	Tone  string
}

// eventKinds in the order the events filter lists them.
var eventKinds = []EventKind{
	{"device_new", "New device", "plus", "is-new"},
	{"online", "Came online", "activity", "is-online"},
	{"offline", "Went offline", "power", "is-offline"},
	{"ip_changed", "IP changed", "refresh-cw", ""},
	{"ip_conflict", "IP conflict", "triangle-alert", "is-conflict"},
	{"scan_failed", "Scan failed", "radar", "is-error"},
	{"sync_failed", "Sync failed", "refresh-cw", "is-error"},
	{"backup_failed", "Backup failed", "download", "is-error"},
	{"sync_recovered", "Sync recovered", "check", "is-online"},
	{"device_missing", "Gone upstream", "circle-help", "is-missing"},
	{"device_returned", "Back upstream", "check", "is-online"},
}

// EventKinds lists every kind the UI tells apart, in filter order.
func EventKinds() []EventKind {
	return eventKinds
}

// backupFailing is the details of the scan_error the backup scheduler raises
// (internal/backup). Events recorded before details were written in sentence
// case carry legacyBackupFailing; both are kept apart from failed scans.
const (
	backupFailing       = "Scheduled backup failing"
	legacyBackupFailing = "scheduled backup failing"
)

// syncFailingPrefixes are the details prefixes of the scan_error an
// integration raises when its sync starts failing ("pihole sync failing:
// timeout", cmd/netis recordStatus), one per integration, sorted.
func syncFailingPrefixes() []string {
	out := make([]string, 0, len(IntegrationTitles))
	for slug := range IntegrationTitles {
		out = append(out, slug+" sync failing:")
	}
	sort.Strings(out)
	return out
}

// eventKey classifies a stored event. Scan, integration and backup failures
// are all stored as scan_error; the details, which each writer formats in
// its own fixed way, tell them apart.
func eventKey(e store.Event) string {
	if e.Type != "scan_error" {
		return e.Type
	}
	if e.Details == backupFailing || e.Details == legacyBackupFailing {
		return "backup_failed"
	}
	for _, p := range syncFailingPrefixes() {
		if strings.HasPrefix(e.Details, p) {
			return "sync_failed"
		}
	}
	return "scan_failed"
}

// EventInfo is the kind an event is shown as. A type added later without a
// kind here shows in plain words rather than as its slug.
func EventInfo(e store.Event) EventKind {
	key := eventKey(e)
	for _, k := range eventKinds {
		if k.Key == key {
			return k
		}
	}
	return EventKind{Key: key, Label: sentence(key), Icon: "activity"}
}

// sentence turns a slug into sentence-case words: "port_opened" is "Port
// opened".
func sentence(slug string) string {
	s := strings.ReplaceAll(slug, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// integrationEventTypes are the event types whose details an integration may
// write starting with its slug.
var integrationEventTypes = map[string]bool{"scan_error": true, "sync_recovered": true, "device_new": true, "device_missing": true, "device_returned": true}

// EventDetail is an event's details as shown: an integration slug at the
// start ("pihole sync failing: timeout", "proxmox guest web is no longer in
// Proxmox") becomes its display name, and a sync failure reads "Pi-hole:
// timeout" since its label already says the sync failed.
func EventDetail(e store.Event) string {
	d := e.Details
	// Only the events integrations write start with their slug; an online or
	// offline event starts with the device's name, which may well be
	// "pihole".
	if !integrationEventTypes[e.Type] {
		return d
	}
	slug, rest, ok := strings.Cut(d, " ")
	if !ok {
		return d
	}
	title, known := IntegrationTitles[slug]
	if !known {
		return d
	}
	if after, found := strings.CutPrefix(rest, "sync failing: "); found && e.Type == "scan_error" {
		return title + ": " + after
	}
	return title + " " + rest
}

// EventDay is one day's events under its heading.
type EventDay struct {
	// Date is the day as YYYY-MM-DD.
	Date   string
	Label  string
	Events []store.Event
}

// GroupEventsByDay splits events (newest first) into days in now's time
// zone. The server does not know the reader's zone, so callers pass the
// server's local time: a homelab server usually shares its owner's clock.
// An event whose timestamp does not parse joins the day before it.
func GroupEventsByDay(evs []store.Event, now time.Time) []EventDay {
	loc := now.Location()
	today := dayStart(now)
	var out []EventDay
	for _, e := range evs {
		date := ""
		label := "Unknown date"
		if t, ok := parseTS(e.TS); ok {
			t = t.In(loc)
			date = t.Format("2006-01-02")
			label = dayLabel(dayStart(t), today)
		} else if len(out) > 0 {
			date, label = out[len(out)-1].Date, out[len(out)-1].Label
		}
		if len(out) == 0 || out[len(out)-1].Date != date || out[len(out)-1].Label != label {
			out = append(out, EventDay{Date: date, Label: label})
		}
		out[len(out)-1].Events = append(out[len(out)-1].Events, e)
	}
	return out
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// dayLabel names a day relative to today: "Today", "Yesterday", "Monday 28
// September", with the year once it is not this one.
func dayLabel(day, today time.Time) string {
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case day.Year() == today.Year():
		return day.Format("Monday 2 January")
	}
	return day.Format("Monday 2 January 2006")
}

// EventKindFilter says how to find the events of one kind in the store: the
// stored type and any details prefixes to keep or drop. The stored type
// itself is accepted too, so ?type=scan_error lists every failure. ok is
// false for a key that names no kind.
func EventKindFilter(key string) (f store.EventFilter, ok bool) {
	switch key {
	case "scan_failed":
		return store.EventFilter{Type: "scan_error", NotDetailPrefixes: append(syncFailingPrefixes(), backupFailing, legacyBackupFailing)}, true
	case "sync_failed":
		return store.EventFilter{Type: "scan_error", DetailPrefixes: syncFailingPrefixes()}, true
	case "backup_failed":
		return store.EventFilter{Type: "scan_error", DetailPrefixes: []string{backupFailing, legacyBackupFailing}}, true
	case "scan_error":
		return store.EventFilter{Type: "scan_error"}, true
	}
	for _, k := range eventKinds {
		if k.Key == key {
			return store.EventFilter{Type: key}, true
		}
	}
	return store.EventFilter{}, false
}
