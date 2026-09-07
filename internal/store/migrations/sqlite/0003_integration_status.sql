CREATE TABLE integration_status (
  name TEXT PRIMARY KEY,
  last_run TEXT NOT NULL,
  ok INTEGER NOT NULL DEFAULT 0,
  detail TEXT NOT NULL DEFAULT '',
  item_count INTEGER NOT NULL DEFAULT 0
);
