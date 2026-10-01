package store

import (
	"context"
	"net/netip"
	"sort"
)

type Subnet struct {
	ID              int64
	CIDR            string
	Name            string
	VLANID          *int64
	Kind            string
	ScanEnabled     bool
	ScanIntervalSec int
	// DHCPStart and DHCPEnd bound the subnet's DHCP pool, both inclusive;
	// both are empty when the pool is not known.
	DHCPStart string
	DHCPEnd   string
	// DHCPPoolSource says who set the pool: "" for nobody, "user", or the
	// DHCP integration that read it (see SetDHCPPoolFrom).
	DHCPPoolSource string
}

// InDHCPPool reports whether ip lies in the subnet's DHCP pool. It is false
// for every address when no pool is set.
func (sn Subnet) InDHCPPool(ip netip.Addr) bool {
	lo, err1 := netip.ParseAddr(sn.DHCPStart)
	hi, err2 := netip.ParseAddr(sn.DHCPEnd)
	if err1 != nil || err2 != nil {
		return false
	}
	return lo.Compare(ip) <= 0 && ip.Compare(hi) <= 0
}

func (s *Store) CreateSubnet(ctx context.Context, sn Subnet) (int64, error) {
	return s.insertReturningID(ctx, `INSERT INTO subnet (cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec,dhcp_start,dhcp_end,dhcp_pool_source)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec, sn.DHCPStart, sn.DHCPEnd, sn.DHCPPoolSource)
}

func (s *Store) GetSubnet(ctx context.Context, id int64) (Subnet, error) {
	var sn Subnet
	err := s.queryRow(ctx, `SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec,dhcp_start,dhcp_end,dhcp_pool_source
		FROM subnet WHERE id=?`, id).
		Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind, &sn.ScanEnabled, &sn.ScanIntervalSec, &sn.DHCPStart, &sn.DHCPEnd, &sn.DHCPPoolSource)
	return sn, err
}

func (s *Store) ListSubnets(ctx context.Context) ([]Subnet, error) {
	rows, err := s.query(ctx, `SELECT id,cidr,name,vlan_id,kind,scan_enabled,scan_interval_sec,dhcp_start,dhcp_end,dhcp_pool_source
		FROM subnet`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subnet
	for rows.Next() {
		var sn Subnet
		if err := rows.Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.VLANID, &sn.Kind,
			&sn.ScanEnabled, &sn.ScanIntervalSec, &sn.DHCPStart, &sn.DHCPEnd, &sn.DHCPPoolSource); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortSubnets(out)
	return out, nil
}

// sortSubnets orders subnets by network address, numerically rather than as
// text (10.6.0.0/24 before 10.10.10.0/24), IPv4 before IPv6, and a shorter
// prefix before a longer one at the same address. A CIDR that does not parse
// sorts last, by its text.
func sortSubnets(sns []Subnet) {
	key := func(sn Subnet) (netip.Prefix, bool) {
		p, err := netip.ParsePrefix(sn.CIDR)
		return p.Masked(), err == nil
	}
	sort.SliceStable(sns, func(i, j int) bool {
		a, aok := key(sns[i])
		b, bok := key(sns[j])
		switch {
		case aok != bok:
			return aok
		case !aok:
			return sns[i].CIDR < sns[j].CIDR
		case a.Addr().Is4() != b.Addr().Is4():
			return a.Addr().Is4()
		case a.Addr() != b.Addr():
			return a.Addr().Less(b.Addr())
		default:
			return a.Bits() < b.Bits()
		}
	})
}

func (s *Store) UpdateSubnet(ctx context.Context, sn Subnet) error {
	_, err := s.exec(ctx, `UPDATE subnet SET cidr=?,name=?,vlan_id=?,kind=?,scan_enabled=?,scan_interval_sec=?,dhcp_start=?,dhcp_end=?,dhcp_pool_source=?
		WHERE id=?`,
		sn.CIDR, sn.Name, sn.VLANID, sn.Kind, sn.ScanEnabled, sn.ScanIntervalSec, sn.DHCPStart, sn.DHCPEnd, sn.DHCPPoolSource, sn.ID)
	return err
}

// SetDHCPPoolFrom records the DHCP pool an integration (source) read from its
// server, but only while the subnet's pool is unowned or already that
// integration's: a pool the user typed, or another integration's, is left
// alone. It reports whether the stored pool changed.
func (s *Store) SetDHCPPoolFrom(ctx context.Context, subnetID int64, start, end, source string) (bool, error) {
	res, err := s.exec(ctx, `UPDATE subnet SET dhcp_start=?,dhcp_end=?,dhcp_pool_source=?
		WHERE id=? AND dhcp_pool_source IN ('',?) AND (dhcp_start<>? OR dhcp_end<>? OR dhcp_pool_source<>?)`,
		start, end, source, subnetID, source, start, end, source)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) DeleteSubnet(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM subnet WHERE id=?`, id)
	return err
}
