package store

import (
	"context"
	"database/sql"
	"strings"
)

type Event struct {
	ID       int64
	TS       string
	Type     string
	DeviceID *int64
	// DeviceName is the device's current name, nil when the event has no
	// device or the device has since been deleted.
	DeviceName *string
	Details    string
}

// eventCols selects an event with its device's name; queries alias event as e
// and LEFT JOIN device as d.
const eventCols = `e.id,e.ts,e.type,e.device_id,d.name,e.details`

func scanEvents(rows *sql.Rows) ([]Event, error) {
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.DeviceID, &e.DeviceName, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AddEvent(ctx context.Context, typ string, deviceID *int64, details string) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO event (type,device_id,details) VALUES (?,?,?)`,
		typ, deviceID, details)
}

func (s *Store) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.query(ctx, `SELECT `+eventCols+` FROM event e LEFT JOIN device d ON d.id=e.device_id
		ORDER BY e.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// EventFilter narrows ListEventsFiltered. Zero fields do not filter.
type EventFilter struct {
	// Type is an event type as stored (device_new, scan_error, ...).
	Type string
	// DetailPrefixes keeps only events whose details start with one of these;
	// NotDetailPrefixes drops those that start with any of these. Together
	// they split one stored type into the kinds the UI names apart (a failed
	// scan and a failed integration sync are both scan_error).
	DetailPrefixes    []string
	NotDetailPrefixes []string
	// Device matches part of the device's current name, ignoring case.
	Device string
	// Since and Until bound the timestamp, Since inclusive and Until
	// exclusive, as RFC3339 UTC strings like the stored ones.
	Since, Until string
	// BeforeID pages backwards: only events older than this id.
	BeforeID int64
	Limit    int
}

// likeEscape escapes s for use inside a LIKE pattern with ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListEventsFiltered returns the events matching f, newest first, and
// whether there are more beyond the page.
func (s *Store) ListEventsFiltered(ctx context.Context, f EventFilter) ([]Event, bool, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	q := `SELECT ` + eventCols + ` FROM event e LEFT JOIN device d ON d.id=e.device_id WHERE 1=1`
	var args []any
	if f.Type != "" {
		q += ` AND e.type=?`
		args = append(args, f.Type)
	}
	if len(f.DetailPrefixes) > 0 {
		ors := make([]string, 0, len(f.DetailPrefixes))
		for _, p := range f.DetailPrefixes {
			ors = append(ors, `e.details LIKE ? ESCAPE '\'`)
			args = append(args, likeEscape(p)+"%")
		}
		q += ` AND (` + strings.Join(ors, ` OR `) + `)`
	}
	for _, p := range f.NotDetailPrefixes {
		q += ` AND e.details NOT LIKE ? ESCAPE '\'`
		args = append(args, likeEscape(p)+"%")
	}
	if f.Device != "" {
		q += ` AND LOWER(d.name) LIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscape(strings.ToLower(f.Device))+"%")
	}
	if f.Since != "" {
		q += ` AND e.ts>=?`
		args = append(args, f.Since)
	}
	if f.Until != "" {
		q += ` AND e.ts<?`
		args = append(args, f.Until)
	}
	if f.BeforeID > 0 {
		q += ` AND e.id<?`
		args = append(args, f.BeforeID)
	}
	q += ` ORDER BY e.id DESC LIMIT ?`
	args = append(args, f.Limit+1)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	out, err := scanEvents(rows)
	if err != nil {
		return nil, false, err
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}
