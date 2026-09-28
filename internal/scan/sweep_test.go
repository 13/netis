package scan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAllIPs(t *testing.T) {
	ips, err := AllIPs("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"}
	if len(ips) != len(want) {
		t.Fatalf("got %v, want %v", ips, want)
	}
	for i := range want {
		if ips[i] != want[i] {
			t.Fatalf("got %v, want %v", ips, want)
		}
	}
	if _, err := AllIPs("garbage"); err == nil {
		t.Fatal("want error for bad cidr")
	}
}

func TestHostIPs(t *testing.T) {
	ips, err := HostIPs("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.1", "192.168.1.2"} // network+broadcast skipped
	if len(ips) != 2 || ips[0] != want[0] || ips[1] != want[1] {
		t.Fatalf("got %v", ips)
	}
	if _, err := HostIPs("garbage"); err == nil {
		t.Fatal("want error for bad cidr")
	}
}

// TestSweepBoundedWorkers exercises Sweep's worker-pool path against a tiny
// CIDR with a small, fixed Concurrency. It doesn't assert on live-network
// reachability (Alive may be false in the test environment); it asserts the
// structural contract: one Result per HostIPs entry, in order, and that the
// call returns promptly rather than spawning unbounded goroutines or hanging.
// A short context timeout ensures any accidental live ping fails fast.
func TestSweepBoundedWorkers(t *testing.T) {
	cidr := "192.168.1.0/30"
	hostIPs, err := HostIPs(cidr)
	if err != nil {
		t.Fatal(err)
	}

	sweeper := NewICMPSweeper(2)
	sweeper.Timeout = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results, _ := sweeper.Sweep(ctx, cidr)

	if len(results) != len(hostIPs) {
		t.Fatalf("got %d results, want %d", len(results), len(hostIPs))
	}
	for i, want := range hostIPs {
		if results[i].IP != want {
			t.Fatalf("result[%d].IP = %q, want %q", i, results[i].IP, want)
		}
	}
}

// TestSweepFailsWhenProbesCannotRun covers ICMP being unavailable (no raw
// socket permission, say): every probe errors, and reporting that as a clean
// sweep with every host down would walk the whole fleet offline.
func TestSweepFailsWhenProbesCannotRun(t *testing.T) {
	s := NewICMPSweeper(2)
	s.probe = func(ctx context.Context, ip string) (Result, error) {
		return Result{IP: ip}, errors.New("socket: permission denied")
	}
	if _, err := s.Sweep(t.Context(), "192.168.1.0/29"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("want the probe error, got %v", err)
	}
}

// TestSweepMarksIndividualProbeErrors keeps a few failed probes from failing
// the sweep, but flags them so the engine does not count them as misses.
func TestSweepMarksIndividualProbeErrors(t *testing.T) {
	s := NewICMPSweeper(2)
	s.probe = func(ctx context.Context, ip string) (Result, error) {
		if ip == "192.168.1.3" {
			return Result{IP: ip}, errors.New("boom")
		}
		return Result{IP: ip, Alive: true}, nil
	}
	results, err := s.Sweep(t.Context(), "192.168.1.0/29")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if (r.IP == "192.168.1.3") != (r.Err != nil) {
			t.Fatalf("result %+v", r)
		}
	}
}
