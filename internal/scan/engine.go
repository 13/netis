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
		e.Events.Emit("scan_error", nil, fmt.Sprintf("subnet %s: %v", sn.CIDR, err))
		return err
	}
	arp, err := e.ARP()
	if err != nil {
		arp = map[string]string{}
	}
	now := time.Now().UTC()
	bucket := now.Truncate(time.Hour).Format(time.RFC3339)

	known, err := e.Store.ListSubnetIfaceIPs(sn.ID)
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
			e.markSeen(k.IfaceID, k.DeviceID, r.RTTms, now, bucket)
			seen[k.IfaceID] = true
			continue
		}
		mac := arp[r.IP]
		if mac != "" {
			if iface, ok, _ := e.Store.FindIfaceByMAC(mac); ok {
				// known device moved to a new IP
				e.Store.AssignIP(iface.ID, sn.ID, r.IP, "dhcp")
				e.Store.RemoveIfaceIPsInSubnetExcept(iface.ID, sn.ID, r.IP)
				e.Events.Emit("ip_changed", &iface.DeviceID,
					fmt.Sprintf("MAC %s now at %s", mac, r.IP))
				e.markSeen(iface.ID, iface.DeviceID, r.RTTms, now, bucket)
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
		went, _ := e.Store.MarkMissed(k.IfaceID, e.OfflineAfter)
		e.Store.RecordAvailability(k.IfaceID, false, bucket)
		if went {
			d, _ := e.Store.GetDevice(k.DeviceID)
			e.Events.Emit("offline", &k.DeviceID, fmt.Sprintf("%s (%s) went offline", d.Name, k.IP))
		}
	}

	e.Broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	return nil
}

func (e *Engine) markSeen(ifaceID, deviceID int64, rtt float64, now time.Time, bucket string) {
	wasOffline, _ := e.Store.MarkSeen(ifaceID, rtt, now)
	e.Store.RecordAvailability(ifaceID, true, bucket)
	if wasOffline {
		d, _ := e.Store.GetDevice(deviceID)
		e.Events.Emit("online", &deviceID, fmt.Sprintf("%s is online", d.Name))
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
	devID, err := e.Store.CreateDevice(d)
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
	ifID, err := e.Store.AddIface(devID, macP, hostP)
	if err != nil {
		return 0, false
	}
	e.Store.AssignIP(ifID, sn.ID, r.IP, "dhcp")
	e.Store.MarkSeen(ifID, r.RTTms, now)
	e.Store.RecordAvailability(ifID, true, bucket)
	e.Events.Emit("device_new", &devID, fmt.Sprintf("new device %s at %s", name, r.IP))
	return ifID, true
}
