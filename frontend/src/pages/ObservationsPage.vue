<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { ArrowPathIcon } from "@heroicons/vue/24/outline";

import OnlineDot from "@/components/OnlineDot.vue";
import { useDevices, useDiscovery } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { fmtDateTime } from "@/utils/format";
import type { Device, Observation } from "@/types";

const router = useRouter();
const discoveryApi = useDiscovery();
const devicesApi = useDevices();
const sort = useTableSort();

const observations = ref<Observation[]>([]);
const devices = ref<Device[]>([]);
const search = ref("");
const sourceFilter = ref<"" | "arp" | "ping" | "dhcp" | "nmap">("");

const deviceByMac = computed(() => {
  const m = new Map<string, Device>();
  for (const d of devices.value) if (d.mac_address) m.set(d.mac_address, d);
  return m;
});

async function refresh() {
  [observations.value, devices.value] = await Promise.all([
    discoveryApi.observations(),
    devicesApi.list(),
  ]);
}

function openObservation(o: Observation) {
  const device = o.mac_address ? deviceByMac.value.get(o.mac_address) : null;
  if (device) router.push(`/devices/${device.id}`);
  else router.push({ path: "/unknown", query: { ip: o.ip_address } });
}

const filtered = computed(() => {
  let out = observations.value;
  if (sourceFilter.value) out = out.filter((o) => o.source === sourceFilter.value);
  if (search.value.trim()) {
    const q = search.value.trim().toLowerCase();
    out = out.filter(
      (o) =>
        o.ip_address.includes(q) ||
        (o.mac_address && o.mac_address.includes(q)) ||
        (o.vendor && o.vendor.toLowerCase().includes(q)) ||
        (o.hostname && o.hostname.toLowerCase().includes(q)),
    );
  }
  return sort.sortArray(out);
});

onMounted(refresh);
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center">
      <h1 class="font-semibold">Observations</h1>
      <span class="ml-2 text-xs dark:text-slate-400 text-slate-500">
        (last 500, most recent first)
      </span>
      <div class="flex-1" />
      <button class="btn btn-secondary text-xs" @click="refresh">
        <ArrowPathIcon class="w-4 h-4" /> refresh
      </button>
    </div>

    <div class="flex flex-wrap gap-2 items-center">
      <input
        v-model="search"
        placeholder="search ip, mac, hostname…"
        class="input max-w-xs"
      />
      <div class="flex gap-1">
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
        >
          {{ src || "all" }}
        </button>
      </div>
      <span class="ml-auto text-xs dark:text-slate-400 text-slate-500">
        {{ filtered.length }} / {{ observations.length }}
      </span>
    </div>

    <div class="panel overflow-x-auto">
      <table class="w-full text-sm">
        <thead class="dark:text-slate-400 text-slate-500">
          <tr>
            <th class="table-cell w-6"></th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('ip_address')">ip{{ sort.getSortIndicator('ip_address') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('mac_address')">mac{{ sort.getSortIndicator('mac_address') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('vendor')">vendor{{ sort.getSortIndicator('vendor') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('hostname')">hostname{{ sort.getSortIndicator('hostname') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('source')">source{{ sort.getSortIndicator('source') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('first_seen')">first seen{{ sort.getSortIndicator('first_seen') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('last_seen')">last seen{{ sort.getSortIndicator('last_seen') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!filtered.length">
            <td class="table-cell italic dark:text-slate-500 text-slate-400" colspan="8">
              No observations yet. Run an ARP scan or import DHCP leases.
            </td>
          </tr>
          <tr
            v-for="o in filtered"
            :key="o.id"
            class="border-t dark:border-slate-800 border-slate-100 cursor-pointer dark:hover:bg-slate-800/40 hover:bg-slate-50"
            @click="openObservation(o)"
          >
            <td class="table-cell">
              <OnlineDot :last-seen="o.last_seen" />
            </td>
            <td class="table-cell font-mono">{{ o.ip_address }}</td>
            <td class="table-cell font-mono text-xs">{{ o.mac_address ?? "—" }}</td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500">{{ o.vendor ?? "—" }}</td>
            <td class="table-cell">{{ o.hostname ?? "—" }}</td>
            <td class="table-cell">
              <span
                class="text-xs uppercase font-semibold"
                :class="{
                  'text-status-observed': o.source === 'arp',
                  'text-status-reserved': o.source === 'ping',
                  'text-status-dhcp': o.source === 'dhcp',
                  'text-green-500': o.source === 'nmap',
                }"
              >
                {{ o.source }}
              </span>
            </td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500">
              {{ fmtDateTime(o.first_seen) }}
            </td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500">
              {{ fmtDateTime(o.last_seen) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
