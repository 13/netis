package scan

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"netis/internal/store"
)

// conflictTracker remembers, per subnet, which IPs were claimed by more than
// one interface at the last check, so an ip_conflict event marks the moment a
// conflict appears rather than repeating on every sweep. It lives in memory:
// a conflict still present after a restart is announced once more.
type conflictTracker struct {
	mu   sync.Mutex
	seen map[int64]map[string]bool
}

// checkConflicts emits ip_conflict for every IP in the subnet that is now
// claimed by several interfaces and was not at the previous check. A conflict
// that clears and later returns is announced again.
func (e *Engine) checkConflicts(ctx context.Context, sn store.Subnet) {
	now, err := e.Store.ConflictingIPs(ctx, sn.ID)
	if err != nil {
		slog.Error("listing IP conflicts failed", "subnet", sn.CIDR, "err", err)
		return
	}
	t := &e.conflicts
	t.mu.Lock()
	if t.seen == nil {
		t.seen = make(map[int64]map[string]bool)
	}
	prev := t.seen[sn.ID]
	cur := make(map[string]bool, len(now))
	var fresh []string
	for ip := range now {
		cur[ip] = true
		if !prev[ip] {
			fresh = append(fresh, ip)
		}
	}
	t.seen[sn.ID] = cur
	t.mu.Unlock()

	for _, ip := range fresh {
		e.Events.Emit(ctx, "ip_conflict", nil,
			fmt.Sprintf("%s in %s is claimed by %s", ip, sn.CIDR, strings.Join(now[ip], ", ")))
	}
}
