-- Device autofill. device_hint holds what each source observed about a
-- device; a source replaces its own rows each time it runs. device_autofill
-- holds what autofill wrote to a device, so a later change by a person is
-- recognised (value differs) and the field becomes theirs (state owned).
-- field there is a device column name, or tag:<name> for a tag.
CREATE TABLE device_hint (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  field TEXT NOT NULL,
  value TEXT NOT NULL,
  confidence INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  seen_at TEXT NOT NULL,
  PRIMARY KEY (device_id, source, field, value)
);
CREATE TABLE device_autofill (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  field TEXT NOT NULL,
  value TEXT NOT NULL,
  source TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('applied','owned')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (device_id, field)
);
