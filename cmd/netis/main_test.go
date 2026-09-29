package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
	"netis/internal/web"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func statusOf(t *testing.T, st *store.Store, name string) *store.IntegrationStatus {
	t.Helper()
	list, err := st.ListIntegrationStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func countEvents(t *testing.T, st *store.Store, typ string) int {
	t.Helper()
	evs, err := st.ListEvents(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func TestIntegrationRunnerReadsCurrentSettings(t *testing.T) {
	st := openTestStore(t)
	runner := newIntegrationRunner(st, events.NewService(st, events.NewBroker()))

	// Unconfigured → the not-configured sentinel, and nothing recorded.
	if err := runner.Run(context.Background(), "pihole"); !errors.Is(err, errNotConfigured) {
		t.Fatalf("unconfigured pihole: got %v, want errNotConfigured", err)
	}
	if s := statusOf(t, st, "pihole"); s != nil {
		t.Fatalf("unconfigured pihole recorded status %+v", s)
	}

	// Configured but unreachable → a real error that is NOT the sentinel, proving
	// the runner read the current setting and attempted the run (run-now no longer
	// reports "not configured" for a configured integration).
	if err := st.SetSetting(t.Context(), "pihole_url", "http://127.0.0.1:9"); err != nil {
		t.Fatal(err)
	}
	err := runner.Run(context.Background(), "pihole")
	if err == nil || errors.Is(err, errNotConfigured) {
		t.Fatalf("configured pihole: got %v, want a non-nil non-sentinel error", err)
	}
	// The production path records the failure, so the settings page and the
	// Run-now toast show "failing" instead of "not configured".
	if s := statusOf(t, st, "pihole"); s == nil || s.OK || s.Detail == "" {
		t.Fatalf("failing pihole status = %+v, want a failing row", s)
	}

	// Unknown integration name → error.
	if err := runner.Run(context.Background(), "bogus"); err == nil {
		t.Fatal("bogus integration should error")
	}
}

// The DHCP lease integrations follow the same path: unconfigured records
// nothing, configured against a dead address records a categorised failure.
func TestIntegrationRunnerDHCPSources(t *testing.T) {
	st := openTestStore(t)
	runner := newIntegrationRunner(st, events.NewService(st, events.NewBroker()))
	for _, name := range []string{"adguard", "opnsense"} {
		if err := runner.Run(context.Background(), name); !errors.Is(err, errNotConfigured) {
			t.Fatalf("unconfigured %s: got %v, want errNotConfigured", name, err)
		}
		if s := statusOf(t, st, name); s != nil {
			t.Fatalf("unconfigured %s recorded status %+v", name, s)
		}
		if err := st.SetSetting(t.Context(), name+"_url", "http://127.0.0.1:9"); err != nil {
			t.Fatal(err)
		}
		if err := runner.Run(context.Background(), name); err == nil || errors.Is(err, errNotConfigured) {
			t.Fatalf("configured %s: got %v, want a failure", name, err)
		}
		if s := statusOf(t, st, name); s == nil || s.OK || s.Detail != "connection failed" {
			t.Fatalf("%s status = %+v, want failing with connection failed", name, s)
		}
	}
	for _, name := range []string{"adguard", "opnsense"} {
		found := false
		for _, n := range integrationNames {
			found = found || n == name
		}
		if !found {
			t.Errorf("%s is not driven by the periodic sync", name)
		}
	}
}

// An HTTP 401 from a lease source is filed under authentication failed.
func TestFailureCategoryDHCPAuth(t *testing.T) {
	for _, msg := range []string{"adguard /control/dhcp/status: HTTP 401", "opnsense /api/kea/leases4/search: HTTP 403"} {
		if got := failureCategory(errors.New(msg)); got != "authentication failed" {
			t.Errorf("%q -> %q", msg, got)
		}
	}
}

func TestIntegrationRunnerRecordsStatus(t *testing.T) {
	st := openTestStore(t)
	var fail atomic.Bool
	runner := newRunner(st, events.NewService(st, events.NewBroker()), time.Minute,
		map[string]integrationFunc{
			"x": func(context.Context) (int, string, error) {
				if fail.Load() {
					return 0, "", errors.New("boom")
				}
				return 3, "3 things", nil
			},
		})

	if err := runner.Run(t.Context(), "x"); err != nil {
		t.Fatal(err)
	}
	if s := statusOf(t, st, "x"); s == nil || !s.OK || s.ItemCount != 3 || s.Detail != "3 things" || s.LastRun == "" {
		t.Fatalf("success status = %+v", s)
	}

	// Two failing runs: both recorded, but only one "sync failing" event per outage.
	fail.Store(true)
	for range 2 {
		if err := runner.Run(t.Context(), "x"); err == nil {
			t.Fatal("want error")
		}
	}
	if s := statusOf(t, st, "x"); s == nil || s.OK || s.Detail != "sync error" {
		t.Fatalf("failure status = %+v", s)
	}
	if n := countEvents(t, st, "scan_error"); n != 1 {
		t.Fatalf("scan_error events = %d, want 1", n)
	}

	// Recovery clears the outage, so the next failure alerts again.
	fail.Store(false)
	if err := runner.Run(t.Context(), "x"); err != nil {
		t.Fatal(err)
	}
	if s := statusOf(t, st, "x"); s == nil || !s.OK {
		t.Fatalf("recovered status = %+v", s)
	}
	// The end of an outage is announced once; a first success is not.
	if n := countEvents(t, st, "sync_recovered"); n != 1 {
		t.Fatalf("sync_recovered events = %d, want 1", n)
	}
	runner.Run(t.Context(), "x")
	if n := countEvents(t, st, "sync_recovered"); n != 1 {
		t.Fatalf("sync_recovered events after a second success = %d, want 1", n)
	}
	fail.Store(true)
	runner.Run(t.Context(), "x")
	if n := countEvents(t, st, "scan_error"); n != 2 {
		t.Fatalf("scan_error events after second outage = %d, want 2", n)
	}
}

// A run that outlives its deadline is cut off and still recorded as failing:
// the status write gets a fresh context rather than the expired one.
func TestIntegrationRunnerDeadline(t *testing.T) {
	st := openTestStore(t)
	runner := newRunner(st, events.NewService(st, events.NewBroker()), 20*time.Millisecond,
		map[string]integrationFunc{
			"x": func(ctx context.Context) (int, string, error) {
				<-ctx.Done()
				return 0, "", ctx.Err()
			},
		})
	if err := runner.Run(t.Context(), "x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
	if s := statusOf(t, st, "x"); s == nil || s.OK || s.Detail != "timeout" {
		t.Fatalf("status = %+v, want a failing row saying timeout", s)
	}
	if n := countEvents(t, st, "scan_error"); n != 1 {
		t.Fatalf("scan_error events = %d, want 1", n)
	}
}

// Run now while the periodic loop (or another click) is mid-run reports busy
// straight away instead of starting an overlapping sync.
func TestIntegrationRunnerBusy(t *testing.T) {
	st := openTestStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	runner := newRunner(st, events.NewService(st, events.NewBroker()), time.Minute,
		map[string]integrationFunc{
			"x": func(context.Context) (int, string, error) {
				calls.Add(1)
				close(started)
				<-release
				return 0, "", nil
			},
			"y": func(context.Context) (int, string, error) { return 0, "", nil },
		})
	done := make(chan error, 1)
	go func() { done <- runner.Run(t.Context(), "x") }()
	<-started

	if err := runner.Run(t.Context(), "x"); !errors.Is(err, web.ErrIntegrationBusy) {
		t.Fatalf("concurrent run: got %v, want ErrIntegrationBusy", err)
	}
	// The lock is per integration: another one runs meanwhile.
	if err := runner.Run(t.Context(), "y"); err != nil {
		t.Fatalf("other integration: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("x ran %d times, want 1", n)
	}
}

func TestRunIntegrationLoopRunsThenStops(t *testing.T) {
	st := openTestStore(t)
	var calls int32
	runner := newRunner(st, events.NewService(st, events.NewBroker()), time.Minute,
		map[string]integrationFunc{
			"x": func(ctx context.Context) (int, string, error) {
				atomic.AddInt32(&calls, 1)
				return 0, "", errNotConfigured
			},
		})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runIntegrationLoop(ctx, runner, "x", time.Hour) // long interval: only the immediate run fires
		close(done)
	}()
	// The loop runs the closure once immediately, before waiting on the ticker.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("loop never ran the closure")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return after context cancel")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("closure called %d times, want exactly 1 (immediate run only)", got)
	}
}

// Shutdown waits on the shared WaitGroup before closing the store, so a sync
// still unwinding after cancel must hold it open until it has finished.
func TestShutdownWaitsForIntegrationRuns(t *testing.T) {
	st := openTestStore(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	runner := newRunner(st, events.NewService(st, events.NewBroker()), time.Minute,
		map[string]integrationFunc{
			"proxmox": func(context.Context) (int, string, error) {
				once.Do(func() { close(started) })
				<-release // ignores cancellation, like a slow store write
				return 1, "done", nil
			},
			"pihole":    func(context.Context) (int, string, error) { return 0, "", errNotConfigured },
			"wireguard": func(context.Context) (int, string, error) { return 0, "", errNotConfigured },
		})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	startIntegrationSyncs(ctx, &wg, runner, time.Hour)
	startRetention(ctx, &wg, st, time.Hour)
	<-started
	cancel()

	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("wg.Wait returned while a sync was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait did not return after the sync finished")
	}
	// The in-flight run finished its status write before Wait returned.
	if s := statusOf(t, st, "proxmox"); s == nil || !s.OK {
		t.Fatalf("status = %+v, want the finished run recorded", s)
	}
}

// Every pihole run builds a fresh client, so it must end its Pi-hole session
// afterwards — whether the sync succeeded or failed. Otherwise each minute
// leaks one session until Pi-hole's max_sessions cap locks everyone out.
func TestPiholeRunLogsOut(t *testing.T) {
	st := openTestStore(t)
	var logins, logouts atomic.Int32
	var fail atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth", func(w http.ResponseWriter, r *http.Request) {
		logins.Add(1)
		w.Write([]byte(`{"session":{"sid":"SID123","valid":true}}`))
	})
	mux.HandleFunc("DELETE /api/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FTL-SID") == "SID123" {
			logouts.Add(1)
		}
		w.WriteHeader(410)
	})
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() {
				w.WriteHeader(500)
				return
			}
			w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/dhcp/leases", serve(`{"leases":[]}`))
	mux.HandleFunc("/api/config/dhcp/hosts", serve(`{"config":{"dhcp":{"hosts":[]}}}`))
	mux.HandleFunc("/api/config/dns/hosts", serve(`{"config":{"dns":{"hosts":[]}}}`))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if err := st.SetSetting(t.Context(), "pihole_url", srv.URL); err != nil {
		t.Fatal(err)
	}
	runner := newIntegrationRunner(st, events.NewService(st, events.NewBroker()))

	if err := runner.Run(t.Context(), "pihole"); err != nil {
		t.Fatalf("successful run: %v", err)
	}
	if li, lo := logins.Load(), logouts.Load(); li != 1 || lo != 1 {
		t.Fatalf("after success: logins=%d logouts=%d, want 1/1", li, lo)
	}
	fail.Store(true)
	if err := runner.Run(t.Context(), "pihole"); err == nil {
		t.Fatal("failing run: want error")
	}
	if li, lo := logins.Load(), logouts.Load(); li != 2 || lo != 2 {
		t.Fatalf("after failure: logins=%d logouts=%d, want 2/2", li, lo)
	}
}

// The raw error of a failed sync names key paths, internal addresses and URLs,
// and the status row and "sync failing" event are shown to every viewer. Only a
// category may leave the log.
func TestIntegrationFailureDetailIsGeneric(t *testing.T) {
	st := openTestStore(t)
	secret := errors.New("read ssh key: open /root/.ssh/id_netis: no such file or directory")
	runner := newRunner(st, events.NewService(st, events.NewBroker()), time.Minute,
		map[string]integrationFunc{
			"x": func(context.Context) (int, string, error) { return 0, "", secret },
		})
	if err := runner.Run(t.Context(), "x"); err == nil {
		t.Fatal("want error")
	}
	s := statusOf(t, st, "x")
	if s == nil || strings.Contains(s.Detail, "/root") || s.Detail == "" {
		t.Fatalf("status detail = %+v, want a generic category", s)
	}
	evs, err := st.ListEvents(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if strings.Contains(e.Details, "/root") {
			t.Fatalf("event leaks the raw error: %q", e.Details)
		}
	}
}

func TestRunnerCallsAfterOnSuccess(t *testing.T) {
	st := openTestStore(t)
	evs := events.NewService(st, events.NewBroker())
	calls := 0
	fail := false
	r := newRunner(st, evs, time.Second, map[string]integrationFunc{
		"x": func(context.Context) (int, string, error) {
			if fail {
				return 0, "", errors.New("boom")
			}
			return 1, "", nil
		},
	})
	r.after = func() { calls++ }
	r.Run(t.Context(), "x")
	fail = true
	r.Run(t.Context(), "x")
	if calls != 1 {
		t.Fatalf("after calls = %d", calls)
	}
}

func TestFailureCategory(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "timeout"},
		{fmt.Errorf("proxmox sync: %w", context.DeadlineExceeded), "timeout"},
		{errors.New("pihole auth: HTTP 401"), "authentication failed"},
		{errors.New("proxmox /nodes: HTTP 401"), "authentication failed"},
		{errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]"), "authentication failed"},
		{errors.New("ssh: handshake failed: knownhosts: key mismatch"), "host key rejected"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "connection failed"},
		{fmt.Errorf("get: %w", &net.DNSError{Err: "no such host", Name: "pve.lan"}), "connection failed"},
		{fmt.Errorf("read ssh key: %w", &fs.PathError{Op: "open", Path: "/root/k", Err: fs.ErrNotExist}), "configuration error"},
		{errors.New("proxmox /nodes: HTTP 500"), "sync error"},
		{errors.New("boom"), "sync error"},
	} {
		if got := failureCategory(tc.err); got != tc.want {
			t.Errorf("failureCategory(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
