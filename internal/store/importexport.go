package store

import (
	"context"
	"database/sql"
	"strings"
)

// DeviceFill is what a CSV import row offers an existing device. Blank fields
// offer nothing.
type DeviceFill struct {
	Name, Kind                     string
	Notes, Vendor, Model, Function string
}

// ImportMayRename reports whether an import may replace a device's name and
// kind: only when netis guessed them itself from a scan and nobody has
// reviewed the device since. Anything an integration created carries that
// integration's identity for it, and anything reviewed carries the user's.
// FillDevice enforces the same rule in SQL.
func ImportMayRename(d Device) bool {
	return d.Source == "scan" && !d.Reviewed
}

// FillDevice applies an import row to an existing device without clobbering
// anything: notes, vendor, model and function are written only where the
// device has none, and name and kind only where ImportMayRename allows. The
// guards are in the statement rather than left to the caller, so a value set
// between an import's preview and its commit is still kept. It does not mark
// the device reviewed.
func (s *Store) FillDevice(ctx context.Context, id int64, f DeviceFill) error {
	_, err := s.exec(ctx, `UPDATE device SET
		name=CASE WHEN ?<>'' AND source='scan' AND reviewed=FALSE THEN ? ELSE name END,
		kind=CASE WHEN ?<>'' AND source='scan' AND reviewed=FALSE THEN ? ELSE kind END,
		notes=CASE WHEN notes='' THEN ? ELSE notes END,
		vendor=CASE WHEN vendor='' THEN ? ELSE vendor END,
		model=CASE WHEN model='' THEN ? ELSE model END,
		function=CASE WHEN function='' THEN ? ELSE function END
		WHERE id=?`,
		f.Name, f.Name, f.Kind, f.Kind, f.Notes, f.Vendor, f.Model, f.Function, id)
	return err
}

// AddDeviceTags attaches the named tags to a device, creating any that do not
// exist, and leaves its other tags alone (compare SetDeviceTags, which
// replaces the set).
func (s *Store) AddDeviceTags(ctx context.Context, deviceID int64, names []string) error {
	return s.withTx(ctx, func(c conn) error {
		for _, n := range names {
			if n = strings.TrimSpace(n); n == "" {
				continue
			}
			id, err := s.findOrCreateTagOn(ctx, c, n)
			if err != nil {
				return err
			}
			if _, err := s.execOn(ctx, c,
				`INSERT INTO device_tag (device_id,tag_id) VALUES (?,?) ON CONFLICT DO NOTHING`,
				deviceID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ExportIface is one interface with its addresses, for the inventory export.
type ExportIface struct {
	DeviceID int64
	MAC      *string
	Hostname *string
	IPs      []ExportIP
}

// ExportIP is one address assignment with the subnet it belongs to.
type ExportIP struct {
	IP     string
	Kind   string
	Subnet string
}

// ListExportIfaces returns every interface with its IP assignments, grouped
// by device id and in interface order, in one query. ListDevices flattens a
// device's addresses; an export wants to keep which MAC holds which IP.
func (s *Store) ListExportIfaces(ctx context.Context) (map[int64][]ExportIface, error) {
	out := make(map[int64][]ExportIface)
	var lastIface int64 = -1
	err := s.eachRow(ctx, `SELECT f.id, f.device_id, f.mac, f.hostname, a.ip, a.kind, sn.cidr
		FROM iface f
		LEFT JOIN ip_assignment a ON a.iface_id=f.id
		LEFT JOIN subnet sn ON sn.id=a.subnet_id
		ORDER BY f.device_id, f.id, a.id`,
		func(rows *sql.Rows) error {
			var ifID, devID int64
			var mac, host, ip, kind, cidr *string
			if err := rows.Scan(&ifID, &devID, &mac, &host, &ip, &kind, &cidr); err != nil {
				return err
			}
			if ifID != lastIface {
				out[devID] = append(out[devID], ExportIface{DeviceID: devID, MAC: mac, Hostname: host})
				lastIface = ifID
			}
			if ip != nil {
				list := out[devID]
				e := ExportIP{IP: *ip}
				if kind != nil {
					e.Kind = *kind
				}
				if cidr != nil {
					e.Subnet = *cidr
				}
				list[len(list)-1].IPs = append(list[len(list)-1].IPs, e)
			}
			return nil
		})
	return out, err
}
