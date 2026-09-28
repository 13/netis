package main

import (
	"context"
	"database/sql"
	"errors"
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

// A typo in -from used to go through store.Open, which creates a missing file
// and migrates it: the "backup" was a fresh empty database, reported as success.
func TestBackupMissingSourceCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/typo.db"
	dest := dir + "/backup.db"
	if err := runBackup(context.Background(), []string{"-from", src, "-to", dest}); err == nil {
		t.Fatal("backing up a missing database succeeded")
	}
	for _, p := range []string{src, src + "-wal", src + "-shm", dest} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after a failed backup (stat err %v)", p, err)
		}
	}
}

func TestBackupRefusesADirectorySource(t *testing.T) {
	dir := t.TempDir()
	if err := runBackup(context.Background(), []string{"-from", dir, "-to", dir + "/b.db"}); err == nil {
		t.Fatal("backing up a directory succeeded")
	}
}

// Backing up must only read the source. Opening it through store.Open ran
// migrations, so a newer binary taking a backup upgraded the live schema
// underneath the netis that owns it.
func TestBackupDoesNotMigrateTheSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := dir + "/old.db"

	// A database without netis's schema stands in for one from an older
	// release: any migration run against it would add tables.
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL; CREATE TABLE legacy (id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO legacy (v) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	schema := func() string {
		var out []string
		rows, err := db.Query(`SELECT name FROM sqlite_master ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		return strings.Join(out, ",")
	}
	before := schema()
	db.Close()

	dest := dir + "/backup.db"
	if err := runBackup(ctx, []string{"-from", src, "-to", dest}); err != nil {
		t.Fatal(err)
	}

	db, err = sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if after := schema(); after != before {
		t.Errorf("source schema changed: before %q, after %q", before, after)
	}

	bk, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatal(err)
	}
	defer bk.Close()
	var v string
	if err := bk.QueryRow(`SELECT v FROM legacy`).Scan(&v); err != nil || v != "keep" {
		t.Errorf("backup row = %q, %v", v, err)
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

// The pg_dump hint used to quote the whole URL, password included, into an
// error that lands in a terminal or a CI log.
func TestBackupPostgresHintRedactsPassword(t *testing.T) {
	for _, dsn := range []string{
		"postgres://netis:hunter2@db.lan/netis",
		"postgres://db.lan/netis?sslmode=disable&password=hunter2",
	} {
		err := runBackup(context.Background(), []string{"-from", dsn, "-to", t.TempDir() + "/x.db"})
		if err == nil || strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("err = %v, want the password redacted", err)
		}
	}
}

// -to must be a Postgres URL; a mistyped one still carries its password.
func TestMigrateDBBadDestinationRedactsPassword(t *testing.T) {
	err := runMigrateDB(context.Background(), []string{"-from", "x.db", "-to", "postgre://netis:hunter2@db.lan/netis"})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v, want the password redacted", err)
	}
}
