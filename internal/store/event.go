package store

import "context"

type Event struct {
	ID       int64
	TS       string
	Type     string
	DeviceID *int64
	Details  string
}

func (s *Store) AddEvent(ctx context.Context, typ string, deviceID *int64, details string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO event (type,device_id,details) VALUES (?,?,?)`,
		typ, deviceID, details)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,ts,type,device_id,details FROM event
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &e.DeviceID, &e.Details); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
