package scan

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"netis/internal/store"
)

// maxParallelScans bounds how many subnets are swept at the same time. Each
// sweep fans out probes of its own, so the real packet rate is this times the
// sweeper's per-subnet concurrency; a homelab with a dozen subnets should not
// be able to point its whole ICMP budget at the network at once.
const maxParallelScans = 4

type Scheduler struct {
	engine  *Engine
	store   *store.Store
	mu      sync.Mutex
	lastRun map[int64]time.Time
	// running holds the subnets with a scan in flight, so a second scan of the
	// same subnet is skipped rather than queued behind the first.
	running map[int64]bool
	sem     chan struct{}
	wg      sync.WaitGroup
	trigger chan int64
}

func NewScheduler(e *Engine, st *store.Store) *Scheduler {
	// Buffer generously so a single "Scan all" fan-out across many subnets is
	// never silently dropped: Trigger is non-blocking, and the scheduler only
	// reads one id per loop turn. 256 is far above any realistic subnet count.
	return &Scheduler{
		engine: e, store: st,
		lastRun: make(map[int64]time.Time),
		running: make(map[int64]bool),
		sem:     make(chan struct{}, maxParallelScans),
		trigger: make(chan int64, 256),
	}
}

func (s *Scheduler) Trigger(subnetID int64) {
	select {
	case s.trigger <- subnetID:
	default:
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.trigger:
			if sn, err := s.store.GetSubnet(ctx, id); err == nil {
				s.start(ctx, sn, true)
			}
		case <-tick.C:
			subnets, err := s.store.ListSubnets(ctx)
			if err != nil {
				continue
			}
			for _, sn := range subnets {
				if !sn.ScanEnabled || sn.Kind == "wireguard" {
					continue
				}
				s.mu.Lock()
				due := time.Since(s.lastRun[sn.ID]) >= time.Duration(sn.ScanIntervalSec)*time.Second
				s.mu.Unlock()
				if due {
					s.start(ctx, sn, false)
				}
			}
		}
	}
}

// shouldRunScan reports whether a scan should execute for sn. Manual scans
// (user-triggered via Trigger) run regardless of the auto-scan flag; periodic
// scans respect it. WireGuard subnets are never ARP-scannable and are always
// skipped.
func shouldRunScan(sn store.Subnet, manual bool) bool {
	if sn.Kind == "wireguard" {
		return false
	}
	if !manual && !sn.ScanEnabled {
		return false
	}
	return true
}

// start launches a scan of sn in its own goroutine, so one slow sweep no
// longer holds up every other subnet's turn and a manual trigger is not stuck
// behind whatever the periodic tick is doing. A subnet already being scanned is
// skipped: sweeps of the same subnet overlapping would double every write they
// make, and the trigger is a button a user can click as often as they like.
func (s *Scheduler) start(ctx context.Context, sn store.Subnet, manual bool) {
	if !shouldRunScan(sn, manual) {
		return
	}
	s.mu.Lock()
	if s.running[sn.ID] {
		s.mu.Unlock()
		slog.Debug("scan already running, skipping", "cidr", sn.CIDR, "manual", manual)
		return
	}
	s.running[sn.ID] = true
	// Stamped at launch rather than at completion so a long sweep does not
	// become due again the moment it finishes.
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.running, sn.ID)
			s.mu.Unlock()
		}()
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-s.sem }()
		s.run(ctx, sn, manual)
	}()
}

// Wait blocks until every scan in flight has finished. Callers use it after
// cancelling the context so the process does not close the database from under
// a sweep that is still writing to it.
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context, sn store.Subnet, manual bool) {
	// shouldRunScan is the authoritative guard for every entry point.
	if !shouldRunScan(sn, manual) {
		return
	}
	err := s.engine.RunSubnet(ctx, sn)
	if err != nil {
		slog.Error("scan failed", "cidr", sn.CIDR, "err", err)
	}
	st := store.IntegrationStatus{
		Name:    "scan",
		LastRun: time.Now().UTC().Format(time.RFC3339),
		OK:      err == nil,
		Detail:  "scanned " + sn.CIDR,
	}
	if err != nil {
		st.Detail = sn.CIDR + ": " + err.Error()
	}
	if serr := s.store.SetIntegrationStatus(ctx, st); serr != nil {
		slog.Error("scan status write", "err", serr)
	}
	s.engine.Broker.Publish("dashboard", "refresh")
}
