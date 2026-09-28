-- The integration syncs find their devices by WireGuard public key and by
-- Proxmox VMID, and create one when the lookup misses. Nothing stopped two
-- overlapping runs (a scheduled one and a "run now") from both missing and
-- both creating, after which the lookup silently picks one of the pair. These
-- partial unique indexes make the second insert fail instead.
--
-- Any duplicates that already exist are resolved without deleting a row: the
-- lowest id keeps the key (it is the one the lookups have been returning), and
-- the later copies lose it and stay visible for the user to merge or delete. A
-- Proxmox copy also becomes source 'manual', because a proxmox device with no
-- VMID is how the sync recognises a cluster node.
UPDATE device SET wg_pubkey = NULL
WHERE wg_pubkey IS NOT NULL AND EXISTS (
  SELECT 1 FROM device d2 WHERE d2.wg_pubkey = device.wg_pubkey AND d2.id < device.id);

UPDATE device SET proxmox_vmid = NULL,
  source = CASE WHEN source = 'proxmox' THEN 'manual' ELSE source END
WHERE proxmox_vmid IS NOT NULL AND EXISTS (
  SELECT 1 FROM device d2 WHERE d2.proxmox_vmid = device.proxmox_vmid AND d2.id < device.id);

CREATE UNIQUE INDEX idx_device_wg_pubkey ON device(wg_pubkey) WHERE wg_pubkey IS NOT NULL;
CREATE UNIQUE INDEX idx_device_proxmox_vmid ON device(proxmox_vmid) WHERE proxmox_vmid IS NOT NULL;

-- Lookup indexes for filters that had none: the retention sweep deletes
-- availability buckets by age (the (iface_id, bucket_start) unique key does
-- not help without an iface), and the session list and revocations filter by
-- user.
CREATE INDEX idx_availability_history_bucket ON availability_history(bucket_start);
CREATE INDEX idx_session_user ON session(user_id);
