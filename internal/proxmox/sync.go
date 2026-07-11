package proxmox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

func (s *Sync) RunOnce(ctx context.Context) error {
	guests, err := s.client.ListGuests(ctx)
	if err != nil {
		return err
	}
	nodeIDs := make(map[string]int64)
	for _, g := range guests {
		if _, ok := nodeIDs[g.Node]; ok {
			continue
		}
		id, err := s.upsertNode(g.Node)
		if err != nil {
			return err
		}
		nodeIDs[g.Node] = id
	}
	for _, g := range guests {
		if err := s.upsertGuest(ctx, g, nodeIDs[g.Node]); err != nil {
			log.Printf("proxmox guest %d: %v", g.VMID, err)
		}
	}
	return nil
}

func (s *Sync) upsertNode(node string) (int64, error) {
	var id int64
	err := s.store.DB.QueryRow(
		`SELECT id FROM device WHERE name=? AND source='proxmox' AND proxmox_vmid IS NULL`, node).
		Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return s.store.CreateDevice(store.Device{Name: node, Kind: "server", Source: "proxmox"})
}

func (s *Sync) upsertGuest(ctx context.Context, g Guest, nodeID int64) error {
	kind := "vm"
	if g.Type == "lxc" {
		kind = "lxc"
	}
	var devID int64
	err := s.store.DB.QueryRow(
		`SELECT id FROM device WHERE proxmox_vmid=? AND source='proxmox'`, g.VMID).Scan(&devID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) { // new guest
		vmid := g.VMID
		devID, err = s.store.CreateDevice(store.Device{
			Name: g.Name, Kind: kind, Source: "proxmox",
			ParentDeviceID: &nodeID, ProxmoxVMID: &vmid,
		})
		if err != nil {
			return err
		}
		s.events.Emit("device_new", &devID, fmt.Sprintf("proxmox guest %s (%d)", g.Name, g.VMID))
	} else {
		d, err := s.store.GetDevice(devID)
		if err != nil {
			return err
		}
		d.Name, d.Kind, d.ParentDeviceID = g.Name, kind, &nodeID
		if err := s.store.UpdateDevice(d); err != nil {
			return err
		}
	}
	if err := s.store.SetCustomField(devID, "proxmox_status", g.Status); err != nil {
		return err
	}
	macs, err := s.client.GuestMACs(ctx, g.Node, g.VMID, g.Type)
	if err != nil {
		return err
	}
	for _, mac := range macs {
		if _, ok, _ := s.store.FindIfaceByMAC(mac); !ok {
			m := mac
			s.store.AddIface(devID, &m, nil)
		}
	}
	return nil
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil {
			if !s.failing {
				s.failing = true
				s.events.Emit("scan_error", nil, "proxmox sync failing: "+err.Error())
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
