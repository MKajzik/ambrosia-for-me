import { describe, expect, it } from "vitest";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { MAX_SLOTS, diffTemplate, draftFromTemplate, hasTemplateChanges, newSlotRow, savedFromTemplate, validateTemplate, type SlotRow, type TemplateDraft } from "./template-draft";

const oats = { id: "m1", name: "Oat bowl" };
const draft = (rows: SlotRow[] = [], over: Partial<TemplateDraft> = {}): TemplateDraft => ({ name: "Base week", shared: false, rows, ...over });
const row = (dayIndex: number, slot: SlotRow["slot"], over: Partial<SlotRow> = {}): SlotRow => ({ ...newSlotRow(dayIndex, slot, oats), ...over });

function valid(d: TemplateDraft, dayCount = 7) {
  const result = validateTemplate(d, dayCount);
  if (!result.ok) throw new Error(`expected a valid draft: ${JSON.stringify(result.errors)}`);
  return result.value;
}
function errors(d: TemplateDraft, dayCount = 7) {
  const result = validateTemplate(d, dayCount);
  if (result.ok) throw new Error("expected errors");
  return result.errors;
}

describe("a template and its draft", () => {
  const template = makeTemplate({
    day_count: 3,
    shared_with_partner: true,
    slots: [
      makeTemplateSlot({ id: "c", day_index: 1, slot: "dinner", meal_id: "m3", meal_name: "Stew", portion: 2 }),
      makeTemplateSlot({ id: "a", day_index: 0, slot: "lunch", meal_id: "m2", meal_name: "Wrap", portion: 1 }),
      makeTemplateSlot({ id: "b", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1.5 }),
    ],
  });

  it("turns into editable text, ordered as the days read", () => {
    const d = draftFromTemplate(template);
    expect(d).toMatchObject({ name: "Base week", shared: true });
    expect(d.rows.map((r) => [r.key, r.dayIndex, r.slot, r.mealName, r.portion])).toEqual([
      ["b", 0, "breakfast", "Oat bowl", "1.5"],
      ["a", 0, "lunch", "Wrap", "1"],
      ["c", 1, "dinner", "Stew", "2"],
    ]);
  });

  it("round-trips: an untouched draft validates to exactly what the server holds, so nothing is saved", () => {
    const saved = savedFromTemplate(template);
    const next = valid(draftFromTemplate(template), 3);
    expect(next).toEqual(saved);
    expect(diffTemplate(saved, next)).toEqual({ patch: null, items: null });
    expect(hasTemplateChanges(saved, next)).toBe(false);
  });
});

describe("validateTemplate", () => {
  it("accepts a good draft and trims the name", () => {
    expect(valid(draft([], { name: "  Base week  " }))).toEqual({ name: "Base week", shared: false, items: [] });
  });

  it("sorts the slots by day, then by slot of the day, and keeps snacks in the order added", () => {
    const rows = [row(1, "dinner"), row(0, "snack", { mealId: "s1" }), row(0, "breakfast"), row(0, "snack", { mealId: "s2" })];
    expect(valid(draft(rows)).items.map((i) => [i.day_index, i.slot, i.meal_id])).toEqual([
      [0, "breakfast", "m1"],
      [0, "snack", "s1"],
      [0, "snack", "s2"],
      [1, "dinner", "m1"],
    ]);
  });

  it("is not changed by the order rows were added in", () => {
    const a = row(0, "breakfast");
    const b = row(1, "dinner");
    expect(valid(draft([a, b])).items).toEqual(valid(draft([b, a])).items);
  });

  it("reads a comma as a decimal point in a portion", () => {
    expect(valid(draft([row(0, "breakfast", { portion: "1,5" })])).items[0]?.portion).toBe(1.5);
  });

  it.each([["0"], ["-1"], ["abc"], [""], ["1e2"], ["100.5"]])("refuses a portion of %j on that row only", (portion) => {
    const bad = row(0, "breakfast", { portion });
    const fine = row(0, "lunch");
    expect(Object.keys(errors(draft([fine, bad])).rows)).toEqual([bad.key]);
  });

  it("flags a second breakfast, lunch or dinner on the same day, on the second row only, but allows many snacks", () => {
    const first = row(0, "lunch");
    const second = row(0, "lunch", { mealId: "m2" });
    expect(errors(draft([first, second])).rows).toEqual({ [second.key]: "This day already has lunch." });
    expect(validateTemplate(draft([row(0, "snack"), row(0, "snack"), row(0, "snack")]), 7).ok).toBe(true);
    expect(validateTemplate(draft([row(0, "lunch"), row(1, "lunch")]), 7).ok).toBe(true);
  });

  it("flags a slot on a day the template does not have", () => {
    const outside = row(3, "breakfast");
    expect(errors(draft([outside]), 3).rows).toEqual({ [outside.key]: "This slot is outside the template's days." });
  });

  it("asks for a name of at most 200 characters, and at most 500 slots", () => {
    expect(errors(draft([], { name: "  " })).name).toBe("Give the template a name.");
    expect(errors(draft([], { name: "x".repeat(201) })).name).toBe("Use at most 200 characters.");
    const many = Array.from({ length: MAX_SLOTS + 1 }, (_, i) => row(i % 7, "snack"));
    expect(errors(draft(many)).items).toBe("A template can have at most 500 slots.");
  });
});

describe("diffTemplate", () => {
  const saved = valid(draft([row(0, "breakfast", { key: "a" })]));

  it("sends only the fields that changed, with the API's field name for sharing", () => {
    expect(diffTemplate(saved, { ...saved, name: "Cut" })).toEqual({ patch: { name: "Cut" }, items: null });
    expect(diffTemplate(saved, { ...saved, shared: true })).toEqual({ patch: { shared_with_partner: true }, items: null });
  });

  it("sends the whole slot list when any slot changes, and never a patch for it", () => {
    const changed = { ...saved, items: [{ day_index: 0, slot: "breakfast" as const, meal_id: "m1", portion: 2 }] };
    expect(diffTemplate(saved, changed)).toEqual({ patch: null, items: changed.items });
    expect(diffTemplate(saved, { ...saved, items: [] })).toEqual({ patch: null, items: [] });
  });
});
