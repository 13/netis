package scan

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/store"
)

// seedOnline runs one sweep in which 10.0.0.9 answers ping, creating the
// device, and sets offline_after=1 so a single miss is visible.
func seedOnline(t *testing.T, e *Engine, st *store.Store, fs *fakeSweeper, sn store.Subnet) {
	t.Helper()
	fs.results = []Result{{IP: "10.0.0.9", Alive: true, RTTms: 1.0}}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(t.Context(), "offline_after", "1"); err != nil {
		t.Fatal(err)
	}
	fs.results = []Result{{IP: "10.0.0.9"}} // swept, no ICMP answer
}

func online(t *testing.T, st *store.Store) bool {
	t.Helper()
	rows, _ := st.ListDevices(t.Context())
	if len(rows) != 1 {
		t.Fatalf("devices=%+v", rows)
	}
	return rows[0].Online
}

// arpSequence returns a fake ARP reader that yields tables in order, repeating
// the last one.
func arpSequence(calls *atomic.Int32, tables ...map[string]string) func() (map[string]string, error) {
	return func() (map[string]string, error) {
		n := int(calls.Add(1)) - 1
		if n >= len(tables) {
			n = len(tables) - 1
		}
		return tables[n], nil
	}
}

// TestARPPresenceKeepsICMPSilentHostOnline covers a known host that stops
// answering ping but still answers ARP: its entry is COMPLETE after the sweep
// and still resolves to the same MAC after the settle wait, so it is present.
func TestARPPresenceKeepsICMPSilentHostOnline(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if !online(t, st) {
		t.Fatal("a host answering ARP must stay online without ICMP")
	}
}

// TestARPPresenceIgnoresStaleEntry covers a host that left: its cached entry
// still reads COMPLETE right after the sweep but fails the kernel's re-probe,
// so it is gone on the re-read and the host takes its miss.
func TestARPPresenceIgnoresStaleEntry(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	var calls atomic.Int32
	e.ARP = arpSequence(&calls, map[string]string{"10.0.0.9": "bc:24:11:00:00:01"}, map[string]string{})
	e.Presence = func(context.Context, string) (float64, bool) { return 0, false }
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if online(t, st) {
		t.Fatal("a stale ARP entry must not keep a host online")
	}
	if calls.Load() != 2 {
		t.Fatalf("ARP read %d times, want a re-read after the settle wait", calls.Load())
	}
}

// TestARPPresenceNeedsSameMAC keeps a re-read that names a different MAC from
// counting: the entry was replaced, not re-confirmed.
func TestARPPresenceNeedsSameMAC(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	var calls atomic.Int32
	e.ARP = arpSequence(&calls, map[string]string{"10.0.0.9": "bc:24:11:00:00:01"},
		map[string]string{"10.0.0.9": "aa:aa:aa:00:00:09"})
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if online(t, st) {
		t.Fatal("an ARP entry whose MAC changed must not confirm presence")
	}
}

// TestTCPPresenceConfirmsWithoutReread covers the TCP fallback: a host that
// answers a connect is present at once, with the connect time as its RTT, and
// no settle wait or ARP re-read is needed.
func TestTCPPresenceConfirmsWithoutReread(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	var calls atomic.Int32
	e.ARP = arpSequence(&calls, map[string]string{"10.0.0.9": "bc:24:11:00:00:01"}, map[string]string{})
	e.ARPSettle = time.Hour // would hang the test if the wait were taken
	var probed []string
	e.Presence = func(_ context.Context, ip string) (float64, bool) {
		probed = append(probed, ip)
		return 3.5, true
	}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if !online(t, st) {
		t.Fatal("a TCP-confirmed host must stay online")
	}
	if len(probed) != 1 || probed[0] != "10.0.0.9" {
		t.Fatalf("probed=%v", probed)
	}
	if calls.Load() != 1 {
		t.Fatalf("ARP read %d times, want no re-read", calls.Load())
	}
}

// TestPresenceOnlyForARPCandidates keeps TCP probes to hosts that answered
// ARP: an address with no entry (a routed subnet, or nobody home) is never
// probed.
func TestPresenceOnlyForARPCandidates(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	e.ARP = func() (map[string]string, error) { return map[string]string{}, nil }
	e.Presence = func(_ context.Context, ip string) (float64, bool) {
		t.Errorf("probed %s without an ARP entry", ip)
		return 0, false
	}
	fs.results = []Result{{IP: "10.0.0.9"}, {IP: "10.0.0.10"}}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
}

// TestPresenceDiscoversICMPSilentDevice covers a host that has never answered
// ping: presence alone is enough to discover it.
func TestPresenceDiscoversICMPSilentDevice(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	fs.results = []Result{{IP: "10.0.0.9"}}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if !online(t, st) {
		t.Fatal("an ARP-present host should be discovered online")
	}
	if fs.results[0].Alive {
		t.Fatal("presence must not modify the sweeper's results")
	}
}

// TestPresenceFallbackSettingOff turns the fallback off: ICMP alone decides.
func TestPresenceFallbackSettingOff(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	if err := st.SetSetting(t.Context(), "presence_fallback", "off"); err != nil {
		t.Fatal(err)
	}
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if online(t, st) {
		t.Fatal("with presence_fallback=off a host that misses ICMP takes its miss")
	}
}

// TestARPSettleWaitsFromSweepEnd checks the re-read happens no sooner than
// ARPSettle after the sweep.
func TestARPSettleWaitsFromSweepEnd(t *testing.T) {
	e, st, fs, snID := testEngine(t)
	sn, _ := st.GetSubnet(t.Context(), snID)
	seedOnline(t, e, st, fs, sn)
	e.ARPSettle = 60 * time.Millisecond
	start := time.Now()
	if err := e.RunSubnet(context.Background(), sn); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < e.ARPSettle {
		t.Fatalf("sweep took %v, want at least the %v settle wait", d, e.ARPSettle)
	}
}

// TestTCPProbe checks the real prober: an accepting port and a refusing port
// both prove the host is up; silence does not.
func TestTCPProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	open := ln.Addr().(*net.TCPAddr).Port

	// A port that was just free is refused on loopback.
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := tmp.Addr().(*net.TCPAddr).Port
	tmp.Close()

	if _, ok := tcpProbe(t.Context(), "127.0.0.1", []int{open}, time.Second); !ok {
		t.Error("an accepting port must confirm presence")
	}
	if _, ok := tcpProbe(t.Context(), "127.0.0.1", []int{closed}, time.Second); !ok {
		t.Error("a refused connection (RST) must confirm presence")
	}
	// 192.0.2.0/24 is TEST-NET-1: nothing answers there.
	start := time.Now()
	if _, ok := tcpProbe(t.Context(), "192.0.2.1", []int{22, 80}, 150*time.Millisecond); ok {
		t.Error("an unanswered address must not confirm presence")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("probe ignored its timeout: %v", d)
	}
}
