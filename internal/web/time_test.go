package web

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// isoText matches an RFC3339 timestamp in page text.
var isoText = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d`)

// tagRE matches one HTML tag with its attributes, where a machine-readable
// datetime is allowed.
var tagRE = regexp.MustCompile(`<[^>]*>`)

// No page shows a raw ISO timestamp: every time is a <time> element whose
// text is relative and whose datetime attribute carries the instant.
func TestNoRawTimestampsInPages(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	now := time.Now().UTC()
	snID, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "lan", Kind: "lan", ScanIntervalSec: 120})
	devID, _ := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	mac := "aa:bb:cc:dd:ee:01"
	ifID, _ := st.AddIface(ctx, devID, &mac, nil)
	st.UpsertIPAssignment(ctx, ifID, snID, "10.0.0.5", "dhcp")
	st.MarkSeen(ctx, ifID, 1, now.Add(-3*time.Minute))
	st.UpsertOpenPort(ctx, ifID, 22, "tcp", "ssh", now.Add(-time.Hour).Format(time.RFC3339))
	st.AddEvent(ctx, "device_new", &devID, "found")
	st.SetIntegrationStatus(ctx, store.IntegrationStatus{Name: "proxmox", LastRun: now.Format(time.RFC3339), OK: true})

	for _, path := range []string{
		"/", "/devices", "/devices/" + itoa(devID), "/events",
		"/subnets/" + itoa(snID) + "/cell?ip=10.0.0.5",
		"/settings?tab=users", "/settings?tab=tokens", "/settings?tab=integrations",
		"/settings?tab=audit", "/settings?tab=about",
	} {
		body := authedGet(t, srv, st, path).Body.String()
		text := tagRE.ReplaceAllString(body, " ")
		if m := isoText.FindString(text); m != "" {
			i := strings.Index(text, m)
			t.Errorf("%s shows a raw timestamp %q in %q", path, m, text[max(0, i-60):min(len(text), i+40)])
		}
	}
	if body := authedGet(t, srv, st, "/devices").Body.String(); !strings.Contains(body, "<time datetime=") {
		t.Error("device list has no <time> element for last seen")
	}
}
