import { describe, expect, it } from "vitest";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { OPTIMISTIC_PREFIX, isOptimistic, isStaleEvent, optimisticItem, restoreItem, withAddedItem, withChecked, withItem, withoutItem } from "./list-cache";

const list = makeList({
  items: [makeItem({ id: "a", name: "Apples", position: 0, version: 2 }), makeItem({ id: "b", name: "Bread", position: 1, version: 1, checked: true, checked_by: "u2" })],
});
const names = (l: ReturnType<typeof makeList>) => l.items.map((i) => i.name);

describe("withChecked", () => {
  it("checks an item for the person who did, without touching its version", () => {
    const next = withChecked(list, "a", true, "u1");
    expect(next.items[0]).toMatchObject({ checked: true, checked_by: "u1", version: 2 });
    expect(next.items[1]).toBe(list.items[1]);
  });

  it("clears who checked it when it is unchecked", () => {
    expect(withChecked(list, "b", false, "u1").items[1]).toMatchObject({ checked: false, checked_by: null });
  });

  it("leaves the list alone for an unknown item, and never mutates its input", () => {
    const before = JSON.stringify(list);
    expect(withChecked(list, "zzz", true, "u1")).toEqual(list);
    withChecked(list, "a", true, "u1");
    expect(JSON.stringify(list)).toBe(before);
  });
});

describe("withAddedItem and withoutItem", () => {
  it("appends and removes", () => {
    const added = withAddedItem(list, makeItem({ id: "c", name: "Cheese", position: 2 }));
    expect(names(added)).toEqual(["Apples", "Bread", "Cheese"]);
    expect(names(withoutItem(added, "a"))).toEqual(["Bread", "Cheese"]);
    expect(names(withoutItem(list, "zzz"))).toEqual(["Apples", "Bread"]);
  });
});

describe("withItem", () => {
  it("inserts an item it does not know, in position order", () => {
    expect(names(withItem(list, makeItem({ id: "c", name: "Between", position: 0.5 })))).toEqual(["Apples", "Between", "Bread"]);
  });

  it("replaces a known item with a newer or equal version", () => {
    expect(withItem(list, makeItem({ id: "a", name: "Apples (red)", version: 3 })).items[0]?.name).toBe("Apples (red)");
    expect(withItem(list, makeItem({ id: "a", name: "Same version", version: 2 })).items[0]?.name).toBe("Same version");
  });

  it("ignores an older version, so a late answer never reverts newer state", () => {
    expect(withItem(list, makeItem({ id: "a", name: "Old", version: 1 }))).toBe(list);
  });
});

describe("restoreItem", () => {
  it("puts back exactly the item it is given, whatever the versions", () => {
    const changed = withItem(list, makeItem({ id: "a", name: "Apples (red)", version: 5 }));
    expect(restoreItem(changed, list.items[0]!).items[0]).toBe(list.items[0]);
  });

  it("puts back an item that is gone", () => {
    expect(names(restoreItem(withoutItem(list, "a"), list.items[0]!))).toEqual(["Apples", "Bread"]);
  });
});

describe("optimisticItem and isOptimistic", () => {
  it("is an unchecked manual item marked as not yet saved", () => {
    const item = optimisticItem("l1", { name: "Milk" }, 7, `${OPTIMISTIC_PREFIX}1`);
    expect(item).toMatchObject({ id: "optimistic:1", list_id: "l1", name: "Milk", category: "other", checked: false, origin: "manual", position: 7, version: 1, ingredient_id: null, quantity: null, unit: null });
    expect(isOptimistic(item)).toBe(true);
    expect(isOptimistic(makeItem())).toBe(false);
  });

  it("takes the ingredient and category it is given", () => {
    expect(optimisticItem("l1", { name: "Kale", ingredientId: "ing1", category: "produce" }, 0, "optimistic:2")).toMatchObject({ ingredient_id: "ing1", category: "produce" });
  });
});

describe("isStaleEvent", () => {
  it("is true for an event at or below the version already held, false for a newer one", () => {
    expect(isStaleEvent(list, "a", 2)).toBe(true);
    expect(isStaleEvent(list, "a", 1)).toBe(true);
    expect(isStaleEvent(list, "a", 3)).toBe(false);
  });

  it("is false for an item it does not hold, or when there is no list yet", () => {
    expect(isStaleEvent(list, "zzz", 1)).toBe(false);
    expect(isStaleEvent(undefined, "a", 1)).toBe(false);
  });
});
