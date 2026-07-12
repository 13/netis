package netdetect

import (
	"net/netip"
	"testing"
)

func mustPrefix(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic(err)
	}
	return p
}

func TestDetectFromFiltersAndDedupes(t *testing.T) {
	lister := func() ([]ifaceInfo, error) {
		return []ifaceInfo{
			{Name: "eth0", Up: true, Addrs: []netip.Prefix{mustPrefix("192.168.1.50/24")}},
			{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Prefix{mustPrefix("127.0.0.1/8")}},
			{Name: "eth1", Up: false, Addrs: []netip.Prefix{mustPrefix("10.1.0.5/24")}},     // down → skip
			{Name: "docker0", Up: true, Addrs: []netip.Prefix{mustPrefix("172.17.0.1/16")}}, // virtual prefix → skip
			{Name: "veth123", Up: true, Addrs: []netip.Prefix{mustPrefix("10.9.0.1/24")}},   // virtual prefix → skip
			{Name: "wlan0", Up: true, Addrs: []netip.Prefix{
				mustPrefix("169.254.5.5/16"), // link-local → skip
				mustPrefix("fe80::1/64"),     // IPv6 → skip
				mustPrefix("10.0.0.2/24"),    // kept
			}},
			{Name: "eth2", Up: true, Addrs: []netip.Prefix{mustPrefix("192.168.1.9/24")}}, // dup CIDR of eth0 → deduped
			{Name: "ptp0", Up: true, Addrs: []netip.Prefix{mustPrefix("203.0.113.7/32")}}, // /32 host route → skip
		}, nil
	}
	got, err := detectFrom(lister)
	if err != nil {
		t.Fatal(err)
	}
	// Expect exactly 10.0.0.0/24 and 192.168.1.0/24, sorted by CIDR.
	if len(got) != 2 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].CIDR != "10.0.0.0/24" || got[0].Iface != "wlan0" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].CIDR != "192.168.1.0/24" || got[1].Iface != "eth0" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestDetectSubnetsRunsAgainstHost(t *testing.T) {
	// Smoke: the real host lister must not error (result contents are
	// environment-dependent, so only the error is asserted).
	if _, err := DetectSubnets(); err != nil {
		t.Fatalf("DetectSubnets on host: %v", err)
	}
}
