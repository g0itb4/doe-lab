// The order of a table's rows, which lives in the address like the rest of a
// view: the column, and which way.
export type Sort = { key: string; dir: "asc" | "desc" };

// The order the address asks for: a column the table has, or else the
// table's own order.
export function sortOf(
  key: string | null,
  dir: string | null,
  keys: readonly string[],
  fallback: Sort,
): Sort {
  if (key === null || !keys.includes(key)) return fallback;
  return { key, dir: dir === "desc" ? "desc" : "asc" };
}

// What a click on a column's heading asks for: that column, upwards; and
// the other way when the table is in that column's order already.
export function nextSort(current: Sort, key: string): Sort {
  return { key, dir: current.key === key && current.dir === "asc" ? "desc" : "asc" };
}

// What a column's heading says of itself to a screen reader.
export function ariaSort(current: Sort, key: string): "ascending" | "descending" | "none" {
  if (current.key !== key) return "none";
  return current.dir === "asc" ? "ascending" : "descending";
}

type Value = string | number | undefined;

// The rows in an order, by what each column says a row is worth. Numbers are
// compared as numbers and words as words, with the digits in them as
// numbers. A row with nothing in the column comes last, whichever way the
// rest run; rows worth the same keep the order they came in.
export function sortRows<T>(
  rows: readonly T[],
  sort: Sort,
  columns: Record<string, (row: T) => Value>,
): T[] {
  const worth = columns[sort.key];
  if (!worth) return [...rows];
  const way = sort.dir === "asc" ? 1 : -1;
  return rows
    .map((row, index) => ({ row, index, value: worth(row) }))
    .sort((a, b) => {
      if (a.value === undefined || b.value === undefined) {
        return Number(a.value === undefined) - Number(b.value === undefined) || a.index - b.index;
      }
      const order =
        typeof a.value === "number" && typeof b.value === "number"
          ? a.value - b.value
          : String(a.value).localeCompare(String(b.value), "en-AU", { numeric: true });
      return way * order || a.index - b.index;
    })
    .map(({ row }) => row);
}
