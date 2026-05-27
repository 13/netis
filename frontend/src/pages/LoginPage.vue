<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();

const username = ref("");
const password = ref("");
const error = ref<string | null>(null);
const loading = ref(false);

async function submit() {
  error.value = null;
  loading.value = true;
  try {
    await auth.login(username.value, password.value);
    const next = (route.query.next as string) || "/";
    router.push(next);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center px-4">
    <form
      class="panel p-6 w-full max-w-sm space-y-4"
      @submit.prevent="submit"
    >
      <div>
        <h1 class="text-xl font-bold text-sky-500">netis</h1>
        <p class="text-xs dark:text-slate-400 text-slate-500 mt-1">
          Sign in to manage your network.
        </p>
      </div>
      <div>
        <label class="label">Username</label>
        <input
          v-model="username"
          class="input"
          autocomplete="username"
          required
          autofocus
        />
      </div>
      <div>
        <label class="label">Password</label>
        <input
          v-model="password"
          type="password"
          class="input"
          autocomplete="current-password"
          required
        />
      </div>
      <p v-if="error" class="text-xs text-red-400">{{ error }}</p>
      <button class="btn btn-primary w-full justify-center" :disabled="loading">
        {{ loading ? "Signing in…" : "Sign in" }}
      </button>
      <p v-if="auth.setupRequired" class="text-xs text-sky-400">
        No users yet — <router-link to="/setup" class="underline">create the first admin</router-link>.
      </p>
    </form>
  </div>
</template>
