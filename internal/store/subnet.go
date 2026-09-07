package store

import "context"

type Subnet struct {
	ID              int64
	CIDR            string
	Name            string
	VLANID          *int64
	Kind            string
	ScanEnabled     bool
	ScanIntervalSec int
}

func (s *Store) CreateSubnet(ctx context.Context, sn Subnet) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO subnet (cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec)
		VALUES (?,?,?,?,?,?)`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec)
}

func (s *Store) GetSubnet(ctx context.Context, id int64) (Subnet, error) {
	var sn Subnet
	err := s.queryRow(ctx, `SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec
		FROM subnet WHERE id=?`, id).
		Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind, &sn.ScanEnabled, &sn.ScanIntervalSec)
	return sn, err
}

func (s *Store) ListSubnets(ctx context.Context) ([]Subnet, error) {
	rows, err := s.query(ctx, `SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec
		FROM subnet ORDER BY cidr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subnet
	for rows.Next() {
		var sn Subnet
		if err := rows.Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind,
			&sn.ScanEnabled, &sn.ScanIntervalSec); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSubnet(ctx context.Context, sn Subnet) error {
	_, err := s.exec(ctx, `UPDATE subnet SET cidr=?,name=?,vlan_id=?,kind=?,scan_enabled=?,scan_interval_sec=?
		WHERE id=?`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec, sn.ID)
	return err
}

func (s *Store) DeleteSubnet(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM subnet WHERE id=?`, id)
	return err
}
