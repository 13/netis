package store

import (
	"context"
	"strings"
)

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

func (s *Store) CreateTag(ctx context.Context, name, color string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO tag (name,color) VALUES (?,?)`, name, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,color FROM tag ORDER BY name`)
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

func (s *Store) TagDevice(ctx context.Context, deviceID, tagID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO device_tag (device_id,tag_id) VALUES (?,?)`,
		deviceID, tagID)
	return err
}

func (s *Store) UntagDevice(ctx context.Context, deviceID, tagID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM device_tag WHERE device_id=? AND tag_id=?`, deviceID, tagID)
	return err
}

// SetDeviceTags syncs a device's tags to exactly the given names: names are
// trimmed, blanks dropped, and de-duplicated; tags that don't exist are
// created (color #888888); tags no longer present are detached. Idempotent
// and order-independent.
func (s *Store) SetDeviceTags(ctx context.Context, deviceID int64, names []string) error {
	// Build the desired set (trim, drop empty, dedup).
	want := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			want[n] = true
		}
	}

	current, err := s.DeviceTags(ctx, deviceID)
	if err != nil {
		return err
	}
	have := map[string]int64{} // name -> tag id, currently attached
	for _, t := range current {
		have[t.Name] = t.ID
	}

	// Detach tags no longer wanted.
	for name, id := range have {
		if !want[name] {
			if err := s.UntagDevice(ctx, deviceID, id); err != nil {
				return err
			}
		}
	}

	// Attach wanted tags not already present (find-or-create).
	for name := range want {
		if _, ok := have[name]; ok {
			continue
		}
		id, err := s.findOrCreateTag(ctx, name)
		if err != nil {
			return err
		}
		if err := s.TagDevice(ctx, deviceID, id); err != nil {
			return err
		}
	}
	return nil
}

// findOrCreateTag returns the id of the tag with the given name, creating it
// with a neutral color if it does not exist yet.
func (s *Store) findOrCreateTag(ctx context.Context, name string) (int64, error) {
	tags, err := s.ListTags(ctx)
	if err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == name {
			return t.ID, nil
		}
	}
	return s.CreateTag(ctx, name, "#888888")
}

func (s *Store) SetCustomField(ctx context.Context, deviceID int64, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO custom_field (device_id,key,value) VALUES (?,?,?)
		ON CONFLICT(device_id,key) DO UPDATE SET value=excluded.value`, deviceID, key, value)
	return err
}

func (s *Store) DeleteCustomField(ctx context.Context, deviceID int64, key string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM custom_field WHERE device_id=? AND key=?`, deviceID, key)
	return err
}

func (s *Store) ListCustomFields(ctx context.Context, deviceID int64) ([]CustomField, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key,value FROM custom_field WHERE device_id=? ORDER BY key`, deviceID)
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

func (s *Store) AddLink(ctx context.Context, deviceID int64, label, url string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO device_link (device_id,label,url) VALUES (?,?,?)`,
		deviceID, label, url)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteLink(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM device_link WHERE id=?`, id)
	return err
}

func (s *Store) ListLinks(ctx context.Context, deviceID int64) ([]Link, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,label,url FROM device_link WHERE device_id=?`, deviceID)
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

func (s *Store) UpsertOpenPort(ctx context.Context, ifaceID int64, port int, proto, guess, seenAt string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO open_port (iface_id,port,proto,service_guess,first_seen,last_seen)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(iface_id,port,proto) DO UPDATE SET
			last_seen=excluded.last_seen, service_guess=excluded.service_guess`,
		ifaceID, port, proto, guess, seenAt, seenAt)
	return err
}

func (s *Store) ListOpenPorts(ctx context.Context, ifaceID int64) ([]OpenPort, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT port,proto,service_guess,first_seen,last_seen
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
