import { ref } from "vue";

export type ToastType = "success" | "error" | "info";

export interface Toast {
  id: number;
  type: ToastType;
  message: string;
}

// Module-level singleton — one toast stack shared across the app.
const toasts = ref<Toast[]>([]);
let seq = 0;

function push(type: ToastType, message: string, timeout: number): number {
  const id = ++seq;
  toasts.value.push({ id, type, message });
  if (timeout > 0) setTimeout(() => dismiss(id), timeout);
  return id;
}

function dismiss(id: number): void {
  toasts.value = toasts.value.filter((t) => t.id !== id);
}

export function useToast() {
  return {
    toasts,
    dismiss,
    success: (message: string) => push("success", message, 4000),
    error: (message: string) => push("error", message, 6000),
    info: (message: string) => push("info", message, 4000),
  };
}
