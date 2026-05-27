<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import Modal from "@/components/Modal.vue";
import OnlineDot from "@/components/OnlineDot.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import { useDevices, useDiscovery } from "@/composables/useApi";
import type { DeviceIPRow } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { useToast } from "@/composables/useToast";
import type { Device, DeviceType, Observation } from "@/types";
import { fmtDate, fmtDateTime, isOnline } from "@/utils/format";

const props = defineProps<{ id: string }>();

const router = useRouter();
const toast = useToast();
const devicesApi = useDevices();
const discoveryApi = useDiscovery();
const sortIps = useTableSort("ip_address");
const sortChildren = useTableSort("hostname");
const sortObservations = useTableSort("last_seen");

const device = ref<Device | null>(null);
const deviceIpRows = ref<DeviceIPRow[]>([]);
const allObservations = ref<Observation[]>([]);
const allDevices = ref<Device[]>([]);

const editing = ref(false);
const saving = ref(false);
const error = ref<string | null>(null);

const editForm = ref<{
  hostname: string;
  mac_address: string;
  vendor: string;
  model: string;
  location: string;
  device_type: DeviceType;
  notes: string;
  wg_pubkey: string;
  parent_device_id: number | null;
}>({
  hostname: "",
  mac_address: "",
  vendor: "",
  model: "",
  location: "",
  device_type: "unknown",
  notes: "",
  wg_pubkey: "",
  parent_device_id: null,
});

const deviceTypes: DeviceType[] = [
  "router", "switch", "server", "vm", "container", "workstation", "iot", "unknown",
];

const online = computed(() => isOnline(device.value?.last_seen));

const deviceObservations = computed<Observation[]>(() =>
  allObservations.value.filter(
    (o) =>
      o.mac_address &&
      device.value?.mac_address &&
      o.mac_address === device.value.mac_address,
  ),
);

// IPs the device has been observed at, that are NOT already an assignment.
const observedOnlyIps = computed(() => {
  const assigned = new Set(deviceIpRows.value.map((r) => r.ip_address));
  const seen = new Map<string, Observation>();
  for (const o of deviceObservations.value) {
    if (assigned.has(o.ip_address)) continue;
    const cur = seen.get(o.ip_address);
    if (!cur || new Date(o.last_seen) > new Date(cur.last_seen)) seen.set(o.ip_address, o);
  }
  return [...seen.values()];
});

const sortedDeviceIpRows = computed(() => {
  const combined = [...deviceIpRows.value, ...observedOnlyIps.value];
  return sortIps.sortArray(combined);
});

const sortedChildren = computed(() => sortChildren.sortArray(device.value?.children ?? []));

const sortedDeviceObservations = computed(() => sortObservations.sortArray(deviceObservations.value));

function startEdit() {
  if (!device.value) return;
  editForm.value = {
    hostname: device.value.hostname,
    mac_address: device.value.mac_address ?? "",
    vendor: device.value.vendor ?? "",
    model: device.value.model ?? "",
    location: device.value.location ?? "",
    device_type: device.value.device_type,
    notes: device.value.notes ?? "",
    wg_pubkey: device.value.wg_pubkey ?? "",
    parent_device_id: device.value.parent_device_id ?? null,
  };
  error.value = null;
  editing.value = true;
}

async function removeDevice() {
  if (!device.value) return;
  if (!confirm(`Delete device ${device.value.hostname}? This cannot be undone.`)) return;
  try {
    await devicesApi.delete(device.value.id);
    toast.info(`Device ${device.value.hostname} deleted.`);
    router.push("/devices");
  } catch (e) {
    toast.error(`Could not delete device: ${(e as Error).message}`);
  }
}

async function saveEdit() {
  if (!device.value) return;
  saving.value = true;
  error.value = null;
  try {
    const updated = await devicesApi.update(device.value.id, {
      hostname: editForm.value.hostname || device.value.hostname,
      mac_address: editForm.value.mac_address || null,
      vendor: editForm.value.vendor || null,
      model: editForm.value.model || null,
      location: editForm.value.location || null,
      device_type: editForm.value.device_type,
      notes: editForm.value.notes || null,
      wg_pubkey: editForm.value.wg_pubkey || null,
      parent_device_id: editForm.value.parent_device_id || null,
    });
    device.value = updated;
    editing.value = false;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}

async function refresh() {
  const id = Number(props.id);
  [device.value, deviceIpRows.value, allObservations.value, allDevices.value] = await Promise.all([
    devicesApi.get(id),
    devicesApi.ips(id),
    discoveryApi.observations(),
    devicesApi.list(),
  ]);
}

function deviceName(id: number | null): string {
  if (!id) return "—";
  const d = allDevices.value.find((d) => d.id === id);
  return d ? d.hostname : `#${id}`;
}

function childInfo(id: number): Device | undefined {
  return allDevices.value.find((d) => d.id === id);
}

const typeColors: Record<string, string> = {
  router: "dark:bg-sky-900/40 bg-sky-100 dark:text-sky-300 text-sky-700 dark:border-sky-700 border-sky-300",
  switch: "dark:bg-cyan-900/40 bg-cyan-100 dark:text-cyan-300 text-cyan-700 dark:border-cyan-700 border-cyan-300",
  server: "dark:bg-violet-900/40 bg-violet-100 dark:text-violet-300 text-violet-700 dark:border-violet-700 border-violet-300",
  vm: "dark:bg-indigo-900/40 bg-indigo-100 dark:text-indigo-300 text-indigo-700 dark:border-indigo-700 border-indigo-300",
  container: "dark:bg-blue-900/40 bg-blue-100 dark:text-blue-300 text-blue-700 dark:border-blue-700 border-blue-300",
  workstation: "dark:bg-emerald-900/40 bg-emerald-100 dark:text-emerald-300 text-emerald-700 dark:border-emerald-700 border-emerald-300",
  iot: "dark:bg-amber-900/40 bg-amber-100 dark:text-amber-300 text-amber-700 dark:border-amber-700 border-amber-300",
  unknown: "dark:bg-slate-800 bg-slate-200 dark:text-slate-300 text-slate-600 dark:border-slate-700 border-slate-300",
};

onMounted(refresh);
</script>

<template>
  <div v-if="device" class="space-y-4 max-w-4xl mx-auto">
    <!-- Header -->
    <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
      <router-link to="/devices" class="text-xs text-sky-400 hover:underline">← devices</router-link>
      <h1 class="font-semibold text-lg flex items-center gap-2">
        <OnlineDot :last-seen="device.last_seen" />
        {{ device.hostname }}
      </h1>
      <span
        class="text-xs px-2 py-0.5 rounded-full border font-medium"
        :class="typeColors[device.device_type] ?? typeColors.unknown"
      >
        {{ device.device_type }}
      </span>
      <span
        v-if="device.wg_pubkey"
        class="text-xs px-2 py-0.5 rounded-full bg-violet-900/40 text-violet-300 border border-violet-700"
      >wireguard</span>
      <div class="flex-1" />
      <button class="btn btn-secondary" @click="startEdit">edit</button>
      <button class="btn btn-secondary text-red-400" @click="removeDevice">delete</button>
    </div>

    <!-- Identity card -->
    <div class="panel p-4 grid grid-cols-2 md:grid-cols-4 gap-x-4 gap-y-4 text-sm">
      <div>
        <div class="label">Status</div>
        <OnlineDot :last-seen="device.last_seen" show-label />
      </div>
      <div>
        <div class="label">Current IP</div>
        <div class="font-mono">
          <span v-if="device.primary_ip" :class="online ? 'text-status-free' : ''">
            {{ device.primary_ip }}
          </span>
          <span v-else class="dark:text-slate-500 text-slate-400">—</span>
        </div>
      </div>
      <div>
        <div class="label">MAC</div>
        <div class="font-mono text-xs">{{ device.mac_address ?? "—" }}</div>
      </div>
      <div>
        <div class="label">Vendor</div>
        <div>{{ device.vendor ?? "—" }}</div>
      </div>
      <div>
        <div class="label">Model</div>
        <div>{{ device.model ?? "—" }}</div>
      </div>
      <div>
        <div class="label">Location</div>
        <div>{{ device.location ?? "—" }}</div>
      </div>
      <div>
        <div class="label">Parent host</div>
        <div>
          <router-link
            v-if="device.parent_device_id"
            :to="`/devices/${device.parent_device_id}`"
            class="text-sky-400 hover:underline"
          >
            {{ deviceName(device.parent_device_id) }}
          </router-link>
          <span v-else class="dark:text-slate-500 text-slate-400">—</span>
        </div>
      </div>
      <div>
        <div class="label">WireGuard key</div>
        <div
          v-if="device.wg_pubkey"
          class="font-mono text-xs truncate"
          :title="device.wg_pubkey"
        >
          {{ device.wg_pubkey.slice(0, 14) }}…
        </div>
        <span v-else class="dark:text-slate-500 text-slate-400">—</span>
      </div>
      <div>
        <div class="label">Added</div>
        <div>{{ fmtDate(device.created_at) }}</div>
      </div>
      <div>
        <div class="label">Updated</div>
        <div>{{ fmtDate(device.updated_at) }}</div>
      </div>
      <div class="col-span-2 md:col-span-4" v-if="device.notes">
        <div class="label">Notes</div>
        <p class="dark:text-slate-300 text-slate-700 whitespace-pre-wrap">{{ device.notes }}</p>
      </div>
    </div>

    <!-- IP addresses -->
    <div class="panel">
      <div class="px-4 py-2.5 border-b dark:border-slate-800 border-slate-200">
        <h2 class="font-semibold text-sm">IP addresses</h2>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="dark:text-slate-400 text-slate-500">
            <tr>
              <th class="table-cell w-6"></th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortIps.toggleSort('ip_address')">ip{{ sortIps.getSortIndicator('ip_address') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortIps.toggleSort('subnet_name')">subnet{{ sortIps.getSortIndicator('subnet_name') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortIps.toggleSort('status')">status{{ sortIps.getSortIndicator('status') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortIps.toggleSort('description')">description{{ sortIps.getSortIndicator('description') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortIps.toggleSort('last_seen')">last seen{{ sortIps.getSortIndicator('last_seen') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!deviceIpRows.length && !observedOnlyIps.length">
              <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="6">
                No IPs assigned or observed for this device.
              </td>
            </tr>
            <!-- Combined sorted IPs -->
            <template v-for="row in sortedDeviceIpRows" :key="`a-${row.ip_address}`">
              <tr
                v-if="'assignment_id' in row"
                class="border-t dark:border-slate-800 border-slate-100"
              >
                <td class="table-cell">
                  <OnlineDot :last-seen="row.last_seen" />
                </td>
                <td class="table-cell font-mono whitespace-nowrap">
                  {{ row.ip_address }}
                </td>
                <td class="table-cell">
                  <router-link :to="`/subnets/${row.subnet_id}`" class="text-sky-400 hover:underline">
                    {{ row.subnet_name }} <span class="dark:text-slate-500 text-slate-400">({{ row.subnet_cidr }})</span>
                  </router-link>
                </td>
                <td class="table-cell"><StatusBadge :status="row.status as any" /></td>
                <td class="table-cell dark:text-slate-400 text-slate-500">{{ row.description ?? "—" }}</td>
                <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                  {{ fmtDateTime(row.last_seen) }}
                </td>
              </tr>
              <tr
                v-else
                class="border-t dark:border-slate-800 border-slate-100 dark:bg-yellow-950/20 bg-yellow-50/60"
              >
                <td class="table-cell">
                  <OnlineDot :last-seen="row.last_seen" />
                </td>
                <td class="table-cell font-mono whitespace-nowrap">
                  {{ row.ip_address }}
                </td>
                <td class="table-cell dark:text-slate-500 text-slate-400 italic">unassigned</td>
                <td class="table-cell"><StatusBadge status="observed" /></td>
                <td class="table-cell dark:text-slate-400 text-slate-500">via {{ (row as any).source }}</td>
                <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                  {{ fmtDateTime(row.last_seen) }}
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Hosted guests -->
    <div v-if="device.children && device.children.length" class="panel">
      <div class="px-4 py-2.5 border-b dark:border-slate-800 border-slate-200">
        <h2 class="font-semibold text-sm">Hosted guests ({{ device.children.length }})</h2>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="dark:text-slate-400 text-slate-500">
            <tr>
              <th class="table-cell w-6"></th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortChildren.toggleSort('hostname')">hostname{{ sortChildren.getSortIndicator('hostname') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortChildren.toggleSort('device_type')">type{{ sortChildren.getSortIndicator('device_type') }}</th>
              <th class="table-cell text-left">ip</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortChildren.toggleSort('mac_address')">mac{{ sortChildren.getSortIndicator('mac_address') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="child in sortedChildren"
              :key="child.id"
              class="border-t dark:border-slate-800 border-slate-100"
            >
              <td class="table-cell">
                <OnlineDot :last-seen="childInfo(child.id)?.last_seen" />
              </td>
              <td class="table-cell whitespace-nowrap">
                <router-link :to="`/devices/${child.id}`" class="text-sky-400 hover:underline">
                  {{ child.hostname }}
                </router-link>
                <span
                  v-if="child.wg_pubkey"
                  class="ml-1.5 text-xs px-1 rounded bg-violet-900/40 text-violet-300 border border-violet-700"
                >wg</span>
              </td>
              <td class="table-cell">{{ child.device_type }}</td>
              <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500">
                {{ childInfo(child.id)?.primary_ip ?? "—" }}
              </td>
              <td class="table-cell font-mono text-xs">{{ child.mac_address ?? "—" }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Observation history -->
    <div class="panel">
      <div class="px-4 py-2.5 border-b dark:border-slate-800 border-slate-200">
        <h2 class="font-semibold text-sm">
          Observation history
          <span class="dark:text-slate-400 text-slate-500 font-normal text-xs">— matched by MAC</span>
        </h2>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="dark:text-slate-400 text-slate-500">
            <tr>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortObservations.toggleSort('ip_address')">ip{{ sortObservations.getSortIndicator('ip_address') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortObservations.toggleSort('hostname')">hostname{{ sortObservations.getSortIndicator('hostname') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortObservations.toggleSort('source')">source{{ sortObservations.getSortIndicator('source') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortObservations.toggleSort('first_seen')">first seen{{ sortObservations.getSortIndicator('first_seen') }}</th>
              <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sortObservations.toggleSort('last_seen')">last seen{{ sortObservations.getSortIndicator('last_seen') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!device.mac_address">
              <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="5">
                No MAC address — cannot match observations.
              </td>
            </tr>
            <tr v-else-if="!deviceObservations.length">
              <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="5">
                No observations recorded for this MAC yet.
              </td>
            </tr>
            <tr
              v-for="obs in sortedDeviceObservations"
              :key="obs.id"
              class="border-t dark:border-slate-800 border-slate-100"
            >
              <td class="table-cell font-mono">{{ obs.ip_address }}</td>
              <td class="table-cell">{{ obs.hostname ?? "—" }}</td>
              <td class="table-cell">{{ obs.source }}</td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                {{ fmtDateTime(obs.first_seen) }}
              </td>
              <td class="table-cell text-xs dark:text-slate-400 text-slate-500 whitespace-nowrap">
                {{ fmtDateTime(obs.last_seen) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Edit modal -->
    <Modal v-if="editing" title="Edit device" @close="editing = false">
      <form class="grid grid-cols-1 sm:grid-cols-2 gap-3" @submit.prevent="saveEdit">
        <div class="sm:col-span-2">
          <label class="label">Hostname *</label>
          <input v-model="editForm.hostname" class="input" required />
        </div>
        <div>
          <label class="label">MAC</label>
          <input v-model="editForm.mac_address" class="input font-mono text-xs" placeholder="aa:bb:cc:dd:ee:ff" />
        </div>
        <div>
          <label class="label">Vendor</label>
          <input v-model="editForm.vendor" class="input" />
        </div>
        <div>
          <label class="label">Model</label>
          <input v-model="editForm.model" class="input" placeholder="e.g. TL-SG1024DE 4.0" />
        </div>
        <div>
          <label class="label">Location</label>
          <input v-model="editForm.location" class="input" placeholder="e.g. rack A-02, shelf 3" />
        </div>
        <div>
          <label class="label">Type</label>
          <select v-model="editForm.device_type" class="input">
            <option v-for="t in deviceTypes" :key="t" :value="t">{{ t }}</option>
          </select>
        </div>
        <div>
          <label class="label">Parent host</label>
          <select v-model="editForm.parent_device_id" class="input">
            <option :value="null">— none —</option>
            <option
              v-for="p in allDevices.filter((x) => x.id !== device!.id)"
              :key="p.id"
              :value="p.id"
            >
              {{ p.hostname }}
            </option>
          </select>
        </div>
        <div class="sm:col-span-2">
          <label class="label">WireGuard pubkey</label>
          <input v-model="editForm.wg_pubkey" class="input font-mono text-xs" placeholder="base64 public key" />
        </div>
        <div class="sm:col-span-2">
          <label class="label">Notes</label>
          <textarea v-model="editForm.notes" class="input min-h-[80px] resize-y" placeholder="Add notes…" />
        </div>
        <p v-if="error" class="sm:col-span-2 text-xs text-red-400">{{ error }}</p>
        <div class="sm:col-span-2 flex gap-2 justify-end">
          <button type="button" class="btn btn-ghost" @click="editing = false">cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? "saving…" : "save changes" }}
          </button>
        </div>
      </form>
    </Modal>
  </div>
  <div v-else class="dark:text-slate-400 text-slate-500 text-sm">Loading…</div>
</template>
