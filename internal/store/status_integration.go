package store

import "context"

type IntegrationStatus struct {
	Name      string
	LastRun   string
	Detail    string
	OK        bool
	ItemCount int
}

func (s *Store) SetIntegrationStatus(ctx context.Context, st IntegrationStatus) error {
	_, err := s.exec(ctx, `INSERT INTO integration_status (name,last_run,ok,detail,item_count)
		VALUES (?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET
			last_run=excluded.last_run, ok=excluded.ok,
			detail=excluded.detail, item_count=excluded.item_count`,
		st.Name, st.LastRun, st.OK, st.Detail, st.ItemCount)
	return err
}

func (s *Store) ListIntegrationStatus(ctx context.Context) ([]IntegrationStatus, error) {
	rows, err := s.query(ctx, `SELECT name,last_run,ok,detail,item_count
		FROM integration_status ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntegrationStatus
	for rows.Next() {
		var st IntegrationStatus
		if err := rows.Scan(&st.Name, &st.LastRun, &st.OK, &st.Detail, &st.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
