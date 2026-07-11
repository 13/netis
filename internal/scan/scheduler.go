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
	return &Scheduler{engine: e, store: st, lastRun: make(map[int64]time.Time), trigger: make(chan int64, 8)}
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
			if sn, err := s.store.GetSubnet(id); err == nil {
				s.run(ctx, sn)
			}
		case <-tick.C:
			subnets, err := s.store.ListSubnets()
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
					s.run(ctx, sn)
				}
			}
		}
	}
}

func (s *Scheduler) run(ctx context.Context, sn store.Subnet) {
	// Guard here (not just at the call sites) so every entry point that
	// reaches run — periodic tick or manual Trigger — is protected.
	if !sn.ScanEnabled || sn.Kind == "wireguard" {
		return
	}
	s.mu.Lock()
	s.lastRun[sn.ID] = time.Now()
	s.mu.Unlock()
	if err := s.engine.RunSubnet(ctx, sn); err != nil {
		log.Printf("scan %s: %v", sn.CIDR, err)
	}
}
