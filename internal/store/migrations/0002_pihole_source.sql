CREATE TABLE device_new (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scan','proxmox','wireguard','pihole')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
INSERT INTO device_new SELECT * FROM device;
DROP TABLE device;
ALTER TABLE device_new RENAME TO device;
