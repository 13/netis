package proxmox

import (
	"context"
	"fmt"
	"log/slog"

	"netis/internal/events"
	"netis/internal/store"
)

type Sync struct {
	store  *store.Store
	client *Client
	events *events.Service
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

// Status summarises a successful run for the integration status row: the
// item count and the detail line shown on the settings page.
func (stats Stats) Status() (int, string) {
	return stats.Guests, fmt.Sprintf("%d guests, %d nodes", stats.Guests, stats.Nodes)
}

func (s *Sync) upsertNode(ctx context.Context, node string) (int64, error) {
	id, ok, err := s.store.FindProxmoxNode(ctx, node)
	if err != nil {
		return 0, err
	}
	if ok {
		return id, nil
	}
	return s.store.CreateDevice(ctx, store.Device{Name: node, Kind: "server", Source: "proxmox"})
}

func (s *Sync) upsertGuest(ctx context.Context, g Guest, nodeID int64) error {
	kind := "vm"
	if g.Type == "lxc" {
		kind = "lxc"
	}
	devID, found, err := s.store.FindProxmoxGuest(ctx, g.VMID)
	if err != nil {
		return err
	}
	if !found { // new guest
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
