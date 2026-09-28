package store

import (
	"context"
	"time"
)

// AuditEntry is one row of the audit log: an attempted change, who made it,
// from where, and the HTTP status it was answered with.
type AuditEntry struct {
	ID int64
	At time.Time
	// UserID is nil for an anonymous request (a failed login) and once the
	// account has been deleted; Username is a snapshot that outlives it.
	UserID   *int64
	Username string
	Action   string
	Target   string
	Detail   string
	IP       string
	Status   int
}

// AuditFilter narrows ListAudit. Empty strings match everything. BeforeID is
// the keyset cursor: only entries with a smaller id are returned, so a page
// stays stable while new entries arrive at the top.
type AuditFilter struct {
	Username string
	Action   string
	BeforeID int64
	Limit    int
}

// AddAudit records one audit entry. A zero At means now.
func (s *Store) AddAudit(ctx context.Context, e AuditEntry) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := s.exec(ctx, `INSERT INTO audit_log (at,user_id,username,action,target,detail,ip,status)
		VALUES (?,?,?,?,?,?,?,?)`,
		e.At.UTC().Format(time.RFC3339), e.UserID, e.Username, e.Action, e.Target, e.Detail, e.IP, e.Status)
	return err
}

// ListAudit returns entries matching f, newest first, and whether there are
// more beyond the page.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, bool, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	q := `SELECT id,at,user_id,username,action,target,detail,ip,status FROM audit_log WHERE 1=1`
	var args []any
	if f.Username != "" {
		q += ` AND username=?`
		args = append(args, f.Username)
	}
	if f.Action != "" {
		q += ` AND action=?`
		args = append(args, f.Action)
	}
	if f.BeforeID > 0 {
		q += ` AND id<?`
		args = append(args, f.BeforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit+1)
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var at string
		if err := rows.Scan(&e.ID, &at, &e.UserID, &e.Username, &e.Action, &e.Target, &e.Detail, &e.IP, &e.Status); err != nil {
			return nil, false, err
		}
		e.At, _ = time.Parse(time.RFC3339, at)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}

// AuditFacets returns the distinct non-empty usernames and actions in the log,
// sorted, for the filter menus.
func (s *Store) AuditFacets(ctx context.Context) (users, actions []string, err error) {
	if users, err = s.distinctAudit(ctx, "username"); err != nil {
		return nil, nil, err
	}
	actions, err = s.distinctAudit(ctx, "action")
	return users, actions, err
}

// distinctAudit lists one column's distinct values. col is always a literal
// from AuditFacets, never user input.
func (s *Store) distinctAudit(ctx context.Context, col string) ([]string, error) {
	rows, err := s.query(ctx, `SELECT DISTINCT `+col+` FROM audit_log WHERE `+col+`<>'' ORDER BY `+col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PruneAudit deletes audit entries older than the cutoff.
func (s *Store) PruneAudit(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.exec(ctx, `DELETE FROM audit_log WHERE at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
