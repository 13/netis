package store

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

func (s *Store) SubnetOccupancy(subnetID int64) (map[string]Occupant, error) {
	rows, err := s.DB.Query(`SELECT a.ip, f.id, d.id, d.name,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,0), st.first_seen IS NOT NULL, a.kind
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
// assignment) is not an error.
func (s *Store) SetIPKind(subnetID int64, ip, kind string) error {
	_, err := s.DB.Exec(`UPDATE ip_assignment SET kind=? WHERE subnet_id=? AND ip=?`,
		kind, subnetID, ip)
	return err
}
