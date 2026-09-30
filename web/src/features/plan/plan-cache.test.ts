import { describe, expect, it } from "vitest";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { SLOTS, SLOT_LABELS, withEntry, withoutSlot, type SlotChange } from "./plan-cache";

const D = "2026-09-28";
const change = (over: Partial<SlotChange> = {}): SlotChange => ({ date: D, slot: "breakfast", mealId: "m-new", mealName: "Pasta", portion: 1.5, ...over });
const names = (range: ReturnType<typeof makePlan>, date = D) => range.days.find((d) => d.date === date)?.entries.map((e) => `${e.slot}:${e.meal_name}:${e.portion}`);

describe("slots", () => {
  it("lists the four slots in the order of a day, with their labels", () => {
    expect(SLOTS).toEqual(["breakfast", "lunch", "dinner", "snack"]);
    expect(SLOT_LABELS).toEqual({ breakfast: "Breakfast", lunch: "Lunch", dinner: "Dinner", snack: "Snacks" });
  });
});

describe("withEntry", () => {
  const plan = makePlan(D, "2026-09-29", [
    makeDay(D, [makeEntry({ id: "e-b", slot: "breakfast", meal_name: "Porridge" }), makeEntry({ id: "e-s1", slot: "snack", meal_name: "Apple" })]),
    makeDay("2026-09-29", [makeEntry({ id: "e-x", date: "2026-09-29", meal_name: "Toast" })]),
  ]);

  it("replaces the meal and portion of a breakfast, lunch or dinner, keeping the entry's id", () => {
    const next = withEntry(plan, change());
    expect(names(next)).toEqual(["breakfast:Pasta:1.5", "snack:Apple:1"]);
    expect(next.days[0]?.entries[0]?.id).toBe("e-b");
    expect(next.days[0]?.entries[0]?.from_template_id).toBeNull();
  });

  it("adds an entry to an empty slot", () => {
    expect(names(withEntry(plan, change({ slot: "dinner", mealName: "Soup", portion: 1 })))).toEqual(["breakfast:Porridge:1", "snack:Apple:1", "dinner:Soup:1"]);
  });

  it("always adds a snack, never replacing one, because the API can only add snacks by slot", () => {
    const next = withEntry(plan, change({ slot: "snack", mealName: "Nuts", portion: 1 }));
    expect(names(next)).toEqual(["breakfast:Porridge:1", "snack:Apple:1", "snack:Nuts:1"]);
    expect(new Set(next.days[0]?.entries.map((e) => e.id)).size).toBe(3);
  });

  it("leaves other days, the totals and the targets alone, and never mutates its input", () => {
    const before = JSON.stringify(plan);
    const next = withEntry(plan, change());
    expect(JSON.stringify(plan)).toBe(before);
    expect(next.days[1]).toBe(plan.days[1]);
    expect(next.days[0]?.nutrition_per_day).toBe(plan.days[0]?.nutrition_per_day);
    expect(next.targets).toBe(plan.targets);
  });

  it("does nothing for a date outside the cached range", () => {
    expect(withEntry(plan, change({ date: "2027-01-01" })).days).toEqual(plan.days);
  });
});

describe("withoutSlot", () => {
  const plan = makePlan(D, D, [
    makeDay(D, [
      makeEntry({ id: "e-b", slot: "breakfast" }),
      makeEntry({ id: "e-s1", slot: "snack", meal_name: "Apple" }),
      makeEntry({ id: "e-s2", slot: "snack", meal_name: "Nuts" }),
    ]),
  ]);

  it("removes the one entry of a single-meal slot", () => {
    expect(names(withoutSlot(plan, D, "breakfast"))).toEqual(["snack:Apple:1", "snack:Nuts:1"]);
  });

  it("removes every snack of that day, as the API does", () => {
    expect(names(withoutSlot(plan, D, "snack"))).toEqual(["breakfast:Oat bowl:1"]);
  });

  it("never mutates its input", () => {
    const before = JSON.stringify(plan);
    withoutSlot(plan, D, "snack");
    expect(JSON.stringify(plan)).toBe(before);
  });
});
