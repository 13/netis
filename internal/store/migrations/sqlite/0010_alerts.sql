-- Alerts: a per-device opt-in for offline/online notifications (off by
-- default, so a home network full of phones coming and going stays quiet), and
-- two new event types — an IP claimed by more than one interface, and an
-- integration that was failing working again.
ALTER TABLE device ADD COLUMN alert_offline INTEGER NOT NULL DEFAULT 0;

-- SQLite cannot alter a CHECK constraint, so the event table is rebuilt.
-- Nothing references it, so the rebuild cannot cascade anywhere.
CREATE TABLE event_new (
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  type TEXT NOT NULL CHECK (type IN
    ('device_new','online','offline','ip_changed','scan_error','ip_conflict','sync_recovered')),
  device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  details TEXT NOT NULL DEFAULT ''
);
INSERT INTO event_new (id,ts,type,device_id,details)
  SELECT id,ts,type,device_id,details FROM event;
DROP TABLE event;
ALTER TABLE event_new RENAME TO event;
CREATE INDEX idx_event_ts ON event(ts DESC);
CREATE INDEX idx_event_device ON event(device_id);
