package store

import (
	"context"
	"database/sql"
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
