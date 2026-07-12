package wireguard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type fakeRunner struct{ out []byte }

func (f *fakeRunner) Run(ctx context.Context, cmd string) ([]byte, error) { return f.out, nil }

func TestSyncCreatesPeersAndStatus(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	st.CreateSubnet(store.Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})

	fresh := time.Now().Unix()
	dump := "priv\tpub\t51820\toff\n" +
		fmt.Sprintf("peerA=\t(none)\t1.2.3.4:51820\t10.6.0.2/32\t%d\t1\t1\toff\n", fresh) +
		"peerB=\t(none)\t(none)\t10.6.0.3/32\t0\t0\t0\toff\n"

	sync := NewSync(st, &fakeRunner{out: []byte(dump)}, events.NewService(st, events.NewBroker()), "wg0")
	for i := 0; i < 2; i++ { // idempotent
		if _, err := sync.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.ListDevices()
	if len(rows) != 2 {
		t.Fatalf("devices=%+v", rows)
	}
	byName := map[string]store.DeviceRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	a := byName["10.6.0.2"]
	if a.Kind != "wg-peer" || a.Source != "wireguard" || !a.Online {
		t.Fatalf("peerA=%+v", a)
	}
	if len(a.IPs) != 1 || a.IPs[0].IP != "10.6.0.2" {
		t.Fatalf("peerA ips=%v", a.IPs)
	}
	if byName["10.6.0.3"].Online {
		t.Fatal("peerB (no handshake) must be offline")
	}
}

type blockingRunner struct{}

func (b *blockingRunner) Run(ctx context.Context, cmd string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunOnceRespectsContextCancellation(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()

	sync := NewSync(st, &blockingRunner{}, events.NewService(st, events.NewBroker()), "wg0")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := sync.RunOnce(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected non-nil error from cancelled context, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("RunOnce did not return within 1s of context cancellation")
	}
}

func TestWireguardRunOnceStats(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	st.CreateSubnet(store.Subnet{CIDR: "10.6.0.0/24", Kind: "wireguard", ScanIntervalSec: 120})
	fresh := time.Now().Unix()
	dump := "priv\tpub\t51820\toff\n" +
		fmt.Sprintf("peerA=\t(none)\t1.2.3.4:51820\t10.6.0.2/32\t%d\t1\t1\toff\n", fresh) +
		"peerB=\t(none)\t(none)\t10.6.0.3/32\t0\t0\t0\toff\n"
	sync := NewSync(st, &fakeRunner{out: []byte(dump)}, events.NewService(st, events.NewBroker()), "wg0")
	stats, err := sync.RunOnce(context.Background())
	if err != nil || stats.Peers != 2 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
