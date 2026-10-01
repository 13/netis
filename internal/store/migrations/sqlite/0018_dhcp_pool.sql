-- A subnet's DHCP pool, the range its DHCP server hands out leases from.
-- Free addresses inside it are not offered for static use: the subnet page
-- marks them, and the next free address and the free ranges skip them. Both
-- empty means the pool is not known.
ALTER TABLE subnet ADD COLUMN dhcp_start TEXT NOT NULL DEFAULT '';
ALTER TABLE subnet ADD COLUMN dhcp_end TEXT NOT NULL DEFAULT '';
