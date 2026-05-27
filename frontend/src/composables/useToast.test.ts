import { describe, expect, it } from "vitest";

import { useToast } from "./useToast";

describe("useToast", () => {
  it("adds a toast and removes it by id", () => {
    const { toasts, success, dismiss } = useToast();
    const before = toasts.value.length;

    const id = success("saved");
    expect(toasts.value.length).toBe(before + 1);
    expect(toasts.value.some((t) => t.id === id && t.type === "success" && t.message === "saved"))
      .toBe(true);

    dismiss(id);
    expect(toasts.value.some((t) => t.id === id)).toBe(false);
  });

  it("tags error toasts with the error type", () => {
    const { toasts, error, dismiss } = useToast();
    const id = error("boom");
    const t = toasts.value.find((x) => x.id === id);
    expect(t?.type).toBe("error");
    dismiss(id);
  });
});
