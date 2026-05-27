import { ref } from "vue";

import { api, ApiError } from "@/api/client";
import type {
  Device,
  IgnoredHost,
  IPRow,
  Observation,
  ScanJob,
  Subnet,
  UnknownDevice,
  User,
} from "@/types";

export function useSubnets() {
  return {
    list: () => api.get<Subnet[]>("/api/subnets"),
    get: (id: number) => api.get<Subnet>(`/api/subnets/${id}`),
    create: (data: {
      name: string;
      cidr: string;
      gateway?: string | null;
      vlan?: number | null;
      description?: string | null;
    }) => api.post<Subnet>("/api/subnets", data),
    update: (
      id: number,
      data: Partial<{
        name: string;
        gateway: string | null;
        vlan: number | null;
        description: string | null;
        scan_enabled: boolean;
        scan_interval_minutes: number | null;
        scan_method: string;
      }>,
    ) => api.patch<Subnet>(`/api/subnets/${id}`, data),
    delete: (id: number) => api.delete<void>(`/api/subnets/${id}`),
    ips: (id: number) => api.get<IPRow[]>(`/api/subnets/${id}/ips`),
    nextFree: (id: number) => api.get<{ ip_address: string | null }>(`/api/subnets/${id}/next-free`),
  };
}

export interface DeviceIPRow {
  ip_address: string;
  subnet_id: number;
  subnet_name: string | null;
  subnet_cidr: string | null;
  status: string;
  assignment_id: number;
  description: string | null;
  observed_mac: string | null;
  observed_hostname: string | null;
  last_seen: string | null;
}

export function useDevices() {
  return {
    list: () => api.get<Device[]>("/api/devices"),
    get: (id: number) => api.get<Device>(`/api/devices/${id}`),
    ips: (id: number) => api.get<DeviceIPRow[]>(`/api/devices/${id}/ips`),
    create: (data: Partial<Device>) => api.post<Device>("/api/devices", data),
    update: (id: number, data: Partial<Device>) => api.patch<Device>(`/api/devices/${id}`, data),
    delete: (id: number) => api.delete<void>(`/api/devices/${id}`),
  };
}

export function useIPs() {
  return {
    create: (data: {
      ip_address: string;
      subnet_id: number;
      device_id?: number | null;
      status?: string;
      description?: string | null;
    }) => api.post("/api/ips", data),
    update: (
      id: number,
      data: { device_id?: number | null; status?: string; description?: string | null },
    ) => api.patch(`/api/ips/${id}`, data),
    release: (id: number) => api.delete<void>(`/api/ips/${id}`),
  };
}

export function useDiscovery() {
  return {
    scan: (subnet_id: number, method: "arp" | "ping" | "nmap" = "arp", timeout = 2) =>
      api.post<{ observations: number }>("/api/discovery/scan", { subnet_id, method, timeout }),
    scanAsync: (subnet_id: number, method: "arp" | "ping" | "nmap" = "arp", timeout = 2) =>
      api.post<ScanJob>("/api/discovery/scan/async", { subnet_id, method, timeout }),
    jobs: () => api.get<ScanJob[]>("/api/discovery/jobs"),
    job: (id: string) => api.get<ScanJob>(`/api/discovery/jobs/${id}`),
    observations: () => api.get<Observation[]>("/api/discovery/observations"),
    unknown: () => api.get<UnknownDevice[]>("/api/discovery/unknown"),
    ignore: (data: { mac_address?: string | null; ip_address?: string | null; note?: string | null }) =>
      api.post<IgnoredHost>("/api/discovery/ignore", data),
    ignored: () => api.get<IgnoredHost[]>("/api/discovery/ignored"),
    unignore: (id: number) => api.delete<void>(`/api/discovery/ignored/${id}`),
    probe: (ip_address: string, method: "arp" | "ping" = "arp", timeout = 2) =>
      api.post<{ ip_address: string; reachable: boolean; mac_address: string | null; mac_vendor: string | null; hostname: string | null }>(
        "/api/discovery/probe",
        { ip_address, method, timeout },
      ),
    uploadLeases: (file: File, format = "auto") => {
      const form = new FormData();
      form.set("file", file);
      return api.postForm<{ imported: number }>(
        `/api/discovery/leases/upload?format=${format}`,
        form,
      );
    },
    importWg: (file: File, subnetId?: number) => {
      const form = new FormData();
      form.set("file", file);
      const qs = subnetId != null ? `?subnet_id=${subnetId}` : "";
      return api.postForm<{
        peers_found: number;
        devices_created: number;
        devices_updated: number;
        ips_assigned: number;
        skipped: number;
      }>(`/api/discovery/wg/import${qs}`, form);
    },
    importPihole: (data: {
      url: string;
      password: string;
      import_leases: boolean;
      import_dns: boolean;
    }) =>
      api.post<{ devices_imported: number; dns_records_imported: number; errors: string[] }>(
        "/api/discovery/pihole/import",
        data,
      ),
  };
}

export function useUsers() {
  return {
    list: () => api.get<User[]>("/api/users"),
    update: (id: number, data: { is_admin?: boolean }) =>
      api.patch<User>(`/api/users/${id}`, data),
    delete: (id: number) => api.delete<void>(`/api/users/${id}`),
  };
}

export interface AdminInfo {
  db_type: "sqlite" | "postgresql";
  db_url_safe: string;
  connected: boolean;
  config_file: string;
  config_writable: boolean;
  scheduler_enabled: boolean;
  alert_webhook_configured: boolean;
}

export interface DbConfig {
  db_type: "sqlite" | "postgresql";
  sqlite_path?: string | null;
  pg_host?: string | null;
  pg_port?: number;
  pg_user?: string | null;
  pg_password?: string | null;
  pg_database?: string | null;
}

export interface MigrateResult {
  migrated: Record<string, number>;
  url_safe: string;
  config_saved: boolean;
  restart_required: boolean;
}

export interface LocalNetwork {
  interface: string;
  ip: string;
  cidr: string;
  name: string;
  gateway: string;
}

export function useAdmin() {
  return {
    info: () => api.get<AdminInfo>("/api/admin/info"),
    testDb: (cfg: DbConfig) => api.post<{ ok: boolean; error?: string; url_safe?: string }>("/api/admin/test-db", cfg),
    migrateDb: (cfg: DbConfig) => api.post<MigrateResult>("/api/admin/migrate-db", cfg),
    localNetworks: () => api.get<LocalNetwork[]>("/api/admin/local-networks"),
  };
}

export function useAuth() {
  return {
    changePassword: (currentPassword: string, newPassword: string) =>
      api.post<void>("/api/auth/change-password", {
        current_password: currentPassword,
        new_password: newPassword,
      }),
  };
}

export function useBackup() {
  return {
    export: () => api.get<unknown>("/api/backup/export"),
    import: (file: File) => {
      const form = new FormData();
      form.set("file", file);
      return api.postForm<{ imported: Record<string, number> }>("/api/backup/import", form);
    },
    importDevices: (file: File) => {
      const form = new FormData();
      form.set("file", file);
      return api.postForm<{ created: number; skipped: number; errors: string[] }>(
        "/api/devices/import",
        form,
      );
    },
  };
}

export function useAsync<T>() {
  const data = ref<T | null>(null);
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function run(fn: () => Promise<T>) {
    loading.value = true;
    error.value = null;
    try {
      data.value = await fn();
    } catch (e) {
      error.value = e instanceof ApiError ? e.message : (e as Error).message;
    } finally {
      loading.value = false;
    }
  }

  return { data, loading, error, run };
}
