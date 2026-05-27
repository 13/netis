import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "./App.vue";
import "./assets/main.css";
import router from "./router";
import { useThemeStore } from "./stores/theme";

const app = createApp(App);
app.use(createPinia());
app.use(router);

// Initialize theme before mount so the FOUC-free dark default sticks
useThemeStore().init();

app.mount("#app");
