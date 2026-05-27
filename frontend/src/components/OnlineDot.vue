<script setup lang="ts">
import { computed } from "vue";

import { fmtDateTime, isOnline, relativeTime } from "@/utils/format";

const props = withDefaults(
  defineProps<{
    lastSeen: string | null | undefined;
    showLabel?: boolean;
  }>(),
  { showLabel: false },
);

const online = computed(() => isOnline(props.lastSeen));
const title = computed(() =>
  props.lastSeen ? `Last seen ${fmtDateTime(props.lastSeen)}` : "Never observed",
);
</script>

<template>
  <span class="inline-flex items-center gap-1.5 align-middle" :title="title">
    <span
      class="inline-block w-2 h-2 rounded-full shrink-0"
      :class="
        online
          ? 'bg-status-free shadow-[0_0_5px_1px] shadow-green-500/50'
          : 'dark:bg-slate-600 bg-slate-300'
      "
    />
    <span
      v-if="showLabel"
      class="text-xs"
      :class="online ? 'text-status-free' : 'dark:text-slate-500 text-slate-400'"
    >
      {{ online ? "online" : relativeTime(lastSeen) }}
    </span>
  </span>
</template>
