-- Indexes for the filters the store actually issues. Each of these columns is
-- the WHERE clause of a query on a hot path (the device list, the subnet grid,
-- a device page) and none of them were indexed.
CREATE INDEX idx_iface_device ON iface(device_id);
CREATE INDEX idx_ip_assignment_iface ON ip_assignment(iface_id);
CREATE INDEX idx_event_device ON event(device_id);
CREATE INDEX idx_device_link_device ON device_link(device_id);
-- device_tag's primary key is (device_id, tag_id), which covers lookups by
-- device but not the join back from a tag.
CREATE INDEX idx_device_tag_tag ON device_tag(tag_id);
