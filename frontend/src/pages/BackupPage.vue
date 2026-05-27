<script setup lang="ts">
import { ArrowDownTrayIcon } from "@heroicons/vue/24/outline";
import { onMounted, ref } from "vue";

import { useBackup, useDiscovery, useSubnets } from "@/composables/useApi";
import type { Subnet } from "@/types";

const backup = useBackup();
const discovery = useDiscovery();
const subnetsApi = useSubnets();

const importMsg = ref<string | null>(null);
const deviceImportMsg = ref<string | null>(null);
const leaseMsg = ref<string | null>(null);
const wgMsg = ref<string | null>(null);
const wgSubnetId = ref<number | "">("");
const subnets = ref<Subnet[]>([]);
const busy = ref(false);

async function exportData() {
  busy.value = true;
  try {
    const data = await backup.export();
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
    a.href = url;
    a.download = `netis-backup-${stamp}.json`;
    a.click();
    URL.revokeObjectURL(url);
  } finally {
    busy.value = false;
  }
}

async function handleBackupImport(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0];
  if (!file) return;
  importMsg.value = "importing…";
  busy.value = true;
  try {
    const res = await backup.import(file);
    const { subnets, devices, ip_addresses, observations } = res.imported;
    importMsg.value = `Imported: ${subnets} subnet(s), ${devices} device(s), ${ip_addresses} IP(s), ${observations} observation(s).`;
  } catch (e) {
    importMsg.value = `Import failed: ${(e as Error).message}`;
  } finally {
    busy.value = false;
    (ev.target as HTMLInputElement).value = "";
  }
}

async function handleDeviceImport(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0];
  if (!file) return;
  deviceImportMsg.value = "importing…";
  busy.value = true;
  try {
    const res = await backup.importDevices(file);
    let msg = `Created ${res.created} device(s), skipped ${res.skipped} (already known by MAC).`;
    if (res.errors.length) msg += ` ${res.errors.length} row(s) had errors.`;
    deviceImportMsg.value = msg;
  } catch (e) {
    deviceImportMsg.value = `Import failed: ${(e as Error).message}`;
  } finally {
    busy.value = false;
    (ev.target as HTMLInputElement).value = "";
  }
}

async function handleWgImport(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0];
  if (!file) return;
  wgMsg.value = "importing…";
  busy.value = true;
  try {
    const sid = wgSubnetId.value ? Number(wgSubnetId.value) : undefined;
    const res = await discovery.importWg(file, sid);
    wgMsg.value = `Found ${res.peers_found} peer(s): created ${res.devices_created} device(s), updated ${res.devices_updated}, assigned ${res.ips_assigned} IP(s).`;
  } catch (e) {
    wgMsg.value = `Import failed: ${(e as Error).message}`;
  } finally {
    busy.value = false;
    (ev.target as HTMLInputElement).value = "";
  }
}

onMounted(async () => {
  subnets.value = await subnetsApi.list();
});

async function handleLeaseImport(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0];
  if (!file) return;
  leaseMsg.value = "importing…";
  busy.value = true;
  try {
    const res = await discovery.uploadLeases(file);
    leaseMsg.value = `Imported ${res.imported} lease observation(s).`;
  } catch (e) {
    leaseMsg.value = `Import failed: ${(e as Error).message}`;
  } finally {
    busy.value = false;
    (ev.target as HTMLInputElement).value = "";
  }
}
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <h1 class="font-semibold">Backup &amp; import</h1>

    <!-- Export -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Export all data</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Downloads a full JSON backup: subnets, devices, IP assignments, and observations.
        Human-readable and suitable for full restoration.
      </p>
      <button class="btn btn-secondary" :disabled="busy" @click="exportData">
        <ArrowDownTrayIcon class="w-4 h-4" /> download backup.json
      </button>
    </div>

    <!-- Full backup import -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Import netis backup</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Restore from a netis JSON backup. Existing records are kept; duplicates are skipped.
        Requires admin.
      </p>
      <input
        type="file"
        accept=".json,application/json"
        :disabled="busy"
        @change="handleBackupImport"
      />
      <p v-if="importMsg" class="text-xs" :class="importMsg.startsWith('Import failed') ? 'text-red-400' : 'dark:text-slate-300 text-slate-700'">
        {{ importMsg }}
      </p>
    </div>

    <!-- Device CSV/JSON import -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Import devices</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Import devices from a <strong>CSV</strong> or <strong>JSON</strong> file.
        Devices are matched by MAC address — existing MACs are skipped, no overwrite.
      </p>
      <div class="text-xs dark:text-slate-500 text-slate-400 space-y-1">
        <div>
          <strong class="dark:text-slate-300 text-slate-600">CSV columns:</strong>
          hostname, mac_address, vendor, device_type, notes
        </div>
        <div>
          <strong class="dark:text-slate-300 text-slate-600">JSON:</strong>
          array of objects with the same fields
        </div>
        <div>
          <strong class="dark:text-slate-300 text-slate-600">device_type values:</strong>
          router, switch, server, vm, container, workstation, iot, unknown
        </div>
      </div>
      <input
        type="file"
        accept=".csv,.json,text/csv,application/json"
        :disabled="busy"
        @change="handleDeviceImport"
      />
      <p
        v-if="deviceImportMsg"
        class="text-xs"
        :class="deviceImportMsg.startsWith('Import failed') ? 'text-red-400' : 'dark:text-slate-300 text-slate-700'"
      >
        {{ deviceImportMsg }}
      </p>
    </div>

    <!-- WireGuard config import -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Import WireGuard peers</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Upload a <strong>wg0.conf</strong> (wg-quick format) or the output of
        <strong>wg show</strong>. Each peer becomes a device record (deduplicated by
        public key) and receives an authoritative <em>static</em> IP assignment — no
        re-scan needed since WireGuard IPs are intentional allocations.
      </p>
      <div class="text-xs dark:text-slate-500 text-slate-400 space-y-1">
        <div>
          <strong class="dark:text-slate-300 text-slate-600">wg0.conf:</strong>
          <span class="font-mono"> [Peer] / PublicKey = … / AllowedIPs = 10.8.0.x/32</span>
        </div>
        <div>
          <strong class="dark:text-slate-300 text-slate-600">wg show:</strong>
          <span class="font-mono"> peer: PUBKEY / allowed ips: 10.8.0.x/32</span>
        </div>
        <div>Add a <span class="font-mono"># comment</span> line before <span class="font-mono">PublicKey</span> to name the peer.</div>
      </div>
      <div>
        <label class="label">Target subnet (optional — auto-detected from AllowedIPs)</label>
        <select v-model="wgSubnetId" class="input max-w-xs">
          <option value="">— auto-detect —</option>
          <option v-for="s in subnets" :key="s.id" :value="s.id">
            {{ s.name }} ({{ s.cidr }})
          </option>
        </select>
      </div>
      <input type="file" :disabled="busy" @change="handleWgImport" />
      <p
        v-if="wgMsg"
        class="text-xs"
        :class="wgMsg.startsWith('Import failed') ? 'text-red-400' : 'dark:text-slate-300 text-slate-700'"
      >
        {{ wgMsg }}
      </p>
    </div>

    <!-- DHCP lease import -->
    <div class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Import DHCP leases</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Auto-detects dnsmasq, ISC dhcpd, CSV, or JSON (including UniFi controller exports).
        Creates <strong>observations only</strong> — authoritative assignments are never
        overwritten.
      </p>
      <div class="text-xs dark:text-slate-500 text-slate-400 space-y-1">
        <div><strong class="dark:text-slate-300 text-slate-600">dnsmasq:</strong> /var/lib/misc/dnsmasq.leases</div>
        <div><strong class="dark:text-slate-300 text-slate-600">ISC dhcpd:</strong> /var/lib/dhcpd/dhcpd.leases</div>
        <div><strong class="dark:text-slate-300 text-slate-600">CSV columns:</strong> ip, mac, hostname</div>
        <div><strong class="dark:text-slate-300 text-slate-600">JSON:</strong> array of {ip, mac, hostname} or UniFi export</div>
      </div>
      <input type="file" :disabled="busy" @change="handleLeaseImport" />
      <p
        v-if="leaseMsg"
        class="text-xs"
        :class="leaseMsg.startsWith('Import failed') ? 'text-red-400' : 'dark:text-slate-300 text-slate-700'"
      >
        {{ leaseMsg }}
      </p>
    </div>
  </div>
</template>
