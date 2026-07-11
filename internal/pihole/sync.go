package pihole

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Fetcher interface {
	Leases(context.Context) ([]Lease, error)
	Reservations(context.Context) ([]Reservation, error)
	DNSRecords(context.Context) ([]DNSRecord, error)
}

type Sync struct {
	store   *store.Store
	client  Fetcher
	events  *events.Service
	failing bool
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
	subnets, err := s.store.ListSubnets()
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
		created, err := s.upsertByMAC(r.MAC, r.IP, r.Hostname, snID, "static")
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
				iface, found, err := s.store.FindIfaceByMAC(l.MAC)
				if err != nil {
					return Stats{}, err
				}
				if found {
					if err := s.store.SetIfaceHostnameIfEmpty(iface.ID, l.Hostname); err != nil {
						return Stats{}, err
					}
				}
			}
			continue
		}
		created, err := s.upsertByMAC(l.MAC, l.IP, l.Hostname, snID, "dhcp")
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
		iface, found, err := s.store.FindIfaceByIP(snID, rec.IP)
		if err != nil {
			return Stats{}, err
		}
		if !found {
			continue // never create a device from a DNS record alone
		}
		if err := s.store.SetIfaceHostnameIfEmpty(iface.ID, rec.Name); err != nil {
			return Stats{}, err
		}
		if err := s.store.SetCustomField(iface.DeviceID, "pihole_dns", rec.Name); err != nil {
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
func (s *Sync) upsertByMAC(mac, ip, hostname string, subnetID int64, kind string) (bool, error) {
	iface, found, err := s.store.FindIfaceByMAC(mac)
	if err != nil {
		return false, err
	}
	if found {
		if err := s.store.UpsertIPAssignment(iface.ID, subnetID, ip, kind); err != nil {
			return false, err
		}
		if hostname != "" {
			return false, s.store.SetIfaceHostnameIfEmpty(iface.ID, hostname)
		}
		return false, nil
	}
	name := hostname
	if name == "" {
		name = "pihole-" + mac
	}
	devID, err := s.store.CreateDevice(store.Device{Name: name, Kind: "other", Source: "pihole"})
	if err != nil {
		return false, err
	}
	m := mac
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	ifID, err := s.store.AddIface(devID, &m, hp)
	if err != nil {
		return false, err
	}
	if err := s.store.UpsertIPAssignment(ifID, subnetID, ip, kind); err != nil {
		return false, err
	}
	s.events.Emit("device_new", &devID, fmt.Sprintf("pihole device %s at %s", name, ip))
	return true, nil
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

func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

func (s *Sync) recordStatus(stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "pihole", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit("scan_error", nil, "pihole sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Leases
		st.Detail = fmt.Sprintf("%d leases, %d new", stats.Leases, stats.Created)
	}
	if serr := s.store.SetIntegrationStatus(st); serr != nil {
		log.Printf("pihole status write: %v", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		s.recordStatus(s.runAndCount(ctx))
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
