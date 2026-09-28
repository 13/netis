-- AdGuard Home and OPNsense are DHCP lease sources like Pi-hole, and a device
-- one of them discovers is labelled with it.
--
-- SQLite cannot alter a CHECK constraint, so the device table is rebuilt.
-- Foreign keys are off while migrations run, so dropping the old table does
-- not cascade into iface, device_tag and the rest; the migration runner checks
-- every reference afterwards. Every current column is listed: a column added
-- to device by an earlier migration and missing here would be dropped
-- (TestDeviceRebuildKeepsAddedColumns guards that). Dropping the table drops
-- its indexes, so the 0009 unique indexes are recreated.
CREATE TABLE device_new (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','router','modem','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN
    ('manual','scan','proxmox','wireguard','pihole','adguard','opnsense')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  reviewed INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  function TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  alert_offline INTEGER NOT NULL DEFAULT 0,
  upstream_missing_since TEXT
);
INSERT INTO device_new
  (id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,
   reviewed,model,function,created_at,alert_offline,upstream_missing_since)
  SELECT
   id,name,kind,notes,vendor,source,parent_device_id,proxmox_vmid,wg_pubkey,icon,
   reviewed,model,function,created_at,alert_offline,upstream_missing_since
  FROM device;
DROP TABLE device;
ALTER TABLE device_new RENAME TO device;
CREATE UNIQUE INDEX idx_device_wg_pubkey ON device(wg_pubkey) WHERE wg_pubkey IS NOT NULL;
CREATE UNIQUE INDEX idx_device_proxmox_vmid ON device(proxmox_vmid) WHERE proxmox_vmid IS NOT NULL;
