package store

import (
	"context"
	"fmt"
	"time"
)

// Hint is one source's observation that a device's field is probably value.
// See the autofill package for how hints become device values.
type Hint struct {
	DeviceID   int64
	Source     string
	Field      string
	Value      string
	Confidence int
	// Detail is the evidence, for people: "MAC 3c:07:54:…", "hostname BRW…".
	Detail string
	// SeenAt is when this (field, value, confidence, detail) content was
	// first observed, not when a pass last confirmed it: a pass that finds a
	// source's hints unchanged skips the write (and the seen_at bump) rather
	// than paying a transaction to restate the same content.
	SeenAt string
}

// Autofill record states: applied while the device still holds the value
// autofill wrote; owned once a person changed or removed it.
const (
	AutofillApplied = "applied"
	AutofillOwned   = "owned"
)

// AutofillRecord is what autofill wrote to one field (or tag:<name>) of a
// device, and whether that value is still autofill's.
type AutofillRecord struct {
	DeviceID  int64
	Field     string
	Value     string
	Source    string
	State     string
	UpdatedAt string
}

// AutofillWrite sets one device column, but only if it still holds Expect
// (and, with Unreviewed, only while the device is unreviewed), so a person's
// edit between the read and the write always wins.
type AutofillWrite struct {
	Field, Value, Source string
	Expect               string
	Unreviewed           bool
}

// AutofillTag attaches a tag the device does not have.
type AutofillTag struct{ Name, Source string }

// AutofillChanges is everything one autofill pass wants to do to a device.
type AutofillChanges struct {
	Writes []AutofillWrite
	Tags   []AutofillTag
	// Own lists record fields a person has taken over.
	Own []string
}

// Empty reports whether there is nothing to apply.
func (c AutofillChanges) Empty() bool {
	return len(c.Writes) == 0 && len(c.Tags) == 0 && len(c.Own) == 0
}

// autofillColumns are the device columns autofill may write. The map is also
// the guard that keeps a field name out of the SQL unless it is one of these.
var autofillColumns = map[string]string{
	"vendor": "vendor", "model": "model", "kind": "kind",
	"icon": "icon", "function": "function", "name": "name",
}

// ReplaceHints makes hints the complete set source has for the device.
func (s *Store) ReplaceHints(ctx context.Context, deviceID int64, source string, hints []Hint) error {
	return s.withTx(ctx, func(c conn) error {
		if _, err := s.execOn(ctx, c, `DELETE FROM device_hint WHERE device_id=? AND source=?`, deviceID, source); err != nil {
			return err
		}
		for _, h := range hints {
			if _, err := s.execOn(ctx, c, `INSERT INTO device_hint (device_id,source,field,value,confidence,detail,seen_at)
				VALUES (?,?,?,?,?,?,?)`, deviceID, source, h.Field, h.Value, h.Confidence, h.Detail, h.SeenAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListHints returns every hint for a device, by field, then confidence
// (highest first), then source and value.
func (s *Store) ListHints(ctx context.Context, deviceID int64) ([]Hint, error) {
	rows, err := s.query(ctx, `SELECT device_id,source,field,value,confidence,detail,seen_at
		FROM device_hint WHERE device_id=? ORDER BY field, confidence DESC, source, value`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hint
	for rows.Next() {
		var h Hint
		if err := rows.Scan(&h.DeviceID, &h.Source, &h.Field, &h.Value, &h.Confidence, &h.Detail, &h.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListAutofill returns the device's autofill records, by field.
func (s *Store) ListAutofill(ctx context.Context, deviceID int64) ([]AutofillRecord, error) {
	rows, err := s.query(ctx, `SELECT device_id,field,value,source,state,updated_at
		FROM device_autofill WHERE device_id=? ORDER BY field`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutofillRecord
	for rows.Next() {
		var r AutofillRecord
		if err := rows.Scan(&r.DeviceID, &r.Field, &r.Value, &r.Source, &r.State, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApplyAutofill applies ch in one transaction and returns the writes and
// tags that took effect. It never marks the device reviewed (compare
// UpdateDevice). A write whose Expect no longer matches is skipped silently:
// someone changed the field meanwhile, and the next pass will see that.
func (s *Store) ApplyAutofill(ctx context.Context, deviceID int64, ch AutofillChanges) ([]AutofillWrite, []AutofillTag, error) {
	for _, w := range ch.Writes {
		if _, ok := autofillColumns[w.Field]; !ok {
			return nil, nil, fmt.Errorf("autofill: unknown field %q", w.Field)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var writes []AutofillWrite
	var tags []AutofillTag
	err := s.withTx(ctx, func(c conn) error {
		writes, tags = nil, nil
		for _, f := range ch.Own {
			if _, err := s.execOn(ctx, c, `UPDATE device_autofill SET state=?, updated_at=? WHERE device_id=? AND field=?`,
				AutofillOwned, now, deviceID, f); err != nil {
				return err
			}
		}
		for _, w := range ch.Writes {
			col := autofillColumns[w.Field]
			q := `UPDATE device SET ` + col + `=? WHERE id=? AND ` + col + `=?`
			if w.Unreviewed {
				q += ` AND reviewed=FALSE`
			}
			res, err := s.execOn(ctx, c, q, w.Value, deviceID, w.Expect)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if err := s.recordAutofillOn(ctx, c, deviceID, w.Field, w.Value, w.Source, now); err != nil {
				return err
			}
			writes = append(writes, w)
		}
		for _, t := range ch.Tags {
			tagID, err := s.findOrCreateTagOn(ctx, c, t.Name)
			if err != nil {
				return err
			}
			res, err := s.execOn(ctx, c, `INSERT INTO device_tag (device_id,tag_id) VALUES (?,?) ON CONFLICT DO NOTHING`, deviceID, tagID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if err := s.recordAutofillOn(ctx, c, deviceID, "tag:"+t.Name, t.Name, t.Source, now); err != nil {
				return err
			}
			tags = append(tags, t)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return writes, tags, nil
}

func (s *Store) recordAutofillOn(ctx context.Context, c conn, deviceID int64, field, value, source, now string) error {
	_, err := s.execOn(ctx, c, `INSERT INTO device_autofill (device_id,field,value,source,state,updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(device_id,field) DO UPDATE SET value=excluded.value, source=excluded.source,
			state=excluded.state, updated_at=excluded.updated_at`,
		deviceID, field, value, source, AutofillApplied, now)
	return err
}

// DeviceIDs returns every device id, ascending.
func (s *Store) DeviceIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.query(ctx, `SELECT id FROM device ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
