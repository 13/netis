-- Alerts: a per-device opt-in for offline/online notifications (off by
-- default, so a home network full of phones coming and going stays quiet), and
-- two new event types — an IP claimed by more than one interface, and an
-- integration that was failing working again.
ALTER TABLE device ADD COLUMN alert_offline BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE event DROP CONSTRAINT event_type_check;
ALTER TABLE event ADD CONSTRAINT event_type_check CHECK (type IN
  ('device_new','online','offline','ip_changed','scan_error','ip_conflict','sync_recovered'));
