import { describe, expect, it } from "vitest";
import { ariaSort, nextSort, sortOf, sortRows, type Sort } from "./sort.ts";

const byName: Sort = { key: "name", dir: "asc" };

describe("the order in the address", () => {
  it("is a column the table has, one way or the other", () => {
    expect(sortOf("cap", "desc", ["name", "cap"], byName)).toEqual({ key: "cap", dir: "desc" });
    expect(sortOf("cap", null, ["name", "cap"], byName)).toEqual({ key: "cap", dir: "asc" });
    expect(sortOf("cap", "sideways", ["name", "cap"], byName)).toEqual({ key: "cap", dir: "asc" });
  });

  it("is the table's own order for anything else", () => {
    expect(sortOf(null, "desc", ["name", "cap"], byName)).toBe(byName);
    expect(sortOf("colour", "desc", ["name", "cap"], byName)).toBe(byName);
  });
});

describe("a click on a column's heading", () => {
  it("orders by that column, upwards; and the other way when it is the order already", () => {
    expect(nextSort(byName, "cap")).toEqual({ key: "cap", dir: "asc" });
    expect(nextSort(byName, "name")).toEqual({ key: "name", dir: "desc" });
    expect(nextSort({ key: "name", dir: "desc" }, "name")).toEqual(byName);
  });

  it("is said to a screen reader on the column that orders the table", () => {
    expect(ariaSort(byName, "name")).toBe("ascending");
    expect(ariaSort({ key: "name", dir: "desc" }, "name")).toBe("descending");
    expect(ariaSort(byName, "cap")).toBe("none");
  });
});

describe("rows in an order", () => {
  const rows = [
    { name: "Ld10", cap: 5000 },
    { name: "Ld2", cap: undefined },
    { name: "Ld1", cap: 7000 },
    { name: "Ld3", cap: 5000 },
  ];
  const columns = {
    name: (r: (typeof rows)[number]) => r.name,
    cap: (r: (typeof rows)[number]) => r.cap,
  };
  const names = (sort: Sort) => sortRows(rows, sort, columns).map((r) => r.name);

  it("compares words as words, with the numbers in them as numbers", () => {
    expect(names(byName)).toEqual(["Ld1", "Ld2", "Ld3", "Ld10"]);
    expect(names({ key: "name", dir: "desc" })).toEqual(["Ld10", "Ld3", "Ld2", "Ld1"]);
  });

  it("compares numbers as numbers, keeps equals in the order they came, and puts nothing last", () => {
    expect(names({ key: "cap", dir: "asc" })).toEqual(["Ld10", "Ld3", "Ld1", "Ld2"]);
    // The other way, a row with nothing is still last, and equals still keep
    // their order.
    expect(names({ key: "cap", dir: "desc" })).toEqual(["Ld1", "Ld10", "Ld3", "Ld2"]);
    const blank = [{ v: undefined }, { v: undefined }, { v: 1 }];
    expect(sortRows(blank, { key: "v", dir: "asc" }, { v: (r) => r.v })).toEqual([
      { v: 1 },
      { v: undefined },
      { v: undefined },
    ]);
  });

  it("leaves the rows as they came for a column the table has not, and never changes what it was given", () => {
    const before = [...rows];
    expect(names({ key: "colour", dir: "asc" })).toEqual(["Ld10", "Ld2", "Ld1", "Ld3"]);
    expect(rows).toEqual(before);
    expect(sortRows(rows, byName, columns)).not.toBe(rows);
  });
});
