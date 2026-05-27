import { computed, ref } from "vue";

export type SortOrder = "asc" | "desc";

interface SortState {
  column: string | null;
  order: SortOrder;
}

/**
 * Composable for managing table sorting state and logic
 * @param defaultColumn - Optional default column to sort by
 */
export function useTableSort(defaultColumn?: string) {
  const sortState = ref<SortState>({
    column: defaultColumn ?? null,
    order: "asc",
  });

  /**
   * Toggle sort on a column. If clicking the same column, reverse order. If new column, sort ascending.
   */
  function toggleSort(column: string) {
    if (sortState.value.column === column) {
      sortState.value.order = sortState.value.order === "asc" ? "desc" : "asc";
    } else {
      sortState.value.column = column;
      sortState.value.order = "asc";
    }
  }

  /**
   * Sort an array of objects by the current sort state
   */
  function sortArray<T extends Record<string, any>>(items: T[]): T[] {
    if (!sortState.value.column) return items;

    const sorted = [...items].sort((a, b) => {
      const aVal = a[sortState.value.column!];
      const bVal = b[sortState.value.column!];

      return compareValues(aVal, bVal, sortState.value.order);
    });

    return sorted;
  }

  /**
   * Get sort indicator for a column header (↑ or ↓)
   */
  const getSortIndicator = (column: string): string => {
    if (sortState.value.column !== column) return "";
    return sortState.value.order === "asc" ? " ↑" : " ↓";
  };

  /**
   * Check if a column is currently being sorted
   */
  const isSorted = (column: string): boolean => {
    return sortState.value.column === column;
  };

  return {
    sortState: computed(() => sortState.value),
    toggleSort,
    sortArray,
    getSortIndicator,
    isSorted,
  };
}

/**
 * Compare two values for sorting
 */
function compareValues(a: any, b: any, order: SortOrder): number {
  // Handle null/undefined
  if (a == null && b == null) return 0;
  if (a == null) return order === "asc" ? 1 : -1;
  if (b == null) return order === "asc" ? -1 : 1;

  // Handle dates
  if (a instanceof Date || typeof a === "string" && isDateString(a)) {
    const aDate = new Date(a);
    const bDate = new Date(b);
    const result = aDate.getTime() - bDate.getTime();
    return order === "asc" ? result : -result;
  }

  // Handle numbers
  if (typeof a === "number" && typeof b === "number") {
    return order === "asc" ? a - b : b - a;
  }

  // Handle booleans (false < true)
  if (typeof a === "boolean" && typeof b === "boolean") {
    const result = a === b ? 0 : a ? 1 : -1;
    return order === "asc" ? result : -result;
  }

  // Handle strings (case-insensitive)
  const aStr = String(a).toLowerCase();
  const bStr = String(b).toLowerCase();

  // Sort IPv4 addresses numerically so 1 < 2 < 10 (not 1 < 10 < 2)
  if (isIpAddress(aStr) && isIpAddress(bStr)) {
    const result = ipToNum(aStr) - ipToNum(bStr);
    return order === "asc" ? result : -result;
  }

  const result = aStr.localeCompare(bStr);
  return order === "asc" ? result : -result;
}

function isDateString(str: string): boolean {
  return /^\d{4}-\d{2}-\d{2}/.test(str);
}

function isIpAddress(str: string): boolean {
  return /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(str);
}

function ipToNum(ip: string): number {
  return ip.split(".").reduce((acc, oct) => acc * 256 + parseInt(oct, 10), 0);
}
