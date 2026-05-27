<script setup lang="ts">
import {
  ArrowPathIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  Squares2X2Icon,
  TableCellsIcon,
} from "@heroicons/vue/24/outline";
import { computed, onMounted, reactive, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";

import Modal from "@/components/Modal.vue";
import { useAdmin, useDiscovery, useSubnets } from "@/composables/useApi";
import type { LocalNetwork } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { useToast } from "@/composables/useToast";
import { useAuthStore } from "@/stores/auth";
import type { Subnet } from "@/types";

const router = useRouter();
const subnetsApi = useSubnets();
const discovery = useDiscovery();
const adminApi = useAdmin();
const auth = useAuthStore();
const sort = useTableSort();
const toast = useToast();

// ── Detect networks ─────────────────────────────────────────────────────────
const detectOpen = ref(false);
const detectBusy = ref(false);
const detected = ref<LocalNetwork[]>([]);
const detectSelected = ref(new Set<string>());

const newNetworks = computed(() => {
  const have = new Set(subnets.value.map((s) => s.cidr));
  return detected.value.filter((n) => !have.has(n.cidr));
});

async function openDetect() {
  detectOpen.value = true;
  detectBusy.value = true;
  detected.value = [];
  try {
    detected.value = await adminApi.localNetworks();
    detectSelected.value = new Set(newNetworks.value.map((n) => n.cidr));
  } catch {
    detected.value = [];
  } finally {
    detectBusy.value = false;
  }
}

function toggleDetected(cidr: string) {
  const s = new Set(detectSelected.value);
  if (s.has(cidr)) s.delete(cidr);
  else s.add(cidr);
  detectSelected.value = s;
}

async function addDetected() {
  detectBusy.value = true;
  const toCreate = detected.value.filter((n) => detectSelected.value.has(n.cidr));
  let added = 0;
  for (const net of toCreate) {
    try {
      await subnetsApi.create({ name: net.name, cidr: net.cidr });
      added += 1;
    } catch {
      // ignore duplicates / failures
    }
  }
  detectOpen.value = false;
  await refresh();
  toast.success(`Added ${added} subnet${added === 1 ? "" : "s"}.`);
}

const subnets = ref<Subnet[]>([]);
const showForm = ref(false);
const editingId = ref<number | null>(null);
const search = ref("");
const viewMode = ref<"list" | "grid">("list");

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

const filteredSubnets = computed(() => {
  if (!search.value.trim()) return subnets.value;
  const q = search.value.trim().toLowerCase();
  return subnets.value.filter(
    (s) =>
      s.name.toLowerCase().includes(q) ||
      s.cidr.includes(q) ||
      (s.gateway && s.gateway.includes(q)) ||
      (s.description && s.description.toLowerCase().includes(q)) ||
      (s.vlan != null && String(s.vlan).includes(q)),
  );
});

const sortedSubnets = computed(() => sort.sortArray(filteredSubnets.value));

const scanState = reactive<Record<number, { busy: boolean; msg: string | null }>>({});

async function quickScan(s: Subnet, method: "arp" | "ping") {
  if (!scanState[s.id]) scanState[s.id] = { busy: false, msg: null };
  scanState[s.id].busy = true;
  scanState[s.id].msg = null;
  try {
    const res = await discovery.scan(s.id, method);
    scanState[s.id].msg = `${res.observations} found`;
    await refresh();
  } catch {
    scanState[s.id].msg = "failed";
  } finally {
    scanState[s.id].busy = false;
  }
}

const blankForm = () => ({ name: "", cidr: "", gateway: "", vlan: "", description: "" });
const form = ref(blankForm());
const editForm = ref({ name: "", gateway: "", vlan: "", description: "" });
const error = ref<string | null>(null);
const saving = ref(false);

async function refresh() {
  subnets.value = await subnetsApi.list();
}

async function create() {
  error.value = null;
  saving.value = true;
  try {
    await subnetsApi.create({
      name: form.value.name,
      cidr: form.value.cidr,
      gateway: form.value.gateway || null,
      vlan: form.value.vlan ? Number(form.value.vlan) : null,
      description: form.value.description || null,
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

function startEdit(s: Subnet) {
  editingId.value = s.id;
  editForm.value = {
    name: s.name,
    gateway: s.gateway ?? "",
    vlan: s.vlan != null ? String(s.vlan) : "",
    description: s.description ?? "",
  };
}

function cancelEdit() {
  editingId.value = null;
}

async function saveEdit(s: Subnet) {
  saving.value = true;
  error.value = null;
  try {
    await subnetsApi.update(s.id, {
      name: editForm.value.name || s.name,
      gateway: editForm.value.gateway || null,
      vlan: editForm.value.vlan ? Number(editForm.value.vlan) : null,
      description: editForm.value.description || null,
    });
    editingId.value = null;
    await refresh();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}

async function remove(s: Subnet) {
  if (!confirm(`Delete subnet ${s.name} (${s.cidr})? All IP assignments will be removed.`))
    return;
  try {
    await subnetsApi.delete(s.id);
    await refresh();
    toast.info(`Subnet ${s.name} deleted.`);
  } catch (e) {
    toast.error(`Could not delete subnet: ${(e as Error).message}`);
  }
}

onMounted(refresh);
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <h1 class="font-semibold">Subnets</h1>
      <div class="flex-1" />
      <button v-if="auth.isAdmin" class="btn btn-secondary" @click="openDetect">
        <MagnifyingGlassIcon class="w-4 h-4" />
        detect networks
      </button>
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
        <PlusIcon v-if="!showForm" class="w-4 h-4" />
        {{ showForm ? "cancel" : "new subnet" }}
      </button>
    </div>

    <!-- Search -->
    <div class="flex flex-wrap gap-2 items-center">
      <div class="relative">
        <MagnifyingGlassIcon class="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 dark:text-slate-500 text-slate-400 pointer-events-none" />
        <input
          v-model="search"
          placeholder="search name, cidr, gateway, vlan…"
          class="input pl-8 py-1.5 text-xs w-48 sm:w-72"
        />
      </div>
      <span class="ml-auto text-xs dark:text-slate-400 text-slate-500">
        {{ filteredSubnets.length }} / {{ subnets.length }}
      </span>
    </div>

    <!-- Create form -->
    <form
      v-if="showForm"
      class="panel p-4 grid grid-cols-1 md:grid-cols-5 gap-3"
      @submit.prevent="create"
    >
      <div>
        <label class="label">Name *</label>
        <input v-model="form.name" class="input" required placeholder="LAN" />
      </div>
      <div>
        <label class="label">CIDR *</label>
        <input v-model="form.cidr" class="input" required placeholder="192.168.1.0/24" />
      </div>
      <div>
        <label class="label">Gateway</label>
        <input v-model="form.gateway" class="input" placeholder="192.168.1.1" />
      </div>
      <div>
        <label class="label">VLAN (0–4094)</label>
        <input v-model="form.vlan" class="input" type="number" min="0" max="4094" />
      </div>
      <div class="md:col-span-5">
        <label class="label">Description</label>
        <input v-model="form.description" class="input" />
      </div>
      <div class="md:col-span-5 flex items-center gap-3">
        <button class="btn btn-primary" :disabled="saving">
          {{ saving ? "creating…" : "create" }}
        </button>
        <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      </div>
    </form>

    <!-- Subnets table -->
    <div v-if="viewMode === 'list'" class="panel overflow-x-auto">
      <table class="w-full text-sm">
        <thead class="dark:text-slate-400 text-slate-500">
          <tr>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('name')">name{{ sort.getSortIndicator('name') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('cidr')">cidr{{ sort.getSortIndicator('cidr') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('gateway')">gateway{{ sort.getSortIndicator('gateway') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('vlan')">vlan{{ sort.getSortIndicator('vlan') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('description')">description{{ sort.getSortIndicator('description') }}</th>
            <th class="table-cell text-right cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('free_ips')">free{{ sort.getSortIndicator('free_ips') }}</th>
            <th class="table-cell text-right cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('assigned_ips')">assigned{{ sort.getSortIndicator('assigned_ips') }}</th>
            <th class="table-cell text-right cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('observed_ips')">observed{{ sort.getSortIndicator('observed_ips') }}</th>
            <th class="table-cell text-right cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('conflicts')">conflicts{{ sort.getSortIndicator('conflicts') }}</th>
            <th class="table-cell"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!subnets.length">
            <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="10">
              No subnets yet.
            </td>
          </tr>
          <template v-for="s in sortedSubnets" :key="s.id">
            <!-- View row -->
            <tr
              v-if="editingId !== s.id"
              class="border-t dark:border-slate-800 border-slate-100 dark:hover:bg-slate-800/40 hover:bg-slate-50 cursor-pointer"
              @click="router.push('/subnets/' + s.id)"
            >
              <td class="table-cell whitespace-nowrap">
                <RouterLink :to="`/subnets/${s.id}`" class="text-sky-400 hover:underline">
                  {{ s.name }}
                </RouterLink>
                <span
                  v-if="s.scan_enabled"
                  class="ml-1.5 inline-flex items-center gap-0.5 text-xs px-1.5 py-0.5 rounded-full bg-green-500/15 text-status-free border border-green-700 align-middle"
                  :title="`Auto-scan every ${s.scan_interval_minutes}m via ${s.scan_method}`"
                ><ArrowPathIcon class="w-3 h-3" /> {{ s.scan_interval_minutes }}m</span>
              </td>
              <td class="table-cell font-mono text-xs">{{ s.cidr }}</td>
              <td class="table-cell">{{ s.gateway ?? "—" }}</td>
              <td class="table-cell">{{ s.vlan ?? "—" }}</td>
              <td class="table-cell dark:text-slate-400 text-slate-500 max-w-[16rem] truncate">
                {{ s.description ?? "—" }}
              </td>
              <td class="table-cell text-right text-status-free">{{ s.free_ips }}</td>
              <td class="table-cell text-right">{{ s.assigned_ips }}</td>
              <td class="table-cell text-right text-status-observed">{{ s.observed_ips }}</td>
              <td
                class="table-cell text-right"
                :class="s.conflicts > 0 ? 'text-status-conflict font-semibold' : ''"
              >
                {{ s.conflicts }}
              </td>
              <td class="table-cell text-right whitespace-nowrap" @click.stop>
                <span
                  v-if="scanState[s.id]?.busy"
                  class="text-xs dark:text-slate-400 text-slate-500 mr-3"
                >scanning…</span>
                <span
                  v-else-if="scanState[s.id]?.msg"
                  class="text-xs dark:text-slate-400 text-slate-500 mr-3"
                >{{ scanState[s.id].msg }}</span>
                <button
                  class="text-xs text-sky-400 hover:underline mr-2"
                  :disabled="scanState[s.id]?.busy"
                  @click="quickScan(s, 'arp')"
                >
                  arp
                </button>
                <button
                  class="text-xs text-sky-400 hover:underline mr-3"
                  :disabled="scanState[s.id]?.busy"
                  @click="quickScan(s, 'ping')"
                >
                  ping
                </button>
                <button
                  class="text-xs text-sky-400 hover:underline mr-2"
                  @click="startEdit(s)"
                >
                  edit
                </button>
                <button class="text-xs text-red-400 hover:underline" @click="remove(s)">
                  delete
                </button>
              </td>
            </tr>
            <!-- Inline edit row -->
            <tr
              v-else
              class="border-t dark:border-slate-800 border-slate-100 dark:bg-slate-800/40 bg-slate-50"
            >
              <td class="table-cell">
                <input v-model="editForm.name" class="input py-0.5" />
              </td>
              <td class="table-cell font-mono text-xs dark:text-slate-400 text-slate-500">
                {{ s.cidr }}
              </td>
              <td class="table-cell">
                <input v-model="editForm.gateway" class="input py-0.5" placeholder="—" />
              </td>
              <td class="table-cell">
                <input
                  v-model="editForm.vlan"
                  class="input py-0.5 w-20"
                  type="number"
                  min="0"
                  max="4094"
                  placeholder="—"
                />
              </td>
              <td class="table-cell" colspan="5">
                <input
                  v-model="editForm.description"
                  class="input py-0.5 w-full"
                  placeholder="description"
                />
              </td>
              <td class="table-cell text-right whitespace-nowrap">
                <button
                  class="text-xs text-sky-400 hover:underline mr-2"
                  :disabled="saving"
                  @click="saveEdit(s)"
                >
                  {{ saving ? "…" : "save" }}
                </button>
                <button class="text-xs dark:text-slate-400 text-slate-500 hover:underline" @click="cancelEdit">
                  cancel
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <p v-if="error && editingId !== null" class="px-3 py-2 text-xs text-red-400">
        {{ error }}
      </p>
    </div>

    <!-- Subnets grid (utilization cards) -->
    <div v-else-if="viewMode === 'grid'">
      <div v-if="!subnets.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
        No subnets yet.
      </div>
      <div v-else-if="!filteredSubnets.length" class="panel px-4 py-8 text-center text-sm dark:text-slate-500 text-slate-400">
        No matches for "{{ search }}".
      </div>
      <div v-else class="grid gap-3 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="s in sortedSubnets"
          :key="s.id"
          class="panel p-4 cursor-pointer hover:dark:border-slate-600 hover:border-slate-300 transition-colors"
          @click="router.push('/subnets/' + s.id)"
        >
          <div class="flex items-start gap-2 mb-3">
            <div class="flex-1 min-w-0">
              <div class="font-semibold text-sm truncate">
                {{ s.name }}
                <span
                  v-if="s.scan_enabled"
                  class="ml-1.5 inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded-full bg-green-500/15 text-status-free border border-green-700 align-middle"
                  :title="`Auto-scan every ${s.scan_interval_minutes}m via ${s.scan_method}`"
                ><ArrowPathIcon class="w-2.5 h-2.5" /> {{ s.scan_interval_minutes }}m</span>
              </div>
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

          <div class="h-1.5 rounded-full dark:bg-slate-800 bg-slate-200 overflow-hidden flex">
            <div class="h-full bg-sky-500 transition-all duration-500" :style="{ width: subnetAssignedPct(s) + '%' }" />
            <div class="h-full bg-amber-400/70 transition-all duration-500" :style="{ width: subnetObservedPct(s) + '%' }" />
          </div>

          <div class="flex gap-3 mt-2 text-xs flex-wrap">
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

          <div class="flex gap-2 mt-3 pt-3 border-t dark:border-slate-800 border-slate-100" @click.stop>
            <button
              class="text-xs text-sky-400 hover:underline"
              :disabled="scanState[s.id]?.busy"
              @click="quickScan(s, 'arp')"
            >arp</button>
            <button
              class="text-xs text-sky-400 hover:underline"
              :disabled="scanState[s.id]?.busy"
              @click="quickScan(s, 'ping')"
            >ping</button>
            <span
              v-if="scanState[s.id]?.busy"
              class="text-xs dark:text-slate-400 text-slate-500"
            >scanning…</span>
            <span
              v-else-if="scanState[s.id]?.msg"
              class="text-xs dark:text-slate-400 text-slate-500"
            >{{ scanState[s.id].msg }}</span>
            <div class="flex-1" />
            <button class="text-xs text-sky-400 hover:underline" @click="startEdit(s)">edit</button>
            <button class="text-xs text-red-400 hover:underline" @click="remove(s)">delete</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Detect networks modal -->
    <Modal v-if="detectOpen" title="Detected networks" @close="detectOpen = false">
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Local interfaces found on the host. Networks you already track are hidden.
      </p>

      <div v-if="detectBusy && !detected.length" class="text-sm dark:text-slate-400 text-slate-500 py-4 text-center">
        Detecting…
      </div>
      <div v-else-if="!newNetworks.length" class="text-sm dark:text-slate-400 text-slate-500 py-4 text-center">
        No new networks detected.
      </div>
      <div v-else class="space-y-2">
        <label
          v-for="net in newNetworks"
          :key="net.cidr"
          class="flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-all duration-150 select-none"
          :class="
            detectSelected.has(net.cidr)
              ? 'dark:border-sky-600 border-sky-400 dark:bg-sky-950/40 bg-sky-50'
              : 'dark:border-slate-700 border-slate-200 dark:hover:border-slate-600 hover:border-slate-300'
          "
        >
          <input
            type="checkbox"
            :checked="detectSelected.has(net.cidr)"
            class="mt-0.5 accent-sky-500"
            @change="toggleDetected(net.cidr)"
          />
          <div class="min-w-0">
            <div class="text-sm font-medium font-mono">{{ net.cidr }}</div>
            <div class="text-xs dark:text-slate-400 text-slate-500">{{ net.interface }} · {{ net.ip }}</div>
          </div>
        </label>
      </div>

      <div class="flex gap-2 justify-end">
        <button class="btn btn-ghost" @click="detectOpen = false">close</button>
        <button
          v-if="newNetworks.length"
          class="btn btn-primary"
          :disabled="detectBusy || detectSelected.size === 0"
          @click="addDetected"
        >
          {{ detectBusy ? "adding…" : `Add ${detectSelected.size}` }}
        </button>
      </div>
    </Modal>
  </div>
</template>
