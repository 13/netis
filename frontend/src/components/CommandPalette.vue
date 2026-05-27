<script setup lang="ts">
import {
  CubeIcon,
  MagnifyingGlassIcon,
  RectangleGroupIcon,
} from "@heroicons/vue/24/outline";
import { computed, nextTick, ref, watch } from "vue";
import { useRouter } from "vue-router";

import { useDevices, useSubnets } from "@/composables/useApi";
import type { Device, Subnet } from "@/types";

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ close: [] }>();

const router = useRouter();
const subnetsApi = useSubnets();
const devicesApi = useDevices();

const query = ref("");
const subnets = ref<Subnet[]>([]);
const devices = ref<Device[]>([]);
const loaded = ref(false);
const activeIndex = ref(0);
const inputEl = ref<HTMLInputElement | null>(null);

interface Result {
  kind: "subnet" | "device";
  id: number;
  label: string;
  sub: string;
  to: string;
}

async function ensureData() {
  if (loaded.value) return;
  [subnets.value, devices.value] = await Promise.all([subnetsApi.list(), devicesApi.list()]);
  loaded.value = true;
}

const results = computed<Result[]>(() => {
  const q = query.value.trim().toLowerCase();
  const out: Result[] = [];

  for (const s of subnets.value) {
    const hay = `${s.name} ${s.cidr} ${s.description ?? ""}`.toLowerCase();
    if (!q || hay.includes(q)) {
      out.push({ kind: "subnet", id: s.id, label: s.name, sub: s.cidr, to: `/subnets/${s.id}` });
    }
  }
  for (const d of devices.value) {
    const hay = `${d.hostname} ${d.mac_address ?? ""} ${d.primary_ip ?? ""} ${d.vendor ?? ""}`.toLowerCase();
    if (!q || hay.includes(q)) {
      out.push({
        kind: "device",
        id: d.id,
        label: d.hostname,
        sub: d.primary_ip ?? d.mac_address ?? d.device_type,
        to: `/devices/${d.id}`,
      });
    }
  }
  return out.slice(0, 20);
});

watch(results, () => { activeIndex.value = 0; });

watch(
  () => props.open,
  async (open) => {
    if (open) {
      query.value = "";
      activeIndex.value = 0;
      await ensureData();
      await nextTick();
      inputEl.value?.focus();
    }
  },
);

function go(r: Result) {
  emit("close");
  router.push(r.to);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "ArrowDown") {
    e.preventDefault();
    activeIndex.value = Math.min(activeIndex.value + 1, results.value.length - 1);
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    activeIndex.value = Math.max(activeIndex.value - 1, 0);
  } else if (e.key === "Enter") {
    e.preventDefault();
    const r = results.value[activeIndex.value];
    if (r) go(r);
  } else if (e.key === "Escape") {
    emit("close");
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-start justify-center z-[55] px-4 pt-[12vh]"
      @click.self="emit('close')"
    >
      <div class="panel w-full max-w-lg overflow-hidden">
        <div class="flex items-center gap-2 px-3 border-b dark:border-slate-800 border-slate-200">
          <MagnifyingGlassIcon class="w-5 h-5 dark:text-slate-400 text-slate-500 shrink-0" />
          <input
            ref="inputEl"
            v-model="query"
            class="flex-1 bg-transparent py-3 text-sm focus:outline-none"
            placeholder="Search subnets, devices, IPs, MACs…"
            @keydown="onKeydown"
          />
          <kbd class="text-[10px] px-1.5 py-0.5 rounded border dark:border-slate-700 border-slate-300 dark:text-slate-400 text-slate-500">esc</kbd>
        </div>

        <ul class="max-h-80 overflow-y-auto py-1">
          <li v-if="!results.length" class="px-4 py-6 text-sm text-center dark:text-slate-500 text-slate-400">
            No matches.
          </li>
          <li
            v-for="(r, i) in results"
            :key="`${r.kind}-${r.id}`"
            class="px-3 py-2 flex items-center gap-3 cursor-pointer mx-1 rounded-lg"
            :class="i === activeIndex ? 'dark:bg-slate-800 bg-slate-100' : ''"
            @mouseenter="activeIndex = i"
            @click="go(r)"
          >
            <RectangleGroupIcon v-if="r.kind === 'subnet'" class="w-4 h-4 text-sky-400 shrink-0" />
            <CubeIcon v-else class="w-4 h-4 text-violet-400 shrink-0" />
            <span class="text-sm flex-1 truncate">{{ r.label }}</span>
            <span class="text-xs font-mono dark:text-slate-400 text-slate-500 truncate">{{ r.sub }}</span>
            <span class="text-[10px] uppercase dark:text-slate-600 text-slate-400">{{ r.kind }}</span>
          </li>
        </ul>
      </div>
    </div>
  </Teleport>
</template>
