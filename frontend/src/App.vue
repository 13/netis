<script setup lang="ts">
import { onMounted } from "vue";

import ToastHost from "./components/ToastHost.vue";
import { useAuthStore } from "./stores/auth";

const auth = useAuthStore();

onMounted(async () => {
  await auth.hydrate();
});
</script>

<template>
  <router-view />
  <ToastHost />
</template>
