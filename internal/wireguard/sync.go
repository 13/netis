package wireguard

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

const onlineWindow = 3 * time.Minute

// ifaceRE is what an interface name may look like: the characters Linux
// interface names are made of in practice, and at most IFNAMSIZ-1 of them.
// Nothing in it means anything to a shell.
var ifaceRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

// ValidIface reports whether name is safe to use as the WireGuard interface.
// The name is spliced into a command the remote host's shell runs, so anything
// else in it (a space, a semicolon, a $( ) ...) would run as a command there.
func ValidIface(name string) error {
	if !ifaceRE.MatchString(name) {
		return fmt.Errorf("invalid wireguard interface name %q: use 1-15 letters, digits, '.', '_' or '-'", name)
	}
	return nil
}

type Sync struct {
	store  *store.Store
	runner Runner
	events *events.Service
	iface  string
}

func NewSync(st *store.Store, r Runner, ev *events.Service, iface string) *Sync {
	return &Sync{store: st, runner: r, events: ev, iface: iface}
}

type Stats struct {
	Peers int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	// Checked here as well as when the setting is saved: this is the point
	// where the name reaches a shell, whatever route it took to get here.
	if err := ValidIface(s.iface); err != nil {
		return Stats{}, err
	}
	out, err := s.runner.Run(ctx, "wg show "+s.iface+" dump")
	if err != nil {
		return Stats{}, err
	}
	peers, err := ParseDump(out)
	if err != nil {
		return Stats{}, err
	}
	subnets, err := s.store.ListSubnets(ctx)
	if err != nil {
		return Stats{}, err
	}
	now := time.Now().UTC()
	for _, p := range peers {
		if err := s.upsertPeer(ctx, p, subnets, now); err != nil {
			return Stats{}, err
		}
	}
	return Stats{Peers: len(peers)}, nil
}

// Status summarises a successful run for the integration status row: the
// item count and the detail line shown on the settings page.
func (stats Stats) Status() (int, string) {
	return stats.Peers, fmt.Sprintf("%d peers", stats.Peers)
}

func (s *Sync) upsertPeer(ctx context.Context, p Peer, subnets []store.Subnet, now time.Time) error {
	var ifaceID int64
	devID, found, err := s.store.FindDeviceByWGPubKey(ctx, p.PubKey)
	// PROJECT DECISION: distinguish a missing peer from real errors.
	// Falling through to CreateDevice on a transient error would try to
	// create a duplicate peer (which the unique index on wg_pubkey refuses).
	if err != nil {
		return err
	}
	if !found { // new peer
		name := p.PubKey
		if len(name) > 8 {
			name = name[:8]
		}
		if len(p.AllowedIPs) > 0 {
			name = strings.SplitN(p.AllowedIPs[0], "/", 2)[0]
		}
		pk := p.PubKey
		devID, err = s.store.CreateDevice(ctx, store.Device{
			Name: name, Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk,
		})
		if err != nil {
			return err
		}
		ifaceID, err = s.store.AddIface(ctx, devID, nil, nil)
		if err != nil {
			return err
		}
		s.assignAllowedIPs(ctx, ifaceID, p, subnets)
		s.events.Emit(ctx, "device_new", &devID, fmt.Sprintf("wireguard peer %s", name))
	} else {
		ifaces, err := s.store.ListIfaces(ctx, devID)
		if err != nil || len(ifaces) == 0 {
			return fmt.Errorf("peer %s has no iface: %v", p.PubKey, err)
		}
		ifaceID = ifaces[0].ID
	}

	if !p.LastHandshake.IsZero() && now.Sub(p.LastHandshake) < onlineWindow {
		wasOffline, _ := s.store.MarkSeen(ctx, ifaceID, 0, now)
		if wasOffline {
			d, _ := s.store.GetDevice(ctx, devID)
			s.events.Emit(ctx, "online", &devID, fmt.Sprintf("wg peer %s connected", d.Name))
		}
	} else {
		went, _ := s.store.MarkMissed(ctx, ifaceID, 1)
		if went {
			d, _ := s.store.GetDevice(ctx, devID)
			s.events.Emit(ctx, "offline", &devID, fmt.Sprintf("wg peer %s disconnected", d.Name))
		}
	}
	return nil
}

func (s *Sync) assignAllowedIPs(ctx context.Context, ifaceID int64, p Peer, subnets []store.Subnet) {
	for _, cidr := range p.AllowedIPs {
		ipStr := strings.SplitN(cidr, "/", 2)[0]
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			continue
		}
		for _, sn := range subnets {
			if sn.Kind != "wireguard" {
				continue
			}
			prefix, err := netip.ParsePrefix(sn.CIDR)
			if err != nil || !prefix.Contains(addr) {
				continue
			}
			s.store.AssignIP(ctx, ifaceID, sn.ID, addr.String(), "static")
		}
	}
}
