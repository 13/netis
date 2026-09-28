package store

import (
	"context"
	"database/sql"
	"time"
)

// UpstreamScope names the devices an integration is authoritative for: the
// ones it created and still recognises by their upstream key.
type UpstreamScope string

const (
	// ScopeProxmoxGuests is every Proxmox guest (a proxmox device with a
	// VMID). Nodes are not included: the sync derives them from the guest
	// list, so a node without guests is not evidence that it is gone.
	ScopeProxmoxGuests UpstreamScope = "proxmox"
	// ScopeWireGuardPeers is every WireGuard peer (a wireguard device with a
	// public key).
	ScopeWireGuardPeers UpstreamScope = "wireguard"
)

func (sc UpstreamScope) where() string {
	switch sc {
	case ScopeProxmoxGuests:
		return `source='proxmox' AND proxmox_vmid IS NOT NULL`
	case ScopeWireGuardPeers:
		return `source='wireguard' AND wg_pubkey IS NOT NULL`
	}
	return `FALSE`
}

// UpstreamChange is a device whose upstream presence changed in a reconcile.
type UpstreamChange struct {
	ID   int64
	Name string
}

// ReconcileUpstream records which of an integration's devices its latest
// complete upstream list contained. seen holds the ids the sync resolved from
// that list. A device in scope that is not in seen and was not already marked
// is marked missing as of now and reported in gone; a marked device that is in
// seen again is cleared and reported in back. Devices already marked and still
// missing are left alone, so each transition is reported once. Nothing is
// deleted. It all runs in one transaction.
//
// Callers must only pass a list they trust to be complete: an empty or partial
// list would mark live devices missing.
func (s *Store) ReconcileUpstream(ctx context.Context, scope UpstreamScope, seen map[int64]bool,
	now time.Time) (gone, back []UpstreamChange, err error) {
	ts := now.UTC().Format(time.RFC3339)
	err = s.withTx(ctx, func(c conn) error {
		gone, back = nil, nil
		rows, err := c.QueryContext(ctx, s.dialect.rebind(
			`SELECT id, name, upstream_missing_since FROM device WHERE `+scope.where()))
		if err != nil {
			return err
		}
		for rows.Next() {
			var ch UpstreamChange
			var since sql.NullString
			if err := rows.Scan(&ch.ID, &ch.Name, &since); err != nil {
				rows.Close()
				return err
			}
			switch {
			case seen[ch.ID] && since.Valid:
				back = append(back, ch)
			case !seen[ch.ID] && !since.Valid:
				gone = append(gone, ch)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, ch := range gone {
			if _, err := s.execOn(ctx, c, `UPDATE device SET upstream_missing_since=? WHERE id=?`, ts, ch.ID); err != nil {
				return err
			}
		}
		for _, ch := range back {
			if _, err := s.execOn(ctx, c, `UPDATE device SET upstream_missing_since=NULL WHERE id=?`, ch.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return gone, back, nil
}

// SubnetIP is one address in one subnet.
type SubnetIP struct {
	SubnetID int64
	IP       string
}

// SyncIntegrationIPs makes the IP assignments an integration owns on an
// interface match want. Each wanted address the interface does not have yet is
// added as a static assignment labelled with source; assignments labelled with
// source that are no longer wanted are removed. Assignments with any other
// source — the user's own, or another integration's — are never touched, and
// an address the interface already has keeps its row and label as they are.
// It all runs in one transaction.
func (s *Store) SyncIntegrationIPs(ctx context.Context, ifaceID int64, source string,
	want []SubnetIP) (added, removed int, err error) {
	err = s.withTx(ctx, func(c conn) error {
		added, removed = 0, 0
		type row struct {
			id     int64
			source string
		}
		have := make(map[SubnetIP]row)
		rows, err := c.QueryContext(ctx, s.dialect.rebind(
			`SELECT id, subnet_id, ip, source FROM ip_assignment WHERE iface_id=?`), ifaceID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k SubnetIP
			var r row
			if err := rows.Scan(&r.id, &k.SubnetID, &k.IP, &r.source); err != nil {
				rows.Close()
				return err
			}
			have[k] = r
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		wanted := make(map[SubnetIP]bool, len(want))
		for _, k := range want {
			if wanted[k] {
				continue
			}
			wanted[k] = true
			if _, ok := have[k]; ok {
				continue
			}
			if _, err := s.execOn(ctx, c,
				`INSERT INTO ip_assignment (iface_id,subnet_id,ip,kind,source) VALUES (?,?,?,'static',?)`,
				ifaceID, k.SubnetID, k.IP, source); err != nil {
				return err
			}
			added++
		}
		for k, r := range have {
			if r.source != source || wanted[k] {
				continue
			}
			if _, err := s.execOn(ctx, c, `DELETE FROM ip_assignment WHERE id=?`, r.id); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return added, removed, nil
}
