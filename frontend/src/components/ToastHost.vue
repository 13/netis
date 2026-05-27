<script setup lang="ts">
import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  InformationCircleIcon,
  XMarkIcon,
} from "@heroicons/vue/24/outline";

import { useToast } from "@/composables/useToast";
import type { ToastType } from "@/composables/useToast";

const { toasts, dismiss } = useToast();

const icons: Record<ToastType, unknown> = {
  success: CheckCircleIcon,
  error: ExclamationTriangleIcon,
  info: InformationCircleIcon,
};

const accent: Record<ToastType, string> = {
  success: "text-status-free",
  error: "text-status-conflict",
  info: "text-sky-400",
};
</script>

<template>
  <Teleport to="body">
    <div class="fixed bottom-4 right-4 z-[60] flex flex-col gap-2 w-80 max-w-[calc(100vw-2rem)]">
      <TransitionGroup name="toast">
        <div
          v-for="t in toasts"
          :key="t.id"
          class="panel p-3 flex items-start gap-2.5 shadow-lg"
        >
          <component :is="icons[t.type]" class="w-5 h-5 shrink-0 mt-0.5" :class="accent[t.type]" />
          <p class="text-sm flex-1 break-words">{{ t.message }}</p>
          <button
            class="dark:text-slate-400 text-slate-400 hover:text-slate-200 dark:hover:text-slate-100 shrink-0"
            aria-label="Dismiss"
            @click="dismiss(t.id)"
          >
            <XMarkIcon class="w-4 h-4" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition: all 0.2s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateX(1rem);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(1rem);
}
</style>
