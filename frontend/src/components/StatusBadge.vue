<script setup lang="ts">
import { computed } from "vue";

import type { IPStatus } from "@/types";

const props = defineProps<{ status: IPStatus }>();

const cls = computed(() => {
  switch (props.status) {
    case "free":
      return "bg-status-free";
    case "reserved":
      return "bg-status-reserved";
    case "static":
      return "bg-status-static";
    case "dhcp":
      return "bg-status-dhcp";
    case "observed":
      return "bg-status-observed";
    case "conflict":
      return "bg-status-conflict";
    default:
      return "bg-slate-500";
  }
});
</script>

<template>
  <span class="inline-flex items-center text-xs uppercase font-semibold tracking-wide">
    <span class="status-dot" :class="cls" />
    {{ status }}
  </span>
</template>
