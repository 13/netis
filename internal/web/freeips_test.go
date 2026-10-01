package web

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"netis/internal/store"
	"netis/internal/web/views"
)

// The DHCP pool is two addresses of the subnet, or a last octet each, or
// both blank; anything else is refused with a message.
func TestParseDHCPPool(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/24")
	for _, tc := range []struct {
		start, end         string
		wantStart, wantEnd string
		bad                bool
	}{
		{"", "", "", "", false},
		{"10.0.0.100", "10.0.0.199", "10.0.0.100", "10.0.0.199", false},
		{"100", " 199 ", "10.0.0.100", "10.0.0.199", false},
		{"10.0.0.5", "10.0.0.5", "10.0.0.5", "10.0.0.5", false},
		{"100", "", "", "", true},
		{"10.0.1.5", "10.0.1.9", "", "", true},
		{"200", "100", "", "", true},
		{"nope", "100", "", "", true},
	} {
		s, e, msg := parseDHCPPool(p, tc.start, tc.end)
		if (msg != "") != tc.bad || s != tc.wantStart || e != tc.wantEnd {
			t.Errorf("parseDHCPPool(%q, %q) = %q, %q, %q", tc.start, tc.end, s, e, msg)
		}
	}
}

// The subnet form saves the pool, and refuses one outside the subnet.
func TestSubnetFormDHCPPool(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	form := url.Values{"cidr": {"192.168.1.0/24"}, "name": {"main"}, "kind": {"lan"}, "scan_interval_sec": {"120"},
		"dhcp_start": {"100"}, "dhcp_end": {"192.168.1.199"}}
	if rec := authedPost(t, srv, st, "/settings/subnets", form); rec.Code != 303 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	subnets, _ := st.ListSubnets(t.Context())
	if len(subnets) != 1 || subnets[0].DHCPStart != "192.168.1.100" || subnets[0].DHCPEnd != "192.168.1.199" {
		t.Fatalf("subnets=%+v", subnets)
	}
	form.Set("cidr", "192.168.2.0/24")
	form.Set("dhcp_end", "192.168.1.199")
	if rec := authedPost(t, srv, st, "/settings/subnets", form); rec.Code != 400 {
		t.Fatalf("pool outside the subnet: code=%d", rec.Code)
	}
}

func TestFreeRanges(t *testing.T) {
	cells := []views.GridCell{
		{IP: "10.0.0.0", State: "edge"},
		{IP: "10.0.0.1", State: "online"},
		{IP: "10.0.0.2", State: "free"},
		{IP: "10.0.0.3", State: "free"},
		{IP: "10.0.0.4", State: "pool"},
		{IP: "10.0.0.5", State: "free"},
		{IP: "10.0.0.6", State: "free"},
		{IP: "10.0.0.7", State: "free"},
	}
	got := freeRanges(cells)
	want := []views.FreeRange{{Start: "10.0.0.2", End: "10.0.0.3", N: 2}, {Start: "10.0.0.5", End: "10.0.0.7", N: 3}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("freeRanges = %+v, want %+v", got, want)
	}
}

// With a DHCP pool set, its free addresses are drawn apart, and the next
// free address, the free ranges and the counts leave them out. Held ports
// carry their hover card details in the grid's JSON.
func TestGridDHCPPool(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	snID, _ := st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/28", Name: "lab", Kind: "lan", ScanIntervalSec: 120,
		DHCPStart: "10.0.0.1", DHCPEnd: "10.0.0.4"})
	devID, _ := st.CreateDevice(t.Context(), store.Device{Name: "gw", Kind: "router", Source: "manual"})
	mac := "aa:bb:cc:dd:ee:01"
	ifID, _ := st.AddIface(t.Context(), devID, &mac, nil)
	st.AssignIP(t.Context(), ifID, snID, "10.0.0.5", "static")

	rec := authedGet(t, srv, st, "/subnets/1")
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if n := strings.Count(body, `class="port pool`); n != 5 {
		t.Errorf("pool ports=%d, want 5 (four in the panel, one in the legend)", n)
	}
	for _, want := range []string{
		`data-next-free="10.0.0.6"`,  // .1–.4 are the pool, .5 is held
		`data-copy-text="10.0.0.6"`,  // and it can be copied
		`data-range="10.0.0.6"`,      // the one free run…
		`data-range-end="10.0.0.14"`, // …up to the last host
		`<b>9</b> free`, `<b>4</b> free in DHCP pool`,
		`id="grid-data"`, `"10.0.0.5":{"n":"gw","m":"aa:bb:cc:dd:ee:01"`,
		`id="port-card"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("subnet page missing %s", want)
		}
	}

	dash := authedGet(t, srv, st, "/").Body.String()
	for _, want := range []string{`href="/subnets/1?ip=10.0.0.6"`, `data-copy-text="10.0.0.6"`, `<b>4</b> in DHCP pool`} {
		if !strings.Contains(dash, want) {
			t.Errorf("dashboard missing %s", want)
		}
	}
}

// "free" in the palette lists each subnet's next free address; words after
// it narrow the subnets.
func TestSearchFree(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.0.0.0/24", Name: "lab", Kind: "lan", ScanIntervalSec: 120,
		DHCPStart: "10.0.0.1", DHCPEnd: "10.0.0.99"})
	st.CreateSubnet(t.Context(), store.Subnet{CIDR: "10.1.0.0/24", Name: "iot", Kind: "lan", ScanIntervalSec: 120})

	search := func(q string) searchResult {
		t.Helper()
		var res searchResult
		if err := json.Unmarshal(authedGet(t, srv, st, "/api/search?q="+url.QueryEscape(q)).Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := search("free")
	if len(res.Free) != 2 || res.Free[0].IP != "10.0.0.100" || res.Free[0].Free != 155 || res.Free[1].IP != "10.1.0.1" {
		t.Fatalf("free = %+v", res.Free)
	}
	if res := search("Free iot"); len(res.Free) != 1 || res.Free[0].Name != "iot" {
		t.Fatalf("free iot = %+v", res.Free)
	}
	if res := search("freezer"); len(res.Free) != 0 {
		t.Fatalf("freezer is not a free query: %+v", res.Free)
	}
}

// A pool typed into the form is the user's; one an integration read stays
// the integration's while the form is saved unchanged, becomes the user's
// once edited, and is handed back to the integrations when cleared. The
// Network page says which integration a pool came from.
func TestSubnetFormDHCPPoolSource(t *testing.T) {
	srv, st := testServer(t)
	st.SetSetting(t.Context(), "onboarded", "1")
	ctx := t.Context()
	form := url.Values{"cidr": {"192.168.1.0/24"}, "name": {"main"}, "kind": {"lan"}, "scan_interval_sec": {"120"},
		"dhcp_start": {"100"}, "dhcp_end": {"199"}}
	if rec := authedPost(t, srv, st, "/settings/subnets", form); rec.Code != 303 {
		t.Fatalf("create: code=%d body=%s", rec.Code, rec.Body.String())
	}
	subnets, _ := st.ListSubnets(ctx)
	if len(subnets) != 1 || subnets[0].DHCPPoolSource != "user" {
		t.Fatalf("typed pool source = %+v", subnets)
	}
	id := subnets[0].ID
	path := "/settings/subnets/" + strconv.FormatInt(id, 10)
	source := func() store.Subnet {
		t.Helper()
		sn, err := st.GetSubnet(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return sn
	}

	// Hand the pool to an integration, as a sync would on an unowned pool.
	sn := source()
	sn.DHCPStart, sn.DHCPEnd, sn.DHCPPoolSource = "", "", ""
	st.UpdateSubnet(ctx, sn)
	if ok, err := st.SetDHCPPoolFrom(ctx, id, "192.168.1.100", "192.168.1.199", "opnsense"); !ok || err != nil {
		t.Fatalf("SetDHCPPoolFrom: %v %v", ok, err)
	}
	if body := authedGet(t, srv, st, "/settings/network").Body.String(); !strings.Contains(body, "From OPNsense") {
		t.Error("the Network page does not say where the pool came from")
	}

	form.Set("name", "renamed") // pool unchanged, in octet shorthand
	if rec := authedPost(t, srv, st, path, form); rec.Code != 303 {
		t.Fatalf("update: code=%d", rec.Code)
	}
	if sn := source(); sn.DHCPPoolSource != "opnsense" || sn.Name != "renamed" {
		t.Fatalf("unchanged pool lost its source: %+v", sn)
	}

	form.Set("dhcp_end", "150")
	authedPost(t, srv, st, path, form)
	if sn := source(); sn.DHCPPoolSource != "user" || sn.DHCPEnd != "192.168.1.150" {
		t.Fatalf("edited pool: %+v", sn)
	}

	form.Set("dhcp_start", "")
	form.Set("dhcp_end", "")
	authedPost(t, srv, st, path, form)
	if sn := source(); sn.DHCPPoolSource != "" || sn.DHCPStart != "" {
		t.Fatalf("cleared pool: %+v", sn)
	}
}
