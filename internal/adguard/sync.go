package adguard

import (
	"context"
	"fmt"

	"netis/internal/events"
	"netis/internal/leases"
	"netis/internal/store"
)

// Fetcher is the part of Client the sync uses.
type Fetcher interface {
	DHCPStatus(context.Context) (DHCP, error)
}

type Sync struct {
	store  *store.Store
	client Fetcher
	events *events.Service
}

func NewSync(st *store.Store, c Fetcher, ev *events.Service) *Sync {
	return &Sync{store: st, client: c, events: ev}
}

type Stats struct {
	// Disabled is set when AdGuard Home's DHCP server is off; nothing is
	// applied then.
	Disabled bool
	Leases   int
	Static   int
	Created  int
}

// RunOnce merges AdGuard Home's leases and static leases into the inventory,
// and its DHCP pool into the subnet that holds it.
// With the DHCP server off it changes nothing and reports that, rather than
// applying whatever stale leases the response still lists.
func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	dhcp, err := s.client.DHCPStatus(ctx)
	if err != nil {
		return Stats{}, err
	}
	if !dhcp.Enabled {
		return Stats{Disabled: true}, nil
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return Stats{}, err
	}
	created, err := leases.Apply(ctx, s.store, s.events, "adguard", subnets, dhcp.Static, dhcp.Leases)
	if err != nil {
		return Stats{}, err
	}
	if _, err := leases.ApplyPools(ctx, s.store, "adguard", subnets, dhcp.Pools); err != nil {
		return Stats{}, err
	}
	return Stats{Leases: len(dhcp.Leases), Static: len(dhcp.Static), Created: created}, nil
}

// Status summarises a successful run for the integration status row.
func (stats Stats) Status() (int, string) {
	if stats.Disabled {
		return 0, "DHCP server disabled in AdGuard Home"
	}
	return stats.Leases, fmt.Sprintf("%d leases, %d static, %d new", stats.Leases, stats.Static, stats.Created)
}
