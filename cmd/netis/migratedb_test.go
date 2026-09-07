package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"netis/internal/store"
)

func TestRedactDSN(t *testing.T) {
	cases := map[string]string{
		"postgres://u:secret@h:5432/db":  "postgres://u:***@h:5432/db",
		"postgres://u@h:5432/db":         "postgres://u@h:5432/db",
		"postgres://h/db":                "postgres://h/db",
		"postgres://u:s@h/db?sslmode=on": "postgres://u:***@h/db?sslmode=on",
	}
	for in, want := range cases {
		if got := redactDSN(in); got != want {
			t.Errorf("redactDSN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToBool(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{int64(0), false}, {int64(1), true}, {float64(0), false}, {float64(2), true},
		{true, true}, {nil, nil}, {"0", false}, {"1", true}, {[]byte("1"), true},
	}
	for _, c := range cases {
		if got := toBool(c.in); got != c.want {
			t.Errorf("toBool(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// TestMigrateDB populates a SQLite database, imports it into a scratch
// Postgres schema, and checks the data — including the self-referencing device
// parent link and the id sequences — survived the crossing.
func TestMigrateDB(t *testing.T) {
	dsn := os.Getenv("NETIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NETIS_TEST_PG_DSN not set")
	}
	ctx := context.Background()

	srcPath := t.TempDir() + "/src.db"
	src, err := store.Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	seedForMigration(t, src)
	src.Close() // the importer opens it itself

	toDSN := pgScratchSchema(t, dsn)
	if err := runMigrateDB(ctx, []string{"-from", srcPath, "-to", toDSN}); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Open(toDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()

	devs, err := dst.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("imported %d devices, want 2", len(devs))
	}
	var child store.Device
	for _, d := range devs {
		if d.Name == "guest" {
			child = d.Device
		}
	}
	if child.ParentDeviceID == nil {
		t.Fatal("parent_device_id was not relinked")
	}
	parent, err := dst.GetDevice(ctx, *child.ParentDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Name != "host" {
		t.Errorf("child's parent is %q, want host", parent.Name)
	}
	if !parent.Reviewed {
		t.Error("reviewed did not survive as a boolean")
	}

	sts, err := dst.ListIntegrationStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sts) != 1 || !sts[0].OK {
		t.Errorf("integration_status = %+v", sts)
	}
	evs, err := dst.ListEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Errorf("imported %d events, want 1", len(evs))
	}
	if _, ok, err := dst.GetUserByName(ctx, "root"); err != nil || !ok {
		t.Errorf("user did not survive: ok=%v err=%v", ok, err)
	}

	// The identity sequences must be past the imported ids, or the next insert
	// collides with an existing row.
	newID, err := dst.CreateDevice(ctx, store.Device{Name: "fresh", Kind: "other", Source: "manual"})
	if err != nil {
		t.Fatalf("insert after import: %v", err)
	}
	if newID <= parent.ID {
		t.Errorf("new device id %d is not past the imported ids", newID)
	}

	// A second import must refuse to write into a populated database.
	err = runMigrateDB(ctx, []string{"-from", srcPath, "-to", toDSN})
	if err == nil {
		t.Error("importing into a non-empty database should fail without -force")
	}
}

func seedForMigration(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	snID, err := s.CreateSubnet(ctx, store.Subnet{CIDR: "10.1.0.0/24", Kind: "lan",
		ScanEnabled: true, ScanIntervalSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.CreateDevice(ctx, store.Device{Name: "host", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	// The child is created after the parent but relinked separately, so the
	// import's second pass is what has to get this right.
	guestID, err := s.CreateDevice(ctx, store.Device{Name: "guest", Kind: "vm", Source: "proxmox"})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.GetDevice(ctx, guestID)
	if err != nil {
		t.Fatal(err)
	}
	guest.ParentDeviceID = &hostID
	if err := s.UpdateDevice(ctx, guest); err != nil {
		t.Fatal(err)
	}

	mac := "de:ad:be:ef:00:01"
	ifID, err := s.AddIface(ctx, hostID, &mac, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignIP(ctx, ifID, snID, "10.1.0.9", "static"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkSeen(ctx, ifID, 3.5, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAvailability(ctx, ifID, true, "2026-09-07T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertOpenPort(ctx, ifID, 22, "tcp", "ssh",
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceTags(ctx, hostID, []string{"prod"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCustomField(ctx, hostID, "rack", "A1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddLink(ctx, hostID, "ui", "http://host/"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEvent(ctx, "device_new", &hostID, "seen"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "root", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "offline_after", "3"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIntegrationStatus(ctx, store.IntegrationStatus{
		Name: "proxmox", LastRun: "2026-09-07T10:00:00Z", OK: true, Detail: "ok", ItemCount: 2}); err != nil {
		t.Fatal(err)
	}
}

// pgScratchSchema returns a DSN pointing at a fresh, empty schema on the test
// server, dropped when the test ends.
func pgScratchSchema(t *testing.T, dsn string) string {
	t.Helper()
	schema := fmt.Sprintf("netis_mig_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer db.Close()
		db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	})
	sep := "?"
	if len(dsn) > 0 && containsRune(dsn, '?') {
		sep = "&"
	}
	return dsn + sep + "search_path=" + schema
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
