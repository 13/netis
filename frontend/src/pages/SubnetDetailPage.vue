<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";

import { PlayIcon } from "@heroicons/vue/24/outline";

import Modal from "@/components/Modal.vue";
import OnlineDot from "@/components/OnlineDot.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import { useDevices, useDiscovery, useIPs, useSubnets } from "@/composables/useApi";
import { useScan } from "@/composables/useScan";
import { useTableSort } from "@/composables/useTableSort";
import { useToast } from "@/composables/useToast";
import { fmtDateTime, isOnline } from "@/utils/format";
import type { Device, IPRow, IPStatus, Subnet } from "@/types";

const GRID_MAX = 4096;
const viewMode = ref<"table" | "grid">("table");
const selectedCell = ref<IPRow | null>(null);

const props = defineProps<{ id: string }>();

const subnetsApi = useSubnets();
const ipsApi = useIPs();
const devicesApi = useDevices();
const discovery = useDiscovery();
const scanRunner = useScan();
const toast = useToast();
const sort = useTableSort("ip_address");

const scanSettings = ref<{ scan_enabled: boolean; scan_interval_minutes: number; scan_method: string }>({
  scan_enabled: false,
  scan_interval_minutes: 15,
  scan_method: "arp",
});
const savingSettings = ref(false);

const subnet = ref<Subnet | null>(null);
const rows = ref<IPRow[]>([]);
const devices = ref<Device[]>([]);
const filter = ref<"" | IPStatus>("");
const search = ref("");
const busy = ref(false);
const scanMsg = ref<string | null>(null);

// Inline editing state — keyed by ip_address
const editingIp = ref<string | null>(null);
const editForm = ref<{
  device_id: number | "";
  status: IPStatus;
  description: string;
}>({ device_id: "", status: "reserved", description: "" });

// Assign dialog for free/observed IPs
const assignDialog = ref<IPRow | null>(null);
const assignForm = ref<{ device_id: number | ""; status: IPStatus; description: string }>({
  device_id: "",
  status: "reserved",
  description: "",
});

async function refresh() {
  const id = Number(props.id);
  [subnet.value, rows.value, devices.value] = await Promise.all([
    subnetsApi.get(id),
    subnetsApi.ips(id),
    devicesApi.list(),
  ]);
  if (subnet.value) {
    scanSettings.value = {
      scan_enabled: subnet.value.scan_enabled,
      scan_interval_minutes: subnet.value.scan_interval_minutes ?? 15,
      scan_method: subnet.value.scan_method ?? "arp",
    };
  }
}

async function saveScanSettings() {
  if (!subnet.value) return;
  savingSettings.value = true;
  try {
    await subnetsApi.update(subnet.value.id, {
      scan_enabled: scanSettings.value.scan_enabled,
      scan_interval_minutes: scanSettings.value.scan_interval_minutes,
      scan_method: scanSettings.value.scan_method,
    });
    await refresh();
    toast.success(
      scanSettings.value.scan_enabled
        ? `Auto-scan every ${scanSettings.value.scan_interval_minutes}m enabled.`
        : "Scheduled scanning disabled.",
    );
  } catch (e) {
    toast.error(`Could not save settings: ${(e as Error).message}`);
  } finally {
    savingSettings.value = false;
  }
}

function applyFilters(out: IPRow[]): IPRow[] {
  if (filter.value) out = out.filter((r) => r.status === filter.value);
  if (search.value.trim()) {
    const q = search.value.trim().toLowerCase();
    out = out.filter(
      (r) =>
        r.ip_address.includes(q) ||
        (r.observed_mac && r.observed_mac.includes(q)) ||
        (r.observed_hostname && r.observed_hostname.toLowerCase().includes(q)) ||
        (r.description && r.description.toLowerCase().includes(q)),
    );
  }
  return out;
}

function ipToNum(ip: string): number {
  return ip.split(".").reduce((acc, oct) => acc * 256 + parseInt(oct, 10), 0);
}

/** Table view: respects user-selected sort column. */
const filteredRows = computed(() => sort.sortArray(applyFilters([...rows.value])));

/** Grid view: always sorted numerically by IP (1 → 254). */
const gridRows = computed(() =>
  applyFilters([...rows.value]).sort((a, b) => ipToNum(a.ip_address) - ipToNum(b.ip_address)),
);

function deviceName(id: number | null): string {
  if (!id) return "—";
  const d = devices.value.find((d) => d.id === id);
  return d ? d.hostname : `#${id}`;
}

// ── Assign (free / observed) ──────────────────────────────────────────────────

function openAssign(row: IPRow) {
  assignForm.value = { device_id: "", status: "reserved", description: "" };
  assignDialog.value = row;
}

async function confirmAssign() {
  const row = assignDialog.value;
  if (!row || !subnet.value) return;
  busy.value = true;
  try {
    await ipsApi.create({
      ip_address: row.ip_address,
      subnet_id: subnet.value.id,
      device_id: assignForm.value.device_id ? Number(assignForm.value.device_id) : null,
      status: assignForm.value.status,
      description: assignForm.value.description || null,
    });
    const ip = row.ip_address;
    assignDialog.value = null;
    await refresh();
    toast.success(`${ip} assigned.`);
  } catch (e) {
    toast.error(`Could not assign IP: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

// ── Inline edit (assigned / reserved / static / dhcp / conflict) ──────────────

function startEdit(row: IPRow) {
  editingIp.value = row.ip_address;
  editForm.value = {
    device_id: row.device_id ?? "",
    status: row.conflict ? "conflict" : row.status,
    description: row.description ?? "",
  };
}

function cancelEdit() {
  editingIp.value = null;
}

async function saveEdit(row: IPRow) {
  if (!row.assignment_id) return;
  busy.value = true;
  try {
    await ipsApi.update(row.assignment_id, {
      device_id: editForm.value.device_id ? Number(editForm.value.device_id) : null,
      status: editForm.value.status,
      description: editForm.value.description || null,
    });
    editingIp.value = null;
    await refresh();
    toast.success(`${row.ip_address} updated.`);
  } catch (e) {
    toast.error(`Could not update IP: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

// ── Release ───────────────────────────────────────────────────────────────────

async function release(row: IPRow) {
  if (!row.assignment_id) return;
  if (!confirm(`Release ${row.ip_address}? The IP will become free.`)) return;
  busy.value = true;
  try {
    await ipsApi.release(row.assignment_id);
    await refresh();
    toast.info(`${row.ip_address} released.`);
  } catch (e) {
    toast.error(`Could not release IP: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

// ── Scan ──────────────────────────────────────────────────────────────────────

async function scan(method: "arp" | "ping" | "nmap") {
  if (!subnet.value) return;
  busy.value = true;
  const label = method === "arp" ? "ARP" : method === "nmap" ? "Nmap" : "Ping";
  scanMsg.value = `${label} scan running…`;
  try {
    const job = await scanRunner.run(subnet.value.id, method, method === "nmap" ? 30 : 2);
    if (job.status === "done") {
      scanMsg.value = `${label} scan complete — found ${job.found} host(s).`;
      await refresh();
    } else {
      scanMsg.value = `Scan failed: ${job.error ?? "unknown error"}`;
    }
  } catch (e) {
    scanMsg.value = `Scan failed: ${(e as Error).message}`;
  } finally {
    busy.value = false;
  }
}

// ── Grid view ─────────────────────────────────────────────────────────────────

function cellClass(row: { status: IPStatus; conflict?: boolean }): string {
  if (row.conflict) return "bg-status-conflict text-white";
  switch (row.status) {
    case "reserved": return "bg-status-reserved text-white";
    case "static": return "bg-status-static text-white";
    case "dhcp": return "bg-status-dhcp text-white";
    case "observed": return "bg-status-observed text-slate-900";
    case "conflict": return "bg-status-conflict text-white";
    default: // free
      return "dark:bg-slate-800 bg-slate-200 dark:text-slate-500 text-slate-400 dark:hover:bg-slate-700 hover:bg-slate-300";
  }
}

function lastOctet(ip: string): string {
  const parts = ip.split(".");
  return parts.length === 4 ? parts[3] : ip;
}

function cellFontClass(ip: string): string {
  const oct = ip.split(".").pop() ?? "";
  return oct.length >= 3 ? "text-[8px]" : "text-[10px]";
}

function openCell(row: IPRow) {
  selectedCell.value = row;
  if (row.assignment_id) {
    editForm.value = {
      device_id: row.device_id ?? "",
      status: row.conflict ? "conflict" : row.status,
      description: row.description ?? "",
    };
  }
}

async function cellSave() {
  if (!selectedCell.value) return;
  await saveEdit(selectedCell.value);
  selectedCell.value = null;
}

async function cellRelease() {
  if (!selectedCell.value) return;
  await release(selectedCell.value);
  selectedCell.value = null;
}

function cellAssign() {
  if (!selectedCell.value) return;
  const row = selectedCell.value;
  selectedCell.value = null;
  openAssign(row);
}

async function cellIgnore() {
  if (!selectedCell.value) return;
  const row = selectedCell.value;
  busy.value = true;
  try {
    await discovery.ignore({ mac_address: row.observed_mac, ip_address: row.ip_address });
    selectedCell.value = null;
    await refresh();
    toast.info(`Ignored ${row.ip_address}.`);
  } catch (e) {
    toast.error(`Could not ignore: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

// ── Single-IP probe ───────────────────────────────────────────────────────────

const probing = ref(false);
const probeMethod = ref<"arp" | "ping">("arp");
const probeResult = ref<{ reachable: boolean; mac_address: string | null; mac_vendor: string | null; hostname: string | null } | null>(null);

watch(selectedCell, () => { probeResult.value = null; });

async function probeCell() {
  if (!selectedCell.value) return;
  probing.value = true;
  probeResult.value = null;
  try {
    const result = await discovery.probe(selectedCell.value.ip_address, probeMethod.value);
    probeResult.value = result;
    if (result.reachable) {
      await refresh();
      // Re-sync selectedCell to the refreshed row data
      const refreshed = rows.value.find((r) => r.ip_address === selectedCell.value?.ip_address);
      if (refreshed) selectedCell.value = refreshed;
    }
  } catch {
    probeResult.value = { reachable: false, mac_address: null, mac_vendor: null, hostname: null };
  } finally {
    probing.value = false;
  }
}

// ── Standalone probe (any IP in header) ──────────────────────────────────────

const probeIpInput = ref("");
const probeIpMethod = ref<"arp" | "ping">("arp");
const probingIp = ref(false);
const probeIpResult = ref<{ reachable: boolean; mac_address: string | null; mac_vendor: string | null; hostname: string | null } | null>(null);

async function probeIp() {
  const ip = probeIpInput.value.trim();
  if (!ip) return;
  probingIp.value = true;
  probeIpResult.value = null;
  try {
    const result = await discovery.probe(ip, probeIpMethod.value);
    probeIpResult.value = result;
    if (result.reachable) await refresh();
  } catch {
    probeIpResult.value = { reachable: false, mac_address: null, mac_vendor: null, hostname: null };
  } finally {
    probingIp.value = false;
  }
}

const legend: { status: IPStatus; label: string }[] = [
  { status: "free", label: "free" },
  { status: "reserved", label: "reserved" },
  { status: "static", label: "static" },
  { status: "dhcp", label: "dhcp" },
  { status: "observed", label: "observed" },
  { status: "conflict", label: "conflict" },
];

const statusFilters: { value: "" | IPStatus; label: string }[] = [
  { value: "", label: "all" },
  { value: "free", label: "free" },
  { value: "reserved", label: "reserved" },
  { value: "static", label: "static" },
  { value: "dhcp", label: "dhcp" },
  { value: "observed", label: "observed" },
  { value: "conflict", label: "conflict" },
];

const assignableStatuses: IPStatus[] = ["reserved", "static", "dhcp"];

/** Group grid rows by /24 block (first 3 octets). Always in numeric IP order. */
const groupedRows = computed(() => {
  const groups = new Map<string, IPRow[]>();
  for (const row of gridRows.value) {
    const parts = row.ip_address.split(".");
    const key = parts.length === 4 ? `${parts[0]}.${parts[1]}.${parts[2]}` : "other";
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(row);
  }
  return [...groups.entries()].map(([prefix, rows]) => ({ prefix, rows }));
});

onMounted(refresh);
</script>

<template>
  <div v-if="subnet" class="space-y-3">
    <!-- Header -->
    <div class="flex flex-wrap items-baseline gap-3">
      <h1 class="font-semibold text-lg">{{ subnet.name }}</h1>
      <span class="font-mono text-sm dark:text-slate-400 text-slate-500">{{ subnet.cidr }}</span>
      <span v-if="subnet.gateway" class="text-xs dark:text-slate-500 text-slate-400">
        gw {{ subnet.gateway }}
      </span>
      <span v-if="subnet.vlan != null" class="text-xs dark:text-slate-500 text-slate-400">
        vlan {{ subnet.vlan }}
      </span>
      <span
        v-if="subnet.description"
        class="text-xs dark:text-slate-400 text-slate-500 italic"
      >
        {{ subnet.description }}
      </span>
      <div class="flex-1" />
      <router-link to="/subnets" class="text-xs text-sky-400 hover:underline">← subnets</router-link>
      <button class="btn btn-secondary" :disabled="busy" @click="scan('arp')">
        <PlayIcon class="w-4 h-4" /> arp scan
      </button>
      <button class="btn btn-secondary" :disabled="busy" @click="scan('ping')">
        <PlayIcon class="w-4 h-4" /> ping sweep
      </button>
      <button class="btn btn-secondary" :disabled="busy" @click="scan('nmap')">
        <PlayIcon class="w-4 h-4" /> nmap
      </button>
    </div>

    <p v-if="scanMsg" class="text-xs dark:text-slate-400 text-slate-500">{{ scanMsg }}</p>

    <!-- Probe single IP -->
    <div class="flex flex-wrap items-center gap-2">
      <input
        v-model="probeIpInput"
        placeholder="probe IP address…"
        class="input max-w-[13rem]"
        @keydown.enter="probeIp"
      />
      <div class="flex rounded-lg overflow-hidden border dark:border-slate-700 border-slate-300 text-xs">
        <button
          v-for="(m, i) in (['arp', 'ping'] as const)"
          :key="m"
          class="px-2.5 py-1 transition-all duration-150"
          :class="[
            i > 0 ? 'border-l dark:border-slate-700 border-slate-300' : '',
            probeIpMethod === m ? 'bg-sky-600 text-white' : 'dark:bg-slate-900 bg-white dark:text-slate-300 text-slate-700',
          ]"
          @click="probeIpMethod = m"
        >{{ m.toUpperCase() }}</button>
      </div>
      <button class="btn btn-secondary" :disabled="probingIp || !probeIpInput.trim()" @click="probeIp">
        {{ probingIp ? "probing…" : "probe" }}
      </button>
      <span
        v-if="probeIpResult !== null"
        class="text-xs font-mono"
        :class="probeIpResult.reachable ? 'text-green-500' : 'dark:text-slate-400 text-slate-500'"
      >
        {{ probeIpResult.reachable
          ? "✓ reachable" + (probeIpResult.mac_address ? " · " + probeIpResult.mac_address : "") + (probeIpResult.mac_vendor ? " · " + probeIpResult.mac_vendor : "") + (probeIpResult.hostname ? " · " + probeIpResult.hostname : "")
          : "✗ no reply" }}
      </span>
    </div>

    <!-- Scheduled scanning -->
    <div class="panel p-4 space-y-3">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">Scheduled scanning</h2>
        <span
          v-if="subnet.scan_enabled"
          class="text-xs px-2 py-0.5 rounded-full bg-green-500/15 text-status-free border border-green-700"
        >
          every {{ subnet.scan_interval_minutes }}m · {{ subnet.scan_method }}
        </span>
        <span v-else class="text-xs dark:text-slate-500 text-slate-400">off</span>
        <div class="flex-1" />
        <span v-if="subnet.last_scanned_at" class="text-xs dark:text-slate-500 text-slate-400">
          last auto-scan {{ fmtDateTime(subnet.last_scanned_at) }}
        </span>
      </div>
      <div class="flex flex-wrap gap-3 items-end">
        <label class="flex items-center gap-2 text-sm pb-2">
          <input v-model="scanSettings.scan_enabled" type="checkbox" class="accent-sky-500" />
          enable
        </label>
        <div class="w-32">
          <label class="label">Every (min)</label>
          <input
            v-model.number="scanSettings.scan_interval_minutes"
            type="number"
            min="1"
            max="1440"
            class="input"
            :disabled="!scanSettings.scan_enabled"
          />
        </div>
        <div>
          <label class="label">Method</label>
          <select v-model="scanSettings.scan_method" class="input" :disabled="!scanSettings.scan_enabled">
            <option value="arp">ARP</option>
            <option value="ping">Ping</option>
            <option value="nmap">Nmap</option>
          </select>
        </div>
        <button class="btn btn-secondary self-end" :disabled="savingSettings" @click="saveScanSettings">
          {{ savingSettings ? "saving…" : "save" }}
        </button>
      </div>
    </div>

    <!-- Stats strip -->
    <div class="flex gap-4 text-xs dark:text-slate-400 text-slate-500">
      <span>total <strong class="dark:text-slate-200 text-slate-700">{{ subnet.total_ips }}</strong></span>
      <span>assigned <strong class="dark:text-slate-200 text-slate-700">{{ subnet.assigned_ips }}</strong></span>
      <span>free <strong class="text-status-free">{{ subnet.free_ips }}</strong></span>
      <span>observed <strong class="text-status-observed">{{ subnet.observed_ips }}</strong></span>
      <span v-if="subnet.conflicts > 0">
        conflicts <strong class="text-status-conflict">{{ subnet.conflicts }}</strong>
      </span>
    </div>

    <!-- Search + filters -->
    <div class="flex flex-wrap gap-2 items-center">
      <input
        v-model="search"
        placeholder="search ip, mac, hostname, description…"
        class="input max-w-xs"
      />
      <div class="flex flex-wrap gap-1">
        <button
          v-for="f in statusFilters"
          :key="f.value"
          class="px-2 py-1 text-xs rounded border transition-colors"
          :class="
            filter === f.value
              ? 'bg-sky-600 border-sky-600 text-white'
              : 'dark:border-slate-700 border-slate-300 dark:hover:bg-slate-800 hover:bg-slate-100'
          "
          @click="filter = f.value"
        >
          {{ f.label }}
        </button>
      </div>
      <span class="text-xs dark:text-slate-400 text-slate-500 ml-auto">
        {{ filteredRows.length }} / {{ rows.length }}
      </span>
      <div class="flex rounded-lg overflow-hidden border dark:border-slate-700 border-slate-300">
        <button
          class="px-2.5 py-1 text-xs transition-all duration-150"
          :class="viewMode === 'table' ? 'bg-sky-600 text-white' : 'dark:bg-slate-900 bg-white dark:text-slate-300 text-slate-700'"
          @click="viewMode = 'table'"
        >table</button>
        <button
          class="px-2.5 py-1 text-xs border-l dark:border-slate-700 border-slate-300 transition-all duration-150"
          :class="viewMode === 'grid' ? 'bg-sky-600 text-white' : 'dark:bg-slate-900 bg-white dark:text-slate-300 text-slate-700'"
          @click="viewMode = 'grid'"
        >grid</button>
      </div>
    </div>

    <!-- IP table -->
    <div v-if="viewMode === 'table'" class="panel overflow-x-auto">
      <table class="w-full text-sm">
        <thead class="dark:text-slate-400 text-slate-500 sticky top-0 dark:bg-slate-900 bg-white z-10">
          <tr>
            <th class="table-cell w-6"></th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('ip_address')">ip{{ sort.getSortIndicator('ip_address') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('status')">status{{ sort.getSortIndicator('status') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('device_id')">device{{ sort.getSortIndicator('device_id') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('observed_mac')">observed mac{{ sort.getSortIndicator('observed_mac') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('observed_vendor')">vendor{{ sort.getSortIndicator('observed_vendor') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('observed_hostname')">hostname{{ sort.getSortIndicator('observed_hostname') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('description')">description{{ sort.getSortIndicator('description') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('last_seen')">last seen{{ sort.getSortIndicator('last_seen') }}</th>
            <th class="table-cell"></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="row in filteredRows" :key="row.ip_address">
            <!-- View row -->
            <tr
              v-if="editingIp !== row.ip_address"
              class="border-t dark:border-slate-800 border-slate-100"
              :class="row.conflict ? 'dark:bg-orange-950/30 bg-orange-50' : ''"
            >
              <td class="table-cell">
                <OnlineDot :last-seen="row.last_seen" />
              </td>
              <td class="table-cell font-mono whitespace-nowrap">
                {{ row.ip_address }}
              </td>
              <td class="table-cell"><StatusBadge :status="row.status" /></td>
              <td class="table-cell">{{ deviceName(row.device_id) }}</td>
              <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500">
                {{ row.observed_mac ?? "—" }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500">
                {{ row.observed_vendor ?? "—" }}
              </td>
              <td class="table-cell">{{ row.observed_hostname ?? "—" }}</td>
              <td class="table-cell dark:text-slate-400 text-slate-500 max-w-[12rem] truncate">
                {{ row.description ?? "—" }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                {{ fmtDateTime(row.last_seen) }}
              </td>
              <td class="table-cell text-right whitespace-nowrap">
                <template v-if="row.status === 'free' || row.status === 'observed'">
                  <button
                    class="text-xs text-sky-400 hover:underline"
                    :disabled="busy"
                    @click="openAssign(row)"
                  >
                    assign
                  </button>
                </template>
                <template v-else>
                  <button
                    class="text-xs text-sky-400 hover:underline mr-2"
                    :disabled="busy"
                    @click="startEdit(row)"
                  >
                    edit
                  </button>
                  <button
                    class="text-xs text-red-400 hover:underline"
                    :disabled="busy"
                    @click="release(row)"
                  >
                    release
                  </button>
                </template>
              </td>
            </tr>
            <!-- Inline edit row -->
            <tr
              v-else
              class="border-t dark:border-slate-800 border-slate-100 dark:bg-slate-800/40 bg-sky-50"
            >
              <td class="table-cell">
                <OnlineDot :last-seen="row.last_seen" />
              </td>
              <td class="table-cell font-mono">{{ row.ip_address }}</td>
              <td class="table-cell">
                <select v-model="editForm.status" class="input py-0.5 w-28">
                  <option v-for="s in assignableStatuses" :key="s" :value="s">{{ s }}</option>
                </select>
              </td>
              <td class="table-cell">
                <select v-model="editForm.device_id" class="input py-0.5 max-w-[10rem]">
                  <option value="">— none —</option>
                  <option v-for="d in devices" :key="d.id" :value="d.id">{{ d.hostname }}</option>
                </select>
              </td>
              <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500">
                {{ row.observed_mac ?? "—" }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500">
                {{ row.observed_vendor ?? "—" }}
              </td>
              <td class="table-cell">{{ row.observed_hostname ?? "—" }}</td>
              <td class="table-cell" colspan="2">
                <input v-model="editForm.description" class="input py-0.5 w-full" placeholder="description" />
              </td>
              <td class="table-cell text-right whitespace-nowrap">
                <button
                  class="text-xs text-sky-400 hover:underline mr-2"
                  :disabled="busy"
                  @click="saveEdit(row)"
                >
                  {{ busy ? "…" : "save" }}
                </button>
                <button
                  class="text-xs dark:text-slate-400 text-slate-500 hover:underline"
                  @click="cancelEdit"
                >
                  cancel
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <!-- IP grid -->
    <div v-else class="panel p-3 space-y-3">
      <!-- Legend -->
      <div class="flex flex-wrap gap-x-4 gap-y-1.5 text-xs dark:text-slate-400 text-slate-500">
        <span v-for="l in legend" :key="l.status" class="inline-flex items-center gap-1.5">
          <span class="w-3 h-3 rounded-sm" :class="cellClass({ status: l.status, conflict: l.status === 'conflict' })" />
          {{ l.label }}
        </span>
        <span class="inline-flex items-center gap-1.5">
          <span class="w-3 h-3 rounded-sm dark:bg-slate-800 bg-slate-200 ring-2 ring-green-400" />
          online
        </span>
      </div>

      <p
        v-if="rows.length > GRID_MAX"
        class="text-sm dark:text-slate-400 text-slate-500 italic py-6 text-center"
      >
        Subnet too large for grid view ({{ rows.length }} hosts) — use table view.
      </p>
      <div v-else class="space-y-3">
        <template v-for="group in groupedRows" :key="group.prefix">
          <div>
            <div
              v-if="groupedRows.length > 1"
              class="text-xs font-mono dark:text-slate-400 text-slate-500 mb-1 border-b dark:border-slate-800 border-slate-200 pb-0.5"
            >
              {{ group.prefix }}.0 — {{ group.prefix }}.255
            </div>
            <div
              class="grid gap-1"
              style="grid-template-columns: repeat(auto-fill, minmax(2.75rem, 1fr))"
            >
              <button
                v-for="row in group.rows"
                :key="row.ip_address"
                class="aspect-square rounded flex items-center justify-center font-mono transition-transform duration-100 hover:scale-110 hover:z-10 relative"
                :class="[cellClass(row), cellFontClass(row.ip_address), isOnline(row.last_seen) ? 'ring-2 ring-green-400' : '']"
                :title="`${row.ip_address} · ${row.status}${row.device_id ? ' · ' + deviceName(row.device_id) : ''}${row.observed_mac ? ' · ' + row.observed_mac : ''}`"
                @click="openCell(row)"
              >
                {{ lastOctet(row.ip_address) }}
              </button>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- Grid cell detail modal -->
    <Modal
      v-if="selectedCell"
      :title="selectedCell.ip_address"
      @close="selectedCell = null"
    >
      <div class="space-y-3">
        <div class="flex items-center gap-3 text-sm">
          <StatusBadge :status="selectedCell.status" />
          <OnlineDot :last-seen="selectedCell.last_seen" show-label />
        </div>
        <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
          <div>
            <div class="label">Device</div>
            <div>{{ deviceName(selectedCell.device_id) }}</div>
          </div>
          <div>
            <div class="label">Observed MAC</div>
            <div class="font-mono text-xs">{{ selectedCell.observed_mac ?? "—" }}</div>
            <div v-if="selectedCell.observed_vendor" class="text-xs dark:text-slate-400 text-slate-500 mt-0.5">{{ selectedCell.observed_vendor }}</div>
          </div>
          <div>
            <div class="label">Hostname</div>
            <div>{{ selectedCell.observed_hostname ?? "—" }}</div>
          </div>
          <div>
            <div class="label">Last seen</div>
            <div class="text-xs">{{ fmtDateTime(selectedCell.last_seen) }}</div>
          </div>
        </div>

        <!-- Probe single IP -->
        <div class="flex flex-wrap items-center gap-2 border-t dark:border-slate-800 border-slate-200 pt-3">
          <div class="flex rounded-lg overflow-hidden border dark:border-slate-700 border-slate-300 text-xs">
            <button
              v-for="(m, i) in (['arp', 'ping'] as const)"
              :key="m"
              class="px-2.5 py-1 transition-all duration-150"
              :class="[
                i > 0 ? 'border-l dark:border-slate-700 border-slate-300' : '',
                probeMethod === m ? 'bg-sky-600 text-white' : 'dark:bg-slate-900 bg-white dark:text-slate-300 text-slate-700',
              ]"
              @click="probeMethod = m"
            >{{ m.toUpperCase() }}</button>
          </div>
          <button class="btn btn-secondary text-xs py-1" :disabled="probing" @click="probeCell">
            {{ probing ? "probing…" : "probe" }}
          </button>
          <span
            v-if="probeResult !== null"
            class="text-xs font-mono"
            :class="probeResult.reachable ? 'text-green-500' : 'dark:text-slate-400 text-slate-500'"
          >
            {{ probeResult.reachable
              ? "✓ reachable" + (probeResult.mac_address ? " · " + probeResult.mac_address : "") + (probeResult.mac_vendor ? " · " + probeResult.mac_vendor : "") + (probeResult.hostname ? " · " + probeResult.hostname : "")
              : "✗ no reply" }}
          </span>
        </div>

        <!-- Free / observed → assign or ignore -->
        <template v-if="selectedCell.status === 'free' || selectedCell.status === 'observed'">
          <div class="flex justify-between gap-2">
            <button
              v-if="selectedCell.status === 'observed'"
              class="btn btn-ghost text-red-400"
              :disabled="busy"
              @click="cellIgnore"
            >ignore</button>
            <div v-else />
            <div class="flex gap-2">
              <button class="btn btn-ghost" @click="selectedCell = null">close</button>
              <button class="btn btn-primary" @click="cellAssign">assign</button>
            </div>
          </div>
        </template>

        <!-- Assigned → edit / release -->
        <template v-else>
          <div class="border-t dark:border-slate-800 border-slate-200 pt-3 grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label class="label">Status</label>
              <select v-model="editForm.status" class="input">
                <option v-for="s in assignableStatuses" :key="s" :value="s">{{ s }}</option>
              </select>
            </div>
            <div>
              <label class="label">Device</label>
              <select v-model="editForm.device_id" class="input">
                <option value="">— none —</option>
                <option v-for="d in devices" :key="d.id" :value="d.id">{{ d.hostname }}</option>
              </select>
            </div>
            <div class="sm:col-span-2">
              <label class="label">Description</label>
              <input v-model="editForm.description" class="input" placeholder="optional" />
            </div>
          </div>
          <div class="flex justify-between gap-2">
            <button class="btn btn-ghost text-red-400" :disabled="busy" @click="cellRelease">release</button>
            <div class="flex gap-2">
              <button class="btn btn-ghost" @click="selectedCell = null">cancel</button>
              <button class="btn btn-primary" :disabled="busy" @click="cellSave">
                {{ busy ? "saving…" : "save" }}
              </button>
            </div>
          </div>
        </template>
      </div>
    </Modal>

    <!-- Assign dialog -->
    <Modal v-if="assignDialog" @close="assignDialog = null">
      <template #header>
        Assign <span class="font-mono text-sky-400">{{ assignDialog.ip_address }}</span>
      </template>
      <div>
        <label class="label">Status</label>
        <select v-model="assignForm.status" class="input">
          <option v-for="s in assignableStatuses" :key="s" :value="s">{{ s }}</option>
        </select>
      </div>
      <div>
        <label class="label">Device</label>
        <select v-model="assignForm.device_id" class="input">
          <option value="">— none —</option>
          <option v-for="d in devices" :key="d.id" :value="d.id">{{ d.hostname }}</option>
        </select>
      </div>
      <div>
        <label class="label">Description</label>
        <input v-model="assignForm.description" class="input" placeholder="optional" />
      </div>
      <div class="flex gap-2 justify-end">
        <button class="btn btn-ghost" @click="assignDialog = null">cancel</button>
        <button class="btn btn-primary" :disabled="busy" @click="confirmAssign">
          {{ busy ? "saving…" : "assign" }}
        </button>
      </div>
    </Modal>
  </div>
  <div v-else class="dark:text-slate-400 text-slate-500 text-sm">Loading…</div>
</template>
