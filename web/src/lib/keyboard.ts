/**
 * The index an arrow, Home, or End key moves focus to within a list of
 * count items, given the currently focused index (-1 when none has focus).
 * Returns null for keys that do not move focus, or for an empty list.
 */
export function nextIndex(current: number, count: number, key: string): number | null {
  if (count === 0) return null;
  switch (key) {
    case "ArrowDown":
      return current < 0 ? 0 : Math.min(current + 1, count - 1);
    case "ArrowUp":
      return current < 0 ? 0 : Math.max(current - 1, 0);
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return null;
  }
}
