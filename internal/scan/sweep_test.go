package scan

import (
	"context"
	"testing"
	"time"
)

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
