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

func (s *Sync) RunOnce(ctx context.Context) error {
	reservations, err := s.client.Reservations(ctx)
	if err != nil {
		return err
	}
	leases, err := s.client.Leases(ctx)
	if err != nil {
		return err
	}
	dns, err := s.client.DNSRecords(ctx)
	if err != nil {
		return err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return err
	}

	// (subnetID<<0, ip) pairs a reservation already claimed this run; leases
	// for these skip assignment so a reservation's static kind is never
	// downgraded to dhcp.
	claimed := make(map[string]bool)

	for _, r := range reservations {
		snID, ok := subnetForIP(subnets, r.IP)
		if !ok {
			continue
		}
		s.upsertByMAC(r.MAC, r.IP, r.Hostname, snID, "static")
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
			if iface, found, _ := s.store.FindIfaceByMAC(l.MAC); found && l.Hostname != "" {
				s.store.SetIfaceHostnameIfEmpty(iface.ID, l.Hostname)
			}
			continue
		}
		s.upsertByMAC(l.MAC, l.IP, l.Hostname, snID, "dhcp")
	}
	for _, rec := range dns {
		snID, ok := subnetForIP(subnets, rec.IP)
		if !ok {
			continue
		}
		iface, found, err := s.store.FindIfaceByIP(snID, rec.IP)
		if err != nil {
			return err
		}
		if !found {
			continue // never create a device from a DNS record alone
		}
		s.store.SetIfaceHostnameIfEmpty(iface.ID, rec.Name)
		s.store.SetCustomField(iface.DeviceID, "pihole_dns", rec.Name)
	}
	return nil
}

// upsertByMAC enriches an existing device (matched by MAC) or creates a new
// pihole-sourced device, then assigns the IP with the given kind.
func (s *Sync) upsertByMAC(mac, ip, hostname string, subnetID int64, kind string) {
	iface, found, err := s.store.FindIfaceByMAC(mac)
	if err != nil {
		log.Printf("pihole: FindIfaceByMAC %s: %v", mac, err)
		return
	}
	if found {
		s.store.UpsertIPAssignment(iface.ID, subnetID, ip, kind)
		if hostname != "" {
			s.store.SetIfaceHostnameIfEmpty(iface.ID, hostname)
		}
		return
	}
	name := hostname
	if name == "" {
		name = "pihole-" + mac
	}
	devID, err := s.store.CreateDevice(store.Device{Name: name, Kind: "other", Source: "pihole"})
	if err != nil {
		log.Printf("pihole: CreateDevice: %v", err)
		return
	}
	m := mac
	var hp *string
	if hostname != "" {
		hp = &hostname
	}
	ifID, err := s.store.AddIface(devID, &m, hp)
	if err != nil {
		log.Printf("pihole: AddIface: %v", err)
		return
	}
	s.store.UpsertIPAssignment(ifID, subnetID, ip, kind)
	s.events.Emit("device_new", &devID, fmt.Sprintf("pihole device %s at %s", name, ip))
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

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "pihole sync failing: "+err.Error())
			}
		} else {
			s.failing = false
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
