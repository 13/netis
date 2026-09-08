package store

import (
	"context"
	"fmt"
	"time"
)

// RetentionResult counts what one retention sweep removed.
type RetentionResult struct {
	Sessions     int64
	Events       int64
	Availability int64
}

// Total is the number of rows the sweep deleted.
func (r RetentionResult) Total() int64 { return r.Sessions + r.Events + r.Availability }

// PruneExpiredSessions deletes sessions whose expiry has passed. Expired
// sessions are already refused at login, so this only stops the table growing
// forever — but a session row is a bearer token at rest, and there is no reason
// to keep one that can no longer be used.
func (s *Store) PruneExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM session WHERE expires_at <= ?`,
		now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneEvents deletes events older than the cutoff.
func (s *Store) PruneEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM event WHERE ts < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneAvailability deletes availability buckets that start before the cutoff.
// These are the fastest-growing rows in the database: one per interface per
// hour, so a hundred interfaces produce nearly a million rows a year.
func (s *Store) PruneAvailability(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM availability_history WHERE bucket_start < ?`,
		cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Prune runs one retention sweep. A retention of zero days disables that
// category, so a user who wants to keep everything can, and the default stays
// a bounded database.
func (s *Store) Prune(ctx context.Context, now time.Time, eventDays, availabilityDays int) (RetentionResult, error) {
	var r RetentionResult
	var err error
	if r.Sessions, err = s.PruneExpiredSessions(ctx, now); err != nil {
		return r, fmt.Errorf("prune sessions: %w", err)
	}
	if eventDays > 0 {
		if r.Events, err = s.PruneEvents(ctx, now.AddDate(0, 0, -eventDays)); err != nil {
			return r, fmt.Errorf("prune events: %w", err)
		}
	}
	if availabilityDays > 0 {
		if r.Availability, err = s.PruneAvailability(ctx, now.AddDate(0, 0, -availabilityDays)); err != nil {
			return r, fmt.Errorf("prune availability: %w", err)
		}
	}
	return r, nil
}
