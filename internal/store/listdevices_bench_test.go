package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// BenchmarkListDevices exists because ListDevices backs the dashboard, the
// device list and the subnet grid, and it used to issue four queries per
// device. Run it against Postgres (NETIS_TEST_PG_DSN) to see the shape that
// matters: per-row round trips are cheap on a local file and expensive on a
// server.
func BenchmarkListDevices(b *testing.B) {
	// Postgres runs get their own schema; repeated runs would otherwise collide
	// on the seed data left by the previous one.
	var s *Store
	if dsn := os.Getenv("NETIS_TEST_PG_DSN"); dsn != "" {
		s = openPGSchema(b, dsn)
	} else {
		var err error
		if s, err = Open(":memory:"); err != nil {
			b.Skip(err)
		}
		b.Cleanup(func() { s.Close() })
	}
	ctx := context.Background()

	snID, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.9.0.0/16", Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 60})
	if err != nil {
		b.Fatal(err)
	}
	const n = 200
	for i := 0; i < n; i++ {
		d, err := s.CreateDevice(ctx, Device{
			Name: fmt.Sprintf("bench-%03d", i), Kind: "other", Source: "manual"})
		if err != nil {
			b.Fatal(err)
		}
		mac := fmt.Sprintf("0a:00:00:00:%02x:%02x", i/256, i%256)
		f, err := s.AddIface(ctx, d, &mac, nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := s.AssignIP(ctx, f, snID, fmt.Sprintf("10.9.%d.%d", i/256, i%256), "dhcp"); err != nil {
			b.Fatal(err)
		}
		if _, err := s.MarkSeen(ctx, f, 1, time.Now()); err != nil {
			b.Fatal(err)
		}
		if err := s.SetDeviceTags(ctx, d, []string{"bench"}); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := s.ListDevices(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) < n {
			b.Fatalf("got %d rows, want at least %d", len(rows), n)
		}
	}
}
