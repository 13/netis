package store

type Tag struct {
	ID    int64
	Name  string
	Color string
}

type CustomField struct{ Key, Value string }

type Link struct {
	ID    int64
	Label string
	URL   string
}

type OpenPort struct {
	Port                                     int
	Proto, ServiceGuess, FirstSeen, LastSeen string
}

func (s *Store) CreateTag(name, color string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO tag (name,color) VALUES (?,?)`, name, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListTags() ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT id,name,color FROM tag ORDER BY name`)
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

func (s *Store) TagDevice(deviceID, tagID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO device_tag (device_id,tag_id) VALUES (?,?)`,
		deviceID, tagID)
	return err
}

func (s *Store) UntagDevice(deviceID, tagID int64) error {
	_, err := s.DB.Exec(`DELETE FROM device_tag WHERE device_id=? AND tag_id=?`, deviceID, tagID)
	return err
}

func (s *Store) SetCustomField(deviceID int64, key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO custom_field (device_id,key,value) VALUES (?,?,?)
		ON CONFLICT(device_id,key) DO UPDATE SET value=excluded.value`, deviceID, key, value)
	return err
}

func (s *Store) DeleteCustomField(deviceID int64, key string) error {
	_, err := s.DB.Exec(`DELETE FROM custom_field WHERE device_id=? AND key=?`, deviceID, key)
	return err
}

func (s *Store) ListCustomFields(deviceID int64) ([]CustomField, error) {
	rows, err := s.DB.Query(`SELECT key,value FROM custom_field WHERE device_id=? ORDER BY key`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomField
	for rows.Next() {
		var c CustomField
		if err := rows.Scan(&c.Key, &c.Value); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddLink(deviceID int64, label, url string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO device_link (device_id,label,url) VALUES (?,?,?)`,
		deviceID, label, url)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteLink(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM device_link WHERE id=?`, id)
	return err
}

func (s *Store) ListLinks(deviceID int64) ([]Link, error) {
	rows, err := s.DB.Query(`SELECT id,label,url FROM device_link WHERE device_id=?`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.Label, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) UpsertOpenPort(ifaceID int64, port int, proto, guess, seenAt string) error {
	_, err := s.DB.Exec(`INSERT INTO open_port (iface_id,port,proto,service_guess,first_seen,last_seen)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(iface_id,port,proto) DO UPDATE SET
			last_seen=excluded.last_seen, service_guess=excluded.service_guess`,
		ifaceID, port, proto, guess, seenAt, seenAt)
	return err
}

func (s *Store) ListOpenPorts(ifaceID int64) ([]OpenPort, error) {
	rows, err := s.DB.Query(`SELECT port,proto,service_guess,first_seen,last_seen
		FROM open_port WHERE iface_id=? ORDER BY port`, ifaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenPort
	for rows.Next() {
		var p OpenPort
		if err := rows.Scan(&p.Port, &p.Proto, &p.ServiceGuess, &p.FirstSeen, &p.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
