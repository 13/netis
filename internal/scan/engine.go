package scan

import (
	"context"
	"fmt"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Engine struct {
	Store        *store.Store
	Events       *events.Service
	Broker       *events.Broker
	Sweeper      Sweeper
	ARP          func() (map[string]string, error)
	Resolve      func(context.Context, string) string
	OfflineAfter int
}

func (e *Engine) RunSubnet(ctx context.Context, sn store.Subnet) error {
	results, err := e.Sweeper.Sweep(ctx, sn.CIDR)
	if err != nil {
		e.Events.Emit(ctx, "scan_error", nil, fmt.Sprintf("subnet %s: %v", sn.CIDR, err))
		return err
	}
	arp, err := e.ARP()
	if err != nil {
		arp = map[string]string{}
	}
	now := time.Now().UTC()
	bucket := now.Truncate(time.Hour).Format(time.RFC3339)

	known, err := e.Store.ListSubnetIfaceIPs(ctx, sn.ID)
	if err != nil {
		return err
	}
	knownByIP := make(map[string]store.SubnetIfaceIP, len(known))
	for _, k := range known {
		knownByIP[k.IP] = k
	}

	aliveIPs := make(map[string]bool)
	seen := make(map[int64]bool) // ifaceID -> seen this sweep, on any IP
	for _, r := range results {
		if !r.Alive {
			continue
		}
		aliveIPs[r.IP] = true
		if k, ok := knownByIP[r.IP]; ok {
			e.markSeen(ctx, k.IfaceID, k.DeviceID, r.RTTms, now, bucket)
			seen[k.IfaceID] = true
			continue
		}
		mac := arp[r.IP]
		if mac != "" {
			if iface, ok, _ := e.Store.FindIfaceByMAC(ctx, mac); ok {
				// known device moved to a new IP
				e.Store.AssignIP(ctx, iface.ID, sn.ID, r.IP, "dhcp")
				e.Store.RemoveIfaceIPsInSubnetExcept(ctx, iface.ID, sn.ID, r.IP)
				e.Events.Emit(ctx, "ip_changed", &iface.DeviceID,
					fmt.Sprintf("MAC %s now at %s", mac, r.IP))
				e.markSeen(ctx, iface.ID, iface.DeviceID, r.RTTms, now, bucket)
				seen[iface.ID] = true
				continue
			}
		}
		if ifID, ok := e.createUnknown(ctx, sn, r, mac, now, bucket); ok {
			seen[ifID] = true
		}
	}

	for _, k := range known {
		if aliveIPs[k.IP] || seen[k.IfaceID] {
			continue
		}
		went, _ := e.Store.MarkMissed(ctx, k.IfaceID, e.OfflineAfter)
		e.Store.RecordAvailability(ctx, k.IfaceID, false, bucket)
		if went {
			d, _ := e.Store.GetDevice(ctx, k.DeviceID)
			e.Events.Emit(ctx, "offline", &k.DeviceID, fmt.Sprintf("%s (%s) went offline", d.Name, k.IP))
		}
	}

	e.Broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	return nil
}

func (e *Engine) markSeen(ctx context.Context, ifaceID, deviceID int64, rtt float64, now time.Time, bucket string) {
	wasOffline, _ := e.Store.MarkSeen(ctx, ifaceID, rtt, now)
	e.Store.RecordAvailability(ctx, ifaceID, true, bucket)
	if wasOffline {
		d, _ := e.Store.GetDevice(ctx, deviceID)
		e.Events.Emit(ctx, "online", &deviceID, fmt.Sprintf("%s is online", d.Name))
	}
}

// createUnknown creates a device+iface for a previously-unseen IP/MAC.
// It returns the new iface ID and true on success, so the caller can mark
// it seen for this sweep and avoid the closing loop mis-marking it missed.
func (e *Engine) createUnknown(ctx context.Context, sn store.Subnet, r Result, mac string, now time.Time, bucket string) (int64, bool) {
	resolved := e.Resolve(ctx, r.IP)
	name := resolved
	if name == "" && mac != "" {
		name = "unknown-" + mac
	}
	if name == "" {
		name = "unknown-" + r.IP
	}
	d := store.Device{Name: name, Kind: "other", Source: "scan", Vendor: Vendor(mac)}
	devID, err := e.Store.CreateDevice(ctx, d)
	if err != nil {
		return 0, false
	}
	var macP, hostP *string
	if mac != "" {
		macP = &mac
	}
	if resolved != "" {
		hostP = &resolved
	}
	ifID, err := e.Store.AddIface(ctx, devID, macP, hostP)
	if err != nil {
		return 0, false
	}
	e.Store.AssignIP(ctx, ifID, sn.ID, r.IP, "dhcp")
	e.Store.MarkSeen(ctx, ifID, r.RTTms, now)
	e.Store.RecordAvailability(ctx, ifID, true, bucket)
	e.Events.Emit(ctx, "device_new", &devID, fmt.Sprintf("new device %s at %s", name, r.IP))
	return ifID, true
}
