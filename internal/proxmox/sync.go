package proxmox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	seen := make(map[int64]bool, len(guests))
	complete := true
	for _, g := range guests {
		devID, err := s.upsertGuest(ctx, g, nodeIDs[g.Node])
		if err != nil {
			slog.Error("proxmox guest sync", "vmid", g.VMID, "err", err)
		}
		if devID == 0 {
			complete = false // not resolved: cannot say which devices are gone
			continue
		}
		seen[devID] = true
	}
	// An empty list is not taken as "every guest was deleted": a token that
	// lost its privileges gets an empty list with a 200, and flagging every
	// guest on that would be worse than missing the removal of the last one.
	if complete && len(guests) > 0 {
		s.reconcile(ctx, seen)
	}
	return Stats{Guests: len(guests), Nodes: len(nodeIDs)}, nil
}

// reconcile marks the guests missing from a complete guest list, clears the
// mark on those that are back, and raises one event for each transition.
// Devices are never deleted: a guest that is gone is the user's to remove.
func (s *Sync) reconcile(ctx context.Context, seen map[int64]bool) {
	gone, back, err := s.store.ReconcileUpstream(ctx, store.ScopeProxmoxGuests, seen, time.Now())
	if err != nil {
		slog.Error("proxmox missing-guest check", "err", err)
		return
	}
	for _, d := range gone {
		s.events.Emit(ctx, "device_missing", &d.ID, fmt.Sprintf("Proxmox guest %s is no longer in Proxmox", d.Name))
	}
	for _, d := range back {
		s.events.Emit(ctx, "device_returned", &d.ID, fmt.Sprintf("Proxmox guest %s is back in Proxmox", d.Name))
	}
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
	if !ok {
		id, err = s.store.CreateDevice(ctx, store.Device{Name: node, Kind: "server", Source: "proxmox"})
		if err != nil {
			return 0, err
		}
	}
	// Record the Proxmox name apart from the device name, so a node the user
	// renamed is still found instead of being created again.
	return id, s.store.SetCustomField(ctx, id, "proxmox_node", node)
}

// upsertGuest creates or updates the device for g and returns its id, which
// is non-zero whenever the guest's device was found or created, even when a
// later step fails.
func (s *Sync) upsertGuest(ctx context.Context, g Guest, nodeID int64) (int64, error) {
	kind := "vm"
	if g.Type == "lxc" {
		kind = "lxc"
	}
	devID, found, err := s.store.FindProxmoxGuest(ctx, g.VMID)
	if err != nil {
		return 0, err
	}
	// Name and kind are only seeded when the guest is created, so the user's
	// edits stick. Afterwards the sync only moves the parent, and only between
	// Proxmox nodes (see SetProxmoxGuestParent), never rewriting the row.
	if !found { // new guest
		vmid := g.VMID
		devID, err = s.store.CreateDevice(ctx, store.Device{
			Name: g.Name, Kind: kind, Source: "proxmox",
			ParentDeviceID: &nodeID, ProxmoxVMID: &vmid,
		})
		if err != nil {
			return 0, err
		}
		s.events.Emit(ctx, "device_new", &devID, fmt.Sprintf("Proxmox guest %s (%d)", g.Name, g.VMID))
	} else if err := s.store.SetProxmoxGuestParent(ctx, devID, nodeID); err != nil {
		return devID, err
	}
	if err := s.store.SetCustomField(ctx, devID, "proxmox_status", g.Status); err != nil {
		return devID, err
	}
	macs, err := s.client.GuestMACs(ctx, g.Node, g.VMID, g.Type)
	if err != nil {
		return devID, err
	}
	for _, mac := range macs {
		if err := s.attachMAC(ctx, devID, g, mac); err != nil {
			return devID, err
		}
	}
	return devID, nil
}

// attachMAC gives the guest an interface for mac. When the MAC is already
// known, the scan or Pi-hole most likely found the guest first and made a
// device for it; that interface is moved to the guest (and the discovered
// device dropped if nothing else is on it) unless the user has reviewed the
// device, in which case it is theirs and is left alone.
func (s *Sync) attachMAC(ctx context.Context, devID int64, g Guest, mac string) error {
	iface, found, err := s.store.FindIfaceByMAC(ctx, mac)
	if err != nil {
		return err
	}
	if !found {
		m := mac
		_, err := s.store.AddIface(ctx, devID, &m, nil)
		return err
	}
	if iface.DeviceID == devID {
		return nil
	}
	moved, removed, err := s.store.AdoptDiscoveredIface(ctx, iface.ID, devID)
	if err != nil {
		return err
	}
	if !moved {
		slog.Info("proxmox guest MAC belongs to a reviewed device; leaving it there",
			"vmid", g.VMID, "mac", mac, "device", iface.DeviceID)
		return nil
	}
	slog.Info("proxmox guest adopted discovered interface",
		"vmid", g.VMID, "mac", mac, "from_device", iface.DeviceID, "from_device_deleted", removed)
	return nil
}
