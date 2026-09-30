import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type NutrientAmounts = Schemas["NutrientAmounts"];

const KEYS = [
  "calories",
  "protein",
  "carbohydrates",
  "sugar",
  "fibre",
  "fat",
  "saturated_fat",
  "sodium",
  "potassium",
  "calcium",
  "iron",
  "magnesium",
  "zinc",
  "vitamin_a",
  "vitamin_c",
  "vitamin_d",
  "vitamin_b12",
  "folate",
] as const;

/** All 18 keys, zero unless overridden (a meal with no ingredients reports zeros). */
export function nutrients(overrides: Partial<NutrientAmounts> = {}): NutrientAmounts {
  return { ...(Object.fromEntries(KEYS.map((k) => [k, 0])) as NutrientAmounts), ...overrides };
}

/** All 18 keys unknown, as the API reports a nutrient some ingredient lacks. */
export function unknownNutrients(overrides: Partial<NutrientAmounts> = {}): NutrientAmounts {
  return { ...(Object.fromEntries(KEYS.map((k) => [k, null])) as NutrientAmounts), ...overrides };
}

const STAMP = "2026-01-01T00:00:00Z";

export function makeIngredient(overrides: Partial<Schemas["Ingredient"]> = {}): Schemas["Ingredient"] {
  return {
    id: "ing-oats",
    name: "Rolled oats",
    category: "grains_bread",
    is_custom: false,
    grams_per_piece: null,
    density_g_per_ml: null,
    nutrients: nutrients({ calories: 380 }),
    created_at: STAMP,
    updated_at: STAMP,
    ...overrides,
  };
}

export function makeMeal(overrides: Partial<Schemas["Meal"]> = {}): Schemas["Meal"] {
  return {
    id: "m1",
    name: "Oat bowl",
    notes: null,
    servings: 2,
    shared_with_partner: false,
    is_owner: true,
    ingredients: [],
    nutrition_per_serving: nutrients(),
    created_at: STAMP,
    updated_at: STAMP,
    ...overrides,
  };
}

export function makeSummary(overrides: Partial<Schemas["MealSummary"]> = {}): Schemas["MealSummary"] {
  return { id: "m1", name: "Oat bowl", notes: null, servings: 2, shared_with_partner: false, created_at: STAMP, updated_at: STAMP, ...overrides };
}

export function partnership(status: "active" | "pending" = "active"): Schemas["Partnership"] {
  return status === "active"
    ? { status, display_name: "Sam", linked_at: STAMP, expires_at: null }
    : { status, display_name: null, linked_at: null, expires_at: "2026-02-01T00:00:00Z" };
}
