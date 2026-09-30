import { describe, expect, it } from "vitest";
import { makeItem } from "@/test/shopping-fixtures";
import { checkedCount, formatItemQuantity, groupByCategory, rangeLabel } from "./items";

describe("groupByCategory", () => {
  it("groups by aisle in the order the catalogue lists the categories, leaving out empty ones", () => {
    const groups = groupByCategory([
      makeItem({ id: "a", category: "other", name: "Bin bags" }),
      makeItem({ id: "b", category: "produce", name: "Apples" }),
      makeItem({ id: "c", category: "dairy_eggs", name: "Milk" }),
    ]);
    expect(groups.map((g) => g.category)).toEqual(["produce", "dairy_eggs", "other"]);
  });

  it("puts unchecked items before checked ones within an aisle, each in list order", () => {
    const [group] = groupByCategory([
      makeItem({ id: "1", category: "produce", position: 0, checked: true, name: "Apples" }),
      makeItem({ id: "2", category: "produce", position: 1, name: "Carrots" }),
      makeItem({ id: "3", category: "produce", position: 2, checked: true, name: "Leeks" }),
      makeItem({ id: "4", category: "produce", position: 3, name: "Onions" }),
    ]);
    expect(group?.items.map((i) => i.name)).toEqual(["Carrots", "Onions", "Apples", "Leeks"]);
  });

  it("does not change the list it was given", () => {
    const items = [makeItem({ id: "1", checked: true }), makeItem({ id: "2", position: 1 })];
    const before = JSON.stringify(items);
    groupByCategory(items);
    expect(JSON.stringify(items)).toBe(before);
  });

  it("is empty for no items", () => {
    expect(groupByCategory([])).toEqual([]);
  });
});

describe("checkedCount", () => {
  it("counts checked and all items", () => {
    expect(checkedCount([makeItem({ id: "1", checked: true }), makeItem({ id: "2" }), makeItem({ id: "3", checked: true })])).toEqual({ done: 2, total: 3 });
    expect(checkedCount([])).toEqual({ done: 0, total: 0 });
  });
});

describe("formatItemQuantity", () => {
  it.each([
    [{ quantity: null, unit: null }, ""],
    [{ quantity: null, unit: "g" as const }, ""],
    [{ quantity: 500, unit: "g" as const }, "500 g"],
    [{ quantity: 1.5, unit: "ml" as const }, "1.5 ml"],
    [{ quantity: 1000, unit: "ml" as const }, "1,000 ml"],
    [{ quantity: 1, unit: "piece" as const }, "1 piece"],
    [{ quantity: 6, unit: "piece" as const }, "6 pieces"],
    [{ quantity: 2, unit: null }, "2"],
    [{ quantity: 0.333333, unit: "g" as const }, "0.33 g"],
  ])("writes %j as %j", (item, text) => {
    expect(formatItemQuantity(item)).toBe(text);
  });
});

describe("rangeLabel", () => {
  it("names the plan range a list was generated from", () => {
    expect(rangeLabel({ source_from: "2026-09-28", source_to: "2026-10-04" })).toBe("Sep 28 – Oct 4");
  });

  it("is null for a list that was not generated", () => {
    expect(rangeLabel({ source_from: null, source_to: null })).toBeNull();
    expect(rangeLabel({ source_from: "2026-09-28", source_to: null })).toBeNull();
  });
});
