import { computed, shallowRef } from "vue";

/** Generic multi-select state keyed by id, for bulk table actions. */
export function useSelection<T extends string | number>() {
  // shallowRef + reassignment keeps reactivity simple and avoids deep-unwrap
  // typing issues with a generic Set element type.
  const selected = shallowRef<Set<T>>(new Set());

  const count = computed(() => selected.value.size);
  const isEmpty = computed(() => selected.value.size === 0);

  function isSelected(id: T): boolean {
    return selected.value.has(id);
  }

  function toggle(id: T): void {
    const s = new Set(selected.value);
    if (s.has(id)) s.delete(id);
    else s.add(id);
    selected.value = s;
  }

  function clear(): void {
    selected.value = new Set();
  }

  function allSelected(ids: T[]): boolean {
    return ids.length > 0 && ids.every((id) => selected.value.has(id));
  }

  /** Select every id, or clear if they're all already selected. */
  function toggleAll(ids: T[]): void {
    selected.value = allSelected(ids) ? new Set() : new Set(ids);
  }

  /** Selected ids that still exist in the given list (drops stale ids). */
  function present(ids: T[]): T[] {
    const have = new Set(ids);
    return [...selected.value].filter((id) => have.has(id));
  }

  return { selected, count, isEmpty, isSelected, toggle, clear, allSelected, toggleAll, present };
}
