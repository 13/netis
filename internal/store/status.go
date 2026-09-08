package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MarkSeen records a successful ping and reports whether this is a transition
// from offline, which is what decides if an "online" event is emitted.
//
// The read and the upsert are two statements rather than one. There is no
// portable single statement that upserts and returns the pre-update value —
// Postgres can do it with an xmax trick, SQLite cannot — and the race the two
// statements would otherwise open is closed by the caller: Scheduler.run is
// invoked synchronously from one goroutine, so sweeps never overlap and no two
// callers touch the same interface at once. The extra round trip is also noise
// next to the ICMP sweep that produced this result.
func (s *Store) MarkSeen(ctx context.Context, ifaceID int64, rttMS float64, at time.Time) (bool, error) {
	ts := at.UTC().Format(time.RFC3339)
	var online bool
	err := s.queryRow(ctx, `SELECT online FROM iface_status WHERE iface_id=?`, ifaceID).Scan(&online)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	wasOffline := err != nil || !online
	_, err = s.exec(ctx, `INSERT INTO iface_status (iface_id,online,first_seen,last_seen,last_rtt_ms,missed_sweeps)
		VALUES (?,TRUE,?,?,?,0)
		ON CONFLICT(iface_id) DO UPDATE SET
			online=TRUE, last_seen=excluded.last_seen, last_rtt_ms=excluded.last_rtt_ms,
			missed_sweeps=0, first_seen=COALESCE(iface_status.first_seen, excluded.first_seen)`,
		ifaceID, ts, ts, rttMS)
	return wasOffline, err
}

func (s *Store) MarkMissed(ctx context.Context, ifaceID int64, offlineAfter int) (bool, error) {
	var online bool
	var missed int
	err := s.queryRow(ctx, `SELECT online,missed_sweeps FROM iface_status WHERE iface_id=?`, ifaceID).
		Scan(&online, &missed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // never seen: nothing to mark
	}
	if err != nil {
		return false, err
	}
	missed++
	wentOffline := online && missed >= offlineAfter
	// The new online state is decided here rather than in a SQL CASE: the row's
	// current values are already in hand, and a bare `?>=?` comparison gives
	// Postgres no type to infer the parameters from.
	stillOnline := online && missed < offlineAfter
	_, err = s.exec(ctx, `UPDATE iface_status SET missed_sweeps=?, online=? WHERE iface_id=?`,
		missed, stillOnline, ifaceID)
	return wentOffline, err
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
