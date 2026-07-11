package store

import (
	"database/sql"
	"errors"
)

type Device struct {
	ID             int64
	Name           string
	Kind           string
	Notes          string
	Vendor         string
	Source         string
	ParentDeviceID *int64
	ProxmoxVMID    *int64
	WGPubKey       *string
	Icon           string
}

type Iface struct {
	ID       int64
	DeviceID int64
	MAC      *string
	Hostname *string
}

type IPRow struct {
	ID       int64
	IP       string
	SubnetID int64
	Kind     string
}

type DeviceRow struct {
	Device
	IPs      []string
	MACs     []string
	Online   bool
	LastSeen *string
	TagNames []string
}

const deviceCols = `id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Notes, &d.Vendor, &d.Source,
		&d.ParentDeviceID, &d.ProxmoxVMID, &d.WGPubKey, &d.Icon)
	return d, err
}

func (s *Store) CreateDevice(d Device) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO device (name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source, d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetDevice(id int64) (Device, error) {
	return scanDevice(s.DB.QueryRow(`SELECT `+deviceCols+` FROM device WHERE id=?`, id))
}

func (s *Store) UpdateDevice(d Device) error {
	_, err := s.DB.Exec(`UPDATE device SET name=?,kind=?,notes=?,vendor=?,source=?,
		parent_device_id=?,proxmox_vmid=?,wg_pubkey=?,icon=? WHERE id=?`,
		d.Name, d.Kind, d.Notes, d.Vendor, d.Source,
		d.ParentDeviceID, d.ProxmoxVMID, d.WGPubKey, d.Icon, d.ID)
	return err
}

func (s *Store) DeleteDevice(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM device WHERE id=?`, id)
	return err
}

func (s *Store) AddIface(deviceID int64, mac, hostname *string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO iface (device_id,mac,hostname) VALUES (?,?,?)`,
		deviceID, mac, hostname)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListIfaces(deviceID int64) ([]Iface, error) {
	rows, err := s.DB.Query(`SELECT id,device_id,mac,hostname FROM iface WHERE device_id=?`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Iface
	for rows.Next() {
		var i Iface
		if err := rows.Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) FindIfaceByMAC(mac string) (Iface, bool, error) {
	var i Iface
	err := s.DB.QueryRow(`SELECT id,device_id,mac,hostname FROM iface WHERE mac=?`, mac).
		Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return i, false, nil
		}
		return i, false, err
	}
	return i, true, nil
}

func (s *Store) FindIfaceByIP(subnetID int64, ip string) (Iface, bool, error) {
	var i Iface
	err := s.DB.QueryRow(`SELECT f.id,f.device_id,f.mac,f.hostname FROM iface f
		JOIN ip_assignment a ON a.iface_id=f.id WHERE a.subnet_id=? AND a.ip=?`, subnetID, ip).
		Scan(&i.ID, &i.DeviceID, &i.MAC, &i.Hostname)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return i, false, nil
		}
		return i, false, err
	}
	return i, true, nil
}

func (s *Store) AssignIP(ifaceID, subnetID int64, ip, kind string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO ip_assignment (iface_id,subnet_id,ip,kind) VALUES (?,?,?,?)`,
		ifaceID, subnetID, ip, kind)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RemoveIfaceIPsInSubnetExcept retires an iface's stale DHCP IP assignments
// in a subnet, keeping only keepIP. Used when a MAC-matched iface is observed
// on a new IP so the old IP no longer lingers in the subnet occupancy/grid.
// Only kind='dhcp' rows are removed: a user-assigned static IP on the same
// iface must survive a DHCP renewal to a different address.
func (s *Store) RemoveIfaceIPsInSubnetExcept(ifaceID, subnetID int64, keepIP string) error {
	_, err := s.DB.Exec(`DELETE FROM ip_assignment WHERE iface_id=? AND subnet_id=? AND ip<>? AND kind='dhcp'`,
		ifaceID, subnetID, keepIP)
	return err
}

func (s *Store) ListIPs(ifaceID int64) ([]IPRow, error) {
	rows, err := s.DB.Query(`SELECT id,ip,subnet_id,kind FROM ip_assignment WHERE iface_id=?`, ifaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPRow
	for rows.Next() {
		var r IPRow
		if err := rows.Scan(&r.ID, &r.IP, &r.SubnetID, &r.Kind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListDevices() ([]DeviceRow, error) {
	devRows, err := s.DB.Query(`SELECT ` + deviceCols + ` FROM device ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer devRows.Close()
	var out []DeviceRow
	for devRows.Next() {
		d, err := scanDevice(devRows)
		if err != nil {
			return nil, err
		}
		out = append(out, DeviceRow{Device: d})
	}
	if err := devRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ifaces, err := s.ListIfaces(out[i].ID)
		if err != nil {
			return nil, err
		}
		for _, f := range ifaces {
			if f.MAC != nil {
				out[i].MACs = append(out[i].MACs, *f.MAC)
			}
			ips, err := s.ListIPs(f.ID)
			if err != nil {
				return nil, err
			}
			for _, p := range ips {
				out[i].IPs = append(out[i].IPs, p.IP)
			}
			online, lastSeen, err := s.ifaceOnline(f.ID)
			if err != nil {
				return nil, err
			}
			if online {
				out[i].Online = true
			}
			if lastSeen != nil && (out[i].LastSeen == nil || *lastSeen > *out[i].LastSeen) {
				out[i].LastSeen = lastSeen
			}
		}
		tags, err := s.deviceTagNames(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].TagNames = tags
	}
	return out, nil
}

type SubnetIfaceIP struct {
	IfaceID  int64
	DeviceID int64
	IP       string
	MAC      *string
}

func (s *Store) ListSubnetIfaceIPs(subnetID int64) ([]SubnetIfaceIP, error) {
	rows, err := s.DB.Query(`SELECT f.id, f.device_id, a.ip, f.mac
		FROM ip_assignment a JOIN iface f ON f.id=a.iface_id
		WHERE a.subnet_id=?`, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubnetIfaceIP
	for rows.Next() {
		var r SubnetIfaceIP
		if err := rows.Scan(&r.IfaceID, &r.DeviceID, &r.IP, &r.MAC); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// IfaceOnline reports whether an iface is currently online along with its
// last-seen timestamp, for callers outside the store package (e.g. the
// device detail page).
func (s *Store) IfaceOnline(ifaceID int64) (bool, *string, error) {
	return s.ifaceOnline(ifaceID)
}

// ifaceOnline and deviceTagNames get real implementations in Task 4;
// these stubs keep Task 3 self-contained.
func (s *Store) ifaceOnline(ifaceID int64) (bool, *string, error) {
	var online bool
	var lastSeen *string
	err := s.DB.QueryRow(`SELECT online,last_seen FROM iface_status WHERE iface_id=?`, ifaceID).
		Scan(&online, &lastSeen)
	if err != nil {
		return false, nil, nil // no status row yet
	}
	return online, lastSeen, nil
}

// ListDeviceEvents returns the most recent events for a single device, same
// shape as ListEvents but filtered to deviceID.
func (s *Store) ListDeviceEvents(deviceID int64, limit int) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT id,ts,type,device_id,details FROM event
		WHERE device_id=? ORDER BY id DESC LIMIT ?`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.DeviceID, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListChildren returns devices whose parent_device_id is deviceID.
func (s *Store) ListChildren(deviceID int64) ([]Device, error) {
	rows, err := s.DB.Query(`SELECT `+deviceCols+` FROM device WHERE parent_device_id=? ORDER BY name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeviceTags returns the full Tag rows attached to a device (compare
// deviceTagNames, which only returns names for the filterable list view).
func (s *Store) DeviceTags(deviceID int64) ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT t.id,t.name,t.color FROM tag t
		JOIN device_tag dt ON dt.tag_id=t.id WHERE dt.device_id=? ORDER BY t.name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) deviceTagNames(deviceID int64) ([]string, error) {
	rows, err := s.DB.Query(`SELECT t.name FROM tag t
		JOIN device_tag dt ON dt.tag_id=t.id WHERE dt.device_id=? ORDER BY t.name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
