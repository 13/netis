package scan

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

type Engine struct {
	Store   *store.Store
	Events  *events.Service
	Broker  *events.Broker
	Sweeper Sweeper
	ARP     func() (map[string]string, error)
	Resolve func(context.Context, string) string
}

// defaultOfflineAfter is the consecutive-miss threshold used when the
// offline_after setting is unset or unusable.
const defaultOfflineAfter = 3

// offlineAfter reads the consecutive-miss threshold from settings, on every
// sweep rather than once at process start. The threshold is editable in
// Settings > General, and caching it meant a saved change did nothing until
// netis restarted while the form showed the new value as if it were live.
//
// A missing, unparseable or out-of-range value falls back to the default: the
// form already bounds it to 1-10, and a bad row must not turn every sweep into
// an offline flap (0) or stop reporting offline at all (huge).
func (e *Engine) offlineAfter(ctx context.Context) int {
	v, err := e.Store.GetSetting(ctx, "offline_after")
	if err != nil {
		slog.Error("reading offline_after setting failed", "err", err)
		return defaultOfflineAfter
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 10 {
		return defaultOfflineAfter
	}
	return n
}

// logStoreErr reports a store write that failed mid-sweep. A sweep does not
// abort on one bad write — the remaining hosts are still worth recording — but
// the failure must not vanish either: silently dropped writes make a database
// outage look like a fleet that has gone quiet.
func logStoreErr(op string, ifaceID int64, err error) {
	if err != nil {
		slog.Error("scan store write failed", "op", op, "iface_id", ifaceID, "err", err)
	}
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
	offlineAfter := e.offlineAfter(ctx)
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
		went, err := e.Store.MarkMissed(ctx, k.IfaceID, offlineAfter)
		logStoreErr("MarkMissed", k.IfaceID, err)
		logStoreErr("RecordAvailability", k.IfaceID,
			e.Store.RecordAvailability(ctx, k.IfaceID, false, bucket))
		if went {
			d, err := e.Store.GetDevice(ctx, k.DeviceID)
			logStoreErr("GetDevice", k.IfaceID, err)
			e.Events.Emit(ctx, "offline", &k.DeviceID, fmt.Sprintf("%s (%s) went offline", d.Name, k.IP))
		}
	}

	e.Broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	return nil
}

func (e *Engine) markSeen(ctx context.Context, ifaceID, deviceID int64, rtt float64, now time.Time, bucket string) {
	wasOffline, err := e.Store.MarkSeen(ctx, ifaceID, rtt, now)
	logStoreErr("MarkSeen", ifaceID, err)
	logStoreErr("RecordAvailability", ifaceID,
		e.Store.RecordAvailability(ctx, ifaceID, true, bucket))
	if wasOffline {
		d, err := e.Store.GetDevice(ctx, deviceID)
		logStoreErr("GetDevice", ifaceID, err)
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
	var macP, hostP *string
	if mac != "" {
		macP = &mac
	}
	if resolved != "" {
		hostP = &resolved
	}
	d := store.Device{Name: name, Kind: "other", Source: "scan", Vendor: Vendor(mac)}
	// Device, interface and IP are created together: a device that got as far
	// as being inserted without an interface is invisible to MAC matching and
	// would linger in the list forever.
	devID, ifID, err := e.Store.CreateDiscoveredDevice(ctx, d, macP, hostP, sn.ID, r.IP, "dhcp")
	if err != nil {
		slog.Error("creating discovered device failed", "ip", r.IP, "mac", mac, "err", err)
		return 0, false
	}
	_, err = e.Store.MarkSeen(ctx, ifID, r.RTTms, now)
	logStoreErr("MarkSeen", ifID, err)
	logStoreErr("RecordAvailability", ifID,
		e.Store.RecordAvailability(ctx, ifID, true, bucket))
	e.Events.Emit(ctx, "device_new", &devID, fmt.Sprintf("new device %s at %s", name, r.IP))
	return ifID, true
}
