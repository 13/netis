import { createRouter, createWebHistory } from "vue-router";

import { useAuthStore } from "@/stores/auth";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", name: "login", component: () => import("@/pages/LoginPage.vue") },
    { path: "/setup", name: "setup", component: () => import("@/pages/SetupPage.vue") },
    {
      path: "/",
      component: () => import("@/layouts/AppLayout.vue"),
      meta: { requiresAuth: true },
      children: [
        { path: "", name: "dashboard", component: () => import("@/pages/DashboardPage.vue") },
        { path: "subnets", name: "subnets", component: () => import("@/pages/SubnetsPage.vue") },
        {
          path: "subnets/:id",
          name: "subnet-detail",
          component: () => import("@/pages/SubnetDetailPage.vue"),
          props: true,
        },
        { path: "devices", name: "devices", component: () => import("@/pages/DevicesPage.vue") },
        {
          path: "devices/:id",
          name: "device-detail",
          component: () => import("@/pages/DeviceDetailPage.vue"),
          props: true,
        },
        { path: "unknown", name: "unknown", component: () => import("@/pages/UnknownPage.vue") },
        {
          path: "observations",
          name: "observations",
          component: () => import("@/pages/ObservationsPage.vue"),
        },
        { path: "users", name: "users", component: () => import("@/pages/UsersPage.vue") },
        { path: "backup", name: "backup", component: () => import("@/pages/BackupPage.vue") },
        { path: "settings", name: "settings", component: () => import("@/pages/SettingsPage.vue") },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.hydrated) await auth.hydrate();

  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    if (auth.setupRequired) return { name: "setup" };
    return { name: "login", query: { next: to.fullPath } };
  }
  if ((to.name === "login" || to.name === "setup") && auth.isAuthenticated) {
    return { name: "dashboard" };
  }
});

export default router;
