package wireguard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

const onlineWindow = 3 * time.Minute

type Sync struct {
	store   *store.Store
	runner  Runner
	events  *events.Service
	iface   string
	failing bool
}

func NewSync(st *store.Store, r Runner, ev *events.Service, iface string) *Sync {
	return &Sync{store: st, runner: r, events: ev, iface: iface}
}

func (s *Sync) RunOnce(ctx context.Context) error {
	out, err := s.runner.Run(ctx, "wg show "+s.iface+" dump")
	if err != nil {
		return err
	}
	peers, err := ParseDump(out)
	if err != nil {
		return err
	}
	subnets, err := s.store.ListSubnets()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, p := range peers {
		if err := s.upsertPeer(p, subnets, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sync) upsertPeer(p Peer, subnets []store.Subnet, now time.Time) error {
	var devID, ifaceID int64
	err := s.store.DB.QueryRow(`SELECT id FROM device WHERE wg_pubkey=?`, p.PubKey).Scan(&devID)
	// PROJECT DECISION: distinguish sql.ErrNoRows (new peer) from real errors.
	// device has no unique constraint on wg_pubkey, so falling through to
	// CreateDevice on a transient error would create a duplicate peer.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) { // new peer
		name := p.PubKey
		if len(name) > 8 {
			name = name[:8]
		}
		if len(p.AllowedIPs) > 0 {
			name = strings.SplitN(p.AllowedIPs[0], "/", 2)[0]
		}
		pk := p.PubKey
		devID, err = s.store.CreateDevice(store.Device{
			Name: name, Kind: "wg-peer", Source: "wireguard", WGPubKey: &pk,
		})
		if err != nil {
			return err
		}
		ifaceID, err = s.store.AddIface(devID, nil, nil)
		if err != nil {
			return err
		}
		s.assignAllowedIPs(ifaceID, p, subnets)
		s.events.Emit("device_new", &devID, fmt.Sprintf("wireguard peer %s", name))
	} else {
		ifaces, err := s.store.ListIfaces(devID)
		if err != nil || len(ifaces) == 0 {
			return fmt.Errorf("peer %s has no iface: %v", p.PubKey, err)
		}
		ifaceID = ifaces[0].ID
	}

	if !p.LastHandshake.IsZero() && now.Sub(p.LastHandshake) < onlineWindow {
		wasOffline, _ := s.store.MarkSeen(ifaceID, 0, now)
		if wasOffline {
			d, _ := s.store.GetDevice(devID)
			s.events.Emit("online", &devID, fmt.Sprintf("wg peer %s connected", d.Name))
		}
	} else {
		went, _ := s.store.MarkMissed(ifaceID, 1)
		if went {
			d, _ := s.store.GetDevice(devID)
			s.events.Emit("offline", &devID, fmt.Sprintf("wg peer %s disconnected", d.Name))
		}
	}
	return nil
}

func (s *Sync) assignAllowedIPs(ifaceID int64, p Peer, subnets []store.Subnet) {
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
			s.store.AssignIP(ifaceID, sn.ID, addr.String(), "static")
		}
	}
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "wireguard sync failing: "+err.Error())
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
