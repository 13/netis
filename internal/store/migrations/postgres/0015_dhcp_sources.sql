-- AdGuard Home and OPNsense are DHCP lease sources like Pi-hole, and a device
-- one of them discovers is labelled with it.
ALTER TABLE device DROP CONSTRAINT IF EXISTS device_source_check;
ALTER TABLE device ADD CONSTRAINT device_source_check CHECK (source IN
  ('manual','scan','proxmox','wireguard','pihole','adguard','opnsense'));
