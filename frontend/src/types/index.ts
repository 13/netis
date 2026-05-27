export type DeviceType =
  | "router"
  | "switch"
  | "server"
  | "vm"
  | "container"
  | "workstation"
  | "iot"
  | "unknown";

export type IPStatus = "free" | "reserved" | "static" | "dhcp" | "observed" | "conflict";

export type ObservationSource = "arp" | "ping" | "dhcp" | "nmap";

export interface User {
  id: number;
  username: string;
  email: string;
  is_admin: boolean;
  created_at: string;
}

export interface Subnet {
  id: number;
  name: string;
  cidr: string;
  gateway: string | null;
  vlan: number | null;
  description: string | null;
  created_at: string;
  total_ips: number;
  assigned_ips: number;
  free_ips: number;
  observed_ips: number;
  conflicts: number;
  scan_enabled: boolean;
  scan_interval_minutes: number | null;
  scan_method: string;
  last_scanned_at: string | null;
}

export type ScanJobStatus = "queued" | "running" | "done" | "error";

export interface ScanJob {
  id: string;
  subnet_id: number;
  method: string;
  trigger: string;
  status: ScanJobStatus;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  found: number;
  error: string | null;
}

export interface DeviceChild {
  id: number;
  hostname: string;
  device_type: DeviceType;
  mac_address: string | null;
  wg_pubkey: string | null;
}

export interface Device {
  id: number;
  hostname: string;
  mac_address: string | null;
  vendor: string | null;
  model: string | null;
  location: string | null;
  device_type: DeviceType;
  notes: string | null;
  wg_pubkey: string | null;
  parent_device_id: number | null;
  children?: DeviceChild[];
  created_at: string;
  updated_at: string;
  last_seen: string | null;
  primary_ip: string | null;
}

export interface IgnoredHost {
  id: number;
  mac_address: string | null;
  ip_address: string | null;
  note: string | null;
  created_at: string;
}

export interface IPRow {
  ip_address: string;
  status: IPStatus;
  assignment_id: number | null;
  device_id: number | null;
  description: string | null;
  observed_mac: string | null;
  observed_vendor: string | null;
  observed_hostname: string | null;
  last_seen: string | null;
  conflict: boolean;
}

export interface Observation {
  id: number;
  ip_address: string;
  mac_address: string | null;
  hostname: string | null;
  vendor: string | null;
  source: ObservationSource;
  first_seen: string;
  last_seen: string;
}

export interface UnknownDevice {
  ip_address: string;
  mac_address: string | null;
  vendor: string | null;
  hostname: string | null;
  source: ObservationSource;
  first_seen: string;
  last_seen: string;
  subnet_id: number | null;
}
