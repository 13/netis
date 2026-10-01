-- Who set a subnet's DHCP pool: '' when nobody has, 'user' when it was typed
-- in, or the DHCP integration that read it from its server. An integration
-- only sets a pool that is unowned or its own, so it never overwrites one a
-- person entered. Pools that exist already were typed in.
ALTER TABLE subnet ADD COLUMN dhcp_pool_source TEXT NOT NULL DEFAULT '';
UPDATE subnet SET dhcp_pool_source = 'user' WHERE dhcp_start <> '';
