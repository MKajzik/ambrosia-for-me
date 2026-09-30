import { describe, expect, it } from "vitest";
import { makeMeal } from "@/test/fixtures";
import { MAX_INGREDIENTS, diffDraft, draftFromMeal, hasChanges, newRow, savedFromMeal, validateDraft, type Draft, type DraftRow } from "./draft";

const oats = { id: "ing-oats", name: "Rolled oats", category: "grains_bread" as const };
const good: Draft = { name: "Oat bowl", notes: "", servings: "2", shared: false, rows: [] };
const row = (over: Partial<DraftRow> = {}): DraftRow => ({ ...newRow(oats), ...over });

function valid(draft: Draft) {
  const result = validateDraft(draft);
  if (!result.ok) throw new Error(`expected a valid draft: ${JSON.stringify(result.errors)}`);
  return result.value;
}
function errors(draft: Draft) {
  const result = validateDraft(draft);
  if (result.ok) throw new Error("expected errors");
  return result.errors;
}

describe("a meal and its draft", () => {
  const meal = makeMeal({
    notes: "Quick",
    shared_with_partner: true,
    servings: 1.5,
    ingredients: [{ id: "line1", ingredient_id: "ing-oats", ingredient_name: "Rolled oats", ingredient_category: "grains_bread", quantity: 80, unit: "g", position: 0 }],
  });

  it("turns into text the person can edit", () => {
    expect(draftFromMeal(meal)).toEqual({
      name: "Oat bowl",
      notes: "Quick",
      servings: "1.5",
      shared: true,
      rows: [{ key: "line1", ingredientId: "ing-oats", name: "Rolled oats", category: "grains_bread", quantity: "80", unit: "g" }],
    });
    expect(draftFromMeal(makeMeal({ notes: null })).notes).toBe("");
  });

  it("round-trips: an untouched draft validates to exactly what the server holds, so nothing is saved", () => {
    const saved = savedFromMeal(meal);
    const next = valid(draftFromMeal(meal));
    expect(next).toEqual(saved);
    expect(diffDraft(saved, next)).toEqual({ patch: null, items: null });
    expect(hasChanges(saved, next)).toBe(false);
  });
});

describe("validateDraft", () => {
  it("accepts a good draft, trims the name and turns blank notes into null", () => {
    expect(valid({ ...good, name: "  Oat bowl  ", notes: "  " })).toEqual({ name: "Oat bowl", notes: null, servings: 2, shared: false, items: [] });
  });

  it("reads a comma as a decimal point in servings and in amounts", () => {
    const r = row({ quantity: "2,5", unit: "ml" });
    expect(valid({ ...good, servings: "1,5", rows: [r] })).toMatchObject({ servings: 1.5, items: [{ ingredient_id: "ing-oats", quantity: 2.5, unit: "ml" }] });
  });

  it.each([["0"], ["-1"], ["abc"], [""], ["1e2"], ["1001"]])("refuses servings %j and says how many are allowed", (servings) => {
    expect(errors({ ...good, servings }).servings).toMatch(/servings/i);
  });

  it.each([["0"], ["-2"], ["abc"], [""], ["1e3"], ["100001"]])("refuses an amount of %j on that row only", (quantity) => {
    const bad = row({ quantity });
    const fine = row({ quantity: "50" });
    const found = errors({ ...good, rows: [fine, bad] });
    expect(Object.keys(found.rows)).toEqual([bad.key]);
  });

  it("reports every problem at once", () => {
    const bad = row({ quantity: "" });
    expect(errors({ name: " ", notes: "x".repeat(2001), servings: "0", shared: false, rows: [bad] })).toEqual({
      name: "Give the meal a name.",
      notes: "Use at most 2000 characters.",
      servings: "Servings must be more than 0 and at most 1000.",
      rows: { [bad.key]: "Enter an amount." },
    });
  });

  it("caps the name at 200 characters and the list at 200 ingredients", () => {
    expect(errors({ ...good, name: "x".repeat(201) }).name).toBe("Use at most 200 characters.");
    const rows = Array.from({ length: MAX_INGREDIENTS + 1 }, () => row());
    expect(errors({ ...good, rows }).ingredients).toBe("A meal can have at most 200 ingredients.");
  });
});

describe("diffDraft", () => {
  const saved = valid({ ...good, notes: "Quick", rows: [row({ key: "a" })] });

  it("sends only the fields that changed", () => {
    expect(diffDraft(saved, { ...saved, name: "Porridge" })).toEqual({ patch: { name: "Porridge" }, items: null });
    expect(diffDraft(saved, { ...saved, servings: 3, shared: true })).toEqual({ patch: { servings: 3, shared_with_partner: true }, items: null });
  });

  it("clears notes with null, not an empty string", () => {
    expect(diffDraft(saved, { ...saved, notes: null })).toEqual({ patch: { notes: null }, items: null });
  });

  it("sends the whole ingredient list when any line changes, and never a patch for it", () => {
    const changed = { ...saved, items: [{ ingredient_id: "ing-oats", quantity: 120, unit: "g" as const }] };
    expect(diffDraft(saved, changed)).toEqual({ patch: null, items: changed.items });
    expect(diffDraft(saved, { ...saved, items: [] })).toEqual({ patch: null, items: [] });
  });

  it("counts a reorder as a change", () => {
    const a = { ingredient_id: "a", quantity: 1, unit: "g" as const };
    const b = { ingredient_id: "b", quantity: 1, unit: "g" as const };
    expect(diffDraft({ ...saved, items: [a, b] }, { ...saved, items: [b, a] }).items).toEqual([b, a]);
  });
});

describe("newRow", () => {
  it("starts at a sensible valid amount, and every row gets its own key", () => {
    const first = newRow(oats);
    const second = newRow(oats);
    expect(first).toMatchObject({ ingredientId: "ing-oats", name: "Rolled oats", category: "grains_bread", quantity: "100", unit: "g" });
    expect(first.key).not.toBe(second.key);
  });
});
