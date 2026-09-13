package scan

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

// blockingSweeper records which CIDRs are being swept and holds each sweep
// until release is closed, so a test can observe concurrency directly.
type blockingSweeper struct {
	mu      sync.Mutex
	entered map[string]int
	release chan struct{}
}

func newBlockingSweeper() *blockingSweeper {
	return &blockingSweeper{
		entered: map[string]int{},
		release: make(chan struct{}),
	}
}

func (b *blockingSweeper) Sweep(ctx context.Context, cidr string) ([]Result, error) {
	b.mu.Lock()
	b.entered[cidr]++
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil, nil
}

func (b *blockingSweeper) count(cidr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.entered[cidr]
}

// sweepsStarted is the total number of sweeps that have entered Sweep. While
// release is still open none of them have returned, so it doubles as the
// number in flight.
func (b *blockingSweeper) sweepsStarted() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, c := range b.entered {
		n += c
	}
	return n
}

func testScheduler(t *testing.T, cidrs ...string) (*Scheduler, *store.Store, *blockingSweeper, []int64) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := newBlockingSweeper()
	e := &Engine{
		Store:   st,
		Events:  events.NewService(st, events.NewBroker()),
		Broker:  events.NewBroker(),
		Sweeper: b,
		ARP:     func() (map[string]string, error) { return nil, nil },
		Resolve: func(ctx context.Context, ip string) string { return "" },
	}
	var ids []int64
	for _, cidr := range cidrs {
		id, err := st.CreateSubnet(t.Context(), store.Subnet{
			CIDR: cidr, Kind: "lan", ScanEnabled: true, ScanIntervalSec: 120,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return NewScheduler(e, st), st, b, ids
}

// waitFor polls cond until it holds, so tests don't depend on sleep lengths.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A slow sweep of one subnet used to hold the scheduler's only goroutine, so
// every other subnet waited behind it however long it took.
func TestSubnetsScanInParallel(t *testing.T) {
	sched, _, b, ids := testScheduler(t, "10.0.0.0/24", "10.0.1.0/24")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	for _, id := range ids {
		sched.Trigger(id)
	}
	waitFor(t, "both subnets to be sweeping at once", func() bool {
		return b.count("10.0.0.0/24") == 1 && b.count("10.0.1.0/24") == 1
	})
	close(b.release)
	sched.Wait()
}

// Trigger is wired to a button an impatient user can click repeatedly, and the
// periodic tick fires every 15s regardless. A second scan of a subnet already
// being swept would double every write the sweep makes.
func TestSubnetNotScannedTwiceAtOnce(t *testing.T) {
	sched, _, b, ids := testScheduler(t, "10.0.0.0/24")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	sched.Trigger(ids[0])
	waitFor(t, "first sweep to start", func() bool { return b.count("10.0.0.0/24") == 1 })
	for i := 0; i < 5; i++ {
		sched.Trigger(ids[0])
	}
	// Give the scheduler room to (wrongly) start a second sweep.
	time.Sleep(50 * time.Millisecond)
	if got := b.count("10.0.0.0/24"); got != 1 {
		t.Fatalf("sweeps in flight = %d, want 1", got)
	}
	close(b.release)
	sched.Wait()
	// Once the first sweep is done the subnet is scannable again.
	sched.Trigger(ids[0])
	waitFor(t, "a later sweep to run", func() bool { return b.count("10.0.0.0/24") == 2 })
	sched.Wait()
}

// Parallel scans must stay bounded: each sweep opens its own fan-out of
// probes, so an unbounded number of subnets scanning at once would multiply
// out into thousands of in-flight packets.
func TestParallelScansAreBounded(t *testing.T) {
	cidrs := make([]string, 0, maxParallelScans+3)
	for i := 0; i < maxParallelScans+3; i++ {
		cidrs = append(cidrs, fmt.Sprintf("10.0.%d.0/24", i))
	}
	sched, _, b, ids := testScheduler(t, cidrs...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	for _, id := range ids {
		sched.Trigger(id)
	}
	waitFor(t, "the concurrency limit to fill", func() bool {
		return b.sweepsStarted() == maxParallelScans
	})
	// Every sweep is still blocked, so the count must not creep past the limit.
	time.Sleep(50 * time.Millisecond)
	if got := b.sweepsStarted(); got != maxParallelScans {
		t.Fatalf("concurrent sweeps = %d, want %d", got, maxParallelScans)
	}
	close(b.release)
	sched.Wait()
}
