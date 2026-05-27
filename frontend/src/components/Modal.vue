<script setup lang="ts">
import { XMarkIcon } from "@heroicons/vue/24/outline";
import { onMounted, onUnmounted, ref } from "vue";

defineProps<{ title?: string }>();
const emit = defineEmits<{ close: [] }>();

const show = ref(false);

function close() {
  show.value = false;
  setTimeout(() => emit("close"), 160);
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") close();
}

onMounted(() => {
  requestAnimationFrame(() => { show.value = true; });
  document.addEventListener("keydown", onKey);
});
onUnmounted(() => document.removeEventListener("keydown", onKey));
</script>

<template>
  <Teleport to="body">
    <!-- Backdrop -->
    <Transition
      enter-active-class="transition-opacity duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="show"
        class="fixed inset-0 z-50 bg-black/65 backdrop-blur-md"
        @click="close"
      />
    </Transition>

    <!-- Panel wrapper (centers without being clickable itself) -->
    <div class="fixed inset-0 z-50 flex items-center justify-center px-4 py-6 pointer-events-none">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0 scale-95 translate-y-3"
        enter-to-class="opacity-100 scale-100 translate-y-0"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100 scale-100 translate-y-0"
        leave-to-class="opacity-0 scale-95 translate-y-2"
      >
        <div
          v-if="show"
          class="pointer-events-auto relative w-full max-w-md max-h-[90vh] flex flex-col
                 rounded-2xl overflow-hidden
                 dark:bg-slate-900 bg-white
                 dark:border dark:border-slate-700/60 border border-slate-200
                 shadow-2xl dark:shadow-black/70"
        >
          <!-- Top gradient accent -->
          <div class="absolute top-0 inset-x-0 h-px bg-gradient-to-r from-transparent via-sky-500 to-transparent opacity-80" />

          <!-- Header -->
          <div
            v-if="title || $slots.header"
            class="flex items-center gap-3 px-6 pt-5 pb-4 shrink-0
                   border-b dark:border-slate-800/80 border-slate-100"
          >
            <h3 class="flex-1 text-base font-semibold leading-snug tracking-tight dark:text-slate-100 text-slate-900">
              <slot name="header">{{ title }}</slot>
            </h3>
            <button
              class="shrink-0 w-7 h-7 flex items-center justify-center rounded-lg
                     dark:text-slate-500 text-slate-400
                     dark:hover:bg-slate-800 hover:bg-slate-100
                     dark:hover:text-slate-200 hover:text-slate-700
                     transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-sky-500/50"
              aria-label="Close"
              @click="close"
            >
              <XMarkIcon class="w-4 h-4" />
            </button>
          </div>

          <!-- Content -->
          <div class="overflow-y-auto px-6 py-5 space-y-4">
            <slot />
          </div>
        </div>
      </Transition>
    </div>
  </Teleport>
</template>
