package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) MarkSeen(ctx context.Context, ifaceID int64, rttMS float64, at time.Time) (bool, error) {
	ts := at.UTC().Format(time.RFC3339)
	var online bool
	err := s.DB.QueryRowContext(ctx, `SELECT online FROM iface_status WHERE iface_id=?`, ifaceID).Scan(&online)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	wasOffline := err != nil || !online
	_, err = s.DB.ExecContext(ctx, `INSERT INTO iface_status (iface_id,online,first_seen,last_seen,last_rtt_ms,missed_sweeps)
		VALUES (?,1,?,?,?,0)
		ON CONFLICT(iface_id) DO UPDATE SET
			online=1, last_seen=excluded.last_seen, last_rtt_ms=excluded.last_rtt_ms,
			missed_sweeps=0, first_seen=COALESCE(iface_status.first_seen, excluded.first_seen)`,
		ifaceID, ts, ts, rttMS)
	return wasOffline, err
}

func (s *Store) MarkMissed(ctx context.Context, ifaceID int64, offlineAfter int) (bool, error) {
	var online bool
	var missed int
	err := s.DB.QueryRowContext(ctx, `SELECT online,missed_sweeps FROM iface_status WHERE iface_id=?`, ifaceID).
		Scan(&online, &missed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // never seen: nothing to mark
	}
	if err != nil {
		return false, err
	}
	missed++
	wentOffline := online && missed >= offlineAfter
	_, err = s.DB.ExecContext(ctx, `UPDATE iface_status SET missed_sweeps=?, online=CASE WHEN ?>=? THEN 0 ELSE online END
		WHERE iface_id=?`, missed, missed, offlineAfter, ifaceID)
	return wentOffline, err
}

func (s *Store) RecordAvailability(ctx context.Context, ifaceID int64, up bool, bucketStart string) error {
	upN := 0
	if up {
		upN = 1
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO availability_history (iface_id,bucket_start,up_count,total_count)
		VALUES (?,?,?,1)
		ON CONFLICT(iface_id,bucket_start) DO UPDATE SET
			up_count=up_count+excluded.up_count, total_count=total_count+1`,
		ifaceID, bucketStart, upN)
	return err
}

func (s *Store) AvailabilityPct(ctx context.Context, ifaceID int64, sinceBucket string) (float64, error) {
	var up, total int
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(up_count),0), COALESCE(SUM(total_count),0)
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
