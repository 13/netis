<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { useUsers } from "@/composables/useApi";
import { useTableSort } from "@/composables/useTableSort";
import { useAuthStore } from "@/stores/auth";
import type { User } from "@/types";
import { fmtDate } from "@/utils/format";

const usersApi = useUsers();
const auth = useAuthStore();
const router = useRouter();
const sort = useTableSort();

const users = ref<User[]>([]);
const busy = ref(false);

const sortedUsers = computed(() => sort.sortArray(users.value));

async function refresh() {
  users.value = await usersApi.list();
}

async function toggleAdmin(u: User) {
  const action = u.is_admin ? "demote" : "promote to admin";
  if (!confirm(`${action} ${u.username}?`)) return;
  busy.value = true;
  try {
    await usersApi.update(u.id, { is_admin: !u.is_admin });
    await refresh();
  } finally {
    busy.value = false;
  }
}

async function remove(u: User) {
  if (!confirm(`Delete user ${u.username}? This cannot be undone.`)) return;
  busy.value = true;
  try {
    await usersApi.delete(u.id);
    // If we just deleted ourselves, log out
    if (u.id === auth.user?.id) {
      auth.logout();
      router.push({ name: "login" });
      return;
    }
    await refresh();
  } finally {
    busy.value = false;
  }
}

onMounted(refresh);
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center">
      <h1 class="font-semibold">Users</h1>
      <span
        v-if="!auth.isAdmin"
        class="ml-3 text-xs dark:text-slate-400 text-slate-500"
      >
        (read-only — admin required for changes)
      </span>
    </div>

    <div class="panel">
      <table class="w-full text-sm">
        <thead class="dark:text-slate-400 text-slate-500">
          <tr>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('username')">username{{ sort.getSortIndicator('username') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('email')">email{{ sort.getSortIndicator('email') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('is_admin')">role{{ sort.getSortIndicator('is_admin') }}</th>
            <th class="table-cell text-left cursor-pointer hover:dark:text-slate-300 hover:text-slate-600" @click="sort.toggleSort('created_at')">joined{{ sort.getSortIndicator('created_at') }}</th>
            <th v-if="auth.isAdmin" class="table-cell"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="u in sortedUsers"
            :key="u.id"
            class="border-t dark:border-slate-800 border-slate-100"
            :class="u.id === auth.user?.id ? 'dark:bg-slate-800/30 bg-slate-50' : ''"
          >
            <td class="table-cell">
              {{ u.username }}
              <span
                v-if="u.id === auth.user?.id"
                class="text-xs dark:text-slate-500 text-slate-400 ml-1"
              >(you)</span>
            </td>
            <td class="table-cell">{{ u.email }}</td>
            <td class="table-cell">
              <span
                :class="u.is_admin ? 'text-sky-400 font-semibold' : 'dark:text-slate-400 text-slate-500'"
              >
                {{ u.is_admin ? "admin" : "user" }}
              </span>
            </td>
            <td class="table-cell text-xs dark:text-slate-400 text-slate-500">
              {{ fmtDate(u.created_at) }}
            </td>
            <td v-if="auth.isAdmin" class="table-cell text-right whitespace-nowrap">
              <button
                class="text-xs text-sky-400 hover:underline mr-3"
                :disabled="busy || u.id === auth.user?.id"
                @click="toggleAdmin(u)"
              >
                {{ u.is_admin ? "demote" : "→ admin" }}
              </button>
              <button
                class="text-xs text-red-400 hover:underline"
                :disabled="busy || u.id === auth.user?.id"
                @click="remove(u)"
              >
                delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
