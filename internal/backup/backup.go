// Package backup takes scheduled SQLite snapshots from the running server and
// keeps the newest few.
//
// It is the in-process counterpart of `netis backup`: the same VACUUM INTO,
// but through the server's open store connection instead of a second handle,
// on a timer instead of cron, and with pruning so the directory does not grow
// without bound.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// nameLayout is the time layout of a backup file name. It is UTC and sorts
// lexically in time order, which is what pruning relies on.
const nameLayout = "netis-20060102-150405.db"

// namePattern matches backup file names and nothing else, so pruning never
// deletes a file netis did not write.
var namePattern = regexp.MustCompile(`^netis-\d{8}-\d{6}\.db$`)

// maxRetryDelay caps how long a failed backup waits before trying again, so a
// transient failure on a daily schedule does not leave a day without a backup.
const maxRetryDelay = time.Hour

// Snapshotter writes a consistent copy of the database to a new file.
// *store.Store satisfies it.
type Snapshotter interface {
	VacuumInto(ctx context.Context, path string) error
}

// Emitter records an event. *events.Service satisfies it.
type Emitter interface {
	Emit(ctx context.Context, typ string, deviceID *int64, details string)
}

// Status is what the scheduler reports about itself.
type Status struct {
	// LastAttempt is when the last backup was tried; zero before the first.
	LastAttempt time.Time
	// LastSuccess is when the newest backup was written, including one found
	// on disk at startup.
	LastSuccess time.Time
	// OK reports whether the last attempt succeeded. Before any attempt it is
	// true when a backup from an earlier run was found.
	OK bool
	// File is the newest backup's file name, without the directory.
	File string
}

// Summary is a one-line, path-free description for the UI, which any signed-in
// user can read.
func (st Status) Summary() string {
	switch {
	case !st.LastAttempt.IsZero() && !st.OK:
		return "failed at " + st.LastAttempt.UTC().Format(time.RFC3339) + ", see server log"
	case st.LastSuccess.IsZero():
		return "none yet"
	}
	return st.LastSuccess.UTC().Format(time.RFC3339) + " (" + st.File + ")"
}

// Scheduler takes a backup every interval and keeps the newest keep of them.
type Scheduler struct {
	db       Snapshotter
	events   Emitter
	dir      string
	interval time.Duration
	keep     int
	now      func() time.Time

	// runMu serialises backups; mu guards status and failing.
	runMu   sync.Mutex
	mu      sync.Mutex
	status  Status
	failing bool
}

// New returns a scheduler writing to dir. events may be nil.
func New(db Snapshotter, events Emitter, dir string, interval time.Duration, keep int) *Scheduler {
	return &Scheduler{db: db, events: events, dir: dir, interval: interval, keep: keep, now: time.Now}
}

// Status returns the current backup status.
func (s *Scheduler) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start runs the schedule in a goroutine tracked on wg until ctx is done.
//
// The first backup is due one interval after the newest one already on disk,
// so restarting netis does not take a backup every time, and an instance that
// was stopped for longer than the interval catches up straight away.
func (s *Scheduler) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Go(func() {
		next := s.seed()
		for {
			if d := next.Sub(s.now()); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-ctx.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
			if ctx.Err() != nil {
				return
			}
			delay := s.interval
			if err := s.RunOnce(ctx); err != nil {
				delay = min(s.interval, maxRetryDelay)
			}
			next = s.now().Add(delay)
		}
	})
}

// seed records the newest backup already in the directory as the last success
// and returns when the next backup is due.
func (s *Scheduler) seed() time.Time {
	names, err := s.list()
	if err != nil || len(names) == 0 {
		return s.now()
	}
	newest := names[len(names)-1]
	t, err := time.ParseInLocation(nameLayout, newest, time.UTC)
	if err != nil {
		return s.now()
	}
	s.mu.Lock()
	s.status = Status{LastSuccess: t, OK: true, File: newest}
	s.mu.Unlock()
	return t.Add(s.interval)
}

// RunOnce takes one backup, prunes old ones and records the outcome. A backup
// cut short by ctx being cancelled (shutdown) is not recorded as a failure.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	now := s.now().UTC()
	name := now.Format(nameLayout)
	err := s.write(ctx, name)
	if err != nil && ctx.Err() != nil {
		return err
	}
	if err == nil {
		if perr := s.prune(); perr != nil {
			// The backup itself is fine; a leftover old file is not a failure.
			slog.Warn("pruning old backups", "dir", s.dir, "err", perr)
		}
		slog.Info("scheduled backup written", "file", filepath.Join(s.dir, name))
	}
	s.record(ctx, now, name, err)
	return err
}

// record stores the outcome of a run and raises one scan_error event per
// outage, like integration syncs do. The event carries no path or raw error:
// every signed-in user can read events, and the log has the details.
func (s *Scheduler) record(ctx context.Context, at time.Time, name string, err error) {
	s.mu.Lock()
	s.status.LastAttempt = at
	s.status.OK = err == nil
	announce := false
	if err == nil {
		s.status.LastSuccess = at
		s.status.File = name
		s.failing = false
	} else if !s.failing {
		s.failing = true
		announce = true
	}
	s.mu.Unlock()

	if err == nil {
		return
	}
	slog.Error("scheduled backup failed", "dir", s.dir, "err", err)
	if announce && s.events != nil {
		ectx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.events.Emit(ectx, "scan_error", nil, "scheduled backup failing")
	}
}

// write snapshots the database to dir/name. The snapshot goes to a hidden temp
// name first and is renamed into place once it is complete and on disk, so a
// crash or a full disk never leaves a truncated file under a backup's name.
func (s *Scheduler) write(ctx context.Context, name string) (err error) {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return err
	}
	final := filepath.Join(s.dir, name)
	tmp := filepath.Join(s.dir, "."+name+".tmp")
	// VACUUM INTO refuses an existing file; a temp left by a crash is garbage.
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if err := s.db.VacuumInto(ctx, tmp); err != nil {
		return fmt.Errorf("vacuum into %s: %w", tmp, err)
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	// Make the rename itself durable. Best effort: not every filesystem
	// supports syncing a directory.
	if d, derr := os.Open(s.dir); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// list returns the backup file names in dir, oldest first.
func (s *Scheduler) list() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && namePattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// prune deletes all but the newest keep backups.
func (s *Scheduler) prune() error {
	names, err := s.list()
	if err != nil {
		return err
	}
	var firstErr error
	for len(names) > s.keep {
		if err := os.Remove(filepath.Join(s.dir, names[0])); err != nil && firstErr == nil {
			firstErr = err
		}
		names = names[1:]
	}
	return firstErr
}
