import { defineStore } from "pinia";

import { api, ApiError, setToken } from "@/api/client";
import type { User } from "@/types";

interface TokenResponse {
  access_token: string;
  token_type: string;
  user: User;
}

export const useAuthStore = defineStore("auth", {
  state: () => ({
    user: null as User | null,
    hydrated: false,
    setupRequired: false,
  }),
  getters: {
    isAuthenticated: (s) => s.user !== null,
    isAdmin: (s) => s.user?.is_admin === true,
  },
  actions: {
    async hydrate() {
      if (this.hydrated) return;
      try {
        const setup = await api.get<{ setup_required: boolean }>("/api/auth/setup-required");
        this.setupRequired = setup.setup_required;
      } catch {
        this.setupRequired = false;
      }
      try {
        this.user = await api.get<User>("/api/auth/me");
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          setToken(null);
          this.user = null;
        }
      }
      this.hydrated = true;
    },
    async login(username: string, password: string) {
      const form = new FormData();
      form.set("username", username);
      form.set("password", password);
      const res = await api.postForm<TokenResponse>("/api/auth/login", form);
      setToken(res.access_token);
      this.user = res.user;
      this.setupRequired = false;
    },
    async register(username: string, email: string, password: string) {
      const res = await api.post<TokenResponse>("/api/auth/register", {
        username,
        email,
        password,
      });
      setToken(res.access_token);
      this.user = res.user;
      this.setupRequired = false;
    },
    logout() {
      setToken(null);
      this.user = null;
    },
  },
});
