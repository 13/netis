package autofill

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

func fakeProber(calls *atomic.Int32, out map[string][]store.Hint) SubnetProber {
	return func(ctx context.Context, ifi *net.Interface) (map[string][]store.Hint, error) {
		if calls != nil {
			calls.Add(1)
		}
		// Hand out a copy: probeSubnet stamps the hints it stores.
		cp := make(map[string][]store.Hint, len(out))
		for ip, hs := range out {
			cp[ip] = append([]store.Hint(nil), hs...)
		}
		return cp, nil
	}
}

func printerAnswers() map[string][]store.Hint {
	return map[string][]store.Hint{
		"10.0.0.5":  {{Field: "kind", Value: "printer", Confidence: 90}},
		"10.0.0.99": {{Field: "kind", Value: "tv", Confidence: 90}},
	}
}

func sourceHints(t *testing.T, st *store.Store, id int64, source string) []store.Hint {
	t.Helper()
	all, err := st.ListHints(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Hint
	for _, h := range all {
		if h.Source == source {
			out = append(out, h)
		}
	}
	return out
}

func TestProbeSubnetStoresHintsAndFills(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id := discovered(t, st, "dev5", "02:00:00:00:00:05", "", "10.0.0.5")
		subs, _ := st.ListSubnets(ctx)
		svc := New(st)
		svc.probers = map[string]SubnetProber{"mdns": fakeProber(nil, printerAnswers())}
		if err := svc.probeSubnet(ctx, subs[0], nil); err != nil {
			t.Fatal(err)
		}
		d, _ := st.GetDevice(ctx, id)
		if d.Kind != "printer" {
			t.Fatalf("kind = %q", d.Kind)
		}
		hs := sourceHints(t, st, id, "mdns")
		if len(hs) != 1 || hs[0].Value != "printer" || hs[0].DeviceID != id {
			t.Fatalf("mdns hints = %+v", hs)
		}
		// 10.0.0.99 has no device: nothing stored for it anywhere.
		ids, _ := st.DeviceIDs(ctx)
		for _, other := range ids {
			for _, h := range sourceHints(t, st, other, "mdns") {
				if h.Value == "tv" {
					t.Fatalf("hint stored for unknown address: %+v", h)
				}
			}
		}
	})
}

func TestProbeSubnetSkipsUnchanged(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx := t.Context()
		id := discovered(t, st, "dev5", "02:00:00:00:00:05", "", "10.0.0.5")
		subs, _ := st.ListSubnets(ctx)
		svc := New(st)
		svc.probers = map[string]SubnetProber{"mdns": fakeProber(nil, printerAnswers())}
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		svc.now = func() time.Time { return t0 }
		if err := svc.probeSubnet(ctx, subs[0], nil); err != nil {
			t.Fatal(err)
		}
		svc.now = func() time.Time { return t0.Add(time.Hour) }
		if err := svc.probeSubnet(ctx, subs[0], nil); err != nil {
			t.Fatal(err)
		}
		hs := sourceHints(t, st, id, "mdns")
		if len(hs) != 1 || hs[0].SeenAt != t0.Format(time.RFC3339) {
			t.Fatalf("mdns hints = %+v", hs)
		}
	})
}

func TestProbeLoopRateLimitsAndSkipsRouted(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		discovered(t, st, "dev5", "02:00:00:00:00:05", "", "10.0.0.5")
		subs, _ := st.ListSubnets(ctx)
		routedID, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.9.0.0/24", Kind: "lan"})
		if err != nil {
			t.Fatal(err)
		}
		routed, _ := st.GetSubnet(ctx, routedID)

		var calls atomic.Int32
		svc := New(st)
		svc.probers = map[string]SubnetProber{"mdns": fakeProber(&calls, printerAnswers())}
		svc.iface = func(cidr string) *net.Interface {
			if cidr == "10.0.0.0/24" {
				return &net.Interface{Name: "test"}
			}
			return nil
		}
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		for range 3 {
			svc.Probe(subs[0])
		}
		svc.Probe(routed)
		deadline := time.Now().Add(5 * time.Second)
		for calls.Load() < 1 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		if n := calls.Load(); n != 1 {
			t.Fatalf("prober calls = %d, want 1", n)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Start did not return")
		}
	})
}

func TestProbeOffDoesNothing(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		discovered(t, st, "dev5", "02:00:00:00:00:05", "", "10.0.0.5")
		if err := st.SetSetting(ctx, "autofill_enabled", "off"); err != nil {
			t.Fatal(err)
		}
		subs, _ := st.ListSubnets(ctx)
		var calls atomic.Int32
		passes := make(chan struct{}, 8)
		svc := New(st)
		svc.afterPass = func() { passes <- struct{}{} }
		svc.probers = map[string]SubnetProber{"mdns": fakeProber(&calls, printerAnswers())}
		svc.iface = func(string) *net.Interface { return &net.Interface{Name: "test"} }
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		svc.Probe(subs[0])
		<-passes
		time.Sleep(100 * time.Millisecond)
		// The worker must have taken the request, or this proves nothing.
		if len(svc.probeCh) != 0 {
			t.Fatal("probe request still queued")
		}
		if n := calls.Load(); n != 0 {
			t.Fatalf("prober calls = %d, want 0", n)
		}
		cancel()
		<-done
	})
}

// TestProbeLoopSkipsIPv6 checks that only IPv4 subnets are probed: the mDNS
// and SSDP probes speak IPv4 only.
func TestProbeLoopSkipsIPv6(t *testing.T) {
	storetest.EachDialect(t, func(t *testing.T, st *store.Store) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		discovered(t, st, "dev5", "02:00:00:00:00:05", "", "10.0.0.5")
		subs, _ := st.ListSubnets(ctx)
		v6ID, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "fd00::/112", Kind: "lan"})
		if err != nil {
			t.Fatal(err)
		}
		v6, _ := st.GetSubnet(ctx, v6ID)

		var mu sync.Mutex
		var probed []string
		svc := New(st)
		svc.probers = map[string]SubnetProber{"mdns": func(_ context.Context, ifi *net.Interface) (map[string][]store.Hint, error) {
			mu.Lock()
			probed = append(probed, ifi.Name)
			mu.Unlock()
			return nil, nil
		}}
		svc.iface = func(cidr string) *net.Interface { return &net.Interface{Name: cidr} }
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		// Requests are handled in order: once the IPv4 one is probed, the
		// IPv6 one has been handled.
		svc.Probe(v6)
		svc.Probe(subs[0])
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(probed)
			mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		if len(probed) != 1 || probed[0] != subs[0].CIDR {
			t.Fatalf("probed = %v, want only %s", probed, subs[0].CIDR)
		}
	})
}
