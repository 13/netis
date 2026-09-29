package web

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// The attention list names every kind of trouble, in order, with the action
// that fixes it; the health strip counts them and links to the lists.
func TestDashboardAttentionItems(t *testing.T) {
	srv, st := testServer(t)
	ctx := t.Context()
	st.SetSetting(ctx, "onboarded", "1")
	now := time.Now().UTC()
	snID, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120})
	mk := func(name, source, ip string) (int64, int64) {
		id, _ := st.CreateDevice(ctx, store.Device{Name: name, Kind: "other", Source: source, Vendor: "Acme"})
		ifID, _ := st.AddIface(ctx, id, nil, nil)
		st.AssignIP(ctx, ifID, snID, ip, "dhcp")
		return id, ifID
	}
	// An unreviewed scan discovery that does not look like "unknown-...".
	fresh, _ := mk("espressif-112", "scan", "10.0.0.112")
	// An alerting device that went offline, and a quiet one that did too.
	nas, nasIf := mk("nas", "manual", "10.0.0.2")
	st.MarkSeen(ctx, nasIf, 1, now.Add(-time.Hour))
	st.MarkMissed(ctx, nasIf, 1)
	st.SetDeviceAlertOffline(ctx, nas, true)
	_, quietIf := mk("quiet", "manual", "10.0.0.3")
	st.MarkSeen(ctx, quietIf, 1, now.Add(-time.Hour))
	st.MarkMissed(ctx, quietIf, 1)
	// A conflict on .2.
	mk("ghost", "manual", "10.0.0.2")
	// A guest gone from Proxmox.
	guest, _ := st.CreateDevice(ctx, store.Device{Name: "k3s-node", Kind: "vm", Source: "proxmox"})
	if _, err := st.DB.Exec(`UPDATE device SET upstream_missing_since=? WHERE id=?`, now.Add(-2*time.Hour).Format(time.RFC3339), guest); err != nil {
		t.Fatal(err)
	}
	// A failing integration and a working one.
	st.SetIntegrationStatus(ctx, store.IntegrationStatus{Name: "pihole", LastRun: now.Format(time.RFC3339), Detail: "timeout"})
	st.SetIntegrationStatus(ctx, store.IntegrationStatus{Name: "proxmox", LastRun: now.Format(time.RFC3339), OK: true})

	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, it := range data.Attention {
		kinds = append(kinds, it.Kind+":"+it.Title)
	}
	want := []string{
		"conflict:10.0.0.2 is claimed by more than one device",
		"integration:Pi-hole is failing",
		"offline:nas",
		"new:espressif-112",
		"upstream:k3s-node",
	}
	if strings.Join(kinds, "|") != strings.Join(want, "|") {
		t.Fatalf("attention =\n%s\nwant\n%s", strings.Join(kinds, "\n"), strings.Join(want, "\n"))
	}
	if data.Attention[3].DeviceID != fresh || data.Attention[3].IP != "10.0.0.112" {
		t.Errorf("new device item = %+v", data.Attention[3])
	}
	if data.Attention[1].Integration != "pihole" {
		t.Errorf("integration item = %+v", data.Attention[1])
	}
	h := data.Health
	if h.New != 1 || h.Conflicts != 1 || h.Failing != 1 || h.Offline != 5 || h.Online != 0 {
		t.Errorf("health = %+v", h)
	}
	if data.ConflictHref != fmt.Sprintf("/subnets/%d?ip=10.0.0.2", snID) {
		t.Errorf("conflict href = %q", data.ConflictHref)
	}

	body := authedGet(t, srv, st, "/").Body.String()
	for _, w := range []string{
		fmt.Sprintf(`action="/devices/%d/approve"`, fresh),
		`name="next" value="/"`,
		`hx-post="/settings/integrations/pihole/run"`,
		`href="/devices?status=offline"`,
		`href="/devices?new=1"`,
		"5 items need you",
		"No longer listed in Proxmox since",
	} {
		if !strings.Contains(body, w) {
			t.Errorf("admin dashboard missing %q", w)
		}
	}
	viewer := viewerGet(t, srv, st, "/")
	for _, w := range []string{"/approve", "/run", "Scan all"} {
		if strings.Contains(viewer, w) {
			t.Errorf("viewer dashboard has admin action %q", w)
		}
	}
	if !strings.Contains(viewer, "espressif-112") {
		t.Error("viewer does not see the new device")
	}
}

func TestDashboardAllClear(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateDevice(t.Context(), store.Device{Name: "nas", Kind: "server", Source: "manual"})
	st.CreateSubnet(t.Context(), store.Subnet{Name: "LAN", CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 300})
	st.SetIntegrationStatus(t.Context(), store.IntegrationStatus{Name: "scan", LastRun: "2026-09-29T10:00:00Z", OK: true})
	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Attention) != 0 {
		t.Fatalf("attention = %+v", data.Attention)
	}
	body := authedGet(t, srv, st, "/").Body.String()
	if strings.Count(body, "All clear") < 2 {
		t.Error("an empty attention list does not say all clear")
	}
	if strings.Contains(body, "Nothing scanned yet") {
		t.Error("a scanned network says nothing was scanned")
	}
}

// Before anything is scanned the zero counts mean "not known yet": the
// dashboard says so instead of all clear, with or without subnets.
func TestDashboardNothingScannedYet(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	check := func(when string) {
		t.Helper()
		body := authedGet(t, srv, st, "/").Body.String()
		if strings.Contains(body, "All clear") {
			t.Errorf("%s: says all clear", when)
		}
		if strings.Count(body, "Nothing scanned yet") < 2 {
			t.Errorf("%s: health strip and attention list do not say nothing was scanned", when)
		}
	}
	check("no subnets")
	st.CreateSubnet(t.Context(), store.Subnet{Name: "LAN", CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 300})
	check("subnet never scanned")

	// A network with only a WireGuard subnet has nothing to sweep.
	srv2, st2 := testServer(t)
	st2.SetSetting(t.Context(), "onboarded", "1")
	st2.CreateSubnet(t.Context(), store.Subnet{Name: "WG", CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 300})
	if body := authedGet(t, srv2, st2, "/").Body.String(); !strings.Contains(body, "All clear") {
		t.Error("a WireGuard-only network waits for a scan that never comes")
	}
}

// More unreviewed devices than the list names are counted and linked.
func TestDashboardCapsNewDevices(t *testing.T) {
	srv, st := testServer(t)
	for i := range 8 {
		st.CreateDevice(t.Context(), store.Device{Name: fmt.Sprintf("dev-%d", i), Kind: "other", Source: "scan"})
	}
	data, err := srv.assembleDashboard(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Attention) != dashNewLimit || data.MoreNew != 8-dashNewLimit || data.Health.New != 8 {
		t.Errorf("listed=%d more=%d new=%d", len(data.Attention), data.MoreNew, data.Health.New)
	}
}

// Approving from the dashboard comes back to the dashboard; a next that
// leaves the site is ignored.
func TestApproveReturnsToNext(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	id, _ := st.CreateDevice(t.Context(), store.Device{Name: "x", Kind: "other", Source: "scan"})
	path := fmt.Sprintf("/devices/%d/approve", id)
	wantRedirect(t, authedPost(t, srv, st, path, url.Values{"next": {"/"}}), "/")
	for _, bad := range []string{"//evil.example", `/\evil.example`, "https://evil.example", "evil"} {
		wantRedirect(t, authedPost(t, srv, st, path, url.Values{"next": {bad}}), "/devices")
	}
	wantRedirect(t, authedPost(t, srv, st, path, nil), "/devices")
}

func TestLocalNext(t *testing.T) {
	for in, want := range map[string]string{
		"/": "/", "/devices/3": "/devices/3", "": "/f", "//x": "/f", `/\x`: "/f", "x": "/f", "/a\r\nb": "/f",
	} {
		if got := localNext(in, "/f"); got != want {
			t.Errorf("localNext(%q) = %q, want %q", in, got, want)
		}
	}
}
