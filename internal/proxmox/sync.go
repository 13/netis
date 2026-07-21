package proxmox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Sync struct {
	store   *store.Store
	client  *Client
	events  *events.Service
	failing bool
}

func NewSync(st *store.Store, c *Client, ev *events.Service) *Sync {
	return &Sync{store: st, client: c, events: ev}
}

type Stats struct {
	Guests int
	Nodes  int
}

func (s *Sync) RunOnce(ctx context.Context) (Stats, error) {
	guests, err := s.client.ListGuests(ctx)
	if err != nil {
		return Stats{}, err
	}
	nodeIDs := make(map[string]int64)
	for _, g := range guests {
		if _, ok := nodeIDs[g.Node]; ok {
			continue
		}
		id, err := s.upsertNode(ctx, g.Node)
		if err != nil {
			return Stats{}, err
		}
		nodeIDs[g.Node] = id
	}
	for _, g := range guests {
		if err := s.upsertGuest(ctx, g, nodeIDs[g.Node]); err != nil {
			slog.Error("proxmox guest sync", "vmid", g.VMID, "err", err)
		}
	}
	return Stats{Guests: len(guests), Nodes: len(nodeIDs)}, nil
}

// runAndCount runs one cycle and returns the stats and error, so Start and
// tests share exactly one code path.
func (s *Sync) runAndCount(ctx context.Context) (Stats, error) {
	return s.RunOnce(ctx)
}

// recordStatus writes the integration_status row and manages the
// once-per-outage scan_error event.
func (s *Sync) recordStatus(ctx context.Context, stats Stats, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	st := store.IntegrationStatus{Name: "proxmox", LastRun: now}
	if err != nil {
		if !s.failing {
			s.failing = true
			s.events.Emit(ctx, "scan_error", nil, "proxmox sync failing: "+err.Error())
		}
		st.OK = false
		st.Detail = err.Error()
	} else {
		s.failing = false
		st.OK = true
		st.ItemCount = stats.Guests
		st.Detail = fmt.Sprintf("%d guests, %d nodes", stats.Guests, stats.Nodes)
	}
	if serr := s.store.SetIntegrationStatus(ctx, st); serr != nil {
		slog.Error("proxmox status write", "err", serr)
	}
	s.events.Broker().Publish("dashboard", "refresh")
}

func (s *Sync) upsertNode(ctx context.Context, node string) (int64, error) {
	var id int64
	err := s.store.DB.QueryRowContext(ctx,
		`SELECT id FROM device WHERE name=? AND source='proxmox' AND proxmox_vmid IS NULL`, node).
		Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return s.store.CreateDevice(ctx, store.Device{Name: node, Kind: "server", Source: "proxmox"})
}

func (s *Sync) upsertGuest(ctx context.Context, g Guest, nodeID int64) error {
	kind := "vm"
	if g.Type == "lxc" {
		kind = "lxc"
	}
	var devID int64
	err := s.store.DB.QueryRowContext(ctx,
		`SELECT id FROM device WHERE proxmox_vmid=? AND source='proxmox'`, g.VMID).Scan(&devID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) { // new guest
		vmid := g.VMID
		devID, err = s.store.CreateDevice(ctx, store.Device{
			Name: g.Name, Kind: kind, Source: "proxmox",
			ParentDeviceID: &nodeID, ProxmoxVMID: &vmid,
		})
		if err != nil {
			return err
		}
		s.events.Emit(ctx, "device_new", &devID, fmt.Sprintf("proxmox guest %s (%d)", g.Name, g.VMID))
	} else {
		d, err := s.store.GetDevice(ctx, devID)
		if err != nil {
			return err
		}
		d.Name, d.Kind, d.ParentDeviceID = g.Name, kind, &nodeID
		if err := s.store.UpdateDevice(ctx, d); err != nil {
			return err
		}
	}
	if err := s.store.SetCustomField(ctx, devID, "proxmox_status", g.Status); err != nil {
		return err
	}
	macs, err := s.client.GuestMACs(ctx, g.Node, g.VMID, g.Type)
	if err != nil {
		return err
	}
	for _, mac := range macs {
		if _, ok, _ := s.store.FindIfaceByMAC(ctx, mac); !ok {
			m := mac
			s.store.AddIface(ctx, devID, &m, nil)
		}
	}
	return nil
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		stats, err := s.runAndCount(ctx)
		s.recordStatus(ctx, stats, err)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
