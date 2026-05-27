<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";

import { useAdmin, useSubnets } from "@/composables/useApi";
import type { LocalNetwork } from "@/composables/useApi";
import { useAuthStore } from "@/stores/auth";
import { MIN_PASSWORD_LENGTH } from "@/utils/format";

const auth = useAuthStore();
const router = useRouter();
const adminApi = useAdmin();
const subnetsApi = useSubnets();

const step = ref<1 | 2>(1);

// Step 1: account creation
const username = ref("");
const email = ref("");
const password = ref("");
const error = ref<string | null>(null);
const loading = ref(false);

// Step 2: network seeding
const networks = ref<LocalNetwork[]>([]);
const selected = ref(new Set<string>());
const seeding = ref(false);

async function submitAccount() {
  error.value = null;
  loading.value = true;
  try {
    await auth.register(username.value, email.value, password.value);
    try {
      networks.value = await adminApi.localNetworks();
    } catch {
      // silently skip if detection unavailable
    }
    if (networks.value.length > 0) {
      selected.value = new Set(networks.value.map((n) => n.cidr));
      step.value = 2;
    } else {
      router.push("/");
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function toggleNetwork(cidr: string) {
  const s = new Set(selected.value);
  if (s.has(cidr)) s.delete(cidr);
  else s.add(cidr);
  selected.value = s;
}

async function seedAndContinue() {
  seeding.value = true;
  const toCreate = networks.value.filter((n) => selected.value.has(n.cidr));
  for (const net of toCreate) {
    try {
      await subnetsApi.create({ name: net.name, cidr: net.cidr });
    } catch {
      // ignore duplicates / failures
    }
  }
  router.push("/");
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center px-4">

    <!-- Step 1: Create admin account -->
    <form
      v-if="step === 1"
      class="panel p-6 w-full max-w-sm space-y-4"
      @submit.prevent="submitAccount"
    >
      <div>
        <h1 class="text-xl font-bold text-sky-500">netis · setup</h1>
        <p class="text-xs dark:text-slate-400 text-slate-500 mt-1">
          Create the first admin account.
        </p>
      </div>
      <div>
        <label class="label">Username</label>
        <input
          v-model="username"
          class="input"
          required
          autofocus
          autocomplete="username"
        />
      </div>
      <div>
        <label class="label">Email</label>
        <input
          v-model="email"
          type="email"
          class="input"
          required
          autocomplete="email"
        />
      </div>
      <div>
        <label class="label">Password</label>
        <input
          v-model="password"
          type="password"
          class="input"
          :minlength="MIN_PASSWORD_LENGTH"
          required
          autocomplete="new-password"
        />
      </div>
      <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      <button class="btn btn-primary w-full justify-center" :disabled="loading">
        {{ loading ? "Creating…" : "Create admin" }}
      </button>
    </form>

    <!-- Step 2: Seed subnets from detected networks -->
    <div v-else class="panel p-6 w-full max-w-md space-y-5">
      <div>
        <h1 class="text-xl font-bold text-sky-500">netis · setup</h1>
        <p class="text-sm font-semibold mt-3">Detected networks</p>
        <p class="text-xs dark:text-slate-400 text-slate-500 mt-1">
          Select the networks to add as subnets. You can add more at any time.
        </p>
      </div>

      <div class="space-y-2">
        <label
          v-for="net in networks"
          :key="net.cidr"
          class="flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-all duration-150 select-none"
          :class="
            selected.has(net.cidr)
              ? 'dark:border-sky-600 border-sky-400 dark:bg-sky-950/40 bg-sky-50'
              : 'dark:border-slate-700 border-slate-200 dark:hover:border-slate-600 hover:border-slate-300'
          "
        >
          <input
            type="checkbox"
            :checked="selected.has(net.cidr)"
            class="mt-0.5 accent-sky-500"
            @change="toggleNetwork(net.cidr)"
          />
          <div class="min-w-0">
            <div class="text-sm font-medium font-mono">{{ net.cidr }}</div>
            <div class="text-xs dark:text-slate-400 text-slate-500">
              {{ net.interface }} · {{ net.ip }}
            </div>
          </div>
        </label>
      </div>

      <div class="flex gap-3">
        <button class="btn btn-ghost" @click="router.push('/')">Skip</button>
        <button
          class="btn btn-primary flex-1 justify-center"
          :disabled="seeding || selected.size === 0"
          @click="seedAndContinue"
        >
          {{ seeding ? "Adding…" : `Add ${selected.size} subnet${selected.size !== 1 ? "s" : ""}` }}
        </button>
      </div>
    </div>

  </div>
</template>
