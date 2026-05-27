<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import {
  ArrowPathIcon,
  EyeSlashIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  Squares2X2Icon,
  TableCellsIcon,
} from "@heroicons/vue/24/outline";


import Modal from "@/components/Modal.vue";
import OnlineDot from "@/components/OnlineDot.vue";
import { useDevices, useDiscovery, useSubnets } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { useScan } from "@/composables/useScan";
import { useSelection } from "@/composables/useSelection";
import { useToast } from "@/composables/useToast";
import type { DeviceType, IgnoredHost, Subnet, UnknownDevice } from "@/types";
import { fmtDate, fmtDateTime, isOnline, relativeTime } from "@/utils/format";

const route = useRoute();
const discovery = useDiscovery();
const devicesApi = useDevices();
const subnetsApi = useSubnets();
const scan = useScan();
const toast = useToast();
const sel = useSelection<string>();
const sortUnknown = useTableSort("last_seen");
const sortIgnored = useTableSort("created_at");

const unknown = ref<UnknownDevice[]>([]);
const subnets = ref<Subnet[]>([]);
const ignored = ref<IgnoredHost[]>([]);
const busy = ref(false);
const showIgnored = ref(false);

import { computed } from "vue";

const search = ref("");
const sourceFilter = ref<"" | "arp" | "ping" | "dhcp" | "nmap">("");
const viewMode = ref<"list" | "grid">("list");

function ipToNum(ip: string): number {
  return ip.split(".").reduce((acc, oct) => acc * 256 + parseInt(oct, 10), 0);
}

function lastOctet(ip: string): string {
  const parts = ip.split(".");
  return parts.length === 4 ? parts[3] : ip;
}

function cellFontClass(label: string): string {
  return label.length >= 3 ? "text-[8px]" : "text-[10px]";
}

function unknownCellClass(u: UnknownDevice): string {
  return isOnline(u.last_seen)
    ? "bg-amber-500 text-slate-900"
    : "dark:bg-amber-900/40 bg-amber-100 dark:text-amber-300 text-amber-700 dark:hover:bg-amber-900/60 hover:bg-amber-200";
}

const groupedUnknown = computed(() => {
  const byPrefix = new Map<string, UnknownDevice[]>();
  for (const u of filteredUnknown.value) {
    const parts = u.ip_address.split(".");
    const key = parts.length === 4 ? `${parts[0]}.${parts[1]}.${parts[2]}` : "other";
    if (!byPrefix.has(key)) byPrefix.set(key, []);
    byPrefix.get(key)!.push(u);
  }
  return [...byPrefix.entries()]
    .sort(([a], [b]) => ipToNum(a + ".0") - ipToNum(b + ".0"))
    .map(([prefix, hosts]) => ({
      prefix,
      hosts: [...hosts].sort((a, b) => ipToNum(a.ip_address) - ipToNum(b.ip_address)),
    }));
});

const filteredUnknown = computed(() => {
  let out = unknown.value;
  if (sourceFilter.value) out = out.filter((u) => u.source === sourceFilter.value);
  if (search.value.trim()) {
    const q = search.value.trim().toLowerCase();
    out = out.filter(
      (u) =>
        u.ip_address.includes(q) ||
        (u.mac_address && u.mac_address.toLowerCase().includes(q)) ||
        (u.vendor && u.vendor.toLowerCase().includes(q)) ||
        (u.hostname && u.hostname.toLowerCase().includes(q)),
    );
  }
  return out;
});

const sortedUnknown = computed(() => sortUnknown.sortArray(filteredUnknown.value));
const sortedIgnored = computed(() => sortIgnored.sortArray(ignored.value));

// Scan panel state
const scanSubnetId = ref<number | "">("");
const scanMethod = ref<"arp" | "ping" | "nmap">("arp");
const scanTimeout = ref(2);
const scanBusy = ref(false);
const scanMsg = ref<{ ok: boolean; text: string } | null>(null);

watch(scanMethod, (m) => { scanTimeout.value = m === "nmap" ? 30 : 2; });

// Convert-to-device modal
const convertTarget = ref<UnknownDevice | null>(null);
const convertForm = ref<{ hostname: string; device_type: DeviceType }>({
  hostname: "",
  device_type: "unknown",
});
const convertError = ref<string | null>(null);
const deviceTypes: DeviceType[] = [
  "router", "switch", "server", "vm", "container", "workstation", "iot", "unknown",
];

async function refreshUnknown() {
  [unknown.value, ignored.value] = await Promise.all([discovery.unknown(), discovery.ignored()]);
}

async function runScan() {
  const sid = Number(scanSubnetId.value);
  if (!sid) return;
  scanBusy.value = true;
  scanMsg.value = null;
  const label = scanMethod.value === "arp" ? "ARP" : scanMethod.value === "nmap" ? "Nmap" : "Ping";
  try {
    const job = await scan.run(sid, scanMethod.value, scanTimeout.value);
    if (job.status === "done") {
      scanMsg.value = { ok: true, text: `${label} scan complete — ${job.found} host(s) observed.` };
      await refreshUnknown();
    } else {
      scanMsg.value = { ok: false, text: `Scan failed: ${job.error ?? "unknown error"}` };
    }
  } catch (e) {
    scanMsg.value = { ok: false, text: `Scan failed: ${(e as Error).message}` };
  } finally {
    scanBusy.value = false;
  }
}

function openConvert(u: UnknownDevice) {
  convertTarget.value = u;
  convertForm.value = { hostname: u.hostname ?? "", device_type: "unknown" };
  convertError.value = null;
}

async function confirmConvert() {
  const u = convertTarget.value;
  if (!u) return;
  busy.value = true;
  convertError.value = null;
  try {
    const hostname = convertForm.value.hostname;
    await devicesApi.create({
      hostname,
      mac_address: u.mac_address,
      device_type: convertForm.value.device_type,
    });
    convertTarget.value = null;
    await refreshUnknown();
    toast.success(`Device “${hostname}” created.`);
  } catch (e) {
    convertError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}

async function ignoreHost(u: UnknownDevice) {
  busy.value = true;
  try {
    await discovery.ignore({ mac_address: u.mac_address, ip_address: u.ip_address });
    await refreshUnknown();
    toast.info(`Ignored ${u.ip_address}.`);
  } catch (e) {
    toast.error(`Could not ignore host: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

async function ignoreAll() {
  if (!unknown.value.length) return;
  const n = unknown.value.length;
  if (!confirm(`Ignore all ${n} unknown host(s)? They'll move to the ignored list.`)) return;
  busy.value = true;
  try {
    await Promise.all(
      unknown.value.map((u) =>
        discovery.ignore({ mac_address: u.mac_address, ip_address: u.ip_address }),
      ),
    );
    await refreshUnknown();
    toast.info(`Ignored ${n} host(s).`);
  } catch (e) {
    toast.error(`Could not ignore hosts: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

async function unignore(id: number) {
  busy.value = true;
  try {
    await discovery.unignore(id);
    await refreshUnknown();
    toast.success("Host restored to discovery.");
  } catch (e) {
    toast.error(`Could not restore host: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

async function bulkIgnore() {
  const ips = sel.present(unknown.value.map((u) => u.ip_address));
  if (!ips.length) return;
  const targets = unknown.value.filter((u) => ips.includes(u.ip_address));
  busy.value = true;
  try {
    await Promise.all(
      targets.map((u) => discovery.ignore({ mac_address: u.mac_address, ip_address: u.ip_address })),
    );
    sel.clear();
    await refreshUnknown();
    toast.info(`Ignored ${targets.length} host(s).`);
  } catch (e) {
    toast.error(`Could not ignore hosts: ${(e as Error).message}`);
  } finally {
    busy.value = false;
  }
}

onMounted(async () => {
  const [s, u, ig] = await Promise.all([
    subnetsApi.list(),
    discovery.unknown(),
    discovery.ignored(),
  ]);
  subnets.value = s;
  unknown.value = u;
  ignored.value = ig;
  if (s.length === 1) scanSubnetId.value = s[0].id;

  const targetIp = route.query.ip as string | undefined;
  if (targetIp) {
    const match = u.find((x) => x.ip_address === targetIp);
    if (match) openConvert(match);
  }
});
</script>

<template>
  <div class="space-y-4">
    <h1 class="font-semibold">Discovery</h1>

    <!-- Scan panel -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Scan a network</h2>
      <div class="flex flex-wrap gap-3 items-end">
        <div class="flex-1 min-w-[180px]">
          <label class="label">Subnet</label>
          <select v-model="scanSubnetId" class="input">
            <option value="">— pick a subnet —</option>
            <option v-for="s in subnets" :key="s.id" :value="s.id">
              {{ s.name }} ({{ s.cidr }})
            </option>
          </select>
        </div>

        <div>
          <label class="label">Method</label>
          <div class="flex rounded-lg overflow-hidden border dark:border-slate-700 border-slate-300">
            <button
              v-for="(m, i) in (['arp', 'ping', 'nmap'] as const)"
              :key="m"
              class="px-3 py-2 text-sm transition-all duration-150"
              :class="[
                i > 0 ? 'border-l dark:border-slate-700 border-slate-300' : '',
                scanMethod === m
                  ? 'bg-sky-600 text-white'
                  : 'dark:bg-slate-900 bg-white dark:text-slate-300 text-slate-700 dark:hover:bg-slate-800 hover:bg-slate-50',
              ]"
              :disabled="scanBusy"
              @click="scanMethod = m"
            >
              {{ m.toUpperCase() }}
            </button>
          </div>
        </div>

        <div class="w-24">
          <label class="label">Timeout (s)</label>
          <input v-model.number="scanTimeout" type="number" class="input" min="1" max="120" :disabled="scanBusy" />
        </div>

        <button class="btn btn-primary self-end" :disabled="scanBusy || !scanSubnetId" @click="runScan">
          <ArrowPathIcon v-if="scanBusy" class="w-4 h-4 animate-spin" />
          <MagnifyingGlassIcon v-else class="w-4 h-4" />
          {{ scanBusy ? "Scanning…" : "Run scan" }}
        </button>
      </div>

      <p v-if="scanMsg" class="text-xs" :class="scanMsg.ok ? 'text-green-500' : 'text-red-400'">
        {{ scanMsg.text }}
      </p>

      <p class="text-xs dark:text-slate-500 text-slate-400">
        <template v-if="scanMethod === 'arp'">
          ARP scan — reveals live hosts and their MAC addresses. Requires the scanner to be on the same L2 segment.
        </template>
        <template v-else-if="scanMethod === 'ping'">
          Ping sweep — ICMP echo, works across routers but does not reveal MAC addresses.
        </template>
        <template v-else>
          Nmap host discovery — combines ARP, ICMP, and TCP probes. Captures MACs and PTR hostnames. Requires nmap to be installed.
        </template>
      </p>
    </div>

    <!-- Unknown hosts -->
    <div class="space-y-2">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold">
          Unknown hosts
          <span v-if="unknown.length" class="ml-1 text-status-observed">({{ unknown.length }})</span>
        </h2>
        <div class="flex-1" />
        <button
          v-if="!sel.isEmpty.value"
          class="btn btn-primary text-xs"
          :disabled="busy"
          @click="bulkIgnore"
        >
          <EyeSlashIcon class="w-4 h-4" />
          ignore {{ sel.count.value }} selected
        </button>
        <button
          v-if="unknown.length"
          class="btn btn-secondary text-xs"
          :disabled="busy"
          @click="ignoreAll"
        >
          <EyeSlashIcon class="w-4 h-4" />
          ignore all
        </button>
        <button class="btn btn-secondary text-xs" :disabled="busy" @click="refreshUnknown">
          <ArrowPathIcon class="w-4 h-4" />
          refresh
        </button>
      </div>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Observed hosts with no matching device record. Take ownership with “→ device”, or ignore to dismiss.
      </p>
      <div class="flex flex-wrap gap-2 items-center">
        <div class="relative">
          <MagnifyingGlassIcon class="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 dark:text-slate-500 text-slate-400 pointer-events-none" />
          <input
            v-model="search"
            placeholder="search ip, mac, hostname, vendor…"
            class="input pl-8 py-1.5 text-xs w-48 sm:w-72"
          />
        </div>
        <div class="flex flex-wrap gap-1">
          <button
            v-for="src in ['', 'arp', 'ping', 'dhcp', 'nmap'] as const"
            :key="src"
            class="px-2 py-1 text-xs rounded border transition-colors"
            :class="
              sourceFilter === src
                ? 'bg-sky-600 border-sky-600 text-white'
                : 'dark:border-slate-700 border-slate-300 dark:hover:bg-slate-800 hover:bg-slate-100'
            "
            @click="sourceFilter = src"
          >{{ src || 'all' }}</button>
        </div>
        <div class="flex items-center gap-0.5 rounded-lg dark:bg-slate-800 bg-slate-100 p-0.5 ml-auto">
          <button
            class="p-1.5 rounded-md transition-colors"
            :class="viewMode === 'list' ? 'dark:bg-slate-700 bg-white shadow-sm dark:text-white text-slate-800' : 'dark:text-slate-500 text-slate-400 hover:dark:text-slate-300 hover:text-slate-600'"
            title="List view"
            @click="viewMode = 'list'"
          >
            <TableCellsIcon class="w-4 h-4" />
          </button>
          <button
            class="p-1.5 rounded-md transition-colors"
            :class="viewMode === 'grid' ? 'dark:bg-slate-700 bg-white shadow-sm dark:text-white text-slate-800' : 'dark:text-slate-500 text-slate-400 hover:dark:text-slate-300 hover:text-slate-600'"
            title="Grid view"
            @click="viewMode = 'grid'"
          >
            <Squares2X2Icon class="w-4 h-4" />
          </button>
        </div>
        <span class="text-xs dark:text-slate-400 text-slate-500">
          {{ filteredUnknown.length }} / {{ unknown.length }}
        </span>
      </div>
      <div v-if="viewMode === 'list'" class="panel overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="dark:text-slate-400 text-slate-500">
            <tr>
              <th class="table-cell text-left w-8">
                <input
                  type="checkbox"
                  class="accent-sky-500 align-middle"
                  :checked="sel.allSelected(filteredUnknown.map((u) => u.ip_address))"
                  :disabled="!filteredUnknown.length"
                  @change="sel.toggleAll(filteredUnknown.map((u) => u.ip_address))"
                />
              </th>
              <th class="table-cell w-6"></th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortUnknown.toggleSort('ip_address')">ip{{ sortUnknown.getSortIndicator('ip_address') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortUnknown.toggleSort('mac_address')">mac{{ sortUnknown.getSortIndicator('mac_address') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortUnknown.toggleSort('hostname')">hostname{{ sortUnknown.getSortIndicator('hostname') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortUnknown.toggleSort('source')">source{{ sortUnknown.getSortIndicator('source') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortUnknown.toggleSort('last_seen')">last seen{{ sortUnknown.getSortIndicator('last_seen') }}</th>
              <th class="table-cell"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!unknown.length">
              <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="8">
                No unknown hosts — all observed MACs have a device record.
              </td>
            </tr>
            <tr v-else-if="!filteredUnknown.length">
              <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="8">
                No matches for the current search/filter.
              </td>
            </tr>
            <tr
              v-for="u in sortedUnknown"
              :key="u.ip_address"
              class="border-t dark:border-slate-800 border-slate-100 cursor-pointer dark:hover:bg-slate-800/40 hover:bg-slate-50"
              :class="sel.isSelected(u.ip_address) ? 'dark:bg-sky-950/30 bg-sky-50' : ''"
              @click="openConvert(u)"
            >
              <td class="table-cell" @click.stop>
                <input
                  type="checkbox"
                  class="accent-sky-500 align-middle"
                  :checked="sel.isSelected(u.ip_address)"
                  @change="sel.toggle(u.ip_address)"
                />
              </td>
              <td class="table-cell">
                <OnlineDot :last-seen="u.last_seen" />
              </td>
              <td class="table-cell font-mono whitespace-nowrap">
                {{ u.ip_address }}
              </td>
              <td class="table-cell font-mono text-xs">
                {{ u.mac_address ?? "—" }}
                <div v-if="u.vendor" class="font-sans text-xs dark:text-slate-400 text-slate-500 mt-0.5">{{ u.vendor }}</div>
              </td>
              <td class="table-cell">{{ u.hostname ?? "—" }}</td>
              <td class="table-cell text-xs uppercase font-semibold dark:text-slate-400 text-slate-500">
                {{ u.source }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                {{ fmtDateTime(u.last_seen) }}
              </td>
              <td class="table-cell text-right whitespace-nowrap" @click.stop>
                <button class="inline-flex items-center gap-1 text-xs text-sky-400 hover:underline mr-3" :disabled="busy" @click="openConvert(u)">
                  <PlusIcon class="w-3.5 h-3.5" /> device
                </button>
                <button class="inline-flex items-center gap-1 text-xs dark:text-slate-400 text-slate-500 hover:underline" :disabled="busy" @click="ignoreHost(u)">
                  <EyeSlashIcon class="w-3.5 h-3.5" /> ignore
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Grid view -->
      <div v-else-if="viewMode === 'grid'">
        <div v-if="!unknown.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No unknown hosts — all observed MACs have a device record.
        </div>
        <div v-else-if="!filteredUnknown.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No matches for the current search/filter.
        </div>
        <div v-else class="panel p-3 space-y-3">
          <template v-for="group in groupedUnknown" :key="group.prefix">
            <div>
              <div class="text-xs font-mono dark:text-slate-400 text-slate-500 mb-1 border-b dark:border-slate-800 border-slate-200 pb-0.5">
                {{ group.prefix }}.0/24
              </div>
              <div
                class="grid gap-1"
                style="grid-template-columns: repeat(auto-fill, minmax(2.75rem, 1fr))"
              >
                <button
                  v-for="u in group.hosts"
                  :key="u.ip_address"
                  class="aspect-square rounded flex items-center justify-center font-mono transition-transform duration-100 hover:scale-110 hover:z-10 relative"
                  :class="[unknownCellClass(u), cellFontClass(lastOctet(u.ip_address)), isOnline(u.last_seen) ? 'ring-2 ring-green-400' : '']"
                  :title="`${u.ip_address}${u.hostname ? ' · ' + u.hostname : ''}${u.mac_address ? ' · ' + u.mac_address : ''}${u.vendor ? ' · ' + u.vendor : ''} · ${relativeTime(u.last_seen)}`"
                  @click="openConvert(u)"
                >
                  {{ lastOctet(u.ip_address) }}
                </button>
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- Ignored hosts -->
    <div v-if="ignored.length" class="space-y-2">
      <button
        class="text-xs dark:text-slate-400 text-slate-500 hover:underline flex items-center gap-1"
        @click="showIgnored = !showIgnored"
      >
        <span>{{ showIgnored ? "▾" : "▸" }}</span>
        Ignored hosts ({{ ignored.length }})
      </button>
      <div v-if="showIgnored" class="panel overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="dark:text-slate-400 text-slate-500">
            <tr>
              <th class="table-cell text-left">mac</th>
              <th class="table-cell text-left">ip</th>
              <th class="table-cell text-left">ignored on</th>
              <th class="table-cell"></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="ih in ignored"
              :key="ih.id"
              class="border-t dark:border-slate-800 border-slate-100"
            >
              <td class="table-cell font-mono text-xs">{{ ih.mac_address ?? "—" }}</td>
              <td class="table-cell font-mono">{{ ih.ip_address ?? "—" }}</td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500">{{ fmtDate(ih.created_at) }}</td>
              <td class="table-cell text-right">
                <button class="text-xs text-sky-400 hover:underline" :disabled="busy" @click="unignore(ih.id)">
                  un-ignore
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Convert-to-device modal -->
    <Modal v-if="convertTarget" title="Add as device" @close="convertTarget = null">
      <form class="space-y-3" @submit.prevent="confirmConvert">
        <div class="text-xs dark:text-slate-400 text-slate-500 space-y-1">
          <div>IP <span class="font-mono dark:text-slate-200 text-slate-700">{{ convertTarget.ip_address }}</span></div>
          <div v-if="convertTarget.mac_address">
            MAC <span class="font-mono dark:text-slate-200 text-slate-700">{{ convertTarget.mac_address }}</span>
            <span v-if="convertTarget.vendor" class="ml-2 dark:text-slate-400 text-slate-500">{{ convertTarget.vendor }}</span>
          </div>
        </div>
        <div>
          <label class="label">Hostname *</label>
          <input v-model="convertForm.hostname" class="input" required autofocus placeholder="my-device" />
        </div>
        <div>
          <label class="label">Type</label>
          <select v-model="convertForm.device_type" class="input">
            <option v-for="t in deviceTypes" :key="t" :value="t">{{ t }}</option>
          </select>
        </div>
        <p v-if="convertError" class="text-xs text-red-400">{{ convertError }}</p>
        <div class="flex gap-2 justify-end">
          <button type="button" class="btn btn-ghost" @click="convertTarget = null">cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            {{ busy ? "creating…" : "create device" }}
          </button>
        </div>
      </form>
    </Modal>
  </div>
</template>
