package wireguard

import (
	"context"
	"sort"
	"strings"
	"testing"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

// swapRunner returns whatever out currently holds.
type swapRunner struct{ out string }

func (r *swapRunner) Run(ctx context.Context, cmd string) ([]byte, error) { return []byte(r.out), nil }

const wgIfaceLine = "priv\tpub\t51820\toff\n"

func peerLine(key, allowed string) string {
	return key + "\t(none)\t(none)\t" + allowed + "\t0\t0\t0\toff\n"
}

func countEvents(t *testing.T, st *store.Store, typ string) int {
	t.Helper()
	evs, err := st.ListEvents(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// A peer removed from the server is marked missing with one event (and kept),
// an empty peer list marks nothing, and a returning peer is cleared.
func TestSyncFlagsPeersMissingUpstream(t *testing.T) {
	storetest.EachDialect(t, testSyncFlagsPeersMissingUpstream)
}

func testSyncFlagsPeersMissingUpstream(t *testing.T, st *store.Store) {
	r := &swapRunner{out: wgIfaceLine + peerLine("A=", "10.6.0.2/32") + peerLine("B=", "10.6.0.3/32")}
	sync := NewSync(st, r, events.NewService(st, events.NewBroker()), "wg0")
	run := func() {
		t.Helper()
		if _, err := sync.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	missing := func(key string) bool {
		t.Helper()
		id, ok, err := st.FindDeviceByWGPubKey(t.Context(), key)
		if err != nil || !ok {
			t.Fatalf("peer %s: ok=%v err=%v", key, ok, err)
		}
		d, _ := st.GetDevice(t.Context(), id)
		return d.UpstreamMissingSince != nil
	}

	run()
	r.out = wgIfaceLine + peerLine("A=", "10.6.0.2/32")
	run()
	run()
	if !missing("B=") || missing("A=") {
		t.Fatalf("after removal: A missing=%v B missing=%v, want only B", missing("A="), missing("B="))
	}
	if n := countEvents(t, st, "device_missing"); n != 1 {
		t.Fatalf("device_missing events = %d, want 1", n)
	}

	r.out = wgIfaceLine // no peers at all: cannot tell
	run()
	if missing("A=") {
		t.Fatal("an empty peer list marked a peer missing")
	}

	r.out = wgIfaceLine + peerLine("A=", "10.6.0.2/32") + peerLine("B=", "10.6.0.3/32")
	run()
	if missing("B=") {
		t.Fatal("returned peer still marked missing")
	}
	if n := countEvents(t, st, "device_returned"); n != 1 {
		t.Fatalf("device_returned events = %d, want 1", n)
	}
	evs, _ := st.ListEvents(t.Context(), 100)
	for _, e := range evs {
		if !strings.HasPrefix(e.Details, "WireGuard peer ") {
			t.Errorf("%s details = %q, want sentence case", e.Type, e.Details)
		}
	}
	if rows, _ := st.ListDevices(t.Context()); len(rows) != 2 {
		t.Fatalf("devices = %d, want 2 (nothing deleted)", len(rows))
	}
}

// An existing peer follows its AllowedIPs: new addresses are added, the ones
// the sync added and the server dropped are removed, and an address the user
// recorded stays.
func TestSyncFollowsAllowedIPs(t *testing.T) {
	storetest.EachDialect(t, testSyncFollowsAllowedIPs)
}

func testSyncFollowsAllowedIPs(t *testing.T, st *store.Store) {
	ctx := t.Context()
	sn, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})
	lan, _ := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.0.0.0/24", Kind: "lan", ScanIntervalSec: 120})

	r := &swapRunner{out: wgIfaceLine + peerLine("A=", "10.6.0.2/32")}
	sync := NewSync(st, r, events.NewService(st, events.NewBroker()), "wg0")
	if _, err := sync.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	devID, _, _ := st.FindDeviceByWGPubKey(ctx, "A=")
	ifaces, _ := st.ListIfaces(ctx, devID)
	ifc := ifaces[0].ID
	// The user's own addresses on the peer: one in the WireGuard subnet, one
	// on the LAN.
	st.AssignIP(ctx, ifc, sn, "10.6.0.99", "static")
	st.AssignIP(ctx, ifc, lan, "10.0.0.5", "static")

	ips := func() string {
		rows, _ := st.ListIPs(ctx, ifc)
		var out []string
		for _, r := range rows {
			out = append(out, r.IP)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}

	r.out = wgIfaceLine + peerLine("A=", "10.6.0.4/32,10.6.0.5/32,192.168.9.0/24")
	if _, err := sync.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := ips(), "10.0.0.5,10.6.0.4,10.6.0.5,10.6.0.99"; got != want {
		t.Fatalf("IPs after AllowedIPs change = %s, want %s", got, want)
	}

	r.out = wgIfaceLine + peerLine("A=", "(none)")
	if _, err := sync.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := ips(), "10.0.0.5,10.6.0.99"; got != want {
		t.Fatalf("IPs after AllowedIPs emptied = %s, want %s", got, want)
	}
}
