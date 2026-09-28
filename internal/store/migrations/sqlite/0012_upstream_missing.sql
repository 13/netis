-- Integration devices that vanished upstream, and which IPs a sync owns.
--
-- upstream_missing_since is when a Proxmox guest or WireGuard peer was first
-- absent from a complete list returned by its integration; NULL while it is
-- present. Only the syncs write it: the device is kept, never deleted.
ALTER TABLE device ADD COLUMN upstream_missing_since TEXT;

-- source says which integration created an IP assignment and may therefore
-- remove it again ('' = the user, or not known). The WireGuard sync has always
-- created static rows for its peers inside WireGuard subnets, so those are the
-- rows it owns.
ALTER TABLE ip_assignment ADD COLUMN source TEXT NOT NULL DEFAULT '';
UPDATE ip_assignment SET source = 'wireguard'
WHERE kind = 'static'
  AND subnet_id IN (SELECT id FROM subnet WHERE kind = 'wireguard')
  AND iface_id IN (SELECT f.id FROM iface f JOIN device d ON d.id = f.device_id
                   WHERE d.source = 'wireguard');

-- New event types for a device leaving and returning upstream, added to the
-- list 0010 (alerts) left behind.
-- SQLite cannot alter a CHECK constraint, so the event table is rebuilt.
-- Nothing references it, so the rebuild cannot cascade anywhere.
CREATE TABLE event_new (
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  type TEXT NOT NULL CHECK (type IN
    ('device_new','online','offline','ip_changed','scan_error','ip_conflict','sync_recovered',
     'device_missing','device_returned')),
  device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  details TEXT NOT NULL DEFAULT ''
);
INSERT INTO event_new (id,ts,type,device_id,details)
  SELECT id,ts,type,device_id,details FROM event;
DROP TABLE event;
ALTER TABLE event_new RENAME TO event;
CREATE INDEX idx_event_ts ON event(ts DESC);
CREATE INDEX idx_event_device ON event(device_id);
