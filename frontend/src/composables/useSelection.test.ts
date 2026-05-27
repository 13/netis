import { describe, expect, it } from "vitest";

import { useSelection } from "./useSelection";

describe("useSelection", () => {
  it("toggles individual ids", () => {
    const s = useSelection<number>();
    expect(s.isEmpty.value).toBe(true);
    s.toggle(1);
    expect(s.isSelected(1)).toBe(true);
    expect(s.count.value).toBe(1);
    s.toggle(1);
    expect(s.isSelected(1)).toBe(false);
    expect(s.isEmpty.value).toBe(true);
  });

  it("selects all then clears via toggleAll", () => {
    const s = useSelection<number>();
    const ids = [1, 2, 3];
    s.toggleAll(ids);
    expect(s.count.value).toBe(3);
    expect(s.allSelected(ids)).toBe(true);
    s.toggleAll(ids);
    expect(s.isEmpty.value).toBe(true);
  });

  it("present() drops ids no longer in the list", () => {
    const s = useSelection<number>();
    s.toggle(1);
    s.toggle(99);
    expect(s.present([1, 2, 3]).sort()).toEqual([1]);
  });

  it("clear() empties the selection", () => {
    const s = useSelection<number>();
    s.toggleAll([1, 2]);
    s.clear();
    expect(s.isEmpty.value).toBe(true);
  });
});
