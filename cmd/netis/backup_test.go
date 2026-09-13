package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"netis/internal/store"
)

// The backup must be a usable database, not just a file that exists: the point
// is that it can be opened and read back.
func TestBackupProducesAReadableDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := dir + "/live.db"

	st, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	devID, err := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSubnet(ctx, store.Subnet{CIDR: "10.4.0.0/24", Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 60}); err != nil {
		t.Fatal(err)
	}
	// Left open on purpose: backing up a live database is the whole point.

	dest := dir + "/backup.db"
	if err := runBackup(ctx, []string{"-from", src, "-to", dest}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("backup file is empty")
	}

	restored, err := store.Open(dest)
	if err != nil {
		t.Fatalf("backup is not a usable database: %v", err)
	}
	defer restored.Close()
	d, err := restored.GetDevice(ctx, devID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "nas" {
		t.Errorf("restored device = %+v", d)
	}
	subnets, err := restored.ListSubnets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(subnets) != 1 || subnets[0].CIDR != "10.4.0.0/24" {
		t.Errorf("restored subnets = %+v", subnets)
	}
}

func TestBackupRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/live.db"
	st, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	dest := dir + "/taken.db"
	if err := os.WriteFile(dest, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = runBackup(context.Background(), []string{"-from", src, "-to", dest})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want a refusal to overwrite", err)
	}
	// The existing file is untouched.
	b, _ := os.ReadFile(dest)
	if string(b) != "precious" {
		t.Error("the existing file was overwritten")
	}
}

// pg_dump already does this properly, so the command says so rather than
// producing something worse.
func TestBackupRedirectsPostgresToPgDump(t *testing.T) {
	err := runBackup(context.Background(),
		[]string{"-from", "postgres://u:p@h/db", "-to", t.TempDir() + "/x.db"})
	if err == nil || !strings.Contains(err.Error(), "pg_dump") {
		t.Fatalf("err = %v, want it to point at pg_dump", err)
	}
}

func TestBackupRequiresDestination(t *testing.T) {
	if err := runBackup(context.Background(), []string{"-from", "x.db"}); err == nil {
		t.Fatal("-to is required")
	}
}
