package store

import "context"

type Occupant struct {
	DeviceID   int64
	DeviceName string
	MAC        string
	LastSeen   string
	Online     bool
	EverSeen   bool
	Count      int
	Kind       string
}

func (s *Store) SubnetOccupancy(ctx context.Context, subnetID int64) (map[string]Occupant, error) {
	rows, err := s.query(ctx, `SELECT a.ip, f.id, d.id, d.name,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,FALSE), st.first_seen IS NOT NULL, a.kind
		FROM ip_assignment a
		JOIN iface f ON f.id=a.iface_id
		JOIN device d ON d.id=f.device_id
		LEFT JOIN iface_status st ON st.iface_id=f.id
		WHERE a.subnet_id=?`, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Occupant)
	ifacesByIP := make(map[string]map[int64]bool) // distinct ifaces per IP → conflict count
	for rows.Next() {
		var o Occupant
		var ip string
		var ifaceID int64
		if err := rows.Scan(&ip, &ifaceID, &o.DeviceID, &o.DeviceName, &o.MAC,
			&o.LastSeen, &o.Online, &o.EverSeen, &o.Kind); err != nil {
			return nil, err
		}
		if ifacesByIP[ip] == nil {
			ifacesByIP[ip] = make(map[int64]bool)
		}
		ifacesByIP[ip][ifaceID] = true
		if _, ok := out[ip]; !ok {
			out[ip] = o // first occupant's details represent the square
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for ip, o := range out {
		o.Count = len(ifacesByIP[ip]) // conflict when >1 distinct iface claims the IP
		out[ip] = o
	}
	return out, nil
}

// SetIPKind updates the lease kind (static/dhcp) of an assignment identified by
// its subnet and IP. Callers validate the kind; a 0-row update (no such
// assignment) is not an error. Setting the kind is a user edit, so the row
// stops belonging to the integration that created it (source is cleared) and
// no later sync removes it.
func (s *Store) SetIPKind(ctx context.Context, subnetID int64, ip, kind string) error {
	_, err := s.exec(ctx, `UPDATE ip_assignment SET kind=?, source='' WHERE subnet_id=? AND ip=?`,
		kind, subnetID, ip)
	return err
}

// IPClaim is one interface holding an address in a subnet, with the device
// kind and icon the grid's details panel draws it with.
type IPClaim struct {
	Occupant
	DeviceKind string
	DeviceIcon string
}

// IPClaims lists every interface holding ip in subnetID, oldest interface
// first. More than one is an IP conflict, and the details panel names each
// device involved rather than only the first. Count on each claim is the
// number of claims.
func (s *Store) IPClaims(ctx context.Context, subnetID int64, ip string) ([]IPClaim, error) {
	rows, err := s.query(ctx, `SELECT d.id, d.name, d.kind, d.icon,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,FALSE), st.first_seen IS NOT NULL, a.kind
		FROM ip_assignment a
		JOIN iface f ON f.id=a.iface_id
		JOIN device d ON d.id=f.device_id
		LEFT JOIN iface_status st ON st.iface_id=f.id
		WHERE a.subnet_id=? AND a.ip=?
		ORDER BY f.id`, subnetID, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPClaim
	for rows.Next() {
		var c IPClaim
		if err := rows.Scan(&c.DeviceID, &c.DeviceName, &c.DeviceKind, &c.DeviceIcon, &c.MAC,
			&c.LastSeen, &c.Online, &c.EverSeen, &c.Kind); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Count = len(out)
	}
	return out, nil
}
