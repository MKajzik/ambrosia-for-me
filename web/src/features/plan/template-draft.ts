import { parseDecimal } from "@/lib/parse-number";
import { SLOTS, SLOT_LABELS, type Slot } from "./plan-cache";
import type { DietTemplate, TemplateSlotInput, UpdateDietTemplateRequest } from "./template-queries";

/** A slot of the template as it is being edited: the portion is the text typed. */
export type SlotRow = { key: string; dayIndex: number; slot: Slot; mealId: string; mealName: string; portion: string };
export type TemplateDraft = { name: string; shared: boolean; rows: SlotRow[] };
/** A draft that passed validation, in the shape the API takes. `items` are ordered by day, slot of the day, then the order added. */
export type ValidTemplate = { name: string; shared: boolean; items: TemplateSlotInput[] };
export type TemplateErrors = { name?: string; items?: string; rows: Record<string, string> };
export const MAX_SLOTS = 500;

let rowCounter = 0;

/** A fresh slot for a meal just picked: a portion of 1, so it is valid the moment it appears. */
export function newSlotRow(dayIndex: number, slot: Slot, meal: { id: string; name: string }): SlotRow {
  rowCounter += 1;
  return { key: `new-${rowCounter}`, dayIndex, slot, mealId: meal.id, mealName: meal.name, portion: "1" };
}

/** Day, then slot of the day, then the order added (`sort` is stable): the same content always gives the same list. */
function ordered<T extends { day_index: number; slot: Slot }>(items: T[]): T[] {
  return [...items].sort((a, b) => a.day_index - b.day_index || SLOTS.indexOf(a.slot) - SLOTS.indexOf(b.slot));
}

export function draftFromTemplate(template: DietTemplate): TemplateDraft {
  return {
    name: template.name,
    shared: template.shared_with_partner,
    rows: ordered(template.slots.map((s) => ({ day_index: s.day_index, slot: s.slot, row: s }))).map(({ row }) => ({
      key: row.id,
      dayIndex: row.day_index,
      slot: row.slot,
      mealId: row.meal_id,
      mealName: row.meal_name,
      portion: String(row.portion),
    })),
  };
}

/** What the server holds, in the same shape validation produces, so the two can be compared. */
export function savedFromTemplate(template: DietTemplate): ValidTemplate {
  return {
    name: template.name,
    shared: template.shared_with_partner,
    items: ordered(template.slots.map((s) => ({ day_index: s.day_index, slot: s.slot, meal_id: s.meal_id, portion: s.portion }))),
  };
}

export function validateTemplate(draft: TemplateDraft, dayCount: number): { ok: true; value: ValidTemplate } | { ok: false; errors: TemplateErrors } {
  const errors: TemplateErrors = { rows: {} };

  const name = draft.name.trim();
  if (!name) errors.name = "Give the template a name.";
  else if (name.length > 200) errors.name = "Use at most 200 characters.";

  if (draft.rows.length > MAX_SLOTS) errors.items = `A template can have at most ${MAX_SLOTS} slots.`;

  const taken = new Set<string>();
  const items: TemplateSlotInput[] = [];
  for (const row of draft.rows) {
    const portion = parseDecimal(row.portion);
    const value = portion.ok ? portion.value : null;
    if (value === null) errors.rows[row.key] = "Enter a portion.";
    else if (value <= 0 || value > 100) errors.rows[row.key] = "The portion must be more than 0 and at most 100.";
    else if (row.dayIndex < 0 || row.dayIndex >= dayCount) errors.rows[row.key] = "This slot is outside the template's days.";
    else if (row.slot !== "snack" && taken.has(`${row.dayIndex}:${row.slot}`)) errors.rows[row.key] = `This day already has ${SLOT_LABELS[row.slot].toLowerCase()}.`;
    else items.push({ day_index: row.dayIndex, slot: row.slot, meal_id: row.mealId, portion: value });
    if (row.slot !== "snack") taken.add(`${row.dayIndex}:${row.slot}`);
  }

  if (errors.name || errors.items || Object.keys(errors.rows).length > 0) return { ok: false, errors };
  return { ok: true, value: { name, shared: draft.shared, items: ordered(items) } };
}

/** The writes needed to bring the server from `saved` to `next`: a patch of changed fields, and the whole slot list if any slot changed. */
export function diffTemplate(saved: ValidTemplate, next: ValidTemplate): { patch: UpdateDietTemplateRequest | null; items: TemplateSlotInput[] | null } {
  const patch: UpdateDietTemplateRequest = {};
  if (next.name !== saved.name) patch.name = next.name;
  if (next.shared !== saved.shared) patch.shared_with_partner = next.shared;
  // Both sides build items as { day_index, slot, meal_id, portion } and order them the same way, so this comparison is stable.
  const sameItems = JSON.stringify(next.items) === JSON.stringify(saved.items);
  return { patch: Object.keys(patch).length > 0 ? patch : null, items: sameItems ? null : next.items };
}

export function hasTemplateChanges(saved: ValidTemplate, next: ValidTemplate): boolean {
  const { patch, items } = diffTemplate(saved, next);
  return patch !== null || items !== null;
}
