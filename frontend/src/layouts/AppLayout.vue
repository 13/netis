<script setup lang="ts">
import {
  ArrowRightStartOnRectangleIcon,
  Bars3Icon,
  MagnifyingGlassIcon,
  MoonIcon,
  SunIcon,
  XMarkIcon,
} from "@heroicons/vue/24/outline";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";

import CommandPalette from "@/components/CommandPalette.vue";
import { useAuth } from "@/composables/useApi";
import { useAuthStore } from "@/stores/auth";
import { useThemeStore } from "@/stores/theme";
import { MIN_PASSWORD_LENGTH } from "@/utils/format";

const auth = useAuthStore();
const theme = useThemeStore();
const authApi = useAuth();
const route = useRoute();
const router = useRouter();

const mobileOpen = ref(false);
watch(route, () => { mobileOpen.value = false; });

const paletteOpen = ref(false);
function onGlobalKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    paletteOpen.value = true;
  }
}
onMounted(() => document.addEventListener("keydown", onGlobalKey));
onUnmounted(() => document.removeEventListener("keydown", onGlobalKey));

const showPwDialog = ref(false);
const pwForm = ref({ current: "", next: "", confirm: "" });
const pwMsg = ref<{ ok: boolean; text: string } | null>(null);
const pwBusy = ref(false);

function openPwDialog() {
  mobileOpen.value = false;
  pwForm.value = { current: "", next: "", confirm: "" };
  pwMsg.value = null;
  showPwDialog.value = true;
}

async function submitPwChange() {
  if (pwForm.value.next !== pwForm.value.confirm) {
    pwMsg.value = { ok: false, text: "New passwords do not match." };
    return;
  }
  if (pwForm.value.next.length < MIN_PASSWORD_LENGTH) {
    pwMsg.value = { ok: false, text: `Password must be at least ${MIN_PASSWORD_LENGTH} characters.` };
    return;
  }
  pwBusy.value = true;
  pwMsg.value = null;
  try {
    await authApi.changePassword(pwForm.value.current, pwForm.value.next);
    pwMsg.value = { ok: true, text: "Password changed successfully." };
    pwForm.value = { current: "", next: "", confirm: "" };
  } catch (e) {
    pwMsg.value = { ok: false, text: (e as Error).message };
  } finally {
    pwBusy.value = false;
  }
}

const baseNav = [
  { to: "/", label: "Dashboard" },
  { to: "/subnets", label: "Subnets" },
  { to: "/devices", label: "Devices" },
  { to: "/unknown", label: "Unknown" },
  { to: "/observations", label: "Observations" },
  { to: "/backup", label: "Backup" },
];

const nav = computed(() => {
  const items = [...baseNav];
  if (auth.isAdmin) items.push({ to: "/users", label: "Users" });
  if (auth.isAdmin) items.push({ to: "/settings", label: "Settings" });
  return items;
});

const isActive = computed(() => (path: string): boolean => {
  if (path === "/") return route.path === "/";
  return route.path.startsWith(path);
});

function logout() {
  auth.logout();
  router.push({ name: "login" });
}
</script>

<template>
  <div class="min-h-screen flex flex-col">
    <header class="border-b dark:border-slate-800 border-slate-200 dark:bg-slate-900 bg-white sticky top-0 z-30">
      <div class="px-4 h-14 flex items-center gap-3">
        <RouterLink to="/" class="font-bold text-sky-500 tracking-tight shrink-0">
          netis
        </RouterLink>

        <!-- Desktop nav -->
        <nav class="hidden md:flex items-center gap-0.5 text-sm flex-1 ml-2">
          <RouterLink
            v-for="item in nav"
            :key="item.to"
            :to="item.to"
            class="px-2.5 py-1.5 rounded-lg transition-all duration-150"
            :class="
              isActive(item.to)
                ? 'dark:bg-slate-800 bg-slate-200 dark:text-slate-100 text-slate-900'
                : 'dark:text-slate-400 text-slate-600 dark:hover:bg-slate-800/60 hover:bg-slate-100'
            "
          >
            {{ item.label }}
          </RouterLink>
        </nav>

        <div class="flex-1 md:hidden" />

        <!-- Desktop right controls -->
        <div class="hidden md:flex items-center gap-1">
          <button
            class="btn btn-ghost text-xs px-2.5 py-1.5 dark:text-slate-400 text-slate-500"
            title="Search (⌘K)"
            @click="paletteOpen = true"
          >
            <MagnifyingGlassIcon class="w-4 h-4" />
            <kbd class="text-[10px] px-1 rounded border dark:border-slate-700 border-slate-300">⌘K</kbd>
          </button>
          <button
            class="btn btn-ghost px-2 py-1.5"
            :title="theme.theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
            @click="theme.toggle()"
          >
            <MoonIcon v-if="theme.theme === 'dark'" class="w-4 h-4" />
            <SunIcon v-else class="w-4 h-4" />
          </button>
          <button
            class="text-xs dark:text-slate-400 text-slate-500 hover:underline px-2 py-1.5"
            @click="openPwDialog"
          >
            {{ auth.user?.username }}<span v-if="auth.user?.is_admin"> · admin</span>
          </button>
          <button class="btn btn-ghost text-xs px-2.5 py-1.5" @click="logout">
            <ArrowRightStartOnRectangleIcon class="w-4 h-4" />
            logout
          </button>
        </div>

        <!-- Mobile search -->
        <button
          class="md:hidden p-2 rounded-lg transition-all duration-150 dark:hover:bg-slate-800 hover:bg-slate-100"
          aria-label="Search"
          @click="paletteOpen = true"
        >
          <MagnifyingGlassIcon class="w-5 h-5" />
        </button>

        <!-- Mobile hamburger -->
        <button
          class="md:hidden p-2 rounded-lg transition-all duration-150 dark:hover:bg-slate-800 hover:bg-slate-100"
          :aria-expanded="mobileOpen"
          aria-label="Toggle navigation"
          @click="mobileOpen = !mobileOpen"
        >
          <XMarkIcon v-if="mobileOpen" class="w-5 h-5" />
          <Bars3Icon v-else class="w-5 h-5" />
        </button>
      </div>

      <!-- Mobile nav drawer -->
      <div
        v-if="mobileOpen"
        class="md:hidden border-t dark:border-slate-800 border-slate-200 px-3 py-3 space-y-1"
      >
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="flex items-center px-3 py-2.5 rounded-lg text-sm transition-all duration-150"
          :class="
            isActive(item.to)
              ? 'dark:bg-slate-800 bg-slate-200 dark:text-slate-100 text-slate-900 font-medium'
              : 'dark:text-slate-400 text-slate-600 dark:hover:bg-slate-800/60 hover:bg-slate-100'
          "
        >
          {{ item.label }}
        </RouterLink>
        <div class="border-t dark:border-slate-700 border-slate-200 pt-3 mt-2 flex items-center gap-2">
          <button class="btn btn-ghost text-xs" @click="theme.toggle()">
            <MoonIcon v-if="theme.theme === 'dark'" class="w-4 h-4" />
            <SunIcon v-else class="w-4 h-4" />
            {{ theme.theme === "dark" ? "dark" : "light" }}
          </button>
          <button
            class="text-xs dark:text-slate-400 text-slate-500 hover:underline flex-1 text-left px-2"
            @click="openPwDialog"
          >
            {{ auth.user?.username }}<span v-if="auth.user?.is_admin"> · admin</span>
          </button>
          <button class="btn btn-ghost text-xs" @click="logout">
            <ArrowRightStartOnRectangleIcon class="w-4 h-4" />
            logout
          </button>
        </div>
      </div>
    </header>

    <main class="flex-1 px-4 py-5 w-full max-w-7xl mx-auto">
      <RouterView />
    </main>

    <CommandPalette :open="paletteOpen" @close="paletteOpen = false" />

    <!-- Change password dialog -->
    <Teleport to="body">
      <div
        v-if="showPwDialog"
        class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 px-4"
        @click.self="showPwDialog = false"
      >
        <div class="panel p-5 w-full max-w-sm space-y-4">
          <h3 class="font-semibold">Change password</h3>
          <form class="space-y-3" @submit.prevent="submitPwChange">
            <div>
              <label class="label">Current password</label>
              <input
                v-model="pwForm.current"
                type="password"
                class="input"
                autocomplete="current-password"
                required
              />
            </div>
            <div>
              <label class="label">New password</label>
              <input
                v-model="pwForm.next"
                type="password"
                class="input"
                autocomplete="new-password"
                :minlength="MIN_PASSWORD_LENGTH"
                required
              />
            </div>
            <div>
              <label class="label">Confirm new password</label>
              <input
                v-model="pwForm.confirm"
                type="password"
                class="input"
                autocomplete="new-password"
                required
              />
            </div>
            <p
              v-if="pwMsg"
              class="text-xs"
              :class="pwMsg.ok ? 'text-green-400' : 'text-red-400'"
            >
              {{ pwMsg.text }}
            </p>
            <div class="flex gap-2 justify-end">
              <button type="button" class="btn btn-ghost" @click="showPwDialog = false">
                cancel
              </button>
              <button type="submit" class="btn btn-primary" :disabled="pwBusy">
                {{ pwBusy ? "saving…" : "change password" }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>
  </div>
</template>
