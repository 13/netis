<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";

import {
  ArrowPathIcon,
  ComputerDesktopIcon,
  ExclamationTriangleIcon,
  MagnifyingGlassIcon,
  SignalIcon,
  Squares2X2Icon,
  TableCellsIcon,
} from "@heroicons/vue/24/outline";

import Modal from "@/components/Modal.vue";
import OnlineDot from "@/components/OnlineDot.vue";
import { useDevices, useDiscovery, useSubnets } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { isOnline, relativeTime } from "@/utils/format";
import type { Device, Observation, Subnet, UnknownDevice } from "@/types";

const router = useRouter();

const subnets = ref<Subnet[]>([]);
const devices = ref<Device[]>([]);
const unknown = ref<UnknownDevice[]>([]);
const observations = ref<Observation[]>([]);
const loading = ref(false);

const subnetsApi = useSubnets();
const devicesApi = useDevices();
const discovery = useDiscovery();

async function refresh() {
  loading.value = true;
  try {
    [subnets.value, devices.value, unknown.value, observations.value] = await Promise.all([
      subnetsApi.list(),
      devicesApi.list(),
      discovery.unknown(),
      discovery.observations(),
    ]);
  } finally {
    loading.value = false;
  }
}

onMounted(refresh);

// ── Stats ─────────────────────────────────────────────────────────────────────

const onlineCount = computed(() =>
  new Set(observations.value.filter((o) => isOnline(o.last_seen)).map((o) => o.ip_address)).size,
);

// ── Live hosts ────────────────────────────────────────────────────────────────

const deviceByMac = computed(() => {
  const m = new Map<string, Device>();
  for (const d of devices.value) if (d.mac_address) m.set(d.mac_address, d);
  return m;
});

const deviceByIp = computed(() => {
  const m = new Map<string, Device>();
  for (const d of devices.value) if (d.primary_ip) m.set(d.primary_ip, d);
  return m;
});

const liveHosts = computed(() => {
  const latestByIp = new Map<string, Observation>();
  const latestMacByIp = new Map<string, string>();
  const latestVendorByIp = new Map<string, string>();

  for (const o of observations.value) {
    const cur = latestByIp.get(o.ip_address);
    if (!cur || o.last_seen > cur.last_seen) latestByIp.set(o.ip_address, o);
    if (o.mac_address && !latestMacByIp.has(o.ip_address)) latestMacByIp.set(o.ip_address, o.mac_address);
    if (o.vendor && !latestVendorByIp.has(o.ip_address)) latestVendorByIp.set(o.ip_address, o.vendor);
  }

  const hosts = [...latestByIp.entries()].map(([ip, obs]) => {
    const mac = latestMacByIp.get(ip) ?? obs.mac_address ?? null;
    const device = (mac ? deviceByMac.value.get(mac) : null) ?? deviceByIp.value.get(ip) ?? null;
    return {
      ip,
      hostname: device?.hostname ?? obs.hostname ?? null,
      mac,
      vendor: device?.vendor ?? latestVendorByIp.get(ip) ?? null,
      device,
      last_seen: obs.last_seen,
      online: isOnline(obs.last_seen),
    };
  });

  return hosts.sort((a, b) => {
    if (a.online !== b.online) return a.online ? -1 : 1;
    return b.last_seen.localeCompare(a.last_seen);
  });
});

const hostSearch = ref("");
const showAllHosts = ref(false);
const hostView = ref<"list" | "grid">("list");
const sort = useTableSort("ip");

const filteredHosts = computed(() => {
  const q = hostSearch.value.trim().toLowerCase();
  if (!q) return liveHosts.value;
  return liveHosts.value.filter(
    (h) =>
      h.ip.includes(q) ||
      h.hostname?.toLowerCase().includes(q) ||
      h.mac?.includes(q) ||
      h.vendor?.toLowerCase().includes(q) ||
      h.device?.hostname.toLowerCase().includes(q),
  );
});

const sortedHosts = computed(() => sort.sortArray(filteredHosts.value));

const HOST_PREVIEW = 30;
const visibleHosts = computed(() =>
  showAllHosts.value ? sortedHosts.value : sortedHosts.value.slice(0, HOST_PREVIEW),
);

// ── Grid helpers ──────────────────────────────────────────────────────────────

function lastOctet(ip: string): string {
  const parts = ip.split(".");
  return parts.length === 4 ? parts[3] : ip;
}

function cellFontClass(ip: string): string {
  const oct = ip.split(".").pop() ?? "";
  return oct.length >= 3 ? "text-[8px]" : "text-[10px]";
}

function hostCellClass(h: { online: boolean; device: Device | null }): string {
  if (h.online && h.device)  return "bg-sky-600 text-white";
  if (h.online && !h.device) return "bg-amber-500 text-slate-900";
  if (!h.online && h.device) return "dark:bg-sky-900/50 bg-sky-200 dark:text-sky-300 text-sky-700";
  return "dark:bg-slate-800 bg-slate-200 dark:text-slate-500 text-slate-400 dark:hover:bg-slate-700 hover:bg-slate-300";
}

type LiveHost = (typeof liveHosts.value)[number];

const selectedHost = ref<LiveHost | null>(null);

async function openHost(h: LiveHost) {
  selectedHost.value = h;
  if (h.device && !h.device.vendor && h.vendor) {
    await devicesApi.update(h.device.id, { vendor: h.vendor });
    await refresh();
    const updated = liveHosts.value.find((x) => x.ip === h.ip);
    if (updated) selectedHost.value = updated;
  }
}

function ipToNum(ip: string): number {
  return ip.split(".").reduce((acc, oct) => acc * 256 + parseInt(oct, 10), 0);
}

const groupedHosts = computed(() => {
  const byPrefix = new Map<string, typeof filteredHosts.value>();
  for (const h of filteredHosts.value) {
    const parts = h.ip.split(".");
    const key = parts.length === 4 ? `${parts[0]}.${parts[1]}.${parts[2]}` : "other";
    if (!byPrefix.has(key)) byPrefix.set(key, []);
    byPrefix.get(key)!.push(h);
  }
  return [...byPrefix.entries()]
    .sort(([a], [b]) => ipToNum(a + ".0") - ipToNum(b + ".0"))
    .map(([prefix, hosts]) => ({
      prefix,
      hosts: [...hosts].sort((a, b) => ipToNum(a.ip) - ipToNum(b.ip)),
    }));
});

// ── Subnet utilization ────────────────────────────────────────────────────────

function subnetUsedPct(s: Subnet): number {
  if (!s.total_ips) return 0;
  return Math.round(((s.assigned_ips + s.observed_ips) / s.total_ips) * 100);
}
function subnetAssignedPct(s: Subnet): number {
  if (!s.total_ips) return 0;
  return (s.assigned_ips / s.total_ips) * 100;
}
function subnetObservedPct(s: Subnet): number {
  if (!s.total_ips) return 0;
  return (s.observed_ips / s.total_ips) * 100;
}
</script>

<template>
  <div class="space-y-5">

    <!-- Header -->
    <div class="flex items-center gap-3">
      <h1 class="font-semibold text-lg">Dashboard</h1>
      <div class="flex-1" />
      <button
        class="btn btn-secondary text-xs gap-1.5"
        :disabled="loading"
        @click="refresh"
      >
        <ArrowPathIcon class="w-3.5 h-3.5" :class="loading && 'animate-spin'" />
        refresh
      </button>
    </div>

    <!-- Stat cards -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <RouterLink
        to="/subnets"
        class="panel p-4 flex items-center gap-4 hover:dark:border-slate-600 hover:border-slate-300 transition-colors group"
      >
        <div class="w-10 h-10 rounded-xl bg-sky-500/10 flex items-center justify-center shrink-0">
          <Squares2X2Icon class="w-5 h-5 text-sky-400" />
        </div>
        <div>
          <div class="text-2xl font-bold leading-none">{{ subnets.length }}</div>
          <div class="text-xs dark:text-slate-400 text-slate-500 mt-1">Subnets</div>
        </div>
      </RouterLink>

      <RouterLink
        to="/devices"
        class="panel p-4 flex items-center gap-4 hover:dark:border-slate-600 hover:border-slate-300 transition-colors"
      >
        <div class="w-10 h-10 rounded-xl bg-violet-500/10 flex items-center justify-center shrink-0">
          <ComputerDesktopIcon class="w-5 h-5 text-violet-400" />
        </div>
        <div>
          <div class="text-2xl font-bold leading-none">{{ devices.length }}</div>
          <div class="text-xs dark:text-slate-400 text-slate-500 mt-1">Devices</div>
        </div>
      </RouterLink>

      <div class="panel p-4 flex items-center gap-4">
        <div class="w-10 h-10 rounded-xl bg-green-500/10 flex items-center justify-center shrink-0">
          <SignalIcon class="w-5 h-5 text-green-400" />
        </div>
        <div>
          <div class="text-2xl font-bold leading-none text-status-free">{{ onlineCount }}</div>
          <div class="text-xs dark:text-slate-400 text-slate-500 mt-1">Online now</div>
        </div>
      </div>

      <RouterLink
        to="/unknown"
        class="panel p-4 flex items-center gap-4 hover:dark:border-slate-600 hover:border-slate-300 transition-colors"
      >
        <div
          class="w-10 h-10 rounded-xl flex items-center justify-center shrink-0 transition-colors"
          :class="unknown.length > 0 ? 'bg-amber-500/10' : 'bg-slate-500/10'"
        >
          <ExclamationTriangleIcon
            class="w-5 h-5 transition-colors"
            :class="unknown.length > 0 ? 'text-amber-400' : 'dark:text-slate-500 text-slate-400'"
          />
        </div>
        <div>
          <div
            class="text-2xl font-bold leading-none"
            :class="unknown.length > 0 ? 'text-status-observed' : ''"
          >
            {{ unknown.length }}
          </div>
          <div class="text-xs dark:text-slate-400 text-slate-500 mt-1">Unknown</div>
        </div>
      </RouterLink>
    </div>

    <!-- Subnet cards -->
    <div v-if="subnets.length" class="space-y-2">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold">Subnets</h2>
        <div class="flex-1" />
        <RouterLink to="/subnets" class="text-xs text-sky-400 hover:underline">manage →</RouterLink>
      </div>
      <div class="grid gap-3" :class="subnets.length === 1 ? 'grid-cols-1' : 'grid-cols-1 sm:grid-cols-2'">
        <div
          v-for="s in subnets"
          :key="s.id"
          class="panel p-4 cursor-pointer hover:dark:border-slate-600 hover:border-slate-300 transition-colors"
          @click="router.push('/subnets/' + s.id)"
        >
          <div class="flex items-start gap-2 mb-3">
            <div class="flex-1 min-w-0">
              <div class="font-semibold text-sm truncate">{{ s.name }}</div>
              <div class="font-mono text-xs dark:text-slate-400 text-slate-500">{{ s.cidr }}</div>
            </div>
            <div class="text-right shrink-0">
              <div class="text-xs dark:text-slate-500 text-slate-400">
                {{ subnetUsedPct(s) }}% used
              </div>
              <div v-if="s.gateway" class="text-xs dark:text-slate-500 text-slate-400 font-mono">
                gw {{ s.gateway }}
              </div>
            </div>
          </div>

          <!-- Utilization bar -->
          <div class="h-1.5 rounded-full dark:bg-slate-800 bg-slate-200 overflow-hidden flex">
            <div
              class="h-full bg-sky-500 transition-all duration-500"
              :style="{ width: subnetAssignedPct(s) + '%' }"
            />
            <div
              class="h-full bg-amber-400/70 transition-all duration-500"
              :style="{ width: subnetObservedPct(s) + '%' }"
            />
          </div>

          <!-- Legend -->
          <div class="flex gap-3 mt-2 text-xs">
            <span class="dark:text-slate-400 text-slate-500">
              <span class="font-semibold text-sky-400">{{ s.assigned_ips }}</span> assigned
            </span>
            <span class="dark:text-slate-400 text-slate-500">
              <span class="font-semibold text-amber-400">{{ s.observed_ips }}</span> observed
            </span>
            <span class="dark:text-slate-400 text-slate-500">
              <span class="font-semibold text-status-free">{{ s.free_ips }}</span> free
            </span>
            <span v-if="s.conflicts > 0" class="text-status-conflict font-semibold ml-auto">
              {{ s.conflicts }} conflict{{ s.conflicts !== 1 ? 's' : '' }}
            </span>
          </div>
        </div>
      </div>
    </div>
    <div v-else class="panel p-6 text-center text-sm dark:text-slate-500 text-slate-400">
      No subnets yet.
      <RouterLink to="/subnets" class="text-sky-400 hover:underline ml-1">Create one →</RouterLink>
    </div>

    <!-- Live hosts -->
    <div class="space-y-2">
      <div class="flex items-center gap-3">
        <h2 class="text-sm font-semibold">
          Live hosts
          <span class="dark:text-slate-500 text-slate-400 font-normal ml-1">
            ({{ filteredHosts.length }}{{ hostSearch ? ' matching' : '' }})
          </span>
        </h2>
        <div class="flex-1" />
        <div class="relative">
          <MagnifyingGlassIcon class="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 dark:text-slate-500 text-slate-400 pointer-events-none" />
          <input
            v-model="hostSearch"
            placeholder="search ip, hostname, mac, vendor…"
            class="input pl-8 py-1.5 text-xs w-48 sm:w-64"
          />
        </div>
        <div class="flex items-center gap-0.5 rounded-lg dark:bg-slate-800 bg-slate-100 p-0.5">
          <button
            class="p-1.5 rounded-md transition-colors"
            :class="hostView === 'list' ? 'dark:bg-slate-700 bg-white shadow-sm dark:text-white text-slate-800' : 'dark:text-slate-500 text-slate-400 hover:dark:text-slate-300 hover:text-slate-600'"
            title="List view"
            @click="hostView = 'list'"
          >
            <TableCellsIcon class="w-4 h-4" />
          </button>
          <button
            class="p-1.5 rounded-md transition-colors"
            :class="hostView === 'grid' ? 'dark:bg-slate-700 bg-white shadow-sm dark:text-white text-slate-800' : 'dark:text-slate-500 text-slate-400 hover:dark:text-slate-300 hover:text-slate-600'"
            title="Grid view"
            @click="hostView = 'grid'"
          >
            <Squares2X2Icon class="w-4 h-4" />
          </button>
        </div>
      </div>

      <!-- List view -->
      <div v-if="hostView === 'list'" class="panel overflow-hidden">
        <div v-if="!liveHosts.length" class="px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No observations yet — run a scan from a subnet page.
        </div>
        <div v-else-if="!filteredHosts.length" class="px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No hosts match "{{ hostSearch }}".
        </div>
        <table v-else class="w-full text-sm">
          <thead>
            <tr class="border-b dark:border-slate-800 border-slate-100">
              <th class="table-cell w-6"></th>
              <th
                class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
                @click="sort.toggleSort('ip')"
              >IP{{ sort.getSortIndicator('ip') }}</th>
              <th
                class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
                @click="sort.toggleSort('hostname')"
              >Hostname{{ sort.getSortIndicator('hostname') }}</th>
              <th
                class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden sm:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
                @click="sort.toggleSort('mac')"
              >MAC{{ sort.getSortIndicator('mac') }}</th>
              <th
                class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden md:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
                @click="sort.toggleSort('vendor')"
              >Vendor{{ sort.getSortIndicator('vendor') }}</th>
              <th class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden lg:table-cell">Device</th>
              <th
                class="table-cell text-right text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
                @click="sort.toggleSort('last_seen')"
              >Last seen{{ sort.getSortIndicator('last_seen') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="h in visibleHosts"
              :key="h.ip"
              class="border-t dark:border-slate-800/60 border-slate-50 dark:hover:bg-slate-800/30 hover:bg-slate-50 transition-colors cursor-pointer"
              @click="h.device ? router.push('/devices/' + h.device.id) : router.push({ path: '/unknown', query: { ip: h.ip } })"
            >
              <td class="table-cell">
                <OnlineDot :last-seen="h.last_seen" />
              </td>
              <td class="table-cell font-mono text-xs font-medium whitespace-nowrap">
                {{ h.ip }}
              </td>
              <td class="table-cell">
                <span v-if="h.hostname" class="font-medium">{{ h.hostname }}</span>
                <span v-else class="dark:text-slate-600 text-slate-300">—</span>
              </td>
              <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500 hidden sm:table-cell whitespace-nowrap">
                {{ h.mac ?? "—" }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500 hidden md:table-cell">
                {{ h.vendor ?? "—" }}
              </td>
              <td class="table-cell hidden lg:table-cell">
                <RouterLink
                  v-if="h.device"
                  :to="`/devices/${h.device.id}`"
                  class="text-xs text-sky-400 hover:underline"
                  @click.stop
                >
                  {{ h.device.hostname }}
                </RouterLink>
                <span v-else class="text-xs dark:text-slate-600 text-slate-300">unregistered</span>
              </td>
              <td class="table-cell text-right text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                {{ relativeTime(h.last_seen) }}
              </td>
            </tr>
          </tbody>
        </table>

        <!-- Show more -->
        <div
          v-if="!showAllHosts && filteredHosts.length > HOST_PREVIEW"
          class="border-t dark:border-slate-800 border-slate-100 px-4 py-2.5 text-center"
        >
          <button
            class="text-xs text-sky-400 hover:underline"
            @click="showAllHosts = true"
          >
            Show all {{ filteredHosts.length }} hosts
          </button>
        </div>
      </div>

      <!-- Grid view -->
      <template v-else>
        <div v-if="!liveHosts.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No observations yet — run a scan from a subnet page.
        </div>
        <div v-else-if="!filteredHosts.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
          No hosts match "{{ hostSearch }}".
        </div>
        <div v-else class="panel p-3 space-y-3">
          <!-- Legend -->
          <div class="flex flex-wrap gap-x-4 gap-y-1.5 text-xs dark:text-slate-400 text-slate-500">
            <span class="inline-flex items-center gap-1.5">
              <span class="w-3 h-3 rounded-sm bg-sky-600" />
              registered + online
            </span>
            <span class="inline-flex items-center gap-1.5">
              <span class="w-3 h-3 rounded-sm bg-amber-500" />
              unregistered + online
            </span>
            <span class="inline-flex items-center gap-1.5">
              <span class="w-3 h-3 rounded-sm dark:bg-sky-900/50 bg-sky-200" />
              registered + offline
            </span>
            <span class="inline-flex items-center gap-1.5">
              <span class="w-3 h-3 rounded-sm dark:bg-slate-800 bg-slate-200" />
              unregistered + offline
            </span>
          </div>

          <template v-for="group in groupedHosts" :key="group.prefix">
            <div>
              <div
                v-if="groupedHosts.length > 1"
                class="text-xs font-mono dark:text-slate-400 text-slate-500 mb-1 border-b dark:border-slate-800 border-slate-200 pb-0.5"
              >
                {{ group.prefix }}.0/24
              </div>
              <div
                class="grid gap-1"
                style="grid-template-columns: repeat(auto-fill, minmax(2.75rem, 1fr))"
              >
                <button
                  v-for="h in group.hosts"
                  :key="h.ip"
                  class="aspect-square rounded flex items-center justify-center font-mono transition-transform duration-100 hover:scale-110 hover:z-10 relative"
                  :class="[hostCellClass(h), cellFontClass(h.ip), h.online ? 'ring-2 ring-green-400' : '']"
                  :title="`${h.ip}${h.hostname ? ' · ' + h.hostname : ''}${h.device ? ' · ' + h.device.hostname : ''}${h.mac ? ' · ' + h.mac : ''}${h.vendor ? ' · ' + h.vendor : ''} · ${relativeTime(h.last_seen)}`"
                  @click="openHost(h)"
                >
                  {{ lastOctet(h.ip) }}
                </button>
              </div>
            </div>
          </template>
        </div>
      </template>
    </div>

    <!-- Host detail modal (grid click) -->
    <Modal
      v-if="selectedHost"
      :title="selectedHost.ip"
      @close="selectedHost = null"
    >
      <div class="space-y-4">
        <div class="flex items-center gap-3">
          <OnlineDot :last-seen="selectedHost.last_seen" show-label />
          <span class="text-xs dark:text-slate-400 text-slate-500">{{ relativeTime(selectedHost.last_seen) }}</span>
        </div>

        <div class="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
          <div>
            <div class="label">Hostname</div>
            <div>{{ selectedHost.hostname ?? "—" }}</div>
          </div>
          <div>
            <div class="label">Device</div>
            <RouterLink
              v-if="selectedHost.device"
              :to="`/devices/${selectedHost.device.id}`"
              class="text-sky-400 hover:underline"
              @click="selectedHost = null"
            >
              {{ selectedHost.device.hostname }}
            </RouterLink>
            <span v-else class="dark:text-slate-500 text-slate-400">unregistered</span>
          </div>
          <div>
            <div class="label">MAC</div>
            <div class="font-mono text-xs">{{ selectedHost.mac ?? "—" }}</div>
          </div>
          <div>
            <div class="label">Vendor</div>
            <div class="text-xs">{{ selectedHost.vendor ?? selectedHost.device?.vendor ?? "—" }}</div>
          </div>
        </div>

        <div class="flex justify-end gap-2 border-t dark:border-slate-800 border-slate-100 pt-3">
          <button class="btn btn-ghost" @click="selectedHost = null">close</button>
          <button
            v-if="selectedHost.device"
            class="btn btn-secondary"
            @click="router.push('/devices/' + selectedHost.device.id); selectedHost = null"
          >
            open device
          </button>
          <button
            v-else
            class="btn btn-secondary"
            @click="router.push({ path: '/unknown', query: { ip: selectedHost!.ip } }); selectedHost = null"
          >
            view unknown
          </button>
        </div>
      </div>
    </Modal>

  </div>
</template>
