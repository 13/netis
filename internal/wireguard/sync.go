package wireguard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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

type Stats struct {
	Peers int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
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

func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

func (s *Sync) recordStatus(ctx context.Context, stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "wireguard", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit(ctx, "scan_error", nil, "wireguard sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Peers
		st.Detail = fmt.Sprintf("%d peers", stats.Peers)
	}
	if serr := s.store.SetIntegrationStatus(ctx, st); serr != nil {
		slog.Error("wireguard status write", "err", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) upsertPeer(ctx context.Context, p Peer, subnets []store.Subnet, now time.Time) error {
	var devID, ifaceID int64
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM device WHERE wg_pubkey=?`, p.PubKey).Scan(&devID)
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

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		// Bound each cycle so a hung `wg show dump` over SSH can't stall the
		// poller forever; a deadline surfaces as a RunOnce error and is
		// recorded as a failing status.
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		stats, err := s.runAndCount(cctx)
		s.recordStatus(cctx, stats, err)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
