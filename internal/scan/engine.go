package scan

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"netis/internal/events"
	"netis/internal/macaddr"
	"netis/internal/store"
)

// Kicker asks for work without waiting for it (autofill.Service).
type Kicker interface{ Kick() }

type Engine struct {
	Store   *store.Store
	Events  *events.Service
	Broker  *events.Broker
	Sweeper Sweeper
	ARP     func() (map[string]string, error)
	Resolve func(context.Context, string) string
	// Autofill is kicked after each sweep so new and changed devices get
	// their details filled in. Nil turns that off.
	Autofill Kicker
	// Presence confirms a host that answered ARP but not ping (TCPProbe in
	// production). Nil skips straight to the ARP re-check.
	Presence func(context.Context, string) (float64, bool)
	// ARPSettle is how long after a sweep an unconfirmed ARP entry must still
	// resolve to count as presence (DefaultARPSettle in production).
	ARPSettle time.Duration

	// nameMiss remembers when an address last resolved to nothing, so a
	// nameless host is not looked up on every sweep.
	nameMu   sync.Mutex
	nameMiss map[string]time.Time

	conflicts conflictTracker
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
	swept, err := e.Sweeper.Sweep(ctx, sn.CIDR)
	if err != nil {
		e.Events.Emit(ctx, "scan_error", nil, fmt.Sprintf("Subnet %s: %v", sn.CIDR, err))
		return err
	}
	sweptAt := time.Now()
	arp, err := e.ARP()
	if err != nil {
		arp = map[string]string{}
	}
	// Copied so presence can mark hosts alive without touching the sweeper's
	// slice.
	results := append([]Result(nil), swept...)
	if e.presenceEnabled(ctx) {
		confirmed := e.confirmPresence(ctx, results, arp, sweptAt)
		for i := range results {
			if rtt, ok := confirmed[results[i].IP]; ok {
				results[i].Alive, results[i].RTTms = true, rtt
			}
		}
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
	names := e.resolveAll(ctx, namesWanted(results, knownByIP, arp))

	aliveIPs := make(map[string]bool)
	seen := make(map[int64]bool)        // ifaceID -> seen this sweep, on any IP
	unknownIPs := make(map[string]bool) // probe could not run: neither seen nor missed
	for _, r := range results {
		if r.Err != nil {
			unknownIPs[r.IP] = true
			continue
		}
		if !r.Alive {
			continue
		}
		aliveIPs[r.IP] = true
		mac := arp[r.IP]
		// A known IP identifies its device unless ARP says a different machine
		// answered: DHCP reuses addresses, and crediting the old device would
		// keep it online while the newcomer (or a swapped peer) went unseen.
		// Without an ARP entry, as on a routed subnet, the IP is all there is.
		if k, ok := knownByIP[r.IP]; ok && !macConflict(k.MAC, mac) {
			e.markSeen(ctx, k.IfaceID, k.DeviceID, r.RTTms, now, bucket)
			e.fillHostname(ctx, k.IfaceID, names[r.IP])
			seen[k.IfaceID] = true
			continue
		}
		if mac != "" {
			if iface, ok, _ := e.Store.FindIfaceByMAC(ctx, mac); ok {
				// known device moved to a new IP
				e.Store.AssignIP(ctx, iface.ID, sn.ID, r.IP, "dhcp")
				e.Store.RemoveIfaceIPsInSubnetExcept(ctx, iface.ID, sn.ID, r.IP)
				e.Events.Emit(ctx, "ip_changed", &iface.DeviceID,
					fmt.Sprintf("MAC %s now at %s", mac, r.IP))
				e.markSeen(ctx, iface.ID, iface.DeviceID, r.RTTms, now, bucket)
				e.fillHostname(ctx, iface.ID, names[r.IP])
				seen[iface.ID] = true
				continue
			}
		}
		if ifID, ok := e.createUnknown(ctx, sn, r, mac, names[r.IP], now, bucket); ok {
			seen[ifID] = true
		}
	}

	for _, k := range known {
		// An alive IP only covers the ifaces there that ARP did not rule out;
		// one displaced by a different MAC misses this sweep like any other.
		if seen[k.IfaceID] || unknownIPs[k.IP] || (aliveIPs[k.IP] && !macConflict(k.MAC, arp[k.IP])) {
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

	e.checkConflicts(ctx, sn)
	e.Broker.Publish(fmt.Sprintf("grid:%d", sn.ID), "refresh")
	if e.Autofill != nil {
		e.Autofill.Kick()
	}
	return nil
}

// macConflict reports whether ARP names a different MAC than the one an iface
// is known by. Either side missing is not a conflict: there is nothing to
// compare.
func macConflict(known *string, arpMAC string) bool {
	return known != nil && *known != "" && arpMAC != "" && !strings.EqualFold(*known, arpMAC)
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
//
// A device with a randomized (private) MAC and no resolved name is named
// private-<mac>, and its device_new event says why: the MAC will change, so
// the user is better off knowing it is probably a phone than hunting a vendor.
func (e *Engine) createUnknown(ctx context.Context, sn store.Subnet, r Result, mac, resolved string, now time.Time, bucket string) (int64, bool) {
	private := macaddr.IsPrivate(mac)
	name := resolved
	if name == "" && mac != "" {
		if private {
			name = "private-" + mac
		} else {
			name = "unknown-" + mac
		}
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
	d := store.Device{Name: name, Kind: "other", Source: "scan"}
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
	msg := fmt.Sprintf("New device %s at %s", name, r.IP)
	if private {
		msg += " (randomized MAC — may be a phone with Private Wi-Fi Address)"
	}
	e.Events.Emit(ctx, "device_new", &devID, msg)
	return ifID, true
}

// resolveParallel bounds concurrent name lookups in a sweep.
const resolveParallel = 16

// nameRetryAfter is how long an address that resolved to nothing is left
// alone before it is looked up again.
const nameRetryAfter = 30 * time.Minute

// namesWanted lists the alive addresses worth a name lookup: those that will
// become a new device or a moved iface, and known ifaces with no hostname yet.
func namesWanted(results []Result, knownByIP map[string]store.SubnetIfaceIP, arp map[string]string) []string {
	var out []string
	for _, r := range results {
		if r.Err != nil || !r.Alive {
			continue
		}
		k, ok := knownByIP[r.IP]
		if !ok || macConflict(k.MAC, arp[r.IP]) || k.Hostname == nil || *k.Hostname == "" {
			out = append(out, r.IP)
		}
	}
	return out
}

// resolveAll looks names up for ips concurrently, skipping addresses that
// came back empty within nameRetryAfter. Lookups are slow (reverse DNS and
// then mDNS each wait for a timeout on a miss), so one host at a time made
// discovering a busy subnet take minutes.
func (e *Engine) resolveAll(ctx context.Context, ips []string) map[string]string {
	out := make(map[string]string)
	if len(ips) == 0 {
		return out
	}
	now := time.Now()
	e.nameMu.Lock()
	var todo []string
	for _, ip := range ips {
		if t, ok := e.nameMiss[ip]; ok && now.Sub(t) < nameRetryAfter {
			continue
		}
		todo = append(todo, ip)
	}
	e.nameMu.Unlock()

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, resolveParallel)
	for _, ip := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			name := e.Resolve(ctx, ip)
			mu.Lock()
			out[ip] = name
			mu.Unlock()
		}(ip)
	}
	wg.Wait()

	e.nameMu.Lock()
	defer e.nameMu.Unlock()
	if e.nameMiss == nil {
		e.nameMiss = make(map[string]time.Time)
	}
	for ip, name := range out {
		if name == "" {
			e.nameMiss[ip] = now
		} else {
			delete(e.nameMiss, ip)
		}
	}
	return out
}

// fillHostname records a discovered name on an iface that has none. It never
// replaces a hostname a user or an integration already set.
func (e *Engine) fillHostname(ctx context.Context, ifaceID int64, name string) {
	if name == "" {
		return
	}
	logStoreErr("SetIfaceHostnameIfEmpty", ifaceID, e.Store.SetIfaceHostnameIfEmpty(ctx, ifaceID, name))
}
