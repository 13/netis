package store

type Occupant struct {
	DeviceID   int64
	DeviceName string
	MAC        string
	LastSeen   string
	Online     bool
	EverSeen   bool
	Count      int
}

func (s *Store) SubnetOccupancy(subnetID int64) (map[string]Occupant, error) {
	rows, err := s.DB.Query(`SELECT a.ip, d.id, d.name,
			COALESCE(f.mac,''), COALESCE(st.last_seen,''),
			COALESCE(st.online,0), st.first_seen IS NOT NULL
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
	for rows.Next() {
		var o Occupant
		var ip string
		if err := rows.Scan(&ip, &o.DeviceID, &o.DeviceName, &o.MAC,
			&o.LastSeen, &o.Online, &o.EverSeen); err != nil {
			return nil, err
		}
		if prev, ok := out[ip]; ok {
			prev.Count++
			out[ip] = prev
			continue
		}
		o.Count = 1
		out[ip] = o
	}
	return out, rows.Err()
}
