-- The visual regression fixture's data (e2e/, TestE2EServe). Every value is
-- fixed, timestamps included: the fixture freezes the clock at
-- 2026-09-15T12:00:00Z, so relative times, day headings and availability
-- bars render the same on every run. SQLite only.

INSERT INTO setting (key, value) VALUES ('onboarded', '1');

INSERT INTO subnet (id, cidr, name, vlan_id, kind, scan_enabled, scan_interval_sec, dhcp_start, dhcp_end) VALUES
  (1, '192.168.1.0/24', 'LAN', NULL, 'lan', 1, 300, '192.168.1.100', '192.168.1.199'),
  (2, '10.0.20.0/24', 'IoT', 20, 'lan', 1, 600, '', ''),
  (3, '10.10.10.0/24', 'Proxmox vmbr0', NULL, 'proxmox-bridge', 1, 300, '', ''),
  (4, '10.6.0.0/24', 'WireGuard', NULL, 'wireguard', 0, 120, '', '');

INSERT INTO device (id, name, kind, vendor, model, function, source, parent_device_id, proxmox_vmid, wg_pubkey, reviewed, alert_offline, upstream_missing_since, notes, created_at) VALUES
  (1, 'opnsense', 'router', 'PC Engines', 'APU2', 'Firewall and DHCP', 'manual', NULL, NULL, NULL, 1, 1, NULL, '', '2026-03-01T09:00:00Z'),
  (2, 'core-switch', 'switch', 'Ubiquiti', 'USW-24-PoE', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-01T09:00:00Z'),
  (3, 'ap-living', 'other', 'Ubiquiti', 'U6-Lite', 'Wi-Fi', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-01T09:00:00Z'),
  (4, 'pve1', 'server', 'ASRock', 'DeskMini X300', 'Hypervisor', 'proxmox', NULL, NULL, NULL, 1, 1, NULL, '', '2026-03-02T09:00:00Z'),
  (5, 'nas', 'server', 'Synology', 'DS920+', 'Storage and backups', 'manual', NULL, NULL, NULL, 1, 1, NULL, 'Scrub runs on the first Sunday of the month.', '2026-03-02T09:00:00Z'),
  (6, 'homeassistant', 'vm', '', '', 'Home automation', 'proxmox', 4, 101, NULL, 1, 0, NULL, '', '2026-03-05T09:00:00Z'),
  (7, 'pihole', 'lxc', '', '', 'DNS', 'proxmox', 4, 102, NULL, 1, 0, NULL, '', '2026-03-05T09:00:00Z'),
  (8, 'k3s-node-1', 'vm', '', '', '', 'proxmox', 4, 103, NULL, 1, 0, '2026-09-15T04:00:00Z', '', '2026-04-01T09:00:00Z'),
  (9, 'printer', 'printer', 'Brother', 'HL-L2350DW', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-10T09:00:00Z'),
  (10, 'ben-laptop', 'computer', 'Framework', 'Laptop 13', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-10T09:00:00Z'),
  (11, 'desktop', 'computer', '', 'Custom build', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-10T09:00:00Z'),
  (12, 'pixel-8', 'phone', 'Google', 'Pixel 8', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-12T09:00:00Z'),
  (13, 'iphone-anna', 'phone', 'Apple', '', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-12T09:00:00Z'),
  (14, 'living-room-tv', 'other', 'LG', 'OLED C2', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-12T09:00:00Z'),
  (15, 'hue-bridge', 'iot', 'Signify', 'Hue Bridge v2', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-15T09:00:00Z'),
  (16, 'thermostat', 'iot', 'Google', 'Nest', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-15T09:00:00Z'),
  (17, 'doorbell-cam', 'iot', 'Reolink', '', '', 'manual', NULL, NULL, NULL, 1, 0, NULL, '', '2026-03-15T09:00:00Z'),
  (18, 'wg-ben-phone', 'wg-peer', '', '', '', 'wireguard', NULL, NULL, 'bQ3sP1xk9mYt2fHc7vLr8nWz4gJd6aKe0uRi5oTy1Es=', 1, 0, NULL, '', '2026-04-01T09:00:00Z'),
  (19, 'wg-parents', 'wg-peer', '', '', '', 'wireguard', NULL, NULL, 'Zk8fQ2mXa7pLc4vNr9tHs1wBj6dGy3eUo0iKq5lTn2A=', 1, 0, NULL, '', '2026-04-01T09:00:00Z'),
  (20, '192.168.1.61', 'other', '', '', '', 'scan', NULL, NULL, NULL, 0, 0, NULL, '', '2026-09-15T07:34:00Z'),
  (21, 'espressif-112', 'other', 'Espressif', '', '', 'scan', NULL, NULL, NULL, 0, 0, NULL, '', '2026-09-15T07:19:00Z'),
  (22, 'tuya-140', 'other', 'Tuya Smart', '', '', 'opnsense', NULL, NULL, NULL, 0, 0, NULL, '', '2026-09-15T02:00:00Z');

-- One interface per device, with the device's id.
INSERT INTO iface (id, device_id, mac, hostname) VALUES
  (1, 1, '00:0d:b9:4a:10:01', 'opnsense'),
  (2, 2, '74:83:c2:10:20:02', NULL),
  (3, 3, '74:83:c2:10:20:03', NULL),
  (4, 4, 'a8:a1:59:30:40:04', 'pve1'),
  (5, 5, '00:11:32:50:60:05', 'nas'),
  (6, 6, 'bc:24:11:aa:00:06', 'homeassistant'),
  (7, 7, 'bc:24:11:aa:00:07', 'pihole'),
  (8, 8, 'bc:24:11:aa:00:08', 'k3s-node-1'),
  (9, 9, '3c:2a:f4:70:80:09', 'BRN3C2AF4'),
  (10, 10, 'f8:e4:3b:90:a0:10', 'ben-laptop'),
  (11, 11, '04:42:1a:b0:c0:11', 'desktop'),
  (12, 12, '6a:1f:22:33:44:12', 'Pixel-8'),
  (13, 13, 'da:a1:19:00:00:13', NULL),
  (14, 14, '58:fd:b1:d0:e0:14', 'LGwebOSTV'),
  (15, 15, 'ec:b5:fa:00:10:15', 'Philips-hue'),
  (16, 16, '18:b4:30:00:20:16', NULL),
  (17, 17, 'ec:71:db:00:30:17', 'Reolink'),
  (18, 18, NULL, NULL),
  (19, 19, NULL, NULL),
  (20, 20, '8e:00:11:22:33:20', NULL),
  (21, 21, '3c:61:05:aa:bb:21', 'espressif'),
  (22, 22, 'd8:1f:12:00:11:22', NULL);

INSERT INTO ip_assignment (iface_id, subnet_id, ip, kind, source) VALUES
  (1, 1, '192.168.1.1', 'static', ''),
  (2, 1, '192.168.1.2', 'static', ''),
  (3, 1, '192.168.1.3', 'static', ''),
  (4, 1, '192.168.1.10', 'static', ''),
  (4, 3, '10.10.10.1', 'static', 'proxmox'),
  (5, 1, '192.168.1.20', 'static', ''),
  (6, 3, '10.10.10.11', 'static', 'proxmox'),
  (7, 3, '10.10.10.13', 'static', 'proxmox'),
  (8, 3, '10.10.10.15', 'static', 'proxmox'),
  (9, 1, '192.168.1.30', 'static', ''),
  (10, 1, '192.168.1.50', 'dhcp', ''),
  (11, 1, '192.168.1.51', 'dhcp', ''),
  (12, 1, '192.168.1.60', 'dhcp', ''),
  (13, 1, '192.168.1.61', 'dhcp', ''),
  (14, 1, '192.168.1.70', 'dhcp', ''),
  (15, 2, '10.0.20.21', 'static', ''),
  (16, 2, '10.0.20.24', 'dhcp', ''),
  (17, 2, '10.0.20.25', 'dhcp', ''),
  (18, 4, '10.6.0.2', 'static', 'wireguard'),
  (19, 4, '10.6.0.4', 'static', 'wireguard'),
  (20, 1, '192.168.1.61', 'dhcp', 'scan'),
  (21, 1, '192.168.1.112', 'dhcp', 'scan'),
  (22, 2, '10.0.20.140', 'dhcp', 'opnsense');

-- Offline: k3s-node-1, ben-laptop, iphone-anna, wg-parents.
INSERT INTO iface_status (iface_id, online, first_seen, last_seen, last_rtt_ms, missed_sweeps)
SELECT id,
       CASE WHEN id IN (8, 10, 13, 19) THEN 0 ELSE 1 END,
       '2026-06-01T08:00:00Z',
       CASE id WHEN 8 THEN '2026-09-15T04:02:00Z'
               WHEN 10 THEN '2026-09-15T11:48:00Z'
               WHEN 13 THEN '2026-09-14T22:10:00Z'
               WHEN 19 THEN '2026-09-13T19:30:00Z'
               ELSE '2026-09-15T11:59:00Z' END,
       CASE WHEN id IN (8, 10, 13, 19) THEN NULL ELSE 0.4 + (id % 7) * 1.3 END,
       CASE WHEN id IN (8, 10, 13, 19) THEN 5 ELSE 0 END
FROM iface;

-- 30 days of hourly availability for every interface, from a fixed pattern:
-- the infrastructure is always up, the rest drop out now and then, and the
-- offline devices are down for their last hours.
WITH RECURSIVE h(n) AS (SELECT 0 UNION ALL SELECT n + 1 FROM h WHERE n < 719)
INSERT INTO availability_history (iface_id, bucket_start, up_count, total_count)
SELECT i.id,
       strftime('%Y-%m-%dT%H:00:00Z', '2026-09-15 12:00:00', '-' || h.n || ' hours'),
       CASE
         WHEN i.id IN (1, 2, 4, 5) THEN 30
         WHEN i.id = 8 AND h.n < 8 THEN 0
         WHEN i.id = 10 AND h.n < 1 THEN 0
         WHEN i.id = 13 AND h.n < 14 THEN 0
         WHEN i.id = 19 AND h.n < 41 THEN 0
         WHEN (h.n * 7 + i.id * 13) % 23 = 0 THEN 0
         WHEN (h.n * 5 + i.id * 3) % 11 = 0 THEN 18
         ELSE 30
       END,
       30
FROM iface i, h;

INSERT INTO open_port (iface_id, port, proto, service_guess, first_seen, last_seen) VALUES
  (1, 53, 'tcp', 'dns', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (1, 443, 'tcp', 'https', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (4, 22, 'tcp', 'ssh', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (4, 8006, 'tcp', 'proxmox', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (5, 22, 'tcp', 'ssh', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (5, 80, 'tcp', 'http', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (5, 443, 'tcp', 'https', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (5, 445, 'tcp', 'smb', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z'),
  (5, 32400, 'tcp', 'plex', '2026-09-10T12:00:00Z', '2026-09-15T11:00:00Z');

-- Palette keys are set explicitly (not left as hex) because the seed loads
-- after migrations run, so migration 0017's hex-to-auto cleanup never sees
-- these rows. 'personal' is left '' to show the auto colour in screenshots.
INSERT INTO tag (id, name, color) VALUES
  (1, 'infra', 'indigo'),
  (2, 'media', 'teal'),
  (3, 'iot', 'sand'),
  (4, 'wireguard', 'violet'),
  (5, 'proxmox', 'sky'),
  (6, 'personal', '');
INSERT INTO device_tag (device_id, tag_id) VALUES
  (1, 1), (2, 1), (4, 1), (5, 1),
  (5, 2), (14, 2),
  (15, 3), (16, 3), (17, 3),
  (18, 4), (19, 4),
  (6, 5), (7, 5), (8, 5),
  (10, 6), (12, 6), (13, 6);
INSERT INTO custom_field (device_id, key, value) VALUES (6, 'proxmox_status', 'running'), (7, 'proxmox_status', 'running');
-- What autofill found for the nas: it filled the vendor from the MAC; the
-- function and tag the open Plex port suggest were already set.
INSERT INTO device_hint (device_id, source, field, value, confidence, detail, seen_at) VALUES
  (5, 'oui', 'vendor', 'Synology', 90, 'MAC 00:11:32:50:60:05', '2026-09-15T12:00:00Z'),
  (5, 'ports', 'function', 'Plex', 70, 'port 32400 open', '2026-09-15T12:00:00Z'),
  (5, 'ports', 'tag', 'media', 60, 'port 32400 open', '2026-09-15T12:00:00Z');
INSERT INTO device_autofill (device_id, field, value, source, state, updated_at) VALUES
  (5, 'vendor', 'Synology', 'oui', 'applied', '2026-09-15T12:00:00Z');
INSERT INTO device_link (device_id, label, url) VALUES (5, 'DSM', 'https://nas.home.arpa:5001');

INSERT INTO integration_status (name, last_run, ok, detail, item_count) VALUES
  ('scan', '2026-09-15T11:58:00Z', 1, '', 19),
  ('proxmox', '2026-09-15T11:57:00Z', 1, '', 4),
  ('wireguard', '2026-09-15T11:59:00Z', 1, '', 2),
  ('pihole', '2026-09-15T11:56:00Z', 0, 'timeout', 0);

-- Oldest first: the event log orders by id.
INSERT INTO event (ts, type, device_id, details) VALUES
  ('2026-09-13T08:00:00Z', 'sync_recovered', NULL, 'proxmox sync working again'),
  ('2026-09-13T19:30:00Z', 'offline', 19, 'WireGuard peer wg-parents disconnected'),
  ('2026-09-14T09:12:00Z', 'scan_error', NULL, 'Subnet 10.0.20.0/24: sendto: operation not permitted'),
  ('2026-09-14T18:30:00Z', 'online', 18, 'WireGuard peer wg-ben-phone connected'),
  ('2026-09-14T22:10:00Z', 'offline', 13, 'iphone-anna (192.168.1.61) went offline'),
  ('2026-09-15T02:00:00Z', 'device_new', 22, 'opnsense device tuya-140 at 10.0.20.140'),
  ('2026-09-15T04:02:00Z', 'device_missing', 8, 'Proxmox guest k3s-node-1 is no longer in Proxmox'),
  ('2026-09-15T06:02:00Z', 'ip_changed', 11, 'MAC 04:42:1a:b0:c0:11 now at 192.168.1.51'),
  ('2026-09-15T07:19:00Z', 'device_new', 21, 'New device espressif-112 at 192.168.1.112'),
  ('2026-09-15T07:34:00Z', 'device_new', 20, 'New device 192.168.1.61 at 192.168.1.61 (randomized MAC — may be a phone with Private Wi-Fi Address)'),
  ('2026-09-15T07:35:00Z', 'ip_conflict', NULL, '192.168.1.61 in 192.168.1.0/24 is claimed by iphone-anna, 192.168.1.61'),
  ('2026-09-15T11:40:00Z', 'scan_error', NULL, 'pihole sync failing: timeout'),
  ('2026-09-15T11:48:00Z', 'offline', 10, 'ben-laptop (192.168.1.50) went offline'),
  ('2026-09-15T11:57:00Z', 'online', 12, 'pixel-8 is online');
