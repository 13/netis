package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"netis/internal/config"
	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/store/storetest"
)

func backupConfig(dir string) config.Config {
	return config.Config{BackupDir: dir, BackupInterval: time.Hour, BackupKeep: 3}
}

// No NETIS_BACKUP_DIR: nothing starts, nothing is written, and the web layer
// is told backups are off.
func TestStartBackupsDisabledWithoutDir(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var wg sync.WaitGroup
	if b := startBackups(context.Background(), &wg, backupConfig(""), st, events.NewService(st, events.NewBroker())); b != nil {
		t.Fatalf("startBackups returned %v with no directory", b)
	}
	wg.Wait() // no goroutine to wait for
}

func TestStartBackupsWritesAndStops(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	b := startBackups(ctx, &wg, backupConfig(dir), st, events.NewService(st, events.NewBroker()))
	if b == nil {
		t.Fatal("backups not started with a directory set")
	}
	deadline := time.Now().Add(5 * time.Second)
	for b.Status().LastSuccess.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("no backup at startup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait() // shutdown waits for the loop, as main does before st.Close
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("%d files in the backup dir, want 1", len(entries))
	}
}

// On Postgres the setting is ignored with a warning rather than failing.
func TestStartBackupsSkipsPostgres(t *testing.T) {
	st := storetest.Postgres(t)
	dir := filepath.Join(t.TempDir(), "backups")
	var wg sync.WaitGroup
	if b := startBackups(context.Background(), &wg, backupConfig(dir), st, events.NewService(st, events.NewBroker())); b != nil {
		t.Fatal("scheduled backups started on Postgres")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("backup dir created on Postgres (stat err %v)", err)
	}
}
