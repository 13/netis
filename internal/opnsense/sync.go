package opnsense

import (
	"context"
	"fmt"
	"log/slog"

	"netis/internal/events"
	"netis/internal/leases"
	"netis/internal/store"
)

// Fetcher is the part of Client the sync uses.
type Fetcher interface {
	Leases(context.Context) (Leases, error)
	ARP(context.Context) ([]ARPEntry, error)
	Pools(ctx context.Context, backend string) ([]leases.Range, error)
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
	Backend string
	Leases  int
	Static  int
	Created int
	// MACsFilled counts interfaces that got their MAC from the firewall's ARP
	// table; ARPUnavailable is set when that table could not be read.
	MACsFilled     int
	ARPUnavailable bool
}

// RunOnce fills in MACs from the firewall's ARP table, merges its DHCP leases
// into the inventory, and gives each subnet its DHCP pool. The ARP step goes
// first so a lease for a host the scanner found without a MAC (on a routed
// subnet) enriches that device instead of creating a second one beside it.
// A failure to read the ARP table or the pools does not fail the run.
func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	ls, err := s.client.Leases(ctx)
	if err != nil {
		return Stats{}, err
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Backend: ls.Backend, Leases: len(ls.Dynamic), Static: len(ls.Static)}

	arp, err := s.client.ARP(ctx)
	if err != nil {
		slog.Warn("opnsense arp table unavailable", "err", err)
		stats.ARPUnavailable = true
	} else if stats.MACsFilled, err = s.fillMACs(ctx, subnets, arp); err != nil {
		return Stats{}, err
	}

	created, err := leases.Apply(ctx, s.store, s.events, "opnsense", subnets, ls.Static, ls.Dynamic)
	if err != nil {
		return Stats{}, err
	}
	stats.Created = created

	// Like the ARP table, the pool is a bonus that does not fail the run.
	if pools, err := s.client.Pools(ctx, ls.Backend); err != nil {
		slog.Warn("opnsense dhcp pools unavailable", "backend", ls.Backend, "err", err)
	} else if _, err := leases.ApplyPools(ctx, s.store, "opnsense", subnets, pools); err != nil {
		return Stats{}, err
	}
	return stats, nil
}

// fillMACs gives the interface holding an address the MAC the firewall's ARP
// table has for it, when that interface has none. It is conservative: an
// expired entry, a MAC seen at more than one address (proxy ARP, a
// multi-homed host), an address held by more than one interface, and a MAC
// another interface already has are all left alone. It never creates a device
// and never changes a MAC that is set.
func (s *Sync) fillMACs(ctx context.Context, subnets []store.Subnet, arp []ARPEntry) (int, error) {
	ipsOfMAC := make(map[string]map[string]bool)
	for _, e := range arp {
		if e.Expired {
			continue
		}
		if ipsOfMAC[e.MAC] == nil {
			ipsOfMAC[e.MAC] = make(map[string]bool)
		}
		ipsOfMAC[e.MAC][e.IP] = true
	}

	// holders maps subnet ID -> IP -> the interfaces assigned it, loaded once
	// per subnet the table touches.
	holders := make(map[int64]map[string][]store.SubnetIfaceIP)
	filled := 0
	for _, e := range arp {
		if e.Expired || len(ipsOfMAC[e.MAC]) != 1 || e.MAC == "ff:ff:ff:ff:ff:ff" || e.MAC == "00:00:00:00:00:00" {
			continue
		}
		snID, ok := leases.SubnetFor(subnets, e.IP)
		if !ok {
			continue
		}
		byIP, ok := holders[snID]
		if !ok {
			rows, err := s.store.ListSubnetIfaceIPs(ctx, snID)
			if err != nil {
				return 0, err
			}
			byIP = make(map[string][]store.SubnetIfaceIP)
			for _, r := range rows {
				byIP[r.IP] = append(byIP[r.IP], r)
			}
			holders[snID] = byIP
		}
		h := byIP[e.IP]
		if len(h) != 1 || (h[0].MAC != nil && *h[0].MAC != "") {
			continue
		}
		ok, err := s.store.SetIfaceMACIfEmpty(ctx, h[0].IfaceID, e.MAC)
		if err != nil {
			return 0, err
		}
		if ok {
			filled++
			mac := e.MAC
			h[0].MAC = &mac
		}
	}
	return filled, nil
}

// Status summarises a successful run for the integration status row.
func (stats Stats) Status() (int, string) {
	detail := fmt.Sprintf("%d leases, %d static, %d new", stats.Leases, stats.Static, stats.Created)
	if stats.Backend != "" {
		detail = stats.Backend + ": " + detail
	}
	if stats.MACsFilled > 0 {
		detail += fmt.Sprintf(", %d MACs from ARP", stats.MACsFilled)
	}
	if stats.ARPUnavailable {
		detail += ", ARP table unavailable"
	}
	return stats.Leases, detail
}
