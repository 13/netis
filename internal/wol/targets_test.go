package wol

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestDirectedBroadcast(t *testing.T) {
	cases := []struct {
		prefix string
		want   string // "" = no broadcast address
	}{
		{"192.168.1.0/24", "192.168.1.255"},
		{"10.0.20.0/23", "10.0.21.255"},
		{"172.16.0.0/16", "172.16.255.255"},
		{"10.0.0.0/8", "10.255.255.255"},
		{"192.168.1.77/24", "192.168.1.255"}, // host bits in the prefix are ignored
		{"10.1.2.0/30", "10.1.2.3"},
		{"10.1.2.0/31", ""},
		{"10.1.2.3/32", ""},
		{"fd00::/64", ""},
		{"0.0.0.0/0", "255.255.255.255"},
	}
	for _, c := range cases {
		got, ok := DirectedBroadcast(netip.MustParsePrefix(c.prefix))
		if c.want == "" {
			if ok {
				t.Errorf("%s: got %s, want none", c.prefix, got)
			}
			continue
		}
		if !ok || got.String() != c.want {
			t.Errorf("%s: got %v ok=%v, want %s", c.prefix, got, ok, c.want)
		}
	}
	if _, ok := DirectedBroadcast(netip.Prefix{}); ok {
		t.Error("invalid prefix produced a broadcast address")
	}
}

func TestTargets(t *testing.T) {
	got := Targets([]netip.Prefix{
		netip.MustParsePrefix("10.0.20.0/24"),
		netip.MustParsePrefix("fd00::/64"),
		netip.MustParsePrefix("10.0.20.0/24"), // same subnet twice
		netip.MustParsePrefix("192.168.1.0/24"),
		netip.MustParsePrefix("10.9.9.9/32"),
	})
	want := []string{"10.0.20.255:9", "192.168.1.255:9", "255.255.255.255:9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Targets = %v, want %v", got, want)
	}
	if got := Targets(nil); !reflect.DeepEqual(got, []string{BroadcastAddr}) {
		t.Errorf("Targets(nil) = %v, want only the limited broadcast", got)
	}
}

func TestSendAll(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:ff"
	var calls []string
	send := func(m, addr string) error {
		calls = append(calls, addr)
		if m != mac {
			t.Errorf("sent for mac %q", m)
		}
		if strings.HasPrefix(addr, "10.") {
			return errors.New("no route")
		}
		return nil
	}

	sent, err := SendAll(mac, []string{"10.0.20.255:9", "192.168.1.255:9", BroadcastAddr}, send)
	if err != nil {
		t.Fatalf("partial failure is not an error: %v", err)
	}
	if !reflect.DeepEqual(sent, []string{"192.168.1.255:9", BroadcastAddr}) {
		t.Errorf("sent = %v", sent)
	}
	if len(calls) != 3 {
		t.Errorf("tried %v, want every address", calls)
	}

	if _, err := SendAll(mac, []string{"10.0.0.255:9", "10.1.0.255:9"}, send); err == nil ||
		!strings.Contains(err.Error(), "10.1.0.255:9") {
		t.Errorf("all failed: err = %v, want it to name the addresses", err)
	}

	calls = nil
	if _, err := SendAll("garbage", []string{BroadcastAddr}, send); err == nil || len(calls) != 0 {
		t.Errorf("bad mac: err=%v calls=%v, want an error and nothing sent", err, calls)
	}
}

func TestHostsOf(t *testing.T) {
	if got := HostsOf([]string{"10.0.20.255:9", BroadcastAddr}); got != "10.0.20.255, 255.255.255.255" {
		t.Errorf("HostsOf = %q", got)
	}
}
