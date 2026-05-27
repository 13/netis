<script setup lang="ts">
import { onMounted, ref, watch } from "vue";

import { useAdmin, useDiscovery } from "@/composables/useApi";
import type { AdminInfo, DbConfig } from "@/composables/useApi";

const adminApi = useAdmin();
const discoveryApi = useDiscovery();

const info = ref<AdminInfo | null>(null);
const loadError = ref<string | null>(null);

// Form state mirrors DbConfig
const form = ref<DbConfig>({
  db_type: "sqlite",
  sqlite_path: "/data/netis.db",
  pg_host: "",
  pg_port: 5432,
  pg_user: "netis",
  pg_password: "",
  pg_database: "netis",
});

const testResult = ref<{ ok: boolean; error?: string; url_safe?: string } | null>(null);
const testBusy = ref(false);

const migrateResult = ref<{ migrated: Record<string, number>; url_safe: string; config_saved: boolean } | null>(null);
const migrateBusy = ref(false);
const migrateError = ref<string | null>(null);

const showPgPassword = ref(false);

async function load() {
  try {
    info.value = await adminApi.info();
    // Pre-fill form from current config
    if (info.value.db_type === "sqlite") {
      form.value.db_type = "sqlite";
      const match = info.value.db_url_safe.match(/sqlite:\/\/\/(.+)/);
      form.value.sqlite_path = match ? match[1] : "/data/netis.db";
    } else {
      form.value.db_type = "postgresql";
      // Parse host/port/db from safe URL (password already redacted)
      const match = info.value.db_url_safe.match(
        /postgresql\+psycopg:\/\/([^:@]+)(?::[^@]*)?@([^:/]+):?(\d+)?\/(.+)/,
      );
      if (match) {
        form.value.pg_user = match[1];
        form.value.pg_host = match[2];
        form.value.pg_port = match[3] ? Number(match[3]) : 5432;
        form.value.pg_database = match[4];
      }
    }
  } catch (e) {
    loadError.value = (e as Error).message;
  }
}

// Reset test/migrate state when form changes
watch(form, () => {
  testResult.value = null;
  migrateResult.value = null;
  migrateError.value = null;
}, { deep: true });

async function testConnection() {
  testResult.value = null;
  testBusy.value = true;
  try {
    testResult.value = await adminApi.testDb(form.value);
  } catch (e) {
    testResult.value = { ok: false, error: (e as Error).message };
  } finally {
    testBusy.value = false;
  }
}

async function migrateAndSwitch() {
  if (!confirm(
    "This will copy all data to the target database and save the new URL to the config file.\n\n" +
    "The backend must be restarted for the switch to take effect.\n\n" +
    "Continue?",
  )) return;
  migrateBusy.value = true;
  migrateError.value = null;
  migrateResult.value = null;
  try {
    const res = await adminApi.migrateDb(form.value);
    migrateResult.value = res;
  } catch (e) {
    migrateError.value = (e as Error).message;
  } finally {
    migrateBusy.value = false;
  }
}

// Pi-hole import
const pihole = ref({ url: "", password: "", import_leases: true, import_dns: true });
const piholeResult = ref<{ devices_imported: number; dns_records_imported: number; errors: string[] } | null>(null);
const piholeError = ref<string | null>(null);
const piholeBusy = ref(false);

async function importFromPihole() {
  piholeResult.value = null;
  piholeError.value = null;
  piholeBusy.value = true;
  try {
    piholeResult.value = await discoveryApi.importPihole(pihole.value);
  } catch (e) {
    piholeError.value = (e as Error).message;
  } finally {
    piholeBusy.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="space-y-5 max-w-2xl">
    <h1 class="font-semibold">Settings</h1>

    <p v-if="loadError" class="text-sm text-red-400">{{ loadError }}</p>

    <!-- Current database panel -->
    <div v-if="info" class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Current database</h2>
      <div class="flex items-center gap-3 text-sm">
        <span
          class="px-2 py-0.5 rounded text-xs font-semibold uppercase"
          :class="info.db_type === 'postgresql'
            ? 'bg-blue-900/40 text-blue-300 border border-blue-700'
            : 'bg-slate-800 text-slate-300 border border-slate-700'"
        >
          {{ info.db_type === "postgresql" ? "PostgreSQL" : "SQLite" }}
        </span>
        <span class="font-mono text-xs dark:text-slate-400 text-slate-500 truncate">
          {{ info.db_url_safe }}
        </span>
        <span
          class="text-xs"
          :class="info.connected ? 'text-status-free' : 'text-status-conflict'"
        >
          {{ info.connected ? "● connected" : "✕ disconnected" }}
        </span>
      </div>
      <p class="text-xs dark:text-slate-500 text-slate-400">
        Config file: <span class="font-mono">{{ info.config_file }}</span>
        <span v-if="!info.config_writable" class="text-status-conflict ml-2">(not writable — changes cannot be saved)</span>
      </p>
    </div>

    <!-- Automation status -->
    <div v-if="info" class="panel p-4 space-y-3">
      <h2 class="text-sm font-semibold">Automation</h2>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
        <div class="flex items-center gap-2">
          <span
            class="w-2 h-2 rounded-full"
            :class="info.scheduler_enabled ? 'bg-status-free' : 'dark:bg-slate-600 bg-slate-300'"
          />
          <span>Scan scheduler</span>
          <span class="text-xs dark:text-slate-400 text-slate-500">
            {{ info.scheduler_enabled ? "running" : "disabled" }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <span
            class="w-2 h-2 rounded-full"
            :class="info.alert_webhook_configured ? 'bg-status-free' : 'dark:bg-slate-600 bg-slate-300'"
          />
          <span>Change alerts</span>
          <span class="text-xs dark:text-slate-400 text-slate-500">
            {{ info.alert_webhook_configured ? "webhook configured" : "no webhook" }}
          </span>
        </div>
      </div>
      <p class="text-xs dark:text-slate-500 text-slate-400">
        Per-subnet scan intervals are configured on each subnet's page. Set
        <span class="font-mono">NETIS_ALERT_WEBHOOK_URL</span> to receive a JSON POST when new
        unknown hosts appear, and <span class="font-mono">NETIS_SCHEDULER_ENABLED</span> to toggle
        background scanning.
      </p>
    </div>

    <!-- Pi-hole import -->
    <div class="panel p-4 space-y-4">
      <h2 class="text-sm font-semibold">Pi-hole import</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Pull DHCP leases and local DNS records from a Pi-hole v6 instance. Credentials are used
        only for this request and are not stored.
      </p>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div class="md:col-span-2">
          <label class="label">Pi-hole URL</label>
          <input v-model="pihole.url" class="input" placeholder="http://192.168.1.1" />
        </div>
        <div class="md:col-span-2">
          <label class="label">Password</label>
          <input v-model="pihole.password" type="password" class="input" autocomplete="off" />
        </div>
      </div>
      <div class="flex flex-wrap gap-4 text-sm">
        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" v-model="pihole.import_leases" class="rounded" />
          <span>DHCP leases / network devices</span>
        </label>
        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" v-model="pihole.import_dns" class="rounded" />
          <span>Local DNS records</span>
        </label>
      </div>
      <div
        v-if="piholeResult"
        class="text-sm px-3 py-2 rounded border border-green-700 dark:bg-green-950/30 bg-green-50"
      >
        <span class="text-green-400">
          Imported {{ piholeResult.devices_imported }} device(s) and
          {{ piholeResult.dns_records_imported }} DNS record(s).
        </span>
        <span v-if="piholeResult.errors.length" class="text-status-conflict ml-2">
          {{ piholeResult.errors.length }} error(s).
        </span>
      </div>
      <p v-if="piholeError" class="text-sm text-red-400">{{ piholeError }}</p>
      <button
        class="btn btn-primary"
        :disabled="piholeBusy || !pihole.url"
        @click="importFromPihole"
      >
        {{ piholeBusy ? "importing…" : "import from Pi-hole" }}
      </button>
    </div>

    <!-- Configure target database -->
    <div class="panel p-4 space-y-4">
      <h2 class="text-sm font-semibold">Configure database</h2>
      <p class="text-xs dark:text-slate-400 text-slate-500">
        Select a target database backend. Use <strong>Test connection</strong> to verify
        credentials, then <strong>Migrate &amp; switch</strong> to copy data and save the
        new config. A backend restart is required for the switch to take effect.
      </p>

      <!-- Type toggle -->
      <div class="flex gap-2">
        <button
          class="px-3 py-1.5 text-sm rounded border transition-colors"
          :class="form.db_type === 'sqlite'
            ? 'bg-sky-600 border-sky-600 text-white'
            : 'dark:border-slate-700 border-slate-300 dark:hover:bg-slate-800 hover:bg-slate-100'"
          @click="form.db_type = 'sqlite'"
        >
          SQLite
        </button>
        <button
          class="px-3 py-1.5 text-sm rounded border transition-colors"
          :class="form.db_type === 'postgresql'
            ? 'bg-sky-600 border-sky-600 text-white'
            : 'dark:border-slate-700 border-slate-300 dark:hover:bg-slate-800 hover:bg-slate-100'"
          @click="form.db_type = 'postgresql'"
        >
          PostgreSQL
        </button>
      </div>

      <!-- SQLite options -->
      <div v-if="form.db_type === 'sqlite'" class="space-y-3">
        <div>
          <label class="label">Database file path</label>
          <input
            v-model="form.sqlite_path"
            class="input max-w-sm"
            placeholder="/data/netis.db"
          />
          <p class="text-xs dark:text-slate-500 text-slate-400 mt-1">
            Use an absolute path inside a Docker volume (e.g. <span class="font-mono">/data/netis.db</span>).
          </p>
        </div>
      </div>

      <!-- PostgreSQL options -->
      <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div class="md:col-span-2">
          <label class="label">Host</label>
          <input v-model="form.pg_host" class="input" placeholder="localhost or postgres (Docker)" />
        </div>
        <div>
          <label class="label">Port</label>
          <input v-model.number="form.pg_port" class="input" type="number" min="1" max="65535" />
        </div>
        <div>
          <label class="label">Database</label>
          <input v-model="form.pg_database" class="input" placeholder="netis" />
        </div>
        <div>
          <label class="label">Username</label>
          <input v-model="form.pg_user" class="input" placeholder="netis" autocomplete="username" />
        </div>
        <div>
          <label class="label">Password</label>
          <div class="relative">
            <input
              v-model="form.pg_password"
              :type="showPgPassword ? 'text' : 'password'"
              class="input pr-16"
              placeholder="optional"
              autocomplete="new-password"
            />
            <button
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-xs dark:text-slate-400 text-slate-500 hover:underline"
              @click="showPgPassword = !showPgPassword"
            >
              {{ showPgPassword ? "hide" : "show" }}
            </button>
          </div>
        </div>
        <div class="md:col-span-2">
          <p class="text-xs dark:text-slate-500 text-slate-400">
            For Docker Compose with the <span class="font-mono">postgres</span> profile, host is
            <span class="font-mono">postgres</span> and credentials are
            <span class="font-mono">netis / netis</span> by default.
          </p>
        </div>
      </div>

      <!-- Test result -->
      <div
        v-if="testResult"
        class="text-sm px-3 py-2 rounded border"
        :class="testResult.ok
          ? 'border-green-700 dark:bg-green-950/30 bg-green-50 text-green-400'
          : 'border-red-700 dark:bg-red-950/30 bg-red-50 text-red-400'"
      >
        <span v-if="testResult.ok">
          ✓ Connection successful — <span class="font-mono text-xs">{{ testResult.url_safe }}</span>
        </span>
        <span v-else>✕ {{ testResult.error }}</span>
      </div>

      <!-- Migrate result -->
      <div
        v-if="migrateResult"
        class="text-sm px-3 py-2 rounded border border-green-700 dark:bg-green-950/30 bg-green-50 space-y-1"
      >
        <p class="text-green-400 font-semibold">Migration complete ✓</p>
        <p class="text-xs dark:text-slate-300 text-slate-700">
          Migrated:
          {{ migrateResult.migrated.subnets }} subnet(s),
          {{ migrateResult.migrated.devices }} device(s),
          {{ migrateResult.migrated.ip_addresses }} IP(s),
          {{ migrateResult.migrated.observations }} observation(s),
          {{ migrateResult.migrated.users }} user(s).
        </p>
        <p class="text-xs">
          Target: <span class="font-mono">{{ migrateResult.url_safe }}</span>
        </p>
        <p v-if="!migrateResult.config_saved" class="text-status-conflict text-xs">
          ⚠ Config file could not be written. Set <span class="font-mono">NETIS_DATABASE_URL</span> manually.
        </p>
        <p v-else class="text-status-observed text-xs font-semibold">
          ⚠ Restart the backend for the new database to take effect.
        </p>
      </div>

      <p v-if="migrateError" class="text-sm text-red-400">{{ migrateError }}</p>

      <!-- Actions -->
      <div class="flex gap-2 pt-1">
        <button
          class="btn btn-secondary"
          :disabled="testBusy || migrateBusy"
          @click="testConnection"
        >
          {{ testBusy ? "testing…" : "test connection" }}
        </button>
        <button
          class="btn btn-primary"
          :disabled="testBusy || migrateBusy || !testResult?.ok"
          :title="!testResult?.ok ? 'Run Test connection first' : ''"
          @click="migrateAndSwitch"
        >
          {{ migrateBusy ? "migrating…" : "migrate & switch" }}
        </button>
      </div>
      <p v-if="!testResult?.ok" class="text-xs dark:text-slate-500 text-slate-400">
        Test the connection before migrating.
      </p>
    </div>
  </div>
</template>
