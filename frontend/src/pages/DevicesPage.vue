<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";

import { MagnifyingGlassIcon, Squares2X2Icon, TableCellsIcon } from "@heroicons/vue/24/outline";

import OnlineDot from "@/components/OnlineDot.vue";
import { useDevices } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { isOnline, relativeTime } from "@/utils/format";
import type { Device, DeviceType } from "@/types";

const router = useRouter();
const devicesApi = useDevices();
const sort = useTableSort("primary_ip");

const devices = ref<Device[]>([]);
const showForm = ref(false);
const viewMode = ref<"list" | "grid">("list");
const search = ref("");
const typeFilter = ref<"" | DeviceType>("");

const filteredDevices = computed(() => {
  let out = devices.value;
  if (typeFilter.value) out = out.filter((d) => d.device_type === typeFilter.value);
  if (search.value.trim()) {
    const q = search.value.trim().toLowerCase();
    out = out.filter(
      (d) =>
        d.hostname.toLowerCase().includes(q) ||
        (d.mac_address && d.mac_address.toLowerCase().includes(q)) ||
        (d.primary_ip && d.primary_ip.includes(q)) ||
        (d.vendor && d.vendor.toLowerCase().includes(q)) ||
        (d.location && d.location.toLowerCase().includes(q)) ||
        (d.notes && d.notes.toLowerCase().includes(q)),
    );
  }
  return out;
});

const sortedDevices = computed(() => sort.sortArray(filteredDevices.value));

// ── Grid helpers ──────────────────────────────────────────────────────────────

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

function deviceCellClass(d: Device): string {
  if (isOnline(d.last_seen)) return "bg-sky-600 text-white";
  return "dark:bg-sky-900/50 bg-sky-200 dark:text-sky-300 text-sky-700 dark:hover:bg-sky-900/70 hover:bg-sky-300";
}

const groupedDevices = computed(() => {
  const withIp: Device[] = [];
  const noIp: Device[] = [];
  for (const d of sortedDevices.value) {
    if (d.primary_ip) withIp.push(d);
    else noIp.push(d);
  }
  const byPrefix = new Map<string, Device[]>();
  for (const d of withIp) {
    const parts = d.primary_ip!.split(".");
    const key = parts.length === 4 ? `${parts[0]}.${parts[1]}.${parts[2]}` : "other";
    if (!byPrefix.has(key)) byPrefix.set(key, []);
    byPrefix.get(key)!.push(d);
  }
  const groups = [...byPrefix.entries()]
    .sort(([a], [b]) => ipToNum(a + ".0") - ipToNum(b + ".0"))
    .map(([prefix, devs]) => ({
      prefix,
      devices: [...devs].sort((a, b) => ipToNum(a.primary_ip!) - ipToNum(b.primary_ip!)),
    }));
  if (noIp.length) groups.push({ prefix: "", devices: noIp });
  return groups;
});

const blankForm = () => ({
  hostname: "",
  mac_address: "",
  vendor: "",
  model: "",
  location: "",
  device_type: "unknown" as DeviceType,
  notes: "",
  wg_pubkey: "",
  parent_device_id: null as number | null,
});
const form = ref(blankForm());
const error = ref<string | null>(null);
const saving = ref(false);

const deviceTypes: DeviceType[] = [
  "router",
  "switch",
  "server",
  "vm",
  "container",
  "workstation",
  "iot",
  "unknown",
];

async function refresh() {
  devices.value = await devicesApi.list();
}

async function create() {
  error.value = null;
  saving.value = true;
  try {
    await devicesApi.create({
      hostname: form.value.hostname,
      mac_address: form.value.mac_address || null,
      vendor: form.value.vendor || null,
      model: form.value.model || null,
      location: form.value.location || null,
      device_type: form.value.device_type,
      notes: form.value.notes || null,
      wg_pubkey: form.value.wg_pubkey || null,
      parent_device_id: form.value.parent_device_id || null,
    });
    showForm.value = false;
    form.value = blankForm();
    await refresh();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}

onMounted(refresh);
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center gap-2">
      <h1 class="font-semibold">Devices</h1>
      <div class="flex-1" />
      <div class="flex items-center gap-0.5 rounded-lg dark:bg-slate-800 bg-slate-100 p-0.5">
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
      <button class="btn btn-primary" @click="showForm = !showForm">
        {{ showForm ? "cancel" : "+ new device" }}
      </button>
    </div>

    <!-- Search + filter -->
    <div class="flex flex-wrap gap-2 items-center">
      <div class="relative">
        <MagnifyingGlassIcon class="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 dark:text-slate-500 text-slate-400 pointer-events-none" />
        <input
          v-model="search"
          placeholder="search hostname, mac, ip, vendor…"
          class="input pl-8 py-1.5 text-xs w-48 sm:w-72"
        />
      </div>
      <div class="flex flex-wrap gap-1">
        <button
          v-for="t in ['', ...deviceTypes] as const"
          :key="t"
          class="px-2 py-1 text-xs rounded border transition-colors"
          :class="
            typeFilter === t
              ? 'bg-sky-600 border-sky-600 text-white'
              : 'dark:border-slate-700 border-slate-300 dark:hover:bg-slate-800 hover:bg-slate-100'
          "
          @click="typeFilter = t as ('' | DeviceType)"
        >{{ t || 'all' }}</button>
      </div>
      <span class="ml-auto text-xs dark:text-slate-400 text-slate-500">
        {{ filteredDevices.length }} / {{ devices.length }}
      </span>
    </div>

    <!-- Create form -->
    <form
      v-if="showForm"
      class="panel p-4 grid grid-cols-1 md:grid-cols-6 gap-3"
      @submit.prevent="create"
    >
      <div>
        <label class="label">Hostname *</label>
        <input v-model="form.hostname" class="input" required />
      </div>
      <div>
        <label class="label">MAC</label>
        <input v-model="form.mac_address" class="input" placeholder="aa:bb:cc:dd:ee:ff" />
      </div>
      <div>
        <label class="label">Vendor</label>
        <input v-model="form.vendor" class="input" />
      </div>
      <div>
        <label class="label">Model</label>
        <input v-model="form.model" class="input" placeholder="e.g. TL-SG1024DE 4.0" />
      </div>
      <div>
        <label class="label">Location</label>
        <input v-model="form.location" class="input" placeholder="e.g. rack A-02, shelf 3" />
      </div>
      <div>
        <label class="label">Type</label>
        <select v-model="form.device_type" class="input">
          <option v-for="t in deviceTypes" :key="t" :value="t">{{ t }}</option>
        </select>
      </div>
      <div class="md:col-span-6">
        <label class="label">Notes</label>
        <input v-model="form.notes" class="input" />
      </div>
      <div class="md:col-span-6 flex items-center gap-3">
        <button class="btn btn-primary" :disabled="saving">
          {{ saving ? "creating…" : "create" }}
        </button>
        <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      </div>
    </form>

    <!-- Devices list (matches dashboard live hosts) -->
    <div v-if="viewMode === 'list'" class="panel overflow-hidden">
      <div v-if="!devices.length" class="px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
        No devices yet.
      </div>
      <div v-else-if="!filteredDevices.length" class="px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
        No devices match the current search/filter.
      </div>
      <table v-else class="w-full text-sm">
        <thead>
          <tr class="border-b dark:border-slate-800 border-slate-100">
            <th class="table-cell w-6"></th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('primary_ip')"
            >IP{{ sort.getSortIndicator('primary_ip') }}</th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('hostname')"
            >Hostname{{ sort.getSortIndicator('hostname') }}</th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden sm:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('mac_address')"
            >MAC{{ sort.getSortIndicator('mac_address') }}</th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden md:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('vendor')"
            >Vendor{{ sort.getSortIndicator('vendor') }}</th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden xl:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('model')"
            >Model{{ sort.getSortIndicator('model') }}</th>
            <th
              class="table-cell text-left text-xs dark:text-slate-500 text-slate-400 font-medium hidden lg:table-cell cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('device_type')"
            >Type{{ sort.getSortIndicator('device_type') }}</th>
            <th
              class="table-cell text-right text-xs dark:text-slate-500 text-slate-400 font-medium cursor-pointer hover:dark:text-slate-300 hover:text-slate-600"
              @click="sort.toggleSort('last_seen')"
            >Last seen{{ sort.getSortIndicator('last_seen') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="d in sortedDevices"
            :key="d.id"
            class="border-t dark:border-slate-800/60 border-slate-50 dark:hover:bg-slate-800/30 hover:bg-slate-50 transition-colors cursor-pointer"
            @click="router.push('/devices/' + d.id)"
          >
            <td class="table-cell">
              <OnlineDot :last-seen="d.last_seen" />
            </td>
            <td class="table-cell font-mono text-xs font-medium whitespace-nowrap">
              {{ d.primary_ip ?? "—" }}
            </td>
            <td class="table-cell">
              <RouterLink :to="`/devices/${d.id}`" class="font-medium text-sky-400 hover:underline" @click.stop>
                {{ d.hostname }}
              </RouterLink>
              <span
                v-if="d.wg_pubkey"
                class="ml-1.5 text-xs px-1 rounded bg-violet-900/40 text-violet-300 border border-violet-700"
                title="WireGuard peer"
              >wg</span>
            </td>
            <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500 hidden sm:table-cell whitespace-nowrap">
              {{ d.mac_address ?? "—" }}
            </td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500 hidden md:table-cell">
              {{ d.vendor ?? "—" }}
            </td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500 hidden xl:table-cell">
              {{ d.model ?? "—" }}
            </td>
            <td class="table-cell text-xs hidden lg:table-cell">
              {{ d.device_type }}
            </td>
            <td class="table-cell text-right text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
              {{ relativeTime(d.last_seen) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Devices grid -->
    <div v-if="viewMode === 'grid'">
      <div v-if="!devices.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
        No devices yet.
      </div>
      <div v-else class="space-y-2">
        <!-- Legend -->
        <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs dark:text-slate-400 text-slate-500">
          <span class="flex items-center gap-1.5">
            <span class="inline-block w-3.5 h-3.5 rounded bg-sky-600 ring-2 ring-green-400"></span> online
          </span>
          <span class="flex items-center gap-1.5">
            <span class="inline-block w-3.5 h-3.5 rounded dark:bg-sky-900/50 bg-sky-200"></span> offline
          </span>
        </div>
      <div class="panel p-3 space-y-3">
        <template v-for="group in groupedDevices" :key="group.prefix">
          <div>
            <div
              v-if="group.prefix"
              class="text-xs font-mono dark:text-slate-400 text-slate-500 mb-1 border-b dark:border-slate-800 border-slate-200 pb-0.5"
            >
              {{ group.prefix }}.0/24
            </div>
            <div
              v-else
              class="text-xs dark:text-slate-500 text-slate-400 mb-1 border-b dark:border-slate-800 border-slate-200 pb-0.5"
            >
              no IP assigned
            </div>
            <div
              class="grid gap-1"
              style="grid-template-columns: repeat(auto-fill, minmax(2.75rem, 1fr))"
            >
              <button
                v-for="d in group.devices"
                :key="d.id"
                class="aspect-square rounded flex items-center justify-center font-mono transition-transform duration-100 hover:scale-110 hover:z-10 relative"
                :class="[deviceCellClass(d), d.primary_ip ? cellFontClass(lastOctet(d.primary_ip)) : 'text-[8px]', isOnline(d.last_seen) ? 'ring-2 ring-green-400' : '']"
                :title="`${d.hostname}${d.primary_ip ? ' · ' + d.primary_ip : ''}${d.vendor ? ' · ' + d.vendor : ''} · ${d.device_type}`"
                @click="router.push('/devices/' + d.id)"
              >
                {{ d.primary_ip ? lastOctet(d.primary_ip) : d.hostname.slice(0, 3) }}
              </button>
            </div>
          </div>
        </template>
      </div>
      </div>
    </div>
  </div>
</template>
