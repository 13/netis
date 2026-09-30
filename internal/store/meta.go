package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

type Tag struct {
	ID    int64
	Name  string
	Color string
}

// MaxTagName is the longest tag name, in characters.
const MaxTagName = 64

// TagNameMsg is what every writer answers when a tag name is empty or too
// long.
const TagNameMsg = "a tag name is 1 to 64 characters"

// ValidTagName trims name and reports whether it is 1 to MaxTagName
// characters. It is the one rule every path that names a tag checks.
func ValidTagName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= MaxTagName
}

// TagNamesFit reports whether every name in a list a device's tags are set
// from is short enough. Blank names pass: the writers drop them.
func TagNamesFit(names []string) bool {
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			if _, ok := ValidTagName(n); !ok {
				return false
			}
		}
	}
	return true
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
	return s.insertReturningID(ctx, `INSERT INTO tag (name,color) VALUES (?,?)`, name, color)
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.query(ctx, `SELECT id,name,color FROM tag ORDER BY name`)
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

// TagCount is a tag with the number of devices carrying it.
type TagCount struct {
	Tag
	Devices int
}

// ListTagsWithCounts returns every tag, ordered by name, with how many
// devices carry it; tags on no device are included with a count of zero.
func (s *Store) ListTagsWithCounts(ctx context.Context) ([]TagCount, error) {
	var out []TagCount
	err := s.eachRow(ctx, `SELECT t.id,t.name,t.color,COUNT(dt.device_id) FROM tag t
		LEFT JOIN device_tag dt ON dt.tag_id=t.id
		GROUP BY t.id,t.name,t.color ORDER BY t.name`, func(r *sql.Rows) error {
		var tc TagCount
		if err := r.Scan(&tc.ID, &tc.Name, &tc.Color, &tc.Devices); err != nil {
			return err
		}
		out = append(out, tc)
		return nil
	})
	return out, err
}

// SetTagColor stores a tag's colour: a palette key, or "" for auto. The web
// layer validates the key. A missing tag is sql.ErrNoRows.
func (s *Store) SetTagColor(ctx context.Context, id int64, color string) error {
	return s.withTx(ctx, func(c conn) error {
		res, err := s.execOn(ctx, c, `UPDATE tag SET color=? WHERE id=?`, color, id)
		if err != nil {
			return err
		}
		return requireRow(res)
	})
}

// ErrTagExists is RenameTag refusing to merge into a tag that already has
// the name when the caller did not ask for a merge.
var ErrTagExists = errors.New("a tag with that name already exists")

// RenameTag renames tag id. If another tag already has name and merge is
// set, id's devices join that tag and id is deleted; mergedInto is that
// tag's id (0 for a plain rename). Without merge that case is ErrTagExists
// and nothing changes. Renaming to its own name is a no-op. A missing tag is
// sql.ErrNoRows. The name is matched exactly, as tags are unique by exact
// name.
func (s *Store) RenameTag(ctx context.Context, id int64, name string, merge bool) (mergedInto int64, err error) {
	err = s.withTx(ctx, func(c conn) error {
		var cur string
		if err := c.QueryRowContext(ctx, s.dialect.rebind(`SELECT name FROM tag WHERE id=?`), id).Scan(&cur); err != nil {
			return err
		}
		if cur == name {
			return nil
		}
		var other int64
		err := c.QueryRowContext(ctx, s.dialect.rebind(`SELECT id FROM tag WHERE name=?`), name).Scan(&other)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err := s.execOn(ctx, c, `UPDATE tag SET name=? WHERE id=?`, name, id)
			return err
		case err != nil:
			return err
		case !merge:
			return ErrTagExists
		}
		// Merge: every device of id joins other (devices already on both keep
		// a single row), then id goes. Its device_tag rows are removed
		// explicitly rather than trusting the cascade to be enabled.
		if _, err := s.execOn(ctx, c, `INSERT INTO device_tag (device_id,tag_id)
			SELECT device_id,? FROM device_tag WHERE tag_id=? ON CONFLICT DO NOTHING`, other, id); err != nil {
			return err
		}
		if _, err := s.execOn(ctx, c, `DELETE FROM device_tag WHERE tag_id=?`, id); err != nil {
			return err
		}
		if _, err := s.execOn(ctx, c, `DELETE FROM tag WHERE id=?`, id); err != nil {
			return err
		}
		mergedInto = other
		return nil
	})
	if err != nil {
		return 0, err
	}
	return mergedInto, nil
}

// DeleteTag detaches a tag from every device and deletes it. A missing tag
// is sql.ErrNoRows.
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(c conn) error {
		if _, err := s.execOn(ctx, c, `DELETE FROM device_tag WHERE tag_id=?`, id); err != nil {
			return err
		}
		res, err := s.execOn(ctx, c, `DELETE FROM tag WHERE id=?`, id)
		if err != nil {
			return err
		}
		return requireRow(res)
	})
}

// requireRow turns an UPDATE or DELETE that touched nothing into
// sql.ErrNoRows.
func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) TagDevice(ctx context.Context, deviceID, tagID int64) error {
	_, err := s.exec(ctx, `INSERT INTO device_tag (device_id,tag_id) VALUES (?,?) ON CONFLICT DO NOTHING`,
		deviceID, tagID)
	return err
}

func (s *Store) UntagDevice(ctx context.Context, deviceID, tagID int64) error {
	_, err := s.exec(ctx, `DELETE FROM device_tag WHERE device_id=? AND tag_id=?`, deviceID, tagID)
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

	// The detaches and attaches go in one transaction: a failure partway
	// through would otherwise leave the device with some of its old tags
	// removed and none of its new ones added, which is a state the user never
	// asked for and cannot tell apart from a successful edit.
	return s.withTx(ctx, func(c conn) error {
		for name, id := range have {
			if want[name] {
				continue
			}
			if _, err := s.execOn(ctx, c,
				`DELETE FROM device_tag WHERE device_id=? AND tag_id=?`, deviceID, id); err != nil {
				return err
			}
		}
		for name := range want {
			if _, ok := have[name]; ok {
				continue
			}
			id, err := s.findOrCreateTagOn(ctx, c, name)
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

// defaultTagColor is what an auto-created tag gets until someone picks one:
// the empty string means auto, drawn in the palette hue its name hashes to (views.TagColor).
const defaultTagColor = ""

// findOrCreateTagOn returns the id of the tag with the given name, creating it
// with the auto colour if it does not exist yet.
//
// It looks the name up directly rather than scanning the whole tag table, which
// is what SetDeviceTags used to do once per name. The insert tolerates a
// concurrent creator via ON CONFLICT and re-reads, so two requests attaching
// the same new tag cannot make one of them fail.
// findOrCreateTagOn takes an explicit connection so it can run inside the
// caller's transaction; SetDeviceTags is its only caller and always has one.
func (s *Store) findOrCreateTagOn(ctx context.Context, c conn, name string) (int64, error) {
	var id int64
	err := c.QueryRowContext(ctx, s.dialect.rebind(`SELECT id FROM tag WHERE name=?`), name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if _, err := s.execOn(ctx, c,
		`INSERT INTO tag (name,color) VALUES (?,?) ON CONFLICT(name) DO NOTHING`,
		name, defaultTagColor); err != nil {
		return 0, err
	}
	err = c.QueryRowContext(ctx, s.dialect.rebind(`SELECT id FROM tag WHERE name=?`), name).Scan(&id)
	return id, err
}

func (s *Store) SetCustomField(ctx context.Context, deviceID int64, key, value string) error {
	_, err := s.exec(ctx, `INSERT INTO custom_field (device_id,key,value) VALUES (?,?,?)
		ON CONFLICT(device_id,key) DO UPDATE SET value=excluded.value`, deviceID, key, value)
	return err
}

func (s *Store) DeleteCustomField(ctx context.Context, deviceID int64, key string) error {
	_, err := s.exec(ctx, `DELETE FROM custom_field WHERE device_id=? AND key=?`, deviceID, key)
	return err
}

func (s *Store) ListCustomFields(ctx context.Context, deviceID int64) ([]CustomField, error) {
	rows, err := s.query(ctx, `SELECT key,value FROM custom_field WHERE device_id=? ORDER BY key`, deviceID)
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
	return s.insertReturningID(ctx, `INSERT INTO device_link (device_id,label,url) VALUES (?,?,?)`,
		deviceID, label, url)
}

func (s *Store) DeleteLink(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM device_link WHERE id=?`, id)
	return err
}

func (s *Store) ListLinks(ctx context.Context, deviceID int64) ([]Link, error) {
	rows, err := s.query(ctx, `SELECT id,label,url FROM device_link WHERE device_id=?`, deviceID)
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
	_, err := s.exec(ctx, `INSERT INTO open_port (iface_id,port,proto,service_guess,first_seen,last_seen)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(iface_id,port,proto) DO UPDATE SET
			last_seen=excluded.last_seen, service_guess=excluded.service_guess`,
		ifaceID, port, proto, guess, seenAt, seenAt)
	return err
}

// ReplaceOpenPorts makes the iface's recorded open ports for proto exactly the
// given set, as found by a port scan at seenAt: ports still open keep their
// first_seen, new ones are added, and ports no longer open are deleted. It all
// happens in one transaction, so a failure never leaves half a result.
func (s *Store) ReplaceOpenPorts(ctx context.Context, ifaceID int64, proto string, ports []OpenPort, seenAt string) error {
	return s.withTx(ctx, func(c conn) error {
		keep := make([]string, 0, len(ports))
		args := []any{ifaceID, proto}
		for _, p := range ports {
			if _, err := s.execOn(ctx, c, `INSERT INTO open_port (iface_id,port,proto,service_guess,first_seen,last_seen)
				VALUES (?,?,?,?,?,?)
				ON CONFLICT(iface_id,port,proto) DO UPDATE SET
					last_seen=excluded.last_seen, service_guess=excluded.service_guess`,
				ifaceID, p.Port, proto, p.ServiceGuess, seenAt, seenAt); err != nil {
				return err
			}
			keep = append(keep, "?")
			args = append(args, p.Port)
		}
		q := `DELETE FROM open_port WHERE iface_id=? AND proto=?`
		if len(keep) > 0 {
			q += ` AND port NOT IN (` + strings.Join(keep, ",") + `)`
		}
		_, err := s.execOn(ctx, c, q, args...)
		return err
	})
}

func (s *Store) ListOpenPorts(ctx context.Context, ifaceID int64) ([]OpenPort, error) {
	rows, err := s.query(ctx, `SELECT port,proto,service_guess,first_seen,last_seen
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
