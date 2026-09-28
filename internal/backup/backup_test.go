package backup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"netis/internal/store"
	"netis/internal/store/storetest"
)

type recordingEmitter struct {
	mu   sync.Mutex
	msgs []string
}

func (e *recordingEmitter) Emit(_ context.Context, typ string, _ *int64, details string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.msgs = append(e.msgs, typ+": "+details)
}

func (e *recordingEmitter) got() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.msgs)
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A scheduled backup is a real, openable database holding the live data, under
// the documented name, with no temp file left beside it.
func TestRunOnceWritesAReadableBackup(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	devID, err := st.CreateDevice(ctx, store.Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "backups") // created on demand
	s := New(st, nil, dir, time.Hour, 3)
	s.now = func() time.Time { return time.Date(2026, 9, 28, 3, 15, 0, 0, time.UTC) }
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if got := dirNames(t, dir); !slices.Equal(got, []string{"netis-20260928-031500.db"}) {
		t.Fatalf("backup dir holds %v", got)
	}
	restored, err := store.Open(filepath.Join(dir, "netis-20260928-031500.db"))
	if err != nil {
		t.Fatalf("backup is not a usable database: %v", err)
	}
	defer restored.Close()
	d, err := restored.GetDevice(ctx, devID)
	if err != nil || d.Name != "nas" {
		t.Fatalf("restored device = %+v, %v", d, err)
	}

	stat := s.Status()
	if !stat.OK || stat.File != "netis-20260928-031500.db" || !stat.LastSuccess.Equal(stat.LastAttempt) {
		t.Errorf("status = %+v", stat)
	}
}

// Pruning keeps the newest N backups and never touches anything else in the
// directory, including files that merely look similar.
func TestPruneKeepsNewestAndIgnoresOtherFiles(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	dir := t.TempDir()
	unrelated := []string{"notes.txt", "netis.db", "netis-20200101-000000.db.bak",
		"netis-2020-01-01.db", "backup-2020-01-01.db"}
	for _, n := range append(slices.Clone(unrelated), "netis-20200101-000000.db", "netis-20200102-000000.db") {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s := New(st, nil, dir, time.Hour, 2)
	clock := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for range 3 {
		if err := s.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour)
	}

	want := append(slices.Clone(unrelated), "netis-20260928-010000.db", "netis-20260928-020000.db")
	slices.Sort(want)
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Errorf("after pruning:\n got %v\nwant %v", got, want)
	}
}

// An unwritable destination is recorded as a failure and announced once per
// outage, not on every retry; the next success clears it.
func TestFailureIsRecordedAndAnnouncedOnce(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	// A regular file where the directory should be: MkdirAll fails even as root.
	dir := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ev := &recordingEmitter{}
	s := New(st, ev, dir, time.Hour, 3)

	for range 2 {
		if err := s.RunOnce(ctx); err == nil {
			t.Fatal("backup into a file path succeeded")
		}
	}
	stat := s.Status()
	if stat.OK || stat.LastAttempt.IsZero() || !stat.LastSuccess.IsZero() {
		t.Errorf("status after failure = %+v", stat)
	}
	if !strings.HasPrefix(stat.Summary(), "failed at ") {
		t.Errorf("summary = %q", stat.Summary())
	}
	if got := ev.got(); !slices.Equal(got, []string{"scan_error: scheduled backup failing"}) {
		t.Errorf("events = %v, want one scan_error", got)
	}

	// Fix the destination: the next run succeeds and clears the outage.
	os.Remove(dir)
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Status().OK {
		t.Error("status still failing after a successful run")
	}
}

// Postgres has no VACUUM INTO; the store says so instead of running it.
func TestVacuumIntoRefusesPostgres(t *testing.T) {
	st := storetest.Postgres(t)
	if err := st.VacuumInto(context.Background(), filepath.Join(t.TempDir(), "x.db")); err != store.ErrBackupUnsupported {
		t.Fatalf("err = %v, want ErrBackupUnsupported", err)
	}
}

// At startup the newest existing backup counts as the last success and sets
// when the next one is due, so a restart does not take an extra backup.
func TestSeedFromExistingBackups(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"netis-20260927-030000.db", "netis-20260926-030000.db", "other.db"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	s := New(nil, nil, dir, 24*time.Hour, 7)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	next := s.seed()
	if want := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("next due %v, want %v", next, want)
	}
	stat := s.Status()
	if !stat.OK || stat.File != "netis-20260927-030000.db" || !stat.LastAttempt.IsZero() {
		t.Errorf("seeded status = %+v", stat)
	}
	if stat.Summary() != "2026-09-27T03:00:00Z (netis-20260927-030000.db)" {
		t.Errorf("summary = %q", stat.Summary())
	}

	// Nothing on disk (or no directory yet): due immediately.
	empty := New(nil, nil, filepath.Join(dir, "missing"), 24*time.Hour, 7)
	empty.now = s.now
	if next := empty.seed(); !next.Equal(now) {
		t.Errorf("empty dir: next due %v, want now", next)
	}
	if empty.Status().Summary() != "none yet" {
		t.Errorf("empty summary = %q", empty.Status().Summary())
	}
}

// Start takes the first backup straight away when none exists, and returns
// when its context is cancelled.
func TestStartBacksUpAndStops(t *testing.T) {
	st := openStore(t)
	dir := t.TempDir()
	s := New(st, nil, dir, time.Hour, 3)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	s.Start(ctx, &wg)

	deadline := time.Now().Add(5 * time.Second)
	for s.Status().LastSuccess.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("no backup taken at startup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	if n := len(dirNames(t, dir)); n != 1 {
		t.Errorf("%d files in backup dir, want 1", n)
	}
}
