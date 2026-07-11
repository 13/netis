CREATE TABLE subnet (
  id INTEGER PRIMARY KEY,
  cidr TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  vlan_id INTEGER,
  kind TEXT NOT NULL DEFAULT 'lan' CHECK (kind IN ('lan','wireguard','proxmox-bridge')),
  scan_enabled INTEGER NOT NULL DEFAULT 1,
  scan_interval_sec INTEGER NOT NULL DEFAULT 120
);

CREATE TABLE device (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'other' CHECK (kind IN
    ('computer','switch','phone','server','printer','iot','vm','lxc','wg-peer','other')),
  notes TEXT NOT NULL DEFAULT '',
  vendor TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','scan','proxmox','wireguard')),
  parent_device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  proxmox_vmid INTEGER,
  wg_pubkey TEXT,
  icon TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE iface (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  mac TEXT,
  hostname TEXT
);
CREATE UNIQUE INDEX idx_iface_mac ON iface(mac) WHERE mac IS NOT NULL;

CREATE TABLE ip_assignment (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  subnet_id INTEGER NOT NULL REFERENCES subnet(id) ON DELETE CASCADE,
  ip TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'dhcp' CHECK (kind IN ('static','dhcp'))
);
CREATE INDEX idx_ip_subnet ON ip_assignment(subnet_id, ip);

CREATE TABLE iface_status (
  iface_id INTEGER PRIMARY KEY REFERENCES iface(id) ON DELETE CASCADE,
  online INTEGER NOT NULL DEFAULT 0,
  first_seen TEXT,
  last_seen TEXT,
  last_rtt_ms REAL,
  missed_sweeps INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE availability_history (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  bucket_start TEXT NOT NULL,
  up_count INTEGER NOT NULL DEFAULT 0,
  total_count INTEGER NOT NULL DEFAULT 0,
  UNIQUE (iface_id, bucket_start)
);

CREATE TABLE open_port (
  id INTEGER PRIMARY KEY,
  iface_id INTEGER NOT NULL REFERENCES iface(id) ON DELETE CASCADE,
  port INTEGER NOT NULL,
  proto TEXT NOT NULL DEFAULT 'tcp' CHECK (proto IN ('tcp','udp')),
  service_guess TEXT NOT NULL DEFAULT '',
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  UNIQUE (iface_id, port, proto)
);

CREATE TABLE tag (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  color TEXT NOT NULL DEFAULT '#888888'
);

CREATE TABLE device_tag (
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tag(id) ON DELETE CASCADE,
  PRIMARY KEY (device_id, tag_id)
);

CREATE TABLE custom_field (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  UNIQUE (device_id, key)
);

CREATE TABLE device_link (
  id INTEGER PRIMARY KEY,
  device_id INTEGER NOT NULL REFERENCES device(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  url TEXT NOT NULL
);

CREATE TABLE event (
  id INTEGER PRIMARY KEY,
  ts TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  type TEXT NOT NULL CHECK (type IN ('device_new','online','offline','ip_changed','scan_error')),
  device_id INTEGER REFERENCES device(id) ON DELETE SET NULL,
  details TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_event_ts ON event(ts DESC);

CREATE TABLE user (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('admin','viewer'))
);

CREATE TABLE session (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE setting (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
