// Package leases merges DHCP leases and reservations from any DHCP server
// integration (Pi-hole, AdGuard Home, OPNsense) into the device inventory, so
// every source follows the same rules: devices are matched by MAC, a
// reservation's static kind is never downgraded by a lease, and nothing a user
// set is overwritten.
package leases

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"netis/internal/events"
	"netis/internal/store"
)

// Entry is one lease or reservation: a MAC holding an IP, and the hostname
// the DHCP server knows it by (possibly empty).
type Entry struct {
	MAC      string
	IP       string
	Hostname string
}

// Apply records static (reservations) and dynamic (leases) on the inventory
// and returns how many devices it created. Entries outside every subnet are
// skipped. Reservations go first as kind static; a lease for an address a
// reservation claimed this run only fills an empty hostname. A lease is kind
// dhcp and retires stale DHCP claims (see assign). An unknown MAC becomes a new
// device with the given source. A store failure is returned so the run is
// reported failing rather than healthy.
func Apply(ctx context.Context, st *store.Store, ev *events.Service, source string,
	subnets []store.Subnet, static, dynamic []Entry) (int, error) {
	created := 0

	// claimed holds the "<subnetID>|<ip>" pairs a reservation assigned this
	// run; a lease for one of these skips assignment so a reservation's static
	// kind is never downgraded to dhcp.
	claimed := make(map[string]bool)

	for _, r := range static {
		snID, ok := SubnetFor(subnets, r.IP)
		if !ok {
			continue
		}
		c, err := upsertByMAC(ctx, st, ev, source, r, snID, "static")
		if err != nil {
			return 0, err
		}
		if c {
			created++
		}
		claimed[fmt.Sprintf("%d|%s", snID, r.IP)] = true
	}
	for _, l := range dynamic {
		snID, ok := SubnetFor(subnets, l.IP)
		if !ok {
			continue
		}
		if claimed[fmt.Sprintf("%d|%s", snID, l.IP)] {
			// a reservation already assigned this IP as static; still enrich
			// the hostname but do not touch the assignment kind
			if l.Hostname != "" {
				iface, found, err := st.FindIfaceByMAC(ctx, l.MAC)
				if err != nil {
					return 0, err
				}
				if found {
					if err := st.SetIfaceHostnameIfEmpty(ctx, iface.ID, l.Hostname); err != nil {
						return 0, err
					}
				}
			}
			continue
		}
		c, err := upsertByMAC(ctx, st, ev, source, l, snID, "dhcp")
		if err != nil {
			return 0, err
		}
		if c {
			created++
		}
	}
	return created, nil
}

// upsertByMAC enriches an existing device (matched by MAC) or creates a new
// device with the given source, then assigns the IP with the given kind. The
// returned bool reports whether a new device was created.
func upsertByMAC(ctx context.Context, st *store.Store, ev *events.Service, source string,
	e Entry, subnetID int64, kind string) (bool, error) {
	iface, found, err := st.FindIfaceByMAC(ctx, e.MAC)
	if err != nil {
		return false, err
	}
	if found {
		if err := assign(ctx, st, source, iface.ID, subnetID, e.IP, kind); err != nil {
			return false, err
		}
		if e.Hostname != "" {
			return false, st.SetIfaceHostnameIfEmpty(ctx, iface.ID, e.Hostname)
		}
		return false, nil
	}
	name := e.Hostname
	if name == "" {
		name = source + "-" + e.MAC
	}
	devID, err := st.CreateDevice(ctx, store.Device{Name: name, Kind: "other", Source: source})
	if err != nil {
		return false, err
	}
	m := e.MAC
	var hp *string
	if e.Hostname != "" {
		h := e.Hostname
		hp = &h
	}
	ifID, err := st.AddIface(ctx, devID, &m, hp)
	if err != nil {
		return false, err
	}
	if err := assign(ctx, st, source, ifID, subnetID, e.IP, kind); err != nil {
		return false, err
	}
	ev.Emit(ctx, "device_new", &devID, fmt.Sprintf("%s device %s at %s", source, name, e.IP))
	return true, nil
}

// assign records ip on the iface with the given kind, labelling a row it
// creates with source (an existing row keeps its label). A DHCP lease is the
// server's current word on who holds the address, so it also retires the
// iface's previous DHCP address in the subnet and any other iface's DHCP claim
// on this one; otherwise a moved lease leaves the old address behind, and once
// that is re-leased two ifaces claim it. A reservation (static) retires
// nothing: the device may still hold an older lease until it renews.
func assign(ctx context.Context, st *store.Store, source string, ifaceID, subnetID int64, ip, kind string) error {
	if err := st.UpsertIPAssignmentFrom(ctx, ifaceID, subnetID, ip, kind, source); err != nil {
		return err
	}
	if kind != "dhcp" {
		return nil
	}
	return st.ClaimDHCPLease(ctx, ifaceID, subnetID, ip)
}

// SubnetFor returns the ID of the first subnet containing ip.
func SubnetFor(subnets []store.Subnet, ip string) (int64, bool) {
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

// macRe matches a canonical lowercase colon-separated MAC (6 hex pairs).
var macRe = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

// NormMAC lowercases a colon-separated MAC, or returns "" if s is not one.
// Dash-separated MACs are accepted too.
func NormMAC(s string) string {
	m := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", ":")
	if !macRe.MatchString(m) {
		return ""
	}
	return m
}

// NormIP returns the canonical form of an IP address, or "" if s is not one.
func NormIP(s string) string {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return a.String()
}
