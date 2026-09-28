package store

import "context"

// SetDeviceAlertOffline records whether the device's offline and online events
// should be sent as notifications. It is kept apart from UpdateDevice so that
// neither the edit form nor an integration sync can reset it by accident.
func (s *Store) SetDeviceAlertOffline(ctx context.Context, id int64, on bool) error {
	_, err := s.exec(ctx, `UPDATE device SET alert_offline=? WHERE id=?`, on, id)
	return err
}

// DeviceAlert returns a device's name and its alert-when-offline flag. A
// missing device is sql.ErrNoRows.
func (s *Store) DeviceAlert(ctx context.Context, id int64) (name string, alert bool, err error) {
	err = s.queryRow(ctx, `SELECT name, alert_offline FROM device WHERE id=?`, id).Scan(&name, &alert)
	return name, alert, err
}

// ConflictingIPs returns the addresses in a subnet claimed by more than one
// interface, each with the names of the devices claiming it (sorted, one per
// interface). The grid shows the same condition as a square with Count > 1.
func (s *Store) ConflictingIPs(ctx context.Context, subnetID int64) (map[string][]string, error) {
	rows, err := s.query(ctx, `SELECT a.ip, d.name
		FROM ip_assignment a
		JOIN iface f ON f.id=a.iface_id
		JOIN device d ON d.id=f.device_id
		WHERE a.subnet_id=? AND a.ip IN (
			SELECT ip FROM ip_assignment WHERE subnet_id=?
			GROUP BY ip HAVING COUNT(DISTINCT iface_id) > 1)
		ORDER BY a.ip, d.name`, subnetID, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var ip, name string
		if err := rows.Scan(&ip, &name); err != nil {
			return nil, err
		}
		out[ip] = append(out[ip], name)
	}
	return out, rows.Err()
}
