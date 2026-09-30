import type { IngredientCategory } from "@/lib/ingredient-categories";
import { parseDecimal } from "@/lib/parse-number";
import type { components } from "@/lib/api/schema.gen";
import type { Meal, MealIngredientInput, UpdateMealRequest } from "./queries";

export type Unit = components["schemas"]["Unit"];
export const UNITS: readonly Unit[] = ["g", "ml", "piece"];
export const MAX_INGREDIENTS = 200;

/** A line of the meal as the person is editing it: the amount is the text they typed. */
export type DraftRow = { key: string; ingredientId: string; name: string; category: IngredientCategory; quantity: string; unit: Unit };
export type Draft = { name: string; notes: string; servings: string; shared: boolean; rows: DraftRow[] };
/** A draft that passed validation, in the shape the API takes. */
export type ValidDraft = { name: string; notes: string | null; servings: number; shared: boolean; items: MealIngredientInput[] };
export type DraftErrors = { name?: string; notes?: string; servings?: string; ingredients?: string; rows: Record<string, string> };

let rowCounter = 0;

/** A fresh line for an ingredient just picked from the search: 100 g, so it is valid the moment it appears. */
export function newRow(ingredient: { id: string; name: string; category: IngredientCategory }): DraftRow {
  rowCounter += 1;
  return { key: `new-${rowCounter}`, ingredientId: ingredient.id, name: ingredient.name, category: ingredient.category, quantity: "100", unit: "g" };
}

export function draftFromMeal(meal: Meal): Draft {
  return {
    name: meal.name,
    notes: meal.notes ?? "",
    servings: String(meal.servings),
    shared: meal.shared_with_partner,
    rows: meal.ingredients.map((line) => ({
      key: line.id,
      ingredientId: line.ingredient_id,
      name: line.ingredient_name,
      category: line.ingredient_category,
      quantity: String(line.quantity),
      unit: line.unit,
    })),
  };
}

/** What the server holds, in the same shape validation produces, so the two can be compared. */
export function savedFromMeal(meal: Meal): ValidDraft {
  return {
    name: meal.name,
    notes: meal.notes,
    servings: meal.servings,
    shared: meal.shared_with_partner,
    items: meal.ingredients.map((line) => ({ ingredient_id: line.ingredient_id, quantity: line.quantity, unit: line.unit })),
  };
}

export function validateDraft(draft: Draft): { ok: true; value: ValidDraft } | { ok: false; errors: DraftErrors } {
  const errors: DraftErrors = { rows: {} };

  const name = draft.name.trim();
  if (!name) errors.name = "Give the meal a name.";
  else if (name.length > 200) errors.name = "Use at most 200 characters.";

  if (draft.notes.length > 2000) errors.notes = "Use at most 2000 characters.";

  const servings = parseDecimal(draft.servings);
  const servingsValue = servings.ok ? servings.value : null;
  if (servingsValue === null || servingsValue <= 0 || servingsValue > 1000) errors.servings = "Servings must be more than 0 and at most 1000.";

  if (draft.rows.length > MAX_INGREDIENTS) errors.ingredients = `A meal can have at most ${MAX_INGREDIENTS} ingredients.`;

  const items: MealIngredientInput[] = [];
  for (const row of draft.rows) {
    const quantity = parseDecimal(row.quantity);
    const value = quantity.ok ? quantity.value : null;
    if (value === null) errors.rows[row.key] = "Enter an amount.";
    else if (value <= 0 || value > 100000) errors.rows[row.key] = "The amount must be more than 0 and at most 100000.";
    else items.push({ ingredient_id: row.ingredientId, quantity: value, unit: row.unit });
  }

  if (errors.name || errors.notes || errors.servings || errors.ingredients || Object.keys(errors.rows).length > 0 || servingsValue === null) {
    return { ok: false, errors };
  }
  const notes = draft.notes.trim();
  return { ok: true, value: { name, notes: notes === "" ? null : notes, servings: servingsValue, shared: draft.shared, items } };
}

/** The writes needed to bring the server from `saved` to `next`: a patch of changed fields, and the whole list if any line changed. */
export function diffDraft(saved: ValidDraft, next: ValidDraft): { patch: UpdateMealRequest | null; items: MealIngredientInput[] | null } {
  const patch: UpdateMealRequest = {};
  if (next.name !== saved.name) patch.name = next.name;
  if (next.notes !== saved.notes) patch.notes = next.notes;
  if (next.servings !== saved.servings) patch.servings = next.servings;
  if (next.shared !== saved.shared) patch.shared_with_partner = next.shared;
  // Both sides build items as { ingredient_id, quantity, unit }, so key order is stable.
  const sameItems = JSON.stringify(next.items) === JSON.stringify(saved.items);
  return { patch: Object.keys(patch).length > 0 ? patch : null, items: sameItems ? null : next.items };
}

export function hasChanges(saved: ValidDraft, next: ValidDraft): boolean {
  const { patch, items } = diffDraft(saved, next);
  return patch !== null || items !== null;
}
