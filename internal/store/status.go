package store

import (
	"context"
	"time"
)

// MarkSeen records a successful ping and reports whether this is a transition
// from offline, which is what decides if an "online" event is emitted.
//
// Subnets are swept in parallel and one interface can have addresses in
// several of them, so two sweeps may mark the same interface at once. Each
// step is therefore a single conditional statement whose row count says
// whether this caller made the transition: the insert only lands for a
// never-seen interface, and the flip only matches a row that is still
// offline, so exactly one concurrent caller reports it. There is no portable
// single statement that upserts and returns the pre-update value, hence the
// sequence.
func (s *Store) MarkSeen(ctx context.Context, ifaceID int64, rttMS float64, at time.Time) (bool, error) {
	ts := at.UTC().Format(time.RFC3339)
	res, err := s.exec(ctx, `INSERT INTO iface_status (iface_id,online,first_seen,last_seen,last_rtt_ms,missed_sweeps)
		VALUES (?,TRUE,?,?,?,0)
		ON CONFLICT(iface_id) DO NOTHING`,
		ifaceID, ts, ts, rttMS)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 1 {
		return n == 1, err
	}
	res, err = s.exec(ctx, `UPDATE iface_status SET
			online=TRUE, last_seen=?, last_rtt_ms=?, missed_sweeps=0, first_seen=COALESCE(first_seen, ?)
		WHERE iface_id=? AND online=FALSE`,
		ts, rttMS, ts, ifaceID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 1 {
		return n == 1, err
	}
	_, err = s.exec(ctx, `UPDATE iface_status SET last_seen=?, last_rtt_ms=?, missed_sweeps=0 WHERE iface_id=?`,
		ts, rttMS, ifaceID)
	return false, err
}

// MarkMissed counts a sweep that did not see the interface and reports
// whether this miss took it offline. Like MarkSeen it never reads and then
// writes: the counter is incremented in SQL, and the offline flip only
// matches a row that is still online, so concurrent sweeps neither lose a
// miss nor report the transition twice.
func (s *Store) MarkMissed(ctx context.Context, ifaceID int64, offlineAfter int) (bool, error) {
	res, err := s.exec(ctx, `UPDATE iface_status SET missed_sweeps=missed_sweeps+1 WHERE iface_id=?`, ifaceID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err // never seen: nothing to mark
	}
	res, err = s.exec(ctx, `UPDATE iface_status SET online=FALSE
		WHERE iface_id=? AND online=TRUE AND missed_sweeps>=?`, ifaceID, offlineAfter)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) RecordAvailability(ctx context.Context, ifaceID int64, up bool, bucketStart string) error {
	upN := 0
	if up {
		upN = 1
	}
	_, err := s.exec(ctx, `INSERT INTO availability_history (iface_id,bucket_start,up_count,total_count)
		VALUES (?,?,?,1)
		ON CONFLICT(iface_id,bucket_start) DO UPDATE SET
			up_count=availability_history.up_count+excluded.up_count,
			total_count=availability_history.total_count+1`,
		ifaceID, bucketStart, upN)
	return err
}

func (s *Store) AvailabilityPct(ctx context.Context, ifaceID int64, sinceBucket string) (float64, error) {
	var up, total int
	err := s.queryRow(ctx, `SELECT COALESCE(SUM(up_count),0), COALESCE(SUM(total_count),0)
		FROM availability_history WHERE iface_id=? AND bucket_start>=?`, ifaceID, sinceBucket).
		Scan(&up, &total)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 100, nil
	}
	return 100 * float64(up) / float64(total), nil
}
