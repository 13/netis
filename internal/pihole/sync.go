package pihole

import (
	"context"
	"fmt"
	"log/slog"

	"netis/internal/events"
	"netis/internal/leases"
	"netis/internal/store"
)

type Fetcher interface {
	Leases(context.Context) ([]Lease, error)
	Reservations(context.Context) ([]Reservation, error)
	DNSRecords(context.Context) ([]DNSRecord, error)
	DHCPPool(context.Context) ([]leases.Range, error)
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
	Leases       int
	Reservations int
	DNSRecords   int
	Created      int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	reservations, err := s.client.Reservations(ctx)
	if err != nil {
		return Stats{}, err
	}
	leaseList, err := s.client.Leases(ctx)
	if err != nil {
		return Stats{}, err
	}
	dns, err := s.client.DNSRecords(ctx)
	if err != nil {
		return Stats{}, err
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Reservations: len(reservations), Leases: len(leaseList), DNSRecords: len(dns)}

	static := make([]leases.Entry, 0, len(reservations))
	for _, r := range reservations {
		static = append(static, leases.Entry{MAC: r.MAC, IP: r.IP, Hostname: r.Hostname})
	}
	dynamic := make([]leases.Entry, 0, len(leaseList))
	for _, l := range leaseList {
		dynamic = append(dynamic, leases.Entry{MAC: l.MAC, IP: l.IP, Hostname: l.Hostname})
	}
	created, err := leases.Apply(ctx, s.store, s.events, "pihole", subnets, static, dynamic)
	if err != nil {
		return Stats{}, err
	}
	stats.Created = created

	// The pool is a bonus: a Pi-hole that will not show its DHCP config still
	// has leases worth having, so a failure here does not fail the run.
	if pools, err := s.client.DHCPPool(ctx); err != nil {
		slog.Warn("pihole dhcp pool unavailable", "err", err)
	} else if _, err := leases.ApplyPools(ctx, s.store, "pihole", subnets, pools); err != nil {
		return Stats{}, err
	}

	for _, rec := range dns {
		snID, ok := leases.SubnetFor(subnets, rec.IP)
		if !ok {
			continue
		}
		iface, found, err := s.store.FindIfaceByIP(ctx, snID, rec.IP)
		if err != nil {
			return Stats{}, err
		}
		if !found {
			continue // never create a device from a DNS record alone
		}
		if err := s.store.SetIfaceHostnameIfEmpty(ctx, iface.ID, rec.Name); err != nil {
			return Stats{}, err
		}
		if err := s.store.SetCustomField(ctx, iface.DeviceID, "pihole_dns", rec.Name); err != nil {
			return Stats{}, err
		}
	}
	return stats, nil
}

// Status summarises a successful run for the integration status row: the
// item count and the detail line shown on the settings page.
func (stats Stats) Status() (int, string) {
	return stats.Leases, fmt.Sprintf("%d leases, %d new", stats.Leases, stats.Created)
}
