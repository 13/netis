-- Audit log: who did what, from where, and whether it was allowed.
--
-- user_id is nulled rather than cascaded when the account goes, and username
-- is a snapshot, so an entry keeps naming its actor after the account is
-- deleted. at is written by the application as UTC RFC3339, like the other
-- time columns. status is the HTTP status the request was answered with, so a
-- refused attempt is told apart from one that took effect.
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY,
  at TEXT NOT NULL,
  user_id INTEGER REFERENCES "user"(id) ON DELETE SET NULL,
  username TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_audit_log_at ON audit_log(at);
CREATE INDEX idx_audit_log_username ON audit_log(username, id);
CREATE INDEX idx_audit_log_action ON audit_log(action, id);
