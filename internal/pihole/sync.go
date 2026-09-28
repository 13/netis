package pihole

import (
	"context"
	"fmt"
	"net/netip"

	"netis/internal/events"
	"netis/internal/store"
)

type Fetcher interface {
	Leases(context.Context) ([]Lease, error)
	Reservations(context.Context) ([]Reservation, error)
	DNSRecords(context.Context) ([]DNSRecord, error)
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
	leases, err := s.client.Leases(ctx)
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
	stats := Stats{Reservations: len(reservations), Leases: len(leases), DNSRecords: len(dns)}

	// claimed holds the "<subnetID>|<ip>" pairs a reservation assigned this
	// run; a lease for one of these skips assignment so a reservation's static
	// kind is never downgraded to dhcp.
	claimed := make(map[string]bool)

	for _, r := range reservations {
		snID, ok := subnetForIP(subnets, r.IP)
		if !ok {
			continue
		}
		created, err := s.upsertByMAC(ctx, r.MAC, r.IP, r.Hostname, snID, "static")
		if err != nil {
			return Stats{}, err
		}
		if created {
			stats.Created++
		}
		claimed[fmt.Sprintf("%d|%s", snID, r.IP)] = true
	}
	for _, l := range leases {
		snID, ok := subnetForIP(subnets, l.IP)
		if !ok {
			continue
		}
		if claimed[fmt.Sprintf("%d|%s", snID, l.IP)] {
			// a reservation already assigned this IP as static; still enrich
			// the hostname but do not touch the assignment kind
			if l.Hostname != "" {
				iface, found, err := s.store.FindIfaceByMAC(ctx, l.MAC)
				if err != nil {
					return Stats{}, err
				}
				if found {
					if err := s.store.SetIfaceHostnameIfEmpty(ctx, iface.ID, l.Hostname); err != nil {
						return Stats{}, err
					}
				}
			}
			continue
		}
		created, err := s.upsertByMAC(ctx, l.MAC, l.IP, l.Hostname, snID, "dhcp")
		if err != nil {
			return Stats{}, err
		}
		if created {
			stats.Created++
		}
	}
	for _, rec := range dns {
		snID, ok := subnetForIP(subnets, rec.IP)
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

// upsertByMAC enriches an existing device (matched by MAC) or creates a new
// pihole-sourced device, then assigns the IP with the given kind. A DB write
// failure is returned so RunOnce surfaces it (and Start emits scan_error)
// rather than silently reporting a healthy cycle. The returned bool reports
// whether a new device was created (vs. an existing one enriched).
func (s *Sync) upsertByMAC(ctx context.Context, mac, ip, hostname string, subnetID int64, kind string) (bool, error) {
	iface, found, err := s.store.FindIfaceByMAC(ctx, mac)
	if err != nil {
		return false, err
	}
	if found {
		if err := s.assign(ctx, iface.ID, subnetID, ip, kind); err != nil {
			return false, err
		}
		if hostname != "" {
			return false, s.store.SetIfaceHostnameIfEmpty(ctx, iface.ID, hostname)
		}
		return false, nil
	}
	name := hostname
	if name == "" {
		name = "pihole-" + mac
	}
	devID, err := s.store.CreateDevice(ctx, store.Device{Name: name, Kind: "other", Source: "pihole"})
	if err != nil {
		return false, err
	}
	m := mac
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	ifID, err := s.store.AddIface(ctx, devID, &m, hp)
	if err != nil {
		return false, err
	}
	if err := s.assign(ctx, ifID, subnetID, ip, kind); err != nil {
		return false, err
	}
	s.events.Emit(ctx, "device_new", &devID, fmt.Sprintf("pihole device %s at %s", name, ip))
	return true, nil
}

// assign records ip on the iface with the given kind. A DHCP lease is the
// server's current word on who holds the address, so it also retires the
// iface's previous DHCP address in the subnet and any other iface's DHCP claim
// on this one; otherwise a moved lease leaves the old address behind, and once
// that is re-leased two ifaces claim it. A reservation (static) retires
// nothing: the device may still hold an older lease until it renews.
func (s *Sync) assign(ctx context.Context, ifaceID, subnetID int64, ip, kind string) error {
	if err := s.store.UpsertIPAssignment(ctx, ifaceID, subnetID, ip, kind); err != nil {
		return err
	}
	if kind != "dhcp" {
		return nil
	}
	return s.store.ClaimDHCPLease(ctx, ifaceID, subnetID, ip)
}

func subnetForIP(subnets []store.Subnet, ip string) (int64, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return 0, false
	}
	for _, sn := range subnets {
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			continue
		}
		if p.Contains(addr) {
			return sn.ID, true
		}
	}
	return 0, false
}

// Status summarises a successful run for the integration status row: the
// item count and the detail line shown on the settings page.
func (stats Stats) Status() (int, string) {
	return stats.Leases, fmt.Sprintf("%d leases, %d new", stats.Leases, stats.Created)
}
