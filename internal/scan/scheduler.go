package scan

import (
	"context"
	"log"
	"sync"
	"time"

	"netis/internal/store"
)

type Scheduler struct {
	engine  *Engine
	store   *store.Store
	mu      sync.Mutex
	lastRun map[int64]time.Time
	trigger chan int64
}

func NewScheduler(e *Engine, st *store.Store) *Scheduler {
	// Buffer generously so a single "Scan all" fan-out across many subnets is
	// never silently dropped (Trigger is non-blocking and the scheduler runs
	// scans one at a time). 256 is far above any realistic subnet count.
	return &Scheduler{engine: e, store: st, lastRun: make(map[int64]time.Time), trigger: make(chan int64, 256)}
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
				s.run(ctx, sn, true)
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
					s.run(ctx, sn, false)
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

func (s *Scheduler) run(ctx context.Context, sn store.Subnet, manual bool) {
	// shouldRunScan is the authoritative guard for every entry point.
	if !shouldRunScan(sn, manual) {
		return
	}
	s.mu.Lock()
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()
	err := s.engine.RunSubnet(ctx, sn)
	if err != nil {
		log.Printf("scan %s: %v", sn.CIDR, err)
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
		log.Printf("scan status write: %v", serr)
	}
	s.engine.Broker.Publish("dashboard", "refresh")
}
